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

func TestCheckConfig(t *testing.T) {
	root := fstest.MapFS{"apis/pets.yaml": {Data: []byte("openapi: 3.1.0\n")}, "docs/a.md": {Data: []byte("# A\n")}}
	if docs, err := checkConfig(Config{Root: root, SpecPath: "apis/pets.yaml"}); err != nil || docs != nil {
		t.Errorf("valid config: %v, %v", docs, err)
	}
	if _, err := checkConfig(Config{SpecPath: "apis/pets.yaml"}); err == nil {
		t.Error("nil Root accepted")
	}
	for _, path := range []string{".", "", "/etc/passwd", "../pets.yaml", "apis/../../pets.yaml"} {
		_, err := checkConfig(Config{Root: root, SpecPath: path})
		if err == nil || !strings.Contains(err.Error(), path) {
			t.Errorf("%q: err = %v", path, err)
		}
	}
	for path, file := range map[string]string{"docs": "a.md", ".": "docs/a.md"} {
		docs, err := checkConfig(Config{Root: root, SpecPath: "apis/pets.yaml", DocsPath: path})
		if err != nil {
			t.Errorf("content directory path %q: %v", path, err)
		} else if _, err := fs.Stat(docs, file); err != nil {
			t.Errorf("content directory %q: %v", path, err)
		}
	}
	for _, path := range []string{"missing", "docs/a.md", "/docs", "../docs", "docs/"} {
		_, err := checkConfig(Config{Root: root, SpecPath: "apis/pets.yaml", DocsPath: path})
		if err == nil || !strings.Contains(err.Error(), path) {
			t.Errorf("content directory path %q: err = %v", path, err)
		}
	}
}

func TestCheckConfigRefusesSymlinkedDocsPath(t *testing.T) {
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
	for _, path := range []string{"docs", "up/sub"} {
		if _, err := checkConfig(Config{Root: root, SpecPath: "api.yaml", DocsPath: path}); err == nil || !strings.Contains(err.Error(), path) {
			t.Errorf("content directory path %q: err = %v, want a refusal", path, err)
		}
	}
	if _, err := checkConfig(Config{Root: root, SpecPath: "api.yaml", DocsPath: "real/guides"}); err != nil {
		t.Errorf("real/guides: %v", err)
	}
	if _, err := checkConfig(Config{Root: root, SpecPath: "api.yaml", DocsPath: "missing"}); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("missing: err = %v, want the cause fs.ErrNotExist", err)
	}
}
