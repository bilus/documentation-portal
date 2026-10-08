package portal_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/bilus/documentation-portal/portal"
)

func TestUnpublishedSaysSoInThePortalsLayout(t *testing.T) {
	account := func(*http.Request) []portal.AccountLink {
		return []portal.AccountLink{{Label: "Ada"}, {Label: "Sign out", URL: "/docs/auth/logout"}}
	}
	h := portal.Unpublished(portal.Config{BasePath: "/docs", Account: account})

	rec := get(h, "/docs/portals/pets/specs/api")

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status %d, want 503 until the first upload", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{"No documentation yet", `class="portal-nav"`, "Ada", `href="/docs/auth/logout"`, `href="/docs/previews/"`} {
		if !strings.Contains(body, want) {
			t.Errorf("the page lacks %q:\n%s", want, body)
		}
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control %q, want no-store: the next check may bring the documentation", got)
	}
	if rec := get(h, "/docs"); rec.Code != http.StatusMovedPermanently {
		t.Errorf("/docs answers %d, want a redirect under the base path like the portal", rec.Code)
	}
	if rec := get(h, "/"); rec.Code != http.StatusNotFound {
		t.Errorf("/ answers %d, want 404 outside the base path", rec.Code)
	}
}

func TestUnpublishedAtTheRoot(t *testing.T) {
	h := portal.Unpublished(portal.Config{})

	rec := get(h, "/anything")

	if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), `href="/previews/"`) {
		t.Errorf("status %d, body %q", rec.Code, rec.Body.String())
	}
}
