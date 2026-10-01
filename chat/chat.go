// Package chat answers customers' questions about an API from its published
// documentation, with a model, read-only tools and a live page.
package chat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/agent/llmagent"
	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/runner"
	"google.golang.org/adk/v2/session"
	"google.golang.org/genai"

	"github.com/bilus/documentation-portal/anthropicmodel"
	"github.com/bilus/documentation-portal/portal"
)

// Limits bound what one client and one conversation may cost.
type Limits struct {
	QuestionLength int           // characters in one question; 0 means 2000
	ToolCalls      int           // per answer; 0 means 12
	Questions      int           // per client in Window; 0 means 20
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
	Model   model.LLM
	Library *portal.Library
	Limits  Limits
}

// The errors that Ask returns for a question it does not answer.
var (
	ErrEmpty        = errors.New("chat: the question is empty")
	ErrTooLong      = errors.New("chat: the question is too long")
	ErrRateLimited  = errors.New("chat: too many questions from this client")
	ErrTurns        = errors.New("chat: the conversation has reached its length")
	ErrTooManyTools = errors.New("chat: the answer needed too many lookups")
	ErrNoAnswer     = errors.New("chat: the model returned no answer")
	ErrDeclined     = errors.New("chat: the model declined the question")
	ErrUnavailable  = errors.New("chat: the model is unavailable")
)

// ADK keys a session by application, user and session; every conversation
// has one user, and its own session.
const appName, userID, agentName = "docportal", "reader", "support"

// Chat answers questions in conversations, each an ADK session.
type Chat struct {
	lib      *portal.Library
	runner   *runner.Runner
	sessions session.Service
	limits   Limits
	now      func() time.Time

	mu    sync.Mutex
	asked map[string][]time.Time // client: when its recent questions came
	convs map[string]*conversation
}

type conversation struct {
	turns    int
	size     int // bytes of its history
	lastUsed time.Time
}

// New builds the chat's agent over cfg's library and model.
func New(cfg Config) (*Chat, error) {
	if cfg.Model == nil || cfg.Library == nil {
		return nil, errors.New("chat: a model and a library are required")
	}
	tt, err := tools(cfg.Library)
	if err != nil {
		return nil, err
	}
	lib := cfg.Library
	a, err := llmagent.New(llmagent.Config{
		Name:        agentName,
		Description: "Answers customers' questions about the API from its documentation.",
		Model:       cfg.Model,
		// A provider, unlike Instruction, is not a template, so braces in the
		// prompt stay as they are.
		InstructionProvider: func(agent.ReadonlyContext) (string, error) {
			return instruction(lib.Title()), nil
		},
		Tools: tt,
	})
	if err != nil {
		return nil, err
	}
	sessions := session.InMemoryService()
	r, err := runner.New(runner.Config{AppName: appName, Agent: a, SessionService: sessions, AutoCreateSession: true})
	if err != nil {
		return nil, err
	}
	return &Chat{
		lib:      lib,
		runner:   r,
		sessions: sessions,
		limits:   cfg.Limits.withDefaults(),
		now:      time.Now,
		asked:    map[string][]time.Time{},
		convs:    map[string]*conversation{},
	}, nil
}

// Ask answers question, asked by client in conversation, in markdown.
func (c *Chat) Ask(ctx context.Context, client, conv, question string) (string, error) {
	question = strings.TrimSpace(question)
	switch {
	case question == "":
		return "", ErrEmpty
	case utf8.RuneCountInString(question) > c.limits.QuestionLength:
		return "", ErrTooLong
	}
	cv, err := c.admit(ctx, client, conv)
	if err != nil {
		return "", err
	}
	c.grow(cv, len(question))
	calls := 0
	var answer strings.Builder
	msg := genai.NewContentFromText(question, genai.RoleUser)
	for ev, err := range c.runner.Run(ctx, userID, conv, msg, agent.RunConfig{}) {
		if err != nil {
			if errors.Is(err, anthropicmodel.ErrRefused) {
				return "", ErrDeclined
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

// admit counts the question against client's rate and the conversation's
// length, and returns the conversation.
func (c *Chat) admit(ctx context.Context, client, conv string) (*conversation, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.now()
	c.sweep(ctx, now)
	if len(c.asked[client]) >= c.limits.Questions {
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
	c.asked[client] = append(c.asked[client], now)
	return cv, nil
}

// sweep forgets the questions older than Window, and drops the conversations
// that nobody used for Idle, with their sessions. The caller holds c.mu.
func (c *Chat) sweep(ctx context.Context, now time.Time) {
	for client, times := range c.asked {
		recent := times[:0:0]
		for _, t := range times {
			if now.Sub(t) < c.limits.Window {
				recent = append(recent, t)
			}
		}
		if len(recent) == 0 {
			delete(c.asked, client)
		} else {
			c.asked[client] = recent
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
