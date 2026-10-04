package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/bilus/documentation-portal/examples/portalapp/mocks"
	"github.com/bilus/documentation-portal/portal"
	"github.com/bilus/documentation-portal/signin"
)

// writeAccessFile writes an access file with content into a directory of
// the test, and returns its path.
func writeAccessFile(t *testing.T, content string) string {
	t.Helper()
	name := filepath.Join(t.TempDir(), "access.yaml")
	if err := os.WriteFile(name, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return name
}

func TestReadAccessFile(t *testing.T) {
	t.Skip("HOLE(1): read the access rules from the access file")
	rules, err := readAccessFile("demo/access.yaml")
	if err != nil {
		t.Fatal(err)
	}
	want := accessRules{
		Labels: map[string][]rule{
			"staff": {
				{Provider: "auth0", Claim: mocks.RolesClaim, Values: []string{"staff"}},
				{Provider: "github", Claim: "teams", Values: []string{"acme/staff"}},
			},
			"partner": {
				{Provider: "auth0", Claim: mocks.RolesClaim, Values: []string{"partner"}},
				{Provider: "github", Claim: "teams", Values: []string{"acme/partners"}},
			},
		},
		Previews: []rule{{Provider: "github", Claim: "orgs", Values: []string{"acme"}}},
	}
	if !reflect.DeepEqual(rules, want) {
		t.Errorf("the demo's access rules:\n got %+v\nwant %+v", rules, want)
	}

	// The values stay as the file gives them.
	rules, err = readAccessFile(writeAccessFile(t, `
labels:
  staff:
    - {provider: github, claim: orgs, values: [Acme, globex]}
    - {provider: auth0, claim: roles, values: [Staff]}
`))
	if err != nil {
		t.Fatal(err)
	}
	if got := rules.Labels["staff"]; len(got) != 2 || !reflect.DeepEqual(got[0].Values, []string{"Acme", "globex"}) || !reflect.DeepEqual(got[1].Values, []string{"Staff"}) {
		t.Errorf("the rules' values: %+v", got)
	}

	// An empty file holds no rules.
	rules, err = readAccessFile(writeAccessFile(t, ""))
	if err != nil || len(rules.Labels) != 0 || len(rules.Previews) != 0 {
		t.Errorf("an empty access file: %+v, %v", rules, err)
	}
}

func TestReadAccessFileRefusesAnInvalidFile(t *testing.T) {
	t.Skip("HOLE(1): read the access rules from the access file")
	if _, err := readAccessFile(filepath.Join(t.TempDir(), "missing.yaml")); err == nil {
		t.Error("a missing access file opened")
	}
	for name, content := range map[string]string{
		"not YAML":                  "labels: [staff",
		"two documents":             "labels: {}\n---\nlabels: {}\n",
		"an unknown key":            "groups: {}\n",
		"an unknown key of a rule":  "previews: [{provider: github, claim: orgs, values: [acme], team: docs}]\n",
		"a duplicate label":         "labels:\n  staff: []\n  staff: []\n",
		"a rule without a provider": "previews: [{claim: orgs, values: [acme]}]\n",
		"a rule without a claim":    "previews: [{provider: github, values: [acme]}]\n",
		"a rule without values":     "previews: [{provider: github, claim: orgs}]\n",
		"a rule with no values":     "previews: [{provider: github, claim: orgs, values: []}]\n",
		"an empty value":            "previews: [{provider: github, claim: orgs, values: [acme, \"\"]}]\n",
		"a value that is no list":   "previews: [{provider: github, claim: orgs, values: acme}]\n",
		"an unknown provider":       "previews: [{provider: okta, claim: groups, values: [staff]}]\n",
		"a GitHub claim of no kind": "labels: {staff: [{provider: github, claim: groups, values: [staff]}]}\n",
		"labels that are no map":    "labels: [staff]\n",
	} {
		if rules, err := readAccessFile(writeAccessFile(t, content)); err == nil {
			t.Errorf("%s: the access file opened as %+v", name, rules)
		}
	}
}

// The demo's portals and sections, with their labels.
var (
	pets       = portal.Portal{Name: "Pets"}
	partners   = portal.Portal{Name: "Partners", Labels: []string{"partner"}}
	api        = portal.Section{Title: "API", Type: portal.SpecSection}
	staffNotes = portal.Section{Title: "Staff notes", Type: portal.DocsSection, Labels: []string{"staff"}}
)

func TestTheAccessRulesMapClaimsToLabels(t *testing.T) {
	t.Skip("HOLE(1): map a reader's provider and claims to labels")
	rules, err := readAccessFile("demo/access.yaml")
	if err != nil {
		t.Fatal(err)
	}
	for name, tc := range map[string]struct {
		provider        string
		claims          signin.Claims
		partners, staff bool
	}{
		"Ada, staff at Auth0":                {"auth0", signin.Claims{mocks.RolesClaim: []any{"staff"}}, false, true},
		"a partner at Auth0, in a string":    {"auth0", signin.Claims{mocks.RolesClaim: "partner"}, true, false},
		"Grace, a partner at GitHub":         {"github", signin.Claims{"orgs": []any{"acme"}, "teams": []any{"acme/partners"}}, true, false},
		"a staff team at GitHub":             {"github", signin.Claims{"teams": []any{"acme/staff", "acme/partners"}}, true, true},
		"Auth0's claim through GitHub":       {"github", signin.Claims{mocks.RolesClaim: []any{"staff"}}, false, false},
		"GitHub's claim through Auth0":       {"auth0", signin.Claims{"teams": []any{"acme/staff"}}, false, false},
		"a role in another letter case":      {"auth0", signin.Claims{mocks.RolesClaim: []any{"Staff"}}, false, false},
		"a reader without claims":            {"auth0", nil, false, false},
		"a claim of other values":            {"auth0", signin.Claims{mocks.RolesClaim: []any{"visitor"}}, false, false},
		"a provider of no rule":              {"okta", signin.Claims{mocks.RolesClaim: []any{"staff"}}, false, false},
		"a claim that holds no string":       {"auth0", signin.Claims{mocks.RolesClaim: []any{map[string]any{"name": "staff"}}}, false, false},
		"a team of the right slug elsewhere": {"github", signin.Claims{"teams": []any{"globex/staff"}}, false, false},
	} {
		a := rules.accessOf(tc.provider, tc.claims)
		if !a.Portal(pets) || !a.Section(pets, api) {
			t.Errorf("%s: the portal and the section without labels are hidden", name)
		}
		if got := a.Portal(partners); got != tc.partners {
			t.Errorf("%s: the partners' portal is open: %v, want %v", name, got, tc.partners)
		}
		if got := a.Section(pets, staffNotes); got != tc.staff {
			t.Errorf("%s: the staff notes are open: %v, want %v", name, got, tc.staff)
		}
	}

	// A GitHub rule matches GitHub's names in any letter case, as GitHub
	// counts them; an Auth0 rule matches its values exactly.
	cased := accessRules{Labels: map[string][]rule{
		"partner": {{Provider: "github", Claim: "teams", Values: []string{"acme/Partners"}}},
		"staff":   {{Provider: "auth0", Claim: mocks.RolesClaim, Values: []string{"Staff"}}},
	}}
	if a := cased.accessOf("github", signin.Claims{"teams": []any{"Acme/partners"}}); !a.Portal(partners) {
		t.Error("a GitHub team of another letter case is not the rule's")
	}
	if a := cased.accessOf("auth0", signin.Claims{mocks.RolesClaim: []any{"staff"}}); a.Section(pets, staffNotes) {
		t.Error("an Auth0 role of another letter case is the rule's")
	}

	// The zero access is nobody's, and allows nothing.
	var nobody readerAccess
	if nobody.Portal(pets) || nobody.Section(pets, api) || nobody.Preview("pr-1") {
		t.Error("the zero access allows something")
	}
}

func TestPreviewsOpenToTheReadersOfThePreviewRules(t *testing.T) {
	t.Skip("HOLE(1): open previews to the readers whom a rule of the previews matches")
	rules, err := readAccessFile("demo/access.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var grace portal.Access = rules.accessOf("github", signin.Claims{"orgs": []any{"acme"}, "teams": []any{"acme/partners"}})
	previews, ok := grace.(portal.PreviewAccess)
	if !ok {
		t.Fatalf("the access %T opens no previews", grace)
	}
	for _, folder := range []string{"pr-1", "pr-2"} {
		if !previews.Preview(folder) {
			t.Errorf("Grace, of the organization acme, may not open %s", folder)
		}
	}
	for name, a := range map[string]readerAccess{
		"Ada at Auth0":                   rules.accessOf("auth0", signin.Claims{mocks.RolesClaim: []any{"staff"}}),
		"another organization":           rules.accessOf("github", signin.Claims{"orgs": []any{"globex"}}),
		"the organization through Auth0": rules.accessOf("auth0", signin.Claims{"orgs": []any{"acme"}}),
	} {
		if a.Preview("pr-1") {
			t.Errorf("%s may open the preview pr-1", name)
		}
	}
}
