package api

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/qunqin/openchat/apps/server/internal/llm"
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
