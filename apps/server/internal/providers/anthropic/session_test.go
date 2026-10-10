package anthropic

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/qunqin/openchat/apps/server/internal/llm"
)

func TestGatewaySessionIsSent(t *testing.T) {
	var session string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		session = r.Header.Get("X-Opencode-Session")
		http.Error(w, `{"type":"error","error":{"type":"invalid_request_error","message":"stop here"}}`, http.StatusBadRequest)
	}))
	defer srv.Close()
	client, err := New(Config{Provider: "opencode-go", BaseURL: srv.URL, APIKey: "k", HTTPClient: srv.Client(), SessionHeader: "x-opencode-session"})
	if err != nil {
		t.Fatal(err)
	}
	max := 100
	_, _ = client.Stream(context.Background(), llm.Request{Model: "claude-haiku-5-5", Session: "conv-123",
		Messages: []llm.Message{llm.TextMessage(llm.RoleUser, "hi")}, Options: llm.Options{MaxTokens: &max}}, func(llm.Event) error { return nil })
	if session != "conv-123" {
		t.Errorf("session header = %q", session)
	}
}
