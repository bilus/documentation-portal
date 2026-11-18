package portal_test

import (
	"net/http"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/bilus/documentation-portal/internal/fakeaccess"
	"github.com/bilus/documentation-portal/portal"
)

// linkRoot is a documentation root with a spec and a guide, for the portals
// with link sections.
var linkRoot = fstest.MapFS{"pets.yaml": {Data: []byte(pets)}, "store.yaml": {Data: []byte(store)}, "guides/a.md": {Data: []byte("# A\n")}}

var (
	apiSection     = portal.Section{Title: "API", Type: portal.SpecSection, Input: "pets.yaml"}
	guidesSection  = portal.Section{Title: "Guides", Type: portal.DocsSection, Input: "guides"}
	supportSection = portal.Section{Title: "Support", Type: portal.LinkSection, URL: "/support"}
)

func TestLinkSections(t *testing.T) {
	sections := []portal.Section{
		apiSection,
		{Title: "Support", Type: portal.LinkSection, URL: "/support?from=docs#contact"},
		guidesSection,
		{Title: "Status", Type: portal.LinkSection, URL: "HTTPS://status.example.com/now"},
	}
	for _, base := range []string{"", "/docs"} {
		h, err := portal.New(portal.Config{Root: linkRoot, Portals: petsPortal(sections), BasePath: base})
		if err != nil {
			t.Fatal(err)
		}
		// The links keep their URLs under a base path, which the portal's own pages carry.
		want := `<a href="` + base + `/portals/pets/specs/api">API</a><a href="/support?from=docs#contact">Support</a><a href="` + base + `/portals/pets/docs/guides/">Guides</a><a href="https://status.example.com/now">Status</a>`
		for _, path := range []string{"/portals/pets/specs/api", "/portals/pets/docs/guides/", "/portals/pets/docs/guides/a.md", "/portals/pets/docs/guides/gone.md"} {
			if nav := navOf(get(h, base+path).Body.String()); !strings.Contains(nav, want) {
				t.Errorf("%s: the navigation bar is %q, want %s", base+path, nav, want)
			}
		}
	}
}

func TestLinkSectionsHaveNoPages(t *testing.T) {
	h := newSections(t, linkRoot, apiSection, guidesSection, supportSection)
	for _, path := range []string{
		"/portals/pets/specs/support",
		"/portals/pets/api/specs/support",
		"/portals/pets/docs/support",
		"/portals/pets/docs/support/",
		"/portals/pets/docs/support/a.md",
		"/portals/pets/raw/support/a.md",
		"/portals/pets/support",
	} {
		if rec := get(h, path); rec.Code != http.StatusNotFound {
			t.Errorf("%s: %d, want 404", path, rec.Code)
		}
	}
}

func TestAPortalOpensAtAFirstLinkSection(t *testing.T) {
	welcome := portal.Section{Title: "Welcome", Type: portal.LinkSection, URL: "/welcome"}
	shop := portal.Portal{Name: "Pet Shop", Sections: []portal.Section{welcome, apiSection}}
	storePortal := portal.Portal{Name: "Store", Sections: []portal.Section{{Title: "API", Type: portal.SpecSection, Input: "store.yaml"}}}

	h := newPortals(t, linkRoot, shop, storePortal)
	if home := get(h, "/").Body.String(); !strings.Contains(home, `<a href="/welcome">Pet Shop</a>`) {
		t.Errorf("the home page does not open Pet Shop at its link section: %q", home)
	}
	if nav := navOf(get(h, "/portals/store/specs/api").Body.String()); !strings.Contains(nav, `<a href="/welcome">Pet Shop</a>`) {
		t.Errorf("the portal menu does not open Pet Shop at its link section: %q", nav)
	}
	for _, path := range []string{"/portals/pet-shop", "/portals/pet-shop/"} {
		if rec := get(h, path); rec.Code != http.StatusFound || rec.Header().Get("Location") != "/welcome" {
			t.Errorf("%s: %d to %q, want the link section's URL", path, rec.Code, rec.Header().Get("Location"))
		}
	}

	alone, err := portal.New(portal.Config{Root: linkRoot, Portals: []portal.Portal{shop}, BasePath: "/docs"})
	if err != nil {
		t.Fatal(err)
	}
	if rec := get(alone, "/docs/"); rec.Code != http.StatusFound || rec.Header().Get("Location") != "/welcome" {
		t.Errorf("/docs/ with one portal: %d to %q, want /welcome without the base path", rec.Code, rec.Header().Get("Location"))
	}
}

func TestHiddenLinkSectionsLeaveTheBar(t *testing.T) {
	partners := portal.Section{Title: "Support", Type: portal.LinkSection, URL: "/support", Labels: []string{"partners"}}
	portals := []portal.Portal{{Name: "Pet Shop", Sections: []portal.Section{apiSection, partners}}}
	h := newAccessPortal(t, linkRoot, portals, fakeaccess.Hook)

	link := `<a href="/support">Support</a>`
	if nav := navOf(getAs(h, "Pet Shop/API", "/portals/pet-shop/specs/api").Body.String()); strings.Contains(nav, "Support") {
		t.Errorf("a reader without the link section sees it: %q", nav)
	}
	if nav := navOf(getAs(h, "Pet Shop/API, Pet Shop/Support", "/portals/pet-shop/specs/api").Body.String()); !strings.Contains(nav, link) {
		t.Errorf("a reader with the link section: %q, want %s", nav, link)
	}
	// A portal with a link section alone is still a portal to open.
	if rec := getAs(h, "Pet Shop/Support", "/"); rec.Code != http.StatusFound || rec.Header().Get("Location") != "/support" {
		t.Errorf("/ for a reader with the link section alone: %d to %q", rec.Code, rec.Header().Get("Location"))
	}

	access := &asked{}
	h, err := portal.New(portal.Config{Root: linkRoot, Portals: portals, Access: func(*http.Request) (portal.Access, error) { return access, nil }})
	if err != nil {
		t.Fatal(err)
	}
	get(h, "/")
	if len(access.sections) < 2 || !reflect.DeepEqual(access.sections[1], partners) {
		t.Errorf("the hook's access was asked about %+v, want the link section as configured: %+v", access.sections, partners)
	}
}

func TestLibraryLinksLinkSections(t *testing.T) {
	lib, err := firstLibrary(portal.NewLibraries(portal.Config{Root: linkRoot, Portals: petsPortal([]portal.Section{supportSection, apiSection, guidesSection})}))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := lib.Sections(), []portal.SectionLink{{Title: "Support", URL: "/support"}, {Title: "API", URL: "/portals/pets/specs/api"}, {Title: "Guides", URL: "/portals/pets/docs/guides/"}}; !reflect.DeepEqual(got, want) {
		t.Errorf("sections: %+v, want %+v", got, want)
	}
	if lib.URL() != "/support" {
		t.Errorf("the portal opens at %q, want its first section's URL", lib.URL())
	}
	if titles := lib.Titles(); !reflect.DeepEqual(titles, []string{"Pets"}) {
		t.Errorf("titles: %q", titles)
	}
	docs, err := lib.Documents()
	if err != nil || len(docs) != 1 || docs[0].Path != "guides/a.md" {
		t.Errorf("documents: %+v, %v, want the guide alone", docs, err)
	}
}

func TestLinkSectionChecks(t *testing.T) {
	with := func(s portal.Section, change func(*portal.Section)) portal.Section {
		change(&s)
		return s
	}
	for name, tc := range map[string]struct {
		sections []portal.Section
		want     string
	}{
		"no url":            {[]portal.Section{with(supportSection, func(s *portal.Section) { s.URL = "" })}, "needs a url"},
		"an input":          {[]portal.Section{with(supportSection, func(s *portal.Section) { s.Input = "./" })}, `input "./"`},
		"a toc":             {[]portal.Section{with(supportSection, func(s *portal.Section) { s.Toc = "toc.json" })}, "toc.json"},
		"a spec's url":      {[]portal.Section{with(apiSection, func(s *portal.Section) { s.URL = "/api" })}, `url "/api"`},
		"a docs url":        {[]portal.Section{with(guidesSection, func(s *portal.Section) { s.URL = "/guides" })}, `url "/guides"`},
		"no title":          {[]portal.Section{with(supportSection, func(s *portal.Section) { s.Title = "" })}, "title"},
		"an empty slug":     {[]portal.Section{with(supportSection, func(s *portal.Section) { s.Title = "--" })}, `"--"`},
		"one slug twice":    {[]portal.Section{supportSection, with(apiSection, func(s *portal.Section) { s.Title = "Support!" })}, `"support"`},
		"a relative url":    {[]portal.Section{with(supportSection, func(s *portal.Section) { s.URL = "support" })}, `url "support"`},
		"another host":      {[]portal.Section{with(supportSection, func(s *portal.Section) { s.URL = "//example.com/support" })}, `url "//example.com/support"`},
		"a script":          {[]portal.Section{with(supportSection, func(s *portal.Section) { s.URL = "javascript:alert(1)" })}, "javascript"},
		"an unknown scheme": {[]portal.Section{apiSection, with(supportSection, func(s *portal.Section) { s.URL = "mailto:help@example.com" })}, "mailto"},
		"no host":           {[]portal.Section{with(supportSection, func(s *portal.Section) { s.URL = "https:///support" })}, `url "https:///support"`},
		"a bad escape":      {[]portal.Section{with(supportSection, func(s *portal.Section) { s.URL = "/a%zz" })}, "%zz"},
	} {
		_, err := portal.New(portal.Config{Root: linkRoot, Portals: petsPortal(tc.sections)})
		if err == nil || !strings.Contains(err.Error(), tc.want) || !strings.Contains(err.Error(), `portal "Pets"`) {
			t.Errorf("%s: err = %v, want one naming the portal and %s", name, err, tc.want)
		}
	}
	newSections(t, linkRoot, supportSection)
}

func TestReadConfigReadsLinkSections(t *testing.T) {
	root := fstest.MapFS{"pets.yaml": {Data: []byte(pets)}, "environment.yaml": {Data: []byte(`
portals:
  - name: Pets
    sections:
      - {title: API, type: spec, input: pets.yaml}
      - {title: Support, type: link, url: /support, labels: [partners]}
`)}}
	cfg, err := portal.ReadConfig(root, "environment.yaml")
	if err != nil {
		t.Fatal(err)
	}
	want := petsPortal([]portal.Section{apiSection, {Title: "Support", Type: portal.LinkSection, URL: "/support", Labels: []string{"partners"}}})
	if !reflect.DeepEqual(cfg.Portals, want) {
		t.Errorf("portals = %+v, want %+v", cfg.Portals, want)
	}
	h, err := portal.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if nav := navOf(get(h, "/portals/pets/specs/api").Body.String()); !strings.Contains(nav, `<a href="/support">Support</a>`) {
		t.Errorf("the navigation bar lacks the link section of the configuration file: %q", nav)
	}
}
