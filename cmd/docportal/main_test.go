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
	if want := (config{Addr: ":8080", ConfigName: "environment.yaml", Refresh: time.Minute, MaxSize: 256 << 20, PreviewIdle: time.Hour, MaxPreviews: 10}); cfg != want {
		t.Errorf("defaults = %+v, want %+v", cfg, want)
	}

	env := map[string]string{"DOCPORTAL_ADDR": ":9090", "DOCPORTAL_CONFIG": "/srv/docs/environment.yaml"}
	cfg, err = parseConfig([]string{"-config", "docs/portal.yaml"}, func(k string) string { return env[k] })
	if err != nil {
		t.Fatal(err)
	}
	if want := (config{Addr: ":9090", ConfigName: "docs/portal.yaml", Refresh: time.Minute, MaxSize: 256 << 20, PreviewIdle: time.Hour, MaxPreviews: 10}); cfg != want {
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

func TestParseConfigArchive(t *testing.T) {
	lookup := func(string) string { return "" }
	cfg, err := parseConfig([]string{"-root", "gs://docs?prefix=portal/", "-archive", "published.tgz"}, lookup)
	if err != nil || cfg.Archive != "published.tgz" {
		t.Errorf("cfg = %+v, %v; want the archive key", cfg, err)
	}
	env := map[string]string{"DOCPORTAL_ROOT": "gs://docs?prefix=portal/", "DOCPORTAL_ARCHIVE": "published.tgz"}
	cfg, err = parseConfig(nil, func(k string) string { return env[k] })
	if err != nil || cfg.Archive != "published.tgz" {
		t.Errorf("from the environment: cfg = %+v, %v; want the archive key", cfg, err)
	}
	for name, args := range map[string][]string{
		"without -root":           {"-archive", "published.tgz"},
		"a path outside the root": {"-root", "gs://docs?prefix=portal/", "-archive", "../published.tgz"},
	} {
		if _, err := parseConfig(args, lookup); err == nil {
			t.Errorf("%s was accepted", name)
		}
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
	_, c, err := addChat(pcfg, "claude-opus-5-5", nil, "")
	if err != nil || c == nil {
		t.Fatalf("the first snapshot: %v, chat %v", err, c)
	}
	// A snapshot whose libraries cannot be built keeps the chat for the next one.
	broken := portal.Config{Root: root, Portals: []portal.Portal{{Name: "Pets", Sections: []portal.Section{{Title: "Guides", Type: portal.DocsSection, Input: "missing"}}}}}
	if _, again, err := addChat(broken, "claude-opus-5-5", c, ""); err == nil || again != c {
		t.Errorf("after a failed snapshot: err %v, chat %v, want an error and the same chat", err, again)
	}
	if _, again, err := addChat(pcfg, "claude-opus-5-5", c, ""); err != nil || again != c {
		t.Errorf("the next good snapshot: err %v, chat %v, want the same chat", err, again)
	}
}

func TestParseConfigReadsTheBucketSettings(t *testing.T) {
	env := map[string]string{"DOCPORTAL_ROOT": "gs://docs?prefix=portal/", "DOCPORTAL_REFRESH": "30s", "DOCPORTAL_MAX_SIZE": "64"}
	cfg, err := parseConfig(nil, func(k string) string { return env[k] })
	if err != nil || cfg.Root != "gs://docs?prefix=portal/" || cfg.Refresh != 30*time.Second || cfg.MaxSize != 64<<20 {
		t.Errorf("from the environment: %+v, %v", cfg, err)
	}
	cfg, err = parseConfig([]string{"-root", "s3://docs?prefix=p/", "-refresh", "2m", "-max-size", "0"}, func(k string) string { return env[k] })
	if err != nil || cfg.Root != "s3://docs?prefix=p/" || cfg.Refresh != 2*time.Minute || cfg.MaxSize != 0 {
		t.Errorf("the flags over the environment: %+v, %v", cfg, err)
	}
	for name, bad := range map[string]map[string]string{
		"a refresh that is not a duration": {"DOCPORTAL_REFRESH": "soon"},
		"a size that is not a number":      {"DOCPORTAL_MAX_SIZE": "big"},
		"a size the limit cannot hold":     {"DOCPORTAL_MAX_SIZE": "17592186044417"},
	} {
		if _, err := parseConfig(nil, func(k string) string { return bad[k] }); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
	for _, args := range [][]string{{"-max-size", "-1"}, {"-max-size", "9223372036854775807"}} {
		if _, err := parseConfig(args, noEnv); err == nil {
			t.Errorf("%v: no error", args)
		}
	}
}

func TestStartupReadsTheConfigurationFileAtItsPath(t *testing.T) {
	dir, url := bucketFolder(t)
	site := filepath.Join(dir, "site")
	if err := os.MkdirAll(filepath.Join(site, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(site, "environment.yaml"), filepath.Join(site, "sub", "environment.yaml")); err != nil {
		t.Fatal(err)
	}
	_, h, err := startup(t.Context(), []string{"-root", url, "-config", "sub/environment.yaml", "-refresh", "0"}, noEnv)
	if err != nil {
		t.Fatal(err)
	}
	if rec := get(h, "/portals/pets/docs/guides/a.md"); rec.Code != http.StatusOK {
		t.Errorf("the document page: %d", rec.Code)
	}
}

func TestBuilderKeepsOneChatAcrossSnapshots(t *testing.T) {
	root := os.DirFS("../../testdata")
	b := &builder{cfg: config{ChatModel: "claude-opus-5-5"}, configPath: "environment.yaml"}
	if _, err := b.build(root); err != nil {
		t.Fatal(err)
	}
	first := b.chat
	if first == nil {
		t.Fatal("the first build made no chat")
	}
	if _, err := b.build(root); err != nil || b.chat != first {
		t.Errorf("the second build: err %v, same chat %v", err, b.chat == first)
	}
}

// getIn returns the response of h to a GET of path from a reader with the
// cookie, such as portal-preview=pr-1.
func getIn(h http.Handler, path, cookie string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodGet, path, nil)
	r.Header.Set("Cookie", cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	return rec
}

// writePreview writes the preview folder name under site/previews/ in dir,
// a bucketFolder: the bucket folder's configuration file and spec, and a
// document a.md that says text.
func writePreview(t *testing.T, dir, name, text string) {
	t.Helper()
	preview := filepath.Join(dir, "site", "previews", name)
	for _, file := range []string{"environment.yaml", "specs/pets.yaml"} {
		data, err := os.ReadFile(filepath.Join(dir, "site", file))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Dir(filepath.Join(preview, file)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(preview, file), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(preview, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(preview, "docs", "a.md"), []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestParseConfigReadsThePreviewSettings(t *testing.T) {
	env := map[string]string{"DOCPORTAL_ROOT": "gs://docs?prefix=portal/", "DOCPORTAL_PREVIEWS": "previews/", "DOCPORTAL_PREVIEW_IDLE": "30m", "DOCPORTAL_MAX_PREVIEWS": "3"}
	lookup := func(k string) string { return env[k] }
	cfg, err := parseConfig(nil, lookup)
	if err != nil || cfg.Previews != "previews/" || cfg.PreviewIdle != 30*time.Minute || cfg.MaxPreviews != 3 {
		t.Errorf("from the environment: %+v, %v", cfg, err)
	}
	cfg, err = parseConfig([]string{"-previews", "ci/previews", "-preview-idle", "0", "-max-previews", "0"}, lookup)
	if err != nil || cfg.Previews != "ci/previews" || cfg.PreviewIdle != 0 || cfg.MaxPreviews != 0 {
		t.Errorf("the flags over the environment: %+v, %v", cfg, err)
	}
	for name, args := range map[string][]string{
		"-previews without -root": {"-previews", "previews"},
		"a negative idle time":    {"-root", "gs://docs", "-previews", "previews", "-preview-idle", "-1m"},
		"a negative limit":        {"-root", "gs://docs", "-previews", "previews", "-max-previews", "-1"},
		"a limit of no number":    {"-max-previews", "many"},
	} {
		if _, err := parseConfig(args, noEnv); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
	for name, bad := range map[string]map[string]string{
		"an idle time that is not a duration": {"DOCPORTAL_PREVIEW_IDLE": "soon"},
		"a limit that is not a number":        {"DOCPORTAL_MAX_PREVIEWS": "many"},
	} {
		if _, err := parseConfig(nil, func(k string) string { return bad[k] }); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}

func TestStartupLeavesThePreviewsOutOfThePublishedDocumentation(t *testing.T) {
	dir, url := bucketFolder(t)
	writePreview(t, dir, "pr-1", "# The preview version\n")
	// A file of a preview puts the bucket folder over a limit of 1 MiB.
	if err := os.WriteFile(filepath.Join(dir, "site", "previews", "pr-1", "big.bin"), make([]byte, 2<<20), 0o644); err != nil {
		t.Fatal(err)
	}
	args := []string{"-root", url, "-config", "environment.yaml", "-refresh", "0", "-max-size", "1"}
	if _, _, err := startup(t.Context(), append(args, "-previews", "previews"), noEnv); err != nil {
		t.Errorf("the previews count against the published documentation's size limit: %v", err)
	}
	if _, _, err := startup(t.Context(), args, noEnv); err == nil {
		t.Error("without -previews, the previews location does not count against the size limit")
	}
}

func TestStartupServesAPreviewWithoutTheChat(t *testing.T) {
	dir, url := bucketFolder(t)
	writePreview(t, dir, "pr-1", "# The preview version\n")
	_, h, err := startup(t.Context(), []string{"-root", url, "-config", "environment.yaml", "-refresh", "0", "-previews", "previews", "-chat-model", "claude-opus-5-5"}, noEnv)
	if err != nil {
		t.Fatal(err)
	}
	rec := get(h, "/previews/pr-1/portals/pets/docs/guides/a.md")
	if cookies := rec.Result().Cookies(); rec.Code != http.StatusFound || rec.Header().Get("Location") != "/portals/pets/docs/guides/a.md" || len(cookies) != 1 || cookies[0].String() != "portal-preview=pr-1; Path=/; HttpOnly; SameSite=Lax" {
		t.Fatalf("the switch to pr-1: %d to %q with the cookies %v", rec.Code, rec.Header().Get("Location"), cookies)
	}
	page := getIn(h, "/portals/pets/docs/guides/a.md", "portal-preview=pr-1")
	if body := page.Body.String(); !strings.Contains(body, "The preview version") || !strings.Contains(body, `class="portal-banner"`) || strings.Contains(body, `href="/portals/pets/chat"`) || page.Header().Get("Cache-Control") != "private" {
		t.Errorf("the preview's page: %q", body)
	}
	if rec := getIn(h, "/portals/pets/chat", "portal-preview=pr-1"); rec.Code != http.StatusNotFound {
		t.Errorf("the preview's chat page: %d", rec.Code)
	}
	// The published documentation keeps its chat.
	if body := get(h, "/portals/pets/docs/guides/a.md").Body.String(); !strings.Contains(body, "The first version") || !strings.Contains(body, `href="/portals/pets/chat"`) || strings.Contains(body, "portal-banner") {
		t.Errorf("the published page: %q", body)
	}
}

func TestStartupEndsADeletedPreview(t *testing.T) {
	dir, url := bucketFolder(t)
	writePreview(t, dir, "pr-1", "# The preview version\n")
	_, h, err := startup(t.Context(), []string{"-root", url, "-config", "environment.yaml", "-refresh", "5ms", "-previews", "previews"}, noEnv)
	if err != nil {
		t.Fatal(err)
	}
	if body := getIn(h, "/portals/pets/docs/guides/a.md", "portal-preview=pr-1").Body.String(); !strings.Contains(body, "The preview version") {
		t.Fatalf("the preview's page: %q", body)
	}
	if err := os.RemoveAll(filepath.Join(dir, "site", "previews", "pr-1")); err != nil {
		t.Fatal(err)
	}
	for start := time.Now(); ; time.Sleep(5 * time.Millisecond) {
		body := getIn(h, "/portals/pets/docs/guides/a.md", "portal-preview=pr-1").Body.String()
		if strings.Contains(body, "The preview <strong>pr-1</strong> is no longer available.") {
			if !strings.Contains(body, "The first version") {
				t.Errorf("the page with the notice: %q", body)
			}
			break
		}
		if time.Since(start) > 5*time.Second {
			t.Fatal("the deleted preview still serves after five seconds")
		}
	}
}

func TestStartupKeepsAPreviewThroughABrokenChange(t *testing.T) {
	dir, url := bucketFolder(t)
	writePreview(t, dir, "pr-1", "# The preview version\n")
	_, h, err := startup(t.Context(), []string{"-root", url, "-config", "environment.yaml", "-refresh", "5ms", "-preview-idle", "1h", "-previews", "previews"}, noEnv)
	if err != nil {
		t.Fatal(err)
	}
	if body := getIn(h, "/portals/pets/docs/guides/a.md", "portal-preview=pr-1").Body.String(); !strings.Contains(body, "The preview version") {
		t.Fatalf("the preview's page: %q", body)
	}
	// A configuration file that docportal refuses keeps the last snapshot,
	// check after check.
	if err := os.WriteFile(filepath.Join(dir, "site", "previews", "pr-1", "environment.yaml"), []byte("portals: [\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond)
	if body := getIn(h, "/portals/pets/docs/guides/a.md", "portal-preview=pr-1").Body.String(); !strings.Contains(body, "The preview version") || strings.Contains(body, "no longer available") {
		t.Errorf("the preview's page after a broken change: %q", body)
	}
}

func TestStartupBeforeTheFirstUploadServesANotice(t *testing.T) {
	dir := t.TempDir()
	args := []string{"-addr", "127.0.0.1:0", "-root", "file://" + dir, "-archive", "published.tgz", "-config", "environment.yaml", "-refresh", "0"}

	_, h, err := startup(t.Context(), args, func(string) string { return "" })
	if err != nil {
		t.Fatalf("before the first upload docportal must start, not fail: %v", err)
	}

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), "not been published") {
		t.Errorf("/ answers %d %q, want the notice that nothing is published yet", rec.Code, rec.Body.String())
	}
}
