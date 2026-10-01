package portal

import (
	"errors"
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"
)

func TestAssetsInNamesMissingFiles(t *testing.T) {
	_, err := assetsIn(fstest.MapFS{"elements/.gitkeep": {}, "elements/styles.min.css": {Data: []byte("/**/")}, "elements/LICENSE": {Data: []byte("Apache-2.0")}})
	if err == nil || !strings.Contains(err.Error(), "web-components.min.js") || strings.Contains(err.Error(), "styles.min.css") || !strings.Contains(err.Error(), "make setup") {
		t.Errorf("err = %v, want it to name web-components.min.js alone and make setup", err)
	}

	_, err = assetsIn(fstest.MapFS{
		"elements/web-components.min.js": {Data: []byte("//")},
		"elements/styles.min.css":        {Data: []byte("/**/")},
	})
	if err == nil || !strings.Contains(err.Error(), "LICENSE") {
		t.Errorf("err = %v, want it to name LICENSE", err)
	}

	dir, err := assetsIn(fstest.MapFS{
		"elements/web-components.min.js": {Data: []byte("//")},
		"elements/styles.min.css":        {Data: []byte("/**/")},
		"elements/LICENSE":               {Data: []byte("Apache-2.0")},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fs.Stat(dir, "web-components.min.js"); err != nil {
		t.Errorf("the returned directory does not hold the script: %v", err)
	}
}

// specSection returns a spec section titled API for input.
func specSection(input string) Section {
	return Section{Title: "API", Type: SpecSection, Input: input}
}

// docsSection returns a docs section titled Documents for input, with the
// toc path toc.
func docsSection(input, toc string) Section {
	return Section{Title: "Documents", Type: DocsSection, Input: input, Toc: toc}
}

func TestOpenSections(t *testing.T) {
	root := fstest.MapFS{"apis/pets.yaml": {Data: []byte("openapi: 3.1.0\n")}, "docs/a.md": {Data: []byte("# A\n")}}
	if sections, err := openSections(Config{Root: root, Sections: []Section{specSection("apis/pets.yaml")}}); err != nil || len(sections) != 1 || sections[0].docs != nil {
		t.Errorf("valid config: %v, %v", sections, err)
	}
	if _, err := openSections(Config{Sections: []Section{specSection("apis/pets.yaml")}}); err == nil {
		t.Error("nil Root accepted")
	}
	for _, path := range []string{".", "", "/etc/passwd", "../pets.yaml", "apis/../../pets.yaml"} {
		_, err := openSections(Config{Root: root, Sections: []Section{specSection(path)}})
		if err == nil || !strings.Contains(err.Error(), path) {
			t.Errorf("%q: err = %v", path, err)
		}
	}
	for path, file := range map[string]string{"docs": "a.md", ".": "docs/a.md"} {
		sections, err := openSections(Config{Root: root, Sections: []Section{specSection("apis/pets.yaml"), docsSection(path, "")}})
		if err != nil {
			t.Errorf("content directory path %q: %v", path, err)
		} else if _, err := fs.Stat(sections[1].docs, file); err != nil {
			t.Errorf("content directory %q: %v", path, err)
		}
	}
	for _, path := range []string{"missing", "docs/a.md", "/docs", "../docs", "docs/"} {
		_, err := openSections(Config{Root: root, Sections: []Section{specSection("apis/pets.yaml"), docsSection(path, "")}})
		if err == nil || !strings.Contains(err.Error(), path) {
			t.Errorf("content directory path %q: err = %v", path, err)
		}
	}
}

func TestOpenSectionsRefusesSymlinkedDocsPath(t *testing.T) {
	link := func(target string) *fstest.MapFile {
		return &fstest.MapFile{Data: []byte(target), Mode: fs.ModeSymlink}
	}
	root := fstest.MapFS{
		"api.yaml":             {Data: []byte("openapi: 3.1.0\n")},
		"elsewhere/a.md":       {Data: []byte("# A\n")},
		"elsewhere/sub/b.md":   {Data: []byte("# B\n")},
		"docs":                 link("elsewhere"),
		"up":                   link("elsewhere"),
		"real/guides/intro.md": {Data: []byte("# Intro\n")},
	}
	open := func(path string) error {
		_, err := openSections(Config{Root: root, Sections: []Section{specSection("api.yaml"), docsSection(path, "")}})
		return err
	}
	for _, path := range []string{"docs", "up/sub"} {
		if err := open(path); err == nil || !strings.Contains(err.Error(), path) {
			t.Errorf("content directory path %q: err = %v, want a refusal", path, err)
		}
	}
	if err := open("real/guides"); err != nil {
		t.Errorf("real/guides: %v", err)
	}
	if err := open("missing"); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("missing: err = %v, want the cause fs.ErrNotExist", err)
	}
}

func TestOpenSectionsChecksTocPath(t *testing.T) {
	root := fstest.MapFS{"api.yaml": {Data: []byte("openapi: 3.0.3\n")}, "docs/a.md": {Data: []byte("# A\n")}}
	for _, p := range []string{"../toc.json", "/toc.json", ".", "nav/../toc.json", "nav//toc.json"} {
		if _, err := openSections(Config{Root: root, Sections: []Section{specSection("api.yaml"), docsSection("docs", p)}}); err == nil || !strings.Contains(err.Error(), "toc path") {
			t.Errorf("%q: err = %v, want one about the toc path", p, err)
		}
	}
	// A missing toc file is no error at startup: the sidebar falls back.
	for _, p := range []string{"", "toc.json", "nav/toc.json"} {
		if _, err := openSections(Config{Root: root, Sections: []Section{specSection("api.yaml"), docsSection("docs", p)}}); err != nil {
			t.Errorf("%q: %v", p, err)
		}
	}
	// A toc file lays out the sidebar of the document pages, which need a
	// content directory.
	spec := specSection("api.yaml")
	spec.Toc = "toc.json"
	if _, err := openSections(Config{Root: root, Sections: []Section{spec}}); err == nil || !strings.Contains(err.Error(), "content directory") {
		t.Errorf("a toc on a spec section: err = %v", err)
	}
}
