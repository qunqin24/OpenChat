package openai

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/qunqin/openchat/apps/server/internal/llm"
)

// A gateway that routes by conversation gets the request's session in the
// header it names, on both request formats, and the client's own agent.
func TestGatewaySessionAndAgentAreSent(t *testing.T) {
	for _, format := range []string{"chat", "responses"} {
		var session, agent string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			session, agent = r.Header.Get("X-Opencode-Session"), r.Header.Get("User-Agent")
			http.Error(w, `{"error":{"message":"stop here"}}`, http.StatusBadRequest)
		}))
		cfg := Config{Provider: "opencode-go", BaseURL: srv.URL, APIKey: "k", HTTPClient: srv.Client(),
			Headers: map[string]string{"User-Agent": "OpenChat/1.0"}, SessionHeader: "x-opencode-session"}
		var client llm.ModelClient
		if format == "chat" {
			client, _ = New(cfg)
		} else {
			client, _ = NewResponses(cfg)
		}
		_, _ = client.Stream(context.Background(), llm.Request{Model: "m", Session: "conv-123",
			Messages: []llm.Message{llm.TextMessage(llm.RoleUser, "hi")}}, func(llm.Event) error { return nil })
		srv.Close()
		if session != "conv-123" || agent != "OpenChat/1.0" {
			t.Errorf("%s: session %q, agent %q", format, session, agent)
		}
	}
}
