package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/bilus/documentation-portal/portal"
	"github.com/bilus/documentation-portal/signin"
)

// accessRules are the rules of the access file: for each label of the
// configuration file, the rules of the readers who have the label, and the
// previews rules, of the readers who may open previews.
type accessRules struct {
	Labels   map[string][]rule `yaml:"labels"`
	Previews []rule            `yaml:"previews"`
}

// rule matches a reader who signed in through the identity provider named
// Provider, auth0 or github, and whose claim Claim holds one of Values: for
// github in any letter case, as GitHub's names count, and else exactly.
type rule struct {
	Provider string   `yaml:"provider"`
	Claim    string   `yaml:"claim"`
	Values   []string `yaml:"values"`
}

// readAccessFile reads the access rules from the access file at name, or
// refuses an invalid file: one that is missing, that is not YAML or holds
// more than one YAML document, with a key it does not know, a rule without
// a provider, a claim or values, an empty value, a provider other than
// auth0 and github, or a GitHub rule on a claim other than login, orgs and
// teams.
func readAccessFile(name string) (accessRules, error) {
	data, err := os.ReadFile(name)
	if err != nil {
		return accessRules{}, fmt.Errorf("read the access file: %w", err)
	}
	var rules accessRules
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&rules); err != nil && !errors.Is(err, io.EOF) {
		return accessRules{}, fmt.Errorf("access file %s: %w", name, err)
	}
	if err := dec.Decode(new(yaml.Node)); !errors.Is(err, io.EOF) {
		return accessRules{}, fmt.Errorf("access file %s: more than one YAML document", name)
	}
	for label, rs := range rules.Labels {
		for i, r := range rs {
			if err := r.check(); err != nil {
				return accessRules{}, fmt.Errorf("access file %s: labels: %s: rule %d: %w", name, label, i+1, err)
			}
		}
	}
	for i, r := range rules.Previews {
		if err := r.check(); err != nil {
			return accessRules{}, fmt.Errorf("access file %s: previews: rule %d: %w", name, i+1, err)
		}
	}
	return rules, nil
}

// check refuses a rule without a provider, a claim or values, with an empty
// value, of a provider other than auth0 and github, or of github on a claim
// other than login, orgs and teams.
func (r rule) check() error {
	switch {
	case r.Provider != "auth0" && r.Provider != "github":
		return fmt.Errorf("the provider %q is neither auth0 nor github", r.Provider)
	case r.Claim == "":
		return errors.New("no claim")
	case r.Provider == "github" && r.Claim != "login" && r.Claim != "orgs" && r.Claim != "teams":
		return fmt.Errorf("GitHub gives no claim %q, only login, orgs and teams", r.Claim)
	case len(r.Values) == 0:
		return errors.New("no values")
	case slices.Contains(r.Values, ""):
		return errors.New("an empty value")
	}
	return nil
}

// providerKey keys the provider name of a request's sign-in in the
// request's context.
type providerKey struct{}

// access returns the access of r's reader under the access rules, for
// portal.Config.Access and portal.PreviewsConfig.Access: the labels whose
// rules match the provider of the reader's sign-in and the reader's claims,
// and whether a previews rule matches the reader. A request without the
// provider of its sign-in, or without an identity, gets an access that
// allows nothing.
func (rules accessRules) access(r *http.Request) (portal.Access, error) {
	provider, ok := r.Context().Value(providerKey{}).(string)
	id, signedIn := signin.IdentityOf(r)
	if !ok || !signedIn {
		return readerAccess{}, nil
	}
	return rules.accessOf(provider, id.Claims), nil
}

// accessOf returns the access of a reader who signed in through the
// identity provider named provider with claims: the labels whose rules
// match, and whether a previews rule matches.
func (rules accessRules) accessOf(provider string, claims signin.Claims) readerAccess {
	a := readerAccess{signedIn: true}
	match := func(r rule) bool { return r.matches(provider, claims) }
	for label, rs := range rules.Labels {
		if slices.ContainsFunc(rs, match) {
			a.labels = append(a.labels, label)
		}
	}
	slices.Sort(a.labels)
	a.previews = slices.ContainsFunc(rules.Previews, match)
	return a
}

// matches reports whether r matches a reader who signed in through the
// identity provider named provider with claims.
func (r rule) matches(provider string, claims signin.Claims) bool {
	if r.Provider != provider {
		return false
	}
	same := func(a, b string) bool { return a == b }
	if provider == "github" {
		same = strings.EqualFold
	}
	return slices.ContainsFunc(claims.Strings(r.Claim), func(have string) bool {
		return slices.ContainsFunc(r.Values, func(want string) bool { return same(have, want) })
	})
}

// readerAccess is the access of one signed-in reader under the access
// rules: the reader's labels, and whether the reader may open previews. Its
// zero value is nobody's access, which allows nothing.
type readerAccess struct {
	signedIn bool
	labels   []string
	previews bool
}

// Portal reports whether the reader may see the portal p: one without
// labels, or one with a label of the reader's, for a signed-in reader.
func (a readerAccess) Portal(p portal.Portal) bool {
	return a.open(p.Labels)
}

// Section reports whether the reader may see the section s: one without
// labels, or one with a label of the reader's, for a signed-in reader.
func (a readerAccess) Section(_ portal.Portal, s portal.Section) bool {
	return a.open(s.Labels)
}

// Preview reports whether the reader may open the preview of folder: for a
// reader whom a previews rule matches, every folder.
func (a readerAccess) Preview(folder string) bool {
	return a.signedIn && a.previews
}

// open reports whether the reader may see a portal or a section with
// labels: for a signed-in reader, one without labels, or one with a label
// of the reader's.
func (a readerAccess) open(labels []string) bool {
	return a.signedIn && (len(labels) == 0 || slices.ContainsFunc(labels, func(l string) bool { return slices.Contains(a.labels, l) }))
}
