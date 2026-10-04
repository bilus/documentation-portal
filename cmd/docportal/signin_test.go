package main

import (
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/oauth2-proxy/mockoidc"

	"github.com/bilus/documentation-portal/signin"
)

// sessionKey is the session key of the tests.
const sessionKey = "the session key of the tests, 32 bytes or more"

// mockProvider starts a mock OpenID Connect provider, which signs every
// reader in at once as its default reader, jane.doe.
func mockProvider(t *testing.T) *mockoidc.MockOIDC {
	t.Helper()
	o, err := mockoidc.Run()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { o.Shutdown() })
	return o
}

// TestSignInKeepsTheReloaderBehindTheSignIn is the smoke test of process 10:
// with an issuer, a request without a session gets a redirect to the
// sign-in and never reaches the reloader; without one, the reloader answers
// every request itself.
func TestSignInKeepsTheReloaderBehindTheSignIn(t *testing.T) {
	o := mockProvider(t)
	reached := 0
	reloader := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { reached++ })
	h, err := signIn(t.Context(), signin.Config{
		Issuer:       o.Issuer(),
		ClientID:     o.ClientID,
		ClientSecret: o.ClientSecret,
		CallbackURL:  "http://docs.test/auth/callback",
		Key:          []byte(sessionKey),
	}, reloader)
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/portals/petstore/", nil))
	if rec.Code != http.StatusFound || reached != 0 {
		t.Errorf("a request without a session got %d and reached the reloader %d times, want a sign-in", rec.Code, reached)
	}
	if h, err := signIn(t.Context(), signin.Config{}, reloader); err != nil || h == nil {
		t.Fatalf("without an issuer: %v", err)
	} else {
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/portals/petstore/", nil))
		if reached != 1 {
			t.Errorf("without an issuer, the request reached the reloader %d times", reached)
		}
	}
}

// signInEnv is an environment with every sign-in setting.
var signInEnv = map[string]string{
	"DOCPORTAL_OIDC_ISSUER":        "https://tenant.auth0.com/",
	"DOCPORTAL_OIDC_CLIENT_ID":     "the client from the environment",
	"DOCPORTAL_OIDC_CLIENT_SECRET": "the client secret",
	"DOCPORTAL_OIDC_CALLBACK_URL":  "https://docs.example.com/auth/callback",
	"DOCPORTAL_OIDC_LOGOUT_URL":    "https://tenant.auth0.com/v2/logout?client_id=x",
	"DOCPORTAL_OIDC_SCOPES":        "openid profile email",
	"DOCPORTAL_OIDC_AUDIENCE":      "https://docs.example.com/api",
	"DOCPORTAL_SESSION_LIFETIME":   "2h",
	"DOCPORTAL_SESSION_KEY":        sessionKey,
}

func TestParseConfigReadsTheSignInSettings(t *testing.T) {
	t.Skip("HOLE(3): read the sign-in settings from their flags and the environment")
	env := func(k string) string { return signInEnv[k] }
	cfg, err := parseConfig(nil, env)
	if err != nil {
		t.Fatal(err)
	}
	want := signInConfig{
		Issuer:       "https://tenant.auth0.com/",
		ClientID:     "the client from the environment",
		ClientSecret: "the client secret",
		CallbackURL:  "https://docs.example.com/auth/callback",
		LogoutURL:    "https://tenant.auth0.com/v2/logout?client_id=x",
		Scopes:       "openid profile email",
		Audience:     "https://docs.example.com/api",
		Lifetime:     2 * time.Hour,
		Key:          sessionKey,
	}
	if cfg.SignIn != want {
		t.Errorf("from the environment: %+v, want %+v", cfg.SignIn, want)
	}
	cfg, err = parseConfig([]string{
		"-oidc-issuer", "https://login.example/",
		"-oidc-client-id", "the client from the flags",
		"-oidc-callback-url", "https://portal.example/auth/callback",
		"-oidc-logout-url", "https://login.example/logout",
		"-oidc-scopes", "openid,email,read:docs",
		"-oidc-audience", "https://portal.example/api",
		"-session-lifetime", "30m",
	}, env)
	if err != nil {
		t.Fatal(err)
	}
	want = signInConfig{
		Issuer:       "https://login.example/",
		ClientID:     "the client from the flags",
		ClientSecret: "the client secret",
		CallbackURL:  "https://portal.example/auth/callback",
		LogoutURL:    "https://login.example/logout",
		Scopes:       "openid,email,read:docs",
		Audience:     "https://portal.example/api",
		Lifetime:     30 * time.Minute,
		Key:          sessionKey,
	}
	if cfg.SignIn != want {
		t.Errorf("the flags over the environment: %+v, want %+v", cfg.SignIn, want)
	}
	for name, args := range map[string][]string{
		"a lifetime that is not a duration": {"-session-lifetime", "a day"},
		"a negative lifetime":               {"-session-lifetime", "-1h"},
	} {
		if _, err := parseConfig(args, env); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
	if _, err := parseConfig(nil, func(k string) string {
		if k == "DOCPORTAL_SESSION_LIFETIME" {
			return "soon"
		}
		return signInEnv[k]
	}); err == nil {
		t.Error("DOCPORTAL_SESSION_LIFETIME that is not a duration: no error")
	}
}

func TestSignInSettingsNeedAnIssuer(t *testing.T) {
	t.Skip("HOLE(3): refuse sign-in settings without an issuer, which would serve every reader without a sign-in")
	for _, args := range [][]string{
		{"-oidc-client-id", "portal"},
		{"-oidc-callback-url", "https://docs.example.com/auth/callback"},
		{"-oidc-logout-url", "https://docs.example.com/"},
		{"-oidc-scopes", "openid"},
		{"-oidc-audience", "api"},
		{"-session-lifetime", "1h"},
	} {
		if _, err := parseConfig(args, noEnv); err == nil || !strings.Contains(err.Error(), "-oidc-issuer") {
			t.Errorf("%v without an issuer: %v, want an error that names -oidc-issuer", args, err)
		}
	}
	for _, name := range []string{"DOCPORTAL_OIDC_CLIENT_SECRET", "DOCPORTAL_SESSION_KEY", "DOCPORTAL_OIDC_CLIENT_ID"} {
		env := func(k string) string {
			if k == name {
				return "set"
			}
			return ""
		}
		if _, err := parseConfig(nil, env); err == nil || !strings.Contains(err.Error(), "-oidc-issuer") {
			t.Errorf("%s without an issuer: %v, want an error that names -oidc-issuer", name, err)
		}
	}
}

func TestTheSignInSecretsComeFromTheEnvironmentAlone(t *testing.T) {
	t.Skip("HOLE(3): take the client secret and the session key from the environment alone, and print neither")
	for _, flag := range []string{"-oidc-client-secret", "-session-key"} {
		if _, err := parseConfig([]string{flag, "a secret of 32 bytes or more, from a flag"}, noEnv); err == nil {
			t.Errorf("%s was accepted", flag)
		}
	}
	env := func(k string) string { return signInEnv[k] }
	_, err := parseConfig([]string{"-h"}, env)
	if err == nil || !strings.Contains(err.Error(), "-oidc-issuer") {
		t.Fatalf("err = %v, want the usage with -oidc-issuer", err)
	}
	for _, secret := range []string{signInEnv["DOCPORTAL_OIDC_CLIENT_SECRET"], sessionKey} {
		if strings.Contains(err.Error(), secret) {
			t.Errorf("the usage shows the secret %q", secret)
		}
	}
	// A startup that cannot reach the issuer names neither secret.
	_, _, err = startup(t.Context(), []string{"-config", sample, "-oidc-issuer", "http://127.0.0.1:1/"}, env)
	if err == nil {
		t.Fatal("a startup with an unreachable issuer: no error")
	}
	for _, secret := range []string{signInEnv["DOCPORTAL_OIDC_CLIENT_SECRET"], sessionKey} {
		if strings.Contains(err.Error(), secret) {
			t.Errorf("the startup error %q shows the secret %q", err, secret)
		}
	}
}

func TestStartupSignsReadersIn(t *testing.T) {
	t.Skip("HOLE(3): sign each reader in through the issuer that -oidc-issuer names, with the reader's name and a sign-out link on every page")
	o := mockProvider(t)
	srv := httptest.NewUnstartedServer(nil)
	defer srv.Close()
	env := map[string]string{"DOCPORTAL_OIDC_CLIENT_SECRET": o.ClientSecret, "DOCPORTAL_SESSION_KEY": sessionKey}
	_, h, err := startup(t.Context(), []string{
		"-config", sample,
		"-chat-model", "claude-opus-5-5",
		"-oidc-issuer", o.Issuer(),
		"-oidc-client-id", o.ClientID,
		"-oidc-callback-url", "http://" + srv.Listener.Addr().String() + "/auth/callback",
	}, func(k string) string { return env[k] })
	if err != nil {
		t.Fatal(err)
	}
	srv.Config.Handler = h
	srv.Start()

	// Without a session, a page sends the reader to the provider.
	resp, err := (&http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}).Get(srv.URL + "/portals/petstore/docs/guides/")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if loc := resp.Header.Get("Location"); resp.StatusCode != http.StatusFound || !strings.HasPrefix(loc, o.AuthorizationEndpoint()+"?") {
		t.Errorf("a page without a session answered %d for %q, want the provider's authorization endpoint", resp.StatusCode, loc)
	}

	// The reader signs in, returns to the page and sees the account links.
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	b := &http.Client{Jar: jar}
	resp, err = b.Get(srv.URL + "/portals/petstore/docs/guides/")
	if err != nil {
		t.Fatal(err)
	}
	page, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	links := `<span class="portal-account"><span>jane.doe</span><span><a href="/auth/sign-out">Sign out</a></span></span>`
	if resp.Request.URL.Path != "/portals/petstore/docs/guides/" || !strings.Contains(string(page), links) {
		t.Errorf("the sign-in ended at %s with a page without the account links %s", resp.Request.URL, links)
	}
	if cc := resp.Header.Get("Cache-Control"); cc != "private" {
		t.Errorf("the signed-in reader's page has Cache-Control %q, want private", cc)
	}
	// The chat page is the signed-in reader's too.
	resp, err = b.Get(srv.URL + "/portals/petstore/chat")
	if err != nil {
		t.Fatal(err)
	}
	page, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(page), "jane.doe") || !strings.Contains(string(page), `href="/auth/sign-out"`) {
		t.Errorf("the chat page answered %d without the account links", resp.StatusCode)
	}
	// The sign-out link ends the session.
	resp, err = b.Get(srv.URL + "/auth/sign-out")
	if err != nil {
		t.Fatal(err)
	}
	page, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(page), "signed out") {
		t.Errorf("the sign-out answered %d with %q", resp.StatusCode, page)
	}
}
