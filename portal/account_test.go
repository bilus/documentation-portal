package portal_test

import (
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/bilus/documentation-portal/internal/fakeaccess"
	"github.com/bilus/documentation-portal/portal"
)

// signedIn is an account hook: a reader named in the request's Reader header
// gets the name and a sign-out link, and any other reader a sign-in link.
func signedIn(r *http.Request) []portal.AccountLink {
	if name := r.Header.Get("Reader"); name != "" {
		return []portal.AccountLink{{Label: name}, {Label: "Sign out", URL: "/auth/sign-out"}}
	}
	return []portal.AccountLink{{Label: "Sign in", URL: "/auth/sign-in"}}
}

// newAccountPortal returns the portal handler of root with portals, their
// stand-in chat pages and the hooks access and account, either of which may
// be nil, or fails the test.
func newAccountPortal(t *testing.T, root fstest.MapFS, portals []portal.Portal, access func(*http.Request) (portal.Access, error), account func(*http.Request) []portal.AccountLink) http.Handler {
	t.Helper()
	h, err := portal.New(portal.Config{Root: root, Portals: portals, Chat: chatRoutes(portals), Access: access, Account: account})
	if err != nil {
		t.Fatal(err)
	}
	return h
}

// getWith returns the response of h to a GET of path with the request
// headers in header, as name and value pairs.
func getWith(h http.Handler, path string, header ...string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodGet, path, nil)
	for i := 0; i+1 < len(header); i += 2 {
		r.Header.Set(header[i], header[i+1])
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	return rec
}

// adasLinks is the end of the navigation bar of the reader Ada.
const adasLinks = `<span class="portal-account"><span>Ada</span><span><a href="/auth/sign-out">Sign out</a></span></span>`

// pagesWithABar lists a request for each HTML page of shopAndStore's
// portals that draws the navigation bar.
var pagesWithABar = []string{
	"/portals/pet-shop/specs/api",
	"/portals/pet-shop/specs/gone",
	"/portals/pet-shop/docs/guides/",
	"/portals/pet-shop/docs/guides/a.md",
	"/portals/pet-shop/docs/guides/gone.md",
	"/portals/pet-shop/docs/gone/a.md",
	"/portals/store/specs/api",
	"/portals/other/specs/api",
}

func TestAccountLinksEndEveryNavigationBar(t *testing.T) {
	root, portals := shopAndStore()
	h := newAccountPortal(t, root, portals, nil, signedIn)
	for _, path := range pagesWithABar {
		page := getWith(h, path, "Reader", "Ada").Body.String()
		nav := page[max(strings.Index(page, "<nav"), 0):max(strings.Index(page, "</nav>"), 0)]
		if !strings.HasSuffix(nav, adasLinks) {
			t.Errorf("%s: the navigation bar does not end with Ada's links: %q", path, nav)
		}
		if menu := strings.Index(nav, `class="portal-menu"`); menu >= 0 && menu > strings.Index(nav, adasLinks) {
			t.Errorf("%s: the account links come before the portal menu: %q", path, nav)
		}
	}
}

func TestTheHomePageShowsTheAccountLinks(t *testing.T) {
	root, portals := shopAndStore()
	home := getWith(newAccountPortal(t, root, portals, nil, signedIn), "/", "Reader", "Ada").Body.String()
	if nav := navOf(home); nav != `<nav class="portal-nav">`+adasLinks || !strings.Contains(home, `<a href="/portals/store/specs/api">Store</a>`) {
		t.Errorf("the home page of Ada: %q", home)
	}
	// A reader who sees no portal can still sign out.
	nobody := getWith(newAccountPortal(t, root, portals, fakeaccess.Hook, signedIn), "/", "Reader", "Ada").Body.String()
	if !strings.Contains(nobody, "No portals are open to you.") || !strings.Contains(nobody, adasLinks) {
		t.Errorf("the home page of Ada, who sees no portal: %q", nobody)
	}
	none := getWith(newAccountPortal(t, root, portals, nil, func(*http.Request) []portal.AccountLink { return nil }), "/").Body.String()
	if strings.Contains(none, "<nav") || strings.Contains(none, "portal-nav") {
		t.Errorf("the home page without account links has a navigation bar: %q", none)
	}
}

func TestAccountLinksAreText(t *testing.T) {
	root, portals := shopAndStore()
	h := newAccountPortal(t, root, portals, nil, func(*http.Request) []portal.AccountLink {
		return []portal.AccountLink{{Label: `<img src=x onerror=alert(1)>Ada`}, {Label: "Sign out", URL: "javascript:alert(1)"}, {Label: `"Bo" & co`, URL: `/auth/sign-out?next=/a&b="c"`}}
	})
	for _, path := range []string{"/", "/portals/pet-shop/specs/api"} {
		nav := navOf(getWith(h, path).Body.String())
		for _, want := range []string{`<span>&lt;img src=x onerror=alert(1)&gt;Ada</span>`, `<span><a href="#ZgotmplZ">Sign out</a></span>`, `<span><a href="/auth/sign-out?next=/a&amp;b=%22c%22">&#34;Bo&#34; &amp; co</a></span>`} {
			if !strings.Contains(nav, want) {
				t.Errorf("%s: the navigation bar lacks %s: %q", path, want, nav)
			}
		}
		if strings.Contains(nav, "<img") || strings.Contains(nav, "javascript:") {
			t.Errorf("%s: the navigation bar runs the account links' markup: %q", path, nav)
		}
	}
}

func TestResponsesArePrivateWithAnAccountHook(t *testing.T) {
	root, portals := accessRoot()
	h := newAccountPortal(t, root, portals, nil, signedIn)
	for _, path := range append(everyRoute, "/assets/elements/styles.min.css", "/assets/elements/missing.css") {
		if rec := getWith(h, path); rec.Header().Get("Cache-Control") != "private" {
			t.Errorf("%s with an account hook: %d, Cache-Control %q, want private", path, rec.Code, rec.Header().Get("Cache-Control"))
		}
	}
}

func TestTheAccountHookAnswersOncePerRequest(t *testing.T) {
	root, portals := shopAndStore()
	calls := 0
	h := newAccountPortal(t, root, portals, nil, func(r *http.Request) []portal.AccountLink {
		calls++
		return signedIn(r)
	})
	for _, path := range []string{"/portals/pet-shop/docs/guides/a.md", "/portals/pet-shop/api/specs/api", "/"} {
		calls = 0
		getWith(h, path, "Reader", "Ada")
		if calls != 1 {
			t.Errorf("%s: %d calls of the account hook, want one", path, calls)
		}
	}
}

func TestAccountLinksOf(t *testing.T) {
	var got [][]portal.AccountLink
	keep := portal.Route{Pattern: "GET /portals/store/links", Handler: http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		links := portal.AccountLinksOf(r)
		got = append(got, links)
		if len(links) > 0 {
			links[0].Label = "changed"
		}
		got = append(got, portal.AccountLinksOf(r))
	})}
	root, portals := shopAndStore()
	for name, tc := range map[string]struct {
		hook func(*http.Request) []portal.AccountLink
		want []portal.AccountLink
	}{
		"an account hook": {signedIn, []portal.AccountLink{{Label: "Ada"}, {Label: "Sign out", URL: "/auth/sign-out"}}},
		"no account hook": {nil, nil},
	} {
		h, err := portal.New(portal.Config{Root: root, Portals: portals, Chat: []portal.Route{keep}, Account: tc.hook})
		if err != nil {
			t.Fatal(err)
		}
		got = nil
		getWith(h, "/portals/store/links", "Reader", "Ada")
		if len(got) != 2 || !slices.Equal(got[1], tc.want) {
			t.Errorf("%s: AccountLinksOf gave %v, want %v, and a copy each time", name, got, tc.want)
		}
	}
	if links := portal.AccountLinksOf(httptest.NewRequest(http.MethodGet, "/portals/store/links", nil)); len(links) != 0 {
		t.Errorf("a request served by no portal handler: %v, want no links", links)
	}
}

func TestAnAccountHookWithoutLinksChangesNoPage(t *testing.T) {
	root, portals := accessRoot()
	plain := newAccountPortal(t, root, portals, nil, nil)
	hooked := newAccountPortal(t, root, portals, nil, func(*http.Request) []portal.AccountLink { return nil })
	for _, path := range everyRoute {
		got, want := getWith(hooked, path), getWith(plain, path)
		if got.Code != want.Code || got.Body.String() != want.Body.String() || got.Header().Get("Location") != want.Header().Get("Location") {
			t.Errorf("%s with an account hook that gives no links: %d %q, want %d %q", path, got.Code, got.Body, want.Code, want.Body)
		}
	}
}

// The navigation bar of shopAndStore's pages before account links: its style
// and its markup, with the portal menu, on the 404 page of a missing portal,
// and in a portal alone.
const (
	barStyle   = "<style>\n.portal-nav { display: flex; gap: 1.25rem; align-items: center; height: 2.5rem; padding: 0 1rem; border-bottom: 1px solid #d8dde6; font: 14px Inter, ui-sans-serif, system-ui, sans-serif; }\n.portal-nav a { color: inherit; text-decoration: none; }\n"
	menuStyle  = ".portal-menu { margin-left: auto; position: relative; }\n.portal-menu summary { cursor: pointer; }\n.portal-menu-list { position: absolute; right: 0; top: 100%; z-index: 10; display: flex; flex-direction: column; min-width: 12rem; margin-top: .5rem; padding: .25rem 0; background: #fff; border: 1px solid #d8dde6; border-radius: 4px; }\n.portal-menu-list a { padding: .375rem 1rem; }\n.portal-menu-list a:hover { background: #f1f3f6; }\n"
	shopBar    = barStyle + menuStyle + "</style>\n" + `<nav class="portal-nav"><a href="/portals/pet-shop/specs/api">API</a><a href="/portals/pet-shop/docs/guides/">Guides</a><details class="portal-menu"><summary>Pet Shop</summary><div class="portal-menu-list"><a href="/portals/pet-shop/specs/api">Pet Shop</a><a href="/portals/store/specs/api">Store</a></div></details></nav>`
	missingBar = barStyle + "</style>\n" + `<nav class="portal-nav"><a href="/">Portals</a></nav>`
	storeBar   = barStyle + "</style>\n" + `<nav class="portal-nav"><a href="/portals/store/specs/api">API</a></nav>`
)

// barOf returns the navigation bar of page with its style, or "" for a page
// without one.
func barOf(page string) string {
	start, end := strings.Index(page, "<style>\n.portal-nav"), strings.Index(page, "</nav>")
	if start < 0 || end < start {
		return ""
	}
	return page[start : end+len("</nav>")]
}

func TestWithoutAccountLinksEveryBarIsAsBefore(t *testing.T) {
	root, portals := shopAndStore()
	two, one := newPortals(t, root, portals...), newPortals(t, root, portals[1])
	for _, tc := range []struct {
		h          http.Handler
		path, want string
	}{
		{two, "/portals/pet-shop/specs/api", shopBar},
		{two, "/portals/pet-shop/specs/gone", shopBar},
		{two, "/portals/pet-shop/docs/guides/", shopBar},
		{two, "/portals/pet-shop/docs/guides/a.md", shopBar},
		{two, "/portals/pet-shop/docs/guides/gone.md", shopBar},
		{two, "/portals/other/specs/api", missingBar},
		{two, "/", ""},
		{one, "/portals/store/specs/api", storeBar},
	} {
		if got := barOf(get(tc.h, tc.path).Body.String()); got != tc.want {
			t.Errorf("%s: the navigation bar %q, want %q", tc.path, got, tc.want)
		}
	}
}
