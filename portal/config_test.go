package portal_test

import (
	"errors"
	"io/fs"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/bilus/documentation-portal/portal"
)

func TestReadConfig(t *testing.T) {
	root := fstest.MapFS{"site/environment.yaml": {Data: []byte(`
sections:
  - title: API
    type: spec
    input: ./OpenAPI.yaml
  - title: Guides
    type: docs
    toc: toc.json
    input: ./docs/
`)}}
	cfg, err := portal.ReadConfig(root, "site/environment.yaml")
	if err != nil {
		t.Fatal(err)
	}
	want := []portal.Section{
		{Title: "API", Type: portal.SpecSection, Input: "./OpenAPI.yaml"},
		{Title: "Guides", Type: portal.DocsSection, Input: "./docs/", Toc: "toc.json"},
	}
	if !reflect.DeepEqual(cfg.Sections, want) {
		t.Errorf("sections = %+v, want %+v", cfg.Sections, want)
	}
	if !reflect.DeepEqual(cfg.Root, fs.FS(root)) || cfg.HideTryIt || cfg.Chat != nil {
		t.Errorf("config = %+v, want root alone besides the sections", cfg)
	}
}

func TestReadConfigReadsEmptyFile(t *testing.T) {
	for _, data := range []string{"", "# nothing yet\n", "sections: []\n"} {
		cfg, err := portal.ReadConfig(fstest.MapFS{"environment.yaml": {Data: []byte(data)}}, "environment.yaml")
		if err != nil || len(cfg.Sections) != 0 {
			t.Errorf("%q: %+v, %v", data, cfg.Sections, err)
		}
	}
}

func TestReadConfigRefuses(t *testing.T) {
	for data, want := range map[string]string{
		"section:\n  - title: API\n":                         "section",
		"sections:\n  - title: API\n    path: api.yaml\n":    "path",
		"sections:\n  - title: API\n    Title: Other\n":      "Title",
		"sections: api.yaml\n":                               "line 1",
		"sections:\n  - api.yaml\n":                          "api.yaml",
		"sections:\n  - title: API\n    title: Other\n":      "title",
		"sections: [\n":                                      "environment.yaml",
		"sections: []\n---\nsections: []\n":                  "more than one",
		"sections: []\n---\nsections: [\n":                   "environment.yaml",
		"sections:\n  - title: API\n    input: [a.yaml]\n":   "line 3",
		"sections:\n  - {title: API, type: spec, toc: {}}\n": "line 2",
	} {
		if _, err := portal.ReadConfig(fstest.MapFS{"environment.yaml": {Data: []byte(data)}}, "environment.yaml"); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: err = %v, want one naming %s", data, err, want)
		}
	}
}

func TestReadConfigRefusesMissingFile(t *testing.T) {
	_, err := portal.ReadConfig(fstest.MapFS{}, "environment.yaml")
	if !errors.Is(err, fs.ErrNotExist) || !strings.Contains(err.Error(), "environment.yaml") {
		t.Errorf("err = %v, want fs.ErrNotExist naming the file", err)
	}
}

func TestReadConfigRefusesNilRoot(t *testing.T) {
	if cfg, err := portal.ReadConfig(nil, "environment.yaml"); err == nil || !strings.Contains(err.Error(), "documentation root") {
		t.Errorf("%+v, %v, want an error about the documentation root", cfg, err)
	}
}
