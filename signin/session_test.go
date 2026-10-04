package signin

import (
	"bytes"
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

func TestCookieCipher(t *testing.T) {
	sealed := func(key []byte, callback string) []byte {
		t.Helper()
		c, err := cookieCipher(key, callback)
		if err != nil {
			t.Fatal(err)
		}
		return c.Seal(nil, make([]byte, c.NonceSize()), []byte("a session"), nil)
	}
	one := sealed(testKey, "https://docs.example/auth/callback")
	if !bytes.Equal(one, sealed(testKey, "https://docs.example/auth/callback")) {
		t.Error("one key and one callback URL gave two ciphers")
	}
	if bytes.Equal(one, sealed([]byte(strings.Repeat("another key ", 4)), "https://docs.example/auth/callback")) {
		t.Error("two keys gave one cipher")
	}
	if bytes.Equal(one, sealed(testKey, "https://other.example/auth/callback")) {
		t.Error("two callback URLs gave one cipher")
	}
}

func TestOpenRefusesWhatSealDidNotMake(t *testing.T) {
	f := serveSignIn(t, stubConfig(newStubProvider(t)))
	sealed, err := f.m.seal(sessionCookie, session{ID: "s", Identity: Identity{Subject: "ada"}})
	if err != nil {
		t.Fatal(err)
	}
	var s session
	if err := f.m.open(sessionCookie, sealed, &s); err != nil || s.Identity.Subject != "ada" {
		t.Fatalf("open(seal(s)) = %s, %v", jsonOf(s), err)
	}
	for name, value := range map[string]string{
		"an empty value":               "",
		"a value shorter than a nonce": "AAAA",
		"a value that is not base64":   "not base64!",
		"a value cut short":            sealed[:len(sealed)-4],
	} {
		if err := f.m.open(sessionCookie, value, &s); err == nil {
			t.Errorf("%s opened", name)
		}
	}
	if err := f.m.open(attemptCookie, sealed, &s); err == nil {
		t.Error("a session cookie's value opened as an attempt cookie's")
	}
}

func TestASessionNeedsAnIDAndASubject(t *testing.T) {
	f := serveSignIn(t, stubConfig(newStubProvider(t)))
	for name, s := range map[string]session{
		"no ID":      {Identity: Identity{Subject: "ada"}, Expires: f.clock.now().Add(time.Hour)},
		"no subject": {ID: "s", Identity: Identity{Name: "Ada"}, Expires: f.clock.now().Add(time.Hour)},
	} {
		value, err := f.m.seal(sessionCookie, s)
		if err != nil {
			t.Fatal(err)
		}
		if resp := f.requestWith(t, "/portals/pets/", &http.Cookie{Name: sessionCookie, Value: value}); resp.StatusCode != http.StatusFound {
			t.Errorf("a session with %s got %d, want a sign-in", name, resp.StatusCode)
		}
	}
}

func TestCloneJSON(t *testing.T) {
	names := []string{"ada"}
	orig := map[string]any{"names": names, "list": []any{map[string]any{"a": 1.0}}}
	c := cloneJSON(orig).(map[string]any)
	c["names"].([]string)[0] = "eve"
	c["list"].([]any)[0].(map[string]any)["a"] = 2.0
	if names[0] != "ada" || orig["list"].([]any)[0].(map[string]any)["a"] != 1.0 {
		t.Errorf("a change to the copy reached the original: %v", orig)
	}
}

func TestRevokeForgetsTheSessionsPastTheirExpiry(t *testing.T) {
	f := serveSignIn(t, stubConfig(newStubProvider(t)))
	f.m.revoke(session{ID: "short", Expires: f.clock.now().Add(time.Hour)})
	f.m.revoke(session{ID: "long", Expires: f.clock.now().Add(3 * time.Hour)})
	f.clock.advance(2 * time.Hour)
	f.m.revoke(session{ID: "new", Expires: f.clock.now().Add(time.Hour)})
	f.m.mu.Lock()
	defer f.m.mu.Unlock()
	if _, ok := f.m.revoked["short"]; ok || len(f.m.revoked) != 2 {
		t.Errorf("the revoked sessions %v, want long and new", f.m.revoked)
	}
}

func TestSignOutWithoutASession(t *testing.T) {
	f := serveSignIn(t, stubConfig(newStubProvider(t)))
	resp := f.requestWith(t, "/auth/sign-out", nil)
	if resp.StatusCode != http.StatusOK || !deletesTheSession(resp) || f.pages.reached() != 0 {
		t.Errorf("a sign-out without a session answered %d with the cookies %q", resp.StatusCode, resp.Header.Values("Set-Cookie"))
	}
}

func TestARevokedSessionIsRefused(t *testing.T) {
	f := serveSignIn(t, stubConfig(newStubProvider(t)))
	c := sessionFor(t, f.m, Identity{Subject: "ada"}, f.clock.now().Add(time.Hour))
	s := opened(t, f.m, c)
	f.m.mu.Lock()
	f.m.revoked[s.ID] = s.Expires
	f.m.mu.Unlock()
	if resp := f.requestWith(t, "/portals/pets/", c); resp.StatusCode != http.StatusFound || f.pages.reached() != 0 {
		t.Errorf("a revoked session got %d, want a sign-in", resp.StatusCode)
	}
}

func TestASignOutFromAnotherSiteAsksFirst(t *testing.T) {
	f := serveSignIn(t, stubConfig(newStubProvider(t)))
	ada := sessionFor(t, f.m, Identity{Subject: "ada"}, f.clock.now().Add(time.Hour))
	for site, signsOut := range map[string]bool{"cross-site": false, "same-origin": true, "same-site": true, "none": true} {
		c := sessionFor(t, f.m, Identity{Subject: "ada"}, f.clock.now().Add(time.Hour))
		if site == "cross-site" {
			c = ada
		}
		r, err := http.NewRequest(http.MethodGet, f.srv.URL+"/auth/sign-out", nil)
		if err != nil {
			t.Fatal(err)
		}
		r.Header.Set("Sec-Fetch-Site", site)
		r.AddCookie(c)
		resp, err := stopAtRedirects(http.DefaultClient).Do(r)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if deletesTheSession(resp) != signsOut {
			t.Errorf("a sign-out with Sec-Fetch-Site %s deleted the session cookie: %v, want %v", site, !signsOut, signsOut)
		}
		if !signsOut && (resp.StatusCode != http.StatusOK || !strings.Contains(string(body), `href="/auth/sign-out"`)) {
			t.Errorf("a cross-site sign-out answered %d with %q, want a page that links the sign-out", resp.StatusCode, body)
		}
	}
	if resp := f.requestWith(t, "/portals/pets/", ada); resp.StatusCode != http.StatusOK {
		t.Errorf("a cross-site sign-out ended the session: %d", resp.StatusCode)
	}
}
