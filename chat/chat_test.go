package chat

import (
	"context"
	"errors"
	"iter"
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

func library(t *testing.T) *portal.Library {
	t.Helper()
	root := fstest.MapFS{
		"api.yaml":  {Data: []byte("openapi: 3.0.3\ninfo:\n  title: Pets\n  version: 1.0.0\npaths:\n  /pets:\n    get:\n      operationId: listPets\n      summary: List the pets\n")},
		"docs/a.md": {Data: []byte("# Getting started\n\nList the pets with GET /pets.\n")},
	}
	lib, err := portal.NewLibrary(portal.Config{Root: root, Sections: []portal.Section{{Title: "API", Type: portal.SpecSection, Input: "api.yaml"}, {Title: "Documents", Type: portal.DocsSection, Input: "docs"}}})
	if err != nil {
		t.Fatal(err)
	}
	return lib
}

func newChat(t *testing.T, m model.LLM, limits Limits) *Chat {
	t.Helper()
	c, err := New(Config{Model: m, Library: library(t), Limits: limits})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestAskLooksUpTheAnswer(t *testing.T) {
	m := fakemodel.New("opus", []fakemodel.Exchange{
		{Match: "the Pets API", Call: &fakemodel.Call{Name: "search", Args: map[string]any{"query": "pets"}}},
		{Match: `"where":"docs/a.md:3"`, Reply: "Call GET /pets, as [Getting started](/docs/a.md) shows."},
	})
	answer, err := newChat(t, m, Limits{}).Ask(t.Context(), "client", "conv", "How do I list pets?")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(answer, "GET /pets") || !m.Exhausted() {
		t.Errorf("answer %q, script exhausted: %v", answer, m.Exhausted())
	}
}

func TestAskRefusesOverLimits(t *testing.T) {
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	c := newChat(t, fakemodel.New("opus", []fakemodel.Exchange{{Reply: "one"}, {Reply: "two"}, {Reply: "three"}}),
		Limits{QuestionLength: 10, Questions: 2, Window: time.Hour, Turns: 2, Idle: 2 * time.Hour})
	c.now = func() time.Time { return now }
	ask := func(client, conv, q string) error {
		_, err := c.Ask(t.Context(), client, conv, q)
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
	if _, ok := c.convs["c1"]; !ok {
		t.Fatal("c1 is gone too early")
	}
	c.admit(t.Context(), "z", "c3")
	if _, ok := c.convs["c1"]; ok {
		t.Error("an idle conversation stayed")
	}
}

func TestAskStopsAfterTooManyLookups(t *testing.T) {
	search := &fakemodel.Call{Name: "search", Args: map[string]any{"query": "pets"}}
	m := fakemodel.New("opus", []fakemodel.Exchange{{Call: search}, {Call: search}, {Reply: "never"}})
	if _, err := newChat(t, m, Limits{ToolCalls: 1}).Ask(t.Context(), "a", "c", "pets?"); !errors.Is(err, ErrTooManyTools) {
		t.Errorf("err = %v, want ErrTooManyTools", err)
	}
}

func TestAskAllowsTheLastLookup(t *testing.T) {
	search := &fakemodel.Call{Name: "search", Args: map[string]any{"query": "pets"}}
	m := fakemodel.New("opus", []fakemodel.Exchange{{Call: search}, {Call: search}, {Reply: "Call GET /pets."}})
	if answer, err := newChat(t, m, Limits{ToolCalls: 2}).Ask(t.Context(), "a", "c", "pets?"); err != nil || answer != "Call GET /pets." {
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
	if _, err := c.Ask(t.Context(), "a", "c", "pets?"); !errors.Is(err, ErrTooManyTools) {
		t.Fatalf("err = %v, want ErrTooManyTools", err)
	}
	if answer, err := c.Ask(t.Context(), "a", "c", "And cats?"); err != nil || answer != "No cats." {
		t.Errorf("the next question: answer %q, err %v", answer, err)
	}
}

func TestAskRunsNoLookupOverTheLimit(t *testing.T) {
	search := func(q string) fakemodel.Call { return fakemodel.Call{Name: "search", Args: map[string]any{"query": q}} }
	m := fakemodel.New("opus", []fakemodel.Exchange{{Calls: []fakemodel.Call{search("pets"), search("cats"), search("dogs")}}})
	c := newChat(t, m, Limits{ToolCalls: 2})
	if _, err := c.Ask(t.Context(), "a", "c", "pets?"); !errors.Is(err, ErrTooManyTools) {
		t.Fatalf("err = %v, want ErrTooManyTools", err)
	}
	got, err := c.sessions.Get(t.Context(), &session.GetRequest{AppName: appName, UserID: userID, SessionID: "c"})
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
	if _, err := newChat(t, failing{anthropicmodel.ErrTruncated}, Limits{}).Ask(t.Context(), "a", "c", "Tell me everything"); !errors.Is(err, ErrCutOff) {
		t.Errorf("err = %v, want ErrCutOff", err)
	}
}

func TestAskLeavesThoughtsOut(t *testing.T) {
	m := fakemodel.New("opus", []fakemodel.Exchange{{Thought: "The guide says GET /pets.", Reply: "Call GET /pets."}})
	if answer, err := newChat(t, m, Limits{}).Ask(t.Context(), "a", "c", "pets?"); err != nil || answer != "Call GET /pets." {
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
		if _, err := c.Ask(t.Context(), "a", step.conv, "q"); !errors.Is(err, step.want) {
			t.Errorf("at +%dm in %s: err %v, want %v", step.minutes, step.conv, err, step.want)
		}
	}
}

func TestAskForgetsClientsAfterTheWindow(t *testing.T) {
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	c := newChat(t, fakemodel.New("opus", []fakemodel.Exchange{{Reply: "1"}, {Reply: "2"}, {Reply: "3"}}), Limits{Window: time.Hour})
	c.now = func() time.Time { return now }
	for _, client := range []string{"a", "b"} {
		if _, err := c.Ask(t.Context(), client, "c-"+client, "q"); err != nil {
			t.Fatal(err)
		}
	}
	now = now.Add(time.Hour)
	if _, err := c.Ask(t.Context(), "z", "c-z", "q"); err != nil {
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
		_, err := c.sessions.Get(t.Context(), &session.GetRequest{AppName: appName, UserID: userID, SessionID: "c1"})
		return err
	}
	if _, err := c.Ask(t.Context(), "a", "c1", "q"); err != nil {
		t.Fatal(err)
	}
	if err := session1(); err != nil {
		t.Fatalf("c1's session: %v", err)
	}
	now = now.Add(time.Hour)
	if _, err := c.Ask(t.Context(), "a", "c2", "q"); err != nil {
		t.Fatal(err)
	}
	if session1() == nil {
		t.Error("c1's session outlived its conversation")
	}
}

func TestAskEndsAConversationThatGrewTooLong(t *testing.T) {
	m := fakemodel.New("opus", []fakemodel.Exchange{{Reply: strings.Repeat("x", 600)}, {Reply: "never"}})
	c := newChat(t, m, Limits{History: 500})
	if _, err := c.Ask(t.Context(), "a", "c", "q1"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Ask(t.Context(), "a", "c", "q2"); !errors.Is(err, ErrTurns) {
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
	if _, err := newChat(t, declining{}, Limits{}).Ask(t.Context(), "a", "c", "Tell me a secret"); !errors.Is(err, ErrDeclined) {
		t.Errorf("err = %v, want ErrDeclined", err)
	}
}

func TestChatToolsNameTheSpec(t *testing.T) {
	root := fstest.MapFS{
		"pets.yaml":  {Data: []byte("openapi: 3.0.3\ninfo:\n  title: Pets\n  version: 1.0.0\npaths:\n  /pets:\n    get:\n      operationId: listPets\n")},
		"store.yaml": {Data: []byte("openapi: 3.0.3\ninfo:\n  title: Store\n  version: 1.0.0\npaths:\n  /orders:\n    get:\n      operationId: listOrders\n      summary: List the orders\n")},
	}
	lib, err := portal.NewLibrary(portal.Config{Root: root, Sections: []portal.Section{
		{Title: "Pets", Type: portal.SpecSection, Input: "pets.yaml"},
		{Title: "Store", Type: portal.SpecSection, Input: "store.yaml"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	m := fakemodel.New("opus", []fakemodel.Exchange{
		{Match: "How do I list orders?", Call: &fakemodel.Call{Name: "list_operations", Args: map[string]any{}}},
		{Match: `"spec":"store"`, Call: &fakemodel.Call{Name: "read_spec", Args: map[string]any{"spec": "store", "pointer": "paths/~1orders/get"}}},
		{Match: "operationId: listOrders", Reply: "Call [List the orders](/specs/store#/operations/listOrders)."},
	})
	c, err := New(Config{Model: m, Library: lib})
	if err != nil {
		t.Fatal(err)
	}
	answer, err := c.Ask(t.Context(), "client", "conv", "How do I list orders?")
	if err != nil || !strings.Contains(answer, "/specs/store#/operations/listOrders") || !m.Exhausted() {
		t.Errorf("answer %q, %v, script exhausted: %v", answer, err, m.Exhausted())
	}
}
