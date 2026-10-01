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

// sample is the sample configuration.
const sample = "../../testdata/environment.yaml"

// writeConfig writes a configuration file with sections, YAML under the
// sections key, to dir and returns its name.
func writeConfig(t *testing.T, dir, sections string) string {
	t.Helper()
	name := filepath.Join(dir, "environment.yaml")
	if err := os.WriteFile(name, []byte("sections:\n"+sections), 0o644); err != nil {
		t.Fatal(err)
	}
	return name
}

func TestStartupServesSpec(t *testing.T) {
	addr, h, err := startup([]string{"-config", sample}, noEnv)
	if err != nil {
		t.Fatal(err)
	}
	if addr != ":8080" {
		t.Errorf("addr = %q, want :8080", addr)
	}

	page := get(h, "/specs/api")
	if page.Code != http.StatusOK || !strings.HasPrefix(page.Header().Get("Content-Type"), "text/html") {
		t.Fatalf("viewer page: %d %s", page.Code, page.Header().Get("Content-Type"))
	}
	if want := `apiDescriptionUrl="/api/specs/api"`; !strings.Contains(page.Body.String(), want) {
		t.Errorf("viewer page does not contain %s", want)
	}

	raw := get(h, "/api/specs/api")
	want, err := os.ReadFile("../../testdata/specs/petstore-3.1.yaml")
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
	if want := (config{Addr: ":8080", ConfigName: "environment.yaml"}); cfg != want {
		t.Errorf("defaults = %+v, want %+v", cfg, want)
	}

	env := map[string]string{"DOCPORTAL_ADDR": ":9090", "DOCPORTAL_CONFIG": "/srv/docs/environment.yaml"}
	cfg, err = parseConfig([]string{"-config", "docs/portal.yaml"}, func(k string) string { return env[k] })
	if err != nil {
		t.Fatal(err)
	}
	if want := (config{Addr: ":9090", ConfigName: "docs/portal.yaml"}); cfg != want {
		t.Errorf("got %+v, want %+v", cfg, want)
	}
	cfg, err = parseConfig(nil, func(k string) string { return env[k] })
	if err != nil || cfg.ConfigName != "/srv/docs/environment.yaml" {
		t.Errorf("from the environment: %+v, %v", cfg, err)
	}

	if _, err := parseConfig([]string{"-config", "a.yaml", "b.yaml"}, noEnv); err == nil {
		t.Error("a positional argument was accepted")
	}
}

func TestParseConfigRejectsUnknownFlag(t *testing.T) {
	// The configuration file replaced -root-dir, -spec-path, -docs-path and
	// -toc-path, and -spec-path replaced -spec.
	for _, flag := range []string{"-spec", "-root-dir", "-spec-path", "-docs-path", "-toc-path"} {
		if _, err := parseConfig([]string{flag, "api.yaml"}, noEnv); err == nil || !strings.Contains(err.Error(), flag) {
			t.Errorf("err = %v, want an error about %s", err, flag)
		}
	}
}

func TestParseConfigHelpListsFlags(t *testing.T) {
	_, err := parseConfig([]string{"-h"}, noEnv)
	if err == nil || !strings.Contains(err.Error(), "-config") {
		t.Errorf("err = %v, want the usage with -config", err)
	}
}

func TestStartupRejectsMissingRootDir(t *testing.T) {
	_, _, err := startup([]string{"-config", "testdata/no-such-dir/environment.yaml"}, noEnv)
	if err == nil || !strings.Contains(err.Error(), "testdata/no-such-dir") {
		t.Errorf("err = %v, want it to name the directory", err)
	}
}

func TestStartupRejectsMissingConfigFile(t *testing.T) {
	_, _, err := startup([]string{"-config", filepath.Join(t.TempDir(), "environment.yaml")}, noEnv)
	if err == nil || !strings.Contains(err.Error(), "environment.yaml") {
		t.Errorf("err = %v, want it to name the configuration file", err)
	}
}

func TestStartupRejectsSpecPathOutsideDir(t *testing.T) {
	config := writeConfig(t, t.TempDir(), "  - {title: API, type: spec, input: ../specs/petstore-3.0.yaml}\n")
	_, _, err := startup([]string{"-config", config}, noEnv)
	if err == nil || !strings.Contains(err.Error(), "../specs/petstore-3.0.yaml") {
		t.Errorf("err = %v, want it to name the spec path", err)
	}
}

func TestStartupServesDocs(t *testing.T) {
	_, h, err := startup([]string{"-config", sample}, noEnv)
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

func TestStartupRejectsUnknownConfigKey(t *testing.T) {
	config := writeConfig(t, t.TempDir(), "  - {title: API, type: spec, input: api.yaml, path: api.yaml}\n")
	_, _, err := startup([]string{"-config", config}, noEnv)
	if err == nil || !strings.Contains(err.Error(), "path") {
		t.Errorf("err = %v, want one about the key path", err)
	}
}

func TestStartupRejectsTocPathOutsideRoot(t *testing.T) {
	config := writeConfig(t, writeEscape(t), "  - {title: API, type: spec, input: api.yaml}\n  - {title: Guides, type: docs, input: docs, toc: ../toc.json}\n")
	_, _, err := startup([]string{"-config", config}, noEnv)
	if err == nil || !strings.Contains(err.Error(), "toc path") {
		t.Errorf("err = %v, want one about the toc path", err)
	}
}

func TestStartupRejectsMissingDocsPath(t *testing.T) {
	config := writeConfig(t, t.TempDir(), "  - {title: API, type: spec, input: api.yaml}\n  - {title: Guides, type: docs, input: no-such-docs}\n")
	_, _, err := startup([]string{"-config", config}, noEnv)
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
	root, name, err := openRoot(filepath.Join(writeEscape(t), "environment.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if name != "environment.yaml" {
		t.Errorf("name = %q, want environment.yaml", name)
	}
	if _, err := fs.ReadFile(root, "docs/a.md"); err != nil {
		t.Errorf("docs/a.md: %v", err)
	}
	if b, err := fs.ReadFile(root, "docs/escape.md"); err == nil {
		t.Errorf("a symlink out of the directory was followed: %q", b)
	}
}

func TestStartupKeepsDocsInside(t *testing.T) {
	config := writeConfig(t, writeEscape(t), "  - {title: API, type: spec, input: api.yaml}\n  - {title: Guides, type: docs, input: docs}\n")
	_, h, err := startup([]string{"-config", config}, noEnv)
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
	_, h, err := startup([]string{"-config", sample}, noEnv)
	if err != nil {
		t.Fatal(err)
	}
	if body := get(h, "/specs/api").Body.String(); strings.Contains(body, "hideTryIt") {
		t.Errorf("the viewer page hides the Try It console by default: %q", body)
	}
}

func TestStartupHidesTryIt(t *testing.T) {
	_, h, err := startup([]string{"-config", sample, "-hide-try-it"}, noEnv)
	if err != nil {
		t.Fatal(err)
	}
	if body := get(h, "/specs/api").Body.String(); !strings.Contains(body, `hideTryIt="true"`) {
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
	config := writeConfig(t, filepath.Join(dir, "root"), "  - {title: API, type: spec, input: api.yaml}\n  - {title: Guides, type: docs, input: docs}\n")
	_, _, err := startup([]string{"-config", config}, noEnv)
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
	_, h, err := startup([]string{"-config", sample, "-chat-model", "claude-opus-5-5"}, noEnv)
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
	_, h, err := startup([]string{"-config", sample}, noEnv)
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
