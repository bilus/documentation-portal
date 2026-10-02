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
portals:
  - name: Delivery
    sections:
      - title: API
        type: spec
        input: ./OpenAPI.yaml
      - title: Guides
        type: docs
        toc: toc.json
        input: ./docs/
  - name: Billing
    sections:
      - {title: API, type: spec, input: billing.yaml}
`)}}
	cfg, err := portal.ReadConfig(root, "site/environment.yaml")
	if err != nil {
		t.Fatal(err)
	}
	want := []portal.Portal{
		{Name: "Delivery", Sections: []portal.Section{
			{Title: "API", Type: portal.SpecSection, Input: "./OpenAPI.yaml"},
			{Title: "Guides", Type: portal.DocsSection, Input: "./docs/", Toc: "toc.json"},
		}},
		{Name: "Billing", Sections: []portal.Section{{Title: "API", Type: portal.SpecSection, Input: "billing.yaml"}}},
	}
	if !reflect.DeepEqual(cfg.Portals, want) {
		t.Errorf("portals = %+v, want %+v", cfg.Portals, want)
	}
	if !reflect.DeepEqual(cfg.Root, fs.FS(root)) || cfg.HideTryIt || cfg.Chat != nil {
		t.Errorf("config = %+v, want root alone besides the portals", cfg)
	}
}

func TestReadConfigReadsEmptyFile(t *testing.T) {
	for _, data := range []string{"", "# nothing yet\n", "portals: []\n"} {
		cfg, err := portal.ReadConfig(fstest.MapFS{"environment.yaml": {Data: []byte(data)}}, "environment.yaml")
		if err != nil || len(cfg.Portals) != 0 {
			t.Errorf("%q: %+v, %v", data, cfg.Portals, err)
		}
	}
}

func TestReadConfigRefuses(t *testing.T) {
	for data, want := range map[string]string{
		"portal:\n  - name: Pets\n":               "portal",
		"sections:\n  - title: API\n":             "sections",
		"portals:\n  - name: Pets\n    path: x\n": "path",
		"portals:\n  - name: Pets\n    sections:\n      - title: API\n        path: a\n": "path",
		"portals:\n  - name: Pets\n    Name: Other\n":                                    "Name",
		"portals: pets.yaml\n":                               "line 1",
		"portals:\n  - pets\n":                               "pets",
		"portals:\n  - name: Pets\n    name: Other\n":        "name",
		"portals: [\n":                                       "environment.yaml",
		"portals: []\n---\nportals: []\n":                    "more than one",
		"portals: []\n---\nportals: [\n":                     "environment.yaml",
		"portals:\n  - name: Pets\n    sections: api.yaml\n": "line 3",
		"portals:\n  - name: Pets\n    sections:\n      - {title: API, type: spec, toc: {}}\n": "line 4",
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

func TestReadConfigRefusesEmptyEntries(t *testing.T) {
	for data, want := range map[string]string{
		"portals:\n  - name: One\n    sections: [{title: A, type: spec, input: a.yaml}]\n  -\n": "portal 2 is empty",
		"portals:\n  - ~\n": "portal 1 is empty",
		"portals:\n  - name: One\n    sections:\n      - {title: A, type: spec, input: a.yaml}\n      -\n": `portal "One": section 2 is empty`,
	} {
		if cfg, err := portal.ReadConfig(fstest.MapFS{"environment.yaml": {Data: []byte(data)}}, "environment.yaml"); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: %+v, %v, want an error naming %s", data, cfg.Portals, err, want)
		}
	}
}
