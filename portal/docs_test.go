package portal_test

import (
	"io/fs"
	"net/http"
	"os"
	"sort"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/bilus/documentation-portal/portal"
)

var sampleDocs = os.DirFS("../testdata/docs")

// docsRoot is a documentation root that holds the spec apis/pets.yaml and
// serves docs as its content directory docs/.
type docsRoot struct{ docs fs.FS }

func (r docsRoot) Open(name string) (fs.File, error) {
	if name == "docs" {
		return r.docs.Open(".")
	}
	if rest, ok := strings.CutPrefix(name, "docs/"); ok {
		return r.docs.Open(rest)
	}
	return fstest.MapFS{"apis/pets.yaml": {Data: []byte(pets)}, "docs": {Mode: fs.ModeDir}}.Open(name)
}

func newDocsPortal(t *testing.T, docs fs.FS) http.Handler {
	t.Helper()
	h, err := portal.New(portal.Config{Root: docsRoot{docs}, SpecPath: "apis/pets.yaml", DocsPath: "docs"})
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func TestNoDocsWithoutContentDir(t *testing.T) {
	h := newPortal(t, fstest.MapFS{"apis/pets.yaml": {Data: []byte(pets)}}, "apis/pets.yaml")
	for _, path := range []string{"/docs/", "/docs/README.md", "/raw/guide/diagram.png"} {
		if rec := get(h, path); rec.Code != http.StatusNotFound {
			t.Errorf("%s: %d", path, rec.Code)
		}
	}
}

func TestDocList(t *testing.T) {
	rec := get(newDocsPortal(t, sampleDocs), "/docs/")
	if rec.Code != http.StatusOK || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/html") {
		t.Fatalf("got %d %s", rec.Code, rec.Header().Get("Content-Type"))
	}
	body := rec.Body.String()
	list := body[max(strings.Index(body, "<ul>"), 0):]
	var at []int
	for _, doc := range []string{"README.md", "guide/intro.md", "unsafe.md"} {
		i := strings.Index(list, `href="/docs/`+doc+`"`)
		if i < 0 {
			t.Errorf("the list does not link %s", doc)
		}
		at = append(at, i)
	}
	if !sort.IntsAreSorted(at) {
		t.Errorf("the list is not sorted by path: links at %v", at)
	}
	for _, name := range []string{".hidden.md", "draft.md", "notes.txt", "page.html", "evil.svg", "diagram.png"} {
		if strings.Contains(body, name) {
			t.Errorf("the list shows %s", name)
		}
	}
}

func TestDocPage(t *testing.T) {
	rec := get(newDocsPortal(t, sampleDocs), "/docs/guide/intro.md")
	if rec.Code != http.StatusOK || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/html") {
		t.Fatalf("got %d %s", rec.Code, rec.Header().Get("Content-Type"))
	}
	for _, want := range []string{
		"<h1",
		"Introduction</h1>",
		"<em>markdown</em>",
		"<li>one</li>",
		"<pre><code",
		`href="/docs/README.md"`,
		`src="/raw/guide/diagram.png"`,
	} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Errorf("the page does not contain %s", want)
		}
	}
}

func TestDocPageDropsActiveContent(t *testing.T) {
	rec := get(newDocsPortal(t, sampleDocs), "/docs/unsafe.md")
	body := rec.Body.String()
	if rec.Code != http.StatusOK || !strings.Contains(body, "Click me") {
		t.Fatalf("got %d %q", rec.Code, body)
	}
	for _, bad := range []string{"<script", "javascript:"} {
		if strings.Contains(body, bad) {
			t.Errorf("the page contains %s", bad)
		}
	}
}

func TestDocPageReadsEachRequest(t *testing.T) {
	docs := fstest.MapFS{"a.md": {Data: []byte("# First\n")}}
	h := newDocsPortal(t, docs)
	if body := get(h, "/docs/a.md").Body.String(); !strings.Contains(body, "First") {
		t.Errorf("before the edit: %q", body)
	}

	docs["a.md"] = &fstest.MapFile{Data: []byte("# Second\n")}
	docs["b.md"] = &fstest.MapFile{Data: []byte("# Added\n")}
	if body := get(h, "/docs/a.md").Body.String(); !strings.Contains(body, "Second") {
		t.Errorf("after the edit: %q", body)
	}
	if body := get(h, "/docs/").Body.String(); !strings.Contains(body, `href="/docs/b.md"`) {
		t.Errorf("the list after the addition: %q", body)
	}

	delete(docs, "a.md")
	if rec := get(h, "/docs/a.md"); rec.Code != http.StatusNotFound {
		t.Errorf("after the removal: %d", rec.Code)
	}
}

func TestDocsNotFound(t *testing.T) {
	h := newDocsPortal(t, sampleDocs)
	for _, path := range []string{"missing.md", "notes.txt", "page.html", ".hidden.md", ".drafts/draft.md", "guide", "guide/"} {
		rec := get(h, "/docs/"+path)
		if rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), path) {
			t.Errorf("%s: %d %q", path, rec.Code, rec.Body)
		}
	}
	for _, path := range []string{"/docs/../api/specs/apis/pets.yaml", "/docs//etc/passwd"} {
		if rec := get(h, path); rec.Code == http.StatusOK {
			t.Errorf("%s: %d %q", path, rec.Code, rec.Body)
		}
	}
}

func TestRawFile(t *testing.T) {
	h := newDocsPortal(t, sampleDocs)
	for path, ctype := range map[string]string{"guide/diagram.png": "image/png", "evil.svg": "image/svg+xml"} {
		rec := get(h, "/raw/"+path)
		want, err := fs.ReadFile(sampleDocs, path)
		if err != nil {
			t.Fatal(err)
		}
		if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != ctype || rec.Body.String() != string(want) {
			t.Errorf("%s: %d %s", path, rec.Code, rec.Header().Get("Content-Type"))
		}
		if rec.Header().Get("X-Content-Type-Options") != "nosniff" || rec.Header().Get("Content-Security-Policy") != "sandbox" {
			t.Errorf("%s: headers %v", path, rec.Header())
		}
	}
	for _, path := range []string{"page.html", "notes.txt", "README.md", "missing.png"} {
		rec := get(h, "/raw/"+path)
		if rec.Code != http.StatusNotFound || strings.HasPrefix(rec.Header().Get("Content-Type"), "text/html") {
			t.Errorf("%s: %d %s", path, rec.Code, rec.Header().Get("Content-Type"))
		}
	}
}

func TestDocPageSidebarFollowsToc(t *testing.T) {
	t.Skip("HOLE(1): lay out the document sidebar from the toc file")
	root := fstest.MapFS{
		"apis/pets.yaml": {Data: []byte(pets)},
		"toc.json": {Data: []byte(`{"items": [{"type": "item", "title": "Second", "uri": "docs/b.md"},` +
			` {"type": "item", "title": "First", "uri": "docs/a.md"}, {"type": "item", "title": "Reference", "uri": "apis/pets.yaml"}]}`)},
		"docs/a.md":        {Data: []byte("# A\n")},
		"docs/b.md":        {Data: []byte("# B\n")},
		"docs/unlisted.md": {Data: []byte("# Unlisted\n")},
	}
	h, err := portal.New(portal.Config{Root: root, SpecPath: "apis/pets.yaml", DocsPath: "docs", TocPath: "toc.json"})
	if err != nil {
		t.Fatal(err)
	}
	page := get(h, "/docs/a.md").Body.String()
	second, first, ref := strings.Index(page, `title="Second"`), strings.Index(page, `title="First"`), strings.Index(page, `title="Reference"`)
	if second < 0 || first < second || ref < first {
		t.Errorf("the sidebar is not in the toc file's order: Second at %d, First at %d, Reference at %d", second, first, ref)
	}
	if strings.Contains(page, `href="/docs/unlisted.md"`) {
		t.Error("the sidebar links a markdown file missing from the toc file")
	}
	if rec := get(h, "/docs/unlisted.md"); rec.Code != http.StatusOK {
		t.Errorf("a markdown file missing from the toc file: %d", rec.Code)
	}
	if list := get(h, "/docs/").Body.String(); !strings.Contains(list, "unlisted.md") {
		t.Error("the document list left out a markdown file missing from the toc file")
	}
}

func TestPagesShareNavigation(t *testing.T) {
	h := newDocsPortal(t, fstest.MapFS{"a.md": {Data: []byte("# A\n")}})
	for _, path := range []string{"/specs/apis/pets.yaml", "/docs/", "/docs/a.md", "/docs/missing.md"} {
		body := get(h, path).Body.String()
		for _, link := range []string{`href="/specs/apis/pets.yaml"`, `href="/docs/"`} {
			if !strings.Contains(body, link) {
				t.Errorf("%s does not link %s", path, link)
			}
		}
	}

	body := get(newPortal(t, fstest.MapFS{"apis/pets.yaml": {Data: []byte(pets)}}, "apis/pets.yaml"), "/specs/apis/pets.yaml").Body.String()
	if strings.Contains(body, `href="/docs/"`) {
		t.Error("the viewer page links the document list without a content directory")
	}
}

func TestDocSidebar(t *testing.T) {
	h := newDocsPortal(t, sampleDocs)

	page := get(h, "/docs/guide/intro.md").Body.String()
	if n := strings.Count(page, "ElementsTableOfContentsItem"); n != 3 {
		t.Errorf("the sidebar has %d items, want one per markdown file: 3", n)
	}
	if !strings.Contains(page, ">guide</div>") {
		t.Error("the sidebar has no group for the guide directory")
	}
	for _, name := range []string{".hidden.md", "draft.md"} {
		if strings.Contains(page, name) {
			t.Errorf("the sidebar shows %s", name)
		}
	}
	link := strings.Index(page, `href="/docs/guide/intro.md"`)
	end := strings.Index(page[max(link, 0):], "</a>")
	if link < 0 || end < 0 || !strings.Contains(page[link:link+end], "sl-bg-primary-tint") || strings.Count(page, "sl-bg-primary-tint") != 1 {
		t.Error("the sidebar does not mark the open document, and it alone")
	}

	list := get(h, "/docs/").Body.String()
	if strings.Count(list, "ElementsTableOfContentsItem") != 3 || strings.Contains(list, "sl-bg-primary-tint") {
		t.Errorf("the document list's sidebar: %q", list)
	}
}

func TestContentDirFollowsNoSymlink(t *testing.T) {
	spec := "openapi: 3.0.3\ninfo:\n  title: Pets\n  version: 1.0.0\npaths:\n  /pets:\n    delete:\n      operationId: secretDeleteAll\n      x-doNotPublish:\n        - main\n"
	link := func(target string) *fstest.MapFile {
		return &fstest.MapFile{Data: []byte(target), Mode: fs.ModeSymlink}
	}
	for _, tc := range []struct {
		name, docsPath string
		root           fstest.MapFS
		gone           []string
	}{
		{"below the root", "docs", fstest.MapFS{
			"api.yaml":       {Data: []byte(spec)},
			"private.md":     {Data: []byte("# hunter3\n")},
			"private.png":    {Data: []byte("hunter4")},
			"docs/a.md":      {Data: []byte("# A\n")},
			"docs/inside.md": link("../private.md"),
			"docs/leak.png":  link("../private.png"),
			"docs/spec.md":   link("../api.yaml"),
			"docs/latest.md": link("a.md"),
		}, []string{"/docs/inside.md", "/docs/spec.md", "/docs/latest.md", "/raw/leak.png"}},
		{"at the root", ".", fstest.MapFS{
			"api.yaml":    {Data: []byte(spec)},
			"a.md":        {Data: []byte("# A\n")},
			"notes.md":    link("api.yaml"),
			"diagram.svg": link("api.yaml"),
		}, []string{"/docs/notes.md", "/raw/diagram.svg"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, err := portal.New(portal.Config{Root: tc.root, SpecPath: "api.yaml", DocsPath: tc.docsPath})
			if err != nil {
				t.Fatal(err)
			}
			for _, path := range tc.gone {
				rec := get(h, path)
				if body := rec.Body.String(); rec.Code != http.StatusNotFound || strings.Contains(body, "hunter") || strings.Contains(body, "secretDeleteAll") {
					t.Errorf("%s: %d %q", path, rec.Code, body)
				}
			}
			list := get(h, "/docs/").Body.String()
			for _, path := range tc.gone {
				if name := path[strings.LastIndex(path, "/")+1:]; strings.Contains(list, name) {
					t.Errorf("the document list shows %s", name)
				}
			}
			if !strings.Contains(list, `href="/docs/a.md"`) {
				t.Error("the document list lost a.md")
			}
		})
	}
}

// untypedFS reports every directory entry with type 0, as a filesystem that
// does not track its entries' types might.
type untypedFS struct{ fstest.MapFS }

func (u untypedFS) ReadDir(name string) ([]fs.DirEntry, error) {
	entries, err := u.MapFS.ReadDir(name)
	for i, e := range entries {
		entries[i] = untypedEntry{e}
	}
	return entries, err
}

type untypedEntry struct{ fs.DirEntry }

func (untypedEntry) Type() fs.FileMode { return 0 }

func TestDocListLstatsEachFile(t *testing.T) {
	root := untypedFS{fstest.MapFS{
		"api.yaml": {Data: []byte(pets)},
		"a.md":     {Data: []byte("# A\n")},
		"notes.md": {Data: []byte("api.yaml"), Mode: fs.ModeSymlink},
	}}
	h, err := portal.New(portal.Config{Root: root, SpecPath: "api.yaml", DocsPath: "."})
	if err != nil {
		t.Fatal(err)
	}
	if list := get(h, "/docs/").Body.String(); strings.Contains(list, "notes.md") || !strings.Contains(list, `href="/docs/a.md"`) {
		t.Errorf("document list: %q", list)
	}
	if rec := get(h, "/docs/notes.md"); rec.Code != http.StatusNotFound {
		t.Errorf("/docs/notes.md: %d", rec.Code)
	}
}
