// Package anthropicmodel adapts the Anthropic Messages API to ADK's
// model.LLM: text, tool calls and thinking blocks, without images or
// streaming. It is a copy of humanizer/anthropicmodel in the Scratchpad
// repository, which docportal cannot import, changed to return thinking
// blocks unchanged, to merge consecutive messages of one role, and to set the
// effort and prompt caching.
package anthropicmodel

import (
	"context"
	"encoding/json"
	"fmt"
	"iter"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"
)

// Config configures the model.
type Config struct {
	// APIKey overrides ANTHROPIC_API_KEY.
	APIKey string
	// BaseURL overrides the API's URL.
	BaseURL string
	// MaxTokens per response; 0 means 16000.
	MaxTokens int
	// Effort is low, medium, high, xhigh or max; empty means medium.
	Effort string
}

type llm struct {
	client    anthropic.Client
	modelID   string
	maxTokens int64
	effort    anthropic.OutputConfigEffort
}

// New returns a model.LLM backed by the Anthropic Messages API. Thinking is
// left at the model's default, which is adaptive on current models.
func New(modelID string, cfg Config) (model.LLM, error) {
	if modelID == "" {
		return nil, fmt.Errorf("no model id")
	}
	var opts []option.RequestOption
	if cfg.APIKey != "" {
		opts = append(opts, option.WithAPIKey(cfg.APIKey))
	}
	if cfg.BaseURL != "" {
		opts = append(opts, option.WithBaseURL(cfg.BaseURL))
	}
	l := &llm{client: anthropic.NewClient(opts...), modelID: modelID, maxTokens: 16000, effort: anthropic.OutputConfigEffortMedium}
	if cfg.MaxTokens > 0 {
		l.maxTokens = int64(cfg.MaxTokens)
	}
	if cfg.Effort != "" {
		l.effort = anthropic.OutputConfigEffort(cfg.Effort)
	}
	return l, nil
}

func (l *llm) Name() string { return l.modelID }

func (l *llm) GenerateContent(ctx context.Context, req *model.LLMRequest, stream bool) iter.Seq2[*model.LLMResponse, error] {
	return func(yield func(*model.LLMResponse, error) bool) {
		params := anthropic.MessageNewParams{
			Model:        anthropic.Model(l.modelID),
			MaxTokens:    l.maxTokens,
			Messages:     messages(req.Contents),
			OutputConfig: anthropic.OutputConfigParam{Effort: l.effort},
			// Each turn resends the whole conversation, so its prefix caches.
			CacheControl: anthropic.NewCacheControlEphemeralParam(),
		}
		if req.Config != nil {
			params.Tools = tools(req.Config.Tools)
		}
		if req.Config != nil && req.Config.SystemInstruction != nil {
			params.System = []anthropic.TextBlockParam{{Text: text(req.Config.SystemInstruction)}}
		}
		resp, err := l.client.Messages.New(ctx, params)
		if err != nil {
			yield(nil, fmt.Errorf("anthropic: %w", err))
			return
		}
		if resp.StopReason == anthropic.StopReasonRefusal {
			yield(nil, ErrRefused)
			return
		}
		var parts []*genai.Part
		for _, block := range resp.Content {
			switch b := block.AsAny().(type) {
			case anthropic.ThinkingBlock:
				// The API wants each thinking block back as it came.
				parts = append(parts, &genai.Part{Thought: true, Text: b.Thinking, ThoughtSignature: []byte(b.Signature)})
			case anthropic.TextBlock:
				parts = append(parts, genai.NewPartFromText(b.Text))
			case anthropic.ToolUseBlock:
				var args map[string]any
				if err := json.Unmarshal([]byte(b.JSON.Input.Raw()), &args); err != nil {
					yield(nil, fmt.Errorf("tool call %s: decode input: %w", b.Name, err))
					return
				}
				parts = append(parts, &genai.Part{FunctionCall: &genai.FunctionCall{ID: b.ID, Name: b.Name, Args: args}})
			}
		}
		yield(&model.LLMResponse{
			Content:      &genai.Content{Role: genai.RoleModel, Parts: parts},
			ModelVersion: string(resp.Model),
			TurnComplete: true,
			UsageMetadata: &genai.GenerateContentResponseUsageMetadata{
				PromptTokenCount:     int32(resp.Usage.InputTokens + resp.Usage.CacheReadInputTokens + resp.Usage.CacheCreationInputTokens),
				CandidatesTokenCount: int32(resp.Usage.OutputTokens),
			},
		}, nil)
	}
}

// ErrRefused means the model declined to answer.
var ErrRefused = fmt.Errorf("anthropic: the model declined the request")

// messages translates ADK's contents into Anthropic messages, merging
// consecutive contents of one role into one message.
func messages(contents []*genai.Content) []anthropic.MessageParam {
	var out []anthropic.MessageParam
	for _, c := range contents {
		blocks := blocks(c)
		if len(blocks) == 0 {
			continue
		}
		role := anthropic.MessageParamRoleUser
		if c.Role == genai.RoleModel {
			role = anthropic.MessageParamRoleAssistant
		}
		if n := len(out); n > 0 && out[n-1].Role == role {
			out[n-1].Content = append(out[n-1].Content, blocks...)
			continue
		}
		out = append(out, anthropic.MessageParam{Role: role, Content: blocks})
	}
	return out
}

// blocks translates one genai content into Anthropic blocks. A function call
// becomes tool_use on the assistant side; a function response becomes
// tool_result, which Anthropic requires on the user side.
func blocks(c *genai.Content) []anthropic.ContentBlockParamUnion {
	var out []anthropic.ContentBlockParamUnion
	for _, p := range c.Parts {
		switch {
		case p.Thought:
			out = append(out, anthropic.NewThinkingBlock(string(p.ThoughtSignature), p.Text))
		case p.FunctionCall != nil:
			out = append(out, anthropic.NewToolUseBlock(p.FunctionCall.ID, p.FunctionCall.Args, p.FunctionCall.Name))
		case p.FunctionResponse != nil:
			body, err := json.Marshal(p.FunctionResponse.Response)
			if err != nil {
				body = []byte(fmt.Sprintf("%v", p.FunctionResponse.Response))
			}
			out = append(out, anthropic.NewToolResultBlock(p.FunctionResponse.ID, string(body), false))
		case p.Text != "":
			out = append(out, anthropic.NewTextBlock(p.Text))
		}
	}
	return out
}

// tools translates ADK's declarations. ADK's functiontool describes its
// arguments in ParametersJsonSchema, not the typed Parameters field; reading
// only the typed one sends Anthropic a tool with no arguments, and the model
// then guesses argument names.
func tools(declared []*genai.Tool) []anthropic.ToolUnionParam {
	var out []anthropic.ToolUnionParam
	for _, t := range declared {
		for _, f := range t.FunctionDeclarations {
			schema, err := inputSchema(f)
			if err != nil {
				continue
			}
			out = append(out, anthropic.ToolUnionParam{OfTool: &anthropic.ToolParam{
				Name:        f.Name,
				Description: anthropic.String(f.Description),
				InputSchema: schema,
			}})
		}
	}
	return out
}

func inputSchema(f *genai.FunctionDeclaration) (anthropic.ToolInputSchemaParam, error) {
	schema := anthropic.ToolInputSchemaParam{Properties: map[string]any{}}
	var raw []byte
	var err error
	switch {
	case f.ParametersJsonSchema != nil:
		raw, err = json.Marshal(f.ParametersJsonSchema)
	case f.Parameters != nil:
		raw, err = json.Marshal(f.Parameters)
	default:
		return schema, nil
	}
	if err != nil {
		return schema, err
	}
	var decoded struct {
		Properties map[string]any `json:"properties"`
		Required   []string       `json:"required"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return schema, err
	}
	if decoded.Properties != nil {
		schema.Properties = decoded.Properties
	}
	schema.Required = decoded.Required
	return schema, nil
}

func text(c *genai.Content) string {
	var b strings.Builder
	for _, p := range c.Parts {
		b.WriteString(p.Text)
	}
	return b.String()
}
