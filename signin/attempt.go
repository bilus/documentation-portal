package signin

import (
	"net/http"
	"time"
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

// start starts a sign-in: it keeps a new state, nonce and PKCE verifier with
// the return target in the attempt cookie, and sends the reader to the
// identity provider, or refuses a request that cannot follow the redirect.
func (m *middleware) start(w http.ResponseWriter, r *http.Request) {
	// HOLE(2): keep a random state, nonce and PKCE verifier with the return target of r in the attempt cookie for ten minutes, send the reader to the authorization endpoint with the state, the nonce, the S256 challenge and the audience, not stored, and refuse a request other than GET and HEAD, or one that upgrades its connection
	http.Redirect(w, r, m.oauth.AuthCodeURL(r.URL.RequestURI()), http.StatusFound)
}

// finish finishes the sign-in at the callback: it checks the attempt's
// state, exchanges the code with the attempt's PKCE verifier, identifies the
// reader from the verified ID token or through the Provider, and sets the
// cookie of a new session that sends the reader to the return target, or
// shows the sign-in error page with a link to retry.
func (m *middleware) finish(w http.ResponseWriter, r *http.Request) {
	// HOLE(2): check the attempt cookie's state and expiry, exchange the code with its verifier, verify an ID token's signature, issuer, audience, expiry and nonce or ask the Provider, set a new session's cookie and send the reader to the return target, or show the sign-in error page with a retry link, writing no secret to the log or the page
	token, err := m.oauth.Exchange(r.Context(), r.FormValue("code"))
	if err != nil || m.provider == nil {
		http.Error(w, "The sign-in failed.", http.StatusForbidden)
		return
	}
	id, err := m.provider.Identity(r.Context(), token)
	if err != nil {
		http.Error(w, "The sign-in failed.", http.StatusForbidden)
		return
	}
	value, err := m.seal(m.sessionName, session{ID: "mock", Identity: id, Expires: m.now().Add(m.lifetime)})
	if err != nil {
		http.Error(w, "The sign-in failed.", http.StatusForbidden)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: m.sessionName, Value: value, Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode})
	http.Redirect(w, r, r.FormValue("state"), http.StatusFound)
}
