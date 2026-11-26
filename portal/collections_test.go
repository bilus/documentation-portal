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
// section API, and the collections AutoStage API, of autostage, Ingest APIs, of
// live-ingest, and Prerelease, of autostage and prerelease. Ingest is in two
// collections, Staging in two, and Lab in none.
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
		Collections: []portal.Collection{
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

func TestNewWithCollections(t *testing.T) {
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

func TestReadConfigReadsCollections(t *testing.T) {
	const file = `
portals:
  - name: Staging
    labels: [prerelease, autostage]
    sections:
      - {title: API, type: spec, input: api.yaml}
collections:
  - name: AutoStage API
    matchLabels: [autostage]
  - name: Ingest
    matchLabels: [live-ingest, beta]
`
	cfg, err := portal.ReadConfig(fstest.MapFS{"environment.yaml": {Data: []byte(file)}}, "environment.yaml")
	if err != nil {
		t.Fatal(err)
	}
	want := []portal.Collection{{Name: "AutoStage API", MatchLabels: []string{"autostage"}}, {Name: "Ingest", MatchLabels: []string{"live-ingest", "beta"}}}
	if !reflect.DeepEqual(cfg.Collections, want) {
		t.Errorf("the collections: %+v, want %+v", cfg.Collections, want)
	}
	for data, want := range map[string]string{
		"collections:\n  - name: A\n    matchLabels: [a]\n  -\n": "collection 2 is empty",
		"collections:\n  - name: A\n    matchLabels: a\n":        "cannot unmarshal",
		"collections:\n  - name: A\n    labels: [a]\n":           "labels",
		"collections:\n  - name: A\n    matchLabels: [{a: b}]\n": "cannot unmarshal",
		"collections: {name: A}\n":                               "cannot unmarshal",
	} {
		_, err := portal.ReadConfig(fstest.MapFS{"environment.yaml": {Data: []byte(data)}}, "environment.yaml")
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: err = %v, want one naming %s", data, err, want)
		}
	}
	if cfg, err := portal.ReadConfig(fstest.MapFS{"environment.yaml": {Data: []byte("portals: []\n")}}, "environment.yaml"); err != nil || cfg.Collections != nil {
		t.Errorf("a file without collections: %+v, %v", cfg.Collections, err)
	}
}

func TestCollectionChecks(t *testing.T) {
	quiet(t)
	for name, tc := range map[string]struct {
		collections []portal.Collection
		want        string
	}{
		"no name":         {[]portal.Collection{{Name: "A", MatchLabels: []string{"autostage"}}, {MatchLabels: []string{"autostage"}}}, "collection 2 has no name"},
		"no match labels": {[]portal.Collection{{Name: "Everything"}}, `collection "Everything" has no match labels`},
		"an empty label":  {[]portal.Collection{{Name: "Blank", MatchLabels: []string{"autostage", ""}}}, `collection "Blank" has an empty match label`},
		"one name twice":  {[]portal.Collection{{Name: "A", MatchLabels: []string{"autostage"}}, {Name: "A", MatchLabels: []string{"live-ingest"}}}, `two collections have the name "A"`},
	} {
		cfg := stages()
		cfg.Collections = tc.collections
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
	cfg := portal.Config{Root: fstest.MapFS{"pets.yaml": {Data: []byte(pets)}}, Collections: []portal.Collection{{Name: "AB", MatchLabels: []string{"a", "b"}}}}
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
		warned := strings.Contains(logged.String(), `portal "`+libs[i].Name()+`" is in no collection`)
		if warned == in {
			t.Errorf("%s: in the collection: %v, want %v", libs[i].Name(), !warned, in)
		}
	}
}

func TestCollectionWarnings(t *testing.T) {
	logged := quiet(t)
	cfg := stages()
	cfg.Collections = append(cfg.Collections, portal.Collection{Name: "Partners", MatchLabels: []string{"partner"}})
	if _, err := portal.New(cfg); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`portal "Lab" is in no collection and is not listed`, `collection "Partners" matches no portal`} {
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
	cfg.Collections = nil
	if _, err := portal.New(cfg); err != nil || logged.Len() != 0 {
		t.Errorf("without collections: %v, logged %q, want nothing", err, logged.String())
	}
}

func TestHomePageListsCollections(t *testing.T) {
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

func TestPortalMenuListsCollections(t *testing.T) {
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
			`<span class="portal-menu-collection">AutoStage API</span>`, link("staging", "Staging"), link("production", "Production"), link("ingest", "Ingest"),
			`<span class="portal-menu-collection">Ingest APIs</span>`, link("ingest", "Ingest"),
			`<span class="portal-menu-collection">Prerelease</span>`, link("staging", "Staging"),
		) || strings.Contains(nav, link("lab", "Lab")) {
			t.Errorf("%s: %d, the navigation bar: %q", path, rec.Code, nav)
		}
	}
}

func TestOneListedPortalOpensAtHome(t *testing.T) {
	quiet(t)
	cfg := stages()
	cfg.Portals = []portal.Portal{cfg.Portals[1], cfg.Portals[3]} // Production and Lab
	cfg.Collections = cfg.Collections[:1]
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
	cfg.Collections = []portal.Collection{{Name: "Partners", MatchLabels: []string{"partner"}}}
	if h, err = portal.New(cfg); err != nil {
		t.Fatal(err)
	}
	if rec := get(h, "/"); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "No portals are open to you.") || strings.Contains(rec.Body.String(), "/portals/") {
		t.Errorf("/ with no listed portal: %d %q", rec.Code, rec.Body)
	}
}

func TestCollectionsFollowAccess(t *testing.T) {
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

func TestACustomHomePageListsTheCollections(t *testing.T) {
	quiet(t)
	templates := fstest.MapFS{"home.html": {Data: []byte(`{{range .Collections}}<h3>{{.Name}}</h3>{{range .Links}}<a class="card" href="{{.URL}}">{{.Label}}</a>{{end}}{{end}}`)}}
	h, err := portal.New(stages(), portal.WithCustomTemplates(templates))
	if err != nil {
		t.Fatal(err)
	}
	if home := get(h, "/").Body.String(); !inOrder(home, "<h3>AutoStage API</h3>", `<a class="card" href="/portals/staging/specs/api">Staging</a>`, "<h3>Ingest APIs</h3>") {
		t.Errorf("the custom home page: %q", home)
	}
	cfg := stages()
	cfg.Collections = nil
	for name, file := range map[string]string{
		"home.html": `{{range .Collections}}{{.Nme}}{{end}}`,
		"nav.html":  `{{define "nav"}}{{range .Nav.Collections}}{{range .Links}}{{.Lable}}{{end}}{{end}}{{end}}`,
	} {
		if _, err := portal.New(cfg, portal.WithCustomTemplates(fstest.MapFS{name: {Data: []byte(file)}})); err == nil {
			t.Errorf("%s with a field the collections lack, and no error", name)
		}
	}
}

func TestLibraryCollections(t *testing.T) {
	t.Skip("HOLE(5): give each library the collections of its portal")
	libs, err := portal.NewLibraries(stages())
	if err != nil {
		t.Fatal(err)
	}
	every := []string{"AutoStage API", "Ingest APIs", "Prerelease"}
	for i, want := range [][]string{{"AutoStage API", "Prerelease"}, {"AutoStage API"}, {"AutoStage API", "Ingest APIs"}, nil} {
		if got := libs[i].Collections(); !slices.Equal(got, want) {
			t.Errorf("%s: the collections %v, want %v", libs[i].Name(), got, want)
		}
		if got := libs[i].CollectionNames(); !slices.Equal(got, every) {
			t.Errorf("%s: every collection %v, want %v", libs[i].Name(), got, every)
		}
		if visible, ok := libs[i].For(portal.Everything); !ok || !slices.Equal(visible.Collections(), want) {
			t.Errorf("%s: a reader's library lost the collections", libs[i].Name())
		}
	}
	cfg := stages()
	cfg.Collections = nil
	if libs, err = portal.NewLibraries(cfg); err != nil || libs[0].Collections() != nil || libs[0].CollectionNames() != nil {
		t.Errorf("without collections: %v", err)
	}
}
