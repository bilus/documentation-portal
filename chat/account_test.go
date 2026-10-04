package chat

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/bilus/live-templ/interpreter"

	"github.com/bilus/documentation-portal/internal/fakemodel"
	"github.com/bilus/documentation-portal/portal"
)

// adasLinks are the account links of the reader Ada.
var adasLinks = []portal.AccountLink{{Label: "Ada"}, {Label: "Sign out", URL: "/auth/sign-out"}}

// servedWithAccount returns a portal handler that serves the routes of c and
// more, as servedByPortal does, with an account hook that gives adasLinks to
// a request whose Reader header names Ada, and no links to any other.
func servedWithAccount(t *testing.T, c *Chat, more ...portal.Route) http.Handler {
	t.Helper()
	var portals []portal.Portal
	for _, a := range c.currentAgents() {
		portals = append(portals, portal.Portal{Name: a.lib.Name(), Sections: []portal.Section{{Title: "API", Type: portal.SpecSection, Input: "api.yaml"}}})
	}
	account := func(r *http.Request) []portal.AccountLink {
		if r.Header.Get(readerHeader) == "Ada" {
			return adasLinks
		}
		return nil
	}
	h, err := portal.New(portal.Config{Root: fstest.MapFS{}, Portals: portals, Chat: append(c.Routes(), more...), Account: account})
	if err != nil {
		t.Fatal(err)
	}
	return h
}

// navOf returns the navigation bar of page, without its closing tag.
func navOf(page string) string {
	return page[max(strings.Index(page, "<nav"), 0):max(strings.Index(page, "</nav>"), 0)]
}

func TestAChatTabShowsTheAccountLinksOfItsGET(t *testing.T) {
	c, err := New(Config{Model: fakemodel.New("opus", nil), Libraries: twoPortals(t)})
	if err != nil {
		t.Fatal(err)
	}
	var session map[string]string
	keep := portal.Route{Pattern: "GET /session", Handler: http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		var err error
		if session, err = c.sessionOf(r); err != nil {
			t.Error(err)
		}
	})}
	h := servedWithAccount(t, c, keep)
	r := httptest.NewRequest(http.MethodGet, "/portals/pet-shop/chat", nil)
	r.Header.Set(readerHeader, "Ada")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	nav := navOf(rec.Body.String())
	want := `<span class="portal-account"><span>Ada</span><span><a href="/auth/sign-out">Sign out</a></span></span>`
	if menu := strings.Index(nav, "</details>"); menu < 0 || !strings.HasSuffix(nav, want) {
		t.Errorf("the chat page's navigation bar does not end with Ada's links after the portal menu: %q", nav)
	}
	// The join mounts the page again from its page session alone.
	r = httptest.NewRequest(http.MethodGet, "/session", nil)
	r.Header.Set(readerHeader, "Ada")
	h.ServeHTTP(httptest.NewRecorder(), r)
	u := &url.URL{Path: "/portals/pet-shop/chat"}
	p, err := c.mount(interpreter.NewCtx(t.Context(), u.Path, u, session, true), c.currentAgents()[0])
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(p.Account, adasLinks) {
		t.Errorf("the joined page's account links: %v, want %v", p.Account, adasLinks)
	}
}

func TestThePageSessionHoldsTheAccountLinks(t *testing.T) {
	c, err := New(Config{Model: fakemodel.New("opus", nil), Libraries: twoPortals(t)})
	if err != nil {
		t.Fatal(err)
	}
	var session map[string]string
	keep := portal.Route{Pattern: "GET /session", Handler: http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		var err error
		if session, err = c.sessionOf(r); err != nil {
			t.Error(err)
		}
	})}
	for name, tc := range map[string]struct {
		h      http.Handler
		reader string
		want   []portal.AccountLink
	}{
		"Ada, with an account hook":    {servedWithAccount(t, c, keep), "Ada", adasLinks},
		"a reader without links":       {servedWithAccount(t, c, keep), "", nil},
		"Ada, without an account hook": {servedByPortal(t, c, keep), "Ada", nil},
	} {
		session = nil
		r := httptest.NewRequest(http.MethodGet, "/session", nil)
		r.Header.Set(readerHeader, tc.reader)
		tc.h.ServeHTTP(httptest.NewRecorder(), r)
		var got []portal.AccountLink
		if err := json.Unmarshal([]byte(session["account"]), &got); err != nil || !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: the page session holds the account links %q, %v, want %v", name, session["account"], err, tc.want)
		}
	}
}

func TestReadAccountLinks(t *testing.T) {
	if got := readAccountLinks(`[{"Label":"Ada"},{"Label":"Sign out","URL":"/auth/sign-out"}]`); !reflect.DeepEqual(got, adasLinks) {
		t.Errorf("Ada's links read back: %v", got)
	}
	for _, value := range []string{"", "null", "[]", "{", `{"Label":"Ada"}`, `"Ada"`, `[{"Label":"Ada"},1]`} {
		if got := readAccountLinks(value); len(got) != 0 {
			t.Errorf("%q: %v, want no links", value, got)
		}
	}
}

func TestAChatTabsAccountLinksAreText(t *testing.T) {
	c, err := New(Config{Model: fakemodel.New("opus", nil), Libraries: twoPortals(t)})
	if err != nil {
		t.Fatal(err)
	}
	var portals []portal.Portal
	for _, a := range c.currentAgents() {
		portals = append(portals, portal.Portal{Name: a.lib.Name(), Sections: []portal.Section{{Title: "API", Type: portal.SpecSection, Input: "api.yaml"}}})
	}
	h, err := portal.New(portal.Config{Root: fstest.MapFS{}, Portals: portals, Chat: c.Routes(), Account: func(*http.Request) []portal.AccountLink {
		return []portal.AccountLink{{Label: "<img src=x onerror=alert(1)>Ada"}, {Label: "Sign out", URL: "javascript:alert(1)"}}
	}})
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/portals/pet-shop/chat", nil))
	nav := navOf(rec.Body.String())
	if !strings.Contains(nav, "&lt;img src=x onerror=alert(1)&gt;Ada") || strings.Contains(nav, "<img") || strings.Contains(nav, "javascript:") {
		t.Errorf("the chat page runs the account links' markup: %q", nav)
	}
}

// chatBars are the navigation bars of twoPortals' chat pages before account
// links: the bar of Pet Shop's page, of Store's, and of the page of a chat
// with Pet Shop alone.
var chatBars = []string{
	`<nav class="portal-nav"><a href="/portals/pet-shop/specs/api" class="">API</a> <a href="/portals/pet-shop/chat" class="current">Chat</a> <details class="portal-menu"><summary>Pet Shop</summary><div class="portal-menu-list"><a href="/portals/pet-shop/specs/api">Pet Shop</a><a href="/portals/store/specs/api">Store</a></div></details></nav>`,
	`<nav class="portal-nav"><a href="/portals/store/specs/api" class="">API</a> <a href="/portals/store/chat" class="current">Chat</a> <details class="portal-menu"><summary>Store</summary><div class="portal-menu-list"><a href="/portals/pet-shop/specs/api">Pet Shop</a><a href="/portals/store/specs/api">Store</a></div></details></nav>`,
	`<nav class="portal-nav"><a href="/portals/pet-shop/specs/api" class="">API</a> <a href="/portals/pet-shop/chat" class="current">Chat</a> </nav>`,
}

func TestWithoutAccountLinksTheChatBarIsAsBefore(t *testing.T) {
	two, err := New(Config{Model: fakemodel.New("opus", nil), Libraries: twoPortals(t)})
	if err != nil {
		t.Fatal(err)
	}
	one, err := New(Config{Model: fakemodel.New("opus", nil), Libraries: twoPortals(t)[:1]})
	if err != nil {
		t.Fatal(err)
	}
	for i, tc := range []struct {
		h    http.Handler
		path string
	}{
		{servedByPortal(t, two), "/portals/pet-shop/chat"},
		{servedWithAccount(t, two), "/portals/store/chat"},
		{servedByPortal(t, one), "/portals/pet-shop/chat"},
	} {
		rec := httptest.NewRecorder()
		tc.h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tc.path, nil))
		page := rec.Body.String()
		if got := navOf(page) + "</nav>"; got != chatBars[i] || strings.Contains(page, "portal-account") {
			t.Errorf("%s without account links: %q, want %q", tc.path, got, chatBars[i])
		}
	}
}
