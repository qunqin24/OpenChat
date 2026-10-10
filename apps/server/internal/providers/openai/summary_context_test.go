package openai

import (
	"testing"

	"github.com/qunqin/openchat/apps/server/internal/llm"
)

// A compressed conversation begins with the summary as a user message and goes
// on with the user message that was kept. Both OpenAI APIs take two user
// messages in a row as they are, in order; nothing is merged or dropped.
func TestASummaryInFrontOfAUserMessageIsSentAsItIs(t *testing.T) {
	history := []llm.Message{
		llm.TextMessage(llm.RoleSystem, "be brief"),
		llm.TextMessage(llm.RoleUser, "<compressed_history_summary>\n- the user wants a report\n</compressed_history_summary>"),
		llm.TextMessage(llm.RoleUser, "find the report"),
	}

	t.Run("chat completions", func(t *testing.T) {
		got := buildMessages(t, Config{}, history...)
		if len(got) != 3 || got[1]["role"] != "user" || got[2]["role"] != "user" {
			t.Fatalf("messages = %v, want system, user, user", got)
		}
		if got[1]["content"] != "<compressed_history_summary>\n- the user wants a report\n</compressed_history_summary>" || got[2]["content"] != "find the report" {
			t.Errorf("the two user messages: %v / %v", got[1], got[2])
		}
	})

	t.Run("responses", func(t *testing.T) {
		sent := buildResponses(t, llm.Request{Model: "gpt-5", Messages: history})
		jsonEq(t, "input", sent["input"], `[
			{"type":"message","role":"user","content":[{"type":"input_text","text":"<compressed_history_summary>\n- the user wants a report\n</compressed_history_summary>"}]},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"find the report"}]}]`)
	})
}
