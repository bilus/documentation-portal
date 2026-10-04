package main

import (
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bilus/documentation-portal/examples/portalapp/mocks"
)

// testKey is the session key of the tests.
const testKey = "the session key of the example's tests, 32 bytes or more"

// portalTest is the example application in a test: its settings for the
// mock Auth0 of Ada, the stub GitHub of Grace and a copy of the demo
// documentation, and the portal's server, which start starts.
type portalTest struct {
	env map[string]string
	dir string // the copy of the demo documentation's bucket folder
	srv *httptest.Server
}

// newPortalTest starts the mock Auth0 and the stub GitHub, and returns the
// settings of the example application for them, with a copy of the demo
// documentation, and the portal's server, not yet started.
func newPortalTest(t *testing.T) *portalTest {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	auth0, err := mocks.StartAuth0(ln, mocks.Ada)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { auth0.Shutdown() })
	github := httptest.NewServer(mocks.GitHub(mocks.Grace))
	t.Cleanup(github.Close)
	srv := httptest.NewUnstartedServer(nil)
	t.Cleanup(srv.Close)
	dir, bucket := demoBucket(t)
	return &portalTest{dir: dir, srv: srv, env: map[string]string{
		"PORTAL_URL":           "http://" + srv.Listener.Addr().String(),
		"PORTAL_BUCKET":        bucket,
		"PORTAL_ACCESS_FILE":   "demo/access.yaml",
		"PORTAL_SESSION_KEY":   testKey,
		"AUTH0_ISSUER":         auth0.Issuer(),
		"AUTH0_CLIENT_ID":      mocks.ClientID,
		"AUTH0_CLIENT_SECRET":  mocks.ClientSecret,
		"GITHUB_CLIENT_ID":     mocks.ClientID,
		"GITHUB_CLIENT_SECRET": mocks.ClientSecret,
		"GITHUB_URL":           github.URL,
		"GITHUB_API_URL":       github.URL,
	}}
}

// demoBucket copies the demo documentation's bucket folder into a
// directory of the test, which the test may change, and returns the
// directory and its file:// bucket folder URL.
func demoBucket(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.CopyFS(dir, os.DirFS("demo/docs")); err != nil {
		t.Fatal(err)
	}
	return dir, "file://" + filepath.ToSlash(dir)
}

// start runs startup with the test's settings until the end of the test,
// starts the portal's server with its handler, and returns the address of
// the settings.
func (p *portalTest) start(t *testing.T) string {
	t.Helper()
	addr, h, err := startup(t.Context(), func(name string) string { return p.env[name] })
	if err != nil {
		t.Fatal(err)
	}
	p.srv.Config.Handler = h
	p.srv.Start()
	return addr
}

// signInAs signs a reader in at the portal at base through the identity
// provider of the provider name, from the sign-in page, as a browser does,
// and returns the reader's client, whose cookie jar holds the session.
func signInAs(t *testing.T, base, provider string) *http.Client {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	c := &http.Client{Jar: jar, Timeout: 10 * time.Second}
	if page := get(t, c, base+"/"); !strings.Contains(page.body, `action="/sign-in/`+provider+`"`) {
		t.Fatalf("/ without a session shows %s %q, want the sign-in page with %s", page.url, page.body, provider)
	}
	resp, err := c.PostForm(base+"/sign-in/"+provider, url.Values{"return": {"/"}})
	if err != nil {
		t.Fatalf("sign in through %s: %v", provider, err)
	}
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK || strings.Contains(string(body), `action="/sign-in/`) {
		t.Fatalf("the sign-in through %s ended at %s with %d %q", provider, resp.Request.URL, resp.StatusCode, body)
	}
	return c
}

// page is a response as a test reads it.
type page struct {
	status int
	url    string // where the redirects ended
	header http.Header
	body   string
}

// get gets url with the client c, following redirects.
func get(t *testing.T, c *http.Client, url string) page {
	t.Helper()
	resp, err := c.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return page{status: resp.StatusCode, url: resp.Request.URL.String(), header: resp.Header, body: string(body)}
}

// TestStartupOffersTheIdentityProviders is the smoke test of the top
// function: startup with the demo documentation, the demo access file, the
// mock Auth0 and the stub GitHub answers a request without a session with
// the sign-in page, which offers both identity providers.
func TestStartupOffersTheIdentityProviders(t *testing.T) {
	p := newPortalTest(t)
	if addr := p.start(t); addr != ":8080" {
		t.Errorf("startup listens on %q, want :8080", addr)
	}
	page := get(t, http.DefaultClient, p.srv.URL+"/portals/pets/")
	if page.status != http.StatusOK {
		t.Fatalf("a page without a session: %d %q", page.status, page.body)
	}
	for _, want := range []string{"Sign in with Auth0", "Sign in with GitHub"} {
		if !strings.Contains(page.body, want) {
			t.Errorf("the page without a session lacks %q: %q", want, page.body)
		}
	}
}

func TestStartupServesEachReaderTheirOwnSections(t *testing.T) {
	t.Skip("HOLE(4): build each snapshot's portal handler with the access hook and the account hook")
	p := newPortalTest(t)
	p.start(t)

	ada := signInAs(t, p.srv.URL, "auth0")
	if page := get(t, ada, p.srv.URL+"/"); !strings.HasSuffix(page.url, "/portals/pets/specs/api") {
		t.Errorf("/ opened %s for Ada, whose only portal is Pets", page.url)
	}
	staff := get(t, ada, p.srv.URL+"/portals/pets/docs/staff-notes/")
	if staff.status != http.StatusOK || !strings.Contains(staff.body, "on-call.md") {
		t.Errorf("Ada's staff notes: %d %q", staff.status, staff.body)
	}
	for _, want := range []string{">API</a>", ">Guides</a>", ">Staff notes</a>", "Ada Lovelace", ">Sign out</a>"} {
		if !strings.Contains(staff.body, want) {
			t.Errorf("Ada's staff notes lack %q", want)
		}
	}
	if strings.Contains(staff.body, "Partner") {
		t.Errorf("Ada's staff notes name the portal of the partners: %q", staff.body)
	}
	if got := staff.header.Get("Cache-Control"); got != "private" {
		t.Errorf("Ada's page has Cache-Control %q, want private", got)
	}
	if page := get(t, ada, p.srv.URL+"/portals/partners/specs/partner-api"); page.status != http.StatusNotFound {
		t.Errorf("the partners' API answers Ada with %d, want 404", page.status)
	}

	grace := signInAs(t, p.srv.URL, "github")
	home := get(t, grace, p.srv.URL+"/")
	for _, want := range []string{">Pets</a>", ">Partners</a>", "Grace Hopper"} {
		if !strings.Contains(home.body, want) {
			t.Errorf("Grace's home page lacks %q: %q", want, home.body)
		}
	}
	if page := get(t, grace, p.srv.URL+"/portals/partners/specs/partner-api"); page.status != http.StatusOK {
		t.Errorf("the partners' API answers Grace with %d, want 200", page.status)
	}
	guides := get(t, grace, p.srv.URL+"/portals/pets/docs/guides/")
	if guides.status != http.StatusOK || strings.Contains(guides.body, "Staff notes") {
		t.Errorf("Grace's guides: %d %q, want them without the staff notes", guides.status, guides.body)
	}
	if page := get(t, grace, p.srv.URL+"/portals/pets/docs/staff-notes/"); page.status != http.StatusNotFound {
		t.Errorf("the staff notes answer Grace with %d, want 404", page.status)
	}
}

func TestStartupRefreshesTheDocumentation(t *testing.T) {
	t.Skip("HOLE(4): build each snapshot's portal handler with the access hook and the account hook")
	p := newPortalTest(t)
	p.env["PORTAL_REFRESH"] = "50ms"
	p.start(t)
	ada := signInAs(t, p.srv.URL, "auth0")
	if err := os.WriteFile(filepath.Join(p.dir, "guides", "faq.md"), []byte("# FAQ\n\nThe answers.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		page := get(t, ada, p.srv.URL+"/portals/pets/docs/guides/faq.md")
		if page.status == http.StatusOK && strings.Contains(page.body, "The answers.") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the new guide never showed: %d %q", page.status, page.body)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestStartupOpensPreviewsToTheOrganization(t *testing.T) {
	t.Skip("HOLE(4): open the preview folders to the readers whom the access rules allow")
	p := newPortalTest(t)
	p.start(t)

	grace := signInAs(t, p.srv.URL, "github")
	preview := get(t, grace, p.srv.URL+"/previews/pr-1/portals/pets/docs/guides/getting-started.md")
	for _, want := range []string{"portal-banner", "pr-1", "The preview of the guide", "Grace Hopper"} {
		if !strings.Contains(preview.body, want) {
			t.Errorf("Grace's preview lacks %q: %d %q", want, preview.status, preview.body)
		}
	}
	// The preview's staff notes open to the staff alone, as the published ones.
	if strings.Contains(preview.body, "Staff notes") {
		t.Errorf("Grace's preview shows the staff notes: %q", preview.body)
	}

	ada := signInAs(t, p.srv.URL, "auth0")
	refused := get(t, ada, p.srv.URL+"/previews/pr-1")
	if refused.status != http.StatusNotFound || !strings.Contains(refused.body, "Preview not found") {
		t.Errorf("the preview answers Ada with %d %q, want 404", refused.status, refused.body)
	}
	if page := get(t, ada, p.srv.URL+"/portals/pets/docs/guides/getting-started.md"); !strings.Contains(page.body, "The published guide") {
		t.Errorf("Ada's guide after the refused preview: %q", page.body)
	}
}

func TestStartupRunsTheChatWithAnAPIKey(t *testing.T) {
	t.Skip("HOLE(4): add the chat with the reader hook of the sign-in, with an API key")
	model := httptest.NewServer(mocks.Model())
	defer model.Close()
	for key, want := range map[string]int{"": http.StatusNotFound, "a test key": http.StatusOK} {
		p := newPortalTest(t)
		p.env["ANTHROPIC_API_KEY"], p.env["ANTHROPIC_BASE_URL"] = key, model.URL
		p.start(t)
		ada := signInAs(t, p.srv.URL, "auth0")
		page := get(t, ada, p.srv.URL+"/portals/pets/chat")
		if page.status != want {
			t.Errorf("with the API key %q, the chat page answers %d, want %d", key, page.status, want)
		}
		if want == http.StatusOK && !strings.Contains(page.body, "Ask about the Pets API") {
			t.Errorf("the chat page: %q", page.body)
		}
		if guides := get(t, ada, p.srv.URL+"/portals/pets/docs/guides/"); strings.Contains(guides.body, ">Chat</a>") != (want == http.StatusOK) {
			t.Errorf("with the API key %q, the navigation bar's chat link is wrong: %q", key, guides.body)
		}
	}
}
