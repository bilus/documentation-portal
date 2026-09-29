package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func noEnv(string) string { return "" }

func get(h http.Handler, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

func TestStartupServesSpec(t *testing.T) {
	addr, h, err := startup([]string{"-specs-dir", "../../testdata/specs", "-spec-path", "petstore-3.0.yaml"}, noEnv)
	if err != nil {
		t.Fatal(err)
	}
	if addr != ":8080" {
		t.Errorf("addr = %q, want :8080", addr)
	}

	page := get(h, "/specs/petstore-3.0.yaml")
	if page.Code != http.StatusOK || !strings.HasPrefix(page.Header().Get("Content-Type"), "text/html") {
		t.Fatalf("viewer page: %d %s", page.Code, page.Header().Get("Content-Type"))
	}
	if want := `apiDescriptionUrl="/api/specs/petstore-3.0.yaml"`; !strings.Contains(page.Body.String(), want) {
		t.Errorf("viewer page does not contain %s", want)
	}

	raw := get(h, "/api/specs/petstore-3.0.yaml")
	want, err := os.ReadFile("../../testdata/specs/petstore-3.0.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if raw.Code != http.StatusOK || !strings.HasPrefix(raw.Header().Get("Content-Type"), "application/yaml") {
		t.Fatalf("raw spec: %d %s", raw.Code, raw.Header().Get("Content-Type"))
	}
	if raw.Body.String() != string(want) {
		t.Error("raw spec differs from the file")
	}
}

func TestParseConfig(t *testing.T) {
	t.Skip("HOLE(1): flags win over the environment, which wins over the defaults")

	cfg, err := parseConfig(nil, noEnv)
	if err != nil {
		t.Fatal(err)
	}
	if want := (config{Addr: ":8080", SpecsDir: ".", SpecPath: "openapi.yaml"}); cfg != want {
		t.Errorf("defaults = %+v, want %+v", cfg, want)
	}

	env := map[string]string{"DOCPORTAL_SPECS_DIR": "/srv/specs", "DOCPORTAL_SPEC_PATH": "api.yaml"}
	cfg, err = parseConfig([]string{"-spec-path", "apis/pets.yaml"}, func(k string) string { return env[k] })
	if err != nil {
		t.Fatal(err)
	}
	if want := (config{Addr: ":8080", SpecsDir: "/srv/specs", SpecPath: "apis/pets.yaml"}); cfg != want {
		t.Errorf("got %+v, want %+v", cfg, want)
	}

	if _, err := parseConfig([]string{"-spec-path", "a.yaml", "b.yaml"}, noEnv); err == nil {
		t.Error("a positional argument was accepted")
	}
}

func TestStartupRejectsMissingSpecsDir(t *testing.T) {
	t.Skip("HOLE(1): startup names a specs directory it cannot open")

	_, _, err := startup([]string{"-specs-dir", "testdata/no-such-dir"}, noEnv)
	if err == nil || !strings.Contains(err.Error(), "testdata/no-such-dir") {
		t.Errorf("err = %v, want it to name the directory", err)
	}
}

func TestStartupRejectsSpecPathOutsideDir(t *testing.T) {
	t.Skip("HOLE(3): startup names a spec path that leaves the specs directory")

	_, _, err := startup([]string{"-specs-dir", "../../testdata/specs", "-spec-path", "../specs/petstore-3.0.yaml"}, noEnv)
	if err == nil || !strings.Contains(err.Error(), "../specs/petstore-3.0.yaml") {
		t.Errorf("err = %v, want it to name the spec path", err)
	}
}
