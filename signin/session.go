package signin

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"html/template"
	"net/http"
	"time"

	"github.com/bilus/documentation-portal/portal"
)

// The middleware's cookies.
const (
	sessionCookie = "signin_session"
	attemptCookie = "signin_attempt"
)

// session is one signed-in reader's session, as the session cookie keeps it.
type session struct {
	ID       string    `json:"id"` // random, new at each sign-in
	Identity Identity  `json:"identity"`
	Expires  time.Time `json:"expires"`
}

// readerKey keys the signed-in reader in a request's context.
type readerKey struct{}

// signedIn is the signed-in reader of a request, as the middleware puts it
// into the request's context: the identity, and the middleware's sign-out
// path for the reader's sign-out link.
type signedIn struct {
	identity Identity
	signOut  string
}

// signOut ends the reader's session: it revokes its session ID, which
// sessionOf reads from the session cookie, until the session's expiry,
// forgets the revoked sessions past theirs, deletes the session cookie, and
// sends the reader to the logout URL or the signed-out page. A sign-out
// that another site starts asks the reader first.
func (m *middleware) signOut(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		// Another site sent the reader here: the reader confirms first.
		page{Title: "Sign out?", Message: "Another site sent you here to sign out.", Link: m.signOutPath, LinkText: "Sign out"}.write(w, http.StatusOK)
		return
	}
	if s, ok := m.sessionOf(r); ok {
		m.revoke(s)
	}
	http.SetCookie(w, m.cookie(m.sessionName, "", "/", -1))
	w.Header().Set("Cache-Control", "no-store")
	if m.logoutURL != "" {
		http.Redirect(w, r, m.logoutURL, http.StatusFound)
		return
	}
	page{Title: "You have signed out", Message: "Your session has ended.", Link: "/", LinkText: "Sign in again"}.write(w, http.StatusOK)
}

// revoke revokes the session s until its expiry, and forgets the revoked
// sessions past theirs.
func (m *middleware) revoke(s session) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.now()
	for id, expires := range m.revoked {
		if !expires.After(now) {
			delete(m.revoked, id)
		}
	}
	m.revoked[s.ID] = s.Expires
}

// sessionOf reads the reader's session from the session cookie of r: none
// for a cookie that is missing, tampered with, expired or revoked.
func (m *middleware) sessionOf(r *http.Request) (session, bool) {
	c, err := r.Cookie(m.sessionName)
	if err != nil {
		return session{}, false
	}
	var s session
	if err := m.open(m.sessionName, c.Value, &s); err != nil || s.ID == "" || s.Identity.Subject == "" || !s.Expires.After(m.now()) {
		return session{}, false
	}
	m.mu.Lock()
	_, revoked := m.revoked[s.ID]
	m.mu.Unlock()
	if revoked {
		return session{}, false
	}
	return s, true
}

// serve serves r with the identity of the session s in its context,
// privately, so that the request hooks read it.
func (m *middleware) serve(w http.ResponseWriter, r *http.Request, s session) {
	ctx := context.WithValue(r.Context(), readerKey{}, signedIn{identity: s.Identity, signOut: m.signOutPath})
	portal.Private(m.next).ServeHTTP(w, r.WithContext(ctx))
}

// cookie returns the cookie name with value for path, which lasts maxAge
// seconds, or which a negative maxAge deletes: HttpOnly, SameSite=Lax, and
// Secure with an https callback URL.
func (m *middleware) cookie(name, value, path string, maxAge int) *http.Cookie {
	return &http.Cookie{Name: name, Value: value, Path: path, MaxAge: maxAge, HttpOnly: true, Secure: m.secure, SameSite: http.SameSiteLaxMode}
}

// cookieCipher returns the AES-256-GCM cipher that seals the cookies of the
// portal at the callback URL callback, under a key derived from key and
// callback with HKDF-SHA-256, so that one portal's cookies open in no other.
func cookieCipher(key []byte, callback string) (cipher.AEAD, error) {
	derived, err := hkdf.Key(sha256.New, key, nil, "signin cookies of "+callback, 32)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(derived)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// seal returns v sealed as the value of the cookie name: encrypted and
// authenticated with the middleware's cipher and bound to the name, so that
// the browser can neither read nor change it, nor pass it off as another
// cookie's value.
func (m *middleware) seal(name string, v any) (string, error) {
	plain, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, m.aead.NonceSize())
	rand.Read(nonce)
	return base64.RawURLEncoding.EncodeToString(m.aead.Seal(nonce, nonce, plain, []byte(name))), nil
}

// open reads into v the value of the cookie name that seal made, or refuses
// a value that seal did not make for that name with this cipher.
func (m *middleware) open(name, value string, v any) error {
	sealed, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return err
	}
	n := m.aead.NonceSize()
	if len(sealed) < n {
		return errors.New("signin: the cookie is too short")
	}
	plain, err := m.aead.Open(nil, sealed[:n], sealed[n:], []byte(name))
	if err != nil {
		return err
	}
	return json.Unmarshal(plain, v)
}

// page is one of the middleware's own pages: the signed-out page or the
// sign-in error page, with a link onward.
type page struct {
	Title, Message, Link, LinkText string
}

// pageTemplate lays out a page.
var pageTemplate = template.Must(template.New("page").Parse(`<!doctype html>
<html lang="en">
<head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><title>{{.Title}}</title></head>
<body><main><h1>{{.Title}}</h1><p>{{.Message}}</p><p><a href="{{.Link}}">{{.LinkText}}</a></p></main></body>
</html>
`))

// write writes p with the status code, for no cache to store.
func (p page) write(w http.ResponseWriter, code int) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	pageTemplate.Execute(w, p)
}
