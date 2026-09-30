package portal

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
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
		sp, err := (&site{root: specs, specPath: path}).loadSpec(path)
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
		_, err := (&site{root: specs, specPath: path}).loadSpec(path)
		var invalid invalidSpecError
		if !errors.As(err, &invalid) || !strings.Contains(invalid.reason, reason) {
			t.Errorf("%s: err = %v, want an invalid spec mentioning %s", path, err, reason)
		}
	}

	if _, err := (&site{root: specs, specPath: "gone.yaml"}).loadSpec("gone.yaml"); !errors.Is(err, errNoSpec) {
		t.Errorf("missing file: err = %v", err)
	}
	if _, err := (&site{root: specs, specPath: "v30.yaml"}).loadSpec("v31.yaml"); !errors.Is(err, errNoSpec) {
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
	(&site{root: fstest.MapFS{}, specPath: "api.yaml"}).docList(rec, httptest.NewRequest(http.MethodGet, "/docs/", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("got %d, want 404", rec.Code)
	}
}

func TestDocPageWithoutDocs(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/docs/a.md", nil)
	req.SetPathValue("path", "a.md")
	(&site{root: fstest.MapFS{}, specPath: "api.yaml"}).docPage(rec, req)
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

func TestRawFileWithoutDocs(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/raw/a.png", nil)
	req.SetPathValue("path", "a.png")
	(&site{root: fstest.MapFS{}, specPath: "api.yaml"}).rawFile(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("got %d, want 404", rec.Code)
	}
}

func TestSidebarGroups(t *testing.T) {
	docs := fstest.MapFS{"b.md": {}, "a/x.md": {}, "a/sub/y.md": {}, "a/z.md": {}, "c/w.md": {}, ".h/v.md": {}}
	got := (&site{docs: docs}).sidebar("a/z.md")
	want := []sidebarGroup{
		{Links: []sidebarLink{{Title: "b.md", URL: "/docs/b.md"}}},
		{Title: "a", Links: []sidebarLink{{Title: "x.md", URL: "/docs/a/x.md"}, {Title: "z.md", URL: "/docs/a/z.md", Current: true}}},
		{Title: "a/sub", Links: []sidebarLink{{Title: "y.md", URL: "/docs/a/sub/y.md"}}},
		{Title: "c", Links: []sidebarLink{{Title: "w.md", URL: "/docs/c/w.md"}}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v\nwant %+v", got, want)
	}
	if got := (&site{}).sidebar(""); got != nil {
		t.Errorf("without a content directory: %+v", got)
	}
}

func TestLinkURL(t *testing.T) {
	root := fstest.MapFS{
		"api.yaml":       {Data: []byte("openapi: 3.1.0\n")},
		"a.md":           {Data: []byte("# A\n")},
		"guide/intro.md": {Data: []byte("# Intro\n")},
		"my guide.md":    {Data: []byte("# Mine\n")},
	}
	s := &site{root: root, specPath: "api.yaml", docsPath: ".", docs: root}
	for _, tc := range []struct{ doc, dest, want string }{
		{"a.md", "guide/intro.md?x=1#y", "/docs/guide/intro.md?x=1#y"},
		{"a.md", "./guide/intro.md", "/docs/guide/intro.md"},
		{"guide/intro.md", "../a.md", "/docs/a.md"},
		{"a.md", "my%20guide.md", "/docs/my%20guide.md"},
		{"a.md", "%zz", "%zz"},
		{"a.md", "?page=2", "?page=2"},
	} {
		got, ok := s.linkURL(tc.doc, []byte(tc.dest))
		if !ok || string(got) != tc.want {
			t.Errorf("%s in %s: %q %v, want %q", tc.dest, tc.doc, got, ok, tc.want)
		}
	}
}
