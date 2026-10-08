package portal_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"html"
	"io/fs"
	"log"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"testing/fstest"

	"github.com/bilus/documentation-portal/internal/fakeaccess"
	"github.com/bilus/documentation-portal/portal"
)

// previewRoot returns a documentation root whose files carry version: the
// spec's title is Pets and version, the document a.md says "The version
// version.", and the image pic.png holds "png" and version.
func previewRoot(version string) fstest.MapFS {
	return fstest.MapFS{
		"specs/pets.yaml": {Data: []byte("openapi: 3.0.3\ninfo:\n  title: Pets " + version + "\n  version: 1.0.0\npaths: {}\n")},
		"docs/a.md":       {Data: []byte("# A\n\nThe " + version + " version.\n")},
		"docs/pic.png":    {Data: []byte("png " + version)},
	}
}

// previewPortals is the portal configuration of the preview pr-1: two
// portals, so that its / shows the home page.
var previewPortals = []portal.Portal{
	{Name: "Pets", Sections: sections("specs/pets.yaml", "docs", "")},
	{Name: "Store", Sections: sections("specs/pets.yaml", "", "")},
}

// opener returns the Open of a previews configuration: the preview folder
// pr-1, with the root previewRoot("preview") and previewPortals; the folder
// broken, whose load fails; and no other folder. It counts its calls in
// opened.
func opener(t *testing.T, opened *atomic.Int64) func(context.Context, string) (http.Handler, error) {
	t.Helper()
	pr1, err := portal.New(portal.Config{Root: previewRoot("preview"), Portals: previewPortals})
	if err != nil {
		t.Fatal(err)
	}
	return func(_ context.Context, folder string) (http.Handler, error) {
		opened.Add(1)
		switch folder {
		case "pr-1":
			return pr1, nil
		case "broken":
			return nil, errors.New("bucket unreachable")
		}
		return nil, fmt.Errorf("preview %q: %w", folder, fs.ErrNotExist)
	}
}

// publishedPortal returns the portal handler of the published documentation
// of the preview tests: previewRoot("published") with one portal, Pets.
func publishedPortal(t *testing.T) http.Handler {
	t.Helper()
	h, err := portal.New(portal.Config{Root: previewRoot("published"), Portals: petsPortal(sections("specs/pets.yaml", "docs", ""))})
	if err != nil {
		t.Fatal(err)
	}
	return h
}

// newPreviews returns the previews handler around publishedPortal, with
// opener's folders and the access hook hook, which may be nil.
func newPreviews(t *testing.T, hook func(*http.Request) (portal.Access, error)) http.Handler {
	t.Helper()
	var opened atomic.Int64
	return portal.WithPreviews(publishedPortal(t), portal.PreviewsConfig{Open: opener(t, &opened), Access: hook})
}

// inPreview is the cookie of a reader in the preview pr-1.
const inPreview = "portal-preview=pr-1"

// cookieOf returns the last cookie named name that rec sets, or nil.
func cookieOf(rec *httptest.ResponseRecorder, name string) *http.Cookie {
	var last *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == name {
			last = c
		}
	}
	return last
}

// TestARequestWithoutAPreviewPassesThrough runs process 11 on requests
// without a preview: each passes through boxes 11.1 to 11.4 to the published
// portal handler as it is, and asks neither the access hook nor the opener.
func TestARequestWithoutAPreviewPassesThrough(t *testing.T) {
	published := publishedPortal(t)
	var opened, asked atomic.Int64
	h := portal.WithPreviews(published, portal.PreviewsConfig{Open: opener(t, &opened), Access: func(*http.Request) (portal.Access, error) {
		asked.Add(1)
		return portal.Everything, nil
	}})
	for _, path := range []string{
		"/",
		"/portals/pets/specs/api",
		"/portals/pets/api/specs/api",
		"/portals/pets/docs/documents/",
		"/portals/pets/docs/documents/a.md",
		"/portals/pets/raw/documents/pic.png",
		"/portals/gone/specs/api",
		"/assets/elements/styles.min.css",
	} {
		want, got := get(published, path), get(h, path)
		if got.Code != want.Code || got.Body.String() != want.Body.String() || !reflect.DeepEqual(got.Header(), want.Header()) {
			t.Errorf("%s: %d %v, want %d %v as the published portal handler answers", path, got.Code, got.Header(), want.Code, want.Header())
		}
	}
	if opened.Load() != 0 || asked.Load() != 0 {
		t.Errorf("requests without a preview opened %d folders and asked the hook %d times", opened.Load(), asked.Load())
	}
}

func TestEnteringAPreviewSwitchesTheReader(t *testing.T) {
	h := newPreviews(t, nil)
	for path, want := range map[string]string{
		"/previews/pr-1":  "/",
		"/previews/pr-1/": "/",
		"/previews/pr-1/portals/pets/docs/documents/a.md":          "/portals/pets/docs/documents/a.md",
		"/previews/pr-1/portals/pets/docs/documents/my%20notes.md": "/portals/pets/docs/documents/my%20notes.md",
		// A path that would lead to another site stays on this one.
		"/previews/pr-1//evil.example/x": "/evil.example/x",
		"/previews/pr-1/%5Cevil.example": "/%5Cevil.example",
		"/previews/pr-1/../../x":         "/x",
	} {
		rec := get(h, path)
		c := cookieOf(rec, "portal-preview")
		if rec.Code != http.StatusFound || rec.Header().Get("Location") != want || c == nil || c.Value != "pr-1" || c.Path != "/" || !c.HttpOnly || c.SameSite != http.SameSiteLaxMode || c.MaxAge != 0 {
			t.Errorf("%s: %d to %q with the cookie %v, want a redirect to %s that sets the session cookie portal-preview=pr-1", path, rec.Code, rec.Header().Get("Location"), c, want)
		}
	}
	// The cookie takes the reader's next request to the preview.
	if rec := getWith(h, "/portals/pets/docs/documents/a.md", "Cookie", inPreview); !strings.Contains(rec.Body.String(), "The preview version.") {
		t.Errorf("the reader's next page: %d %q", rec.Code, rec.Body)
	}
	// A switch clears the notice of an ended preview.
	if c := cookieOf(getWith(h, "/previews/pr-1", "Cookie", "portal-preview-ended=gone"), "portal-preview-ended"); c == nil || c.MaxAge >= 0 {
		t.Errorf("the switch left the notice cookie: %v", c)
	}
}

func TestAPreviewServesEveryRequestFromItsFolder(t *testing.T) {
	h := newPreviews(t, nil)
	for path, want := range map[string]string{
		"/portals/pets/docs/documents/a.md":   "The preview version.",
		"/portals/pets/api/specs/api":         "title: Pets preview",
		"/portals/pets/raw/documents/pic.png": "png preview",
		"/portals/pets/specs/api":             `apiDescriptionUrl="/portals/pets/api/specs/api"`,
		"/portals/store/specs/api":            `apiDescriptionUrl="/portals/store/api/specs/api"`,
		"/portals/pets/docs/documents/":       `href="/portals/pets/docs/documents/a.md"`,
	} {
		if rec := getWith(h, path, "Cookie", inPreview); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), want) {
			t.Errorf("%s in the preview: %d, want %q in %q", path, rec.Code, want, rec.Body)
		}
	}
	if rec := getWith(h, "/assets/elements/styles.min.css", "Cookie", inPreview); rec.Code != http.StatusOK {
		t.Errorf("an Elements asset in the preview: %d", rec.Code)
	}
	// Without the cookie, the published documentation answers.
	for path, want := range map[string]string{
		"/portals/pets/docs/documents/a.md":   "The published version.",
		"/portals/pets/api/specs/api":         "title: Pets published",
		"/portals/pets/raw/documents/pic.png": "png published",
	} {
		if rec := get(h, path); !strings.Contains(rec.Body.String(), want) {
			t.Errorf("%s without a preview: want %q in %q", path, want, rec.Body)
		}
	}
	if rec := get(h, "/portals/store/specs/api"); rec.Code != http.StatusNotFound {
		t.Errorf("the preview's portal Store answers %d without the preview", rec.Code)
	}
}

func TestEveryPageOfAPreviewShowsTheBanner(t *testing.T) {
	h := newPreviews(t, nil)
	const banner = `<div class="portal-banner">Preview <strong>pr-1</strong><a href="/previews/">Leave the preview</a></div>`
	for _, path := range []string{
		"/",
		"/portals/pets/specs/api",
		"/portals/pets/specs/gone",
		"/portals/pets/docs/documents/",
		"/portals/pets/docs/documents/a.md",
		"/portals/pets/docs/documents/gone.md",
		"/portals/gone/specs/api",
	} {
		body := getWith(h, path, "Cookie", inPreview).Body.String()
		at := strings.Index(body, banner)
		if at < 0 {
			t.Errorf("%s in the preview shows no banner: %q", path, body)
		}
		if nav := strings.Index(body, `<nav class="portal-nav">`); nav >= 0 && nav < at {
			t.Errorf("%s in the preview shows the banner below the navigation bar", path)
		}
	}
	if body := getWith(h, "/portals/pets/api/specs/api", "Cookie", inPreview).Body.String(); strings.Contains(body, "portal-banner") {
		t.Errorf("the raw spec of the preview holds a banner: %q", body)
	}
	for _, path := range []string{"/portals/pets/specs/api", "/portals/pets/docs/documents/a.md"} {
		if body := get(h, path).Body.String(); strings.Contains(body, "portal-banner") {
			t.Errorf("%s without a preview shows a banner", path)
		}
	}
}

func TestLeavingThePreviewOpensThePublishedDocumentation(t *testing.T) {
	h := newPreviews(t, nil)
	for _, path := range []string{"/previews/", "/previews"} {
		rec := getWith(h, path, "Cookie", inPreview)
		if c := cookieOf(rec, "portal-preview"); rec.Code != http.StatusFound || rec.Header().Get("Location") != "/" || c == nil || c.MaxAge >= 0 || c.Path != "/" {
			t.Errorf("%s: %d to %q with the cookie %v, want a redirect to / that clears portal-preview", path, rec.Code, rec.Header().Get("Location"), c)
		}
	}
}

func TestAFolderThatDoesNotOpenIsNotFound(t *testing.T) {
	var logged bytes.Buffer
	prev := log.Writer()
	log.SetOutput(&logged)
	t.Cleanup(func() { log.SetOutput(prev) })
	h := newPreviews(t, nil)
	for path, name := range map[string]string{
		"/previews/no-such":                        "no-such",
		"/previews/no-such/portals/pets/specs/api": "no-such",
		"/previews/broken":                         "broken",
		"/previews/a%20b":                          "a b",
		"/previews/a%2Fb":                          "a/b",
		"/previews/%2e%2e":                         "..",
		"/previews/%2e":                            ".",
		"/previews/pr~1":                           "pr~1",
		"/previews/caf%C3%A9":                      "café",
		"/previews/%3Cscript%3E":                   "<script>",
	} {
		for _, cookie := range []string{"", inPreview} {
			rec := getWith(h, path, "Cookie", cookie)
			body := rec.Body.String()
			if rec.Code != http.StatusNotFound || !strings.Contains(body, "Preview not found") || !strings.Contains(body, "No preview has the folder name "+html.EscapeString(name)+".") || strings.Contains(body, "<script>") {
				t.Errorf("%s with the cookie %q: %d %q, want the 404 page naming %q", path, cookie, rec.Code, body, name)
			}
			if c := cookieOf(rec, "portal-preview"); c != nil {
				t.Errorf("%s with the cookie %q set the cookie %v", path, cookie, c)
			}
			if got := rec.Header().Get("Cache-Control"); got != "no-store" {
				t.Errorf("%s with the cookie %q: Cache-Control %q, want no-store, or the browser restores a preview page from its history after the cookie changed", path, cookie, got)
			}
			// A reader in a preview stays there, with the preview's banner.
			if inside := strings.Contains(body, "Preview <strong>pr-1</strong>"); inside != (cookie != "") {
				t.Errorf("%s with the cookie %q: the banner of pr-1 shows %v", path, cookie, inside)
			}
		}
	}
	if !strings.Contains(logged.String(), "bucket unreachable") {
		t.Errorf("the failed load of broken is not in the log: %q", logged.String())
	}
}

func TestAPreviewThatNoLongerOpensReturnsTheReaderWithANotice(t *testing.T) {
	var logged bytes.Buffer
	prev := log.Writer()
	log.SetOutput(&logged)
	t.Cleanup(func() { log.SetOutput(prev) })
	h := newPreviews(t, nil)
	const notice = `<div class="portal-banner">The preview <strong>gone</strong> is no longer available. This is the published documentation.</div>`
	rec := getWith(h, "/portals/pets/docs/documents/a.md", "Cookie", "portal-preview=gone")
	if body := rec.Body.String(); rec.Code != http.StatusOK || !strings.Contains(body, "The published version.") || !strings.Contains(body, notice) {
		t.Errorf("the page after the preview: %d %q, want the published page with the notice", rec.Code, body)
	}
	if c := cookieOf(rec, "portal-preview"); c == nil || c.MaxAge >= 0 {
		t.Errorf("the preview cookie stays: %v", c)
	}
	if c := cookieOf(rec, "portal-preview-ended"); c != nil && c.MaxAge >= 0 {
		t.Errorf("the page with the notice keeps the notice cookie: %v", c)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("the page with the notice: Cache-Control %q, want no-store", got)
	}
	// A response that is no page, here the redirect of /, carries the notice
	// in its cookie to the next page, which clears it.
	rec = getWith(h, "/", "Cookie", "portal-preview=gone")
	ended := cookieOf(rec, "portal-preview-ended")
	if rec.Code != http.StatusFound || ended == nil || ended.Value != "gone" || ended.MaxAge < 0 || ended.Path != "/" {
		t.Fatalf("the redirect of /: %d with the notice cookie %v", rec.Code, ended)
	}
	rec = getWith(h, rec.Header().Get("Location"), "Cookie", "portal-preview-ended=gone")
	if !strings.Contains(rec.Body.String(), notice) {
		t.Errorf("the page after the redirect shows no notice: %q", rec.Body)
	}
	if c := cookieOf(rec, "portal-preview-ended"); c == nil || c.MaxAge >= 0 {
		t.Errorf("the page with the notice keeps its cookie: %v", c)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("the page with the notice of the cookie: Cache-Control %q, want no-store", got)
	}
	// A failed load returns the reader too, and its cause goes to the log.
	rec = getWith(h, "/portals/pets/specs/api", "Cookie", "portal-preview=broken")
	if !strings.Contains(rec.Body.String(), "The preview <strong>broken</strong> is no longer available.") {
		t.Errorf("the page after a failed load: %q", rec.Body)
	}
	if !strings.Contains(logged.String(), "bucket unreachable") {
		t.Errorf("the failed load is not in the log: %q", logged.String())
	}
}

func TestTheAccessHookDecidesWhoOpensPreviews(t *testing.T) {
	var logged bytes.Buffer
	prev := log.Writer()
	log.SetOutput(&logged)
	t.Cleanup(func() { log.SetOutput(prev) })
	h := newPreviews(t, fakeaccess.Hook)
	if rec := getWith(h, "/previews/pr-1", fakeaccess.PreviewsHeader, "pr-1"); rec.Code != http.StatusFound || cookieOf(rec, "portal-preview") == nil {
		t.Errorf("a reader who may open pr-1: %d", rec.Code)
	}
	if rec := getWith(h, "/portals/pets/docs/documents/a.md", "Cookie", inPreview, fakeaccess.PreviewsHeader, "pr-1"); !strings.Contains(rec.Body.String(), "The preview version.") {
		t.Errorf("a reader who may open pr-1 reads %q", rec.Body)
	}
	if rec := getWith(h, "/previews/pr-1", fakeaccess.PreviewsHeader, "pr-2"); rec.Code != http.StatusNotFound || cookieOf(rec, "portal-preview") != nil {
		t.Errorf("a reader who may open pr-2 alone switched to pr-1: %d", rec.Code)
	}
	// A reader whose access no longer opens the preview leaves it, with the notice.
	rec := getWith(h, "/portals/pets/docs/documents/a.md", "Cookie", inPreview)
	if body := rec.Body.String(); !strings.Contains(body, "The published version.") || !strings.Contains(body, "The preview <strong>pr-1</strong> is no longer available.") || cookieOf(rec, "portal-preview") == nil {
		t.Errorf("a reader without access to pr-1 in pr-1: %q", body)
	}
	for name, hook := range map[string]func(*http.Request) (portal.Access, error){
		"an access without Preview": func(*http.Request) (portal.Access, error) { return fakeaccess.Parse("Pets/API"), nil },
		"an error of the hook":      func(*http.Request) (portal.Access, error) { return nil, errors.New("the token expired") },
		"no access":                 func(*http.Request) (portal.Access, error) { return nil, nil },
	} {
		if rec := get(newPreviews(t, hook), "/previews/pr-1"); rec.Code != http.StatusNotFound || cookieOf(rec, "portal-preview") != nil {
			t.Errorf("%s opened pr-1: %d", name, rec.Code)
		}
	}
	if !strings.Contains(logged.String(), "access hook: the token expired") {
		t.Errorf("the hook's error is not in the log: %q", logged.String())
	}
	everyone := newPreviews(t, func(*http.Request) (portal.Access, error) { return portal.Everything, nil })
	if rec := get(everyone, "/previews/pr-1"); rec.Code != http.StatusFound {
		t.Errorf("Everything does not open pr-1: %d", rec.Code)
	}
}

func TestPreviewResponsesArePrivate(t *testing.T) {
	h := newPreviews(t, nil)
	for _, path := range []string{
		"/",
		"/portals/pets/docs/documents/a.md",
		"/portals/pets/docs/documents/gone.md",
		"/portals/pets/api/specs/api",
		"/portals/pets/raw/documents/pic.png",
		"/assets/elements/styles.min.css",
		"/assets/elements/missing.js",
		"/previews/pr-1",
		"/previews/",
	} {
		if got := getWith(h, path, "Cookie", inPreview).Header().Get("Cache-Control"); got != "no-store" {
			t.Errorf("%s in the preview: Cache-Control %q, want no-store", path, got)
		}
	}
	for _, path := range []string{"/", "/portals/pets/docs/documents/a.md", "/portals/pets/api/specs/api"} {
		if got := get(h, path).Header().Get("Cache-Control"); got != "" {
			t.Errorf("%s without a preview: Cache-Control %q, want none", path, got)
		}
	}
}

func TestWithoutAnOpenerThereAreNoPreviews(t *testing.T) {
	published := publishedPortal(t)
	for name, cfg := range map[string]portal.PreviewsConfig{
		"nothing":        {},
		"an access hook": {Access: fakeaccess.Hook},
	} {
		if h := portal.WithPreviews(published, cfg); h != published {
			t.Errorf("with %s and no opener, WithPreviews wraps the published portal handler", name)
		}
	}
}

func TestThePortalHandlerAnswersAPreviewFolderAsNotFound(t *testing.T) {
	h := newAccountPortal(t, previewRoot("published"), petsPortal(sections("specs/pets.yaml", "docs", "")), nil, signedIn)
	for path, name := range map[string]string{
		"/previews/pr-1":                        "pr-1",
		"/previews/pr-1/portals/pets/specs/api": "pr-1",
		"/previews/%3Cb%3E":                     "<b>",
	} {
		rec := getWith(h, path, "Reader", "Ada")
		body := rec.Body.String()
		if rec.Code != http.StatusNotFound || !strings.Contains(body, "<h1>Preview not found</h1>") || !strings.Contains(body, "No preview has the folder name "+html.EscapeString(name)+".") {
			t.Errorf("%s: %d %q, want the 404 page naming %q", path, rec.Code, body, name)
		}
		if bar := barOf(body); !strings.Contains(bar, `<a href="/">Portals</a>`) || !strings.HasSuffix(bar, adasLinks+"</nav>") {
			t.Errorf("%s: the navigation bar %q, want Portals and the account links", path, bar)
		}
	}
}

func TestASwitchReadsThePathAsServeMuxDoes(t *testing.T) {
	h := newPreviews(t, nil)
	// An escaped letter of previews names the path that ServeMux routes.
	rec := get(h, "/%70reviews/pr-1")
	if c := cookieOf(rec, "portal-preview"); rec.Code != http.StatusFound || rec.Header().Get("Location") != "/" || c == nil || c.Value != "pr-1" {
		t.Errorf("/%%70reviews/pr-1: %d to %q with the cookie %v, want the switch to pr-1", rec.Code, rec.Header().Get("Location"), c)
	}
	rec = getWith(h, "/%70reviews/", "Cookie", inPreview)
	if c := cookieOf(rec, "portal-preview"); rec.Code != http.StatusFound || c == nil || c.MaxAge >= 0 {
		t.Errorf("/%%70reviews/: %d with the cookie %v, want the way out of the preview", rec.Code, c)
	}
	if rec := get(h, "/%70reviews/no-such"); rec.Code != http.StatusNotFound || rec.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("/%%70reviews/no-such: %d with Cache-Control %q, want the uncacheable 404 page", rec.Code, rec.Header().Get("Cache-Control"))
	}
}

func TestASwitchKeepsTheQueryAndTheEscapes(t *testing.T) {
	h := newPreviews(t, nil)
	for path, want := range map[string]string{
		"/previews/pr-1/portals/pets/docs/documents/a.md?x=1&y=2": "/portals/pets/docs/documents/a.md?x=1&y=2",
		"/previews/pr-1/portals/pets/docs/documents/a%2Fb.md":     "/portals/pets/docs/documents/a%2Fb.md",
		"/previews/pr-1/portals/pets/docs/documents/a+b.md":       "/portals/pets/docs/documents/a+b.md",
		// Escaped slashes stay escaped, so they name no other host.
		"/previews/pr-1/%2F%2Fevil.example/x": "/%2F%2Fevil.example/x",
	} {
		if rec := get(h, path); rec.Code != http.StatusFound || rec.Header().Get("Location") != want {
			t.Errorf("%s: %d to %q, want a redirect to %s", path, rec.Code, rec.Header().Get("Location"), want)
		}
	}
}

// setCookies returns the Set-Cookie lines of rec for the cookie name.
func setCookies(rec *httptest.ResponseRecorder, name string) []string {
	var lines []string
	for _, line := range rec.Header().Values("Set-Cookie") {
		if strings.HasPrefix(line, name+"=") {
			lines = append(lines, line)
		}
	}
	return lines
}

func TestTheNoticeCookieGoesOutOnce(t *testing.T) {
	h := newPreviews(t, nil)
	// The page with the notice of its own response needs no notice cookie.
	if lines := setCookies(getWith(h, "/portals/pets/docs/documents/a.md", "Cookie", "portal-preview=gone"), "portal-preview-ended"); len(lines) != 0 {
		t.Errorf("the page with the notice sets %q", lines)
	}
	// A redirect sets it once, and the page after the redirect clears it once.
	rec := getWith(h, "/", "Cookie", "portal-preview=gone")
	if lines := setCookies(rec, "portal-preview-ended"); len(lines) != 1 || strings.Contains(lines[0], "Max-Age=0") {
		t.Errorf("the redirect of / sets %q, want the notice cookie once", lines)
	}
	rec = getWith(h, rec.Header().Get("Location"), "Cookie", "portal-preview-ended=gone")
	if lines := setCookies(rec, "portal-preview-ended"); len(lines) != 1 || !strings.Contains(lines[0], "Max-Age=0") {
		t.Errorf("the page after the redirect sets %q, want the notice cookie cleared once", lines)
	}
}
