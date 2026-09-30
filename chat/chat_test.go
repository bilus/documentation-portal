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
	lib, err := portal.NewLibrary(portal.Config{Root: root, SpecPath: "api.yaml", DocsPath: "docs"})
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
		{Match: `"where":"a.md:3"`, Reply: "Call GET /pets, as [Getting started](/docs/a.md) shows."},
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
