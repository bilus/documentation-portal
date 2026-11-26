//go:build chat

package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/bilus/documentation-portal/examples/portalapp/mocks"
)

func TestBuildKeepsOneChatAcrossSnapshots(t *testing.T) {
	demo := os.DirFS("demo/docs")
	broken := fstest.MapFS{"environment.yaml": {Data: []byte("portals: [")}}

	without := &builder{configPath: "environment.yaml"}
	if _, err := without.build(demo); err != nil || without.chat != nil {
		t.Errorf("a build without an API key: %v, with the chat %v", err, without.chat)
	}

	b := &builder{configPath: "environment.yaml", model: chatSettings{apiKey: "a test key", model: "claude-opus-5-5", baseURL: "http://127.0.0.1:1"}}
	if _, err := b.build(demo); err != nil || b.chat == nil {
		t.Fatalf("the first build: %v, with the chat %v", err, b.chat)
	}
	first := b.chat
	if _, err := b.build(broken); err == nil || b.chat != first {
		t.Errorf("a failed build: %v, and the chat changed: %v", err, b.chat != first)
	}
	if _, err := b.build(demo); err != nil || b.chat != first {
		t.Errorf("a later build: %v, and the chat changed: %v", err, b.chat != first)
	}
}

func TestBuildPreviewHasTheHooksWithoutTheChat(t *testing.T) {
	rules, err := readAccessFile("demo/access.yaml")
	if err != nil {
		t.Fatal(err)
	}
	b := &builder{configPath: "environment.yaml", rules: rules, model: chatSettings{apiKey: "a test key", model: "claude-opus-5-5"}}
	h, err := b.buildPreview(os.DirFS("demo/docs/previews/pr-1"))
	if err != nil {
		t.Fatal(err)
	}
	if b.chat != nil {
		t.Error("a preview's build made a chat")
	}
	// The access hook hides the preview from a request outside the sign-in.
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if !strings.Contains(rec.Body.String(), "No portals are open to you") || rec.Header().Get("Cache-Control") != "private" {
		t.Errorf("the preview's home page outside the sign-in: %q, Cache-Control %q", rec.Body, rec.Header().Get("Cache-Control"))
	}
	if _, err := b.buildPreview(fstest.MapFS{}); err == nil {
		t.Error("a preview folder without a configuration file built")
	}
}

func TestTheChatFollowsARefresh(t *testing.T) {
	model := httptest.NewServer(mocks.Model())
	defer model.Close()
	p := newPortalTest(t)
	p.env["ANTHROPIC_API_KEY"], p.env["ANTHROPIC_BASE_URL"], p.env["PORTAL_REFRESH"] = "a test key", model.URL, "50ms"
	p.start(t)
	ada := signInAs(t, p.srv.URL, "auth0")
	if page := get(t, ada, p.srv.URL+"/portals/pets/chat"); !strings.Contains(page.body, "Ask about the Pets API") {
		t.Fatalf("the first chat page: %d %q", page.status, page.body)
	}
	store := "openapi: 3.1.0\ninfo:\n  title: Store API\n  version: 1.0.0\npaths: {}\n"
	if err := os.WriteFile(filepath.Join(p.dir, "specs", "store.yaml"), []byte(store), 0o644); err != nil {
		t.Fatal(err)
	}
	config, err := os.ReadFile(filepath.Join(p.dir, "environment.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	config = []byte(strings.Replace(string(config), "      - title: Guides\n", "      - title: Store\n        type: spec\n        input: specs/store.yaml\n      - title: Guides\n", 1))
	if err := os.WriteFile(filepath.Join(p.dir, "environment.yaml"), config, 0o644); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		page := get(t, ada, p.srv.URL+"/portals/pets/chat")
		if heading := regexp.MustCompile(`<h1>([^<]*)</h1>`).FindStringSubmatch(page.body); heading != nil && strings.Contains(heading[1], "Store API") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the chat page never named the new API: %q", page.body)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestAPreviewHasNoChat(t *testing.T) {
	model := httptest.NewServer(mocks.Model())
	defer model.Close()
	p := newPortalTest(t)
	p.env["ANTHROPIC_API_KEY"], p.env["ANTHROPIC_BASE_URL"] = "a test key", model.URL
	p.start(t)
	grace := signInAs(t, p.srv.URL, "github")
	if page := get(t, grace, p.srv.URL+"/portals/pets/docs/guides/"); !strings.Contains(page.body, ">Chat</a>") {
		t.Fatalf("the published guides lack the chat link: %q", page.body)
	}
	preview := get(t, grace, p.srv.URL+"/previews/pr-1/portals/pets/docs/guides/")
	if !strings.Contains(preview.body, "portal-banner") || strings.Contains(preview.body, ">Chat</a>") {
		t.Errorf("the preview's guides: %d %q, want the banner without the chat link", preview.status, preview.body)
	}
	if page := get(t, grace, p.srv.URL+"/portals/pets/chat"); page.status != http.StatusNotFound {
		t.Errorf("the chat page in the preview answers %d, want 404", page.status)
	}
}

func TestStartupRunsTheChatWithAnAPIKey(t *testing.T) {
	model := httptest.NewServer(mocks.Model())
	defer model.Close()
	for key, want := range map[string]int{"": http.StatusNotFound, "a test key": http.StatusOK} {
		p := newPortalTest(t)
		p.env["ANTHROPIC_API_KEY"], p.env["ANTHROPIC_BASE_URL"] = key, model.URL
		p.start(t)
		ada := signInAs(t, p.srv.URL, "auth0")
		page := get(t, ada, p.srv.URL+"/portals/pets/chat")
		if page.status != want {
			t.Errorf("with the API key %q, the chat page answers %d, want %d", key, page.status, want)
		}
		if want == http.StatusOK && !strings.Contains(page.body, "Ask about the Pets API") {
			t.Errorf("the chat page: %q", page.body)
		}
		if guides := get(t, ada, p.srv.URL+"/portals/pets/docs/guides/"); strings.Contains(guides.body, ">Chat</a>") != (want == http.StatusOK) {
			t.Errorf("with the API key %q, the navigation bar's chat link is wrong: %q", key, guides.body)
		}
	}
}
