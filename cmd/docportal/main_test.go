package main

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/bilus/documentation-portal/portal"
)

func noEnv(string) string { return "" }

func get(h http.Handler, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

// sample is the sample configuration.
const sample = "../../testdata/environment.yaml"

// writeConfig writes a configuration file to dir, with one portal, Pets,
// whose sections are the YAML of sections, a list at an indent of two
// spaces, and returns its name.
func writeConfig(t *testing.T, dir, sections string) string {
	t.Helper()
	name := filepath.Join(dir, "environment.yaml")
	yaml := "portals:\n  - name: Pets\n    sections:\n"
	for _, line := range strings.SplitAfter(sections, "\n") {
		if line != "" {
			yaml += "  " + line
		}
	}
	if err := os.WriteFile(name, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	return name
}

func TestStartupServesSpec(t *testing.T) {
	addr, h, err := startup(t.Context(), []string{"-config", sample}, noEnv)
	if err != nil {
		t.Fatal(err)
	}
	if addr != ":8080" {
		t.Errorf("addr = %q, want :8080", addr)
	}

	page := get(h, "/portals/petstore/specs/api")
	if page.Code != http.StatusOK || !strings.HasPrefix(page.Header().Get("Content-Type"), "text/html") {
		t.Fatalf("viewer page: %d %s", page.Code, page.Header().Get("Content-Type"))
	}
	if want := `apiDescriptionUrl="/portals/petstore/api/specs/api"`; !strings.Contains(page.Body.String(), want) {
		t.Errorf("viewer page does not contain %s", want)
	}

	raw := get(h, "/portals/petstore/api/specs/api")
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
	if want := (config{Addr: ":8080", ConfigName: "environment.yaml", Refresh: time.Minute, MaxSize: 256 << 20}); cfg != want {
		t.Errorf("defaults = %+v, want %+v", cfg, want)
	}

	env := map[string]string{"DOCPORTAL_ADDR": ":9090", "DOCPORTAL_CONFIG": "/srv/docs/environment.yaml"}
	cfg, err = parseConfig([]string{"-config", "docs/portal.yaml"}, func(k string) string { return env[k] })
	if err != nil {
		t.Fatal(err)
	}
	if want := (config{Addr: ":9090", ConfigName: "docs/portal.yaml", Refresh: time.Minute, MaxSize: 256 << 20}); cfg != want {
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
	_, _, err := startup(t.Context(), []string{"-config", "testdata/no-such-dir/environment.yaml"}, noEnv)
	if err == nil || !strings.Contains(err.Error(), "testdata/no-such-dir") {
		t.Errorf("err = %v, want it to name the directory", err)
	}
}

func TestStartupRejectsMissingConfigFile(t *testing.T) {
	_, _, err := startup(t.Context(), []string{"-config", filepath.Join(t.TempDir(), "environment.yaml")}, noEnv)
	if err == nil || !strings.Contains(err.Error(), "environment.yaml") {
		t.Errorf("err = %v, want it to name the configuration file", err)
	}
}

func TestStartupRejectsSpecPathOutsideDir(t *testing.T) {
	config := writeConfig(t, t.TempDir(), "  - {title: API, type: spec, input: ../specs/petstore-3.0.yaml}\n")
	_, _, err := startup(t.Context(), []string{"-config", config}, noEnv)
	if err == nil || !strings.Contains(err.Error(), "../specs/petstore-3.0.yaml") {
		t.Errorf("err = %v, want it to name the spec path", err)
	}
}

func TestStartupServesDocs(t *testing.T) {
	_, h, err := startup(t.Context(), []string{"-config", sample}, noEnv)
	if err != nil {
		t.Fatal(err)
	}

	list := get(h, "/portals/petstore/docs/guides/")
	if list.Code != http.StatusOK || !strings.HasPrefix(list.Header().Get("Content-Type"), "text/html") || !strings.Contains(list.Body.String(), `href="/portals/petstore/docs/guides/guide/intro.md"`) {
		t.Errorf("document list: %d %s %q", list.Code, list.Header().Get("Content-Type"), list.Body)
	}
	page := get(h, "/portals/petstore/docs/guides/guide/intro.md")
	if page.Code != http.StatusOK || !strings.HasPrefix(page.Header().Get("Content-Type"), "text/html") || !strings.Contains(page.Body.String(), "Introduction") {
		t.Errorf("document page: %d %s %q", page.Code, page.Header().Get("Content-Type"), page.Body)
	}
}

func TestStartupRejectsUnknownConfigKey(t *testing.T) {
	config := writeConfig(t, t.TempDir(), "  - {title: API, type: spec, input: api.yaml, path: api.yaml}\n")
	_, _, err := startup(t.Context(), []string{"-config", config}, noEnv)
	if err == nil || !strings.Contains(err.Error(), "path") {
		t.Errorf("err = %v, want one about the key path", err)
	}
}

func TestStartupRejectsTocPathOutsideRoot(t *testing.T) {
	config := writeConfig(t, writeEscape(t), "  - {title: API, type: spec, input: api.yaml}\n  - {title: Guides, type: docs, input: docs, toc: ../toc.json}\n")
	_, _, err := startup(t.Context(), []string{"-config", config}, noEnv)
	if err == nil || !strings.Contains(err.Error(), "toc path") {
		t.Errorf("err = %v, want one about the toc path", err)
	}
}

func TestStartupRejectsMissingDocsPath(t *testing.T) {
	config := writeConfig(t, t.TempDir(), "  - {title: API, type: spec, input: api.yaml}\n  - {title: Guides, type: docs, input: no-such-docs}\n")
	_, _, err := startup(t.Context(), []string{"-config", config}, noEnv)
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

func TestOpenSourceKeepsReadsInside(t *testing.T) {
	src, name, err := openSource(t.Context(), config{ConfigName: filepath.Join(writeEscape(t), "environment.yaml")})
	if err != nil {
		t.Fatal(err)
	}
	if name != "environment.yaml" {
		t.Errorf("name = %q, want environment.yaml", name)
	}
	root, err := src.Read(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
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
	_, h, err := startup(t.Context(), []string{"-config", config}, noEnv)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/portals/pets/docs/guides/escape.md", "/portals/pets/docs/guides/inside.md", "/portals/pets/raw/guides/leak.png"} {
		if rec := get(h, path); rec.Code != http.StatusNotFound || strings.Contains(rec.Body.String(), "hunter") {
			t.Errorf("%s: %d %q", path, rec.Code, rec.Body)
		}
	}
	if list := get(h, "/portals/pets/docs/guides/").Body.String(); strings.Contains(list, "escape.md") || strings.Contains(list, "inside.md") || !strings.Contains(list, `href="/portals/pets/docs/guides/a.md"`) {
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
	_, h, err := startup(t.Context(), []string{"-config", sample}, noEnv)
	if err != nil {
		t.Fatal(err)
	}
	if body := get(h, "/portals/petstore/specs/api").Body.String(); strings.Contains(body, "hideTryIt") {
		t.Errorf("the viewer page hides the Try It console by default: %q", body)
	}
}

func TestStartupHidesTryIt(t *testing.T) {
	_, h, err := startup(t.Context(), []string{"-config", sample, "-hide-try-it"}, noEnv)
	if err != nil {
		t.Fatal(err)
	}
	if body := get(h, "/portals/petstore/specs/api").Body.String(); !strings.Contains(body, `hideTryIt="true"`) {
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
	_, _, err := startup(t.Context(), []string{"-config", config}, noEnv)
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
	_, h, err := startup(t.Context(), []string{"-config", sample, "-chat-model", "claude-opus-5-5"}, noEnv)
	if err != nil {
		t.Fatal(err)
	}
	page := get(h, "/portals/petstore/chat")
	if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), "Ask about the") {
		t.Errorf("chat page: %d %q", page.Code, page.Body)
	}
	if nav := get(h, "/portals/petstore/docs/guides/").Body.String(); !strings.Contains(nav, `href="/portals/petstore/chat"`) {
		t.Errorf("the navigation bar has no Chat link: %q", nav)
	}
}

func TestStartupWithoutChat(t *testing.T) {
	_, h, err := startup(t.Context(), []string{"-config", sample}, noEnv)
	if err != nil {
		t.Fatal(err)
	}
	if rec := get(h, "/portals/petstore/chat"); rec.Code != http.StatusNotFound {
		t.Errorf("/portals/petstore/chat: %d", rec.Code)
	}
	if nav := get(h, "/portals/petstore/docs/guides/").Body.String(); strings.Contains(nav, `href="/portals/petstore/chat"`) {
		t.Error("the navigation bar links a chat that is off")
	}
}

func TestStartupChatPageLinksTheSections(t *testing.T) {
	_, h, err := startup(t.Context(), []string{"-config", sample, "-chat-model", "claude-opus-5-5"}, noEnv)
	if err != nil {
		t.Fatal(err)
	}
	page := get(h, "/portals/petstore/chat").Body.String()
	nav := page[max(strings.Index(page, "<nav"), 0):max(strings.Index(page, "</nav>"), 0)]
	var links []string
	for _, m := range regexp.MustCompile(`<a href="([^"]*)"[^>]*>([^<]*)</a>`).FindAllStringSubmatch(nav, -1) {
		links = append(links, m[2]+" "+m[1])
	}
	if want := []string{"API /portals/petstore/specs/api", "Guides /portals/petstore/docs/guides/", "Chat /portals/petstore/chat"}; !slices.Equal(links, want) {
		t.Errorf("the chat page links %q, want %q", links, want)
	}
}

func TestStartupServesAChatPageInEveryPortal(t *testing.T) {
	dir := t.TempDir()
	spec, err := os.ReadFile("../../testdata/specs/petstore-3.1.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "pets.yaml"), spec, 0o644); err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(dir, "environment.yaml")
	two := "portals:\n  - {name: One, sections: [{title: API, type: spec, input: pets.yaml}]}\n  - {name: Two, sections: [{title: API, type: spec, input: pets.yaml}]}\n"
	if err := os.WriteFile(config, []byte(two), 0o644); err != nil {
		t.Fatal(err)
	}
	_, h, err := startup(t.Context(), []string{"-config", config, "-chat-model", "claude-opus-5-5"}, noEnv)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/portals/one/chat", "/portals/two/chat"} {
		if page := get(h, path); page.Code != http.StatusOK || !strings.Contains(page.Body.String(), "Ask about the") {
			t.Errorf("%s: %d", path, page.Code)
		}
	}
}

// bucketFolder writes the sample spec and a document under site/ in a
// directory, and returns the Go CDK URL of that bucket folder.
func bucketFolder(t *testing.T) (dir, url string) {
	t.Helper()
	dir = t.TempDir()
	site := filepath.Join(dir, "site")
	for name, body := range map[string]string{
		"environment.yaml": "portals:\n  - name: Pets\n    sections:\n      - title: API\n        type: spec\n        input: specs/pets.yaml\n      - title: Guides\n        type: docs\n        input: docs\n",
		"docs/a.md":        "# The first version\n",
	} {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(site, name)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(site, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	spec, err := os.ReadFile("../../testdata/specs/petstore-3.1.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(site, "specs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(site, "specs", "pets.yaml"), spec, 0o644); err != nil {
		t.Fatal(err)
	}
	return dir, "file://" + filepath.ToSlash(dir) + "?prefix=site/&metadata=skip"
}

func TestStartupServesABucketFolder(t *testing.T) {
	dir, url := bucketFolder(t)
	_, h, err := startup(t.Context(), []string{"-root", url, "-config", "environment.yaml", "-refresh", "0"}, noEnv)
	if err != nil {
		t.Fatal(err)
	}
	if rec := get(h, "/portals/pets/api/specs/api"); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "title: Petstore") {
		t.Errorf("the raw spec: %d %q", rec.Code, rec.Body.String()[:min(80, rec.Body.Len())])
	}
	if rec := get(h, "/portals/pets/docs/guides/a.md"); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "The first version") {
		t.Errorf("the document page: %d", rec.Code)
	}
	// The snapshot is in memory: a change in the folder does not show without a refresh.
	if err := os.WriteFile(filepath.Join(dir, "site", "docs", "a.md"), []byte("# The second version\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if rec := get(h, "/portals/pets/docs/guides/a.md"); !strings.Contains(rec.Body.String(), "The first version") {
		t.Errorf("the document page follows the folder without a refresh")
	}
}

func TestStartupRefusesABucketFolderItCannotServe(t *testing.T) {
	dir, url := bucketFolder(t)
	// A big file puts the folder over a limit of 1 MiB, the smallest limit; 0 means none.
	if err := os.WriteFile(filepath.Join(dir, "site", "big.bin"), make([]byte, 2<<20), 0o644); err != nil {
		t.Fatal(err)
	}
	for name, args := range map[string][]string{
		"a missing configuration file": {"-root", url, "-config", "portal.yaml"},
		"a folder over the size limit": {"-root", url, "-config", "environment.yaml", "-max-size", "1"},
		"a URL of no driver":           {"-root", "nothing://" + dir, "-config", "environment.yaml"},
		"a missing directory":          {"-root", "file://" + filepath.ToSlash(dir) + "/missing?prefix=site/", "-config", "environment.yaml"},
	} {
		if _, _, err := startup(t.Context(), args, noEnv); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}

func TestStartupRefreshesTheBucket(t *testing.T) {
	t.Skip("HOLE(3): a settled change of the bucket folder replaces the snapshot in service")
	dir, url := bucketFolder(t)
	_, h, err := startup(t.Context(), []string{"-root", url, "-config", "environment.yaml", "-refresh", "5ms"}, noEnv)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "site", "docs", "a.md"), []byte("# The second version\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for start := time.Now(); ; time.Sleep(5 * time.Millisecond) {
		if rec := get(h, "/portals/pets/docs/guides/a.md"); strings.Contains(rec.Body.String(), "The second version") {
			break
		}
		if time.Since(start) > 5*time.Second {
			t.Fatal("the document page did not follow the folder within five seconds")
		}
	}
	// A new section in the configuration file reaches the navigation bar.
	config := "portals:\n  - name: Pets\n    sections:\n      - title: API\n        type: spec\n        input: specs/pets.yaml\n      - title: Handbook\n        type: docs\n        input: docs\n"
	if err := os.WriteFile(filepath.Join(dir, "site", "environment.yaml"), []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}
	for start := time.Now(); ; time.Sleep(5 * time.Millisecond) {
		if rec := get(h, "/portals/pets/docs/handbook/a.md"); rec.Code == http.StatusOK {
			break
		}
		if time.Since(start) > 5*time.Second {
			t.Fatal("the new section did not appear within five seconds")
		}
	}
	if rec := get(h, "/portals/pets/docs/guides/a.md"); rec.Code != http.StatusNotFound {
		t.Errorf("the old section still answers %d", rec.Code)
	}
}

func TestAddChatKeepsTheChatOnAFailure(t *testing.T) {
	root := os.DirFS("../../testdata")
	pcfg, err := portal.ReadConfig(root, "environment.yaml")
	if err != nil {
		t.Fatal(err)
	}
	_, c, err := addChat(pcfg, "claude-opus-5-5", nil)
	if err != nil || c == nil {
		t.Fatalf("the first snapshot: %v, chat %v", err, c)
	}
	// A snapshot whose libraries cannot be built keeps the chat for the next one.
	broken := portal.Config{Root: root, Portals: []portal.Portal{{Name: "Pets", Sections: []portal.Section{{Title: "Guides", Type: portal.DocsSection, Input: "missing"}}}}}
	if _, again, err := addChat(broken, "claude-opus-5-5", c); err == nil || again != c {
		t.Errorf("after a failed snapshot: err %v, chat %v, want an error and the same chat", err, again)
	}
	if _, again, err := addChat(pcfg, "claude-opus-5-5", c); err != nil || again != c {
		t.Errorf("the next good snapshot: err %v, chat %v, want the same chat", err, again)
	}
}
