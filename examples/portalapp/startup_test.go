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
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/bilus/documentation-portal/examples/portalapp/mocks"
)

// testKey is the session key of the tests.
const testKey = "the session key of the example's tests, 32 bytes or more"

// portalTest is the example application in a test: its settings for the
// mock Auth0 of Ada, the stub GitHub of Grace and a copy of the demo
// documentation, and the portal's server, started by the method start.
type portalTest struct {
	env map[string]string
	dir string // the copy of the demo documentation's bucket folder
	srv *httptest.Server
}

// newPortalTest starts the mock Auth0 and the stub GitHub, and returns the
// settings of the example application for them, with a copy of the demo
// documentation, and the portal's server, nil until the method start.
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
// directory of the test, open to the test's changes, and returns the
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
// provider of the provider name, from the sign-in page, like a browser,
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

func TestOpenBucketLeavesThePreviewsOut(t *testing.T) {
	_, bucket := demoBucket(t)
	published, location, err := openBucket(t.Context(), bucket, "previews")
	if err != nil {
		t.Fatal(err)
	}
	listing, err := published.List(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	var keys []string
	for _, o := range listing {
		keys = append(keys, o.Key)
	}
	want := []string{"environment.yaml", "guides/getting-started.md", "specs/partners.yaml", "specs/pets.yaml", "staff/on-call.md"}
	if !slices.Equal(keys, want) {
		t.Errorf("the published documentation lists %q, want %q", keys, want)
	}
	pr1, err := location.Folder("pr-1")
	if err != nil {
		t.Fatal(err)
	}
	if listing, err := pr1.List(t.Context()); err != nil || len(listing) != 3 || listing[0].Key != "environment.yaml" {
		t.Errorf("the preview folder pr-1 lists %v, %v", listing, err)
	}
	if _, _, err := openBucket(t.Context(), "nosuchscheme://docs", "previews"); err == nil {
		t.Error("a bucket folder URL of no driver opened")
	}
	if _, _, err := openBucket(t.Context(), bucket, "../previews"); err == nil {
		t.Error("a previews location outside the bucket folder opened")
	}
}

func TestOpenBucketOpensTheNamedPreviewsLocation(t *testing.T) {
	dir, bucket := demoBucket(t)
	if err := os.Rename(filepath.Join(dir, "previews"), filepath.Join(dir, "pulls")); err != nil {
		t.Fatal(err)
	}
	published, location, err := openBucket(t.Context(), bucket, "pulls")
	if err != nil {
		t.Fatal(err)
	}
	if listing, err := published.List(t.Context()); err != nil || len(listing) != 5 {
		t.Errorf("the published documentation lists %d objects, %v, want 5", len(listing), err)
	}
	pr1, err := location.Folder("pr-1")
	if err != nil {
		t.Fatal(err)
	}
	if listing, err := pr1.List(t.Context()); err != nil || len(listing) != 3 {
		t.Errorf("the preview folder pulls/pr-1 lists %v, %v", listing, err)
	}
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
	p := newPortalTest(t)
	p.start(t)

	ada := signInAs(t, p.srv.URL, "auth0")
	adaHome := get(t, ada, p.srv.URL+"/")
	for _, want := range []string{"<h2>APIs</h2>", ">Pets</a>", "<h2>Operations</h2>", `<a href="https://status.example.com/">Status</a>`} {
		if !strings.Contains(adaHome.body, want) {
			t.Errorf("Ada's home page lacks %q: %q", want, adaHome.body)
		}
	}
	if strings.Contains(adaHome.body, "Partners") {
		t.Errorf("Ada's home page names the portal of the partners: %q", adaHome.body)
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
	for _, want := range []string{"<h2>APIs</h2>", ">Pets</a>", ">Partners</a>", "<h2>Operations</h2>", ">Status</a>", "Grace Hopper"} {
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
