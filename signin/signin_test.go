package signin

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/oauth2"
)

// testKey is the session key of the tests.
var testKey = []byte("the session key of the tests, 32 bytes or more")

// served is the handler that the middleware wraps in the tests: it records
// the identity of each request that reaches it, runs then if set, and
// answers with the reader's subject.
type served struct {
	mu         sync.Mutex
	identities []Identity
	then       func(http.ResponseWriter, *http.Request)
}

func (s *served) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	id, _ := IdentityOf(r)
	s.mu.Lock()
	s.identities = append(s.identities, id)
	then := s.then
	s.mu.Unlock()
	if then != nil {
		then(w, r)
	}
	fmt.Fprintf(w, "the page of %q", id.Subject)
}

// reached returns the number of requests that reached the handler.
func (s *served) reached() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.identities)
}

// last returns the identity of the last request that reached the handler.
func (s *served) last() Identity {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.identities) == 0 {
		return Identity{}
	}
	return s.identities[len(s.identities)-1]
}

// clock is the middleware's clock in the tests: the time now, plus the
// test's advances.
type clock struct{ offset atomic.Int64 }

func (c *clock) now() time.Time          { return time.Now().Add(time.Duration(c.offset.Load())) }
func (c *clock) advance(d time.Duration) { c.offset.Add(int64(d)) }

// fixture is the sign-in middleware of a test, served with its wrapped
// handler and its clock.
type fixture struct {
	m     *middleware
	srv   *httptest.Server
	pages *served
	clock *clock
}

// serveSignIn serves the sign-in middleware of cfg around a served handler,
// with the callback URL at /auth/callback of the server's own origin unless
// cfg names one, and the test key unless cfg has a key.
func serveSignIn(t *testing.T, cfg Config) *fixture {
	t.Helper()
	srv := httptest.NewUnstartedServer(nil)
	if cfg.CallbackURL == "" {
		cfg.CallbackURL = "http://" + srv.Listener.Addr().String() + "/auth/callback"
	}
	if cfg.Key == nil {
		cfg.Key = testKey
	}
	f := &fixture{srv: srv, pages: &served{}, clock: &clock{}}
	h, err := New(t.Context(), cfg, f.pages)
	if err != nil {
		srv.Close()
		t.Fatal(err)
	}
	f.m = h.(*middleware)
	f.m.now = f.clock.now
	srv.Config.Handler = h
	srv.Start()
	t.Cleanup(srv.Close)
	return f
}

// transcript records a browser's requests and responses, with each
// response's body, on any server.
type transcript struct {
	mu        sync.Mutex
	urls      []string
	responses []*http.Response
	bodies    []string
}

func (tr *transcript) RoundTrip(r *http.Request) (*http.Response, error) {
	resp, err := http.DefaultTransport.RoundTrip(r)
	if err != nil {
		return nil, err
	}
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		return nil, err
	}
	resp.Body = io.NopCloser(bytes.NewReader(body))
	tr.mu.Lock()
	tr.urls = append(tr.urls, r.URL.String())
	tr.responses = append(tr.responses, resp)
	tr.bodies = append(tr.bodies, string(body))
	tr.mu.Unlock()
	return resp, nil
}

// text returns the whole transcript as text: each URL, and each response's
// status, headers and body.
func (tr *transcript) text() string {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	var b strings.Builder
	for i, resp := range tr.responses {
		fmt.Fprintf(&b, "%s\n%s\n", tr.urls[i], resp.Status)
		resp.Header.Write(&b)
		b.WriteString(tr.bodies[i] + "\n")
	}
	return b.String()
}

// sessionCookies returns the session cookies of the responses, in order,
// deletions included.
func (tr *transcript) sessionCookies() []*http.Cookie {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	var cookies []*http.Cookie
	for _, resp := range tr.responses {
		for _, c := range resp.Cookies() {
			if c.Name == sessionCookie {
				cookies = append(cookies, c)
			}
		}
	}
	return cookies
}

// browser returns a client that keeps cookies and follows redirects, like a
// reader's browser, and the transcript of its exchanges.
func browser(t *testing.T) (*http.Client, *transcript) {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	tr := &transcript{}
	return &http.Client{Jar: jar, Transport: tr}, tr
}

// stopAtRedirects returns a copy of c that does not follow redirects.
func stopAtRedirects(c *http.Client) *http.Client {
	d := *c
	d.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &d
}

// fetch sends a GET for rawURL with c, and returns the last response and its
// body.
func fetch(t *testing.T, c *http.Client, rawURL string) (*http.Response, string) {
	t.Helper()
	resp, err := c.Get(rawURL)
	if err != nil {
		t.Fatalf("GET %s: %v", rawURL, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp, string(body)
}

// requestWith sends a GET for the path of f's server with the cookie c, and
// returns the response without following a redirect.
func (f *fixture) requestWith(t *testing.T, path string, c *http.Cookie) *http.Response {
	t.Helper()
	r, err := http.NewRequest(http.MethodGet, f.srv.URL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if c != nil {
		r.AddCookie(c)
	}
	resp, err := stopAtRedirects(http.DefaultClient).Do(r)
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	return resp
}

// sessionFor returns a session cookie of the middleware m for id, which
// expires at expires.
func sessionFor(t *testing.T, m *middleware, id Identity, expires time.Time) *http.Cookie {
	t.Helper()
	value, err := m.seal(sessionCookie, session{ID: fmt.Sprintf("session of %s at %d", id.Subject, time.Now().UnixNano()), Identity: id, Expires: expires})
	if err != nil {
		t.Fatal(err)
	}
	return &http.Cookie{Name: sessionCookie, Value: value}
}

// opened returns the session in the cookie c of the middleware m.
func opened(t *testing.T, m *middleware, c *http.Cookie) session {
	t.Helper()
	var s session
	if err := m.open(sessionCookie, c.Value, &s); err != nil {
		t.Fatalf("open the session cookie: %v", err)
	}
	return s
}

// s256 returns the S256 code challenge of a PKCE verifier.
func s256(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// stubProvider is an OAuth 2.0 provider without OpenID Connect, like
// GitHub: its server authorizes every reader at once, its token endpoint
// checks the client and the PKCE verifier and gives one access token, and
// its Identity names the token's reader.
type stubProvider struct {
	srv *httptest.Server

	mu        sync.Mutex
	authorize url.Values // the query of the last authorization request
	exchange  url.Values // the form of the last token request
	identity  Identity   // what Identity returns for the access token
	err       error      // what Identity returns, if not nil
}

func newStubProvider(t *testing.T) *stubProvider {
	t.Helper()
	p := &stubProvider{identity: Identity{
		Subject: "ada",
		Name:    "Ada Lovelace",
		Email:   "ada@example.com",
		Claims:  Claims{"login": "ada", "orgs": []any{"analytical-engines"}},
	}}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /authorize", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		p.mu.Lock()
		p.authorize = q
		p.mu.Unlock()
		back, err := url.Parse(q.Get("redirect_uri"))
		if err != nil {
			http.Error(w, "bad redirect_uri", http.StatusBadRequest)
			return
		}
		back.RawQuery = url.Values{"code": {"the code"}, "state": {q.Get("state")}}.Encode()
		http.Redirect(w, r, back.String(), http.StatusFound)
	})
	mux.HandleFunc("POST /token", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		p.mu.Lock()
		p.exchange = r.PostForm
		challenge := p.authorize.Get("code_challenge")
		p.mu.Unlock()
		id, secret, ok := r.BasicAuth()
		if !ok {
			id, secret = r.PostForm.Get("client_id"), r.PostForm.Get("client_secret")
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case id != "portal" || secret != "the client secret":
			w.WriteHeader(http.StatusUnauthorized)
			io.WriteString(w, `{"error":"invalid_client"}`)
		case r.PostForm.Get("code") != "the code", challenge != "" && s256(r.PostForm.Get("code_verifier")) != challenge:
			w.WriteHeader(http.StatusBadRequest)
			io.WriteString(w, `{"error":"invalid_grant"}`)
		default:
			io.WriteString(w, `{"access_token":"the access token","token_type":"bearer"}`)
		}
	})
	p.srv = httptest.NewServer(mux)
	t.Cleanup(p.srv.Close)
	return p
}

func (p *stubProvider) Endpoint() oauth2.Endpoint {
	return oauth2.Endpoint{AuthURL: p.srv.URL + "/authorize", TokenURL: p.srv.URL + "/token"}
}

func (p *stubProvider) Identity(_ context.Context, token *oauth2.Token) (Identity, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.err != nil {
		return Identity{}, p.err
	}
	if token.AccessToken != "the access token" {
		return Identity{}, fmt.Errorf("no reader has the access token %q", token.AccessToken)
	}
	return p.identity, nil
}

// lastAuthorize returns the query of the last authorization request.
func (p *stubProvider) lastAuthorize() url.Values {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.authorize
}

// lastExchange returns the form of the last token request.
func (p *stubProvider) lastExchange() url.Values {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.exchange
}

// stubConfig returns a configuration of the sign-in through p.
func stubConfig(p *stubProvider) Config {
	return Config{Provider: p, ClientID: "portal", ClientSecret: "the client secret"}
}

// TestAReaderSignsInAndOut is the smoke test of process 10.2: a reader asks
// for a page, passes the sign-in at a provider without OpenID Connect, which
// authorizes every reader at once, gets the page as the signed-in reader,
// ends the session at the sign-out path, and meets the sign-in again at the
// next page.
func TestAReaderSignsInAndOut(t *testing.T) {
	p := newStubProvider(t)
	f := serveSignIn(t, stubConfig(p))
	b, _ := browser(t)

	resp, body := fetch(t, b, f.srv.URL+"/portals/pets/docs/?q=1")
	if got := resp.Request.URL.RequestURI(); got != "/portals/pets/docs/?q=1" || body != `the page of "ada"` {
		t.Fatalf("the sign-in ended at %s with %q, want the first page as ada", got, body)
	}
	if resp, _ := fetch(t, b, f.srv.URL+"/auth/sign-out"); resp.StatusCode != http.StatusOK {
		t.Errorf("the sign-out answered %d", resp.StatusCode)
	}
	resp, _ = fetch(t, stopAtRedirects(b), f.srv.URL+"/portals/pets/docs/")
	if loc := resp.Header.Get("Location"); resp.StatusCode != http.StatusFound || !strings.HasPrefix(loc, p.srv.URL+"/authorize?") {
		t.Errorf("after the sign-out, a page answered %d for %s, want a sign-in at the provider", resp.StatusCode, loc)
	}
	if n := f.pages.reached(); n != 1 {
		t.Errorf("%d requests reached the portal, want the first page alone", n)
	}
}

// jsonOf returns v as JSON, for messages.
func jsonOf(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}
