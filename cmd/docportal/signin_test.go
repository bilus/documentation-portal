package main

import (
	"bytes"
	"io"
	"log"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/oauth2-proxy/mockoidc"

	"github.com/bilus/documentation-portal/portal"
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

// TestSignInKeepsTheHandlerBehindTheSignIn is the smoke test of process 12:
// with an issuer, a request without a session gets a redirect to the
// sign-in and never reaches the wrapped handler, in docportal the previews
// handler; without one, the handler answers every request itself.
func TestSignInKeepsTheHandlerBehindTheSignIn(t *testing.T) {
	o := mockProvider(t)
	reached := 0
	wrapped := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { reached++ })
	h, err := signIn(t.Context(), signin.Config{
		Issuer:       o.Issuer(),
		ClientID:     o.ClientID,
		ClientSecret: o.ClientSecret,
		CallbackURL:  "http://docs.test/auth/callback",
		Key:          []byte(sessionKey),
	}, wrapped)
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/portals/petstore/", nil))
	if rec.Code != http.StatusFound || reached != 0 {
		t.Errorf("a request without a session got %d and reached the wrapped handler %d times, want a sign-in", rec.Code, reached)
	}
	if h, err := signIn(t.Context(), signin.Config{}, wrapped); err != nil || h == nil {
		t.Fatalf("without an issuer: %v", err)
	} else {
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/portals/petstore/", nil))
		if reached != 1 {
			t.Errorf("without an issuer, the request reached the wrapped handler %d times", reached)
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

func TestStartupSignsAReaderIntoAPreview(t *testing.T) {
	dir, url := bucketFolder(t)
	writePreview(t, dir, "pr-1", "# The preview version\n")
	o := mockProvider(t)
	srv := httptest.NewUnstartedServer(nil)
	defer srv.Close()
	env := map[string]string{"DOCPORTAL_OIDC_CLIENT_SECRET": o.ClientSecret, "DOCPORTAL_SESSION_KEY": sessionKey}
	_, h, err := startup(t.Context(), []string{
		"-root", url, "-config", "environment.yaml", "-refresh", "0", "-previews", "previews",
		"-oidc-issuer", o.Issuer(),
		"-oidc-client-id", o.ClientID,
		"-oidc-callback-url", "http://" + srv.Listener.Addr().String() + "/auth/callback",
	}, func(k string) string { return env[k] })
	if err != nil {
		t.Fatal(err)
	}
	srv.Config.Handler = h
	srv.Start()

	// Without a session, a preview link sends the reader to the provider,
	// before any switch to the preview.
	resp, err := (&http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}).Get(srv.URL + "/previews/pr-1/portals/pets/docs/guides/a.md")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if loc := resp.Header.Get("Location"); resp.StatusCode != http.StatusFound || !strings.HasPrefix(loc, o.AuthorizationEndpoint()+"?") || slices.ContainsFunc(resp.Cookies(), func(c *http.Cookie) bool { return c.Name == "portal-preview" }) {
		t.Errorf("a preview link without a session answered %d for %q with the cookies %v, want the provider's authorization endpoint", resp.StatusCode, loc, resp.Cookies())
	}

	// The reader signs in, switches to the preview and sees the account links
	// on its page.
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	b := &http.Client{Jar: jar}
	resp, err = b.Get(srv.URL + "/previews/pr-1/portals/pets/docs/guides/a.md")
	if err != nil {
		t.Fatal(err)
	}
	page, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	links := `<span class="portal-account"><span>jane.doe</span><span><a href="/auth/sign-out">Sign out</a></span></span>`
	if resp.Request.URL.Path != "/portals/pets/docs/guides/a.md" || !strings.Contains(string(page), "The preview version") || !strings.Contains(string(page), `class="portal-banner"`) {
		t.Fatalf("the sign-in ended at %s with %q, want the preview's page", resp.Request.URL, page)
	}
	if !strings.Contains(string(page), links) {
		t.Errorf("the preview's page lacks the account links %s", links)
	}
}

func TestSignInConfigOf(t *testing.T) {
	if cfg := signInConfigOf(signInConfig{ClientID: "portal"}); cfg.Issuer != "" || cfg.ClientID != "" || cfg.Key != nil {
		t.Errorf("without an issuer, the sign-in configuration names the issuer %q and the client %q", cfg.Issuer, cfg.ClientID)
	}
	var logs strings.Builder
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	s := signInConfig{
		Issuer:       "https://tenant.auth0.com/",
		ClientID:     "portal",
		ClientSecret: "the client secret",
		CallbackURL:  "https://docs.example.com/auth/callback",
		LogoutURL:    "https://tenant.auth0.com/v2/logout",
		Scopes:       "openid, email read:docs",
		Audience:     "https://docs.example.com/api",
		Lifetime:     time.Hour,
	}
	a, b := signInConfigOf(s), signInConfigOf(s)
	if len(a.Key) != 32 || bytes.Equal(a.Key, b.Key) {
		t.Errorf("without a session key: keys of %d and %d bytes, equal %v; want two random keys of 32 bytes", len(a.Key), len(b.Key), bytes.Equal(a.Key, b.Key))
	}
	if strings.Count(logs.String(), "DOCPORTAL_SESSION_KEY") != 2 {
		t.Errorf("the log %q, want a notice for each random key", logs.String())
	}
	if a.Issuer != s.Issuer || a.ClientID != s.ClientID || a.ClientSecret != s.ClientSecret || a.CallbackURL != s.CallbackURL ||
		a.LogoutURL != s.LogoutURL || a.Audience != s.Audience || a.Lifetime != s.Lifetime || !slices.Equal(a.Scopes, []string{"openid", "email", "read:docs"}) {
		t.Errorf("the sign-in configuration does not carry the settings: scopes %q, lifetime %v", a.Scopes, a.Lifetime)
	}
	s.Key = sessionKey
	if c := signInConfigOf(s); string(c.Key) != sessionKey {
		t.Error("the sign-in configuration does not carry the session key")
	}
	// A short key goes on unchanged, and signin.New refuses it.
	s.Key = "short"
	if c := signInConfigOf(s); string(c.Key) != "short" {
		t.Error("the sign-in configuration replaced a short session key")
	}
}

func TestAddAccountNeedsAnIssuer(t *testing.T) {
	pcfg, err := portal.ReadConfig(os.DirFS("../../testdata"), "environment.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if got := addAccount(pcfg, ""); got.Account != nil {
		t.Error("without an issuer, the portal configuration has an account hook")
	}
	h, err := portal.New(addAccount(pcfg, "https://tenant.auth0.com/"))
	if err != nil {
		t.Fatal(err)
	}
	// Outside a sign-in middleware the hook names nobody, and the pages turn private.
	rec := get(h, "/portals/petstore/docs/guides/")
	if cc := rec.Header().Get("Cache-Control"); cc != "private" || strings.Contains(rec.Body.String(), "portal-account") {
		t.Errorf("with an issuer, a page outside the middleware has Cache-Control %q and account links %v", cc, strings.Contains(rec.Body.String(), "portal-account"))
	}
}

func TestAddChatTakesTheReaderHookWithAnIssuer(t *testing.T) {
	pcfg, err := portal.ReadConfig(os.DirFS("../../testdata"), "environment.yaml")
	if err != nil {
		t.Fatal(err)
	}
	// The chat's reader hook makes its pages private.
	for issuer, want := range map[string]string{"": "", "https://tenant.auth0.com/": "private"} {
		withChat, _, err := addChat(pcfg, "claude-opus-5-5", nil, issuer)
		if err != nil {
			t.Fatal(err)
		}
		h, err := portal.New(withChat)
		if err != nil {
			t.Fatal(err)
		}
		if got := get(h, "/portals/petstore/chat").Header().Get("Cache-Control"); got != want {
			t.Errorf("issuer %q: the chat page has Cache-Control %q, want %q", issuer, got, want)
		}
	}
}
