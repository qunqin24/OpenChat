package llm_test

import (
	"encoding/json"
	"testing"

	"github.com/qunqin24/openchat/apps/server/internal/llm"
)

func TestToolCallSignatureSurvivesTheStoredEncoding(t *testing.T) {
	// The signature rides the same jsonb the transcript is stored in; losing it
	// there is losing it for every later request of the run.
	in := llm.Parts{
		llm.ToolCallPart{
			ID: "c1", Name: "read", Arguments: json.RawMessage(`{"id":"n-1"}`), RawArguments: `{"id":"n-1"}`,
			Signature: llm.SignatureGemini + "CvUBsig=",
		},
		llm.ToolCallPart{ID: "c2", Name: "read", RawArguments: "{}"},
	}
	data, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var out llm.Parts
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	first, second := out[0].(llm.ToolCallPart), out[1].(llm.ToolCallPart)
	if first.Signature != llm.SignatureGemini+"CvUBsig=" {
		t.Errorf("signature = %q", first.Signature)
	}
	if second.Signature != "" {
		t.Errorf("a call without one gained %q", second.Signature)
	}
}

func TestTextAndReasoningSignaturesSurviveTheStoredEncoding(t *testing.T) {
	// A Gemini signature on answer text, and OpenRouter's reasoning_details on a
	// reasoning block, are both replayed on a later request; the JSON array
	// rides in Redacted and must come back byte for byte.
	details := []byte(`[{"format":"anthropic-claude-v1","index":0,"signature":"EqQB","text":"hm","type":"reasoning.text"}]`)
	in := llm.Parts{
		llm.TextPart{Text: "answer", Phase: "final_answer", Signature: llm.SignatureGemini + "Cx0B"},
		llm.TextPart{Text: "plain"},
		llm.ReasoningPart{Text: "hm", Signature: llm.SignatureOpenRouter + "anthropic/claude-sonnet-4.5", Redacted: details},
	}
	data, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var out llm.Parts
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	if got := out[0].(llm.TextPart); got != in[0].(llm.TextPart) {
		t.Errorf("text part = %+v, want %+v", got, in[0])
	}
	if got := out[1].(llm.TextPart); got.Signature != "" {
		t.Errorf("a text part without one gained %q", got.Signature)
	}
	reasoning := out[2].(llm.ReasoningPart)
	if reasoning.Signature != llm.SignatureOpenRouter+"anthropic/claude-sonnet-4.5" || string(reasoning.Redacted) != string(details) {
		t.Errorf("reasoning part = %+v", reasoning)
	}
}
