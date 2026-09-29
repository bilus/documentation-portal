package portal

import (
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"
)

func TestAssetsInNamesMissingFiles(t *testing.T) {
	_, err := assetsIn(fstest.MapFS{"elements/.gitkeep": {}, "elements/styles.min.css": {Data: []byte("/**/")}})
	if err == nil || !strings.Contains(err.Error(), "web-components.min.js") || strings.Contains(err.Error(), "styles.min.css") || !strings.Contains(err.Error(), "make setup") {
		t.Errorf("err = %v, want it to name web-components.min.js alone and make setup", err)
	}

	dir, err := assetsIn(fstest.MapFS{
		"elements/web-components.min.js": {Data: []byte("//")},
		"elements/styles.min.css":        {Data: []byte("/**/")},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fs.Stat(dir, "web-components.min.js"); err != nil {
		t.Errorf("the returned directory does not hold the script: %v", err)
	}
}

func TestCheckConfig(t *testing.T) {
	specs := fstest.MapFS{"apis/pets.yaml": {Data: []byte("openapi: 3.1.0\n")}}
	if err := checkConfig(Config{Specs: specs, SpecPath: "apis/pets.yaml"}); err != nil {
		t.Errorf("valid config: %v", err)
	}
	if err := checkConfig(Config{SpecPath: "apis/pets.yaml"}); err == nil {
		t.Error("nil Specs accepted")
	}
	for _, path := range []string{".", "", "/etc/passwd", "../pets.yaml", "apis/../../pets.yaml"} {
		err := checkConfig(Config{Specs: specs, SpecPath: path})
		if err == nil || !strings.Contains(err.Error(), path) {
			t.Errorf("%q: err = %v", path, err)
		}
	}
}
