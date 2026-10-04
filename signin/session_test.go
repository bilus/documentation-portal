package signin

import (
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/bilus/documentation-portal/portal"
)

func TestTamperedOrExpiredSessionCookiesAreRefused(t *testing.T) {
	t.Skip("HOLE(1): refuse a session cookie that is tampered with, expired, or sealed under another key, by another portal or for another cookie")
	f := serveSignIn(t, stubConfig(newStubProvider(t)))
	ada := Identity{Subject: "ada", Name: "Ada Lovelace"}
	if resp := f.requestWith(t, "/portals/pets/", sessionFor(t, f.m, ada, f.clock.now().Add(time.Hour))); resp.StatusCode != http.StatusOK {
		t.Fatalf("a sound session cookie got %d", resp.StatusCode)
	}

	tampered := sessionFor(t, f.m, ada, f.clock.now().Add(time.Hour))
	value := []byte(tampered.Value)
	if i := len(value) / 2; value[i] == 'A' {
		value[i] = 'B'
	} else {
		value[i] = 'A'
	}
	tampered.Value = string(value)
	other := serveSignIn(t, Config{Provider: newStubProvider(t), ClientID: "portal", ClientSecret: "the client secret", Key: []byte(strings.Repeat("another key ", 4))})
	// Another portal, with another callback URL and the same key.
	neighbour := serveSignIn(t, stubConfig(newStubProvider(t)))
	asAttempt, err := f.m.seal(attemptCookie, session{ID: "an attempt's value", Identity: ada, Expires: f.clock.now().Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	plain := base64.RawURLEncoding.EncodeToString([]byte(jsonOf(session{ID: "plain", Identity: ada, Expires: f.clock.now().Add(time.Hour)})))
	for name, c := range map[string]*http.Cookie{
		"tampered with":                 tampered,
		"expired":                       sessionFor(t, f.m, ada, f.clock.now().Add(-time.Second)),
		"sealed under another key":      sessionFor(t, other.m, ada, other.clock.now().Add(time.Hour)),
		"sealed by another portal":      sessionFor(t, neighbour.m, ada, neighbour.clock.now().Add(time.Hour)),
		"sealed for the attempt cookie": {Name: sessionCookie, Value: asAttempt},
		"not sealed":                    {Name: sessionCookie, Value: plain},
	} {
		before := f.pages.reached()
		resp := f.requestWith(t, "/portals/pets/", c)
		if resp.StatusCode != http.StatusFound || f.pages.reached() != before {
			t.Errorf("a session cookie %s got %d and reached the portal %d times, want a sign-in", name, resp.StatusCode, f.pages.reached()-before)
		}
	}
}

func TestIdentityOf(t *testing.T) {
	t.Skip("HOLE(1): give the wrapped handler a copy of the reader's identity, claims included")
	f := serveSignIn(t, stubConfig(newStubProvider(t)))
	ada := Identity{Subject: "ada", Name: "Ada Lovelace", Email: "ada@example.com", Claims: Claims{
		"groups": []any{"staff", "partners"},
		"org":    map[string]any{"name": "Analytical Engines"},
	}}
	var first, again Identity
	var signedIn bool
	f.pages.then = func(_ http.ResponseWriter, r *http.Request) {
		first, signedIn = IdentityOf(r)
		if !signedIn {
			return
		}
		first.Claims["groups"].([]any)[0] = "admins"
		first.Claims["org"].(map[string]any)["name"] = "Mallory's"
		first.Claims["role"] = "admin"
		again, _ = IdentityOf(r)
	}
	if resp := f.requestWith(t, "/portals/pets/", sessionFor(t, f.m, ada, f.clock.now().Add(time.Hour))); resp.StatusCode != http.StatusOK {
		t.Fatalf("the page answered %d", resp.StatusCode)
	}
	if !signedIn {
		t.Fatal("the wrapped handler got no identity")
	}
	if !reflect.DeepEqual(again, ada) {
		t.Errorf("after a change to a copy, the identity is %s, want %s", jsonOf(again), jsonOf(ada))
	}
	if id, ok := IdentityOf(httptest.NewRequest(http.MethodGet, "/portals/pets/", nil)); ok {
		t.Errorf("a request outside the middleware has the identity %s", jsonOf(id))
	}
}

func TestTheReadyHooks(t *testing.T) {
	t.Skip("HOLE(1): give the account links and the reader ID of the signed-in reader of a request")
	cfg := stubConfig(newStubProvider(t))
	cfg.SignOutPath = "/account/sign-out"
	f := serveSignIn(t, cfg)
	for _, tc := range []struct {
		id    Identity
		label string
	}{
		{Identity{Subject: "ada", Name: "Ada Lovelace", Email: "ada@example.com"}, "Ada Lovelace"},
		{Identity{Subject: "ada", Email: "ada@example.com"}, "ada@example.com"},
		{Identity{Subject: "ada"}, "ada"},
	} {
		var links []portal.AccountLink
		var readerID string
		f.pages.then = func(_ http.ResponseWriter, r *http.Request) {
			links, readerID = AccountLinks(r), ReaderID(r)
		}
		f.requestWith(t, "/portals/pets/", sessionFor(t, f.m, tc.id, f.clock.now().Add(time.Hour)))
		want := []portal.AccountLink{{Label: tc.label}, {Label: "Sign out", URL: "/account/sign-out"}}
		if !slices.Equal(links, want) || readerID != "ada" {
			t.Errorf("for %s: the account links %v and the reader ID %q, want %v and ada", jsonOf(tc.id), links, readerID, want)
		}
	}
	outside := httptest.NewRequest(http.MethodGet, "/portals/pets/", nil)
	if links, id := AccountLinks(outside), ReaderID(outside); links != nil || id != "" {
		t.Errorf("outside the middleware: the account links %v and the reader ID %q, want none", links, id)
	}
}

func TestSignOutEndsTheSession(t *testing.T) {
	t.Skip("HOLE(1): revoke the session at sign-out and delete its cookie, so that a copy of the cookie stops working")
	f := serveSignIn(t, stubConfig(newStubProvider(t)))
	ada := sessionFor(t, f.m, Identity{Subject: "ada"}, f.clock.now().Add(time.Hour))
	grace := sessionFor(t, f.m, Identity{Subject: "grace"}, f.clock.now().Add(time.Hour))

	r, err := http.NewRequest(http.MethodGet, f.srv.URL+"/auth/sign-out", nil)
	if err != nil {
		t.Fatal(err)
	}
	r.AddCookie(ada)
	resp, err := stopAtRedirects(http.DefaultClient).Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	page, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(page), `href="/"`) {
		t.Errorf("the sign-out answered %d with %q, want the signed-out page with a link to sign in again", resp.StatusCode, page)
	}
	if cc := resp.Header.Get("Cache-Control"); cc != "no-store" {
		t.Errorf("the signed-out page has Cache-Control %q, want no-store", cc)
	}
	if !deletesTheSession(resp) {
		t.Errorf("the sign-out did not delete the session cookie: %q", resp.Header.Values("Set-Cookie"))
	}
	if resp := f.requestWith(t, "/portals/pets/", ada); resp.StatusCode != http.StatusFound {
		t.Errorf("a copy of the signed-out session's cookie got %d, want a sign-in", resp.StatusCode)
	}
	if resp := f.requestWith(t, "/portals/pets/", grace); resp.StatusCode != http.StatusOK {
		t.Errorf("another reader's session got %d after the sign-out, want the page", resp.StatusCode)
	}
}

// deletesTheSession reports whether resp deletes the session cookie of the
// whole portal.
func deletesTheSession(resp *http.Response) bool {
	for _, c := range resp.Cookies() {
		if c.Name == sessionCookie && c.MaxAge < 0 && c.Path == "/" {
			return true
		}
	}
	return false
}

func TestSignOutGoesToTheLogoutURL(t *testing.T) {
	t.Skip("HOLE(1): send the reader to the logout URL after the sign-out")
	const logout = "https://idp.example/v2/logout?client_id=portal&returnTo=https%3A%2F%2Fdocs.example%2F"
	cfg := stubConfig(newStubProvider(t))
	cfg.LogoutURL = logout
	f := serveSignIn(t, cfg)
	r, err := http.NewRequest(http.MethodGet, f.srv.URL+"/auth/sign-out", nil)
	if err != nil {
		t.Fatal(err)
	}
	r.AddCookie(sessionFor(t, f.m, Identity{Subject: "ada"}, f.clock.now().Add(time.Hour)))
	resp, err := stopAtRedirects(http.DefaultClient).Do(r)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusFound || resp.Header.Get("Location") != logout || !deletesTheSession(resp) {
		t.Errorf("the sign-out answered %d for %q with the cookies %q, want the logout URL and the session cookie deleted",
			resp.StatusCode, resp.Header.Get("Location"), resp.Header.Values("Set-Cookie"))
	}
	if cc := resp.Header.Get("Cache-Control"); cc != "no-store" {
		t.Errorf("the sign-out's redirect has Cache-Control %q, want no-store", cc)
	}
}

func TestResponsesToASignedInReaderArePrivate(t *testing.T) {
	t.Skip("HOLE(1): mark each response to a signed-in reader private, whatever the wrapped handler sets")
	f := serveSignIn(t, stubConfig(newStubProvider(t)))
	f.pages.then = func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=60")
	}
	resp := f.requestWith(t, "/portals/pets/", sessionFor(t, f.m, Identity{Subject: "ada"}, f.clock.now().Add(time.Hour)))
	if cc := resp.Header.Get("Cache-Control"); resp.StatusCode != http.StatusOK || cc != "private" {
		t.Errorf("a signed-in reader's page answered %d with Cache-Control %q, want private", resp.StatusCode, cc)
	}
}

func TestClaimsStrings(t *testing.T) {
	t.Skip("HOLE(1): read a claim as a list of strings")
	c := Claims{
		"groups": []any{"staff", 7.0, "partners"},
		"role":   "admin",
		"count":  3.0,
		"names":  []string{"ada", "grace"},
		"empty":  []any{},
		"none":   nil,
	}
	for name, want := range map[string][]string{
		"groups":  {"staff", "partners"},
		"role":    {"admin"},
		"count":   nil,
		"names":   {"ada", "grace"},
		"empty":   nil,
		"none":    nil,
		"missing": nil,
	} {
		if got := c.Strings(name); !slices.Equal(got, want) {
			t.Errorf("Strings(%q) = %q, want %q", name, got, want)
		}
	}
}
