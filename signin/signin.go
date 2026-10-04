// Package signin signs the readers of a portal in through an identity
// provider. It stays outside the portal's core packages, which import
// neither it nor any OAuth or OpenID Connect library. The API is not stable
// yet.
//
// New wraps the portal handler in the sign-in middleware: it sends a reader
// without a session to the identity provider, finishes the sign-in at the
// callback URL, keeps the reader's identity and claims in a sealed session
// cookie, and puts them into the context of each of the reader's requests,
// where the request hooks read them with IdentityOf. AccountLinks and
// ReaderID are ready hooks for portal.Config.Account and chat.Config.Reader.
//
// An OpenID Connect provider, such as Auth0, Okta, Google, Keycloak or
// Microsoft Entra ID, needs only its issuer in the Config. A provider without
// OpenID Connect, such as GitHub, plugs in through a Provider, which turns
// the provider's tokens into the reader's identity.
package signin

import (
	"cmp"
	"context"
	"crypto/cipher"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"

	"github.com/bilus/documentation-portal/portal"
)

// Config configures the sign-in middleware for one identity provider: an
// OpenID Connect provider by its Issuer, or an OAuth 2.0 provider without
// OpenID Connect through a Provider. Its values come from the deployment,
// never from the portal's configuration file.
type Config struct {
	// Issuer is the issuer URL of an OpenID Connect provider, such as
	// https://TENANT.auth0.com/, at which New discovers the provider's
	// endpoints. Empty with a Provider.
	Issuer string
	// Provider is an OAuth 2.0 provider without OpenID Connect, such as
	// GitHub. Nil with an Issuer.
	Provider Provider

	ClientID     string
	ClientSecret string // the middleware writes it to no log and no response

	// CallbackURL is the portal's URL to which the provider sends the reader
	// back, as registered at the provider, such as
	// https://docs.example.com/auth/callback. The middleware answers its
	// path, and sets its cookies Secure when it is an https URL.
	CallbackURL string
	// LogoutURL is where sign-out sends the reader, such as the provider's
	// logout endpoint, which ends the provider's session too: an http or
	// https URL, or a path. Empty for the middleware's signed-out page.
	LogoutURL string
	// SignOutPath is the middleware's sign-out route; empty means
	// /auth/sign-out.
	SignOutPath string

	// Scopes are the scopes of the authorization request. Nil means openid,
	// profile and email for an Issuer, which always gets openid.
	Scopes []string
	// Audience, if not empty, is the authorization request's audience
	// parameter, as Auth0 takes it for its APIs.
	Audience string

	// Lifetime is how long a session lasts after its sign-in; 0 means 8
	// hours.
	Lifetime time.Duration
	// Key seals the session cookie and the attempt cookie: 32 bytes or more
	// of secret, the same for every replica of the portal.
	Key []byte
}

// Provider is an OAuth 2.0 provider without OpenID Connect, such as GitHub,
// which issues no ID token. The middleware runs the authorization code flow
// with its endpoints, with state and PKCE, and asks it for the identity of
// the reader behind the flow's tokens.
type Provider interface {
	// Endpoint returns the provider's authorization and token URLs.
	Endpoint() oauth2.Endpoint
	// Identity returns the identity of the reader to whom the provider gave
	// token, such as from the provider's user API, or an error, which fails
	// the sign-in. An identity without a subject fails it too.
	Identity(ctx context.Context, token *oauth2.Token) (Identity, error)
}

// Identity is a signed-in reader as the identity provider names them.
type Identity struct {
	Subject string // the provider's stable ID of the reader, never empty
	Name    string // the name claim, or else preferred_username; may be empty
	Email   string // the email claim as the provider gives it, verified or not
	Claims  Claims // the ID token's claims, or those that the Provider gives
}

// Claims are the identity provider's statements about a reader, such as
// groups or roles, as JSON values.
type Claims map[string]any

// Strings returns the claim name as a list of strings: the string elements
// of a list, or a single string as a list of one, and nil for a claim that
// holds neither.
func (c Claims) Strings(name string) []string {
	switch v := c[name].(type) {
	case string:
		return []string{v}
	case []string:
		return slices.Clone(v)
	case []any:
		var s []string
		for _, e := range v {
			if e, ok := e.(string); ok {
				s = append(s, e)
			}
		}
		return s
	}
	return nil
}

// New builds the sign-in middleware around next: it checks cfg, and
// discovers an OpenID Connect provider's endpoints at its issuer, or refuses
// a configuration that cannot sign in a reader.
func New(ctx context.Context, cfg Config, next http.Handler) (http.Handler, error) {
	if err := cfg.check(); err != nil {
		return nil, err
	}
	if next == nil {
		return nil, errors.New("signin: no handler to wrap")
	}
	aead, err := cookieCipher(cfg.Key, cfg.CallbackURL)
	if err != nil {
		return nil, err
	}
	callback, _ := url.Parse(cfg.CallbackURL) // check parsed it
	m := &middleware{
		next:         next,
		oauth:        &oauth2.Config{ClientID: cfg.ClientID, ClientSecret: cfg.ClientSecret, RedirectURL: cfg.CallbackURL, Scopes: cfg.Scopes},
		provider:     cfg.Provider,
		audience:     cfg.Audience,
		callbackPath: callback.Path,
		signOutPath:  cmp.Or(cfg.SignOutPath, defaultSignOutPath),
		sessionName:  sessionCookie,
		attemptName:  attemptCookie,
		logoutURL:    cfg.LogoutURL,
		secure:       callback.Scheme == "https",
		lifetime:     cmp.Or(cfg.Lifetime, 8*time.Hour),
		aead:         aead,
		client:       &http.Client{Timeout: 30 * time.Second},
		now:          time.Now,
		revoked:      map[string]time.Time{},
	}
	if m.secure {
		// Prefixes that keep the cookies to this origin over https.
		m.sessionName, m.attemptName = "__Host-"+sessionCookie, "__Secure-"+attemptCookie
	}
	if cfg.Provider != nil {
		m.oauth.Endpoint = cfg.Provider.Endpoint()
		return m, nil
	}
	provider, err := oidc.NewProvider(oidc.ClientContext(ctx, m.client), cfg.Issuer)
	if err != nil {
		return nil, fmt.Errorf("signin: discover the issuer %s: %w", cfg.Issuer, err)
	}
	m.oauth.Endpoint = provider.Endpoint()
	m.oauth.Scopes = withOpenID(cfg.Scopes)
	m.verifier = provider.Verifier(&oidc.Config{ClientID: cfg.ClientID})
	return m, nil
}

// defaultSignOutPath is the sign-out path of a Config without one.
const defaultSignOutPath = "/auth/sign-out"

// check refuses a configuration that cannot sign in a reader, with an error
// that names the setting and never the client secret's value.
func (cfg Config) check() error {
	switch {
	case (cfg.Issuer == "") == (cfg.Provider == nil):
		return errors.New("signin: give either an issuer or a provider")
	case cfg.ClientID == "":
		return errors.New("signin: the client ID is empty")
	case cfg.ClientSecret == "":
		return errors.New("signin: the client secret is empty")
	case cfg.Lifetime < 0:
		return fmt.Errorf("signin: the session lifetime %v is negative", cfg.Lifetime)
	case len(cfg.Key) < 32:
		return errors.New("signin: the session key is shorter than 32 bytes")
	}
	signOut := cmp.Or(cfg.SignOutPath, defaultSignOutPath)
	if !localPath(signOut) || strings.ContainsAny(signOut, "?#") {
		return fmt.Errorf("signin: the sign-out path %q is not a path", signOut)
	}
	callback, err := url.Parse(cfg.CallbackURL)
	if err != nil || !webURL(callback) {
		return fmt.Errorf("signin: the callback URL %q is not an absolute http or https URL", cfg.CallbackURL)
	}
	if callback.Path == "" || callback.Path == "/" || callback.Path == signOut {
		return fmt.Errorf("signin: the callback URL %q needs a path of its own, other than / and the sign-out path", cfg.CallbackURL)
	}
	if cfg.LogoutURL != "" && !localPath(cfg.LogoutURL) {
		if u, err := url.Parse(cfg.LogoutURL); err != nil || !webURL(u) {
			return fmt.Errorf("signin: the logout URL %q is neither an http or https URL nor a path", cfg.LogoutURL)
		}
	}
	return nil
}

// webURL reports whether u is an absolute http or https URL with a host.
func webURL(u *url.URL) bool {
	return (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

// withOpenID returns scopes with openid first, or openid, profile and email
// for no scopes.
func withOpenID(scopes []string) []string {
	if len(scopes) == 0 {
		return []string{oidc.ScopeOpenID, "profile", "email"}
	}
	others := slices.DeleteFunc(slices.Clone(scopes), func(s string) bool { return s == oidc.ScopeOpenID })
	return append([]string{oidc.ScopeOpenID}, others...)
}

// middleware is the sign-in middleware as a Go value, built by New.
type middleware struct {
	next         http.Handler
	oauth        *oauth2.Config
	verifier     *oidc.IDTokenVerifier // of an Issuer's ID tokens; nil with a Provider
	provider     Provider              // nil with an Issuer
	audience     string                // the authorization request's audience, or none
	callbackPath string
	signOutPath  string
	sessionName  string        // the session cookie's name: __Host-signin_session over https
	attemptName  string        // the attempt cookie's name: __Secure-signin_attempt over https
	logoutURL    string        // where sign-out sends the reader, or none for the signed-out page
	secure       bool          // whether the cookies are Secure: with an https callback URL
	lifetime     time.Duration // of a session
	aead         cipher.AEAD   // seals the cookies
	client       *http.Client  // for the requests to the provider, with a timeout
	now          func() time.Time

	mu      sync.Mutex
	revoked map[string]time.Time // the revoked sessions: each ID until its session would expire
}

// ServeHTTP answers each request: it ends the session at the sign-out path,
// finishes a sign-in at the callback, serves a signed-in reader's request
// with the reader's identity, and starts a sign-in for any other request.
func (m *middleware) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case m.signOutPath:
		m.signOut(w, r)
	case m.callbackPath:
		m.finish(w, r)
	default:
		if s, ok := m.sessionOf(r); ok {
			m.serve(w, r, s)
		} else {
			m.start(w, r)
		}
	}
}

// IdentityOf returns the identity of r's reader, as the sign-in middleware
// that serves r put it into r's context, or false for a request that no
// sign-in middleware serves to a signed-in reader. The identity is a copy,
// so a change to it changes no other caller's.
func IdentityOf(r *http.Request) (Identity, bool) {
	s, ok := r.Context().Value(readerKey{}).(signedIn)
	if !ok {
		return Identity{}, false
	}
	id := s.identity
	if id.Claims != nil {
		id.Claims = cloneJSON(map[string]any(id.Claims)).(map[string]any)
	}
	return id, true
}

// cloneJSON returns a deep copy of v, a JSON value as encoding/json decodes
// it, or a list of strings.
func cloneJSON(v any) any {
	switch v := v.(type) {
	case map[string]any:
		c := make(map[string]any, len(v))
		for k, e := range v {
			c[k] = cloneJSON(e)
		}
		return c
	case []any:
		c := make([]any, len(v))
		for i, e := range v {
			c[i] = cloneJSON(e)
		}
		return c
	case []string:
		return slices.Clone(v)
	}
	return v
}

// AccountLinks returns the account links of r's reader, for
// portal.Config.Account: the reader's name, or else the email or the
// subject, and a link to the sign-out path, or none for a request that no
// sign-in middleware serves to a signed-in reader.
func AccountLinks(r *http.Request) []portal.AccountLink {
	s, ok := r.Context().Value(readerKey{}).(signedIn)
	if !ok {
		return nil
	}
	return []portal.AccountLink{
		{Label: cmp.Or(s.identity.Name, s.identity.Email, s.identity.Subject)},
		{Label: "Sign out", URL: s.signOut},
	}
}

// ReaderID returns the reader ID of r's reader, for chat.Config.Reader: the
// subject of the reader's identity, or "" for a request that no sign-in
// middleware serves to a signed-in reader.
func ReaderID(r *http.Request) string {
	s, _ := r.Context().Value(readerKey{}).(signedIn)
	return s.identity.Subject
}
