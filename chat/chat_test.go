//go:build chat

package chat

import (
	"context"
	"errors"
	"io"
	"iter"
	"log"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/session"

	"github.com/bilus/documentation-portal/anthropicmodel"
	"github.com/bilus/documentation-portal/internal/fakemodel"
	"github.com/bilus/documentation-portal/portal"
)

// petsPortal returns one portal, named Pets, with sections.
func petsPortal(sections []portal.Section) []portal.Portal {
	return []portal.Portal{{Name: "Pets", Sections: sections}}
}

// firstLibrary returns the first of libs, the libraries of NewLibraries, or
// its error.
func firstLibrary(libs []*portal.Library, err error) (*portal.Library, error) {
	if err != nil {
		return nil, err
	}
	return libs[0], nil
}

func library(t *testing.T) *portal.Library {
	t.Helper()
	root := fstest.MapFS{
		"api.yaml":  {Data: []byte("openapi: 3.0.3\ninfo:\n  title: Pets\n  version: 1.0.0\npaths:\n  /pets:\n    get:\n      operationId: listPets\n      summary: List the pets\n")},
		"docs/a.md": {Data: []byte("# Getting started\n\nList the pets with GET /pets.\n")},
	}
	lib, err := firstLibrary(portal.NewLibraries(portal.Config{Root: root, Portals: petsPortal([]portal.Section{{Title: "API", Type: portal.SpecSection, Input: "api.yaml"}, {Title: "Documents", Type: portal.DocsSection, Input: "docs"}})}))
	if err != nil {
		t.Fatal(err)
	}
	return lib
}

func newChat(t *testing.T, m model.LLM, limits Limits) *Chat {
	t.Helper()
	c, err := New(Config{Model: m, Libraries: []*portal.Library{library(t)}, Limits: limits})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestAskLooksUpTheAnswer(t *testing.T) {
	m := fakemodel.New("opus", []fakemodel.Exchange{
		{Match: "the Pets API", Call: &fakemodel.Call{Name: "search", Args: map[string]any{"query": "pets"}}},
		{Match: `"url":"/portals/pets/docs/documents/a.md","where":"docs/a.md:3"`, Reply: "Call GET /pets, as [Getting started](/portals/pets/docs/documents/a.md) shows."},
	})
	answer, err := newChat(t, m, Limits{}).Ask(t.Context(), "pets", portal.Everything, AskerOf("", "client"), "conv", "How do I list pets?")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(answer, "GET /pets") || !strings.Contains(answer, "(/portals/pets/docs/documents/a.md)") || !m.Exhausted() {
		t.Errorf("answer %q, script exhausted: %v", answer, m.Exhausted())
	}
}

func TestAskRefusesOverLimits(t *testing.T) {
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	c := newChat(t, fakemodel.New("opus", []fakemodel.Exchange{{Reply: "one"}, {Reply: "two"}, {Reply: "three"}}),
		Limits{QuestionLength: 10, Questions: 2, Window: time.Hour, Turns: 2, Idle: 2 * time.Hour})
	c.now = func() time.Time { return now }
	ask := func(client, conv, q string) error {
		_, err := c.Ask(t.Context(), "pets", portal.Everything, AskerOf("", client), conv, q)
		return err
	}
	if err := ask("a", "c1", "   "); !errors.Is(err, ErrEmpty) {
		t.Errorf("empty: %v", err)
	}
	if err := ask("a", "c1", "far too long a question"); !errors.Is(err, ErrTooLong) {
		t.Errorf("too long: %v", err)
	}
	if err := ask("a", "c1", "q1"); err != nil {
		t.Fatal(err)
	}
	if err := ask("a", "c1", "q2"); err != nil {
		t.Fatal(err)
	}
	if err := ask("b", "c1", "q3"); !errors.Is(err, ErrTurns) {
		t.Errorf("a third turn: %v", err)
	}
	if err := ask("a", "c2", "q3"); !errors.Is(err, ErrRateLimited) {
		t.Errorf("a third question in the hour: %v", err)
	}
	now = now.Add(time.Hour)
	if err := ask("a", "c2", "q3"); err != nil {
		t.Errorf("after the window: %v", err)
	}
	now = now.Add(2 * time.Hour)
	if _, ok := c.convs["pets/all/c1"]; !ok {
		t.Fatal("c1 is gone too early")
	}
	c.admit(t.Context(), AskerOf("", "z"), "c3")
	if _, ok := c.convs["pets/all/c1"]; ok {
		t.Error("an idle conversation stayed")
	}
}

func TestAskStopsAfterTooManyLookups(t *testing.T) {
	search := &fakemodel.Call{Name: "search", Args: map[string]any{"query": "pets"}}
	m := fakemodel.New("opus", []fakemodel.Exchange{{Call: search}, {Call: search}, {Reply: "never"}})
	if _, err := newChat(t, m, Limits{ToolCalls: 1}).Ask(t.Context(), "pets", portal.Everything, AskerOf("", "a"), "c", "pets?"); !errors.Is(err, ErrTooManyTools) {
		t.Errorf("err = %v, want ErrTooManyTools", err)
	}
}

func TestAskAllowsTheLastLookup(t *testing.T) {
	search := &fakemodel.Call{Name: "search", Args: map[string]any{"query": "pets"}}
	m := fakemodel.New("opus", []fakemodel.Exchange{{Call: search}, {Call: search}, {Reply: "Call GET /pets."}})
	if answer, err := newChat(t, m, Limits{ToolCalls: 2}).Ask(t.Context(), "pets", portal.Everything, AskerOf("", "a"), "c", "pets?"); err != nil || answer != "Call GET /pets." {
		t.Errorf("answer %q, err %v", answer, err)
	}
}

// strict fails the test on a request whose history holds a tool call without
// its result, which the Anthropic API rejects.
type strict struct {
	model.LLM
	t *testing.T
}

func (s strict) GenerateContent(ctx context.Context, req *model.LLMRequest, stream bool) iter.Seq2[*model.LLMResponse, error] {
	answered := map[string]bool{}
	for _, c := range req.Contents {
		for _, p := range c.Parts {
			if p.FunctionResponse != nil {
				answered[p.FunctionResponse.ID] = true
			}
		}
	}
	for _, c := range req.Contents {
		for _, p := range c.Parts {
			if p.FunctionCall != nil && !answered[p.FunctionCall.ID] {
				s.t.Errorf("the history holds tool call %s without its result", p.FunctionCall.ID)
			}
		}
	}
	return s.LLM.GenerateContent(ctx, req, stream)
}

func TestAskContinuesAfterTooManyLookups(t *testing.T) {
	search := &fakemodel.Call{Name: "search", Args: map[string]any{"query": "pets"}}
	m := fakemodel.New("opus", []fakemodel.Exchange{{Call: search}, {Call: search}, {Match: "And cats?", Reply: "No cats."}})
	c := newChat(t, strict{m, t}, Limits{ToolCalls: 1})
	if _, err := c.Ask(t.Context(), "pets", portal.Everything, AskerOf("", "a"), "c", "pets?"); !errors.Is(err, ErrTooManyTools) {
		t.Fatalf("err = %v, want ErrTooManyTools", err)
	}
	if answer, err := c.Ask(t.Context(), "pets", portal.Everything, AskerOf("", "a"), "c", "And cats?"); err != nil || answer != "No cats." {
		t.Errorf("the next question: answer %q, err %v", answer, err)
	}
}

func TestAskRunsNoLookupOverTheLimit(t *testing.T) {
	search := func(q string) fakemodel.Call { return fakemodel.Call{Name: "search", Args: map[string]any{"query": q}} }
	m := fakemodel.New("opus", []fakemodel.Exchange{{Calls: []fakemodel.Call{search("pets"), search("cats"), search("dogs")}}})
	c := newChat(t, m, Limits{ToolCalls: 2})
	if _, err := c.Ask(t.Context(), "pets", portal.Everything, AskerOf("", "a"), "c", "pets?"); !errors.Is(err, ErrTooManyTools) {
		t.Fatalf("err = %v, want ErrTooManyTools", err)
	}
	got, err := c.sessions.Get(t.Context(), &session.GetRequest{AppName: appName, UserID: userID, SessionID: "pets/all/c"})
	if err != nil {
		t.Fatal(err)
	}
	ran, refused := 0, 0
	for ev := range got.Session.Events().All() {
		if ev.Content == nil {
			continue
		}
		for _, p := range ev.Content.Parts {
			if r := p.FunctionResponse; r != nil {
				if _, ok := r.Response["matches"]; ok {
					ran++
				} else {
					refused++
				}
			}
		}
	}
	if ran != 2 || refused != 1 {
		t.Errorf("%d lookups ran and %d were refused, want 2 and 1", ran, refused)
	}
}

// failing is a model whose every request fails with err.
type failing struct{ err error }

func (failing) Name() string { return "failing" }

func (f failing) GenerateContent(context.Context, *model.LLMRequest, bool) iter.Seq2[*model.LLMResponse, error] {
	return func(yield func(*model.LLMResponse, error) bool) { yield(nil, f.err) }
}

func TestAskReportsACutOffAnswer(t *testing.T) {
	if _, err := newChat(t, failing{anthropicmodel.ErrTruncated}, Limits{}).Ask(t.Context(), "pets", portal.Everything, AskerOf("", "a"), "c", "Tell me everything"); !errors.Is(err, ErrCutOff) {
		t.Errorf("err = %v, want ErrCutOff", err)
	}
}

func TestAskLeavesThoughtsOut(t *testing.T) {
	m := fakemodel.New("opus", []fakemodel.Exchange{{Thought: "The guide says GET /pets.", Reply: "Call GET /pets."}})
	if answer, err := newChat(t, m, Limits{}).Ask(t.Context(), "pets", portal.Everything, AskerOf("", "a"), "c", "pets?"); err != nil || answer != "Call GET /pets." {
		t.Errorf("answer %q, err %v", answer, err)
	}
}

func TestAskCountsOnlyAdmittedQuestions(t *testing.T) {
	var now time.Time
	c := newChat(t, fakemodel.New("opus", []fakemodel.Exchange{{Reply: "1"}, {Reply: "2"}, {Reply: "3"}, {Reply: "4"}}),
		Limits{Questions: 3, Window: time.Hour, Turns: 2, Idle: 24 * time.Hour})
	c.now = func() time.Time { return now }
	// The refused question at +65m must not count: at +67m the client has
	// asked twice in the hour.
	for _, step := range []struct {
		minutes int
		conv    string
		want    error
	}{{0, "c1", nil}, {10, "c1", nil}, {65, "c1", ErrTurns}, {66, "c2", nil}, {67, "c2", nil}} {
		now = time.Date(2026, 9, 30, 12, step.minutes, 0, 0, time.UTC)
		if _, err := c.Ask(t.Context(), "pets", portal.Everything, AskerOf("", "a"), step.conv, "q"); !errors.Is(err, step.want) {
			t.Errorf("at +%dm in %s: err %v, want %v", step.minutes, step.conv, err, step.want)
		}
	}
}

func TestAskForgetsClientsAfterTheWindow(t *testing.T) {
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	c := newChat(t, fakemodel.New("opus", []fakemodel.Exchange{{Reply: "1"}, {Reply: "2"}, {Reply: "3"}}), Limits{Window: time.Hour})
	c.now = func() time.Time { return now }
	for _, client := range []string{"a", "b"} {
		if _, err := c.Ask(t.Context(), "pets", portal.Everything, AskerOf("", client), "c-"+client, "q"); err != nil {
			t.Fatal(err)
		}
	}
	now = now.Add(time.Hour)
	if _, err := c.Ask(t.Context(), "pets", portal.Everything, AskerOf("", "z"), "c-z", "q"); err != nil {
		t.Fatal(err)
	}
	if len(c.asked) != 1 {
		t.Errorf("clients remembered after the window: %v", c.asked)
	}
}

func TestIdleConversationsLoseTheirSessions(t *testing.T) {
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	c := newChat(t, fakemodel.New("opus", []fakemodel.Exchange{{Reply: "1"}, {Reply: "2"}}), Limits{Idle: time.Hour})
	c.now = func() time.Time { return now }
	session1 := func() error {
		_, err := c.sessions.Get(t.Context(), &session.GetRequest{AppName: appName, UserID: userID, SessionID: "pets/all/c1"})
		return err
	}
	if _, err := c.Ask(t.Context(), "pets", portal.Everything, AskerOf("", "a"), "c1", "q"); err != nil {
		t.Fatal(err)
	}
	if err := session1(); err != nil {
		t.Fatalf("c1's session: %v", err)
	}
	now = now.Add(time.Hour)
	if _, err := c.Ask(t.Context(), "pets", portal.Everything, AskerOf("", "a"), "c2", "q"); err != nil {
		t.Fatal(err)
	}
	if session1() == nil {
		t.Error("c1's session outlived its conversation")
	}
}

func TestAskEndsAConversationThatGrewTooLong(t *testing.T) {
	m := fakemodel.New("opus", []fakemodel.Exchange{{Reply: strings.Repeat("x", 600)}, {Reply: "never"}})
	c := newChat(t, m, Limits{History: 500})
	if _, err := c.Ask(t.Context(), "pets", portal.Everything, AskerOf("", "a"), "c", "q1"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Ask(t.Context(), "pets", portal.Everything, AskerOf("", "a"), "c", "q2"); !errors.Is(err, ErrTurns) {
		t.Errorf("err = %v, want ErrTurns", err)
	}
}

// declining is a model that declines every request.
type declining struct{}

func (declining) Name() string { return "declining" }

func (declining) GenerateContent(context.Context, *model.LLMRequest, bool) iter.Seq2[*model.LLMResponse, error] {
	return func(yield func(*model.LLMResponse, error) bool) { yield(nil, anthropicmodel.ErrRefused) }
}

func TestAskReportsARefusal(t *testing.T) {
	if _, err := newChat(t, declining{}, Limits{}).Ask(t.Context(), "pets", portal.Everything, AskerOf("", "a"), "c", "Tell me a secret"); !errors.Is(err, ErrDeclined) {
		t.Errorf("err = %v, want ErrDeclined", err)
	}
}

func TestChatToolsNameTheSpec(t *testing.T) {
	root := fstest.MapFS{
		"pets.yaml":  {Data: []byte("openapi: 3.0.3\ninfo:\n  title: Pets\n  version: 1.0.0\npaths:\n  /pets:\n    get:\n      operationId: listPets\n")},
		"store.yaml": {Data: []byte("openapi: 3.0.3\ninfo:\n  title: Store\n  version: 1.0.0\npaths:\n  /orders:\n    get:\n      operationId: listOrders\n      summary: List the orders\n")},
	}
	lib, err := firstLibrary(portal.NewLibraries(portal.Config{Root: root, Portals: petsPortal([]portal.Section{
		{Title: "Pets", Type: portal.SpecSection, Input: "pets.yaml"},
		{Title: "Store", Type: portal.SpecSection, Input: "store.yaml"},
	})}))
	if err != nil {
		t.Fatal(err)
	}
	m := fakemodel.New("opus", []fakemodel.Exchange{
		{Match: "How do I list orders?", Call: &fakemodel.Call{Name: "list_operations", Args: map[string]any{}}},
		{Match: `"spec":"store"`, Call: &fakemodel.Call{Name: "read_spec", Args: map[string]any{"spec": "store", "pointer": "paths/~1orders/get"}}},
		{Match: "operationId: listOrders", Reply: "Call [List the orders](/portals/pets/specs/store#/operations/listOrders)."},
	})
	c, err := New(Config{Model: m, Libraries: []*portal.Library{lib}})
	if err != nil {
		t.Fatal(err)
	}
	answer, err := c.Ask(t.Context(), "pets", portal.Everything, AskerOf("", "client"), "conv", "How do I list orders?")
	if err != nil || !strings.Contains(answer, "/portals/pets/specs/store#/operations/listOrders") || !m.Exhausted() {
		t.Errorf("answer %q, %v, script exhausted: %v", answer, err, m.Exhausted())
	}
}

func TestChatNamesEveryAPI(t *testing.T) {
	spec := func(title, version string) *fstest.MapFile {
		return &fstest.MapFile{Data: []byte("openapi: 3.0.3\ninfo:\n  title: " + title + "\n  version: " + version + "\npaths: {}\n")}
	}
	root := fstest.MapFS{"pets.yaml": spec("Pets", "1.0.0"), "store.yaml": spec("Store", "1.0.0"), "pets-v2.yaml": spec("Pets", "2.0.0")}
	lib, err := firstLibrary(portal.NewLibraries(portal.Config{Root: root, Portals: petsPortal([]portal.Section{
		{Title: "Pets", Type: portal.SpecSection, Input: "pets.yaml"},
		{Title: "Store", Type: portal.SpecSection, Input: "store.yaml"},
		{Title: "Pets v2", Type: portal.SpecSection, Input: "pets-v2.yaml"},
	})}))
	if err != nil {
		t.Fatal(err)
	}
	m := fakemodel.New("opus", []fakemodel.Exchange{{Match: "questions from customers about the Pets and Store APIs.", Reply: "Ask me about either."}})
	c, err := New(Config{Model: m, Libraries: []*portal.Library{lib}})
	if err != nil {
		t.Fatal(err)
	}
	if answer, err := c.Ask(t.Context(), "pets", portal.Everything, AskerOf("", "client"), "conv", "What can you help with?"); err != nil || !m.Exhausted() {
		t.Errorf("answer %q, %v, script exhausted: %v", answer, err, m.Exhausted())
	}
	mux := servedByPortal(t, c)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/portals/pets/chat", nil))
	if !strings.Contains(rec.Body.String(), "Ask about the Pets and Store APIs") {
		t.Errorf("the chat page's heading names not every API: %q", rec.Body)
	}
}

func TestReadSpecNamesItsSpec(t *testing.T) {
	m := fakemodel.New("opus", []fakemodel.Exchange{
		{Match: "Show me listPets.", Call: &fakemodel.Call{Name: "read_spec", Args: map[string]any{"spec": "api", "pointer": "paths/~1pets/get"}}},
		{Match: `"spec":"api","truncated":false`, Reply: "Here it is."},
	})
	if answer, err := newChat(t, m, Limits{}).Ask(t.Context(), "pets", portal.Everything, AskerOf("", "client"), "conv", "Show me listPets."); err != nil || !m.Exhausted() {
		t.Errorf("answer %q, %v, script exhausted: %v", answer, err, m.Exhausted())
	}
}

// twoPortals returns the libraries of two portals: Pet Shop, with a spec
// titled Pets, and Store, with a spec titled Store.
func twoPortals(t *testing.T) []*portal.Library {
	t.Helper()
	root := fstest.MapFS{
		"pets.yaml":  {Data: []byte("openapi: 3.0.3\ninfo:\n  title: Pets\n  version: 1.0.0\npaths:\n  /pets:\n    get:\n      operationId: listPets\n")},
		"store.yaml": {Data: []byte("openapi: 3.0.3\ninfo:\n  title: Store\n  version: 1.0.0\npaths:\n  /orders:\n    get:\n      operationId: listOrders\n")},
	}
	libs, err := portal.NewLibraries(portal.Config{Root: root, Portals: []portal.Portal{
		{Name: "Pet Shop", Sections: []portal.Section{{Title: "API", Type: portal.SpecSection, Input: "pets.yaml"}}},
		{Name: "Store", Sections: []portal.Section{{Title: "API", Type: portal.SpecSection, Input: "store.yaml"}}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	return libs
}

func TestChatInEveryPortal(t *testing.T) {
	libs := twoPortals(t)
	for i, want := range []struct{ name, slug, url, chat string }{
		{"Pet Shop", "pet-shop", "/portals/pet-shop/specs/api", "/portals/pet-shop/chat"},
		{"Store", "store", "/portals/store/specs/api", "/portals/store/chat"},
	} {
		if l := libs[i]; l.Name() != want.name || l.Slug() != want.slug || l.URL() != want.url || l.ChatURL() != want.chat {
			t.Errorf("library %d: %q %q %q %q, want %+v", i, l.Name(), l.Slug(), l.URL(), l.ChatURL(), want)
		}
	}
	m := fakemodel.New("opus", []fakemodel.Exchange{
		{Match: "questions from customers about the Store API.", Call: &fakemodel.Call{Name: "list_operations", Args: map[string]any{}}},
		{Match: `{"operations":[{"method":"get","operationId":"listOrders"`, Reply: "Use listOrders."},
	})
	c, err := New(Config{Model: m, Libraries: libs})
	if err != nil {
		t.Fatal(err)
	}
	if answer, err := c.Ask(t.Context(), "store", portal.Everything, AskerOf("", "client"), "conv", "How do I list orders?"); err != nil || !m.Exhausted() {
		t.Errorf("answer %q, %v, script exhausted: %v", answer, err, m.Exhausted())
	}
	if answer, err := c.Ask(t.Context(), "other", portal.Everything, AskerOf("", "client"), "conv", "How?"); err == nil {
		t.Errorf("a slug of no portal: %q", answer)
	}
	mux := servedByPortal(t, c)
	for path, want := range map[string]string{"/portals/pet-shop/chat": "Ask about the Pets API", "/portals/store/chat": "Ask about the Store API"} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		page := rec.Body.String()
		if rec.Code != http.StatusOK || !strings.Contains(page, want) || !strings.Contains(page, "portal-menu") || !strings.Contains(page, `href="/portals/pet-shop/specs/api"`) || !strings.Contains(page, `href="/portals/store/specs/api"`) {
			t.Errorf("%s: %d %q", path, rec.Code, page)
		}
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/chat", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("/chat: %d", rec.Code)
	}
}

func TestChatLimitSpansPortals(t *testing.T) {
	m := fakemodel.New("opus", []fakemodel.Exchange{{Reply: "one"}, {Reply: "two"}, {Reply: "three"}})
	c, err := New(Config{Model: m, Libraries: twoPortals(t), Limits: Limits{Questions: 2, Window: time.Hour}})
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []struct{ portal, conv string }{{"pet-shop", "a"}, {"store", "b"}} {
		if _, err := c.Ask(t.Context(), q.portal, portal.Everything, AskerOf("", "client"), q.conv, "A question?"); err != nil {
			t.Fatalf("%s: %v", q.portal, err)
		}
	}
	if _, err := c.Ask(t.Context(), "pet-shop", portal.Everything, AskerOf("", "client"), "c", "A third?"); !errors.Is(err, ErrRateLimited) {
		t.Errorf("a third question across two portals: err = %v, want ErrRateLimited", err)
	}
}

func TestNewNeedsAModelAndALibrary(t *testing.T) {
	m := fakemodel.New("opus", nil)
	for name, cfg := range map[string]Config{
		"no library": {Model: m},
		"no model":   {Libraries: []*portal.Library{library(t)}},
	} {
		if _, err := New(cfg); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}

func TestNewRefusesLibrariesItCannotServe(t *testing.T) {
	m := fakemodel.New("opus", nil)
	lib := library(t)
	for name, libs := range map[string][]*portal.Library{
		"no library":       {},
		"a nil library":    {lib, nil},
		"one portal twice": {lib, lib},
	} {
		if _, err := New(Config{Model: m, Libraries: libs}); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}

func TestChatPageMenu(t *testing.T) {
	c, err := New(Config{Model: fakemodel.New("opus", nil), Libraries: twoPortals(t)})
	if err != nil {
		t.Fatal(err)
	}
	mux := servedByPortal(t, c)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/portals/store/chat", nil))
	page := rec.Body.String()
	for _, want := range []string{"<summary>Store</summary>", `<a href="/portals/pet-shop/specs/api">Pet Shop</a><a href="/portals/store/specs/api">Store</a>`} {
		if !strings.Contains(page, want) {
			t.Errorf("the Store portal's chat page lacks %s: %q", want, page)
		}
	}
}

func TestAskNamesNoPortal(t *testing.T) {
	if _, err := newChat(t, fakemodel.New("opus", nil), Limits{}).Ask(t.Context(), "other", portal.Everything, AskerOf("", "client"), "conv", "How?"); !errors.Is(err, ErrNoPortal) {
		t.Errorf("err = %v, want ErrNoPortal", err)
	}
}

// libraryOf returns the library of a Pets portal whose only document says
// text.
func libraryOf(t *testing.T, text string) *portal.Library {
	t.Helper()
	root := fstest.MapFS{
		"api.yaml":  {Data: []byte("openapi: 3.0.3\ninfo:\n  title: Pets\n  version: 1.0.0\npaths:\n  /pets:\n    get:\n      operationId: listPets\n")},
		"docs/a.md": {Data: []byte("# Guide\n\n" + text + "\n")},
	}
	lib, err := firstLibrary(portal.NewLibraries(portal.Config{Root: root, Portals: petsPortal([]portal.Section{{Title: "API", Type: portal.SpecSection, Input: "api.yaml"}, {Title: "Documents", Type: portal.DocsSection, Input: "docs"}})}))
	if err != nil {
		t.Fatal(err)
	}
	return lib
}

func TestChatReloadKeepsConversations(t *testing.T) {
	m := fakemodel.New("opus", []fakemodel.Exchange{
		{Match: "What does the guide say?", Call: &fakemodel.Call{Name: "read_document", Args: map[string]any{"path": "docs/a.md"}}},
		{Match: "The old text.", Reply: "The guide says: the old text."},
		{Match: "And now?", Call: &fakemodel.Call{Name: "read_document", Args: map[string]any{"path": "docs/a.md"}}},
		{Match: "The new text.", Reply: "The guide says: the new text."},
		{Match: "Another?", Reply: "Three."},
	})
	c, err := New(Config{Model: m, Libraries: []*portal.Library{libraryOf(t, "The old text.")}, Limits: Limits{Questions: 3, Window: time.Hour, Turns: 2}})
	if err != nil {
		t.Fatal(err)
	}
	if answer, err := c.Ask(t.Context(), "pets", portal.Everything, AskerOf("", "client"), "conv", "What does the guide say?"); err != nil || !strings.Contains(answer, "the old text") {
		t.Fatalf("before the reload: %q, %v", answer, err)
	}
	if err := c.Reload([]*portal.Library{libraryOf(t, "The new text.")}); err != nil {
		t.Fatal(err)
	}
	// The conversation goes on, with the model reading the new library, and
	// its second turn is its last.
	if answer, err := c.Ask(t.Context(), "pets", portal.Everything, AskerOf("", "client"), "conv", "And now?"); err != nil || !strings.Contains(answer, "the new text") {
		t.Fatalf("after the reload: %q, %v", answer, err)
	}
	if _, err := c.Ask(t.Context(), "pets", portal.Everything, AskerOf("", "client"), "conv", "A third?"); !errors.Is(err, ErrTurns) {
		t.Errorf("a third turn after the reload: err = %v, want ErrTurns", err)
	}
	// The client's questions before the reload count against its limit too.
	if _, err := c.Ask(t.Context(), "pets", portal.Everything, AskerOf("", "client"), "other", "Another?"); err != nil {
		t.Fatalf("a third question: %v", err)
	}
	if _, err := c.Ask(t.Context(), "pets", portal.Everything, AskerOf("", "client"), "another", "A fourth?"); !errors.Is(err, ErrRateLimited) {
		t.Errorf("a fourth question after the reload: err = %v, want ErrRateLimited", err)
	}
}

func TestChatReloadServesTheNewPortals(t *testing.T) {
	m := fakemodel.New("opus", []fakemodel.Exchange{{Reply: "one"}})
	c, err := New(Config{Model: m, Libraries: []*portal.Library{library(t)}})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Reload(twoPortals(t)); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Ask(t.Context(), "pets", portal.Everything, AskerOf("", "client"), "conv", "Gone?"); !errors.Is(err, ErrNoPortal) {
		t.Errorf("a removed portal: err = %v, want ErrNoPortal", err)
	}
	if _, err := c.Ask(t.Context(), "store", portal.Everything, AskerOf("", "client"), "conv", "Here?"); err != nil {
		t.Errorf("an added portal: %v", err)
	}
	var paths []string
	for _, r := range c.Routes() {
		paths = append(paths, r.Pattern)
	}
	if got := strings.Join(paths, " "); !strings.Contains(got, "/portals/store/chat") || strings.Contains(got, "/portals/pets/chat") {
		t.Errorf("routes after the reload: %s", got)
	}
}

func TestChatReloadRefusesWhatNewRefuses(t *testing.T) {
	m := fakemodel.New("opus", []fakemodel.Exchange{{Reply: "one"}})
	c, err := New(Config{Model: m, Libraries: []*portal.Library{library(t)}})
	if err != nil {
		t.Fatal(err)
	}
	lib := library(t)
	for name, libs := range map[string][]*portal.Library{"no library": nil, "a nil library": {nil}, "one slug twice": {lib, lib}} {
		if err := c.Reload(libs); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
	if _, err := c.Ask(t.Context(), "pets", portal.Everything, AskerOf("", "client"), "conv", "Still here?"); err != nil {
		t.Errorf("after the refused reloads: %v", err)
	}
}

func TestChatPageMenuListsCollections(t *testing.T) {
	api := []portal.Section{{Title: "API", Type: portal.SpecSection, Input: "api.yaml"}}
	cfg := portal.Config{
		Root: fstest.MapFS{"api.yaml": {Data: []byte("openapi: 3.0.3\ninfo:\n  title: Pets\n  version: 1.0.0\npaths:\n  /pets:\n    get:\n      operationId: listPets\n")}},
		Portals: []portal.Portal{
			{Name: "Staging", Labels: []string{"autostage"}, Sections: api},
			{Name: "Ingest", Labels: []string{"live-ingest", "autostage"}, Sections: api},
			{Name: "Lab", Sections: api},
		},
		Collections: []portal.Collection{{Name: "AutoStage API", MatchLabels: []string{"autostage"}}, {Name: "Ingest APIs", MatchLabels: []string{"live-ingest"}}},
	}
	libs, err := portal.NewLibraries(cfg)
	if err != nil {
		t.Fatal(err)
	}
	c, err := New(Config{Model: fakemodel.New("opus", nil), Libraries: libs})
	if err != nil {
		t.Fatal(err)
	}
	cfg.Chat = c.Routes()
	prev := log.Writer()
	log.SetOutput(io.Discard)
	t.Cleanup(func() { log.SetOutput(prev) })
	h, err := portal.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/portals/staging/chat", "/portals/lab/chat"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		page := rec.Body.String()
		rest, ordered := page, true
		for _, want := range []string{
			`<span class="portal-menu-collection">AutoStage API</span>`, `<a href="/portals/staging/specs/api">Staging</a><a href="/portals/ingest/specs/api">Ingest</a>`,
			`<span class="portal-menu-collection">Ingest APIs</span>`, `<a href="/portals/ingest/specs/api">Ingest</a></div>`,
		} {
			i := strings.Index(rest, want)
			if ordered = i >= 0; !ordered {
				break
			}
			rest = rest[i+len(want):]
		}
		if rec.Code != http.StatusOK || !ordered || strings.Contains(page, `>Lab</a>`) {
			t.Errorf("%s: %d %q", path, rec.Code, page)
		}
	}
}

// without allows every portal but the one it names, and every section.
type without string

func (w without) Portal(p portal.Portal) bool                { return p.Name != string(w) }
func (w without) Section(portal.Portal, portal.Section) bool { return true }

// collected returns the portal handler with a chat over portals, each with
// the spec section API, and collections, for readers with access.
func collected(t *testing.T, access portal.Access, collections []portal.Collection, portals ...portal.Portal) http.Handler {
	t.Helper()
	for i := range portals {
		if portals[i].Type != portal.LinkPortal {
			portals[i].Sections = []portal.Section{{Title: "API", Type: portal.SpecSection, Input: "api.yaml"}}
		}
	}
	cfg := portal.Config{
		Root:        fstest.MapFS{"api.yaml": {Data: []byte("openapi: 3.0.3\ninfo:\n  title: Pets\n  version: 1.0.0\npaths:\n  /pets:\n    get:\n      operationId: listPets\n")}},
		Portals:     portals,
		Collections: collections,
		Access:      func(*http.Request) (portal.Access, error) { return access, nil },
	}
	libs, err := portal.NewLibraries(cfg)
	if err != nil {
		t.Fatal(err)
	}
	c, err := New(Config{Model: fakemodel.New("opus", nil), Libraries: libs})
	if err != nil {
		t.Fatal(err)
	}
	cfg.Chat = c.Routes()
	prev := log.Writer()
	log.SetOutput(io.Discard)
	t.Cleanup(func() { log.SetOutput(prev) })
	h, err := portal.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func TestChatPageMenuCountsTheListedPortals(t *testing.T) {
	auto := []portal.Collection{{Name: "AutoStage API", MatchLabels: []string{"autostage"}}}
	h := collected(t, portal.Everything, auto, portal.Portal{Name: "Staging", Labels: []string{"autostage"}}, portal.Portal{Name: "Lab"})
	for _, path := range []string{"/portals/staging/chat", "/portals/lab/chat"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if page := rec.Body.String(); rec.Code != http.StatusOK || strings.Contains(page, `<details class="portal-menu">`) {
			t.Errorf("%s: %d, with a menu of one listed portal: %q", path, rec.Code, page)
		}
	}
}

func TestChatPageMenuFollowsAccess(t *testing.T) {
	collections := []portal.Collection{{Name: "AutoStage API", MatchLabels: []string{"autostage"}}, {Name: "Ingest APIs", MatchLabels: []string{"live-ingest"}}}
	h := collected(t, without("Ingest"), collections,
		portal.Portal{Name: "Staging", Labels: []string{"autostage"}},
		portal.Portal{Name: "Production", Labels: []string{"autostage"}},
		portal.Portal{Name: "Ingest", Labels: []string{"live-ingest"}})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/portals/staging/chat", nil))
	page := rec.Body.String()
	if rec.Code != http.StatusOK || !strings.Contains(page, `>AutoStage API</span>`) || !strings.Contains(page, `>Production</a>`) || strings.Contains(page, "Ingest") {
		t.Errorf("the chat menu without Ingest: %d %q", rec.Code, page)
	}
}

func TestChatPageMenuListsLinkPortals(t *testing.T) {
	h := collected(t, portal.Everything, nil, portal.Portal{Name: "Pets"}, portal.Portal{Name: "Store"})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/portals/pets/chat", nil))
	if page := rec.Body.String(); rec.Code != http.StatusOK || strings.Contains(page, "Status") {
		t.Fatalf("without a link portal: %d %q", rec.Code, page)
	}
	h = collected(t, portal.Everything, nil, portal.Portal{Name: "Pets"}, portal.Portal{Name: "Status", Type: portal.LinkPortal, URL: "https://status.example.com/"})
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/portals/pets/chat", nil))
	page := rec.Body.String()
	if want := `<a href="/portals/pets/specs/api">Pets</a><a href="https://status.example.com/">Status</a>`; rec.Code != http.StatusOK || !strings.Contains(page, want) {
		t.Errorf("the chat page's menu lacks the link portal: %d %q", rec.Code, page)
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/portals/status/chat", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("a link portal's chat page: %d, want 404", rec.Code)
	}
}

func TestChatPageMenuDropsACollectionWithoutTheChatsPortals(t *testing.T) {
	api := []portal.Section{{Title: "API", Type: portal.SpecSection, Input: "api.yaml"}}
	cfg := portal.Config{
		Root: fstest.MapFS{"api.yaml": {Data: []byte("openapi: 3.0.3\ninfo:\n  title: Pets\n  version: 1.0.0\npaths:\n  /pets:\n    get:\n      operationId: listPets\n")}},
		Portals: []portal.Portal{
			{Name: "Pets", Labels: []string{"apis"}, Sections: api},
			{Name: "Store", Labels: []string{"shops"}, Sections: api},
			{Name: "Status", Type: portal.LinkPortal, URL: "https://status.example.com/", Labels: []string{"apis"}},
		},
		Collections: []portal.Collection{{Name: "APIs", MatchLabels: []string{"apis"}}, {Name: "Shops", MatchLabels: []string{"shops"}}},
	}
	libs, err := portal.NewLibraries(cfg)
	if err != nil {
		t.Fatal(err)
	}
	// A chat over Pets alone, in a portal handler that serves Store too.
	c, err := New(Config{Model: fakemodel.New("opus", nil), Libraries: libs[:1]})
	if err != nil {
		t.Fatal(err)
	}
	cfg.Chat = c.Routes()
	h, err := portal.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/portals/pets/chat", nil))
	page := rec.Body.String()
	if rec.Code != http.StatusOK || !strings.Contains(page, `>APIs</span>`) || !strings.Contains(page, `>Status</a>`) || strings.Contains(page, "Shops") || strings.Contains(page, `>Store</a>`) {
		t.Errorf("the chat menu of a chat over Pets alone: %d %q", rec.Code, page)
	}
}

// showing allows every portal and section, and the collections with the
// listed names.
type showing []string

func (showing) Portal(portal.Portal) bool                  { return true }
func (showing) Section(portal.Portal, portal.Section) bool { return true }
func (s showing) Collection(c portal.Collection) bool      { return slices.Contains(s, c.Name) }

func TestChatPageMenuFollowsTheCollectionAccess(t *testing.T) {
	collections := []portal.Collection{{Name: "AutoStage API", MatchLabels: []string{"autostage"}}, {Name: "Ingest APIs", MatchLabels: []string{"live-ingest"}}}
	h := collected(t, showing{"AutoStage API"}, collections,
		portal.Portal{Name: "Staging", Labels: []string{"autostage"}},
		portal.Portal{Name: "Production", Labels: []string{"autostage"}},
		portal.Portal{Name: "Ingest", Labels: []string{"live-ingest"}})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/portals/staging/chat", nil))
	page := rec.Body.String()
	if rec.Code != http.StatusOK || !strings.Contains(page, `>AutoStage API</span>`) || !strings.Contains(page, `>Production</a>`) || strings.Contains(page, "Ingest") {
		t.Errorf("the chat menu without the collection Ingest APIs: %d %q", rec.Code, page)
	}
}
