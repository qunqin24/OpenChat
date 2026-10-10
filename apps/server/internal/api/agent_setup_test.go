package api

import (
	"strings"
	"testing"
)

// The system prompt is the front of every cached prefix and of every replayed
// thinking block, so nothing in it may depend on when the request is made.
func TestBasePromptHasNoDate(t *testing.T) {
	prompt := basePrompt(nil)
	if strings.Contains(prompt, "Today's date is") {
		t.Errorf("prompt states a date: %q", prompt)
	}
	if !strings.Contains(prompt, "[Date: ") {
		t.Errorf("prompt does not explain the date lines on user messages: %q", prompt)
	}
	if prompt != basePrompt(nil) {
		t.Error("prompt is not deterministic")
	}
}
