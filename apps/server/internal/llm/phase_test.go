package llm_test

import (
	"encoding/json"
	"testing"

	"github.com/qunqin/openchat/apps/server/internal/llm"
)

func TestTextPhaseSurvivesTheStoredEncoding(t *testing.T) {
	// OpenAI wants the phase resent on every assistant message of the newer
	// models; it rides the transcript's jsonb or it is lost after one turn.
	in := llm.Parts{
		llm.TextPart{Text: "checking", Phase: "commentary"},
		llm.TextPart{Text: "plain"},
	}
	data, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var out llm.Parts
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	if got := out[0].(llm.TextPart).Phase; got != "commentary" {
		t.Errorf("phase = %q", got)
	}
	if got := out[1].(llm.TextPart).Phase; got != "" {
		t.Errorf("a part without one gained %q", got)
	}
}
