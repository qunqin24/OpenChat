package llm_test

import (
	"encoding/json"
	"testing"

	"github.com/qunqin/openchat/apps/server/internal/llm"
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
