package chat

import (
	"context"
	"encoding/json"
	"errors"
	"iter"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/bilus/live-templ/interpreter"
	"google.golang.org/adk/v2/model"

	"github.com/bilus/documentation-portal/internal/fakeaccess"
	"github.com/bilus/documentation-portal/internal/fakemodel"
	"github.com/bilus/documentation-portal/portal"
)

// accessConfig returns the portal configuration of the access tests, with
// three portals: Pet Shop, with the specs API, titled Pets, and Orders,
// titled Store, the guides of Guides and the notes of Internal; Garden, with
// the spec API, titled Garden; and Vault, with the spec API, titled Vault.
func accessConfig() portal.Config {
	spec := func(title, path, id string) *fstest.MapFile {
		return &fstest.MapFile{Data: []byte("openapi: 3.0.3\ninfo:\n  title: " + title + "\n  version: 1.0.0\npaths:\n  " + path + ":\n    get:\n      operationId: " + id + "\n      summary: List the " + strings.TrimPrefix(path, "/") + "\n")}
	}
	root := fstest.MapFS{
		"pets.yaml":     spec("Pets", "/pets", "listPets"),
		"store.yaml":    spec("Store", "/orders", "listOrders"),
		"garden.yaml":   spec("Garden", "/plants", "listPlants"),
		"vault.yaml":    spec("Vault", "/gold", "listGold"),
		"guides/a.md":   {Data: []byte("# Getting started\n\nList the orders with GET /orders.\n")},
		"internal/x.md": {Data: []byte("# Secret plans\n\nThe orders hide a secret.\n")},
	}
	return portal.Config{Root: root, Portals: []portal.Portal{
		{Name: "Pet Shop", Sections: []portal.Section{
			{Title: "API", Type: portal.SpecSection, Input: "pets.yaml"},
			{Title: "Orders", Type: portal.SpecSection, Input: "store.yaml"},
			{Title: "Guides", Type: portal.DocsSection, Input: "guides"},
			{Title: "Internal", Type: portal.DocsSection, Input: "internal"},
		}},
		{Name: "Garden", Sections: []portal.Section{{Title: "API", Type: portal.SpecSection, Input: "garden.yaml"}}},
		{Name: "Vault", Sections: []portal.Section{{Title: "API", Type: portal.SpecSection, Input: "vault.yaml"}}},
	}}
}

// customer is the reader who may see the Orders and the Guides of Pet Shop
// and the API of Garden.
const customer = "Pet Shop/Orders, Pet Shop/Guides, Garden/API"

// newAccessChat returns the chat of accessConfig's portals over m, and the
// portal handler with its routes and the stub access hook, or fails the test.
func newAccessChat(t *testing.T, m model.LLM, more ...portal.Route) (*Chat, http.Handler) {
	t.Helper()
	cfg := accessConfig()
	libs, err := portal.NewLibraries(cfg)
	if err != nil {
		t.Fatal(err)
	}
	c, err := New(Config{Model: m, Libraries: libs})
	if err != nil {
		t.Fatal(err)
	}
	cfg.Chat, cfg.Access = append(c.Routes(), more...), fakeaccess.Hook
	h, err := portal.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return c, h
}

// servedByPortal returns a portal handler without an access hook that serves
// routes, the routes of a chat, as the routes of the portal configuration's
// Chat. The handler's own portal plays no part.
func servedByPortal(t *testing.T, routes []portal.Route) http.Handler {
	t.Helper()
	h, err := portal.New(portal.Config{
		Root:    fstest.MapFS{},
		Portals: []portal.Portal{{Name: "Elsewhere", Sections: []portal.Section{{Title: "API", Type: portal.SpecSection, Input: "api.yaml"}}}},
		Chat:    routes,
	})
	if err != nil {
		t.Fatal(err)
	}
	return h
}

// sessionThroughPortal returns the session of a chat page of c for a request
// that a portal handler without an access hook serves, as at the page's load.
func sessionThroughPortal(t *testing.T, c *Chat) map[string]string {
	t.Helper()
	var session map[string]string
	keep := portal.Route{Pattern: "GET /session", Handler: http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		var err error
		if session, err = c.sessionOf(r); err != nil {
			t.Error(err)
		}
	})}
	servedByPortal(t, []portal.Route{keep}).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/session", nil))
	return session
}

// recording passes each request on to its model, and keeps the request's
// instructions and the results of its tool calls, as JSON.
type recording struct {
	model.LLM
	seen []string
}

func (r *recording) GenerateContent(ctx context.Context, req *model.LLMRequest, stream bool) iter.Seq2[*model.LLMResponse, error] {
	var b strings.Builder
	if req.Config != nil && req.Config.SystemInstruction != nil {
		for _, p := range req.Config.SystemInstruction.Parts {
			b.WriteString(p.Text)
		}
	}
	for _, c := range req.Contents {
		for _, p := range c.Parts {
			if p.FunctionResponse != nil {
				body, _ := json.Marshal(p.FunctionResponse.Response)
				b.Write(body)
			}
		}
	}
	r.seen = append(r.seen, b.String())
	return r.LLM.GenerateContent(ctx, req, stream)
}

// hiddenFromCustomer lists text that only the sections hidden from the
// customer hold.
var hiddenFromCustomer = []string{"Pets", "listPets", "/pets", "Secret plans", "a secret", "internal/x.md", "Vault", "Garden"}

func TestAskAnswersFromTheVisibleSections(t *testing.T) {
	m := &recording{LLM: fakemodel.New("opus", []fakemodel.Exchange{
		{Match: "questions from customers about the Store API.", Call: &fakemodel.Call{Name: "search", Args: map[string]any{"query": "orders"}}},
		{Match: `"where":"guides/a.md:3"`, Call: &fakemodel.Call{Name: "list_operations", Args: map[string]any{}}},
		{Match: "listOrders", Call: &fakemodel.Call{Name: "list_documents", Args: map[string]any{}}},
		{Match: "Getting started", Call: &fakemodel.Call{Name: "read_spec", Args: map[string]any{"spec": "api", "pointer": "paths/~1pets/get"}}},
		{Match: "no spec section has the slug", Call: &fakemodel.Call{Name: "read_document", Args: map[string]any{"path": "internal/x.md"}}},
		{Match: "no such document", Reply: "Call GET /orders."},
	})}
	c, _ := newAccessChat(t, m)
	answer, err := c.Ask(t.Context(), "pet-shop", fakeaccess.Parse(customer), "client", "conv", "How do I list the orders?")
	if err != nil || answer != "Call GET /orders." {
		t.Fatalf("answer %q, %v, script exhausted: %v", answer, err, m.LLM.(*fakemodel.Fake).Exhausted())
	}
	for i, seen := range m.seen {
		for _, hidden := range hiddenFromCustomer {
			if strings.Contains(seen, hidden) {
				t.Errorf("request %d holds %q, from a section hidden from the reader: %s", i+1, hidden, seen)
			}
		}
	}
}

func TestAskAnswersAHiddenPortalAsMissing(t *testing.T) {
	c, _ := newAccessChat(t, fakemodel.New("opus", []fakemodel.Exchange{{Reply: "One."}}))
	_, missing := c.Ask(t.Context(), "other", portal.Everything, "client", "conv", "Gold?")
	for name, tc := range map[string]struct {
		slug   string
		access portal.Access
	}{
		"a hidden portal":                    {"vault", fakeaccess.Parse(customer)},
		"a portal without a visible section": {"pet-shop", fakeaccess.Parse("Pet Shop/Gone")},
		"no access":                          {"pet-shop", nil},
	} {
		_, err := c.Ask(t.Context(), tc.slug, tc.access, "client", "conv", "Gold?")
		if !errors.Is(err, ErrNoPortal) || err.Error() != strings.Replace(missing.Error(), "other", tc.slug, 1) {
			t.Errorf("%s: err = %v, want %v, as for a missing portal", name, err, missing)
		}
	}
	if answer, err := c.Ask(t.Context(), "garden", fakeaccess.Parse(customer), "client", "conv", "Plants?"); err != nil || answer != "One." {
		t.Errorf("a visible portal: %q, %v", answer, err)
	}
}

func TestChatPageShowsTheVisibleSections(t *testing.T) {
	_, h := newAccessChat(t, fakemodel.New("opus", nil))
	r := httptest.NewRequest(http.MethodGet, "/portals/pet-shop/chat", nil)
	r.Header.Set(fakeaccess.Header, customer)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	page := rec.Body.String()
	for _, want := range []string{"Ask about the Store API", `href="/portals/pet-shop/specs/orders"`, ">Orders</a>", ">Guides</a>", ">Chat</a>", `<a href="/portals/pet-shop/specs/orders">Pet Shop</a><a href="/portals/garden/specs/api">Garden</a>`} {
		if rec.Code != http.StatusOK || !strings.Contains(page, want) {
			t.Errorf("the chat page lacks %s: %d %q", want, rec.Code, page)
		}
	}
	for _, hidden := range []string{"Pets", ">API</a>", ">Internal</a>", "Vault"} {
		if strings.Contains(page, hidden) {
			t.Errorf("the chat page shows %s, hidden from the reader: %q", hidden, page)
		}
	}
	r = httptest.NewRequest(http.MethodGet, "/portals/vault/chat", nil)
	r.Header.Set(fakeaccess.Header, customer)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	if rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), "No portal has the slug vault.") {
		t.Errorf("the chat page of a hidden portal: %d %q", rec.Code, rec.Body)
	}
}

func TestChatPageKeepsTheAccessOfItsLoad(t *testing.T) {
	m := &recording{LLM: fakemodel.New("opus", []fakemodel.Exchange{
		{Match: "questions from customers about the Store API.", Call: &fakemodel.Call{Name: "list_documents", Args: map[string]any{}}},
		{Match: `"path":"guides/a.md"`, Reply: "One guide: Getting started."},
	})}
	// The page's GET passes through the portal handler, which finds the
	// reader, into the page's session; the join mounts the page again from
	// that session alone.
	var c *Chat
	var session map[string]string
	keep := portal.Route{Pattern: "GET /session", Handler: http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		var err error
		if session, err = c.sessionOf(r); err != nil {
			t.Error(err)
		}
	})}
	c, h := newAccessChat(t, m, keep)
	r := httptest.NewRequest(http.MethodGet, "/session", nil)
	r.Header.Set(fakeaccess.Header, customer)
	h.ServeHTTP(httptest.NewRecorder(), r)
	u := &url.URL{Path: "/portals/pet-shop/chat"}
	lv := interpreter.NewCtx(t.Context(), u.Path, u, session, true)
	p, err := c.mount(lv, c.agents[0])
	if err != nil {
		t.Fatal(err)
	}
	var nav, menu []string
	for _, l := range p.Nav {
		nav = append(nav, l.Label)
	}
	for _, l := range p.Menu {
		menu = append(menu, l.Label)
	}
	if p.Heading != "Ask about the Store API" || strings.Join(nav, ", ") != "Orders, Guides, Chat" || strings.Join(menu, ", ") != "Pet Shop, Garden" {
		t.Errorf("the joined page: heading %q, navigation bar %v, menu %v", p.Heading, nav, menu)
	}
	p.Form.Question = "Which guides are there?"
	p.Ask(lv)
	if last := p.Messages[len(p.Messages)-1]; last.Error || !strings.Contains(last.HTML, "One guide") {
		t.Errorf("the joined page's answer: %+v", last)
	}
	for _, seen := range m.seen {
		for _, hidden := range hiddenFromCustomer {
			if strings.Contains(seen, hidden) {
				t.Errorf("the joined page's question reached %q, hidden from the reader: %s", hidden, seen)
			}
		}
	}
	if _, err := c.mount(interpreter.NewCtx(t.Context(), "/portals/vault/chat", &url.URL{Path: "/portals/vault/chat"}, session, true), c.agents[2]); err == nil {
		t.Error("the page of a portal hidden from the reader mounts")
	}
	if _, err := c.mount(interpreter.NewCtx(t.Context(), u.Path, u, map[string]string{"client": "client"}, true), c.agents[0]); err == nil {
		t.Error("a page whose session holds no access mounts")
	}
}
