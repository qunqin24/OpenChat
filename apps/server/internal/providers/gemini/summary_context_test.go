package gemini

import (
	"testing"

	"github.com/qunqin24/openchat/apps/server/internal/llm"
)

// A compressed conversation begins with the summary as a user message and goes
// on with the user message that was kept. This protocol wants turns to
// alternate, so the two are one turn with two parts, in order.
func TestBuildBodyMergesASummaryIntoTheUserMessageAfterIt(t *testing.T) {
	body := build(t, llm.Request{
		Model: "gemini-2.5-flash",
		Messages: []llm.Message{
			llm.TextMessage(llm.RoleSystem, "be brief"),
			llm.TextMessage(llm.RoleUser, "<compressed_history_summary>\n- the user wants a report\n</compressed_history_summary>"),
			llm.TextMessage(llm.RoleUser, "find the report"),
		},
	})

	got := contents(t, body)
	if len(got) != 1 || got[0]["role"] != "user" {
		t.Fatalf("contents = %v, want one user turn", got)
	}
	parts := got[0]["parts"].([]any)
	if len(parts) != 2 {
		t.Fatalf("parts = %v, want the summary and the kept message", parts)
	}
	if text := parts[0].(map[string]any)["text"].(string); text[:len("<compressed_history_summary>")] != "<compressed_history_summary>" {
		t.Errorf("the summary should come first: %q", text)
	}
	if parts[1].(map[string]any)["text"] != "find the report" {
		t.Errorf("the kept message: %v", parts[1])
	}
}
