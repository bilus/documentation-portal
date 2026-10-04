package main

import (
	"net/http"

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
	// HOLE(1): read the access file, and refuse an invalid one
	return accessRules{}, nil
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
	// HOLE(1): read the provider name and the identity of r, and return their access
	return readerAccess{}, nil
}

// accessOf returns the access of a reader who signed in through the
// identity provider named provider with claims: the labels whose rules
// match, and whether a previews rule matches.
func (rules accessRules) accessOf(provider string, claims signin.Claims) readerAccess {
	// HOLE(1): collect the labels with a matching rule, and match the previews rules
	return readerAccess{}
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
	// HOLE(1): allow a portal without labels, or with one of the reader's labels
	return false
}

// Section reports whether the reader may see the section s: one without
// labels, or one with a label of the reader's, for a signed-in reader.
func (a readerAccess) Section(_ portal.Portal, s portal.Section) bool {
	// HOLE(1): allow a section without labels, or with one of the reader's labels
	return false
}

// Preview reports whether the reader may open the preview of folder: for a
// reader whom a previews rule matches, every folder.
func (a readerAccess) Preview(folder string) bool {
	// HOLE(1): allow every folder to a reader whom a previews rule matches
	return false
}
