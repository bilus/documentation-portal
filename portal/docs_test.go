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

func newDocsPortal(t *testing.T, docs fs.FS) http.Handler {
	t.Helper()
	h, err := portal.New(portal.Config{Specs: fstest.MapFS{"apis/pets.yaml": {Data: []byte(pets)}}, SpecPath: "apis/pets.yaml", Docs: docs})
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
	t.Skip("HOLE(1): list the markdown files of the content directory")
	rec := get(newDocsPortal(t, sampleDocs), "/docs/")
	if rec.Code != http.StatusOK || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/html") {
		t.Fatalf("got %d %s", rec.Code, rec.Header().Get("Content-Type"))
	}
	body := rec.Body.String()
	var at []int
	for _, doc := range []string{"README.md", "guide/intro.md", "unsafe.md"} {
		i := strings.Index(body, `href="/docs/`+doc+`"`)
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
	t.Skip("HOLE(2): render a markdown file as its document page")
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
		`href="../README.md"`,
		`src="/raw/guide/diagram.png"`,
	} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Errorf("the page does not contain %s", want)
		}
	}
}

func TestDocPageDropsActiveContent(t *testing.T) {
	t.Skip("HOLE(2): drop the active content of the markdown file")
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
	t.Skip("HOLE(2): read the markdown file on every request")
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
	t.Skip("HOLE(2): answer 404 for a path without a markdown file")
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
	t.Skip("HOLE(3): serve the images of the content directory")
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

func TestPagesShareNavigation(t *testing.T) {
	t.Skip("HOLE(3): show the navigation bar on every page")
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
	t.Skip("HOLE(4): show the document sidebar on the document list and the document pages")
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
