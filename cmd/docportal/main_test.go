package main

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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
	addr, h, err := startup([]string{"-root-dir", "../../testdata/specs", "-spec-path", "petstore-3.0.yaml"}, noEnv)
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
	if want := (config{Addr: ":8080", RootDir: ".", SpecPath: "openapi.yaml"}); cfg != want {
		t.Errorf("defaults = %+v, want %+v", cfg, want)
	}

	env := map[string]string{"DOCPORTAL_ROOT_DIR": "/srv/docs", "DOCPORTAL_SPEC_PATH": "api.yaml"}
	cfg, err = parseConfig([]string{"-spec-path", "apis/pets.yaml"}, func(k string) string { return env[k] })
	if err != nil {
		t.Fatal(err)
	}
	if want := (config{Addr: ":8080", RootDir: "/srv/docs", SpecPath: "apis/pets.yaml"}); cfg != want {
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

func TestStartupRejectsMissingRootDir(t *testing.T) {
	_, _, err := startup([]string{"-root-dir", "testdata/no-such-dir"}, noEnv)
	if err == nil || !strings.Contains(err.Error(), "testdata/no-such-dir") {
		t.Errorf("err = %v, want it to name the directory", err)
	}
}

func TestStartupRejectsSpecPathOutsideDir(t *testing.T) {
	_, _, err := startup([]string{"-root-dir", "../../testdata/specs", "-spec-path", "../specs/petstore-3.0.yaml"}, noEnv)
	if err == nil || !strings.Contains(err.Error(), "../specs/petstore-3.0.yaml") {
		t.Errorf("err = %v, want it to name the spec path", err)
	}
}

func TestStartupServesDocs(t *testing.T) {
	_, h, err := startup([]string{"-root-dir", "../../testdata", "-spec-path", "specs/petstore-3.0.yaml", "-docs-path", "docs"}, noEnv)
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

func TestParseConfigReadsDocsPath(t *testing.T) {
	env := func(k string) string { return map[string]string{"DOCPORTAL_DOCS_PATH": "guides"}[k] }
	if cfg, err := parseConfig(nil, env); err != nil || cfg.DocsPath != "guides" {
		t.Errorf("from the environment: %+v, %v", cfg, err)
	}
	if cfg, err := parseConfig([]string{"-docs-path", "docs"}, env); err != nil || cfg.DocsPath != "docs" {
		t.Errorf("the flag over the environment: %+v, %v", cfg, err)
	}
}

func TestParseConfigReadsTocPath(t *testing.T) {
	if cfg, err := parseConfig(nil, noEnv); err != nil || cfg.TocPath != "" {
		t.Errorf("by default: %+v, %v", cfg, err)
	}
	env := func(k string) string { return map[string]string{"DOCPORTAL_TOC_PATH": "toc.json"}[k] }
	if cfg, err := parseConfig(nil, env); err != nil || cfg.TocPath != "toc.json" {
		t.Errorf("from the environment: %+v, %v", cfg, err)
	}
	if cfg, err := parseConfig([]string{"-toc-path", "nav/toc.json"}, env); err != nil || cfg.TocPath != "nav/toc.json" {
		t.Errorf("the flag over the environment: %+v, %v", cfg, err)
	}
}

func TestStartupRejectsTocPathOutsideRoot(t *testing.T) {
	_, _, err := startup([]string{"-root-dir", writeEscape(t), "-spec-path", "api.yaml", "-docs-path", "docs", "-toc-path", "../toc.json"}, noEnv)
	if err == nil || !strings.Contains(err.Error(), "toc path") {
		t.Errorf("err = %v, want one about the toc path", err)
	}
}

func TestStartupRejectsMissingDocsPath(t *testing.T) {
	_, _, err := startup([]string{"-root-dir", "../../testdata/specs", "-spec-path", "petstore-3.0.yaml", "-docs-path", "no-such-docs"}, noEnv)
	if err == nil || !strings.Contains(err.Error(), "no-such-docs") {
		t.Errorf("err = %v, want it to name the directory", err)
	}
}

// writeEscape writes dir/secret.md and dir/root holding private.md,
// private.png and a content directory docs with a.md and three symlinks:
// escape.md to the secret, inside.md to private.md and leak.png to
// private.png. It returns dir/root.
func writeEscape(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	docs := filepath.Join(dir, "root", "docs")
	if err := os.MkdirAll(docs, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string]string{
		"secret.md":        "# hunter2\n",
		"root/private.md":  "# hunter3\n",
		"root/private.png": "hunter4",
		"root/docs/a.md":   "# A\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for link, target := range map[string]string{"escape.md": "../../secret.md", "inside.md": "../private.md", "leak.png": "../private.png"} {
		if err := os.Symlink(target, filepath.Join(docs, link)); err != nil {
			t.Fatal(err)
		}
	}
	return filepath.Join(dir, "root")
}

func TestOpenRootKeepsReadsInside(t *testing.T) {
	cfg, err := openRoot(writeEscape(t), "api.yaml", "docs", "")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SpecPath != "api.yaml" || cfg.DocsPath != "docs" {
		t.Errorf("SpecPath = %q, DocsPath = %q", cfg.SpecPath, cfg.DocsPath)
	}
	if _, err := fs.ReadFile(cfg.Root, "docs/a.md"); err != nil {
		t.Errorf("docs/a.md: %v", err)
	}
	if b, err := fs.ReadFile(cfg.Root, "docs/escape.md"); err == nil {
		t.Errorf("a symlink out of the directory was followed: %q", b)
	}
}

func TestStartupKeepsDocsInside(t *testing.T) {
	_, h, err := startup([]string{"-root-dir", writeEscape(t), "-spec-path", "api.yaml", "-docs-path", "docs"}, noEnv)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/docs/escape.md", "/docs/inside.md", "/raw/leak.png"} {
		if rec := get(h, path); rec.Code != http.StatusNotFound || strings.Contains(rec.Body.String(), "hunter") {
			t.Errorf("%s: %d %q", path, rec.Code, rec.Body)
		}
	}
	if list := get(h, "/docs/").Body.String(); strings.Contains(list, "escape.md") || strings.Contains(list, "inside.md") || !strings.Contains(list, `href="/docs/a.md"`) {
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
	_, h, err := startup([]string{"-root-dir", "../../testdata/specs", "-spec-path", "petstore-3.0.yaml"}, noEnv)
	if err != nil {
		t.Fatal(err)
	}
	if body := get(h, "/specs/petstore-3.0.yaml").Body.String(); strings.Contains(body, "hideTryIt") {
		t.Errorf("the viewer page hides the Try It console by default: %q", body)
	}
}

func TestStartupHidesTryIt(t *testing.T) {
	_, h, err := startup([]string{"-root-dir", "../../testdata/specs", "-spec-path", "petstore-3.0.yaml", "-hide-try-it"}, noEnv)
	if err != nil {
		t.Fatal(err)
	}
	if body := get(h, "/specs/petstore-3.0.yaml").Body.String(); !strings.Contains(body, `hideTryIt="true"`) {
		t.Errorf("the viewer page shows the Try It console: %q", body)
	}
}

func TestStartupRejectsDocsPathOutsideRoot(t *testing.T) {
	dir := t.TempDir()
	for _, sub := range []string{"root", "outside"} {
		if err := os.Mkdir(filepath.Join(dir, sub), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "outside", "a.md"), []byte("# A\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../outside", filepath.Join(dir, "root", "docs")); err != nil {
		t.Fatal(err)
	}
	_, _, err := startup([]string{"-root-dir", filepath.Join(dir, "root"), "-spec-path", "api.yaml", "-docs-path", "docs"}, noEnv)
	if err == nil || !strings.Contains(err.Error(), "docs") {
		t.Errorf("err = %v, want a refusal naming the content directory path", err)
	}
}

func TestParseConfigReadsChatModel(t *testing.T) {
	env := func(k string) string { return map[string]string{"DOCPORTAL_CHAT_MODEL": "claude-opus-5-5"}[k] }
	if cfg, err := parseConfig(nil, env); err != nil || cfg.ChatModel != "claude-opus-5-5" {
		t.Errorf("from the environment: %+v, %v", cfg, err)
	}
	if cfg, err := parseConfig([]string{"-chat-model", "claude-sonnet-5-5"}, env); err != nil || cfg.ChatModel != "claude-sonnet-5-5" {
		t.Errorf("the flag over the environment: %+v, %v", cfg, err)
	}
}

func TestStartupServesChat(t *testing.T) {
	_, h, err := startup([]string{"-root-dir", "../../testdata", "-spec-path", "specs/petstore-3.1.yaml", "-docs-path", "docs", "-chat-model", "claude-opus-5-5"}, noEnv)
	if err != nil {
		t.Fatal(err)
	}
	page := get(h, "/chat")
	if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), "Ask about the") {
		t.Errorf("chat page: %d %q", page.Code, page.Body)
	}
	if nav := get(h, "/docs/").Body.String(); !strings.Contains(nav, `href="/chat"`) {
		t.Errorf("the navigation bar has no Chat link: %q", nav)
	}
}

func TestStartupWithoutChat(t *testing.T) {
	_, h, err := startup([]string{"-root-dir", "../../testdata", "-spec-path", "specs/petstore-3.1.yaml", "-docs-path", "docs"}, noEnv)
	if err != nil {
		t.Fatal(err)
	}
	if rec := get(h, "/chat"); rec.Code != http.StatusNotFound {
		t.Errorf("/chat: %d", rec.Code)
	}
	if nav := get(h, "/docs/").Body.String(); strings.Contains(nav, `href="/chat"`) {
		t.Error("the navigation bar links a chat that is off")
	}
}
