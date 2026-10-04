package signin

import (
	"cmp"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

// attempt is one sign-in from its start to its callback, as the attempt
// cookie keeps it.
type attempt struct {
	State    string    `json:"state"`
	Nonce    string    `json:"nonce"`
	Verifier string    `json:"verifier"` // the PKCE code verifier
	Return   string    `json:"return"`   // the return target
	Expires  time.Time `json:"expires"`
}

// attemptLifetime is how long a sign-in attempt lasts.
const attemptLifetime = 10 * time.Minute

// maxCookie bounds a cookie's name and value together, below the 4096 bytes
// that browsers keep.
const maxCookie = 4000

// start starts a sign-in: it keeps a new state, nonce and PKCE verifier with
// the return target in the attempt cookie, and sends the reader to the
// identity provider, or refuses a request that cannot follow the redirect.
func (m *middleware) start(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if (r.Method != http.MethodGet && r.Method != http.MethodHead) || r.Header.Get("Upgrade") != "" {
		http.Error(w, "Sign in first.", http.StatusForbidden)
		return
	}
	a := attempt{
		State:    rand.Text(),
		Nonce:    rand.Text(),
		Verifier: oauth2.GenerateVerifier(),
		Return:   returnTarget(r.URL.RequestURI()),
		Expires:  m.now().Add(attemptLifetime),
	}
	value, err := m.seal(m.attemptName, a)
	if err != nil {
		http.Error(w, "The sign-in could not start.", http.StatusInternalServerError)
		return
	}
	http.SetCookie(w, m.cookie(m.attemptName, value, m.callbackPath, int(attemptLifetime/time.Second)))
	opts := []oauth2.AuthCodeOption{oauth2.S256ChallengeOption(a.Verifier)}
	if m.verifier != nil {
		opts = append(opts, oidc.Nonce(a.Nonce))
	}
	if m.audience != "" {
		opts = append(opts, oauth2.SetAuthURLParam("audience", m.audience))
	}
	http.Redirect(w, r, m.oauth.AuthCodeURL(a.State, opts...), http.StatusFound)
}

// returnTarget returns target, the request target of the sign-in's start,
// when it is a relative path on the portal's own origin, such as
// /portals/pets/?q=1, and / for any other: an absolute or protocol-relative
// URL, a backslash, a control character or a target that does not parse.
func returnTarget(target string) string {
	if localPath(target) {
		return target
	}
	return "/"
}

// localPath reports whether s is a path, with an optional query, on the
// origin of the page that holds it: one leading slash, and no backslash, no
// control character, no scheme and no host.
func localPath(s string) bool {
	if !strings.HasPrefix(s, "/") || strings.HasPrefix(s, "//") ||
		strings.ContainsFunc(s, func(r rune) bool { return r == '\\' || r < ' ' || r == 0x7f }) {
		return false
	}
	u, err := url.Parse(s)
	return err == nil && u.Scheme == "" && u.Host == "" && u.User == nil
}

// finish finishes the sign-in at the callback: it checks the attempt's
// state, exchanges the code with the attempt's PKCE verifier, identifies the
// reader from the verified ID token or through the Provider, and sets the
// cookie of a new session and sends the reader to the return target, or
// shows the sign-in error page with a link to retry.
func (m *middleware) finish(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	http.SetCookie(w, m.cookie(m.attemptName, "", m.callbackPath, -1))
	a, err := m.attemptOf(r)
	if err != nil {
		m.fail(w, cmp.Or(a.Return, "/"), err)
		return
	}
	if e := r.FormValue("error"); e != "" {
		m.fail(w, a.Return, fmt.Errorf("the identity provider answered %q", e))
		return
	}
	ctx := context.WithValue(r.Context(), oauth2.HTTPClient, m.client)
	token, err := m.oauth.Exchange(ctx, r.FormValue("code"), oauth2.VerifierOption(a.Verifier))
	if err != nil {
		m.fail(w, a.Return, fmt.Errorf("exchange the code: %w", err))
		return
	}
	id, err := m.identify(ctx, token, a.Nonce)
	if err != nil {
		m.fail(w, a.Return, err)
		return
	}
	value, err := m.seal(m.sessionName, session{ID: rand.Text(), Identity: id, Expires: m.now().Add(m.lifetime)})
	if err == nil && len(m.sessionName)+len(value) > maxCookie {
		err = errors.New("the identity is too large for the session cookie")
	}
	if err != nil {
		m.fail(w, a.Return, err)
		return
	}
	http.SetCookie(w, m.cookie(m.sessionName, value, "/", int(m.lifetime/time.Second)))
	http.Redirect(w, r, a.Return, http.StatusFound)
}

// attemptOf returns the sign-in attempt of the callback r, from its attempt
// cookie, or an error for a cookie that is missing, not sealed by this
// middleware or expired, or of another state than the callback's. With a
// cookie that opens, it returns the attempt with the error.
func (m *middleware) attemptOf(r *http.Request) (attempt, error) {
	c, err := r.Cookie(m.attemptName)
	if err != nil {
		return attempt{}, errors.New("the callback came without its attempt cookie")
	}
	var a attempt
	if err := m.open(m.attemptName, c.Value, &a); err != nil {
		return attempt{}, fmt.Errorf("open the attempt cookie: %w", err)
	}
	a.Return = returnTarget(a.Return)
	if !a.Expires.After(m.now()) {
		return a, errors.New("the sign-in attempt expired")
	}
	if subtle.ConstantTimeCompare([]byte(r.FormValue("state")), []byte(a.State)) != 1 {
		return a, errors.New("the callback's state is not the attempt's")
	}
	return a, nil
}

// identify returns the identity of the reader to whom the provider gave
// token: from the ID token, whose signature, issuer, audience and expiry the
// verifier checks, with the attempt's nonce, or through the Provider. An
// identity without a subject fails.
func (m *middleware) identify(ctx context.Context, token *oauth2.Token, nonce string) (Identity, error) {
	var id Identity
	if m.provider != nil {
		var err error
		if id, err = m.provider.Identity(ctx, token); err != nil {
			return Identity{}, fmt.Errorf("the provider named no reader: %w", err)
		}
	} else {
		raw, ok := token.Extra("id_token").(string)
		if !ok {
			return Identity{}, errors.New("the identity provider returned no ID token")
		}
		idToken, err := m.verifier.Verify(ctx, raw)
		if err != nil {
			return Identity{}, fmt.Errorf("verify the ID token: %w", err)
		}
		if subtle.ConstantTimeCompare([]byte(idToken.Nonce), []byte(nonce)) != 1 {
			return Identity{}, errors.New("the ID token's nonce is not the attempt's")
		}
		var claims Claims
		if err := idToken.Claims(&claims); err != nil {
			return Identity{}, fmt.Errorf("read the ID token's claims: %w", err)
		}
		id = Identity{Subject: idToken.Subject, Name: claimText(claims, "name", "preferred_username"), Email: claimText(claims, "email"), Claims: claims}
	}
	if id.Subject == "" {
		return Identity{}, errors.New("the identity has no subject")
	}
	return id, nil
}

// claimText returns the first of the claims names that holds a non-empty
// string, or "".
func claimText(claims Claims, names ...string) string {
	for _, name := range names {
		if s, ok := claims[name].(string); ok && s != "" {
			return s
		}
	}
	return ""
}

// fail shows the sign-in error page with a link to retry at retry, and
// writes the cause to the log without the client secret.
func (m *middleware) fail(w http.ResponseWriter, retry string, cause error) {
	log.Print(m.redact("sign-in: " + cause.Error()))
	page{
		Title:    "Sign-in failed",
		Message:  "The sign-in did not finish, so the portal stays closed to you.",
		Link:     retry,
		LinkText: "Try again",
	}.write(w, http.StatusForbidden)
}

// redact returns s without the client secret, plain or query-escaped.
func (m *middleware) redact(s string) string {
	secret := m.oauth.ClientSecret
	if secret == "" {
		return s
	}
	return strings.NewReplacer(secret, "[client secret]", url.QueryEscape(secret), "[client secret]").Replace(s)
}
