// Package fakemodel is a scripted model.LLM for tests: no network, loud on
// any request its script does not expect. It is a copy of humanizer/fakemodel
// in the Scratchpad repository.
package fakemodel

import (
	"context"
	"encoding/json"
	"fmt"
	"iter"
	"strings"

	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"
)

var _ model.LLM = (*Fake)(nil)

// Exchange pairs a substring the next prompt must contain with the reply to
// return. Requests consume the script in order.
type Exchange struct {
	Match string
	Reply string
	// Call, when set, makes this exchange a tool call rather than an answer.
	// Without it a scripted model can never exercise an agent's tool loop.
	Call *Call
	// Calls carries several tool calls in one turn, which is what real
	// models emit and what makes a runner's concurrency visible.
	Calls []Call
}

// Call is a scripted tool call.
type Call struct {
	Name string
	Args map[string]any
}

type Fake struct {
	name   string
	script []Exchange
	next   int
}

func New(name string, script []Exchange) *Fake {
	return &Fake{name: name, script: script}
}

func (f *Fake) Name() string {
	return f.name
}

func (f *Fake) GenerateContent(ctx context.Context, req *model.LLMRequest, stream bool) iter.Seq2[*model.LLMResponse, error] {
	prompt := promptOf(req)
	return func(yield func(*model.LLMResponse, error) bool) {
		if f.Exhausted() {
			yield(nil, fmt.Errorf("fakemodel %q: script of length %d is exhausted, unexpected prompt: %s",
				f.name, len(f.script), excerpt(prompt)))
			return
		}
		next := f.script[f.next]
		if !strings.Contains(prompt, next.Match) {
			yield(nil, fmt.Errorf("fakemodel %q: exchange %d wanted a prompt containing %q, got: %s",
				f.name, f.next, next.Match, excerpt(prompt)))
			return
		}
		f.next++
		calls := next.Calls
		if next.Call != nil {
			calls = append([]Call{*next.Call}, calls...)
		}
		if len(calls) > 0 {
			parts := make([]*genai.Part, 0, len(calls))
			for i, c := range calls {
				parts = append(parts, &genai.Part{FunctionCall: &genai.FunctionCall{
					ID:   fmt.Sprintf("call_%d_%d", f.next, i),
					Name: c.Name,
					Args: c.Args,
				}})
			}
			yield(&model.LLMResponse{
				Content:      &genai.Content{Role: genai.RoleModel, Parts: parts},
				TurnComplete: true,
				UsageMetadata: &genai.GenerateContentResponseUsageMetadata{
					PromptTokenCount:     int32(len(prompt) / 4),
					CandidatesTokenCount: 8,
				},
			}, nil)
			return
		}
		yield(&model.LLMResponse{
			Content:      genai.NewContentFromText(next.Reply, genai.RoleModel),
			TurnComplete: true,
			// Token counts are made up but non-zero, so accounting that
			// reads them can be tested without a network call.
			UsageMetadata: &genai.GenerateContentResponseUsageMetadata{
				PromptTokenCount:     int32(len(prompt) / 4),
				CandidatesTokenCount: int32(len(next.Reply) / 4),
			},
		}, nil)
	}
}

// Exhausted reports whether every scripted exchange was consumed.
func (f *Fake) Exhausted() bool {
	return f.next >= len(f.script)
}

// promptOf includes the system instruction, which is where ADK puts an
// agent's instruction: matching only on user content hides most of what the
// model is actually told. It includes tool results as JSON, so a script can
// match what a tool returned.
func promptOf(req *model.LLMRequest) string {
	var b strings.Builder
	if req.Config != nil && req.Config.SystemInstruction != nil {
		for _, part := range req.Config.SystemInstruction.Parts {
			b.WriteString(part.Text)
		}
	}
	for _, content := range req.Contents {
		for _, part := range content.Parts {
			b.WriteString(part.Text)
			if r := part.FunctionResponse; r != nil {
				body, _ := json.Marshal(r.Response)
				b.Write(body)
			}
		}
	}
	return b.String()
}

// excerpt keeps a failure readable when the prompt is a whole document.
func excerpt(prompt string) string {
	const max = 400
	if len(prompt) <= max {
		return prompt
	}
	return prompt[:max] + "..."
}
