package main

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bilus/documentation-portal/portal"
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

func TestParseConfigRejectsUnknownFlag(t *testing.T) {
	// -spec was the flag's name before it became -spec-path.
	if _, err := parseConfig([]string{"-spec", "api.yaml"}, noEnv); err == nil || !strings.Contains(err.Error(), "-spec") {
		t.Errorf("err = %v, want an error about -spec", err)
	}
}

func TestParseConfigHelpListsFlags(t *testing.T) {
	_, err := parseConfig([]string{"-h"}, noEnv)
	if err == nil || !strings.Contains(err.Error(), "-spec-path") {
		t.Errorf("err = %v, want the usage with -spec-path", err)
	}
}

func TestStartupRejectsMissingSpecsDir(t *testing.T) {
	_, _, err := startup([]string{"-specs-dir", "testdata/no-such-dir"}, noEnv)
	if err == nil || !strings.Contains(err.Error(), "testdata/no-such-dir") {
		t.Errorf("err = %v, want it to name the directory", err)
	}
}

func TestOpenSpecsKeepsReadsInside(t *testing.T) {
	dir := t.TempDir()
	specs := filepath.Join(dir, "specs")
	if err := os.Mkdir(specs, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string]string{"secret.yaml": "token: hunter2\n", "specs/api.yaml": "openapi: 3.1.0\n"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink("../secret.yaml", filepath.Join(specs, "escape.yaml")); err != nil {
		t.Fatal(err)
	}

	cfg, err := openSpecs(specs, "api.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SpecPath != "api.yaml" {
		t.Errorf("SpecPath = %q", cfg.SpecPath)
	}
	if _, err := fs.ReadFile(cfg.Specs, "api.yaml"); err != nil {
		t.Errorf("api.yaml: %v", err)
	}
	if b, err := fs.ReadFile(cfg.Specs, "escape.yaml"); err == nil {
		t.Errorf("a symlink out of the directory was followed: %q", b)
	}
}

func TestStartupRejectsSpecPathOutsideDir(t *testing.T) {
	_, _, err := startup([]string{"-specs-dir", "../../testdata/specs", "-spec-path", "../specs/petstore-3.0.yaml"}, noEnv)
	if err == nil || !strings.Contains(err.Error(), "../specs/petstore-3.0.yaml") {
		t.Errorf("err = %v, want it to name the spec path", err)
	}
}

func TestStartupServesDocs(t *testing.T) {
	_, h, err := startup([]string{"-specs-dir", "../../testdata/specs", "-spec-path", "petstore-3.0.yaml", "-docs-dir", "../../testdata/docs"}, noEnv)
	if err != nil {
		t.Fatal(err)
	}

	list := get(h, "/docs/")
	if list.Code != http.StatusOK || !strings.HasPrefix(list.Header().Get("Content-Type"), "text/html") || !strings.Contains(list.Body.String(), `href="/docs/guide/intro.md"`) {
		t.Errorf("document list: %d %s %q", list.Code, list.Header().Get("Content-Type"), list.Body)
	}
	page := get(h, "/docs/guide/intro.md")
	if page.Code != http.StatusOK || !strings.HasPrefix(page.Header().Get("Content-Type"), "text/html") || !strings.Contains(page.Body.String(), "Introduction") {
		t.Errorf("document page: %d %s %q", page.Code, page.Header().Get("Content-Type"), page.Body)
	}
}

func TestParseConfigReadsDocsDir(t *testing.T) {
	env := func(k string) string { return map[string]string{"DOCPORTAL_DOCS_DIR": "/srv/docs"}[k] }
	if cfg, err := parseConfig(nil, env); err != nil || cfg.DocsDir != "/srv/docs" {
		t.Errorf("from the environment: %+v, %v", cfg, err)
	}
	if cfg, err := parseConfig([]string{"-docs-dir", "docs"}, env); err != nil || cfg.DocsDir != "docs" {
		t.Errorf("the flag over the environment: %+v, %v", cfg, err)
	}
}

func TestStartupRejectsMissingDocsDir(t *testing.T) {
	_, _, err := startup([]string{"-specs-dir", "../../testdata/specs", "-spec-path", "petstore-3.0.yaml", "-docs-dir", "testdata/no-such-docs"}, noEnv)
	if err == nil || !strings.Contains(err.Error(), "testdata/no-such-docs") {
		t.Errorf("err = %v, want it to name the directory", err)
	}
}

// writeEscape writes dir/secret.md, dir/docs/a.md and dir/docs/escape.md, a
// symlink to the secret, and returns dir/docs.
func writeEscape(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	docs := filepath.Join(dir, "docs")
	if err := os.Mkdir(docs, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string]string{"secret.md": "# hunter2\n", "docs/a.md": "# A\n"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink("../secret.md", filepath.Join(docs, "escape.md")); err != nil {
		t.Fatal(err)
	}
	return docs
}

func TestOpenDocsKeepsReadsInside(t *testing.T) {
	cfg, err := openDocs(portal.Config{SpecPath: "api.yaml"}, writeEscape(t))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SpecPath != "api.yaml" {
		t.Errorf("SpecPath = %q", cfg.SpecPath)
	}
	if _, err := fs.ReadFile(cfg.Docs, "a.md"); err != nil {
		t.Errorf("a.md: %v", err)
	}
	if b, err := fs.ReadFile(cfg.Docs, "escape.md"); err == nil {
		t.Errorf("a symlink out of the directory was followed: %q", b)
	}

	if cfg, err := openDocs(portal.Config{SpecPath: "api.yaml"}, ""); err != nil || cfg.Docs != nil {
		t.Errorf("without a content directory name: %+v, %v", cfg, err)
	}
}

func TestStartupKeepsDocsInside(t *testing.T) {
	_, h, err := startup([]string{"-specs-dir", "../../testdata/specs", "-spec-path", "petstore-3.0.yaml", "-docs-dir", writeEscape(t)}, noEnv)
	if err != nil {
		t.Fatal(err)
	}
	if rec := get(h, "/docs/escape.md"); rec.Code != http.StatusNotFound || strings.Contains(rec.Body.String(), "hunter2") {
		t.Errorf("escape.md: %d %q", rec.Code, rec.Body)
	}
	if list := get(h, "/docs/").Body.String(); strings.Contains(list, "escape.md") || !strings.Contains(list, `href="/docs/a.md"`) {
		t.Errorf("document list: %q", list)
	}
}

func TestParseConfigReadsHideTryIt(t *testing.T) {
	env := func(v string) func(string) string {
		return func(k string) string { return map[string]string{"DOCPORTAL_HIDE_TRY_IT": v}[k] }
	}
	if cfg, err := parseConfig(nil, env("true")); err != nil || !cfg.HideTryIt {
		t.Errorf("from the environment: %+v, %v", cfg, err)
	}
	if cfg, err := parseConfig([]string{"-hide-try-it"}, noEnv); err != nil || !cfg.HideTryIt {
		t.Errorf("from the flag: %+v, %v", cfg, err)
	}
	if cfg, err := parseConfig([]string{"-hide-try-it=false"}, env("1")); err != nil || cfg.HideTryIt {
		t.Errorf("the flag over the environment: %+v, %v", cfg, err)
	}
	if _, err := parseConfig(nil, env("maybe")); err == nil || !strings.Contains(err.Error(), "DOCPORTAL_HIDE_TRY_IT") {
		t.Errorf("err = %v, want it to name DOCPORTAL_HIDE_TRY_IT", err)
	}
}

func TestStartupShowsTryIt(t *testing.T) {
	_, h, err := startup([]string{"-specs-dir", "../../testdata/specs", "-spec-path", "petstore-3.0.yaml"}, noEnv)
	if err != nil {
		t.Fatal(err)
	}
	if body := get(h, "/specs/petstore-3.0.yaml").Body.String(); strings.Contains(body, "hideTryIt") {
		t.Errorf("the viewer page hides the Try It console by default: %q", body)
	}
}

func TestStartupHidesTryIt(t *testing.T) {
	_, h, err := startup([]string{"-specs-dir", "../../testdata/specs", "-spec-path", "petstore-3.0.yaml", "-hide-try-it"}, noEnv)
	if err != nil {
		t.Fatal(err)
	}
	if body := get(h, "/specs/petstore-3.0.yaml").Body.String(); !strings.Contains(body, `hideTryIt="true"`) {
		t.Errorf("the viewer page shows the Try It console: %q", body)
	}
}
