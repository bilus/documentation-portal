package portal_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/bilus/documentation-portal/portal"
)

const pets = "openapi: 3.0.3\ninfo:\n  title: Pets\n  version: 1.0.0\npaths: {}\n"

func newPortal(t *testing.T, files fstest.MapFS, specPath string) http.Handler {
	t.Helper()
	h, err := portal.New(portal.Config{Root: files, Sections: sections(specPath, "", "")})
	if err != nil {
		t.Fatal(err)
	}
	return h
}

// sections returns a spec section titled API for specPath and, unless
// docsPath is empty, a docs section titled Documents for docsPath with the toc
// path tocPath.
func sections(specPath, docsPath, tocPath string) []portal.Section {
	s := []portal.Section{{Title: "API", Type: portal.SpecSection, Input: specPath}}
	if docsPath != "" {
		s = append(s, portal.Section{Title: "Documents", Type: portal.DocsSection, Input: docsPath, Toc: tocPath})
	}
	return s
}

func get(h http.Handler, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

func TestNewServesSpec(t *testing.T) {
	h := newPortal(t, fstest.MapFS{"apis/pets.yaml": {Data: []byte(pets)}}, "apis/pets.yaml")

	page := get(h, "/specs/apis/pets.yaml")
	if page.Code != http.StatusOK || !strings.HasPrefix(page.Header().Get("Content-Type"), "text/html") {
		t.Fatalf("viewer page: %d %s", page.Code, page.Header().Get("Content-Type"))
	}
	if want := `apiDescriptionUrl="/api/specs/apis/pets.yaml"`; !strings.Contains(page.Body.String(), want) {
		t.Errorf("viewer page does not contain %s", want)
	}

	raw := get(h, "/api/specs/apis/pets.yaml")
	if raw.Code != http.StatusOK || !strings.HasPrefix(raw.Header().Get("Content-Type"), "application/yaml") || raw.Body.String() != pets {
		t.Errorf("raw spec: %d %s %q", raw.Code, raw.Header().Get("Content-Type"), raw.Body)
	}
}

func TestIndexRedirects(t *testing.T) {
	rec := get(newPortal(t, fstest.MapFS{"apis/pets.yaml": {Data: []byte(pets)}}, "apis/pets.yaml"), "/")
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/specs/apis/pets.yaml" {
		t.Errorf("got %d to %q", rec.Code, rec.Header().Get("Location"))
	}
}

func TestViewerPage(t *testing.T) {
	rec := get(newPortal(t, fstest.MapFS{"apis/pets.yaml": {Data: []byte(pets)}}, "apis/pets.yaml"), "/specs/apis/pets.yaml")
	if rec.Code != http.StatusOK || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/html") {
		t.Fatalf("got %d %s", rec.Code, rec.Header().Get("Content-Type"))
	}
	for _, want := range []string{
		"<title>Pets</title>",
		`apiDescriptionUrl="/api/specs/apis/pets.yaml"`,
		`router="hash"`,
		`src="/assets/elements/web-components.min.js"`,
		`href="/assets/elements/styles.min.css"`,
	} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Errorf("page does not contain %s", want)
		}
	}
	if strings.Contains(rec.Body.String(), "hideTryIt") {
		t.Error("the default viewer page hides the Try It console")
	}
}

func TestViewerPageHidesTryIt(t *testing.T) {
	h, err := portal.New(portal.Config{Root: fstest.MapFS{"apis/pets.yaml": {Data: []byte(pets)}}, Sections: sections("apis/pets.yaml", "", ""), HideTryIt: true})
	if err != nil {
		t.Fatal(err)
	}
	if body := get(h, "/specs/apis/pets.yaml").Body.String(); !strings.Contains(body, `hideTryIt="true"`) {
		t.Errorf("the viewer page shows the Try It console: %q", body)
	}
}

func TestRawSpec(t *testing.T) {
	files := fstest.MapFS{
		"apis/pets.yaml":   {Data: []byte(pets)},
		"apis/secret.yaml": {Data: []byte("token: hunter2\n")},
		"broken.yaml":      {Data: []byte("openapi: [3.0\n")},
	}
	h := newPortal(t, files, "apis/pets.yaml")

	rec := get(h, "/api/specs/apis/pets.yaml")
	if rec.Code != http.StatusOK || !strings.HasPrefix(rec.Header().Get("Content-Type"), "application/yaml") || rec.Body.String() != pets {
		t.Errorf("configured spec: %d %s %q", rec.Code, rec.Header().Get("Content-Type"), rec.Body)
	}
	for _, path := range []string{"/api/specs/apis/secret.yaml", "/api/specs/apis/../apis/secret.yaml", "/specs/apis/secret.yaml"} {
		if rec := get(h, path); rec.Code == http.StatusOK || strings.Contains(rec.Body.String(), "hunter2") {
			t.Errorf("%s: %d %q", path, rec.Code, rec.Body)
		}
	}

	rec = get(newPortal(t, files, "broken.yaml"), "/api/specs/broken.yaml")
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "broken.yaml") {
		t.Errorf("invalid spec: %d %q", rec.Code, rec.Body)
	}
}

func TestRawSpecLeavesOutUnpublishedParts(t *testing.T) {
	spec := "openapi: 3.0.3\ninfo:\n  title: Pets\n  version: 1.0.0\npaths:\n  /pets:\n    get:\n      operationId: listPets\n    delete:\n      operationId: deleteAllPets\n      x-doNotPublish:\n        - main\ncomponents:\n  schemas:\n    Pet:\n      properties:\n        name:\n          type: string\n        internalNote:\n          type: string\n          x-doNotPublish:\n            - main\n"
	rec := get(newPortal(t, fstest.MapFS{"apis/pets.yaml": {Data: []byte(spec)}}, "apis/pets.yaml"), "/api/specs/apis/pets.yaml")
	body := rec.Body.String()
	if rec.Code != http.StatusOK || !strings.Contains(body, "listPets") || !strings.Contains(body, "name") {
		t.Fatalf("got %d %q", rec.Code, body)
	}
	for _, s := range []string{"deleteAllPets", "internalNote", "x-doNotPublish"} {
		if strings.Contains(body, s) {
			t.Errorf("the raw spec keeps %s", s)
		}
	}
}

func TestViewerPageLeavesOutUnpublishedTitle(t *testing.T) {
	spec := "openapi: 3.0.3\ninfo:\n  title: Internal Billing API\n  x-doNotPublish-title:\n    - main\n  version: 1.0.0\npaths: {}\n"
	body := get(newPortal(t, fstest.MapFS{"apis/pets.yaml": {Data: []byte(spec)}}, "apis/pets.yaml"), "/specs/apis/pets.yaml").Body.String()
	if strings.Contains(body, "Internal Billing API") {
		t.Errorf("the viewer page shows the unpublished title: %q", body)
	}
}

func TestSpecErrorPages(t *testing.T) {
	files := fstest.MapFS{
		"broken.yaml":   {Data: []byte("openapi: [3.0\n")},
		"swagger.yaml":  {Data: []byte("swagger: \"2.0\"\ninfo:\n  title: Pets\n  version: 1.0.0\n")},
		"untitled.yaml": {Data: []byte("openapi: 3.1.0\ninfo:\n  version: 1.0.0\n")},
	}
	for path, reason := range map[string]string{"broken.yaml": "", "swagger.yaml": "Swagger 2.0", "untitled.yaml": "title"} {
		rec := get(newPortal(t, files, path), "/specs/"+path)
		body := rec.Body.String()
		if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(body, path) || !strings.Contains(body, reason) || strings.Contains(body, "<elements-api") {
			t.Errorf("%s: %d %q", path, rec.Code, body)
		}
	}

	rec := get(newPortal(t, files, "gone.yaml"), "/specs/gone.yaml")
	if rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), "gone.yaml") {
		t.Errorf("missing spec: %d %q", rec.Code, rec.Body)
	}
}

func TestElementsAssets(t *testing.T) {
	h := newPortal(t, fstest.MapFS{"apis/pets.yaml": {Data: []byte(pets)}}, "apis/pets.yaml")
	for path, ctype := range map[string]string{
		"/assets/elements/web-components.min.js": "text/javascript",
		"/assets/elements/styles.min.css":        "text/css",
	} {
		rec := get(h, path)
		if rec.Code != http.StatusOK || rec.Body.Len() == 0 || !strings.HasPrefix(rec.Header().Get("Content-Type"), ctype) {
			t.Errorf("%s: %d, %d bytes, %s", path, rec.Code, rec.Body.Len(), rec.Header().Get("Content-Type"))
		}
	}
}

func TestNewRefusesBadChatRoutes(t *testing.T) {
	ok := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
	for _, r := range []portal.Route{
		{Pattern: "GET /chat", Handler: nil},
		{Pattern: "GET /chat/{", Handler: ok},
		{Pattern: "GET /specs/{path...}", Handler: ok},
	} {
		cfg := portal.Config{Root: fstest.MapFS{"api.yaml": {Data: []byte(pets)}}, Sections: sections("api.yaml", "", ""), Chat: []portal.Route{r}}
		if _, err := portal.New(cfg); err == nil {
			t.Errorf("%s with handler %v: no error", r.Pattern, r.Handler)
		}
	}
}
