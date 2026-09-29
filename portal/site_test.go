package portal

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
)

func TestLoadSpec(t *testing.T) {
	specs := fstest.MapFS{
		"v30.yaml":      {Data: []byte("openapi: 3.0.3\ninfo:\n  title: Pets\n  version: 1.0.0\npaths: {}\n")},
		"v31.yaml":      {Data: []byte("openapi: 3.1.0\ninfo:\n  title: Pets\n  version: 2.0.0\n")},
		"broken.yaml":   {Data: []byte("openapi: [3.0\n")},
		"workflow.yaml": {Data: []byte("name: ci\non: push\njobs: {}\n")},
		"swagger.yaml":  {Data: []byte("swagger: \"2.0\"\ninfo:\n  title: Pets\n  version: 1.0.0\n")},
		"untitled.yaml": {Data: []byte("openapi: 3.1.0\ninfo:\n  version: 1.0.0\n")},
		// Unquoted, 3.1 is a YAML float and no OpenAPI version.
		"float.yaml": {Data: []byte("openapi: 3.1\ninfo:\n  title: Pets\n")},
	}

	for _, path := range []string{"v30.yaml", "v31.yaml"} {
		sp, err := (&site{specs: specs, specPath: path}).loadSpec(path)
		if err != nil || sp.Title != "Pets" || string(sp.Raw) != string(specs[path].Data) {
			t.Errorf("%s: %+v, %v", path, sp, err)
		}
	}

	for path, reason := range map[string]string{
		"broken.yaml":   "YAML",
		"workflow.yaml": "OpenAPI",
		"swagger.yaml":  "Swagger 2.0",
		"untitled.yaml": "title",
		"float.yaml":    "OpenAPI",
	} {
		_, err := (&site{specs: specs, specPath: path}).loadSpec(path)
		var invalid invalidSpecError
		if !errors.As(err, &invalid) || !strings.Contains(invalid.reason, reason) {
			t.Errorf("%s: err = %v, want an invalid spec mentioning %s", path, err, reason)
		}
	}

	if _, err := (&site{specs: specs, specPath: "gone.yaml"}).loadSpec("gone.yaml"); !errors.Is(err, errNoSpec) {
		t.Errorf("missing file: err = %v", err)
	}
	if _, err := (&site{specs: specs, specPath: "v30.yaml"}).loadSpec("v31.yaml"); !errors.Is(err, errNoSpec) {
		t.Errorf("a spec other than the configured one: err = %v", err)
	}
}

func TestMarkdownFiles(t *testing.T) {
	docs := fstest.MapFS{
		"a.md":       {},
		"a/b.md":     {},
		"a-b.md":     {},
		"b.markdown": {},
		"c.txt":      {},
		".x.md":      {},
		".d/e.md":    {},
		"f/.g.md":    {},
	}
	got, err := markdownFiles(docs)
	if err != nil {
		t.Fatal(err)
	}
	// Byte order puts "-" and "." before "/", unlike a directory walk.
	if want := []string{"a-b.md", "a.md", "a/b.md", "b.markdown"}; !slices.Equal(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestDocListWithoutDocs(t *testing.T) {
	rec := httptest.NewRecorder()
	(&site{specs: fstest.MapFS{}, specPath: "api.yaml"}).docList(rec, httptest.NewRequest(http.MethodGet, "/docs/", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("got %d, want 404", rec.Code)
	}
}

func TestDocPageWithoutDocs(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/docs/a.md", nil)
	req.SetPathValue("path", "a.md")
	(&site{specs: fstest.MapFS{}, specPath: "api.yaml"}).docPage(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("got %d, want 404", rec.Code)
	}
}

func TestRawImageURL(t *testing.T) {
	for dest, want := range map[string]string{
		"diagram.png":               "/raw/guide/diagram.png",
		"../logo.svg":               "/raw/logo.svg",
		"img/a b.png":               "/raw/guide/img/a%20b.png",
		"../../outside.png":         "../../outside.png",
		"/static/logo.png":          "/static/logo.png",
		"https://example.com/a.png": "https://example.com/a.png",
		"data:image/png;base64,AA":  "data:image/png;base64,AA",
	} {
		if got := string(rawImageURL("guide", []byte(dest))); got != want {
			t.Errorf("%s: got %s, want %s", dest, got, want)
		}
	}
}
