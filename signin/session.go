package signin

import (
	"context"
	"crypto/cipher"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"time"
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
// sends the reader to the logout URL or the signed-out page.
func (m *middleware) signOut(w http.ResponseWriter, r *http.Request) {
	// HOLE(1): revoke the ID of sessionOf's session until the session's expiry, forget the revoked sessions past theirs, delete the session cookie, and send the reader to the logout URL or the signed-out page, not stored
	http.SetCookie(w, &http.Cookie{Name: m.sessionName, Path: "/", MaxAge: -1})
	io.WriteString(w, "You have signed out.")
}

// sessionOf reads the reader's session from the session cookie of r: none
// for a cookie that is missing, tampered with, expired or revoked.
func (m *middleware) sessionOf(r *http.Request) (session, bool) {
	// HOLE(1): refuse a session cookie that is tampered with, expired or revoked
	c, err := r.Cookie(m.sessionName)
	if err != nil {
		return session{}, false
	}
	var s session
	if err := m.open(m.sessionName, c.Value, &s); err != nil {
		return session{}, false
	}
	return s, true
}

// serve serves r with the identity of the session s in its context,
// privately, so that the request hooks read it.
func (m *middleware) serve(w http.ResponseWriter, r *http.Request, s session) {
	// HOLE(1): mark the response private, whatever the wrapped handler does with Cache-Control
	m.next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), readerKey{}, signedIn{identity: s.Identity, signOut: m.signOutPath})))
}

// cookieCipher returns the AES-256-GCM cipher that seals the cookies of the
// portal at the callback URL callback, under a key derived from key and
// callback with HKDF-SHA-256, so that one portal's cookies open in no other.
func cookieCipher(key []byte, callback string) (cipher.AEAD, error) {
	// HOLE(1): derive a 32-byte key from key and callback with HKDF-SHA-256, and return its AES-256-GCM cipher
	return nil, nil
}

// seal returns v sealed as the value of the cookie name: encrypted and
// authenticated with the middleware's cipher and bound to the name, so that
// the browser can neither read nor change it, nor pass it off as another
// cookie's value.
func (m *middleware) seal(name string, v any) (string, error) {
	// HOLE(1): encrypt and authenticate v's JSON with m's cipher and a random nonce, with name as the additional data
	b, err := json.Marshal(v)
	return base64.RawURLEncoding.EncodeToString(b), err
}

// open reads into v the value of the cookie name that seal made, or refuses
// a value that seal did not make for that name with this cipher.
func (m *middleware) open(name, value string, v any) error {
	// HOLE(1): refuse a value tampered with, sealed for another cookie's name or under another key
	b, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}
