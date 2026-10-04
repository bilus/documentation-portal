package portal_test

import (
	"bytes"
	"errors"
	"log"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/bilus/documentation-portal/internal/fakeaccess"
	"github.com/bilus/documentation-portal/portal"
)

const (
	beta  = "openapi: 3.0.3\ninfo:\n  title: Beta Pets\n  version: 0.1.0\npaths:\n  /pets/beta:\n    get:\n      operationId: listBetaPets\n"
	vault = "openapi: 3.0.3\ninfo:\n  title: Vault\n  version: 1.0.0\npaths: {}\n"
)

// accessRoot returns a documentation root with three portals: Pet Shop, with
// the specs API and Beta and the docs sections Guides and Internal; Store,
// with the spec API; and Vault, with the spec API. The guide a.md links the
// API, the Beta spec and a document of Internal, and so does the toc file of
// Guides.
func accessRoot() (fstest.MapFS, []portal.Portal) {
	root := fstest.MapFS{
		"pets.yaml":           {Data: []byte(pets)},
		"beta.yaml":           {Data: []byte(beta)},
		"store.yaml":          {Data: []byte(store)},
		"vault.yaml":          {Data: []byte(vault)},
		"guides/a.md":         {Data: []byte("# A\n\n[the API](pets.yaml)\n\n[the beta](beta.yaml)\n\n[a secret](internal/x.md)\n")},
		"internal/x.md":       {Data: []byte("# X\n\nThe secret plans.\n")},
		"internal/x.png":      {Data: []byte("\x89PNG\r\n\x1a\n")},
		"toc.json":            {Data: []byte(`{"items": [{"type": "item", "title": "A", "uri": "guides/a.md"}, {"type": "item", "title": "Beta pets", "uri": "beta.yaml"}, {"type": "item", "title": "Secret notes", "uri": "internal/x.md"}]}`)},
		"guides/.hidden.md":   {Data: []byte("# Hidden\n")},
		"internal/sub/y.md":   {Data: []byte("# Y\n")},
		"internal/.hidden.md": {Data: []byte("# Hidden\n")},
	}
	return root, []portal.Portal{
		{Name: "Pet Shop", Sections: []portal.Section{
			{Title: "API", Type: portal.SpecSection, Input: "pets.yaml"},
			{Title: "Beta", Type: portal.SpecSection, Input: "beta.yaml"},
			{Title: "Guides", Type: portal.DocsSection, Input: "guides", Toc: "toc.json"},
			{Title: "Internal", Type: portal.DocsSection, Input: "internal"},
		}},
		{Name: "Store", Sections: []portal.Section{{Title: "API", Type: portal.SpecSection, Input: "store.yaml"}}},
		{Name: "Vault", Sections: []portal.Section{{Title: "API", Type: portal.SpecSection, Input: "vault.yaml"}}},
	}
}

// withoutHidden returns the portals of accessRoot without the portal and the
// sections that the reader with access "Pet Shop/API, Pet Shop/Guides,
// Store/API" may not see: the portals of a configuration where they are
// missing.
func withoutHidden(portals []portal.Portal) []portal.Portal {
	shop := portal.Portal{Name: "Pet Shop", Sections: []portal.Section{portals[0].Sections[0], portals[0].Sections[2]}}
	return []portal.Portal{shop, portals[1]}
}

// partner is the reader who may see the API and the guides of Pet Shop and
// the API of Store.
const partner = "Pet Shop/API, Pet Shop/Guides, Store/API"

// chatRoutes returns a stand-in for the chat page of each of portals, which
// writes the portal's name.
func chatRoutes(portals []portal.Portal) []portal.Route {
	var routes []portal.Route
	for _, p := range portals {
		name := p.Name
		slug := strings.ToLower(strings.ReplaceAll(name, " ", "-"))
		routes = append(routes, portal.Route{Pattern: "GET /portals/" + slug + "/chat", Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Write([]byte("The chat of " + name + "."))
		})})
	}
	return routes
}

// newAccessPortal returns the portal handler of root with portals, their
// stand-in chat pages and the access hook, or fails the test.
func newAccessPortal(t *testing.T, root fstest.MapFS, portals []portal.Portal, hook func(*http.Request) (portal.Access, error)) http.Handler {
	t.Helper()
	h, err := portal.New(portal.Config{Root: root, Portals: portals, Chat: chatRoutes(portals), Access: hook})
	if err != nil {
		t.Fatal(err)
	}
	return h
}

// getAs returns the response of h to a GET of path from the reader who may
// see the sections in visible, a value of the stub hook's header.
func getAs(h http.Handler, visible, path string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodGet, path, nil)
	r.Header.Set(fakeaccess.Header, visible)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	return rec
}

// navOf returns the navigation bar of page.
func navOf(page string) string {
	return page[max(strings.Index(page, "<nav"), 0):max(strings.Index(page, "</nav>"), 0)]
}

// everyRoute lists a request for each route of accessRoot's portals.
var everyRoute = []string{
	"/",
	"/portals/pet-shop",
	"/portals/pet-shop/",
	"/portals/pet-shop/specs/api",
	"/portals/pet-shop/api/specs/beta",
	"/portals/pet-shop/docs/guides",
	"/portals/pet-shop/docs/guides/",
	"/portals/pet-shop/docs/guides/a.md",
	"/portals/pet-shop/docs/internal/x.md",
	"/portals/pet-shop/raw/internal/x.png",
	"/portals/pet-shop/specs/gone",
	"/portals/store/specs/api",
	"/portals/store/chat",
	"/portals/vault/specs/api",
	"/portals/other/specs/api",
}

func TestAHookThatShowsEverythingChangesNoPage(t *testing.T) {
	root, portals := accessRoot()
	everyone := newAccessPortal(t, root, portals, nil)
	hooked := newAccessPortal(t, root, portals, fakeaccess.Hook)
	all := "Pet Shop/API, Pet Shop/Beta, Pet Shop/Guides, Pet Shop/Internal, Store/API, Vault/API"
	for _, path := range everyRoute {
		got, want := getAs(hooked, all, path), getAs(everyone, "", path)
		if got.Code != want.Code || got.Body.String() != want.Body.String() || got.Header().Get("Location") != want.Header().Get("Location") {
			t.Errorf("%s with a hook that shows everything: %d %q, want %d %q", path, got.Code, got.Body, want.Code, want.Body)
		}
	}
}

func TestWithoutAHookNoResponseIsPrivate(t *testing.T) {
	root, portals := accessRoot()
	h := newAccessPortal(t, root, portals, nil)
	for _, path := range append(everyRoute, "/assets/elements/styles.min.css", "/assets/elements/missing.css") {
		if rec := getAs(h, "", path); rec.Header().Get("Cache-Control") != "" {
			t.Errorf("%s without a hook: Cache-Control %q", path, rec.Header().Get("Cache-Control"))
		}
	}
}

func TestHomeListsTheVisiblePortals(t *testing.T) {
	root, portals := accessRoot()
	h := newAccessPortal(t, root, portals, fakeaccess.Hook)
	if rec := getAs(h, "Store/API", "/"); rec.Code != http.StatusFound || rec.Header().Get("Location") != "/portals/store/specs/api" {
		t.Errorf("/ with one visible portal: %d to %q, want its first section", rec.Code, rec.Header().Get("Location"))
	}
	rec := getAs(h, "Pet Shop/Guides, Store/API", "/")
	home := rec.Body.String()
	shop, store := strings.Index(home, `<a href="/portals/pet-shop/docs/guides/">Pet Shop</a>`), strings.Index(home, `<a href="/portals/store/specs/api">Store</a>`)
	if rec.Code != http.StatusOK || shop < 0 || store < shop || strings.Contains(home, "Vault") {
		t.Errorf("/ with two visible portals: %d %q", rec.Code, home)
	}
	menu := navOf(getAs(h, "Pet Shop/Guides, Store/API", "/portals/store/specs/api").Body.String())
	if !strings.Contains(menu, `<a href="/portals/pet-shop/docs/guides/">Pet Shop</a><a href="/portals/store/specs/api">Store</a></div>`) || strings.Contains(menu, "Vault") {
		t.Errorf("the portal menu lists other than the visible portals: %q", menu)
	}
	rec = getAs(h, "", "/")
	if none := rec.Body.String(); rec.Code != http.StatusOK || !strings.Contains(none, "No portals are open to you.") || strings.Contains(none, "/portals/") {
		t.Errorf("/ with no visible portal: %d %q", rec.Code, none)
	}
}

func TestNavigationBarListsTheVisibleSections(t *testing.T) {
	root, portals := accessRoot()
	h := newAccessPortal(t, root, portals, fakeaccess.Hook)
	reader := "Pet Shop/Beta, Pet Shop/Guides"
	nav := navOf(getAs(h, reader, "/portals/pet-shop/docs/guides/").Body.String())
	if want := `<a href="/portals/pet-shop/specs/beta">Beta</a><a href="/portals/pet-shop/docs/guides/">Guides</a><a href="/portals/pet-shop/chat">Chat</a>`; !strings.Contains(nav, want) || strings.Contains(nav, "portal-menu") {
		t.Errorf("the navigation bar: %q, want %s and no menu", nav, want)
	}
	if rec := getAs(h, reader, "/portals/pet-shop/"); rec.Code != http.StatusFound || rec.Header().Get("Location") != "/portals/pet-shop/specs/beta" {
		t.Errorf("/portals/pet-shop/: %d to %q, want the first visible section", rec.Code, rec.Header().Get("Location"))
	}
	if rec := getAs(h, reader, "/"); rec.Code != http.StatusFound || rec.Header().Get("Location") != "/portals/pet-shop/specs/beta" {
		t.Errorf("/: %d to %q, want the first visible section of the only visible portal", rec.Code, rec.Header().Get("Location"))
	}
}

func TestHiddenRoutesAnswerAsMissing(t *testing.T) {
	root, portals := accessRoot()
	hidden := newAccessPortal(t, root, portals, fakeaccess.Hook)
	missing := newAccessPortal(t, root, withoutHidden(portals), fakeaccess.Hook)
	for _, path := range []string{
		"/portals/vault",
		"/portals/vault/",
		"/portals/vault/specs/api",
		"/portals/vault/api/specs/api",
		"/portals/vault/chat",
		"/portals/pet-shop/specs/beta",
		"/portals/pet-shop/api/specs/beta",
		"/portals/pet-shop/docs/internal",
		"/portals/pet-shop/docs/internal/",
		"/portals/pet-shop/docs/internal/x.md",
		"/portals/pet-shop/raw/internal/x.png",
	} {
		got, want := getAs(hidden, partner, path), getAs(missing, partner, path)
		if got.Code != http.StatusNotFound || got.Code != want.Code || got.Body.String() != want.Body.String() || got.Header().Get("Content-Type") != want.Header().Get("Content-Type") {
			t.Errorf("%s: %d %q, want %d %q, as for a missing one", path, got.Code, got.Body, want.Code, want.Body)
		}
	}
}

func TestLinksToHiddenSectionsLeadNowhere(t *testing.T) {
	root, portals := accessRoot()
	hidden := newAccessPortal(t, root, portals, fakeaccess.Hook)
	missing := newAccessPortal(t, root, withoutHidden(portals), fakeaccess.Hook)
	for _, path := range []string{"/portals/pet-shop/docs/guides/a.md", "/portals/pet-shop/docs/guides/"} {
		got, want := getAs(hidden, partner, path), getAs(missing, partner, path)
		if got.Code != http.StatusOK || got.Body.String() != want.Body.String() {
			t.Errorf("%s: %d %q, want %q, as with the sections missing", path, got.Code, got.Body, want.Body)
		}
	}
	page := getAs(hidden, partner, "/portals/pet-shop/docs/guides/a.md").Body.String()
	hrefs := linkHrefs(page)
	if hrefs["the API"] != "/portals/pet-shop/specs/api" || !strings.Contains(page, `href="/portals/pet-shop/docs/guides/a.md"><div title="A"`) {
		t.Errorf("the links to visible pages: %v", hrefs)
	}
	for _, text := range []string{"the beta", "a secret"} {
		if href, ok := hrefs[text]; ok || !strings.Contains(page, text) {
			t.Errorf("the link %q leads to %q, want its text alone", text, href)
		}
	}
	for _, entry := range []string{"Beta pets", "Secret notes"} {
		if strings.Contains(page, entry) {
			t.Errorf("the sidebar holds the entry %q for a hidden page", entry)
		}
	}
}

func TestHookErrorHidesEverything(t *testing.T) {
	var logged bytes.Buffer
	prev := log.Writer()
	log.SetOutput(&logged)
	t.Cleanup(func() { log.SetOutput(prev) })
	root, portals := accessRoot()
	failing := func(*http.Request) (portal.Access, error) {
		return fakeaccess.Parse(partner), errors.New("the token expired")
	}
	empty := func(*http.Request) (portal.Access, error) { return nil, nil }
	for name, tc := range map[string]struct {
		hook func(*http.Request) (portal.Access, error)
		logs string
	}{
		"an error":  {failing, "access hook: the token expired"},
		"no access": {empty, "access hook: no access"},
	} {
		logged.Reset()
		h := newAccessPortal(t, root, portals, tc.hook)
		if rec := getAs(h, partner, "/"); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "No portals are open to you.") {
			t.Errorf("%s: /: %d %q", name, rec.Code, rec.Body)
		}
		for _, path := range []string{"/portals/store/specs/api", "/portals/pet-shop/docs/guides/a.md", "/portals/store/chat"} {
			if rec := getAs(h, partner, path); rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), "Portal not found") || rec.Header().Get("Cache-Control") != "private" {
				t.Errorf("%s: %s: %d with Cache-Control %q: %q", name, path, rec.Code, rec.Header().Get("Cache-Control"), rec.Body)
			}
		}
		if lines := strings.Count(logged.String(), tc.logs); lines != 4 {
			t.Errorf("%s: 4 requests logged %d lines with %q:\n%s", name, lines, tc.logs, logged.String())
		}
	}
}

func TestResponsesArePrivateWithAHook(t *testing.T) {
	root, portals := accessRoot()
	h := newAccessPortal(t, root, portals, fakeaccess.Hook)
	for _, path := range append(everyRoute, "/assets/elements/styles.min.css", "/assets/elements/missing.css", "/assets/elements/") {
		if rec := getAs(h, partner, path); rec.Header().Get("Cache-Control") != "private" {
			t.Errorf("%s: %d with Cache-Control %q, want private", path, rec.Code, rec.Header().Get("Cache-Control"))
		}
	}
}

// asked records each portal and section that an access was asked about, and
// allows the sections labelled public.
type asked struct {
	portals  []portal.Portal
	sections []portal.Section
}

func (a *asked) Portal(p portal.Portal) bool {
	a.portals = append(a.portals, p)
	return true
}

func (a *asked) Section(_ portal.Portal, s portal.Section) bool {
	a.sections = append(a.sections, s)
	return slices.Contains(s.Labels, "public")
}

func TestHookGetsTheConfiguredPortalsAndSections(t *testing.T) {
	root := fstest.MapFS{"pets.yaml": {Data: []byte(pets)}, "guides/a.md": {Data: []byte("# A\n")}}
	shop := portal.Portal{Name: "Pets", Labels: []string{"partner-acme"}, Sections: []portal.Section{
		{Title: "API", Type: portal.SpecSection, Input: "./pets.yaml"},
		{Title: "Guides", Type: portal.DocsSection, Input: "./guides/", Labels: []string{"public", "beta"}},
	}}
	access := &asked{}
	h, err := portal.New(portal.Config{Root: root, Portals: []portal.Portal{shop}, Access: func(*http.Request) (portal.Access, error) { return access, nil }})
	if err != nil {
		t.Fatal(err)
	}
	if rec := getAs(h, "", "/portals/pets/"); rec.Header().Get("Location") != "/portals/pets/docs/guides/" {
		t.Errorf("/portals/pets/: %d to %q, want the section labelled public", rec.Code, rec.Header().Get("Location"))
	}
	if len(access.portals) == 0 || !reflect.DeepEqual(access.portals[0], shop) {
		t.Errorf("the hook's access was asked about %+v, want the portal as configured: %+v", access.portals, shop)
	}
	if len(access.sections) < 2 || !reflect.DeepEqual(access.sections[:2], shop.Sections) {
		t.Errorf("the hook's access was asked about %+v, want the sections as configured: %+v", access.sections, shop.Sections)
	}
}

func TestAccessOf(t *testing.T) {
	root, portals := accessRoot()
	var got portal.Access
	record := portal.Route{Pattern: "GET /record", Handler: http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { got = portal.AccessOf(r) })}
	for name, tc := range map[string]struct {
		hook  func(*http.Request) (portal.Access, error)
		shows []string // the portals that the reader's access allows
	}{
		"no hook":     {nil, []string{"Pet Shop", "Store", "Vault"}},
		"a stub hook": {fakeaccess.Hook, []string{"Pet Shop", "Store"}},
		"an error":    {func(*http.Request) (portal.Access, error) { return portal.Everything, errors.New("down") }, nil},
	} {
		h, err := portal.New(portal.Config{Root: root, Portals: portals, Chat: []portal.Route{record}, Access: tc.hook})
		if err != nil {
			t.Fatal(err)
		}
		got = nil
		getAs(h, partner, "/record")
		var shows []string
		for _, p := range portals {
			if got != nil && got.Portal(p) {
				shows = append(shows, p.Name)
			}
		}
		if !reflect.DeepEqual(shows, tc.shows) {
			t.Errorf("%s: AccessOf allows the portals %v, want %v", name, shows, tc.shows)
		}
	}
	outside := portal.AccessOf(httptest.NewRequest(http.MethodGet, "/", nil))
	for _, p := range portals {
		if outside == nil || outside.Portal(p) || outside.Section(p, p.Sections[0]) {
			t.Errorf("a request served by no portal handler: %v allows %s, want an access that allows nothing", outside, p.Name)
		}
	}
}

func TestLibraryForLimitsTheSections(t *testing.T) {
	root, portals := accessRoot()
	libs, err := portal.NewLibraries(portal.Config{Root: root, Portals: portals})
	if err != nil {
		t.Fatal(err)
	}
	shop, ok := libs[0].For(fakeaccess.Parse("Pet Shop/Beta, Pet Shop/Guides"))
	if !ok {
		t.Fatal("the Pet Shop library is hidden from a reader of two of its sections")
	}
	if got := shop.Sections(); !reflect.DeepEqual(got, []portal.SectionLink{{Title: "Beta", URL: "/portals/pet-shop/specs/beta"}, {Title: "Guides", URL: "/portals/pet-shop/docs/guides/"}}) {
		t.Errorf("sections: %+v", got)
	}
	if shop.URL() != "/portals/pet-shop/specs/beta" || !reflect.DeepEqual(shop.Titles(), []string{"Beta Pets"}) || shop.Title() != "Beta Pets" {
		t.Errorf("URL %q, titles %v, title %q, want the Beta spec's", shop.URL(), shop.Titles(), shop.Title())
	}
	ops, err := shop.Operations()
	if err != nil || len(ops) != 1 || ops[0].OperationID != "listBetaPets" {
		t.Errorf("operations: %+v, %v", ops, err)
	}
	docs, err := shop.Documents()
	if err != nil || len(docs) != 1 || docs[0].Path != "guides/a.md" {
		t.Errorf("documents: %+v, %v", docs, err)
	}
	if text, err := shop.ReadDocument("internal/x.md"); err == nil {
		t.Errorf("a document of a hidden section: %q", text)
	}
	if text, err := shop.ReadDocument("guides/a.md"); err != nil || strings.Contains(text, "(/portals/pet-shop/specs/api)") || !strings.Contains(text, "(/portals/pet-shop/specs/beta)") {
		t.Errorf("a.md as the reader's page shows it: %q, %v", text, err)
	}
	if part, err := shop.SpecPart("api", "info"); err == nil {
		t.Errorf("a part of a hidden spec: %q", part)
	}
	if matches, err := shop.Search("plans", 10); err != nil || len(matches) != 0 {
		t.Errorf("a search for what only a hidden section holds: %+v, %v", matches, err)
	}
	for name, access := range map[string]portal.Access{"a hidden portal": fakeaccess.Parse("Store/API"), "a portal without a visible section": fakeaccess.Parse("Pet Shop/Other"), "no access": nil} {
		if lib, ok := libs[0].For(access); ok || lib != nil {
			t.Errorf("%s: %v, %v, want no library", name, lib, ok)
		}
	}
	if all, ok := libs[0].For(portal.Everything); !ok || len(all.Sections()) != 4 {
		t.Errorf("Everything: %v, %v, want every section", all, ok)
	}
	if len(libs[0].Sections()) != 4 {
		t.Errorf("For changed the library it limits: %+v", libs[0].Sections())
	}
}

func TestTocProblemsDoNotDependOnTheReader(t *testing.T) {
	var logged bytes.Buffer
	prev := log.Writer()
	log.SetOutput(&logged)
	t.Cleanup(func() { log.SetOutput(prev) })
	root := fstest.MapFS{
		"overview/start.md": {Data: []byte("# Start\n")},
		"internal/x.md":     {Data: []byte("# X\n")},
		"notes/n.md":        {Data: []byte("# N\n")},
		"toc.json":          {Data: []byte(`{"items": [{"type": "item", "title": "Secret X", "uri": "internal/x.md"}]}`)},
		"broken.json":       {Data: []byte(`{"items": [{"type": "item", "title": "Gone", "uri": "notes/gone.md"}]}`)},
	}
	portals := []portal.Portal{
		{Name: "Shop", Sections: []portal.Section{
			{Title: "Overview", Type: portal.DocsSection, Input: "overview", Toc: "toc.json"},
			{Title: "Internal", Type: portal.DocsSection, Input: "internal"},
		}},
		{Name: "Lab", Sections: []portal.Section{{Title: "Notes", Type: portal.DocsSection, Input: "notes", Toc: "broken.json"}}},
	}
	h := newAccessPortal(t, root, portals, fakeaccess.Hook)
	restricted, everything := "Shop/Overview", "Shop/Overview, Shop/Internal"
	for _, reader := range []string{restricted, everything, restricted, everything} {
		page := getAs(h, reader, "/portals/shop/docs/overview/start.md").Body.String()
		if reader == restricted && (strings.Contains(page, "Secret X") || !strings.Contains(page, `href="/portals/shop/docs/overview/start.md"`)) {
			t.Errorf("the sidebar of a reader who may not see the toc's pages: %q", page)
		}
		if reader == everything && !strings.Contains(page, "Secret X") {
			t.Errorf("the sidebar of a reader of every section lacks the toc's entry: %q", page)
		}
	}
	if strings.Contains(logged.String(), "toc.json") {
		t.Errorf("the log blames a toc file for one reader's access:\n%s", logged.String())
	}
	for range 2 {
		getAs(h, "Lab/Notes", "/portals/lab/docs/notes/n.md")
	}
	if n := strings.Count(logged.String(), "toc file broken.json: names no page"); n != 1 {
		t.Errorf("a toc file that names no page of the whole portal logged %d lines, want 1:\n%s", n, logged.String())
	}
}

func TestNewRefusesAChatPageWithoutAHandler(t *testing.T) {
	root, portals := accessRoot()
	cfg := portal.Config{Root: root, Portals: portals, Chat: []portal.Route{{Pattern: "GET /portals/store/chat"}}, Access: fakeaccess.Hook}
	if _, err := portal.New(cfg); err == nil {
		t.Error("the chat page of a portal without a handler: no error")
	}
}

func TestLibraryPortal(t *testing.T) {
	root, portals := accessRoot()
	libs, err := portal.NewLibraries(portal.Config{Root: root, Portals: portals})
	if err != nil {
		t.Fatal(err)
	}
	shop, ok := libs[0].For(fakeaccess.Parse("Pet Shop/Guides"))
	if !ok {
		t.Fatal("the Pet Shop library is hidden from a reader of its guides")
	}
	for name, lib := range map[string]*portal.Library{"the library": libs[0], "the library for a reader": shop} {
		if got := lib.Portal(); !reflect.DeepEqual(got, portals[0]) {
			t.Errorf("%s's portal: %+v, want the portal as configured: %+v", name, got, portals[0])
		}
	}
}

func TestGuardChecksEveryRouteOfAPortal(t *testing.T) {
	root, portals := accessRoot()
	page := func(name string) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("The page of " + name + ".")) })
	}
	for _, tc := range []struct{ pattern, method, path string }{
		{"GET  /portals/vault/chat", http.MethodGet, "/portals/vault/chat"},
		{"GET\t/portals/vault/chat", http.MethodGet, "/portals/vault/chat"},
		{"HEAD /portals/vault/chat", http.MethodHead, "/portals/vault/chat"},
		{"GET /portals/vault/chat/{rest...}", http.MethodGet, "/portals/vault/chat/x"},
		{"POST /portals/vault/ask", http.MethodPost, "/portals/vault/ask"},
		{"GET /portals/{p}/extra", http.MethodGet, "/portals/vault/extra"},
	} {
		h, err := portal.New(portal.Config{Root: root, Portals: portals, Chat: []portal.Route{{Pattern: tc.pattern, Handler: page("Vault")}}, Access: fakeaccess.Hook})
		if err != nil {
			t.Fatalf("%q: %v", tc.pattern, err)
		}
		r := httptest.NewRequest(tc.method, tc.path, nil)
		r.Header.Set(fakeaccess.Header, partner)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		if rec.Code != http.StatusNotFound || strings.Contains(rec.Body.String(), "The page of Vault.") {
			t.Errorf("%q: %s %s answers %d %q, want the 404 of a missing portal", tc.pattern, tc.method, tc.path, rec.Code, rec.Body)
		}
	}
	h, err := portal.New(portal.Config{Root: root, Portals: portals, Chat: []portal.Route{{Pattern: "GET  /portals/store/chat", Handler: page("Store")}}, Access: fakeaccess.Hook})
	if err != nil {
		t.Fatal(err)
	}
	if rec := getAs(h, partner, "/portals/store/chat"); rec.Code != http.StatusOK || rec.Body.String() != "The page of Store." {
		t.Errorf("a visible portal's page: %d %q", rec.Code, rec.Body)
	}
}

// wantPrivate serves routes as the Chat of a portal handler with the stub
// access hook, through a real server, and fails the test for each route whose
// response is not 200 with Cache-Control: private.
func wantPrivate(t *testing.T, routes map[string]http.HandlerFunc) {
	t.Helper()
	var chat []portal.Route
	for path, h := range routes {
		chat = append(chat, portal.Route{Pattern: "GET " + path, Handler: h})
	}
	root, portals := accessRoot()
	h, err := portal.New(portal.Config{Root: root, Portals: portals, Chat: chat, Access: fakeaccess.Hook})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(h)
	defer srv.Close()
	for path := range routes {
		resp, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK || resp.Header.Get("Cache-Control") != "private" {
			t.Errorf("%s: %d with Cache-Control %q, want 200 with private", path, resp.StatusCode, resp.Header.Get("Cache-Control"))
		}
	}
}

func TestASilentResponseIsPrivate(t *testing.T) {
	wantPrivate(t, map[string]http.HandlerFunc{
		"/silent":  func(http.ResponseWriter, *http.Request) {},
		"/public":  func(w http.ResponseWriter, _ *http.Request) { w.Header().Set("Cache-Control", "public, max-age=60") },
		"/deleted": func(w http.ResponseWriter, _ *http.Request) { w.Header().Del("Cache-Control") },
	})
}

func TestAFlushedResponseIsPrivate(t *testing.T) {
	flush := func(w http.ResponseWriter) {
		if err := http.NewResponseController(w).Flush(); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}
	wantPrivate(t, map[string]http.HandlerFunc{
		"/controller": func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Del("Cache-Control")
			flush(w)
		},
		"/flusher": func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Cache-Control", "no-cache")
			f, ok := w.(http.Flusher)
			if !ok {
				http.Error(w, "no http.Flusher", http.StatusInternalServerError)
				return
			}
			f.Flush()
		},
		"/hints": func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusEarlyHints)
			w.Header().Del("Cache-Control")
			flush(w)
		},
		"/hints-and-text": func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusEarlyHints)
			w.Header().Del("Cache-Control")
			w.Write([]byte("tea"))
		},
	})
}

func TestATocProblemComesBackAfterARestrictedReader(t *testing.T) {
	var logged bytes.Buffer
	prev := log.Writer()
	log.SetOutput(&logged)
	t.Cleanup(func() { log.SetOutput(prev) })
	root := fstest.MapFS{
		"overview/start.md": {Data: []byte("# Start\n")},
		"internal/x.md":     {Data: []byte("# X\n")},
	}
	portals := []portal.Portal{{Name: "Shop", Sections: []portal.Section{
		{Title: "Overview", Type: portal.DocsSection, Input: "overview", Toc: "toc.json"},
		{Title: "Internal", Type: portal.DocsSection, Input: "internal"},
	}}}
	h := newAccessPortal(t, root, portals, fakeaccess.Hook)
	page := func(reader string) { getAs(h, reader, "/portals/shop/docs/overview/start.md") }
	page("Shop/Overview, Shop/Internal") // the toc file is missing: one line
	root["toc.json"] = &fstest.MapFile{Data: []byte(`{"items": [{"type": "item", "title": "X", "uri": "internal/x.md"}]}`)}
	page("Shop/Overview") // the whole site has no problem now, though this reader sees no page of the toc
	delete(root, "toc.json")
	page("Shop/Overview") // missing again: a second line
	if n := strings.Count(logged.String(), "toc file toc.json"); n != 2 {
		t.Errorf("a toc file missing, fixed and missing again logged %d lines, want 2:\n%s", n, logged.String())
	}
}
