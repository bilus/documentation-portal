package portal_test

import (
	"net/http"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/bilus/documentation-portal/portal"
)

// linked returns a portal configuration with the portals Pets, labelled
// apis, the link portal Status, labelled apis and ops, which links
// https://status.example.com/, Store, labelled apis, and the link portal
// Support, without labels, which links /support.
func linked() portal.Config {
	api := []portal.Section{{Title: "API", Type: portal.SpecSection, Input: "pets.yaml"}}
	return portal.Config{
		Root: fstest.MapFS{"pets.yaml": {Data: []byte(pets)}},
		Portals: []portal.Portal{
			{Name: "Pets", Labels: []string{"apis"}, Sections: api},
			{Name: "Status", Type: portal.LinkPortal, URL: "https://status.example.com/", Labels: []string{"apis", "ops"}},
			{Name: "Store", Labels: []string{"apis"}, Sections: api},
			{Name: "Support", Type: portal.LinkPortal, URL: "/support"},
		},
	}
}

const (
	statusLink  = `<a href="https://status.example.com/">Status</a>`
	supportLink = `<a href="/support">Support</a>`
)

func TestNewWithALinkPortal(t *testing.T) {
	h, err := portal.New(linked())
	if err != nil {
		t.Fatal(err)
	}
	if rec := get(h, "/"); rec.Code != http.StatusOK || !inOrder(rec.Body.String(), link("pets", "Pets"), link("store", "Store")) {
		t.Errorf("/: %d %q", rec.Code, rec.Body)
	}
	if rec := get(h, "/portals/pets/specs/api"); rec.Code != http.StatusOK {
		t.Errorf("a portal's page: %d", rec.Code)
	}
}

func TestReadConfigReadsLinkPortals(t *testing.T) {
	const file = `
portals:
  - name: Foobar
    type: link
    url: https://foobar
    labels: [xyz]
  - name: Pets
    sections:
      - {title: API, type: spec, input: api.yaml}
`
	cfg, err := portal.ReadConfig(fstest.MapFS{"environment.yaml": {Data: []byte(file)}}, "environment.yaml")
	if err != nil {
		t.Fatal(err)
	}
	want := []portal.Portal{
		{Name: "Foobar", Type: portal.LinkPortal, URL: "https://foobar", Labels: []string{"xyz"}},
		{Name: "Pets", Sections: []portal.Section{{Title: "API", Type: portal.SpecSection, Input: "api.yaml"}}},
	}
	if !reflect.DeepEqual(cfg.Portals, want) {
		t.Errorf("the portals: %+v, want %+v", cfg.Portals, want)
	}
}

func TestLinkPortalChecks(t *testing.T) {
	api := []portal.Section{{Title: "API", Type: portal.SpecSection, Input: "pets.yaml"}}
	for name, tc := range map[string]struct {
		p    portal.Portal
		want string
	}{
		"no url":             {portal.Portal{Name: "Status", Type: portal.LinkPortal}, "a link portal needs a url"},
		"sections":           {portal.Portal{Name: "Status", Type: portal.LinkPortal, URL: "/status", Sections: api}, "a link portal takes a url and no sections"},
		"a relative url":     {portal.Portal{Name: "Status", Type: portal.LinkPortal, URL: "status"}, `url "status"`},
		"another host":       {portal.Portal{Name: "Status", Type: portal.LinkPortal, URL: "//example.com/status"}, `url "//example.com/status"`},
		"a script":           {portal.Portal{Name: "Status", Type: portal.LinkPortal, URL: "javascript:alert(1)"}, `url "javascript:alert(1)"`},
		"a url with no type": {portal.Portal{Name: "Status", URL: "/status", Sections: api}, `url "/status" needs the type "link"`},
		"another type":       {portal.Portal{Name: "Status", Type: "docs", Sections: api}, `the type "docs" is not "link"`},
		"an empty slug":      {portal.Portal{Name: "--", Type: portal.LinkPortal, URL: "/status"}, `"--"`},
	} {
		cfg := linked()
		cfg.Portals = []portal.Portal{cfg.Portals[0], tc.p}
		if _, err := portal.New(cfg); err == nil || !strings.Contains(err.Error(), tc.want) || !strings.Contains(err.Error(), tc.p.Name) {
			t.Errorf("%s: err = %v, want %s", name, err, tc.want)
		}
	}
	cfg := linked()
	cfg.Portals = []portal.Portal{cfg.Portals[0], {Name: "PETS", Type: portal.LinkPortal, URL: "/pets"}}
	if _, err := portal.New(cfg); err == nil || !strings.Contains(err.Error(), `"pets"`) {
		t.Errorf("a link portal with another portal's slug: err = %v", err)
	}
	cfg.Portals = []portal.Portal{{Name: "Status", Type: portal.LinkPortal, URL: "/status"}}
	if _, err := portal.New(cfg); err != nil {
		t.Errorf("link portals alone: %v", err)
	}
}

func TestLinkPortalsAreListed(t *testing.T) {
	cfg := linked()
	cfg.BasePath = "/docs"
	h, err := portal.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	pets, store := `<a href="/docs/portals/pets/specs/api">Pets</a>`, `<a href="/docs/portals/store/specs/api">Store</a>`
	if home := get(h, "/docs/").Body.String(); !inOrder(home, pets, statusLink, store, supportLink) {
		t.Errorf("the home page: %q", home)
	}
	page := get(h, "/docs/portals/store/specs/api").Body.String()
	nav := page[max(strings.Index(page, "<nav"), 0):max(strings.Index(page, "</nav>"), 0)]
	if !inOrder(nav, "<summary>Store</summary>", pets, statusLink, store, supportLink) {
		t.Errorf("the portal menu: %q", nav)
	}
}

func TestLinkPortalsKeepTheirPlaceInACollection(t *testing.T) {
	quiet(t)
	cfg := linked()
	cfg.Collections = []portal.Collection{{Name: "Operations", MatchLabels: []string{"ops"}}, {Name: "APIs", MatchLabels: []string{"apis"}}}
	h, err := portal.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	home := get(h, "/").Body.String()
	if !inOrder(home, "<h2>Operations</h2>", statusLink, "<h2>APIs</h2>", link("pets", "Pets"), statusLink, link("store", "Store")) || strings.Contains(home, supportLink) {
		t.Errorf("the home page: %q", home)
	}
}

func TestALinkPortalHasNoPages(t *testing.T) {
	h, err := portal.New(linked())
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/portals/status", "/portals/status/", "/portals/status/specs/api", "/portals/support/docs/guides/"} {
		if rec := get(h, path); rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), "No portal has the slug") {
			t.Errorf("%s: %d %q", path, rec.Code, rec.Body)
		}
	}
}

// askedAbout allows every portal without the label ops, and records the portals
// it was askedAbout about.
type askedAbout struct{ portals []portal.Portal }

func (a *askedAbout) Portal(p portal.Portal) bool {
	a.portals = append(a.portals, p)
	return !strings.Contains(strings.Join(p.Labels, " "), "ops")
}
func (a *askedAbout) Section(portal.Portal, portal.Section) bool { return true }

func TestLinkPortalsFollowAccess(t *testing.T) {
	cfg := linked()
	access := &askedAbout{}
	cfg.Access = func(*http.Request) (portal.Access, error) { return access, nil }
	h, err := portal.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	home := get(h, "/").Body.String()
	if !inOrder(home, link("pets", "Pets"), link("store", "Store"), supportLink) || strings.Contains(home, "Status") {
		t.Errorf("the home page without Status: %q", home)
	}
	if len(access.portals) < 2 || !reflect.DeepEqual(access.portals[1], cfg.Portals[1]) {
		t.Errorf("the hook was askedAbout about %+v, want the link portal as configured", access.portals)
	}
}

func TestOneListedLinkPortalShowsTheHomePage(t *testing.T) {
	cfg := linked()
	cfg.Portals = cfg.Portals[1:2]
	for _, url := range []string{"https://status.example.com/", "/"} {
		cfg.Portals[0].URL = url
		h, err := portal.New(cfg)
		if err != nil {
			t.Fatal(err)
		}
		if rec := get(h, "/"); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `<a href="`+url+`">Status</a>`) {
			t.Errorf("/ with the link portal %s alone: %d to %q: %q", url, rec.Code, rec.Header().Get("Location"), rec.Body)
		}
	}
}

func TestALinkPortalsURLIsWrittenAsChecked(t *testing.T) {
	cfg := linked()
	cfg.Portals[1].URL = "HTTPS://status.example.com/"
	h, err := portal.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if home := get(h, "/").Body.String(); !strings.Contains(home, statusLink) {
		t.Errorf("the home page links the URL as configured, not as net/url writes it: %q", home)
	}
}

func TestLinkURLsHoldNoCredentials(t *testing.T) {
	cfg := linked()
	cfg.Portals[1].URL = "https://ops:hunter2@status.example.com/"
	_, err := portal.New(cfg)
	if err == nil || !strings.Contains(err.Error(), `url "https://status.example.com/" holds a user name or a password`) || strings.Contains(err.Error(), "hunter2") {
		t.Errorf("a link portal's URL with credentials: err = %v", err)
	}
	cfg = linked()
	cfg.Portals[0].Sections = append(cfg.Portals[0].Sections, portal.Section{Title: "Reports", Type: portal.LinkSection, URL: "https://reports@reports.example.com/x"})
	if _, err = portal.New(cfg); err == nil || !strings.Contains(err.Error(), `url "https://reports.example.com/x" holds a user name or a password`) {
		t.Errorf("a link section's URL with a user name: err = %v", err)
	}
}

func TestTheLinkTypeIsExact(t *testing.T) {
	cfg := linked()
	cfg.Portals[1].Type = "Link"
	if _, err := portal.New(cfg); err == nil || !strings.Contains(err.Error(), `the type "Link" is not "link"`) {
		t.Errorf("the type Link: err = %v", err)
	}
}

func TestALinkPortalOfNoCollectionIsWarnedAbout(t *testing.T) {
	logged := quiet(t)
	cfg := linked()
	cfg.Collections = []portal.Collection{{Name: "APIs", MatchLabels: []string{"apis"}}}
	if _, err := portal.New(cfg); err != nil {
		t.Fatal(err)
	}
	if want := `portal "Support" is in no collection and is not listed`; strings.Count(logged.String(), want) != 1 || strings.Count(logged.String(), "\n") != 1 {
		t.Errorf("logged %q, want one line with %s", logged.String(), want)
	}
}

func TestReadConfigKeepsALinkPortalsSections(t *testing.T) {
	const file = `
portals:
  - name: Status
    type: link
    url: /status
    sections:
      - {title: API, type: spec, input: pets.yaml}
`
	cfg, err := portal.ReadConfig(fstest.MapFS{"environment.yaml": {Data: []byte(file)}, "pets.yaml": {Data: []byte(pets)}}, "environment.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := portal.New(cfg); err == nil || !strings.Contains(err.Error(), "a link portal takes a url and no sections") {
		t.Errorf("a link portal with sections in the file: err = %v", err)
	}
}

func TestLibrariesLeaveOutLinkPortals(t *testing.T) {
	libs, err := portal.NewLibraries(linked())
	if err != nil {
		t.Fatal(err)
	}
	if len(libs) != 2 || libs[0].Name() != "Pets" || libs[1].Name() != "Store" {
		t.Errorf("%d libraries, want those of Pets and Store", len(libs))
	}
}

func TestLibraryMenu(t *testing.T) {
	quiet(t)
	cfg := linked()
	libs, err := portal.NewLibraries(cfg)
	if err != nil {
		t.Fatal(err)
	}
	all := []portal.PortalLink{{Name: "Pets", URL: "/portals/pets/specs/api"}, {Name: "Status", URL: "https://status.example.com/", Link: true}, {Name: "Store", URL: "/portals/store/specs/api"}, {Name: "Support", URL: "/support", Link: true}}
	if menu, collections := libs[1].Menu(portal.Everything); !reflect.DeepEqual(menu, all) || collections != nil {
		t.Errorf("the menu: %+v %+v", menu, collections)
	}
	if menu, _ := libs[0].Menu(named{"Pets"}); menu != nil {
		t.Errorf("the menu with one visible portal: %+v", menu)
	}
	if menu, _ := libs[0].Menu(nil); menu != nil {
		t.Errorf("the menu without access: %+v", menu)
	}
	cfg.Collections = []portal.Collection{{Name: "Operations", MatchLabels: []string{"ops"}}, {Name: "APIs", MatchLabels: []string{"apis"}}}
	if libs, err = portal.NewLibraries(cfg); err != nil {
		t.Fatal(err)
	}
	menu, collections := libs[0].Menu(portal.Everything)
	want := []portal.MenuCollection{{Name: "Operations", Portals: all[1:2]}, {Name: "APIs", Portals: all[:3]}}
	if !reflect.DeepEqual(menu, all[:3]) || !reflect.DeepEqual(collections, want) {
		t.Errorf("the menu with collections: %+v %+v", menu, collections)
	}
}
