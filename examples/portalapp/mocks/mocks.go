// Package mocks holds the stand-ins of the example application's tests and
// of its local run: mockoidc in Auth0's place, a stub of GitHub, and a stub
// of the Anthropic Messages API. None of them checks a signature or keeps
// anything private, so they serve tests and a developer's machine alone.
package mocks

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/golang-jwt/jwt/v5"
	"github.com/oauth2-proxy/mockoidc"
)

// RolesClaim is the namespaced claim of a reader's roles in Auth0's ID
// token, as the README's post-login Action adds it.
const RolesClaim = "https://docs.example.com/roles"

// The example application's client at the mock Auth0 and at the stub
// GitHub, without spaces, so that a shell reads the settings of Serve.
const (
	ClientID     = "portalapp"
	ClientSecret = "the-client-secret-of-the-mocks"
)

// Auth0Reader is a reader of the mock Auth0.
type Auth0Reader struct {
	Subject, Name, Email string
	Roles                []string // in the claim RolesClaim
}

// GitHubReader is a reader of the stub GitHub.
type GitHubReader struct {
	ID                 int64
	Login, Name, Email string
	Orgs               []string // organization logins, as GitHub spells them
	Teams              []string // each org/team-slug
}

// The demo readers: Ada with the role staff at Auth0, and Grace in the
// organization Acme and its team partners at GitHub.
var (
	Ada   = Auth0Reader{Subject: "auth0|ada", Name: "Ada Lovelace", Email: "ada@example.com", Roles: []string{"staff"}}
	Grace = GitHubReader{ID: 2, Login: "grace", Name: "Grace Hopper", Email: "grace@example.com", Orgs: []string{"Acme"}, Teams: []string{"Acme/partners"}}
)

// StartAuth0 starts the mock Auth0 on ln: mockoidc with the client
// ClientID, which authorizes reader at each authorization request. The
// caller stops it with Shutdown.
func StartAuth0(ln net.Listener, reader Auth0Reader) (*mockoidc.MockOIDC, error) {
	m, err := mockoidc.NewServer(nil)
	if err != nil {
		return nil, err
	}
	m.ClientID, m.ClientSecret = ClientID, ClientSecret
	// mockoidc pops a reader at each authorization, else its default reader.
	if err := m.AddMiddleware(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == mockoidc.AuthorizationEndpoint {
				m.QueueUser(auth0User(reader))
			}
			next.ServeHTTP(w, r)
		})
	}); err != nil {
		return nil, err
	}
	if err := m.Start(ln, nil); err != nil {
		return nil, err
	}
	return m, nil
}

// auth0User is an Auth0Reader as a reader of mockoidc.
type auth0User Auth0Reader

func (u auth0User) ID() string { return u.Subject }

func (u auth0User) Userinfo([]string) ([]byte, error) {
	return json.Marshal(map[string]string{"sub": u.Subject, "name": u.Name, "email": u.Email})
}

func (u auth0User) Claims(_ []string, base *mockoidc.IDTokenClaims) (jwt.Claims, error) {
	return auth0Claims{IDTokenClaims: base, Name: u.Name, Email: u.Email, Roles: u.Roles}, nil
}

// auth0Claims are the claims of a mock Auth0 reader's ID token. The roles'
// tag holds RolesClaim.
type auth0Claims struct {
	*mockoidc.IDTokenClaims
	Name  string   `json:"name,omitempty"`
	Email string   `json:"email,omitempty"`
	Roles []string `json:"https://docs.example.com/roles,omitempty"`
}

// GitHub returns the stub GitHub for reader, its OAuth app's routes and its
// API on one host. /login/oauth/authorize authorizes reader at once, and
// /login/oauth/access_token checks the client, the code and the PKCE
// verifier and gives an access token with the authorization's scopes;
// /user, /user/orgs and /user/teams check the access token, and serve one
// entry of a list on each page, with a Link header to the next page. As all
// of the reader's memberships are private, a token without the scope
// read:org lists no organization and no team.
func GitHub(reader GitHubReader) http.Handler {
	g := &stubGitHub{reader: reader, codes: map[string]grant{}, tokens: map[string]string{}}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /login/oauth/authorize", g.authorize)
	mux.HandleFunc("POST /login/oauth/access_token", g.token)
	mux.HandleFunc("GET /user", g.user)
	mux.HandleFunc("GET /user/orgs", g.orgs)
	mux.HandleFunc("GET /user/teams", g.teams)
	return mux
}

// stubGitHub is the stub GitHub as a Go value.
type stubGitHub struct {
	reader GitHubReader

	mu     sync.Mutex
	codes  map[string]grant  // each code not yet exchanged
	tokens map[string]string // the scopes of each access token given
}

// grant is what an authorization gives a code: the PKCE challenge, and the
// scopes.
type grant struct {
	challenge, scope string
}

// authorize authorizes the reader at once and sends the browser back to the
// redirect URI with a code and the state.
func (g *stubGitHub) authorize(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	back, err := url.Parse(q.Get("redirect_uri"))
	if q.Get("client_id") != ClientID || q.Get("code_challenge_method") != "S256" || q.Get("code_challenge") == "" ||
		err != nil || (back.Scheme != "http" && back.Scheme != "https") {
		http.Error(w, "The authorization request is incomplete.", http.StatusBadRequest)
		return
	}
	code := rand.Text()
	g.mu.Lock()
	g.codes[code] = grant{challenge: q.Get("code_challenge"), scope: q.Get("scope")}
	g.mu.Unlock()
	values := back.Query()
	values.Set("code", code)
	values.Set("state", q.Get("state"))
	back.RawQuery = values.Encode()
	http.Redirect(w, r, back.String(), http.StatusFound)
}

// token exchanges a code once for an access token, for the example's client
// with the code's PKCE verifier.
func (g *stubGitHub) token(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	id, secret, ok := r.BasicAuth()
	if !ok {
		id, secret = r.PostForm.Get("client_id"), r.PostForm.Get("client_secret")
	}
	code := r.PostForm.Get("code")
	g.mu.Lock()
	granted, known := g.codes[code]
	delete(g.codes, code)
	g.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	switch {
	case id != ClientID || secret != ClientSecret:
		w.WriteHeader(http.StatusUnauthorized)
		io.WriteString(w, `{"error":"incorrect_client_credentials"}`)
	case !known || s256(r.PostForm.Get("code_verifier")) != granted.challenge:
		w.WriteHeader(http.StatusBadRequest)
		io.WriteString(w, `{"error":"bad_verification_code"}`)
	default:
		token := "gho_" + rand.Text()
		g.mu.Lock()
		g.tokens[token] = granted.scope
		g.mu.Unlock()
		json.NewEncoder(w).Encode(map[string]string{"access_token": token, "token_type": "bearer", "scope": granted.scope})
	}
}

// s256 returns PKCE's S256 challenge of verifier.
func s256(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// authorized reports whether r carries an access token of the stub, and
// whether the token has the scope read:org.
func (g *stubGitHub) authorized(r *http.Request) (ok, readOrg bool) {
	token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	g.mu.Lock()
	defer g.mu.Unlock()
	scope, known := g.tokens[token]
	return ok && known, slices.Contains(strings.Fields(strings.ReplaceAll(scope, ",", " ")), "read:org")
}

// user serves the reader, as GET /user of GitHub's API does.
func (g *stubGitHub) user(w http.ResponseWriter, r *http.Request) {
	if ok, _ := g.authorized(r); !ok {
		badCredentials(w)
		return
	}
	writeJSON(w, map[string]any{"id": g.reader.ID, "login": g.reader.Login, "name": g.reader.Name, "email": g.reader.Email})
}

// orgs serves the reader's organizations, as GET /user/orgs does.
func (g *stubGitHub) orgs(w http.ResponseWriter, r *http.Request) {
	var list []any
	for _, org := range g.reader.Orgs {
		list = append(list, map[string]any{"login": org})
	}
	g.page(w, r, list)
}

// teams serves the reader's teams, as GET /user/teams does.
func (g *stubGitHub) teams(w http.ResponseWriter, r *http.Request) {
	var list []any
	for _, team := range g.reader.Teams {
		org, slug, _ := strings.Cut(team, "/")
		list = append(list, map[string]any{"slug": slug, "organization": map[string]any{"login": org}})
	}
	g.page(w, r, list)
}

// page serves the entry of list on the page of r's query, from 1, with a
// Link header to the next page when there is one, or an empty list for a
// token without the scope read:org.
func (g *stubGitHub) page(w http.ResponseWriter, r *http.Request, list []any) {
	ok, readOrg := g.authorized(r)
	if !ok {
		badCredentials(w)
		return
	}
	if !readOrg {
		list = nil
	}
	n, err := strconv.Atoi(r.URL.Query().Get("page"))
	if err != nil || n < 1 {
		n = 1
	}
	entries := []any{}
	if n <= len(list) {
		entries = list[n-1 : n]
	}
	if n < len(list) {
		next := url.URL{Scheme: "http", Host: r.Host, Path: r.URL.Path, RawQuery: "page=" + strconv.Itoa(n+1)}
		w.Header().Set("Link", fmt.Sprintf(`<%s>; rel="next"`, next.String()))
	}
	writeJSON(w, entries)
}

// badCredentials answers as GitHub's API does to a missing or unknown
// access token.
func badCredentials(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	io.WriteString(w, `{"message":"Bad credentials"}`)
}

// writeJSON writes v as JSON with the status 200.
func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

// Model returns a stub of the Anthropic Messages API, which answers its nth
// request with the text "Answer n.".
func Model() http.Handler {
	var n atomic.Int64
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/messages" {
			http.NotFound(w, r)
			return
		}
		io.Copy(io.Discard, r.Body)
		i := n.Add(1)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"id":"msg_%d","type":"message","role":"assistant","model":"claude-opus-5-5","content":[{"type":"text","text":"Answer %d."}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`, i, i)
	})
}

// Serve runs the mock Auth0 for Ada and the stub GitHub for Grace on
// auth0Addr and githubAddr until the end of ctx, and writes the example
// application's settings for them to out, a line NAME=value each.
func Serve(ctx context.Context, auth0Addr, githubAddr string, out io.Writer) error {
	auth0Listener, err := net.Listen("tcp", auth0Addr)
	if err != nil {
		return err
	}
	githubListener, err := net.Listen("tcp", githubAddr)
	if err != nil {
		auth0Listener.Close()
		return err
	}
	auth0, err := StartAuth0(auth0Listener, Ada)
	if err != nil {
		auth0Listener.Close()
		githubListener.Close()
		return err
	}
	defer auth0.Shutdown()
	github := &http.Server{Handler: GitHub(Grace)}
	go github.Serve(githubListener)
	defer github.Shutdown(context.Background())
	githubURL := "http://" + githubListener.Addr().String()
	if _, err := fmt.Fprintf(out, "AUTH0_ISSUER=%s\nAUTH0_CLIENT_ID=%s\nAUTH0_CLIENT_SECRET=%s\nGITHUB_CLIENT_ID=%s\nGITHUB_CLIENT_SECRET=%s\nGITHUB_URL=%s\nGITHUB_API_URL=%s\n",
		auth0.Issuer(), ClientID, ClientSecret, ClientID, ClientSecret, githubURL, githubURL); err != nil {
		return err
	}
	<-ctx.Done()
	return nil
}
