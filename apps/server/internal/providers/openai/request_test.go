package openai

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/qunqin/openchat/apps/server/internal/llm"
)

func buildMessages(t *testing.T, cfg Config, messages ...llm.Message) []map[string]any {
	t.Helper()

	body, err := buildBody(llm.Request{Model: "m", Messages: messages}, cfg)
	if err != nil {
		t.Fatalf("buildBody: %v", err)
	}
	var sent struct {
		Messages []map[string]any `json:"messages"`
	}
	if err := json.Unmarshal(body, &sent); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	return sent.Messages
}

func TestBuildBodyConvertsHistory(t *testing.T) {
	tests := []struct {
		name  string
		input llm.Message
		want  map[string]any
	}{
		{
			name:  "a user turn is plain string content",
			input: llm.TextMessage(llm.RoleUser, "what is an aperture?"),
			want:  map[string]any{"role": "user", "content": "what is an aperture?"},
		},
		{
			name:  "a system turn keeps its role",
			input: llm.TextMessage(llm.RoleSystem, "be brief"),
			want:  map[string]any{"role": "system", "content": "be brief"},
		},
		{
			name: "an assistant turn carries its tool calls",
			input: llm.Message{Role: llm.RoleAssistant, Parts: []llm.Part{
				llm.TextPart{Text: "looking"},
				llm.ToolCallPart{ID: "call_1", Name: "search", Arguments: json.RawMessage(`{"q":"x"}`), RawArguments: `{"q":"x"}`},
			}},
			want: map[string]any{
				"role":    "assistant",
				"content": "looking",
				"tool_calls": []any{map[string]any{
					"id":       "call_1",
					"type":     "function",
					"function": map[string]any{"name": "search", "arguments": `{"q":"x"}`},
				}},
			},
		},
		{
			name: "a tool result is matched by call id",
			input: llm.Message{Role: llm.RoleTool, Parts: []llm.Part{
				llm.ToolResultPart{CallID: "call_1", Name: "search", Content: "three notes", Status: llm.ToolSucceeded},
			}},
			want: map[string]any{"role": "tool", "tool_call_id": "call_1", "content": "three notes"},
		},
		{
			name: "a failed tool says so instead of returning nothing",
			input: llm.Message{Role: llm.RoleTool, Parts: []llm.Part{
				llm.ToolResultPart{CallID: "call_2", Name: "search", Status: llm.ToolFailed, Error: "note not found"},
			}},
			want: map[string]any{"role": "tool", "tool_call_id": "call_2", "content": "tool failed: note not found"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := buildMessages(t, Config{}, tc.input)
			if len(got) != 1 {
				t.Fatalf("messages = %v, want one", got)
			}
			wantJSON, _ := json.Marshal(tc.want)
			gotJSON, _ := json.Marshal(got[0])
			if string(gotJSON) != string(wantJSON) {
				t.Errorf("message = %s, want %s", gotJSON, wantJSON)
			}
		})
	}
}

func TestBuildBodySplitsToolResultsIntoOneMessageEach(t *testing.T) {
	// The protocol matches results to calls by tool_call_id, so two results in
	// one message would lose which answer belongs to which question.
	message := llm.Message{Role: llm.RoleTool, Parts: []llm.Part{
		llm.ToolResultPart{CallID: "call_1", Content: "a", Status: llm.ToolSucceeded},
		llm.ToolResultPart{CallID: "call_2", Content: "b", Status: llm.ToolSucceeded},
	}}

	got := buildMessages(t, Config{}, message)
	if len(got) != 2 {
		t.Fatalf("messages = %d, want 2", len(got))
	}
	if got[0]["tool_call_id"] != "call_1" || got[1]["tool_call_id"] != "call_2" {
		t.Errorf("call ids = %v / %v", got[0]["tool_call_id"], got[1]["tool_call_id"])
	}
}

func TestBuildBodyDropsReasoningFromHistoryUnlessTheEndpointWantsIt(t *testing.T) {
	message := llm.Message{Role: llm.RoleAssistant, Parts: []llm.Part{
		llm.ReasoningPart{Text: "thinking out loud"},
		llm.TextPart{Text: "the answer"},
	}}

	t.Run("dropped by default", func(t *testing.T) {
		got := buildMessages(t, Config{}, message)
		if content := got[0]["content"]; content != "the answer" {
			t.Errorf("content = %q, most endpoints on this protocol reject their own reasoning coming back", content)
		}
	})

	t.Run("echoed when compat says so", func(t *testing.T) {
		got := buildMessages(t, Config{Compat: Compat{EchoReasoning: true}}, message)
		if content := got[0]["content"]; content != "thinking out loudthe answer" {
			t.Errorf("content = %q", content)
		}
	})
}

func TestBuildBodyInlinesAResolvedImageAttachment(t *testing.T) {
	message := llm.Message{Role: llm.RoleUser, Parts: []llm.Part{
		llm.TextPart{Text: "what is this?"},
		llm.AttachmentPart{Ref: "att-1", Filename: "a.png", MediaType: "image/png", Data: []byte{0x89, 0x50}},
	}}

	got := buildMessages(t, Config{}, message)
	blocks, ok := got[0]["content"].([]any)
	if !ok || len(blocks) != 2 {
		t.Fatalf("content = %v, want a text block and an image block", got[0]["content"])
	}
	image := blocks[1].(map[string]any)["image_url"].(map[string]any)["url"].(string)
	if !strings.HasPrefix(image, "data:image/png;base64,") {
		t.Errorf("image url = %q", image)
	}
}

func TestBuildBodyInlinesAResolvedPDFAttachment(t *testing.T) {
	message := llm.Message{Role: llm.RoleUser, Parts: []llm.Part{
		llm.TextPart{Text: "summarise"},
		llm.AttachmentPart{Ref: "att-1", Filename: "report.pdf", MediaType: "application/pdf", Data: []byte("%PDF-1.4")},
	}}

	got := buildMessages(t, Config{}, message)
	blocks, ok := got[0]["content"].([]any)
	if !ok || len(blocks) != 2 {
		t.Fatalf("content = %v, want a text block and a file block", got[0]["content"])
	}
	block := blocks[1].(map[string]any)
	file := block["file"].(map[string]any)
	if block["type"] != "file" || file["filename"] != "report.pdf" || !strings.HasPrefix(file["file_data"].(string), "data:application/pdf;base64,") {
		t.Errorf("file block = %v", block)
	}
}

func TestBuildBodyRefusesWhatItCannotFaithfullySend(t *testing.T) {
	tests := []struct {
		name string
		req  llm.Request
	}{
		{
			name: "an attachment the context builder never resolved",
			req: llm.Request{Model: "m", Messages: []llm.Message{{Role: llm.RoleUser, Parts: []llm.Part{
				llm.AttachmentPart{Ref: "att-1", MediaType: "image/png"},
			}}}},
		},
		{
			name: "an attachment type this protocol has no block for",
			req: llm.Request{Model: "m", Messages: []llm.Message{{Role: llm.RoleUser, Parts: []llm.Part{
				llm.AttachmentPart{Ref: "att-2", MediaType: "audio/mpeg", Data: []byte("ID3")},
			}}}},
		},
		{
			name: "a tool call with no id for a result to match",
			req: llm.Request{Model: "m", Messages: []llm.Message{{Role: llm.RoleAssistant, Parts: []llm.Part{
				llm.ToolCallPart{Name: "search", RawArguments: "{}"},
			}}}},
		},
		{
			name: "a tool result with no call id",
			req: llm.Request{Model: "m", Messages: []llm.Message{{Role: llm.RoleTool, Parts: []llm.Part{
				llm.ToolResultPart{Name: "search", Content: "x", Status: llm.ToolSucceeded},
			}}}},
		},
		{
			name: "no model at all",
			req:  llm.Request{Messages: []llm.Message{llm.TextMessage(llm.RoleUser, "hi")}},
		},
		{
			name: "an extra option that would retarget the request",
			req: llm.Request{
				Model:    "m",
				Messages: []llm.Message{llm.TextMessage(llm.RoleUser, "hi")},
				Options:  llm.Options{Extra: map[string]any{"model": "something-else"}},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := buildBody(tc.req, Config{}); err == nil {
				t.Fatal("the request was accepted")
			}
		})
	}
}

func TestBuildBodyPassesVendorKnobsThrough(t *testing.T) {
	temperature := 0.0
	body, err := buildBody(llm.Request{
		Model:    "deepseek-reasoner",
		Messages: []llm.Message{llm.TextMessage(llm.RoleUser, "hi")},
		Tools:    []llm.Tool{{Name: "search", Description: "find notes"}},
		Options: llm.Options{
			Temperature: &temperature,
			ToolChoice:  llm.ToolChoiceRequired,
			Reasoning:   &llm.ReasoningOptions{Effort: llm.EffortHigh},
			Extra:       map[string]any{"top_k": 40},
		},
	}, Config{})
	if err != nil {
		t.Fatalf("buildBody: %v", err)
	}

	var sent map[string]any
	if err := json.Unmarshal(body, &sent); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	// A deliberate zero has to survive, or "be deterministic" silently becomes
	// "use whatever the provider defaults to".
	if sent["temperature"] != float64(0) {
		t.Errorf("temperature = %v, want an explicit 0", sent["temperature"])
	}
	if sent["tool_choice"] != "required" {
		t.Errorf("tool_choice = %v", sent["tool_choice"])
	}
	if sent["reasoning_effort"] != "high" {
		t.Errorf("reasoning_effort = %v", sent["reasoning_effort"])
	}
	if sent["top_k"] != float64(40) {
		t.Errorf("top_k = %v", sent["top_k"])
	}

	tools := sent["tools"].([]any)
	function := tools[0].(map[string]any)["function"].(map[string]any)
	if function["parameters"] == nil {
		t.Error("a tool with no parameters still needs a schema, the field is required")
	}
}
