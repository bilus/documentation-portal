package portal_test

import (
	"net/http"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/bilus/documentation-portal/portal"
)

// newPortals returns the portal handler of root with portals, or fails the
// test.
func newPortals(t *testing.T, root fstest.MapFS, portals ...portal.Portal) http.Handler {
	t.Helper()
	h, err := portal.New(portal.Config{Root: root, Portals: portals})
	if err != nil {
		t.Fatal(err)
	}
	return h
}

// shopAndStore returns a documentation root with two portals: Pet Shop, with
// the spec pets.yaml and the guides of guides/, and Store, with the spec
// store.yaml.
func shopAndStore() (fstest.MapFS, []portal.Portal) {
	root := fstest.MapFS{
		"pets.yaml":       {Data: []byte(pets)},
		"store.yaml":      {Data: []byte(store)},
		"guides/a.md":     {Data: []byte("# A\n\n![flow](flow.png)\n\n[the API](pets.yaml)\n\n[the store](store.yaml)\n")},
		"guides/flow.png": {Data: []byte("\x89PNG\r\n\x1a\n")},
	}
	return root, []portal.Portal{
		{Name: "Pet Shop", Sections: []portal.Section{
			{Title: "API", Type: portal.SpecSection, Input: "pets.yaml"},
			{Title: "Guides", Type: portal.DocsSection, Input: "guides"},
		}},
		{Name: "Store", Sections: []portal.Section{{Title: "API", Type: portal.SpecSection, Input: "store.yaml"}}},
	}
}

func TestPortalURLs(t *testing.T) {
	root, portals := shopAndStore()
	h := newPortals(t, root, portals...)
	for path, want := range map[string]int{
		"/portals/pet-shop/specs/api":           http.StatusOK,
		"/portals/pet-shop/api/specs/api":       http.StatusOK,
		"/portals/pet-shop/docs/guides/":        http.StatusOK,
		"/portals/pet-shop/docs/guides/a.md":    http.StatusOK,
		"/portals/pet-shop/raw/guides/flow.png": http.StatusOK,
		"/portals/store/specs/api":              http.StatusOK,
		"/portals/store/api/specs/api":          http.StatusOK,
		"/specs/api":                            http.StatusNotFound,
		"/api/specs/api":                        http.StatusNotFound,
		"/docs/guides/a.md":                     http.StatusNotFound,
		"/raw/guides/flow.png":                  http.StatusNotFound,
		"/portals/store/docs/guides/":           http.StatusNotFound,
		"/portals/other/specs/api":              http.StatusNotFound,
	} {
		if rec := get(h, path); rec.Code != want {
			t.Errorf("%s: %d, want %d", path, rec.Code, want)
		}
	}
	if raw := get(h, "/portals/store/api/specs/api").Body.String(); raw != store {
		t.Errorf("the Store portal's raw spec: %q", raw)
	}
	if rec := get(h, "/portals/store/"); rec.Code != http.StatusFound || rec.Header().Get("Location") != "/portals/store/specs/api" {
		t.Errorf("/portals/store/: %d to %q, want the portal's first section", rec.Code, rec.Header().Get("Location"))
	}
	if viewer := get(h, "/portals/store/specs/api").Body.String(); !strings.Contains(viewer, `apiDescriptionUrl="/portals/store/api/specs/api"`) {
		t.Errorf("the Store portal's viewer page: %q", viewer)
	}
	page := get(h, "/portals/pet-shop/docs/guides/a.md").Body.String()
	for _, want := range []string{
		`src="/portals/pet-shop/raw/guides/flow.png"`,
		`<a href="/portals/pet-shop/specs/api">API</a>`,
		`<a href="/portals/pet-shop/docs/guides/">Guides</a>`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("the document page lacks %s", want)
		}
	}
	hrefs := linkHrefs(page)
	if hrefs["the API"] != "/portals/pet-shop/specs/api" {
		t.Errorf("a link to the portal's own spec: href %q", hrefs["the API"])
	}
	if href, ok := hrefs["the store"]; ok {
		t.Errorf("a link to another portal's spec leads to %q, want nowhere", href)
	}
}

func TestPortalChecks(t *testing.T) {
	root := fstest.MapFS{"api.yaml": {Data: []byte(pets)}}
	api := []portal.Section{{Title: "API", Type: portal.SpecSection, Input: "api.yaml"}}
	for name, tc := range map[string]struct {
		portals []portal.Portal
		want    string
	}{
		"no portals":       {nil, "no portals"},
		"no name":          {[]portal.Portal{{Sections: api}}, "name"},
		"an empty slug":    {[]portal.Portal{{Name: "--", Sections: api}}, `"--"`},
		"one slug twice":   {[]portal.Portal{{Name: "Pets", Sections: api}, {Name: "pets!", Sections: api}}, `"pets"`},
		"a later section":  {[]portal.Portal{{Name: "Pets", Sections: api}, {Name: "Store", Sections: []portal.Section{{Title: "API", Type: portal.SpecSection, Input: "../store.yaml"}}}}, "../store.yaml"},
		"no sections":      {[]portal.Portal{{Name: "Pets", Sections: api}, {Name: "Store"}}, "no sections"},
		"a portal's error": {[]portal.Portal{{Name: "Pets", Sections: api}, {Name: "Store"}}, `"Store"`},
	} {
		if _, err := portal.New(portal.Config{Root: root, Portals: tc.portals}); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want one naming %s", name, err, tc.want)
		}
	}
	newPortals(t, root, portal.Portal{Name: "Pets", Sections: api}, portal.Portal{Name: "Store", Sections: api})
}

func TestHomePage(t *testing.T) {
	root, portals := shopAndStore()
	if rec := get(newPortals(t, root, portals[1]), "/"); rec.Code != http.StatusFound || rec.Header().Get("Location") != "/portals/store/specs/api" {
		t.Errorf("/ with one portal: %d to %q, want its first section", rec.Code, rec.Header().Get("Location"))
	}
	h := newPortals(t, root, portals...)
	rec := get(h, "/")
	home := rec.Body.String()
	shop, store := strings.Index(home, `<a href="/portals/pet-shop/specs/api">Pet Shop</a>`), strings.Index(home, `<a href="/portals/store/specs/api">Store</a>`)
	if rec.Code != http.StatusOK || !strings.Contains(home, "<title>Portals</title>") || shop < 0 || store < shop {
		t.Errorf("/ with two portals: %d %q", rec.Code, home)
	}
	if rec := get(h, "/portals/other/specs/api"); rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), "other") || !strings.Contains(rec.Body.String(), `href="/"`) {
		t.Errorf("a slug of no portal: %d %q", rec.Code, rec.Body)
	}
}

func TestPortalMenu(t *testing.T) {
	root, portals := shopAndStore()
	h := newPortals(t, root, portals...)
	for _, path := range []string{"/portals/store/specs/api", "/portals/pet-shop/docs/guides/", "/portals/pet-shop/docs/guides/a.md"} {
		page := get(h, path).Body.String()
		nav := page[max(strings.Index(page, "<nav"), 0):max(strings.Index(page, "</nav>"), 0)]
		menu := strings.Index(nav, `class="portal-menu"`)
		shop, store := strings.Index(nav, `<a href="/portals/pet-shop/specs/api">Pet Shop</a>`), strings.Index(nav, `<a href="/portals/store/specs/api">Store</a>`)
		if menu < strings.Index(nav, `>API</a>`) || shop < menu || store < shop {
			t.Errorf("%s: the navigation bar does not end with the portal menu: %q", path, nav)
		}
	}
	if store := get(h, "/portals/store/specs/api").Body.String(); !strings.Contains(store, "<summary>Store</summary>") {
		t.Errorf("the menu is not labelled with the page's portal: %q", store)
	}
	if one := get(newPortals(t, root, portals[1]), "/portals/store/specs/api").Body.String(); strings.Contains(one, "portal-menu") {
		t.Errorf("a menu with one portal: %q", one)
	}
}

func TestPortalPathRedirects(t *testing.T) {
	root, portals := shopAndStore()
	h := newPortals(t, root, portals...)
	if rec := get(h, "/portals/store"); rec.Code != http.StatusFound || rec.Header().Get("Location") != "/portals/store/specs/api" {
		t.Errorf("/portals/store: %d to %q, want the portal's first section", rec.Code, rec.Header().Get("Location"))
	}
	if rec := get(h, "/portals/other"); rec.Code != http.StatusNotFound {
		t.Errorf("/portals/other: %d to %q, want 404", rec.Code, rec.Header().Get("Location"))
	}
}

func TestLibrariesOfPortals(t *testing.T) {
	root, portals := shopAndStore()
	libs, err := portal.NewLibraries(portal.Config{Root: root, Portals: portals})
	if err != nil {
		t.Fatal(err)
	}
	if len(libs) != 2 {
		t.Fatalf("%d libraries, want 2", len(libs))
	}
	for i, want := range []struct{ name, slug, url, chat, title string }{
		{"Pet Shop", "pet-shop", "/portals/pet-shop/specs/api", "/portals/pet-shop/chat", "Pets"},
		{"Store", "store", "/portals/store/specs/api", "/portals/store/chat", "Store"},
	} {
		l := libs[i]
		if l.Name() != want.name || l.Slug() != want.slug || l.URL() != want.url || l.ChatURL() != want.chat || l.Title() != want.title {
			t.Errorf("library %d: %q %q %q %q %q, want %+v", i, l.Name(), l.Slug(), l.URL(), l.ChatURL(), l.Title(), want)
		}
	}
	if docs, err := libs[1].Documents(); err != nil || len(docs) != 0 {
		t.Errorf("the Store portal's documents: %+v, %v, want none", docs, err)
	}
}
