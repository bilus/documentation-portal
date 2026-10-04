package signin

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/oauth2-proxy/mockoidc"
	"golang.org/x/oauth2"
)

// reader is a reader at the mock OpenID Connect provider, whose ID token
// the function alter may change before the provider's signature.
type reader struct {
	subject, name, username, email string
	groups                         []string
	alter                          func(*mockoidc.IDTokenClaims)
}

// readerClaims are the claims of a reader's ID token.
type readerClaims struct {
	*mockoidc.IDTokenClaims
	Name     string   `json:"name,omitempty"`
	Username string   `json:"preferred_username,omitempty"`
	Email    string   `json:"email,omitempty"`
	Groups   []string `json:"groups,omitempty"`
}

func (u reader) ID() string { return u.subject }

func (u reader) Userinfo([]string) ([]byte, error) {
	return json.Marshal(map[string]string{"sub": u.subject, "name": u.name, "email": u.email})
}

func (u reader) Claims(_ []string, base *mockoidc.IDTokenClaims) (jwt.Claims, error) {
	if u.alter != nil {
		u.alter(base)
	}
	return readerClaims{IDTokenClaims: base, Name: u.name, Username: u.username, Email: u.email, Groups: u.groups}, nil
}

// ada is the reader of the mock OpenID Connect provider in the tests.
var ada = reader{subject: "ada", name: "Ada Lovelace", email: "ada@example.com", groups: []string{"staff"}}

// newOIDC starts a mock OpenID Connect provider, changed first by setup, if
// any, such as with a middleware. The provider authorizes its default
// reader at each authorization request, or a reader from its queue.
func newOIDC(t *testing.T, setup ...func(*mockoidc.MockOIDC)) *mockoidc.MockOIDC {
	t.Helper()
	o, err := mockoidc.NewServer(nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range setup {
		s(o)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	if err := o.Start(ln, nil); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { o.Shutdown() })
	return o
}

// oidcConfig returns a configuration of the sign-in through o.
func oidcConfig(o *mockoidc.MockOIDC) Config {
	return Config{Issuer: o.Issuer(), ClientID: o.ClientID, ClientSecret: o.ClientSecret}
}

// queue queues ada as the reader of the next authorization request, with
// the change alter to her ID token.
func queue(o *mockoidc.MockOIDC, alter func(*mockoidc.IDTokenClaims)) {
	u := ada
	u.alter = alter
	o.QueueUser(u)
}

func TestSignInReturnsToTheFirstPage(t *testing.T) {
	o := newOIDC(t)
	queue(o, nil)
	f := serveSignIn(t, oidcConfig(o))
	b, tr := browser(t)
	resp, body := fetch(t, b, f.srv.URL+"/portals/pets/docs/documents/a.md?highlight=1")
	if got := resp.Request.URL.RequestURI(); got != "/portals/pets/docs/documents/a.md?highlight=1" || body != `the page of "ada"` {
		t.Fatalf("the sign-in ended at %s with %q, want the first page as ada", got, body)
	}
	id := f.pages.last()
	if id.Subject != "ada" || id.Name != "Ada Lovelace" || id.Email != "ada@example.com" || !slices.Equal(id.Claims.Strings("groups"), []string{"staff"}) {
		t.Errorf("the portal got the identity %s", jsonOf(id))
	}
	cookies := tr.sessionCookies()
	if len(cookies) != 1 {
		t.Fatalf("the sign-in set %d session cookies, want one", len(cookies))
	}
	if c := cookies[0]; !c.HttpOnly || c.SameSite != http.SameSiteLaxMode || c.Path != "/" || c.Secure {
		t.Errorf("the session cookie is HttpOnly %v, SameSite %v, Path %q, Secure %v; want HttpOnly, Lax, / and, over http, not Secure",
			c.HttpOnly, c.SameSite, c.Path, c.Secure)
	}
	// The browser opens a second page with its session.
	if _, body := fetch(t, b, f.srv.URL+"/portals/pets/specs/api"); body != `the page of "ada"` || f.pages.reached() != 2 {
		t.Errorf("the second page answered %q", body)
	}
}

func TestTheCookiesAreSecureOverHTTPS(t *testing.T) {
	cfg := oidcConfig(newOIDC(t))
	cfg.CallbackURL = "https://docs.example/auth/callback"
	f := serveSignIn(t, cfg)
	// A sibling host can set no __Host- cookie, which keeps the attempt to
	// this origin.
	attempt, callback := startAt(t, f, "https://docs.example/portals/pets/")
	if attempt.Name != "__Host-"+attemptCookie || !attempt.Secure || !attempt.HttpOnly || attempt.Path != "/" {
		t.Errorf("the attempt cookie %s, want a Secure, HttpOnly cookie __Host-%s for /", attempt, attemptCookie)
	}
	r := httptest.NewRequest(http.MethodGet, callback, nil)
	r.AddCookie(attempt)
	rec := httptest.NewRecorder()
	f.m.ServeHTTP(rec, r)
	var sess, deleted *http.Cookie
	for _, c := range rec.Result().Cookies() {
		switch {
		case c.Name == "__Host-"+sessionCookie:
			sess = c
		case c.Name == attempt.Name && c.MaxAge < 0 && c.Path == attempt.Path:
			deleted = c
		}
	}
	if rec.Code != http.StatusFound || sess == nil || !sess.Secure || !sess.HttpOnly || sess.Path != "/" {
		t.Fatalf("the callback answered %d with the cookies %q, want a Secure, HttpOnly cookie __Host-%s for /", rec.Code, rec.Result().Header.Values("Set-Cookie"), sessionCookie)
	}
	if deleted == nil {
		t.Errorf("the callback did not delete the attempt cookie: %q", rec.Result().Header.Values("Set-Cookie"))
	}
	// The browser's next page carries the session back.
	r = httptest.NewRequest(http.MethodGet, "https://docs.example/portals/pets/", nil)
	r.AddCookie(sess)
	rec = httptest.NewRecorder()
	f.m.ServeHTTP(rec, r)
	if rec.Code != http.StatusOK || f.pages.last().Subject != "1234567890" {
		t.Errorf("the session read back over https answered %d for %q", rec.Code, f.pages.last().Subject)
	}
}

// startAt starts a sign-in at the request target path in f's middleware,
// and returns the attempt cookie and the URL of the callback, the
// provider's answer to the authorization request.
func startAt(t *testing.T, f *fixture, path string) (*http.Cookie, string) {
	t.Helper()
	rec := httptest.NewRecorder()
	f.m.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	var attempt *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if strings.HasSuffix(c.Name, attemptCookie) {
			attempt = c
		}
	}
	if rec.Code != http.StatusFound || attempt == nil {
		t.Fatalf("the sign-in's start answered %d with the cookies %q", rec.Code, rec.Result().Header.Values("Set-Cookie"))
	}
	resp, err := stopAtRedirects(http.DefaultClient).Get(rec.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("the provider answered the authorization request with %d", resp.StatusCode)
	}
	return attempt, resp.Header.Get("Location")
}

// callBack sends the callback request to f's middleware with the attempt
// cookie, if not nil, and returns the response.
func callBack(t *testing.T, f *fixture, callback string, attempt *http.Cookie) *http.Response {
	t.Helper()
	r, err := http.NewRequest(http.MethodGet, callback, nil)
	if err != nil {
		t.Fatal(err)
	}
	if attempt != nil {
		r.AddCookie(attempt)
	}
	resp, err := stopAtRedirects(http.DefaultClient).Do(r)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

func TestTheReturnTargetFallsBackToTheRoot(t *testing.T) {
	o := newOIDC(t)
	f := serveSignIn(t, oidcConfig(o))
	for target, want := range map[string]string{
		"/portals/pets/?q=1":    "/portals/pets/?q=1",
		"//evil.example/x":      "/",
		"/%2F%2Fevil.example/x": "/%2F%2Fevil.example/x",
		"/portals/pets/a%0Ab":   "/portals/pets/a%0Ab",
	} {
		attempt, callback := startAt(t, f, target)
		resp := callBack(t, f, callback, attempt)
		if got := resp.Header.Get("Location"); resp.StatusCode != http.StatusFound || got != want {
			t.Errorf("a sign-in that started at %q ended with %d at %q, want %q", target, resp.StatusCode, got, want)
		}
	}
}

func TestASignInIssuesANewSession(t *testing.T) {
	o := newOIDC(t)
	f := serveSignIn(t, oidcConfig(o))
	b, tr := browser(t)
	fetch(t, b, f.srv.URL+"/portals/pets/")
	fetch(t, b, f.srv.URL+"/auth/sign-out")
	fetch(t, b, f.srv.URL+"/portals/pets/")
	// The browser holds an expired session, as one that another reader planted.
	planted := session{ID: "planted", Identity: Identity{Subject: "mallory"}, Expires: f.clock.now().Add(-time.Minute)}
	value, err := f.m.seal(sessionCookie, planted)
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(f.srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	b.Jar.SetCookies(u, []*http.Cookie{{Name: sessionCookie, Value: value, Path: "/"}})
	fetch(t, b, f.srv.URL+"/portals/pets/")

	var ids []string
	for _, c := range tr.sessionCookies() {
		if c.MaxAge >= 0 && c.Value != "" {
			ids = append(ids, opened(t, f.m, c).ID)
		}
	}
	if len(ids) != 3 || ids[0] == "" || ids[0] == ids[1] || ids[1] == ids[2] || ids[0] == ids[2] || slices.Contains(ids, "planted") {
		t.Errorf("three sign-ins issued the session IDs %q, want three new ones", ids)
	}
	if id := f.pages.last(); id.Subject == "mallory" {
		t.Error("the portal served the planted session's reader")
	}
}

func TestAFailedSignInShowsARetryLink(t *testing.T) {
	var failToken atomic.Bool
	o := newOIDC(t, func(o *mockoidc.MockOIDC) {
		o.AddMiddleware(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == mockoidc.TokenEndpoint && failToken.Load() {
					http.Error(w, `{"error":"server_error"}`, http.StatusInternalServerError)
					return
				}
				next.ServeHTTP(w, r)
			})
		})
	})
	f := serveSignIn(t, oidcConfig(o))
	for name, tc := range map[string]struct {
		callback func(attempt *http.Cookie, callback string) (*http.Cookie, string)
		retry    string
	}{
		"cancelled at the provider": {func(a *http.Cookie, cb string) (*http.Cookie, string) {
			u, _ := url.Parse(cb)
			return a, f.srv.URL + "/auth/callback?" + url.Values{"error": {"access_denied"}, "state": {u.Query().Get("state")}}.Encode()
		}, "/portals/pets/docs/"},
		"refused at the token endpoint": {func(a *http.Cookie, cb string) (*http.Cookie, string) {
			failToken.Store(true)
			return a, cb
		}, "/portals/pets/docs/"},
		"without its attempt": {func(_ *http.Cookie, cb string) (*http.Cookie, string) {
			return nil, cb
		}, "/"},
		"after its attempt expired": {func(a *http.Cookie, cb string) (*http.Cookie, string) {
			f.clock.advance(11 * time.Minute)
			return a, cb
		}, "/portals/pets/docs/"},
		"with another state": {func(a *http.Cookie, cb string) (*http.Cookie, string) {
			u, _ := url.Parse(cb)
			q := u.Query()
			q.Set("state", "another state")
			u.RawQuery = q.Encode()
			return a, u.String()
		}, "/portals/pets/docs/"},
	} {
		failToken.Store(false)
		attempt, callback := startAt(t, f, "/portals/pets/docs/")
		a, cb := tc.callback(attempt, callback)
		resp := callBack(t, f, cb, a)
		page, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusForbidden || !strings.Contains(string(page), `href="`+tc.retry+`"`) {
			t.Errorf("%s: the callback answered %d with %q, want the sign-in error page with a link to %s", name, resp.StatusCode, page, tc.retry)
		}
		if cc := resp.Header.Get("Cache-Control"); cc != "no-store" {
			t.Errorf("%s: the sign-in error page has Cache-Control %q, want no-store", name, cc)
		}
		for _, c := range resp.Cookies() {
			if c.Name == sessionCookie && c.MaxAge >= 0 {
				t.Errorf("%s: the failed sign-in set a session cookie", name)
			}
		}
		f.clock.offset.Store(0)
	}
	if n := f.pages.reached(); n != 0 {
		t.Errorf("%d requests reached the portal", n)
	}
}

func TestSignInChecksTheStateAndUsesPKCE(t *testing.T) {
	var mu sync.Mutex
	var authorize, token []url.Values
	o := newOIDC(t, func(o *mockoidc.MockOIDC) {
		o.AddMiddleware(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				switch r.URL.Path {
				case mockoidc.AuthorizationEndpoint:
					authorize = append(authorize, r.URL.Query())
				case mockoidc.TokenEndpoint:
					if err := r.ParseForm(); err == nil && r.PostForm.Get("client_id") != "" {
						token = append(token, r.PostForm)
					}
				}
				mu.Unlock()
				next.ServeHTTP(w, r)
			})
		})
	})
	f := serveSignIn(t, oidcConfig(o))
	b, _ := browser(t)
	fetch(t, b, f.srv.URL+"/portals/pets/")
	fetch(t, b, f.srv.URL+"/auth/sign-out")
	fetch(t, b, f.srv.URL+"/portals/pets/")
	mu.Lock()
	defer mu.Unlock()
	if len(authorize) != 2 || len(token) != 2 {
		t.Fatalf("two sign-ins sent %d authorization and %d token requests", len(authorize), len(token))
	}
	for i, q := range authorize {
		if q.Get("state") == "" || q.Get("nonce") == "" || q.Get("code_challenge_method") != "S256" || q.Get("code_challenge") != s256(token[i].Get("code_verifier")) {
			t.Errorf("sign-in %d: the authorization request %v and the code verifier %q", i, q, token[i].Get("code_verifier"))
		}
	}
	for _, p := range []string{"state", "nonce", "code_challenge"} {
		if authorize[0].Get(p) == authorize[1].Get(p) {
			t.Errorf("two sign-ins sent the same %s", p)
		}
	}
	if f.pages.reached() != 2 {
		t.Errorf("%d requests reached the portal, want 2", f.pages.reached())
	}
}

// tamperIDToken returns a middleware of the mock provider that changes the
// subject of each ID token from its token endpoint, without a new
// signature.
func tamperIDToken(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != mockoidc.TokenEndpoint {
			next.ServeHTTP(w, r)
			return
		}
		rec := httptest.NewRecorder()
		next.ServeHTTP(rec, r)
		var body map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err == nil {
			if raw, ok := body["id_token"].(string); ok {
				parts := strings.Split(raw, ".")
				if payload, err := base64.RawURLEncoding.DecodeString(parts[1]); err == nil && len(parts) == 3 {
					payload = bytes.Replace(payload, []byte(`"sub":"ada"`), []byte(`"sub":"eve"`), 1)
					parts[1] = base64.RawURLEncoding.EncodeToString(payload)
					body["id_token"] = strings.Join(parts, ".")
				}
			}
		}
		out, _ := json.Marshal(body)
		for k, v := range rec.Header() {
			w.Header()[k] = v
		}
		w.Header().Del("Content-Length")
		w.WriteHeader(rec.Code)
		w.Write(out)
	})
}

func TestSignInValidatesTheIDToken(t *testing.T) {
	for name, tc := range map[string]struct {
		setup func(*mockoidc.MockOIDC)
		alter func(*mockoidc.IDTokenClaims)
		ok    bool
	}{
		"sound":                  {ok: true},
		"with another signature": {setup: func(o *mockoidc.MockOIDC) { o.AddMiddleware(tamperIDToken) }},
		"of another issuer":      {alter: func(c *mockoidc.IDTokenClaims) { c.Issuer = "https://another.example/" }},
		"for another audience":   {alter: func(c *mockoidc.IDTokenClaims) { c.Audience = []string{"another client"} }},
		"with an expiry in the past": {alter: func(c *mockoidc.IDTokenClaims) {
			c.ExpiresAt = jwt.NewNumericDate(time.Now().Add(-time.Minute))
		}},
		"with another nonce": {alter: func(c *mockoidc.IDTokenClaims) { c.Nonce = "another nonce" }},
	} {
		var setup []func(*mockoidc.MockOIDC)
		if tc.setup != nil {
			setup = append(setup, tc.setup)
		}
		o := newOIDC(t, setup...)
		queue(o, tc.alter)
		f := serveSignIn(t, oidcConfig(o))
		b, _ := browser(t)
		resp, _ := fetch(t, b, f.srv.URL+"/portals/pets/")
		if signedIn := resp.StatusCode == http.StatusOK && f.pages.reached() == 1; signedIn != tc.ok {
			t.Errorf("an ID token %s: the sign-in ended with %d, and the portal saw %s", name, resp.StatusCode, jsonOf(f.pages.last()))
		}
	}
}

func TestASessionLastsTheConfiguredLifetime(t *testing.T) {
	for lifetime, want := range map[time.Duration]time.Duration{0: 8 * time.Hour, 2 * time.Hour: 2 * time.Hour} {
		cfg := oidcConfig(newOIDC(t))
		cfg.Lifetime = lifetime
		f := serveSignIn(t, cfg)
		b, tr := browser(t)
		before := time.Now()
		fetch(t, b, f.srv.URL+"/portals/pets/")
		after := time.Now()
		cookies := tr.sessionCookies()
		if len(cookies) != 1 {
			t.Fatalf("lifetime %v: %d session cookies, want one", lifetime, len(cookies))
		}
		if cookies[0].MaxAge != int(want/time.Second) {
			t.Errorf("lifetime %v: the session cookie lasts %d seconds, want %v", lifetime, cookies[0].MaxAge, want)
		}
		s := opened(t, f.m, cookies[0])
		if s.Expires.Before(before.Add(want)) || s.Expires.After(after.Add(want)) {
			t.Errorf("lifetime %v: the session expires at %v, want %v after the sign-in", lifetime, s.Expires, want)
		}
		f.clock.advance(want + time.Second)
		if resp := f.requestWith(t, "/portals/pets/", cookies[0]); resp.StatusCode != http.StatusFound {
			t.Errorf("lifetime %v: a session past it got %d, want a sign-in", lifetime, resp.StatusCode)
		}
	}
}

func TestSignInThroughAProviderWithoutOpenIDConnect(t *testing.T) {
	p := newStubProvider(t)
	f := serveSignIn(t, stubConfig(p))
	b, _ := browser(t)
	resp, body := fetch(t, b, f.srv.URL+"/portals/pets/docs/")
	if got := resp.Request.URL.RequestURI(); got != "/portals/pets/docs/" || body != `the page of "ada"` {
		t.Fatalf("the sign-in ended at %s with %q, want the first page as ada", got, body)
	}
	if id := f.pages.last(); id.Name != "Ada Lovelace" || !slices.Equal(id.Claims.Strings("orgs"), []string{"analytical-engines"}) {
		t.Errorf("the portal got the identity %s", jsonOf(id))
	}
	q, form := p.lastAuthorize(), p.lastExchange()
	if q.Get("state") == "" || q.Get("code_challenge_method") != "S256" || q.Get("code_challenge") != s256(form.Get("code_verifier")) {
		t.Errorf("the authorization request %v and the code verifier %q", q, form.Get("code_verifier"))
	}

	for name, change := range map[string]func(){
		"an error":                      func() { p.err = errors.New("the user API is down") },
		"an identity without a subject": func() { p.identity = Identity{Name: "No One"} },
	} {
		p.mu.Lock()
		change()
		p.mu.Unlock()
		b, _ := browser(t)
		reached := f.pages.reached()
		if resp, _ := fetch(t, b, f.srv.URL+"/portals/pets/"); resp.StatusCode != http.StatusForbidden || f.pages.reached() != reached {
			t.Errorf("with %s, the sign-in ended with %d", name, resp.StatusCode)
		}
		p.mu.Lock()
		p.err, p.identity = nil, Identity{Subject: "ada"}
		p.mu.Unlock()
	}
}

func TestTheClientSecretStaysOnTheServer(t *testing.T) {
	var logs syncBuffer
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })

	// A sign-in that succeeds.
	o := newOIDC(t)
	f := serveSignIn(t, oidcConfig(o))
	b, tr := browser(t)
	if resp, _ := fetch(t, b, f.srv.URL+"/portals/pets/"); resp.StatusCode != http.StatusOK {
		t.Fatalf("the sign-in ended with %d", resp.StatusCode)
	}
	// A sign-in whose provider refuses the secret, and repeats it in its answer.
	const secret = "the portal's client secret"
	refusing := newOIDC(t, func(o *mockoidc.MockOIDC) { o.ClientSecret = "another secret" })
	cfg := oidcConfig(refusing)
	cfg.ClientSecret = secret
	g := serveSignIn(t, cfg)
	b2, tr2 := browser(t)
	if resp, _ := fetch(t, b2, g.srv.URL+"/portals/pets/"); resp.StatusCode != http.StatusForbidden {
		t.Errorf("the refused sign-in ended with %d", resp.StatusCode)
	}

	seen := tr.text() + tr2.text()
	for _, s := range []string{o.ClientSecret, secret} {
		if strings.Contains(logs.String(), s) || strings.Contains(seen, s) {
			t.Errorf("the client secret %q is in the log %q or in what the browser saw", s, logs.String())
		}
	}
	if !strings.Contains(logs.String(), "sign-in") {
		t.Errorf("the refused sign-in left no line in the log: %q", logs.String())
	}
}

func TestWithOpenID(t *testing.T) {
	for _, tc := range []struct{ scopes, want []string }{
		{nil, []string{"openid", "profile", "email"}},
		{[]string{"email"}, []string{"openid", "email"}},
		{[]string{"profile", "openid", "groups"}, []string{"openid", "profile", "groups"}},
	} {
		if got := withOpenID(tc.scopes); !slices.Equal(got, tc.want) {
			t.Errorf("withOpenID(%q) = %q, want %q", tc.scopes, got, tc.want)
		}
	}
}

func TestNewRefusesAConfigurationThatCannotSignIn(t *testing.T) {
	o := newOIDC(t)
	const secret = "the portal's client secret"
	good := Config{Issuer: o.Issuer(), ClientID: o.ClientID, ClientSecret: secret, CallbackURL: "https://docs.example/auth/callback", Key: testKey}
	if _, err := New(t.Context(), good, http.NotFoundHandler()); err != nil {
		t.Fatalf("a sound configuration: %v", err)
	}
	for name, change := range map[string]func(*Config){
		"an issuer and a provider":            func(c *Config) { c.Provider = newStubProvider(t) },
		"neither an issuer nor a provider":    func(c *Config) { c.Issuer = "" },
		"no client ID":                        func(c *Config) { c.ClientID = "" },
		"no client secret":                    func(c *Config) { c.ClientSecret = "" },
		"a relative callback URL":             func(c *Config) { c.CallbackURL = "/auth/callback" },
		"a callback URL that is not http":     func(c *Config) { c.CallbackURL = "ftp://docs.example/auth/callback" },
		"a callback URL without a path":       func(c *Config) { c.CallbackURL = "https://docs.example" },
		"a callback URL at the root":          func(c *Config) { c.CallbackURL = "https://docs.example/" },
		"a callback URL at the sign-out path": func(c *Config) { c.CallbackURL = "https://docs.example/auth/sign-out" },
		"a sign-out path that is not a path":  func(c *Config) { c.SignOutPath = "sign-out" },
		"a logout URL with a script":          func(c *Config) { c.LogoutURL = "javascript:alert(1)" },
		"a protocol-relative logout URL":      func(c *Config) { c.LogoutURL = "//evil.example/" },
		"a negative lifetime":                 func(c *Config) { c.Lifetime = -time.Hour },
		"a short key":                         func(c *Config) { c.Key = []byte("thirty-one bytes, one too short") },
		"an issuer that does not answer":      func(c *Config) { c.Issuer = "http://127.0.0.1:1/" },
		"an issuer that names another":        func(c *Config) { c.Issuer = o.Issuer() + "/" },
		"a sign-out path with an escape":      func(c *Config) { c.SignOutPath = "/auth/sign%2Dout" },
		"a sign-out path at the root":         func(c *Config) { c.SignOutPath = "/" },
		"a sign-out path with a dot segment":  func(c *Config) { c.SignOutPath = "/auth/./sign-out" },
		"a callback URL with an escape":       func(c *Config) { c.CallbackURL = "https://docs.example/auth/call%2Dback" },
		"a callback URL with a space":         func(c *Config) { c.CallbackURL = "https://docs.example/auth/call%20back" },
		"a callback URL with a dot segment":   func(c *Config) { c.CallbackURL = "https://docs.example/auth/./callback" },
		"a logout path at the sign-out path":  func(c *Config) { c.LogoutURL = "/auth/sign-out" },
		"a logout URL at the sign-out path":   func(c *Config) { c.LogoutURL = "https://docs.example/auth/sign-out" },
		"a provider without endpoints": func(c *Config) {
			c.Issuer, c.Provider = "", endpointProvider{newStubProvider(t), oauth2.Endpoint{}}
		},
		"a provider with a relative authorization URL": func(c *Config) {
			c.Issuer, c.Provider = "", endpointProvider{newStubProvider(t), oauth2.Endpoint{AuthURL: "/authorize", TokenURL: "https://idp.example/token"}}
		},
		"a provider with a token URL that is not http": func(c *Config) {
			c.Issuer, c.Provider = "", endpointProvider{newStubProvider(t), oauth2.Endpoint{AuthURL: "https://idp.example/authorize", TokenURL: "ftp://idp.example/token"}}
		},
		"a provider without a token URL": func(c *Config) {
			c.Issuer, c.Provider = "", endpointProvider{newStubProvider(t), oauth2.Endpoint{AuthURL: "https://idp.example/authorize"}}
		},
		"a provider with an authorization URL without a host": func(c *Config) {
			c.Issuer, c.Provider = "", endpointProvider{newStubProvider(t), oauth2.Endpoint{AuthURL: "https:///authorize", TokenURL: "https://idp.example/token"}}
		},
		"an issuer that names no endpoints": func(c *Config) { c.Issuer = issuerWithoutEndpoints(t) },
	} {
		cfg := good
		change(&cfg)
		_, err := New(t.Context(), cfg, http.NotFoundHandler())
		if err == nil {
			t.Errorf("%s: no error", name)
		} else if strings.Contains(err.Error(), secret) {
			t.Errorf("%s: the error %q names the client secret", name, err)
		}
	}
	if _, err := New(t.Context(), good, nil); err == nil {
		t.Error("no handler to wrap: no error")
	}
	// A route under another route's path works.
	under := good
	under.SignOutPath = "/auth/callback/out"
	if _, err := New(t.Context(), under, http.NotFoundHandler()); err != nil {
		t.Errorf("a sign-out path under the callback's: %v", err)
	}
}

func TestRequestsThatCannotFollowARedirectAreRefused(t *testing.T) {
	f := serveSignIn(t, oidcConfig(newOIDC(t)))
	post, err := http.NewRequest(http.MethodPost, f.srv.URL+"/portals/pets/", strings.NewReader("q=1"))
	if err != nil {
		t.Fatal(err)
	}
	upgrade, err := http.NewRequest(http.MethodGet, f.srv.URL+"/live/websocket", nil)
	if err != nil {
		t.Fatal(err)
	}
	upgrade.Header.Set("Connection", "Upgrade")
	upgrade.Header.Set("Upgrade", "websocket")
	for name, r := range map[string]*http.Request{"a POST": post, "a socket's upgrade": upgrade} {
		resp, err := stopAtRedirects(http.DefaultClient).Do(r)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusForbidden || resp.Header.Get("Location") != "" || len(resp.Cookies()) != 0 {
			t.Errorf("%s without a session got %d to %q with the cookies %q, want 403 alone",
				name, resp.StatusCode, resp.Header.Get("Location"), resp.Header.Values("Set-Cookie"))
		}
	}
	if f.pages.reached() != 0 {
		t.Errorf("%d requests reached the portal", f.pages.reached())
	}
}

// syncBuffer is a log buffer with a lock for its writers and its reader.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func TestReturnTarget(t *testing.T) {
	for target, want := range map[string]string{
		"/":                      "/",
		"/portals/pets/?q=1":     "/portals/pets/?q=1",
		"/portals/pets/a%2Fb":    "/portals/pets/a%2Fb",
		"":                       "/",
		"portals/pets/":          "/",
		"//evil.example/x":       "/",
		"/\\evil.example/x":      "/",
		"/portals\\pets":         "/",
		"https://evil.example/x": "/",
		"http:/evil.example":     "/",
		"/\t/evil.example":       "/",
		"/a\nb":                  "/",
		"/a\x7fb":                "/",
		"/%zz":                   "/",
	} {
		if got := returnTarget(target); got != want {
			t.Errorf("returnTarget(%q) = %q, want %q", target, got, want)
		}
	}
}

func TestRedactHidesTheClientSecret(t *testing.T) {
	m := &middleware{oauth: &oauth2.Config{ClientSecret: "s3cret &/+"}}
	got := m.redact("plain s3cret &/+, escaped " + url.QueryEscape("s3cret &/+"))
	if strings.Contains(got, "s3cret") || strings.Count(got, "[client secret]") != 2 {
		t.Errorf("redact left %q", got)
	}
}

func TestAnIdentityTooLargeForItsCookieFailsTheSignIn(t *testing.T) {
	p := newStubProvider(t)
	p.identity.Claims = Claims{"notes": strings.Repeat("a long claim ", 400)}
	f := serveSignIn(t, stubConfig(p))
	b, tr := browser(t)
	if resp, _ := fetch(t, b, f.srv.URL+"/portals/pets/"); resp.StatusCode != http.StatusForbidden || f.pages.reached() != 0 {
		t.Errorf("an identity of 5 kB ended its sign-in with %d", resp.StatusCode)
	}
	if cookies := tr.sessionCookies(); len(cookies) != 0 {
		t.Errorf("the failed sign-in set %d session cookies", len(cookies))
	}
}

func TestATokenResponseWithoutAnIDTokenFailsTheSignIn(t *testing.T) {
	o := newOIDC(t, func(o *mockoidc.MockOIDC) {
		o.AddMiddleware(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != mockoidc.TokenEndpoint {
					next.ServeHTTP(w, r)
					return
				}
				rec := httptest.NewRecorder()
				next.ServeHTTP(rec, r)
				var body map[string]any
				json.Unmarshal(rec.Body.Bytes(), &body)
				delete(body, "id_token")
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(rec.Code)
				json.NewEncoder(w).Encode(body)
			})
		})
	})
	f := serveSignIn(t, oidcConfig(o))
	b, _ := browser(t)
	if resp, _ := fetch(t, b, f.srv.URL+"/portals/pets/"); resp.StatusCode != http.StatusForbidden || f.pages.reached() != 0 {
		t.Errorf("a token response without an ID token ended the sign-in with %d", resp.StatusCode)
	}
}

func TestTheClientSecretStaysOutOfTheLogInAnyEscaping(t *testing.T) {
	var logs syncBuffer
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	const secret = `a "quoted" back\slash secret & <more>`
	quoted := strconv.Quote(secret)
	inJSON, _ := json.Marshal(secret)
	forms := []string{secret, url.QueryEscape(secret), quoted[1 : len(quoted)-1], string(inJSON[1 : len(inJSON)-1])}

	// A provider that repeats the refused secret in its error's description.
	refusing := newOIDC(t, func(o *mockoidc.MockOIDC) { o.ClientSecret = "another secret" })
	cfg := oidcConfig(refusing)
	cfg.ClientSecret = secret
	b, _ := browser(t)
	if resp, _ := fetch(t, b, serveSignIn(t, cfg).srv.URL+"/portals/pets/"); resp.StatusCode != http.StatusForbidden {
		t.Errorf("the refused sign-in ended with %d", resp.StatusCode)
	}
	// Token endpoints that repeat it in a body without an error field, and
	// in the error field.
	for _, field := range []string{"message", "error"} {
		p := newStubProvider(t)
		p.echoSecret = field
		cfg := stubConfig(p)
		cfg.ClientSecret = secret
		b, _ := browser(t)
		if resp, _ := fetch(t, b, serveSignIn(t, cfg).srv.URL+"/portals/pets/"); resp.StatusCode != http.StatusForbidden {
			t.Errorf("the sign-in refused with the secret in the field %s ended with %d", field, resp.StatusCode)
		}
	}
	for _, form := range forms {
		if strings.Contains(logs.String(), form) {
			t.Errorf("the log holds the client secret as %q: %q", form, logs.String())
		}
	}
	if strings.Contains(logs.String(), "slash") {
		t.Errorf("the log holds a part of the client secret: %q", logs.String())
	}
	if strings.Count(logs.String(), "sign-in") < 3 {
		t.Errorf("the refused sign-ins left too few lines in the log: %q", logs.String())
	}
}

func TestAnOverlongFirstPageStillSignsIn(t *testing.T) {
	f := serveSignIn(t, oidcConfig(newOIDC(t)))
	b, _ := browser(t)
	resp, body := fetch(t, b, f.srv.URL+"/"+strings.Repeat("a", 3021))
	if resp.StatusCode != http.StatusOK || body != `the page of "1234567890"` || resp.Request.URL.RequestURI() != "/" {
		t.Errorf("a sign-in that started at a page of 3 kB ended with %d at %s", resp.StatusCode, resp.Request.URL.RequestURI())
	}
}

func TestALongFirstRequestKeepsTheAttemptCookieSmall(t *testing.T) {
	f := serveSignIn(t, oidcConfig(newOIDC(t)))
	target := "/portals/pets/?q=" + strings.Repeat("a&", 1000)
	attempt, callback := startAt(t, f, target)
	if n := len(attempt.Name) + len(attempt.Value); n > 4000 {
		t.Errorf("the attempt cookie of a target of %d bytes holds %d bytes, more than a browser keeps", len(target), n)
	}
	if resp := callBack(t, f, callback, attempt); resp.StatusCode != http.StatusFound || resp.Header.Get("Location") != "/" {
		t.Errorf("the sign-in ended with %d at %q, want /", resp.StatusCode, resp.Header.Get("Location"))
	}
}

// endpointProvider is a Provider with the endpoint e.
type endpointProvider struct {
	Provider
	e oauth2.Endpoint
}

func (p endpointProvider) Endpoint() oauth2.Endpoint { return p.e }

// issuerWithoutEndpoints returns the issuer URL of a server whose discovery
// document names neither an authorization nor a token endpoint.
func issuerWithoutEndpoints(t *testing.T) string {
	t.Helper()
	var issuer string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"issuer": issuer, "jwks_uri": issuer + "/keys"})
	}))
	t.Cleanup(srv.Close)
	issuer = srv.URL
	return issuer
}
