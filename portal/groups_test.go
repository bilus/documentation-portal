package portal_test

import (
	"bytes"
	"log"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/bilus/documentation-portal/portal"
)

// stages returns a portal configuration with the portals Staging, labelled
// prerelease and autostage, Production, labelled autostage, Ingest, labelled
// live-ingest and autostage, and Lab, without labels, each with the spec
// section API, and the groups AutoStage API, of autostage, Ingest APIs, of
// live-ingest, and Prerelease, of autostage and prerelease. Ingest is in two
// groups, Staging in two, and Lab in none.
func stages() portal.Config {
	api := []portal.Section{{Title: "API", Type: portal.SpecSection, Input: "pets.yaml"}}
	return portal.Config{
		Root: fstest.MapFS{"pets.yaml": {Data: []byte(pets)}},
		Portals: []portal.Portal{
			{Name: "Staging", Labels: []string{"prerelease", "autostage"}, Sections: api},
			{Name: "Production", Labels: []string{"autostage"}, Sections: api},
			{Name: "Ingest", Labels: []string{"live-ingest", "autostage"}, Sections: api},
			{Name: "Lab", Sections: api},
		},
		Groups: []portal.Group{
			{Name: "AutoStage API", MatchLabels: []string{"autostage"}},
			{Name: "Ingest APIs", MatchLabels: []string{"live-ingest"}},
			{Name: "Prerelease", MatchLabels: []string{"autostage", "prerelease"}},
		},
	}
}

// link returns the home page's and the portal menu's link to the portal with
// slug and name.
func link(slug, name string) string {
	return `<a href="/portals/` + slug + `/specs/api">` + name + `</a>`
}

// inOrder reports whether each of parts follows the one before it in s.
func inOrder(s string, parts ...string) bool {
	for _, part := range parts {
		i := strings.Index(s, part)
		if i < 0 {
			return false
		}
		s = s[i+len(part):]
	}
	return true
}

// named allows the portals with the listed names and every section of them.
type named []string

func (n named) Portal(p portal.Portal) bool                { return slices.Contains(n, p.Name) }
func (n named) Section(portal.Portal, portal.Section) bool { return true }

// quiet keeps the log of the test out of its output, and returns it.
func quiet(t *testing.T) *bytes.Buffer {
	t.Helper()
	var logged bytes.Buffer
	prev := log.Writer()
	log.SetOutput(&logged)
	t.Cleanup(func() { log.SetOutput(prev) })
	return &logged
}

func TestNewWithGroups(t *testing.T) {
	quiet(t)
	h, err := portal.New(stages())
	if err != nil {
		t.Fatal(err)
	}
	rec := get(h, "/")
	if home := rec.Body.String(); rec.Code != http.StatusOK || !inOrder(home, link("staging", "Staging"), link("production", "Production")) {
		t.Errorf("/: %d %q", rec.Code, home)
	}
	if rec := get(h, "/portals/production/specs/api"); rec.Code != http.StatusOK {
		t.Errorf("a portal's page: %d", rec.Code)
	}
}

func TestReadConfigReadsGroups(t *testing.T) {
	const file = `
portals:
  - name: Staging
    labels: [prerelease, autostage]
    sections:
      - {title: API, type: spec, input: api.yaml}
groups:
  - name: AutoStage API
    matchLabels: [autostage]
  - name: Ingest
    matchLabels: [live-ingest, beta]
`
	cfg, err := portal.ReadConfig(fstest.MapFS{"environment.yaml": {Data: []byte(file)}}, "environment.yaml")
	if err != nil {
		t.Fatal(err)
	}
	want := []portal.Group{{Name: "AutoStage API", MatchLabels: []string{"autostage"}}, {Name: "Ingest", MatchLabels: []string{"live-ingest", "beta"}}}
	if !reflect.DeepEqual(cfg.Groups, want) {
		t.Errorf("the groups: %+v, want %+v", cfg.Groups, want)
	}
	for data, want := range map[string]string{
		"groups:\n  - name: A\n    matchLabels: [a]\n  -\n": "group 2 is empty",
		"groups:\n  - name: A\n    matchLabels: a\n":        "cannot unmarshal",
		"groups:\n  - name: A\n    labels: [a]\n":           "labels",
		"groups:\n  - name: A\n    matchLabels: [{a: b}]\n": "cannot unmarshal",
		"groups: {name: A}\n":                               "cannot unmarshal",
	} {
		_, err := portal.ReadConfig(fstest.MapFS{"environment.yaml": {Data: []byte(data)}}, "environment.yaml")
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: err = %v, want one naming %s", data, err, want)
		}
	}
	if cfg, err := portal.ReadConfig(fstest.MapFS{"environment.yaml": {Data: []byte("portals: []\n")}}, "environment.yaml"); err != nil || cfg.Groups != nil {
		t.Errorf("a file without groups: %+v, %v", cfg.Groups, err)
	}
}

func TestGroupChecks(t *testing.T) {
	quiet(t)
	for name, tc := range map[string]struct {
		groups []portal.Group
		want   string
	}{
		"no name":         {[]portal.Group{{Name: "A", MatchLabels: []string{"autostage"}}, {MatchLabels: []string{"autostage"}}}, "group 2 has no name"},
		"no match labels": {[]portal.Group{{Name: "Everything"}}, `group "Everything" has no match labels`},
		"an empty label":  {[]portal.Group{{Name: "Blank", MatchLabels: []string{"autostage", ""}}}, `group "Blank" has an empty match label`},
		"one name twice":  {[]portal.Group{{Name: "A", MatchLabels: []string{"autostage"}}, {Name: "A", MatchLabels: []string{"live-ingest"}}}, `two groups have the name "A"`},
	} {
		cfg := stages()
		cfg.Groups = tc.groups
		if _, err := portal.New(cfg); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: New: err = %v, want %s", name, err, tc.want)
		}
		if _, err := portal.NewLibraries(cfg); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: NewLibraries: err = %v, want %s", name, err, tc.want)
		}
	}
}

func TestAPortalNeedsEveryMatchLabel(t *testing.T) {
	quiet(t)
	api := []portal.Section{{Title: "API", Type: portal.SpecSection, Input: "pets.yaml"}}
	cfg := portal.Config{Root: fstest.MapFS{"pets.yaml": {Data: []byte(pets)}}, Groups: []portal.Group{{Name: "AB", MatchLabels: []string{"a", "b"}}}}
	for _, labels := range [][]string{{"a", "b"}, {"b", "a", "c"}, {"a"}, {"b"}, {"x"}, nil} {
		cfg.Portals = append(cfg.Portals, portal.Portal{Name: "P" + strings.Join(labels, ""), Labels: labels, Sections: api})
	}
	libs, err := portal.NewLibraries(cfg)
	if err != nil {
		t.Fatal(err)
	}
	logged := quiet(t)
	if _, err := portal.New(cfg); err != nil {
		t.Fatal(err)
	}
	for i, in := range []bool{true, true, false, false, false, false} {
		warned := strings.Contains(logged.String(), `portal "`+libs[i].Name()+`" is in no group`)
		if warned == in {
			t.Errorf("%s: in the group: %v, want %v", libs[i].Name(), !warned, in)
		}
	}
}

func TestGroupWarnings(t *testing.T) {
	logged := quiet(t)
	cfg := stages()
	cfg.Groups = append(cfg.Groups, portal.Group{Name: "Partners", MatchLabels: []string{"partner"}})
	if _, err := portal.New(cfg); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`portal "Lab" is in no group and is not listed`, `group "Partners" matches no portal`} {
		if n := strings.Count(logged.String(), want); n != 1 {
			t.Errorf("%d lines with %q, want 1:\n%s", n, want, logged.String())
		}
	}
	if n := strings.Count(logged.String(), "\n"); n != 2 {
		t.Errorf("%d lines, want 2:\n%s", n, logged.String())
	}
	logged.Reset()
	if _, err := portal.NewLibraries(cfg); err != nil || logged.Len() != 0 {
		t.Errorf("NewLibraries: %v, logged %q, want nothing", err, logged.String())
	}
	cfg.Groups = nil
	if _, err := portal.New(cfg); err != nil || logged.Len() != 0 {
		t.Errorf("without groups: %v, logged %q, want nothing", err, logged.String())
	}
}

func TestHomePageListsGroups(t *testing.T) {
	quiet(t)
	h, err := portal.New(stages())
	if err != nil {
		t.Fatal(err)
	}
	rec := get(h, "/")
	home := rec.Body.String()
	if rec.Code != http.StatusOK || !inOrder(home,
		"<h2>AutoStage API</h2>", link("staging", "Staging"), link("production", "Production"), link("ingest", "Ingest"),
		"<h2>Ingest APIs</h2>", link("ingest", "Ingest"),
		"<h2>Prerelease</h2>", link("staging", "Staging"),
	) {
		t.Errorf("/: %d %q", rec.Code, home)
	}
	for l, want := range map[string]int{link("staging", "Staging"): 2, link("production", "Production"): 1, link("ingest", "Ingest"): 2, link("lab", "Lab"): 0} {
		if n := strings.Count(home, l); n != want {
			t.Errorf("%d of %s, want %d", n, l, want)
		}
	}
}

func TestPortalMenuListsGroups(t *testing.T) {
	quiet(t)
	h, err := portal.New(stages())
	if err != nil {
		t.Fatal(err)
	}
	for path, name := range map[string]string{"/portals/production/specs/api": "Production", "/portals/lab/specs/api": "Lab"} {
		rec := get(h, path)
		page := rec.Body.String()
		nav := page[max(strings.Index(page, "<nav"), 0):max(strings.Index(page, "</nav>"), 0)]
		if rec.Code != http.StatusOK || !inOrder(nav, "<summary>"+name+"</summary>",
			`<span class="portal-menu-group">AutoStage API</span>`, link("staging", "Staging"), link("production", "Production"), link("ingest", "Ingest"),
			`<span class="portal-menu-group">Ingest APIs</span>`, link("ingest", "Ingest"),
			`<span class="portal-menu-group">Prerelease</span>`, link("staging", "Staging"),
		) || strings.Contains(nav, link("lab", "Lab")) {
			t.Errorf("%s: %d, the navigation bar: %q", path, rec.Code, nav)
		}
	}
}

func TestOneListedPortalOpensAtHome(t *testing.T) {
	quiet(t)
	cfg := stages()
	cfg.Portals = []portal.Portal{cfg.Portals[1], cfg.Portals[3]} // Production and Lab
	cfg.Groups = cfg.Groups[:1]
	h, err := portal.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if rec := get(h, "/"); rec.Code != http.StatusFound || rec.Header().Get("Location") != "/portals/production/specs/api" {
		t.Errorf("/ with one listed portal: %d to %q, want its first section", rec.Code, rec.Header().Get("Location"))
	}
	for _, path := range []string{"/portals/production/specs/api", "/portals/lab/specs/api"} {
		if rec := get(h, path); rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), "portal-menu") {
			t.Errorf("%s: %d, with a menu of one listed portal", path, rec.Code)
		}
	}
	cfg.Groups = []portal.Group{{Name: "Partners", MatchLabels: []string{"partner"}}}
	if h, err = portal.New(cfg); err != nil {
		t.Fatal(err)
	}
	if rec := get(h, "/"); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "No portals are open to you.") || strings.Contains(rec.Body.String(), "/portals/") {
		t.Errorf("/ with no listed portal: %d %q", rec.Code, rec.Body)
	}
}

func TestGroupsFollowAccess(t *testing.T) {
	quiet(t)
	cfg := stages()
	access := named{"Staging", "Production", "Lab"}
	cfg.Access = func(*http.Request) (portal.Access, error) { return access, nil }
	h, err := portal.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	home := get(h, "/").Body.String()
	if !inOrder(home, "<h2>AutoStage API</h2>", link("staging", "Staging"), link("production", "Production"), "<h2>Prerelease</h2>", link("staging", "Staging")) || strings.Contains(home, "Ingest") {
		t.Errorf("/ without Ingest: %q", home)
	}
	access = named{"Production", "Ingest", "Lab"}
	home = get(h, "/").Body.String()
	if !inOrder(home, "<h2>AutoStage API</h2>", link("production", "Production"), link("ingest", "Ingest"), "<h2>Ingest APIs</h2>", link("ingest", "Ingest")) || strings.Contains(home, "Prerelease") {
		t.Errorf("/ without Staging: %q", home)
	}
	access = named{"Production", "Lab"}
	if rec := get(h, "/"); rec.Code != http.StatusFound || rec.Header().Get("Location") != "/portals/production/specs/api" {
		t.Errorf("/ with one visible listed portal: %d to %q", rec.Code, rec.Header().Get("Location"))
	}
}

func TestACustomHomePageListsTheGroups(t *testing.T) {
	quiet(t)
	templates := fstest.MapFS{"home.html": {Data: []byte(`{{range .Groups}}<h3>{{.Name}}</h3>{{range .Links}}<a class="card" href="{{.URL}}">{{.Label}}</a>{{end}}{{end}}`)}}
	h, err := portal.New(stages(), portal.WithCustomTemplates(templates))
	if err != nil {
		t.Fatal(err)
	}
	if home := get(h, "/").Body.String(); !inOrder(home, "<h3>AutoStage API</h3>", `<a class="card" href="/portals/staging/specs/api">Staging</a>`, "<h3>Ingest APIs</h3>") {
		t.Errorf("the custom home page: %q", home)
	}
	cfg := stages()
	cfg.Groups = nil
	for name, file := range map[string]string{
		"home.html": `{{range .Groups}}{{.Nme}}{{end}}`,
		"nav.html":  `{{define "nav"}}{{range .Nav.Groups}}{{range .Links}}{{.Lable}}{{end}}{{end}}{{end}}`,
	} {
		if _, err := portal.New(cfg, portal.WithCustomTemplates(fstest.MapFS{name: {Data: []byte(file)}})); err == nil {
			t.Errorf("%s with a field the groups lack, and no error", name)
		}
	}
}

func TestLibraryGroups(t *testing.T) {
	t.Skip("HOLE(4): give each library the groups of its portal")
	libs, err := portal.NewLibraries(stages())
	if err != nil {
		t.Fatal(err)
	}
	every := []string{"AutoStage API", "Ingest APIs", "Prerelease"}
	for i, want := range [][]string{{"AutoStage API", "Prerelease"}, {"AutoStage API"}, {"AutoStage API", "Ingest APIs"}, nil} {
		if got := libs[i].Groups(); !slices.Equal(got, want) {
			t.Errorf("%s: the groups %v, want %v", libs[i].Name(), got, want)
		}
		if got := libs[i].GroupNames(); !slices.Equal(got, every) {
			t.Errorf("%s: every group %v, want %v", libs[i].Name(), got, every)
		}
		if visible, ok := libs[i].For(portal.Everything); !ok || !slices.Equal(visible.Groups(), want) {
			t.Errorf("%s: a reader's library lost the groups", libs[i].Name())
		}
	}
	cfg := stages()
	cfg.Groups = nil
	if libs, err = portal.NewLibraries(cfg); err != nil || libs[0].Groups() != nil || libs[0].GroupNames() != nil {
		t.Errorf("without groups: %v", err)
	}
}
