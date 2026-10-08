package portal_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/bilus/documentation-portal/portal"
)

// baseRoot is a documentation root with one portal of a spec section and a
// docs section, for the pages under a base path.
var baseRoot = fstest.MapFS{
	"specs/pets.yaml": {Data: []byte("openapi: 3.1.0\ninfo:\n  title: Pets\n  version: '1'\npaths: {}\n")},
	"docs/a.md":       {Data: []byte("# A\n\nSee [the spec](specs/pets.yaml) and [B](b.md).\n")},
	"docs/b.md":       {Data: []byte("# B\n")},
}

func basePortal(t *testing.T, base string) http.Handler {
	t.Helper()
	h, err := portal.New(portal.Config{Root: baseRoot, Portals: petsPortal(sections("specs/pets.yaml", "docs", "")), BasePath: base})
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func TestNewRefusesABadBasePath(t *testing.T) {
	for _, base := range []string{"docs", "/docs/", "/docs/../x", "//docs"} {
		if _, err := portal.New(portal.Config{Root: baseRoot, Portals: petsPortal(sections("specs/pets.yaml", "", "")), BasePath: base}); err == nil {
			t.Errorf("base path %q was accepted", base)
		}
	}
}

func TestUnderABasePathEveryURLCarriesIt(t *testing.T) {
	h := basePortal(t, "/docs")

	home := get(h, "/docs/")
	if home.Code != http.StatusFound || !strings.HasPrefix(home.Header().Get("Location"), "/docs/portals/pets/") {
		t.Fatalf("/docs/ answers %d to %q; with one portal it opens the portal under the base path", home.Code, home.Header().Get("Location"))
	}
	for _, path := range []string{"/docs/portals/pets/specs/api", "/docs/portals/pets/docs/documents/", "/docs/portals/pets/docs/documents/a.md"} {
		rec := get(h, path)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s answers %d: %s", path, rec.Code, rec.Body.String())
		}
		body := rec.Body.String()
		for _, attr := range []string{`href="/`, `src="/`} {
			for _, at := range indexesOf(body, attr) {
				if !strings.HasPrefix(body[at:], attr+"docs/") {
					t.Errorf("%s links outside the base path: %s", path, body[at:min(at+60, len(body))])
				}
			}
		}
		if !strings.Contains(body, `/docs/assets/elements/styles.min.css`) {
			t.Errorf("%s loads the assets from the root", path)
		}
	}
	viewer := get(h, "/docs/portals/pets/specs/api").Body.String()
	if !strings.Contains(viewer, `apiDescriptionUrl="/docs/portals/pets/api/specs/api"`) {
		t.Error("the viewer page fetches the raw spec from the root")
	}
	if rec := get(h, "/docs/assets/elements/styles.min.css"); rec.Code != http.StatusOK {
		t.Errorf("the assets are not served under the base path: %d", rec.Code)
	}
}

func TestUnderABasePathTheRootIsNotServed(t *testing.T) {
	h := basePortal(t, "/docs")

	if rec := get(h, "/portals/pets/specs/api"); rec.Code != http.StatusNotFound {
		t.Errorf("a page outside the base path answers %d, want 404", rec.Code)
	}
	if rec := get(h, "/docs"); rec.Code != http.StatusMovedPermanently || rec.Header().Get("Location") != "/docs/" {
		t.Errorf("the base path itself answers %d to %q, want a redirect to /docs/", rec.Code, rec.Header().Get("Location"))
	}
	if rec := get(h, "/docsx/"); rec.Code != http.StatusNotFound {
		t.Errorf("a path that only starts like the base path answers %d, want 404", rec.Code)
	}
}

func TestUnderABasePathPreviewsSwitchThere(t *testing.T) {
	published := basePortal(t, "/docs")
	open := func(context.Context, string) (http.Handler, error) { return basePortal(t, "/docs"), nil }
	h := portal.WithPreviews(published, portal.PreviewsConfig{Open: open, BasePath: "/docs"})

	enter := get(h, "/docs/previews/pr-1/portals/pets/specs/api")
	if enter.Code != http.StatusFound || enter.Header().Get("Location") != "/docs/portals/pets/specs/api" {
		t.Fatalf("entering a preview answers %d to %q", enter.Code, enter.Header().Get("Location"))
	}
	cookie := enter.Result().Cookies()[0] //nolint:bodyclose // recorder response has no body to close
	if cookie.Path != "/docs/" {
		t.Errorf("the preview cookie is set for %q, want /docs/, so that it stays off the rest of the host", cookie.Path)
	}

	req := httptest.NewRequest(http.MethodGet, "/docs/portals/pets/docs/documents/a.md", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if !strings.Contains(rec.Body.String(), `href="/docs/previews/"`) {
		t.Error("the banner's way back leads outside the base path")
	}

	leave := get(h, "/docs/previews/")
	if leave.Code != http.StatusFound || leave.Header().Get("Location") != "/docs/" {
		t.Errorf("leaving the preview answers %d to %q", leave.Code, leave.Header().Get("Location"))
	}
	if rec := get(h, "/previews/pr-1"); rec.Code != http.StatusNotFound {
		t.Errorf("a preview switch outside the base path answers %d, want 404", rec.Code)
	}
}

// indexesOf returns the index of every occurrence of sub in s.
func indexesOf(s, sub string) []int {
	var at []int
	for off := 0; ; {
		i := strings.Index(s[off:], sub)
		if i < 0 {
			return at
		}
		at = append(at, off+i)
		off += i + len(sub)
	}
}
