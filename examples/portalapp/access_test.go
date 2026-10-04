package main

import (
	"context"
	"net/http"
	"net/http/httptest"
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
	rules, err := readAccessFile("demo/access.yaml")
	if err != nil {
		t.Fatal(err)
	}
	want := accessRules{
		Labels: map[string][]rule{
			"staff": {
				{Provider: "auth0", Claim: mocks.RolesClaim, Values: []string{"staff"}},
				{Provider: "github", Claim: "team_ids", Values: []string{"2001"}},
			},
			"partner": {
				{Provider: "auth0", Claim: mocks.RolesClaim, Values: []string{"partner"}},
				{Provider: "github", Claim: "team_ids", Values: []string{"2002"}},
			},
		},
		Previews: []rule{{Provider: "github", Claim: "org_ids", Values: []string{"1001"}}},
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

func TestAGitHubRuleMayNameAnID(t *testing.T) {
	rules, err := readAccessFile(writeAccessFile(t, `
labels:
  partner:
    - {provider: github, claim: team_ids, values: [2002]}
  staff:
    - {provider: github, claim: id, values: [2]}
previews:
  - {provider: github, claim: org_ids, values: [1001]}
`))
	if err != nil {
		t.Fatal(err)
	}
	grace := rules.accessOf("github", signin.Claims{"id": "2", "orgs": []any{"Acme"}, "org_ids": []any{"1001"}, "teams": []any{"Acme/partners"}, "team_ids": []any{"2002"}})
	if !grace.Portal(partners) || !grace.Section(pets, staffNotes) || !grace.Preview("pr-1") {
		t.Errorf("Grace's IDs open nothing: %+v", grace)
	}
	// Another account, in an organization and a team of Grace's names but
	// of other IDs, as after a rename, opens nothing.
	other := rules.accessOf("github", signin.Claims{"id": "7", "orgs": []any{"Acme"}, "org_ids": []any{"7001"}, "teams": []any{"Acme/partners"}, "team_ids": []any{"7002"}})
	if other.Portal(partners) || other.Section(pets, staffNotes) || other.Preview("pr-1") {
		t.Errorf("Grace's names open to another account: %+v", other)
	}
}

func TestTheRulesNameTheirValues(t *testing.T) {
	rules := accessRules{
		Labels: map[string][]rule{
			"staff":   {{Provider: "auth0", Claim: mocks.RolesClaim, Values: []string{"staff"}}},
			"partner": {{Provider: "github", Claim: "team_ids", Values: []string{"2002"}}},
		},
		Previews: []rule{{Provider: "github", Claim: "orgs", Values: []string{"acme"}}},
	}
	for name, tc := range map[string]struct {
		provider, claim, value string
		want                   bool
	}{
		"a label's value":                {"github", "team_ids", "2002", true},
		"a previews rule's value":        {"github", "orgs", "acme", true},
		"a GitHub name in another case":  {"github", "orgs", "ACME", true},
		"an Auth0 value":                 {"auth0", mocks.RolesClaim, "staff", true},
		"an Auth0 value in another case": {"auth0", mocks.RolesClaim, "Staff", false},
		"a value of another claim":       {"github", "teams", "2002", false},
		"a value of another provider":    {"auth0", "team_ids", "2002", false},
		"a value that no rule names":     {"github", "team_ids", "2001", false},
	} {
		if got := rules.names(tc.provider, tc.claim, tc.value); got != tc.want {
			t.Errorf("%s: names(%s, %s, %s) is %v", name, tc.provider, tc.claim, tc.value, got)
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
	rules, err := readAccessFile("demo/access.yaml")
	if err != nil {
		t.Fatal(err)
	}
	for name, tc := range map[string]struct {
		provider        string
		claims          signin.Claims
		partners, staff bool
	}{
		"Ada, staff at Auth0":             {"auth0", signin.Claims{mocks.RolesClaim: []any{"staff"}}, false, true},
		"a partner at Auth0, in a string": {"auth0", signin.Claims{mocks.RolesClaim: "partner"}, true, false},
		"Grace, a partner at GitHub":      {"github", signin.Claims{"org_ids": []any{"1001"}, "team_ids": []any{"2002"}}, true, false},
		"a staff team at GitHub":          {"github", signin.Claims{"team_ids": []any{"2001", "2002"}}, true, true},
		"Auth0's claim through GitHub":    {"github", signin.Claims{mocks.RolesClaim: []any{"staff"}}, false, false},
		"GitHub's claim through Auth0":    {"auth0", signin.Claims{"team_ids": []any{"2001"}}, false, false},
		"a role in another letter case":   {"auth0", signin.Claims{mocks.RolesClaim: []any{"Staff"}}, false, false},
		"a reader without claims":         {"auth0", nil, false, false},
		"a claim of other values":         {"auth0", signin.Claims{mocks.RolesClaim: []any{"visitor"}}, false, false},
		"a provider of no rule":           {"okta", signin.Claims{mocks.RolesClaim: []any{"staff"}}, false, false},
		"a claim that holds no string":    {"auth0", signin.Claims{mocks.RolesClaim: []any{map[string]any{"name": "staff"}}}, false, false},
		"the names of the teams, not IDs": {"github", signin.Claims{"teams": []any{"Acme/staff", "Acme/partners"}}, false, false},
		"a team of another ID":            {"github", signin.Claims{"team_ids": []any{"3001"}}, false, false},
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

func TestTheAccessHookAllowsNothingWithoutASignIn(t *testing.T) {
	rules, err := readAccessFile("demo/access.yaml")
	if err != nil {
		t.Fatal(err)
	}
	plain := httptest.NewRequest("GET", "/", nil)
	withProvider := plain.WithContext(context.WithValue(plain.Context(), providerKey{}, "github"))
	for name, r := range map[string]*http.Request{"a request outside the sign-in": plain, "a provider without an identity": withProvider} {
		a, err := rules.access(r)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if a == nil || a.Portal(pets) || a.Section(pets, api) || a.(portal.PreviewAccess).Preview("pr-1") {
			t.Errorf("%s: the access %#v allows something", name, a)
		}
	}
}

func TestPreviewsOpenToTheReadersOfThePreviewRules(t *testing.T) {
	rules, err := readAccessFile("demo/access.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var grace portal.Access = rules.accessOf("github", signin.Claims{"org_ids": []any{"1001"}, "team_ids": []any{"2002"}})
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
		"another organization":           rules.accessOf("github", signin.Claims{"org_ids": []any{"1002"}}),
		"the organization's name alone":  rules.accessOf("github", signin.Claims{"orgs": []any{"Acme"}}),
		"the organization through Auth0": rules.accessOf("auth0", signin.Claims{"org_ids": []any{"1001"}}),
	} {
		if a.Preview("pr-1") {
			t.Errorf("%s may open the preview pr-1", name)
		}
	}
}
