package agent

import (
	"encoding/json"
	"testing"

	"github.com/qunqin24/openchat/apps/server/internal/llm"
)

// A tool result with a NUL in it (text read out of a PDF) must still make a
// row Postgres takes, and an escaped backslash before "u0000" is not a NUL.
func TestStorableParts(t *testing.T) {
	parts := []llm.Part{llm.ToolResultPart{CallID: "c", Name: "web_search", Content: "a\x00b \\u0000 c", Status: llm.ToolSucceeded}}
	data := EncodeParts(parts)
	var back llm.Parts
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatal(err)
	}
	if got := back[0].(llm.ToolResultPart).Content; got != "ab \\u0000 c" {
		t.Fatalf("content %q", got)
	}
	if got := StorableText("x\x00y\xffz"); got != "xy�z" {
		t.Fatalf("text %q", got)
	}
}
