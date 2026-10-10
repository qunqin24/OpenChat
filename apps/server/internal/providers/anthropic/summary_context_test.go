package anthropic

import (
	"encoding/json"
	"testing"

	"github.com/qunqin/openchat/apps/server/internal/llm"
)

// A compressed conversation begins with the summary as a user message and goes
// on with the user message that was kept: two user turns in a row, which this
// protocol only takes as one. The adapter merges them, in order, and what
// follows alternates as it did.
func TestBuildBodyMergesASummaryIntoTheUserMessageAfterIt(t *testing.T) {
	body := build(t, llm.Request{
		Model: "claude-sonnet-4-5",
		Messages: []llm.Message{
			llm.TextMessage(llm.RoleSystem, "be brief"),
			llm.TextMessage(llm.RoleUser, "<compressed_history_summary>\n- the user wants a report\n</compressed_history_summary>"),
			llm.TextMessage(llm.RoleUser, "[Date: 2026-10-10]\nfind the report"),
			{Role: llm.RoleAssistant, Parts: []llm.Part{
				llm.ToolCallPart{ID: "toolu_1", Name: "search", Arguments: json.RawMessage(`{"q":"report"}`), RawArguments: `{"q":"report"}`},
			}},
			{Role: llm.RoleTool, Parts: []llm.Part{
				llm.ToolResultPart{CallID: "toolu_1", Name: "search", Content: "three notes", Status: llm.ToolSucceeded},
			}},
		},
	}, Config{})

	if len(body.Messages) != 3 {
		t.Fatalf("messages = %d, want user, assistant, user: %v", len(body.Messages), body.Messages)
	}
	first := body.Messages[0]
	if first["role"] != "user" {
		t.Fatalf("the request must begin with a user turn: %v", first)
	}
	blocks := first["content"].([]any)
	if len(blocks) != 2 {
		t.Fatalf("first turn = %v, want the summary and the kept message in one turn", blocks)
	}
	if text := blocks[0].(map[string]any)["text"].(string); text[:len("<compressed_history_summary>")] != "<compressed_history_summary>" {
		t.Errorf("the summary should come first: %q", text)
	}
	if text := blocks[1].(map[string]any)["text"].(string); text != "[Date: 2026-10-10]\nfind the report" {
		t.Errorf("the kept message: %q", text)
	}
}
