//go:build chat

package main

import (
	"errors"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/bilus/documentation-portal/chat"
	"github.com/bilus/documentation-portal/portal"
)

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
	if body := page.Body.String(); !strings.Contains(body, "The preview version") || !strings.Contains(body, `class="portal-banner"`) || strings.Contains(body, `href="/portals/pets/chat"`) || page.Header().Get("Cache-Control") != "no-store" {
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

func TestAddChatSkipsLinkPortalsAlone(t *testing.T) {
	pcfg := portal.Config{Root: fstest.MapFS{}, Portals: []portal.Portal{{Name: "Status", Type: portal.LinkPortal, URL: "https://status.example.com/"}}}
	got, c, err := addChat(pcfg, "claude-opus-5-5", nil, "")
	if err != nil || c != nil || got.Chat != nil {
		t.Fatalf("link portals alone with a chat model: chat %v, routes %v, err = %v", c, got.Chat, err)
	}
	// A chat kept from a snapshot with documentation loses its agents.
	docs, err := portal.ReadConfig(os.DirFS("../../testdata"), "environment.yaml")
	if err != nil {
		t.Fatal(err)
	}
	_, kept, err := addChat(docs, "claude-opus-5-5", nil, "")
	if err != nil || kept == nil {
		t.Fatalf("a snapshot with documentation: chat %v, err = %v", kept, err)
	}
	if _, again, err := addChat(pcfg, "claude-opus-5-5", kept, ""); err != nil || again != kept {
		t.Fatalf("a later snapshot of link portals alone: chat %v, err = %v, want the same chat", again, err)
	}
	if _, err := kept.Ask(t.Context(), "petstore", portal.Everything, chat.AskerOf("", "client"), "conv", "How?"); !errors.Is(err, chat.ErrNoPortal) {
		t.Errorf("a question about the removed documentation: err = %v, want ErrNoPortal", err)
	}
	if _, err := portal.New(got); err != nil {
		t.Errorf("the portal handler: %v", err)
	}
}

func TestStartupSignsReadersIn(t *testing.T) {
	o := mockProvider(t)
	srv := httptest.NewUnstartedServer(nil)
	defer srv.Close()
	env := map[string]string{"DOCPORTAL_OIDC_CLIENT_SECRET": o.ClientSecret, "DOCPORTAL_SESSION_KEY": sessionKey}
	_, h, err := startup(t.Context(), []string{
		"-config", sample,
		"-chat-model", "claude-opus-5-5",
		"-oidc-issuer", o.Issuer(),
		"-oidc-client-id", o.ClientID,
		"-oidc-callback-url", "http://" + srv.Listener.Addr().String() + "/auth/callback",
	}, func(k string) string { return env[k] })
	if err != nil {
		t.Fatal(err)
	}
	srv.Config.Handler = h
	srv.Start()

	// Without a session, a page sends the reader to the provider.
	resp, err := (&http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}).Get(srv.URL + "/portals/petstore/docs/guides/")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if loc := resp.Header.Get("Location"); resp.StatusCode != http.StatusFound || !strings.HasPrefix(loc, o.AuthorizationEndpoint()+"?") {
		t.Errorf("a page without a session answered %d for %q, want the provider's authorization endpoint", resp.StatusCode, loc)
	}

	// The reader signs in, returns to the page and sees the account links.
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	b := &http.Client{Jar: jar}
	resp, err = b.Get(srv.URL + "/portals/petstore/docs/guides/")
	if err != nil {
		t.Fatal(err)
	}
	page, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	links := `<span class="portal-account"><span>jane.doe</span><span><a href="/auth/sign-out">Sign out</a></span></span>`
	if resp.Request.URL.Path != "/portals/petstore/docs/guides/" || !strings.Contains(string(page), links) {
		t.Errorf("the sign-in ended at %s with a page without the account links %s", resp.Request.URL, links)
	}
	if cc := resp.Header.Get("Cache-Control"); cc != "private" {
		t.Errorf("the signed-in reader's page has Cache-Control %q, want private", cc)
	}
	// The chat page is the signed-in reader's too.
	resp, err = b.Get(srv.URL + "/portals/petstore/chat")
	if err != nil {
		t.Fatal(err)
	}
	page, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(page), "jane.doe") || !strings.Contains(string(page), `href="/auth/sign-out"`) {
		t.Errorf("the chat page answered %d without the account links", resp.StatusCode)
	}
	// The sign-out link ends the session.
	resp, err = b.Get(srv.URL + "/auth/sign-out")
	if err != nil {
		t.Fatal(err)
	}
	page, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(page), "signed out") {
		t.Errorf("the sign-out answered %d with %q", resp.StatusCode, page)
	}
}

func TestAddChatTakesTheReaderHookWithAnIssuer(t *testing.T) {
	pcfg, err := portal.ReadConfig(os.DirFS("../../testdata"), "environment.yaml")
	if err != nil {
		t.Fatal(err)
	}
	// The chat's reader hook makes its pages private.
	for issuer, want := range map[string]string{"": "", "https://tenant.auth0.com/": "private"} {
		withChat, _, err := addChat(pcfg, "claude-opus-5-5", nil, issuer)
		if err != nil {
			t.Fatal(err)
		}
		h, err := portal.New(withChat)
		if err != nil {
			t.Fatal(err)
		}
		if got := get(h, "/portals/petstore/chat").Header().Get("Cache-Control"); got != want {
			t.Errorf("issuer %q: the chat page has Cache-Control %q, want %q", issuer, got, want)
		}
	}
}
