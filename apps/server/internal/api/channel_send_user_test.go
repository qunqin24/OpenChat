package api

import "testing"

// A new channel sends the end user's identifier by default only to the
// vendors' own endpoints.
func TestDefaultSendUserID(t *testing.T) {
	tests := []struct {
		name    string
		kind    ChannelType
		baseURL string
		want    bool
	}{
		{"openai, its default endpoint", "openai", "", true},
		{"anthropic, its default endpoint", "anthropic", "", true},
		{"openai with an explicit base URL on its host", "openai", "https://api.openai.com/v1", true},
		{"a relay pointed at OpenAI's host", "openai-compatible", "https://API.OpenAI.com/v1/", true},
		{"anthropic's host with a port", "anthropic", "https://api.anthropic.com:443/v1", true},
		{"openai type through a relay", "openai", "https://relay.example.com/v1", false},
		{"a look-alike host", "openai-compatible", "https://api.openai.com.evil.example/v1", false},
		{"the host only in the path", "openai-compatible", "https://relay.example.com/api.openai.com", false},
		{"deepseek", "deepseek", "", false},
		{"gemini", "gemini", "", false},
		{"a relay with no URL yet", "openai-compatible", "", false},
		{"an unparsable URL", "openai", "://", false},
	}
	for _, tt := range tests {
		if got := defaultSendUserID(tt.kind, tt.baseURL); got != tt.want {
			t.Errorf("%s: %v, want %v", tt.name, got, tt.want)
		}
	}
}
