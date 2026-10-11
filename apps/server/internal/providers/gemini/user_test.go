package gemini

import (
	"strings"
	"testing"

	"github.com/qunqin/openchat/apps/server/internal/llm"
)

// Gemini has no field for an end user, so the identifier never reaches the wire.
func TestTheEndUserIdentifierIsNotSent(t *testing.T) {
	body, err := buildBody(llm.Request{Model: "gemini-x", User: "abc123",
		Messages: []llm.Message{llm.TextMessage(llm.RoleUser, "hi")}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "abc123") {
		t.Errorf("the identifier is in the body: %s", body)
	}
}
