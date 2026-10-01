package portal

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
)

// apiSection returns a spec section titled API, with the slug api, for input.
func apiSection(input string) *section {
	return &section{Section: Section{Title: "API", Type: SpecSection, Input: input}, slug: "api"}
}

// docsOf returns a docs section titled Documents, with the slug documents,
// for the content directory docs at input, with the toc path toc.
func docsOf(input string, docs fs.FS, toc string) *section {
	return &section{Section: Section{Title: "Documents", Type: DocsSection, Input: input, Toc: toc}, slug: "documents", docs: docs}
}

func TestSpecFor(t *testing.T) {
	s := &site{sections: []*section{
		{Section: Section{Title: "Guides", Type: DocsSection, Input: "docs"}, slug: "guides"},
		apiSection("api.yaml"),
		{Section: Section{Title: "API", Type: SpecSection, Input: "other.yaml"}, slug: "api"},
	}}
	if sec, ok := s.specFor("api"); !ok || sec != s.sections[1] {
		t.Errorf("api: %+v, %v, want the first spec section with the slug", sec, ok)
	}
	for _, slug := range []string{"guides", "API", "", "api.yaml"} {
		if sec, ok := s.specFor(slug); ok {
			t.Errorf("%q: %+v, want no spec section", slug, sec)
		}
	}
}

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
		sp, err := (&site{root: specs, sections: []*section{apiSection(path)}}).loadSpec(path)
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
		_, err := (&site{root: specs, sections: []*section{apiSection(path)}}).loadSpec(path)
		var invalid invalidSpecError
		if !errors.As(err, &invalid) || !strings.Contains(invalid.reason, reason) {
			t.Errorf("%s: err = %v, want an invalid spec mentioning %s", path, err, reason)
		}
	}

	if _, err := (&site{root: specs, sections: []*section{apiSection("gone.yaml")}}).loadSpec("gone.yaml"); !errors.Is(err, errNoSpec) {
		t.Errorf("missing file: err = %v", err)
	}
	if _, err := (&site{root: specs, sections: []*section{apiSection("v30.yaml")}}).loadSpec("v31.yaml"); !errors.Is(err, errNoSpec) {
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
	(&site{root: fstest.MapFS{}, sections: []*section{apiSection("api.yaml")}}).docList(rec, httptest.NewRequest(http.MethodGet, "/docs/", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("got %d, want 404", rec.Code)
	}
}

func TestDocPageWithoutDocs(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/docs/a.md", nil)
	req.SetPathValue("path", "a.md")
	(&site{root: fstest.MapFS{}, sections: []*section{apiSection("api.yaml")}}).docPage(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("got %d, want 404", rec.Code)
	}
}

func TestRawImageURL(t *testing.T) {
	for dest, want := range map[string]string{
		"diagram.png":               "/raw/documents/guide/diagram.png",
		"../logo.svg":               "/raw/documents/logo.svg",
		"img/a b.png":               "/raw/documents/guide/img/a%20b.png",
		"../../outside.png":         "../../outside.png",
		"/static/logo.png":          "/static/logo.png",
		"https://example.com/a.png": "https://example.com/a.png",
		"data:image/png;base64,AA":  "data:image/png;base64,AA",
	} {
		if got := string(rawImageURL("documents", "guide", []byte(dest))); got != want {
			t.Errorf("%s: got %s, want %s", dest, got, want)
		}
	}
}

func TestRawFileWithoutDocs(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/raw/a.png", nil)
	req.SetPathValue("path", "a.png")
	(&site{root: fstest.MapFS{}, sections: []*section{apiSection("api.yaml")}}).rawFile(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("got %d, want 404", rec.Code)
	}
}

func TestSidebarGroups(t *testing.T) {
	docs := fstest.MapFS{"b.md": {}, "a/x.md": {}, "a/sub/y.md": {}, "a/z.md": {}, "c/w.md": {}, ".h/v.md": {}}
	got := (&site{}).sidebar(docsOf(".", docs, ""), "a/z.md")
	want := []sidebarGroup{
		{Links: []sidebarLink{{Title: "b.md", URL: "/docs/documents/b.md"}}},
		{Title: "a", Links: []sidebarLink{{Title: "x.md", URL: "/docs/documents/a/x.md"}, {Title: "z.md", URL: "/docs/documents/a/z.md", Current: true}}},
		{Title: "a/sub", Links: []sidebarLink{{Title: "y.md", URL: "/docs/documents/a/sub/y.md"}}},
		{Title: "c", Links: []sidebarLink{{Title: "w.md", URL: "/docs/documents/c/w.md"}}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v\nwant %+v", got, want)
	}
	if got := (&site{}).sidebar(apiSection("api.yaml"), ""); got != nil {
		t.Errorf("of a spec section: %+v", got)
	}
}

func TestTocSidebar(t *testing.T) {
	toc := `{"items": [
		{"type": "item", "title": "Getting started", "uri": "docs/start.md"},
		{"type": "item", "title": "API reference", "uri": "api.yaml"},
		{"type": "item", "title": "Status", "uri": "https://status.example.com/"},
		{"type": "item", "title": "Hidden", "uri": "docs/.draft.md"},
		{"type": "item", "title": "Missing", "uri": "docs/missing.md"},
		{"type": "item", "title": "Outside the content directory", "uri": "notes.md"},
		{"type": "item", "title": "Outside the root", "uri": "../secret.md"},
		{"type": "item", "title": "Mail", "uri": "mailto:team@example.com"},
		{"type": "item", "title": "Host only", "uri": "//example.com/a.md"},
		{"type": "group", "title": "Guides", "items": [
			{"type": "item", "title": "OAuth", "uri": "/docs/guides/oauth.md#scopes"},
			{"type": "group", "title": "More", "items": [
				{"type": "item", "title": "Devices", "uri": "docs/guides/devices.md"}
			]}
		]},
		{"type": "divider", "title": "Reference"},
		{"type": "item", "title": "Pets", "uri": "api.yaml/paths/~1pets/get"}
	]}`
	root := fstest.MapFS{
		"toc.json":               {Data: []byte(toc)},
		"api.yaml":               {Data: []byte("openapi: 3.0.3\ninfo:\n  title: Pets\n  version: 1.0.0\npaths:\n  /pets:\n    get:\n      operationId: listPets\n")},
		"notes.md":               {Data: []byte("# Notes\n")},
		"docs/start.md":          {Data: []byte("# Start\n")},
		"docs/.draft.md":         {Data: []byte("# Draft\n")},
		"docs/guides/oauth.md":   {Data: []byte("# OAuth\n")},
		"docs/guides/devices.md": {Data: []byte("# Devices\n")},
		"docs/unlisted.md":       {Data: []byte("# Unlisted\n")},
	}
	docs, err := fs.Sub(root, "docs")
	if err != nil {
		t.Fatal(err)
	}
	sec := docsOf("docs", docs, "toc.json")
	s := &site{root: root, sections: []*section{apiSection("api.yaml"), sec}}
	want := []sidebarGroup{
		{Links: []sidebarLink{{Title: "Getting started", URL: "/docs/documents/start.md"}, {Title: "API reference", URL: "/specs/api"}, {Title: "Status", URL: "https://status.example.com/"}}},
		{Title: "Guides", Links: []sidebarLink{{Title: "OAuth", URL: "/docs/documents/guides/oauth.md#scopes", Current: true}, {Title: "Devices", URL: "/docs/documents/guides/devices.md"}}},
		{Title: "Reference", Links: []sidebarLink{{Title: "Pets", URL: "/specs/api#/operations/listPets"}}},
	}
	if got := s.sidebar(sec, "guides/oauth.md"); !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v\nwant %+v", got, want)
	}
}

// countedFS counts the opens of each name of fsys. It provides Open alone,
// so that every read of fsys opens a file.
type countedFS struct {
	fsys  fs.FS
	opens map[string]int
}

func (c *countedFS) Open(name string) (fs.File, error) {
	c.opens[name]++
	return c.fsys.Open(name)
}

func TestTocSidebarReadsOnce(t *testing.T) {
	files := fstest.MapFS{"api.yaml": {Data: []byte("openapi: 3.0.3\ninfo:\n  title: Pets\n  version: 1.0.0\npaths:\n  /pets:\n    get:\n      operationId: listPets\n")}}
	var items []string
	for i := range 20 {
		files[fmt.Sprintf("docs/g%02d.md", i)] = &fstest.MapFile{Data: []byte("# G\n")}
		items = append(items, fmt.Sprintf(`{"type": "item", "title": "G%d", "uri": "docs/g%02d.md"}`, i, i),
			`{"type": "item", "title": "Pets", "uri": "api.yaml/paths/~1pets/get"}`)
	}
	files["toc.json"] = &fstest.MapFile{Data: []byte(`{"items": [` + strings.Join(items, ", ") + `]}`)}
	sub, err := fs.Sub(files, "docs")
	if err != nil {
		t.Fatal(err)
	}
	root, docs := &countedFS{files, map[string]int{}}, &countedFS{sub, map[string]int{}}
	sec := docsOf("docs", docs, "toc.json")
	s := &site{root: root, sections: []*section{apiSection("api.yaml"), sec}}
	if groups := s.sidebar(sec, "g00.md"); len(groups) != 1 || len(groups[0].Links) != 40 {
		t.Fatalf("groups %+v", groups)
	}
	// One listing opens the directory twice: fs.WalkDir stats it, then reads it.
	if n := docs.opens["."]; n > 2 {
		t.Errorf("one sidebar opened the content directory %d times, want one listing, which opens it twice", n)
	}
	if n := root.opens["api.yaml"]; n > 2 {
		t.Errorf("one sidebar opened the spec %d times, want at most twice, to check it and to read it", n)
	}
}

func TestTocLink(t *testing.T) {
	root := fstest.MapFS{
		"api.yaml":             {Data: []byte("openapi: 3.0.3\ninfo:\n  title: Pets\n  version: 1.0.0\npaths:\n  /pets:\n    get:\n      operationId: listPets\n")},
		"notes.md":             {Data: []byte("# Notes\n")},
		"docs/start.md":        {Data: []byte("# Start\n")},
		"docs/.draft.md":       {Data: []byte("# Draft\n")},
		"docs/guides/oauth.md": {Data: []byte("# OAuth\n")},
		"docs/a#b.md":          {Data: []byte("# A\n")},
	}
	docs, err := fs.Sub(root, "docs")
	if err != nil {
		t.Fatal(err)
	}
	sec := docsOf("docs", docs, "")
	s := &site{root: root, sections: []*section{apiSection("api.yaml"), sec}}
	p, err := s.newTocPages(sec, "guides/oauth.md")
	if err != nil {
		t.Fatal(err)
	}
	for uri, want := range map[string]string{
		"docs/start.md":             "/docs/documents/start.md",
		"/docs/start.md":            "/docs/documents/start.md",
		"docs/./start.md":           "/docs/documents/start.md",
		"docs/start.md?x=1#a":       "/docs/documents/start.md?x=1#a",
		"docs/guides/oauth.md":      "/docs/documents/guides/oauth.md",
		"api.yaml":                  "/specs/api",
		"api.yaml/paths/~1pets/get": "/specs/api#/operations/listPets",
		"https://example.com/a?b#c": "https://example.com/a?b#c",
		"http://example.com":        "http://example.com",
		"docs/a%23b.md":             "/docs/documents/a%23b.md",
	} {
		got, ok := p.link("T", uri)
		if wantLink := (sidebarLink{Title: "T", URL: want, Current: uri == "docs/guides/oauth.md"}); !ok || got != wantLink {
			t.Errorf("%s: %+v, %v, want %+v", uri, got, ok, wantLink)
		}
	}
	for _, uri := range []string{"docs/.draft.md", "docs/missing.md", "notes.md", "../secret.md", "docs/../../secret.md",
		"mailto:team@example.com", "//example.com/a.md", "ftp://example.com/a", "https:/no-host", "javascript:alert(1)", "%zz", "docs/",
		"ftp:/docs/start.md", "https:/docs/start.md"} {
		if got, ok := p.link("T", uri); ok {
			t.Errorf("%s: %+v, want no link", uri, got)
		}
	}
	if got, ok := p.link("", "docs/start.md"); ok {
		t.Errorf("an empty title: %+v, want no link", got)
	}
}

func TestTocGroups(t *testing.T) {
	root := fstest.MapFS{"a.md": {}, "b.md": {}, "c.md": {}, "d.md": {}, "e.md": {}, "f.md": {}}
	sec := docsOf(".", root, "")
	s := &site{root: root, sections: []*section{apiSection("api.yaml"), sec}}
	p, err := s.newTocPages(sec, "c.md")
	if err != nil {
		t.Fatal(err)
	}
	item := func(name string) tocEntry {
		return tocEntry{Type: "item", Title: strings.ToUpper(name), URI: name + ".md"}
	}
	link := func(name string) sidebarLink {
		return sidebarLink{Title: strings.ToUpper(name), URL: "/docs/documents/" + name + ".md", Current: name == "c"}
	}
	entries := []tocEntry{
		item("a"),
		{Type: "item", Title: "Gone", URI: "gone.md"},
		{Type: "group", Title: "G", Items: []tocEntry{item("b"), {Type: "group", Title: "Inner", Items: []tocEntry{item("c")}}, {Type: "divider", Title: "Skipped"}}},
		item("d"),
		{Type: "group", Title: "Empty", Items: []tocEntry{{Type: "item", Title: "Gone", URI: "gone.md"}}},
		{Type: "divider", Title: "D"},
		item("e"),
		{Type: "divider", Title: "Nothing after it"},
		{Type: "group", Title: "H", Items: []tocEntry{item("f")}},
	}
	want := []sidebarGroup{
		{Links: []sidebarLink{link("a")}},
		{Title: "G", Links: []sidebarLink{link("b"), link("c")}},
		{Links: []sidebarLink{link("d")}},
		{Title: "D", Links: []sidebarLink{link("e")}},
		{Title: "Nothing after it"},
		{Title: "H", Links: []sidebarLink{link("f")}},
	}
	if got := p.groups(entries); !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v\nwant %+v", got, want)
	}
	if got := p.groups(nil); got != nil {
		t.Errorf("no entries: %+v", got)
	}
	top, err := s.newTocPages(sec, "a.md")
	if err != nil {
		t.Fatal(err)
	}
	if got := top.groups([]tocEntry{item("a")}); len(got) != 1 || !got[0].Links[0].Current {
		t.Errorf("a top-level item of the current page: %+v", got)
	}
}

func TestReadToc(t *testing.T) {
	toc := `{"items": [{"type": "item", "title": "A", "uri": "docs/a.md", "slug": "a"},` +
		` {"type": "group", "title": "G", "items": [{"type": "divider", "title": "D"}]}]}`
	root := fstest.MapFS{
		"nav/toc.json": {Data: []byte(toc)},
		"empty.json":   {Data: []byte(`{"items": []}`)},
		"nested.json":  {Data: []byte(`{"items": [{"type": "group", "title": "G", "items": [{"type": "item", "title": "A"}]}]}`)},
		"group.json":   {Data: []byte(`{"items": [{"type": "group", "items": []}]}`)},
		"divider.json": {Data: []byte(`{"items": [{"type": "divider"}]}`)},
		"twice.json":   {Data: []byte(`{"items": []} {"items": []}`)},
		"bom.json":     {Data: []byte("\ufeff" + toc)},
		"linked":       {Data: []byte("nav"), Mode: fs.ModeSymlink},
	}
	want := []tocEntry{
		{Type: "item", Title: "A", URI: "docs/a.md"},
		{Type: "group", Title: "G", Items: []tocEntry{{Type: "divider", Title: "D"}}},
	}
	if got, err := readToc(root, "nav/toc.json"); err != nil || !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, %v\nwant %+v", got, err, want)
	}
	if got, err := readToc(root, "bom.json"); err != nil || !reflect.DeepEqual(got, want) {
		t.Errorf("with a byte order mark: %+v, %v", got, err)
	}
	if got, err := readToc(root, "empty.json"); err != nil || len(got) != 0 {
		t.Errorf("no entries: %+v, %v", got, err)
	}
	for _, p := range []string{"nested.json", "group.json", "divider.json", "twice.json", "linked/toc.json", "nav", "missing.json"} {
		if got, err := readToc(root, p); err == nil || got != nil {
			t.Errorf("%s: %+v, %v, want no entries and an error", p, got, err)
		}
	}
}

func TestTocSidebarFallsBack(t *testing.T) {
	var logged bytes.Buffer
	prev := log.Writer()
	log.SetOutput(&logged)
	t.Cleanup(func() { log.SetOutput(prev) })
	for name, tc := range map[string]struct {
		toc     *fstest.MapFile
		problem string
	}{
		"missing":          {nil, "not exist"},
		"not JSON":         {&fstest.MapFile{Data: []byte("{")}, "unexpected end of JSON input"},
		"no items":         {&fstest.MapFile{Data: []byte(`{"title": "Docs"}`)}, "no items"},
		"unknown type":     {&fstest.MapFile{Data: []byte(`{"items": [{"type": "link", "title": "A", "uri": "docs/a.md"}]}`)}, "unknown type"},
		"no title":         {&fstest.MapFile{Data: []byte(`{"items": [{"type": "item", "uri": "docs/a.md"}]}`)}, "no title"},
		"item without uri": {&fstest.MapFile{Data: []byte(`{"items": [{"type": "item", "title": "A"}]}`)}, "no uri"},
		"symlink":          {&fstest.MapFile{Data: []byte("other.json"), Mode: fs.ModeSymlink}, "symlink"},
		"no page served":   {&fstest.MapFile{Data: []byte(`{"items": [{"type": "item", "title": "Gone", "uri": "docs/gone.md"}]}`)}, "names no page"},
		"only other sites": {&fstest.MapFile{Data: []byte(`{"items": [{"type": "item", "title": "Status", "uri": "https://status.example.com/"}]}`)}, "names no page"},
	} {
		logged.Reset()
		root := fstest.MapFS{
			"docs/a.md":  {Data: []byte("# A\n")},
			"other.json": {Data: []byte(`{"items": [{"type": "item", "title": "A", "uri": "docs/a.md"}]}`)},
		}
		if tc.toc != nil {
			root["toc.json"] = tc.toc
		}
		docs, err := fs.Sub(root, "docs")
		if err != nil {
			t.Fatal(err)
		}
		sec := docsOf("docs", docs, "toc.json")
		s := &site{root: root, sections: []*section{sec}}
		want := []sidebarGroup{{Links: []sidebarLink{{Title: "a.md", URL: "/docs/documents/a.md", Current: true}}}}
		if got := s.sidebar(sec, "a.md"); !reflect.DeepEqual(got, want) {
			t.Errorf("%s: got %+v", name, got)
		}
		if line := logged.String(); !strings.Contains(line, "toc.json") || !strings.Contains(line, tc.problem) {
			t.Errorf("%s: the log %q names no toc file or no %q", name, line, tc.problem)
		}
	}
}

func TestTocSidebarLogsAProblemOnce(t *testing.T) {
	var logged bytes.Buffer
	prev := log.Writer()
	log.SetOutput(&logged)
	t.Cleanup(func() { log.SetOutput(prev) })
	root := fstest.MapFS{"docs/a.md": {Data: []byte("# A\n")}}
	docs, err := fs.Sub(root, "docs")
	if err != nil {
		t.Fatal(err)
	}
	sec := docsOf("docs", docs, "toc.json")
	s := &site{root: root, sections: []*section{sec}}
	for range 3 {
		s.sidebar(sec, "a.md")
	}
	if n := strings.Count(logged.String(), "\n"); n != 1 {
		t.Errorf("3 pages with a missing toc file logged %d lines, want 1:\n%s", n, logged.String())
	}
	// Fixed, then missing again: the problem comes back, and the log says so.
	root["toc.json"] = &fstest.MapFile{Data: []byte(`{"items": [{"type": "item", "title": "A", "uri": "docs/a.md"}]}`)}
	s.sidebar(sec, "a.md")
	delete(root, "toc.json")
	s.sidebar(sec, "a.md")
	s.sidebar(sec, "a.md")
	root["toc.json"] = &fstest.MapFile{Data: []byte("{")}
	s.sidebar(sec, "a.md")
	if n := strings.Count(logged.String(), "\n"); n != 3 {
		t.Errorf("after a fix, the same problem and a new one, %d lines, want 3:\n%s", n, logged.String())
	}
}

// failingFS fails every open.
type failingFS struct{}

func (failingFS) Open(name string) (fs.File, error) {
	return nil, &fs.PathError{Op: "open", Path: name, Err: errors.New("disk on fire")}
}

func TestTocSidebarLogsAFailedListing(t *testing.T) {
	var logged bytes.Buffer
	prev := log.Writer()
	log.SetOutput(&logged)
	t.Cleanup(func() { log.SetOutput(prev) })
	root := fstest.MapFS{"toc.json": {Data: []byte(`{"items": [{"type": "item", "title": "A", "uri": "docs/a.md"}]}`)}}
	sec := docsOf("docs", failingFS{}, "toc.json")
	s := &site{root: root, sections: []*section{sec}}
	if got := s.sidebar(sec, "a.md"); got != nil {
		t.Errorf("got %+v", got)
	}
	if !strings.Contains(logged.String(), "disk on fire") {
		t.Errorf("the log %q names no failed listing", logged.String())
	}
}

func TestLinkURL(t *testing.T) {
	root := fstest.MapFS{
		"api.yaml":       {Data: []byte("openapi: 3.1.0\n")},
		"a.md":           {Data: []byte("# A\n")},
		"guide/intro.md": {Data: []byte("# Intro\n")},
		"my guide.md":    {Data: []byte("# Mine\n")},
	}
	sec := docsOf(".", root, "")
	s := &site{root: root, sections: []*section{apiSection("api.yaml"), sec}}
	for _, tc := range []struct{ doc, dest, want string }{
		{"a.md", "guide/intro.md?x=1#y", "/docs/documents/guide/intro.md?x=1#y"},
		{"a.md", "./guide/intro.md", "/docs/documents/guide/intro.md"},
		{"guide/intro.md", "../a.md", "/docs/documents/a.md"},
		{"a.md", "my%20guide.md", "/docs/documents/my%20guide.md"},
		{"a.md", "?page=2", "?page=2"},
	} {
		got, ok := s.linkURL(sec, tc.doc, []byte(tc.dest))
		if !ok || string(got) != tc.want {
			t.Errorf("%s in %s: %q %v, want %q", tc.dest, tc.doc, got, ok, tc.want)
		}
	}
	if got, ok := s.linkURL(sec, "a.md", []byte("%zz")); ok {
		t.Errorf("%%zz leads to %q, want nowhere", got)
	}
}

func TestOperationRoute(t *testing.T) {
	spec := []byte("openapi: 3.0.3\ninfo:\n  title: Radio\n  version: 1.0.0\npaths:\n" +
		"  /devices:\n    parameters: []\n    post:\n      operationId: registerDevice\n" +
		"  /apps/links/{appLinkId}:\n    get:\n      summary: Retrieve an app link\n" +
		"  /a/{b}/c/{d}:\n    get:\n      summary: Two parameters\n" +
		"  /a~b:\n    get:\n      operationId: tilde\n")
	for pointer, want := range map[string]string{
		"paths/~1devices/post":                 "/operations/registerDevice",
		"paths/~1apps~1links~1{appLinkId}/get": "/paths/apps-links-appLinkId/get",
		// Elements collapses the first run of dashes only.
		"paths/~1a~1{b}~1c~1{d}/get": "/paths/a-b--c--d/get",
		"paths/~1a~0b/get":           "/operations/tilde",
	} {
		if got, ok := operationRoute(spec, pointer); !ok || got != want {
			t.Errorf("%s: %q %v, want %q", pointer, got, ok, want)
		}
	}
	for _, pointer := range []string{"paths/~1devices/get", "paths/~1devices/parameters", "paths/~1cats/get", "paths/~1devices", "paths/~1devices/post/responses", "components/schemas/Device", ""} {
		if got, ok := operationRoute(spec, pointer); ok {
			t.Errorf("%s: %q, want no operation", pointer, got)
		}
	}
}

func TestOperationRouteEdges(t *testing.T) {
	spec := []byte("openapi: 3.0.3\ninfo:\n  title: Radio\n  version: 1.0.0\npaths:\n" +
		"  /a~1b:\n    get:\n      operationId: tildeOne\n" +
		"  /x/{a}/:\n    get:\n      summary: A trailing slash\n")
	for pointer, want := range map[string]string{
		// ~01 decodes to ~1, since ~1 is replaced before ~0.
		"paths/~1a~01b/get": "/operations/tildeOne",
		// Elements trims one dash from each end, so x-a- keeps its last one.
		"paths/~1x~1{a}~1/get": "/paths/x-a-/get",
	} {
		if got, ok := operationRoute(spec, pointer); !ok || got != want {
			t.Errorf("%s: %q %v, want %q", pointer, got, ok, want)
		}
	}
}

func TestSpecURLEdges(t *testing.T) {
	root := fstest.MapFS{
		"api.yaml":    {Data: []byte("openapi: 3.1.0\ninfo:\n  title: Radio\n")},
		"api.yaml.md": {Data: []byte("# Notes on the API\n")},
		"a.md":        {Data: []byte("# A\n")},
	}
	sec := docsOf(".", root, "")
	s := &site{root: root, sections: []*section{apiSection("api.yaml"), sec}}
	if got, ok := s.linkURL(sec, "a.md", []byte("api.yaml.md")); !ok || string(got) != "/docs/documents/api.yaml.md" {
		t.Errorf("api.yaml.md: %q %v, want its document page", got, ok)
	}
	if got, ok := s.specURL("./api.yaml"); !ok || got != "/specs/api" {
		t.Errorf("specURL(./api.yaml): %q %v", got, ok)
	}
	if _, got, ok := s.docAt("./guide/../a.md"); !ok || got != "a.md" {
		t.Errorf("docAt(./guide/../a.md): %q %v", got, ok)
	}
}

func TestOperationRouteYAML(t *testing.T) {
	spec := []byte("openapi: 3.1.0\ninfo:\n  title: Pets\n  version: 1.0.0\n" +
		"x-items:\n  pets: &pets\n    get:\n      operationId: listPets\n" +
		"paths:\n" +
		"  /pets: *pets\n" +
		"  /nulls:\n    get:\n      operationId: null\n" +
		"  /empty:\n    get:\n      operationId: \"\"\n" +
		"  /pets/{pet id}:\n    get:\n      summary: A space\n" +
		"webhooks:\n  /pets:\n    get:\n      operationId: petHook\n")
	for pointer, want := range map[string]string{
		"paths/~1pets/get":           "/operations/listPets",
		"paths/~1nulls/get":          "/paths/nulls/get",
		"paths/~1empty/get":          "/paths/empty/get",
		"paths/~1pets~1{pet id}/get": "/paths/pets-pet-id/get",
	} {
		if got, ok := operationRoute(spec, pointer); !ok || got != want {
			t.Errorf("%s: %q %v, want %q", pointer, got, ok, want)
		}
	}
	if got, ok := operationRoute(spec, "webhooks/~1pets/get"); ok {
		t.Errorf("webhooks/~1pets/get: %q, want no operation", got)
	}
}

func TestOperationRoutePathItemRef(t *testing.T) {
	spec := []byte("openapi: 3.1.0\ninfo:\n  title: Pets\n  version: 1.0.0\n" +
		"components:\n  pathItems:\n" +
		"    Pets:\n      get:\n        operationId: listPets\n      post:\n        summary: Add a pet\n" +
		"    Hop:\n      $ref: '#/components/pathItems/Pets'\n" +
		"    A:\n      $ref: '#/components/pathItems/B'\n" +
		"    B:\n      $ref: '#/components/pathItems/A'\n" +
		"paths:\n" +
		"  /pets:\n    $ref: '#/components/pathItems/Pets'\n" +
		"  /hop:\n    $ref: '#/components/pathItems/Hop'\n" +
		"  /loop:\n    $ref: '#/components/pathItems/A'\n" +
		"  /gone:\n    $ref: '#/components/pathItems/Missing'\n" +
		"  /external:\n    $ref: 'other.yaml#/components/pathItems/Pets'\n")
	for pointer, want := range map[string]string{
		"paths/~1pets/get":  "/operations/listPets",
		"paths/~1pets/post": "/paths/pets/post",
		"paths/~1hop/get":   "/operations/listPets",
	} {
		if got, ok := operationRoute(spec, pointer); !ok || got != want {
			t.Errorf("%s: %q %v, want %q", pointer, got, ok, want)
		}
	}
	for _, pointer := range []string{"paths/~1loop/get", "paths/~1gone/get", "paths/~1external/get"} {
		if got, ok := operationRoute(spec, pointer); ok {
			t.Errorf("%s: %q, want no operation", pointer, got)
		}
	}
}

func TestSpecPart(t *testing.T) {
	spec := func(title, input string) *section {
		return &section{Section: Section{Title: title, Type: SpecSection, Input: input}, slug: strings.ToLower(title)}
	}
	s := &site{sections: []*section{
		{Section: Section{Title: "Guides", Type: DocsSection, Input: "specs/api.yaml"}, slug: "guides"},
		spec("Dir", "specs"),
		spec("API", "specs/api.yaml"),
		spec("Again", "specs/api.yaml"),
	}}
	for target, want := range map[string]struct{ slug, pointer string }{
		"specs/api.yaml":                  {"api", ""},
		"./specs/api.yaml":                {"api", ""},
		"specs/api.yaml/paths/~1pets/get": {"api", "paths/~1pets/get"},
		"specs/other.yaml":                {"dir", "other.yaml"},
		"specs":                           {"dir", ""},
	} {
		sec, pointer, ok := s.specPart(target)
		if !ok || sec.slug != want.slug || pointer != want.pointer {
			t.Errorf("%s: %+v, %q, %v, want %+v", target, sec, pointer, ok, want)
		}
	}
	for _, target := range []string{"spec", "specs.yaml", "api.yaml", "", "."} {
		if sec, pointer, ok := s.specPart(target); ok {
			t.Errorf("%q: %+v, %q, want no spec section", target, sec, pointer)
		}
	}
}

func TestTocLinkAcrossSpecs(t *testing.T) {
	spec := func(path, id string) *fstest.MapFile {
		return &fstest.MapFile{Data: []byte("openapi: 3.0.3\ninfo:\n  title: T\n  version: 1.0.0\npaths:\n  " + path + ":\n    get:\n      operationId: " + id + "\n")}
	}
	root := fstest.MapFS{"pets.yaml": spec("/pets", "listPets"), "store.yaml": spec("/orders", "listOrders"), "docs/a.md": {Data: []byte("# A\n")}}
	docs, err := fs.Sub(root, "docs")
	if err != nil {
		t.Fatal(err)
	}
	s := &site{root: root, sections: []*section{
		{Section: Section{Title: "Pets", Type: SpecSection, Input: "pets.yaml"}, slug: "pets"},
		{Section: Section{Title: "Store", Type: SpecSection, Input: "store.yaml"}, slug: "store"},
		{Section: Section{Title: "Gone", Type: SpecSection, Input: "gone.yaml"}, slug: "gone"},
		docsOf("docs", docs, ""),
	}}
	p, err := s.newTocPages(s.sections[3], "a.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ uri, want string }{
		{"pets.yaml/paths/~1pets/get", "/specs/pets#/operations/listPets"},
		{"store.yaml/paths/~1orders/get", "/specs/store#/operations/listOrders"},
		{"store.yaml", "/specs/store"},
		{"pets.yaml", "/specs/pets"},
	} {
		if got, ok := p.link("T", c.uri); !ok || got.URL != c.want {
			t.Errorf("%s: %+v, %v, want %s", c.uri, got, ok, c.want)
		}
	}
	if got, ok := p.link("T", "gone.yaml"); ok {
		t.Errorf("a spec section without its file: %+v", got)
	}
}

func TestSpecURLAcrossSpecs(t *testing.T) {
	root := fstest.MapFS{"pets.yaml": {Data: []byte("openapi: 3.0.3\ninfo:\n  title: Pets\n  version: 1.0.0\npaths:\n  /pets:\n    get:\n      operationId: listPets\n")}}
	s := &site{root: root, sections: []*section{
		{Section: Section{Title: "Gone", Type: SpecSection, Input: "gone.yaml"}, slug: "gone"},
		{Section: Section{Title: "Pets", Type: SpecSection, Input: "pets.yaml"}, slug: "pets"},
	}}
	if got, ok := s.specURL("pets.yaml/paths/~1pets/get"); !ok || got != "/specs/pets#/operations/listPets" {
		t.Errorf("pets.yaml: %q, %v", got, ok)
	}
	if got, ok := s.specURL("gone.yaml"); ok {
		t.Errorf("a spec section without its file: %q", got)
	}
}

func TestDocsFor(t *testing.T) {
	s := &site{sections: []*section{apiSection("api.yaml"), docsOf("docs", fstest.MapFS{}, "")}}
	if sec, ok := s.docsFor("documents"); !ok || sec != s.sections[1] {
		t.Errorf("documents: %+v, %v", sec, ok)
	}
	for _, slug := range []string{"api", "Documents", "", "docs"} {
		if sec, ok := s.docsFor(slug); ok {
			t.Errorf("%q: %+v, want no docs section", slug, sec)
		}
	}
}

func TestTocLinkAcrossDocs(t *testing.T) {
	root := fstest.MapFS{"guides/a.md": {Data: []byte("# A\n")}, "ops/c.md": {Data: []byte("# C\n")}}
	guides, err := fs.Sub(root, "guides")
	if err != nil {
		t.Fatal(err)
	}
	ops, err := fs.Sub(root, "ops")
	if err != nil {
		t.Fatal(err)
	}
	s := &site{root: root, sections: []*section{
		{Section: Section{Title: "Guides", Type: DocsSection, Input: "guides"}, slug: "guides", docs: guides},
		{Section: Section{Title: "Ops", Type: DocsSection, Input: "ops"}, slug: "ops", docs: ops},
	}}
	p, err := s.newTocPages(s.sections[0], "a.md")
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := p.link("C", "ops/c.md"); !ok || got.URL != "/docs/ops/c.md" || got.Current {
		t.Errorf("a file of another docs section: %+v, %v", got, ok)
	}
	if got, ok := p.link("A", "ops/a.md"); ok {
		t.Errorf("a file that only the page's own section holds, under another section's path: %+v", got)
	}
}

func TestTocProblemsPerSection(t *testing.T) {
	var logged bytes.Buffer
	prev := log.Writer()
	log.SetOutput(&logged)
	t.Cleanup(func() { log.SetOutput(prev) })
	root := fstest.MapFS{"a/x.md": {Data: []byte("# X\n")}, "b/y.md": {Data: []byte("# Y\n")}}
	a, err := fs.Sub(root, "a")
	if err != nil {
		t.Fatal(err)
	}
	b, err := fs.Sub(root, "b")
	if err != nil {
		t.Fatal(err)
	}
	s := &site{root: root, sections: []*section{
		{Section: Section{Title: "A", Type: DocsSection, Input: "a", Toc: "a.json"}, slug: "a", docs: a},
		{Section: Section{Title: "B", Type: DocsSection, Input: "b", Toc: "b.json"}, slug: "b", docs: b},
	}}
	for range 2 {
		s.sidebar(s.sections[0], "x.md")
		s.sidebar(s.sections[1], "y.md")
	}
	if n := strings.Count(logged.String(), "\n"); n != 2 {
		t.Errorf("two sections with missing toc files, each shown twice, logged %d lines, want one each:\n%s", n, logged.String())
	}
}
