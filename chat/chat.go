// Package chat answers customers' questions about an API from its published
// documentation, with a model, read-only tools and a live page.
//
// New builds the chat over the libraries of the portals, and Routes gives
// their chat pages for the Chat of the portal configuration, whose access
// hook and account hook the pages follow. Config.Reader, a request hook,
// names each request's reader, so that a signed-in reader's questions count
// against one question limit from any client.
package chat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/agent/llmagent"
	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/runner"
	"google.golang.org/adk/v2/session"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/genai"

	"github.com/bilus/documentation-portal/anthropicmodel"
	"github.com/bilus/documentation-portal/portal"
)

// Limits bound what one asker and one conversation may cost.
type Limits struct {
	QuestionLength int           // characters in one question; 0 means 2000
	ToolCalls      int           // per answer; 0 means 12
	Questions      int           // per asker in Window; 0 means 20
	Window         time.Duration // 0 means an hour
	Turns          int           // questions in one conversation; 0 means 20
	History        int           // bytes of messages and lookups in one conversation; 0 means 512 KiB
	Idle           time.Duration // a conversation unused this long is dropped; 0 means an hour
}

func (l Limits) withDefaults() Limits {
	or := func(v, d int) int {
		if v > 0 {
			return v
		}
		return d
	}
	orDuration := func(v, d time.Duration) time.Duration {
		if v > 0 {
			return v
		}
		return d
	}
	return Limits{
		QuestionLength: or(l.QuestionLength, 2000),
		ToolCalls:      or(l.ToolCalls, 12),
		Questions:      or(l.Questions, 20),
		Window:         orDuration(l.Window, time.Hour),
		Turns:          or(l.Turns, 20),
		History:        or(l.History, 512<<10),
		Idle:           orDuration(l.Idle, time.Hour),
	}
}

// Config configures the chat.
type Config struct {
	Model     model.LLM
	Libraries []*portal.Library // one for each portal, whose chat page the chat serves
	Limits    Limits

	// Reader is the reader hook: it returns the reader ID of r's reader,
	// such as the subject of the reader's verified token, or "" for a reader
	// who is not signed in. The question limit counts a signed-in reader's
	// questions by the reader ID, from any client, and any other reader's by
	// the client. A chat page keeps the reader ID of its load in its page
	// session, which the reader's browser can read, and answers with
	// Cache-Control: private. Without the hook, the chat counts every
	// reader's questions by the client.
	Reader func(r *http.Request) string
}

// The errors that Ask returns for a question it does not answer.
var (
	ErrEmpty        = errors.New("chat: the question is empty")
	ErrTooLong      = errors.New("chat: the question is too long")
	ErrRateLimited  = errors.New("chat: too many questions from this asker")
	ErrTurns        = errors.New("chat: the conversation has reached its length")
	ErrTooManyTools = errors.New("chat: the answer needed too many lookups")
	ErrNoAnswer     = errors.New("chat: the model returned no answer")
	ErrDeclined     = errors.New("chat: the model declined the question")
	ErrCutOff       = errors.New("chat: the answer was cut off")
	ErrUnavailable  = errors.New("chat: the model is unavailable")
	ErrNoPortal     = errors.New("chat: no portal has the slug")
	ErrNoAsker      = errors.New("chat: the question has no asker")
)

// ADK keys a session by application, user and session; every conversation
// has one user, and its own session.
const appName, userID, agentName = "docportal", "reader", "support"

// Chat answers questions in conversations, each an ADK session, on the chat
// page of each portal.
type Chat struct {
	model    model.LLM
	agents   []*portalAgent // one for each library, in order
	sessions session.Service
	limits   Limits
	reader   func(*http.Request) string // the reader hook, or nil
	now      func() time.Time

	mu    sync.Mutex
	asked map[Asker][]time.Time // when each asker's recent questions came
	convs map[string]*conversation
}

// portalAgent answers from the library of one portal.
type portalAgent struct {
	lib    *portal.Library
	runner *runner.Runner
}

type conversation struct {
	turns    int
	size     int // bytes of its history
	lastUsed time.Time
}

// New builds the chat's agent for each of cfg's libraries over cfg's model,
// with one question limit for all of them, or refuses no model, no library,
// a nil library and two libraries with one slug.
func New(cfg Config) (*Chat, error) {
	if cfg.Model == nil || len(cfg.Libraries) == 0 {
		return nil, errors.New("chat: a model and a library are required")
	}
	c := &Chat{
		model:    cfg.Model,
		sessions: session.InMemoryService(),
		limits:   cfg.Limits.withDefaults(),
		reader:   cfg.Reader,
		now:      time.Now,
		asked:    map[Asker][]time.Time{},
		convs:    map[string]*conversation{},
	}
	agents, err := newAgents(cfg.Model, cfg.Libraries, c.sessions)
	if err != nil {
		return nil, err
	}
	c.agents = agents
	return c, nil
}

// newAgents builds an agent for each of libs over m, with their
// conversations in sessions, or refuses no library, a nil library and two
// libraries with one slug.
func newAgents(m model.LLM, libs []*portal.Library, sessions session.Service) ([]*portalAgent, error) {
	if len(libs) == 0 {
		return nil, errors.New("chat: a library is required")
	}
	agents := make([]*portalAgent, 0, len(libs))
	for i, lib := range libs {
		if lib == nil {
			return nil, fmt.Errorf("chat: library %d is nil", i+1)
		}
		for _, a := range agents {
			if a.lib.Slug() == lib.Slug() {
				return nil, fmt.Errorf("chat: two libraries have the slug %q", lib.Slug())
			}
		}
		a, err := newAgent(m, lib, sessions)
		if err != nil {
			return nil, err
		}
		agents = append(agents, a)
	}
	return agents, nil
}

// newAgent builds the agent that answers from lib with m, and its runner,
// which keeps its conversations in sessions. Its tools and its instructions
// read the library of each answer, which Ask limits to the reader's access.
func newAgent(m model.LLM, lib *portal.Library, sessions session.Service) (*portalAgent, error) {
	tt, err := tools()
	if err != nil {
		return nil, err
	}
	a, err := llmagent.New(llmagent.Config{
		Name:        agentName,
		Description: "Answers customers' questions about the API from its documentation.",
		Model:       m,
		// A provider, unlike Instruction, is not a template, so braces in the
		// prompt stay as they are.
		InstructionProvider: func(ctx agent.ReadonlyContext) (string, error) {
			lib, err := libraryIn(ctx)
			if err != nil {
				return "", err
			}
			return instruction(lib.Titles()), nil
		},
		Tools:               tt,
		BeforeToolCallbacks: []llmagent.BeforeToolCallback{spend},
	})
	if err != nil {
		return nil, err
	}
	r, err := runner.New(runner.Config{AppName: appName, Agent: a, SessionService: sessions, AutoCreateSession: true})
	if err != nil {
		return nil, err
	}
	return &portalAgent{lib: lib, runner: r}, nil
}

// Reload replaces the chat's agents with agents for libs, one for each, over
// the chat's model, and keeps its sessions, conversations and question
// counts, so that a new snapshot of the documentation answers the
// conversations in progress. It refuses what New refuses: no library, a nil
// library and two libraries with one slug, and then keeps the old agents.
func (c *Chat) Reload(libs []*portal.Library) error {
	agents, err := newAgents(c.model, libs, c.sessions)
	if err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.agents = agents
	return nil
}

// agentFor returns the agent of the portal whose slug is slug, or false.
func (c *Chat) agentFor(slug string) (*portalAgent, bool) {
	for _, a := range c.currentAgents() {
		if a.lib.Slug() == slug {
			return a, true
		}
	}
	return nil, false
}

// currentAgents returns the agents, which a reload replaces as a whole.
func (c *Chat) currentAgents() []*portalAgent {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.agents
}

// Ask answers question, asked by asker in conversation conv on the chat page
// of the portal whose slug is portalSlug, in markdown, from the sections of
// that portal visible to the reader with access, and counts it against the
// asker's question limit. It answers a portal hidden from the reader as a
// missing one, with ErrNoPortal, and refuses the zero Asker with ErrNoAsker.
//
// The conversation keeps a separate history for each view of the portal, so
// that no answer reads an earlier lookup from a section hidden from its
// reader. The view of a reader who sees every section, as every reader does
// without an access hook, stays the same across snapshots. The view of any
// other reader changes with the set of its visible sections and with their
// configuration.
func (c *Chat) Ask(ctx context.Context, portalSlug string, access portal.Access, asker Asker, conv, question string) (string, error) {
	if asker == (Asker{}) {
		return "", ErrNoAsker
	}
	a, ok := c.agentFor(portalSlug)
	if !ok {
		return "", fmt.Errorf("%w %q", ErrNoPortal, portalSlug)
	}
	lib, ok := a.lib.For(access)
	if !ok {
		return "", fmt.Errorf("%w %q", ErrNoPortal, portalSlug)
	}
	// A conversation lives in one portal, with a session for each view of the portal.
	conv = portalSlug + "/" + viewOf(lib) + "/" + conv
	question = strings.TrimSpace(question)
	switch {
	case question == "":
		return "", ErrEmpty
	case utf8.RuneCountInString(question) > c.limits.QuestionLength:
		return "", ErrTooLong
	}
	cv, err := c.admit(ctx, asker, conv)
	if err != nil {
		return "", err
	}
	c.grow(cv, len(question))
	calls := 0
	var answer strings.Builder
	msg := genai.NewContentFromText(question, genai.RoleUser)
	run := context.WithValue(ctx, budgetKey{}, &budget{max: int64(c.limits.ToolCalls)})
	run = context.WithValue(run, libraryKey{}, lib)
	for ev, err := range a.runner.Run(run, userID, conv, msg, agent.RunConfig{}) {
		if err != nil {
			switch {
			case errors.Is(err, anthropicmodel.ErrRefused):
				return "", ErrDeclined
			case errors.Is(err, anthropicmodel.ErrTruncated):
				return "", ErrCutOff
			}
			return "", fmt.Errorf("%w: %v", ErrUnavailable, err)
		}
		if ev.Content == nil {
			continue
		}
		c.grow(cv, size(ev.Content))
		answered := false
		for _, p := range ev.Content.Parts {
			switch {
			case p.FunctionCall != nil:
				calls++
			case p.FunctionResponse != nil:
				answered = true
			}
		}
		// The run stops after the tools answer the calls over the limit: a
		// call without its result in the history would make the
		// conversation's next request invalid.
		if calls > c.limits.ToolCalls && answered {
			return "", ErrTooManyTools
		}
		if ev.IsFinalResponse() {
			for _, p := range ev.Content.Parts {
				if !p.Thought {
					answer.WriteString(p.Text)
				}
			}
		}
	}
	if strings.TrimSpace(answer.String()) == "" {
		return "", ErrNoAnswer
	}
	return answer.String(), nil
}

// admit counts the question against the asker's question limit and the
// conversation's length, and returns the conversation.
func (c *Chat) admit(ctx context.Context, asker Asker, conv string) (*conversation, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.now()
	c.sweep(ctx, now)
	if len(c.asked[asker]) >= c.limits.Questions {
		return nil, ErrRateLimited
	}
	cv := c.convs[conv]
	if cv == nil {
		cv = &conversation{}
		c.convs[conv] = cv
	}
	if cv.turns >= c.limits.Turns || cv.size >= c.limits.History {
		return nil, ErrTurns
	}
	cv.turns++
	cv.lastUsed = now
	c.asked[asker] = append(c.asked[asker], now)
	return cv, nil
}

// sweep forgets the questions older than Window, and drops the conversations
// that nobody used for Idle, with their sessions. The caller holds c.mu.
func (c *Chat) sweep(ctx context.Context, now time.Time) {
	for asker, times := range c.asked {
		recent := times[:0:0]
		for _, t := range times {
			if now.Sub(t) < c.limits.Window {
				recent = append(recent, t)
			}
		}
		if len(recent) == 0 {
			delete(c.asked, asker)
		} else {
			c.asked[asker] = recent
		}
	}
	for id, cv := range c.convs {
		if now.Sub(cv.lastUsed) >= c.limits.Idle {
			delete(c.convs, id)
			// The session goes with it; a failed delete leaves memory only.
			_ = c.sessions.Delete(ctx, &session.DeleteRequest{AppName: appName, UserID: userID, SessionID: id})
		}
	}
}

// budget counts the lookups of one answer, and travels in its run's context.
type budget struct {
	used atomic.Int64
	max  int64
}

type budgetKey struct{}

// libraryKey keys the library of one answer in its run's context.
type libraryKey struct{}

// libraryIn returns the library of the answer that runs in ctx, which Ask
// limits to the reader's access, or an error outside an answer of Ask.
func libraryIn(ctx context.Context) (*portal.Library, error) {
	if lib, ok := ctx.Value(libraryKey{}).(*portal.Library); ok {
		return lib, nil
	}
	return nil, errors.New("chat: no library for this answer")
}

// spend runs before each tool call. Once the answer has used its lookups, the
// call reads nothing and returns an error to the model; the calls of one turn
// may run at once, so the count is atomic.
func spend(ctx agent.Context, _ tool.Tool, _ map[string]any) (map[string]any, error) {
	if b, ok := ctx.Value(budgetKey{}).(*budget); ok && b.used.Add(1) > b.max {
		return map[string]any{"error": "no lookups left for this question"}, nil
	}
	return nil, nil
}

// grow adds n bytes to the history of cv.
func (c *Chat) grow(cv *conversation, n int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	cv.size += n
}

// size returns the bytes that content adds to a conversation's history.
func size(content *genai.Content) int {
	n := 0
	for _, p := range content.Parts {
		n += len(p.Text) + len(p.ThoughtSignature)
		if p.FunctionCall != nil {
			args, _ := json.Marshal(p.FunctionCall.Args)
			n += len(args)
		}
		if p.FunctionResponse != nil {
			response, _ := json.Marshal(p.FunctionResponse.Response)
			n += len(response)
		}
	}
	return n
}
