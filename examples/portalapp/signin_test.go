package main

import (
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/bilus/documentation-portal/portal"
	"github.com/bilus/documentation-portal/signin"
)

// serveSignIn starts the portal's server with the sign-in of the test's
// settings in front of next.
func (p *portalTest) serveSignIn(t *testing.T, next http.Handler) {
	t.Helper()
	s, err := readSettings(func(name string) string { return p.env[name] })
	if err != nil {
		t.Fatal(err)
	}
	h, err := signIn(t.Context(), s.providers, next)
	if err != nil {
		t.Fatal(err)
	}
	p.srv.Config.Handler = h
	p.srv.Start()
}

// echo answers each request with the provider name of its sign-in and the
// subject of its reader.
var echo = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	provider, _ := r.Context().Value(providerKey{}).(string)
	id, _ := signin.IdentityOf(r)
	fmt.Fprintf(w, "%s %s", provider, id.Subject)
})

// noRedirects is a client that follows no redirect.
var noRedirects = &http.Client{
	Timeout:       10 * time.Second,
	CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
}

// send sends a request of method to url with the form values form, if
// any, and the headers of header, through c.
func send(t *testing.T, c *http.Client, method, url string, form url.Values, header map[string]string) *http.Response {
	t.Helper()
	var body *strings.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	} else {
		body = strings.NewReader("")
	}
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		t.Fatal(err)
	}
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	for k, v := range header {
		req.Header.Set(k, v)
	}
	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}
	resp.Body.Close()
	return resp
}

// choiceCookie returns the choice cookie that resp sets, or nil.
func choiceCookie(resp *http.Response) *http.Cookie {
	for _, c := range resp.Cookies() {
		if c.Name == "signin_provider" {
			return c
		}
	}
	return nil
}

func TestTheSignInPageOffersTheConfiguredProviders(t *testing.T) {
	t.Skip("HOLE(3): offer the configured providers on the sign-in page")
	auth0 := []string{"AUTH0_ISSUER", "AUTH0_CLIENT_ID", "AUTH0_CLIENT_SECRET"}
	github := []string{"GITHUB_CLIENT_ID", "GITHUB_CLIENT_SECRET", "GITHUB_URL", "GITHUB_API_URL"}
	for name, tc := range map[string]struct {
		drop      []string
		want, not []string
	}{
		"both providers": {nil, []string{"Sign in with Auth0", "Sign in with GitHub"}, nil},
		"Auth0 alone":    {github, []string{"Sign in with Auth0"}, []string{"GitHub"}},
		"GitHub alone":   {auth0, []string{"Sign in with GitHub"}, []string{"Auth0"}},
	} {
		p := newPortalTest(t)
		for _, k := range tc.drop {
			delete(p.env, k)
		}
		p.serveSignIn(t, echo)
		for target, value := range map[string]string{
			"":                          "/",
			"?return=/portals/pets/":    "/portals/pets/",
			"?return=//evil.example/":   "/",
			"?return=https://evil.test": "/",
		} {
			page := get(t, http.DefaultClient, p.srv.URL+"/sign-in"+target)
			if page.status != http.StatusOK || page.header.Get("Cache-Control") != "no-store" {
				t.Errorf("%s: the sign-in page%s answers %d with Cache-Control %q", name, target, page.status, page.header.Get("Cache-Control"))
			}
			for _, want := range tc.want {
				provider := strings.ToLower(strings.TrimPrefix(want, "Sign in with "))
				if !strings.Contains(page.body, want) || !strings.Contains(page.body, `action="/sign-in/`+provider+`"`) {
					t.Errorf("%s: the sign-in page%s lacks the form of %s: %q", name, target, provider, page.body)
				}
			}
			for _, not := range tc.not {
				if strings.Contains(page.body, not) {
					t.Errorf("%s: the sign-in page%s offers %s", name, target, not)
				}
			}
			if want := `name="return" value="` + value + `"`; !strings.Contains(page.body, want) {
				t.Errorf("%s: the sign-in page%s lacks %s: %q", name, target, want, page.body)
			}
		}
	}
}

func TestARequestWithoutAProviderGoesToTheSignInPage(t *testing.T) {
	t.Skip("HOLE(3): send a request without a provider to the sign-in page")
	p := newPortalTest(t)
	p.serveSignIn(t, echo)
	resp := send(t, noRedirects, "GET", p.srv.URL+"/portals/pets/docs/guides/?q=1", nil, nil)
	if got := resp.Header.Get("Location"); resp.StatusCode != http.StatusFound || got != "/sign-in?return=%2Fportals%2Fpets%2Fdocs%2Fguides%2F%3Fq%3D1" {
		t.Errorf("a GET without a provider: %d to %q", resp.StatusCode, got)
	}
	if got := resp.Header.Get("Cache-Control"); got != "no-store" {
		t.Errorf("the redirect has Cache-Control %q, want no-store", got)
	}
	if resp := send(t, noRedirects, "HEAD", p.srv.URL+"/portals/pets/", nil, nil); resp.StatusCode != http.StatusFound {
		t.Errorf("a HEAD without a provider: %d", resp.StatusCode)
	}
	unknown := map[string]string{"Cookie": "signin_provider=okta"}
	if resp := send(t, noRedirects, "GET", p.srv.URL+"/portals/pets/", nil, unknown); resp.StatusCode != http.StatusFound || !strings.HasPrefix(resp.Header.Get("Location"), "/sign-in?") {
		t.Errorf("a GET with the choice of an unknown provider: %d to %q", resp.StatusCode, resp.Header.Get("Location"))
	}
	for name, req := range map[string]struct {
		method string
		header map[string]string
	}{
		"a POST":                 {"POST", nil},
		"an upgrade to a socket": {"GET", map[string]string{"Connection": "Upgrade", "Upgrade": "websocket"}},
	} {
		if resp := send(t, noRedirects, req.method, p.srv.URL+"/portals/pets/chat", nil, req.header); resp.StatusCode != http.StatusForbidden {
			t.Errorf("%s without a provider: %d, want 403", name, resp.StatusCode)
		}
	}
}

func TestAReaderSignsInThroughTheChosenProvider(t *testing.T) {
	t.Skip("HOLE(3): sign a reader in through the chosen provider")
	p := newPortalTest(t)
	p.serveSignIn(t, echo)
	for provider, want := range map[string]string{"auth0": "auth0 auth0|ada", "github": "github github|2"} {
		jar, err := cookiejar.New(nil)
		if err != nil {
			t.Fatal(err)
		}
		c := &http.Client{Jar: jar, Timeout: 10 * time.Second}
		resp, err := c.PostForm(p.srv.URL+"/sign-in/"+provider, url.Values{"return": {"/portals/pets/docs/guides/?q=1"}})
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		if got := resp.Request.URL.RequestURI(); got != "/portals/pets/docs/guides/?q=1" || string(body) != want {
			t.Errorf("%s: the sign-in ended at %s with %q, want the return target with %q", provider, got, body, want)
		}
		page := get(t, c, p.srv.URL+"/portals/pets/")
		if page.body != want {
			t.Errorf("%s: the wrapped handler saw %q, want %q", provider, page.body, want)
		}
		u, _ := url.Parse(p.srv.URL)
		var choice string
		for _, cookie := range c.Jar.Cookies(u) {
			if cookie.Name == "signin_provider" {
				choice = cookie.Value
			}
		}
		if choice != provider {
			t.Errorf("%s: the choice cookie holds %q", provider, choice)
		}
	}
}

func TestTheChoiceStaysOnTheSite(t *testing.T) {
	t.Skip("HOLE(3): keep the choice of a provider to the portal's own pages")
	p := newPortalTest(t)
	p.serveSignIn(t, echo)
	choose := func(provider, target string, header map[string]string) *http.Response {
		return send(t, noRedirects, "POST", p.srv.URL+"/sign-in/"+provider, url.Values{"return": {target}}, header)
	}
	for name, header := range map[string]map[string]string{
		"from another site":   {"Sec-Fetch-Site": "cross-site"},
		"from another origin": {"Origin": "http://evil.example"},
	} {
		if resp := choose("github", "/", header); resp.StatusCode != http.StatusForbidden || choiceCookie(resp) != nil {
			t.Errorf("a choice %s: %d, with the cookie %v", name, resp.StatusCode, choiceCookie(resp))
		}
	}
	for target, want := range map[string]string{
		"/portals/pets/?q=1":   "/portals/pets/?q=1",
		"//evil.example/x":     "/",
		"https://evil.example": "/",
		`/\evil.example`:       "/",
		"evil":                 "/",
		"":                     "/",
	} {
		resp := choose("github", target, nil)
		if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != want {
			t.Errorf("the return target %q: %d to %q, want %q", target, resp.StatusCode, resp.Header.Get("Location"), want)
		}
		c := choiceCookie(resp)
		if c == nil || c.Value != "github" || !c.HttpOnly || c.SameSite != http.SameSiteLaxMode || c.Path != "/" || c.MaxAge != 30*24*60*60 {
			t.Errorf("the return target %q: the choice cookie %+v", target, c)
		}
	}
	if resp := choose("okta", "/", nil); resp.StatusCode != http.StatusNotFound || choiceCookie(resp) != nil {
		t.Errorf("the choice of an unknown provider: %d", resp.StatusCode)
	}
	if resp := send(t, noRedirects, "GET", p.srv.URL+"/sign-in/github", nil, nil); resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("a GET of a choice: %d, want 405", resp.StatusCode)
	}
}

func TestSignOutClearsTheChoice(t *testing.T) {
	t.Skip("HOLE(3): clear the choice at a sign-out")
	p := newPortalTest(t)
	const logout = "https://tenant.example/v2/logout?client_id=portalapp"
	p.env["AUTH0_LOGOUT_URL"] = logout
	p.serveSignIn(t, echo)

	// Auth0's sign-out ends at its logout URL, without the choice.
	ada := signInAs(t, p.srv.URL, "auth0")
	resp := send(t, &http.Client{Jar: ada.Jar, CheckRedirect: noRedirects.CheckRedirect}, "GET", p.srv.URL+"/auth/auth0/sign-out", nil, nil)
	if c := choiceCookie(resp); resp.StatusCode != http.StatusFound || resp.Header.Get("Location") != logout || c == nil || c.MaxAge >= 0 {
		t.Errorf("Ada's sign-out: %d to %q, with the choice cookie %+v", resp.StatusCode, resp.Header.Get("Location"), c)
	}

	grace := signInAs(t, p.srv.URL, "github")
	resp = send(t, grace, "GET", p.srv.URL+"/auth/github/sign-out", nil, map[string]string{"Sec-Fetch-Site": "cross-site"})
	if c := choiceCookie(resp); resp.StatusCode != http.StatusOK || c != nil {
		t.Errorf("a sign-out from another site: %d, with the choice cookie %v", resp.StatusCode, c)
	}
	if page := get(t, grace, p.srv.URL+"/portals/pets/"); page.body != "github github|2" {
		t.Errorf("after a sign-out from another site, Grace's page: %q", page.body)
	}

	signedOut := get(t, grace, p.srv.URL+"/auth/github/sign-out")
	if !strings.Contains(signedOut.body, "You have signed out") {
		t.Errorf("the sign-out answers %d %q", signedOut.status, signedOut.body)
	}
	if page := get(t, grace, p.srv.URL+"/portals/pets/"); !strings.Contains(page.body, `action="/sign-in/github"`) {
		t.Errorf("after the sign-out, Grace's next page: %s %q, want the sign-in page", page.url, page.body)
	}
}

func TestTheAccessHookReadsTheReadersSignIn(t *testing.T) {
	t.Skip("HOLE(3): sign a reader in through the chosen provider")
	rules, err := readAccessFile("demo/access.yaml")
	if err != nil {
		t.Fatal(err)
	}
	access := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		a, err := rules.access(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		previews, _ := a.(portal.PreviewAccess)
		fmt.Fprintf(w, "pets %v, partners %v, staff notes %v, previews %v",
			a.Portal(pets), a.Portal(partners), a.Section(pets, staffNotes), previews != nil && previews.Preview("pr-1"))
	})
	p := newPortalTest(t)
	p.serveSignIn(t, access)
	for provider, want := range map[string]string{
		"auth0":  "pets true, partners false, staff notes true, previews false",
		"github": "pets true, partners true, staff notes false, previews true",
	} {
		c := signInAs(t, p.srv.URL, provider)
		if page := get(t, c, p.srv.URL+"/"); page.body != want {
			t.Errorf("through %s: %q, want %q", provider, page.body, want)
		}
	}

	// A request outside the sign-in sees nothing.
	a, err := rules.access(httptest.NewRequest("GET", "/", nil))
	if err != nil {
		t.Fatal(err)
	}
	if a == nil || a.Portal(pets) || a.Section(pets, api) {
		t.Errorf("a request without a sign-in sees the portal without labels: %#v", a)
	}
}
