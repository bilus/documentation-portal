package anthropicmodel

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"
)

// fakeAPI answers each request with the next response and keeps the request
// bodies.
func fakeAPI(t *testing.T, responses ...string) (*httptest.Server, *[]map[string]any) {
	t.Helper()
	var bodies []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("request body: %v", err)
		}
		bodies = append(bodies, body)
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, responses[len(bodies)-1])
	}))
	t.Cleanup(srv.Close)
	return srv, &bodies
}

func generate(t *testing.T, m model.LLM, contents []*genai.Content) (*genai.Content, error) {
	t.Helper()
	for resp, err := range m.GenerateContent(t.Context(), &model.LLMRequest{Contents: contents}, false) {
		if err != nil {
			return nil, err
		}
		return resp.Content, nil
	}
	return nil, errors.New("no response")
}

func TestGenerateContentReturnsThinkingBlocks(t *testing.T) {
	srv, bodies := fakeAPI(t,
		`{"id":"msg_1","type":"message","role":"assistant","model":"claude-opus-5-5","content":[{"type":"thinking","thinking":"","signature":"sig-1"},{"type":"tool_use","id":"toolu_1","name":"list_documents","input":{}}],"stop_reason":"tool_use","usage":{"input_tokens":10,"output_tokens":5}}`,
		`{"id":"msg_2","type":"message","role":"assistant","model":"claude-opus-5-5","content":[{"type":"text","text":"Two guides."}],"stop_reason":"end_turn","usage":{"input_tokens":20,"output_tokens":3}}`,
	)
	m, err := New("claude-opus-5-5", Config{APIKey: "test", BaseURL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	question := genai.NewContentFromText("Which guides are there?", genai.RoleUser)
	first, err := generate(t, m, []*genai.Content{question})
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Parts) != 2 || !first.Parts[0].Thought || string(first.Parts[0].ThoughtSignature) != "sig-1" || first.Parts[1].FunctionCall == nil {
		t.Fatalf("first response: %+v", first.Parts)
	}
	result := &genai.Content{Role: genai.RoleUser, Parts: []*genai.Part{{FunctionResponse: &genai.FunctionResponse{
		ID: "toolu_1", Name: "list_documents", Response: map[string]any{"documents": []any{"a.md"}},
	}}}}
	if _, err := generate(t, m, []*genai.Content{question, first, result}); err != nil {
		t.Fatal(err)
	}

	sent := (*bodies)[1]["messages"].([]any)
	assistant := sent[1].(map[string]any)["content"].([]any)
	thinking := assistant[0].(map[string]any)
	if thinking["type"] != "thinking" || thinking["signature"] != "sig-1" || assistant[1].(map[string]any)["type"] != "tool_use" {
		t.Errorf("the second request lost the thinking block before the tool call: %v", assistant)
	}
	if got := sent[2].(map[string]any)["content"].([]any)[0].(map[string]any)["type"]; got != "tool_result" {
		t.Errorf("the tool's result went back as %v", got)
	}
	for i, body := range *bodies {
		if effort := body["output_config"].(map[string]any)["effort"]; effort != "medium" {
			t.Errorf("request %d: effort %v", i, effort)
		}
		if _, ok := body["cache_control"]; !ok {
			t.Errorf("request %d: no cache_control", i)
		}
	}
}

func TestGenerateContentReturnsRedactedThinking(t *testing.T) {
	srv, bodies := fakeAPI(t,
		`{"id":"msg_1","type":"message","role":"assistant","model":"claude-opus-5-5","content":[{"type":"redacted_thinking","data":"opaque-1"},{"type":"tool_use","id":"toolu_1","name":"list_documents","input":{}}],"stop_reason":"tool_use","usage":{"input_tokens":10,"output_tokens":5}}`,
		`{"id":"msg_2","type":"message","role":"assistant","model":"claude-opus-5-5","content":[{"type":"text","text":"Two guides."}],"stop_reason":"end_turn","usage":{"input_tokens":20,"output_tokens":3}}`,
	)
	m, err := New("claude-opus-5-5", Config{APIKey: "test", BaseURL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	question := genai.NewContentFromText("Which guides are there?", genai.RoleUser)
	first, err := generate(t, m, []*genai.Content{question})
	if err != nil {
		t.Fatal(err)
	}
	result := &genai.Content{Role: genai.RoleUser, Parts: []*genai.Part{{FunctionResponse: &genai.FunctionResponse{
		ID: "toolu_1", Name: "list_documents", Response: map[string]any{"documents": []any{"a.md"}},
	}}}}
	if _, err := generate(t, m, []*genai.Content{question, first, result}); err != nil {
		t.Fatal(err)
	}
	assistant := (*bodies)[1]["messages"].([]any)[1].(map[string]any)["content"].([]any)
	if len(assistant) != 2 || assistant[0].(map[string]any)["type"] != "redacted_thinking" || assistant[0].(map[string]any)["data"] != "opaque-1" {
		t.Errorf("the second request lost the redacted thinking before the tool call: %v", assistant)
	}
}

func TestGenerateContentSendsTheInstructionAndTools(t *testing.T) {
	srv, bodies := fakeAPI(t, `{"id":"msg_1","type":"message","role":"assistant","model":"claude-opus-5-5","content":[{"type":"text","text":"Hi."}],"stop_reason":"end_turn","usage":{"input_tokens":10,"output_tokens":2}}`)
	m, err := New("claude-opus-5-5", Config{APIKey: "test", BaseURL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	req := &model.LLMRequest{
		Contents: []*genai.Content{genai.NewContentFromText("Hi", genai.RoleUser)},
		Config: &genai.GenerateContentConfig{
			SystemInstruction: genai.NewContentFromText("Answer from the guides.", genai.RoleUser),
			Tools: []*genai.Tool{{FunctionDeclarations: []*genai.FunctionDeclaration{{
				Name:                 "search",
				Description:          "Finds a phrase.",
				ParametersJsonSchema: map[string]any{"type": "object", "properties": map[string]any{"query": map[string]any{"type": "string"}}, "required": []any{"query"}},
			}}}},
		},
	}
	for _, err := range m.GenerateContent(t.Context(), req, false) {
		if err != nil {
			t.Fatal(err)
		}
	}
	body, _ := json.Marshal((*bodies)[0])
	for _, want := range []string{
		`"system":[{"text":"Answer from the guides.","type":"text"}]`,
		`"input_schema":{"properties":{"query":{"type":"string"}},"required":["query"],"type":"object"}`,
	} {
		if !strings.Contains(string(body), want) {
			t.Errorf("the request has no %s: %s", want, body)
		}
	}
}

func TestGenerateContentReportsRefusal(t *testing.T) {
	srv, _ := fakeAPI(t, `{"id":"msg_1","type":"message","role":"assistant","model":"claude-opus-5-5","content":[],"stop_reason":"refusal","usage":{"input_tokens":10,"output_tokens":0}}`)
	m, err := New("claude-opus-5-5", Config{APIKey: "test", BaseURL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := generate(t, m, []*genai.Content{genai.NewContentFromText("?", genai.RoleUser)}); !errors.Is(err, ErrRefused) {
		t.Errorf("err = %v, want ErrRefused", err)
	}
}

func TestMessagesMergesConsecutiveRoles(t *testing.T) {
	got := messages([]*genai.Content{
		genai.NewContentFromText("one", genai.RoleUser),
		genai.NewContentFromText("two", genai.RoleUser),
		genai.NewContentFromText("three", genai.RoleModel),
	})
	if len(got) != 2 || len(got[0].Content) != 2 || len(got[1].Content) != 1 {
		t.Errorf("got %d messages: %+v", len(got), got)
	}
}
