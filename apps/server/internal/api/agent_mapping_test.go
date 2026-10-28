package api

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/qunqin24/openchat/apps/server/internal/llm"
)

func TestToolCallSignatureNeverReachesTheBrowser(t *testing.T) {
	// A thought signature is the provider's, replayed by the server on the next
	// request. The contract has no field for it, and the browser has no use for
	// an opaque blob that is also an encrypted record of the model's reasoning.
	part, ok := toMessagePart(llm.ToolCallPart{
		ID: "c1", Name: "read", Arguments: json.RawMessage(`{}`), RawArguments: `{}`,
		Signature: llm.SignatureGemini + "CvUBsecret=",
	})
	if !ok {
		t.Fatal("a tool call was hidden")
	}
	data, err := json.Marshal(part)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "CvUBsecret") || strings.Contains(strings.ToLower(string(data)), "signature") {
		t.Errorf("wire form leaks the signature: %s", data)
	}
}

func TestTextAndReasoningSignaturesNeverReachTheBrowser(t *testing.T) {
	// Gemini's signature on answer text and OpenRouter's reasoning_details on a
	// reasoning block are replayed by the server only. Both are opaque blobs
	// that encode the model's own reasoning state.
	for _, part := range []llm.Part{
		llm.TextPart{Text: "answer", Signature: llm.SignatureGemini + "CxsecretText="},
		llm.ReasoningPart{
			Text: "thinking", Signature: llm.SignatureOpenRouter + "anthropic/claude-sonnet-4.5",
			Redacted: []byte(`[{"type":"reasoning.encrypted","data":"CxsecretBlob="}]`),
		},
	} {
		mapped, ok := toMessagePart(part)
		if !ok {
			t.Fatalf("%T was hidden", part)
		}
		data, err := json.Marshal(mapped)
		if err != nil {
			t.Fatal(err)
		}
		lowered := strings.ToLower(string(data))
		if strings.Contains(string(data), "Cxsecret") || strings.Contains(lowered, "signature") || strings.Contains(lowered, "redacted") {
			t.Errorf("%T wire form leaks the signature: %s", part, data)
		}
	}
}
