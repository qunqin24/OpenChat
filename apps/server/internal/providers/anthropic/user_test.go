package anthropic

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/qunqin/openchat/apps/server/internal/llm"
)

func wireBody(t *testing.T, cfg Config, user string) map[string]any {
	t.Helper()
	var raw []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ = io.ReadAll(r.Body)
		http.Error(w, `{"type":"error","error":{"type":"invalid_request_error","message":"stop here"}}`, http.StatusBadRequest)
	}))
	defer srv.Close()
	cfg.BaseURL, cfg.APIKey, cfg.HTTPClient = srv.URL, "k", srv.Client()
	client, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	max := 100
	_, _ = client.Stream(context.Background(), llm.Request{Model: "claude-haiku-5-5", User: user,
		Messages: []llm.Message{llm.TextMessage(llm.RoleUser, "hi")}, Options: llm.Options{MaxTokens: &max}},
		func(llm.Event) error { return nil })
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("the request body is not JSON: %v (%q)", err, raw)
	}
	return body
}

// The end-user identifier goes out as metadata.user_id, but only for a channel
// that takes it and a request that has one.
func TestMetadataUserIDFollowsTheChannelSwitch(t *testing.T) {
	on := wireBody(t, Config{Provider: "anthropic", SendUser: true}, "abc123")
	metadata, _ := on["metadata"].(map[string]any)
	if metadata["user_id"] != "abc123" {
		t.Errorf("switch on: metadata = %v", on["metadata"])
	}
	if _, ok := wireBody(t, Config{Provider: "relay"}, "abc123")["metadata"]; ok {
		t.Error("switch off: metadata was sent")
	}
	if _, ok := wireBody(t, Config{Provider: "anthropic", SendUser: true}, "")["metadata"]; ok {
		t.Error("no identifier: metadata was sent")
	}
}

func TestExtraCannotOverwriteMetadata(t *testing.T) {
	max := 10
	req := llm.Request{Model: "claude-haiku-5-5", Messages: []llm.Message{llm.TextMessage(llm.RoleUser, "hi")},
		Options: llm.Options{MaxTokens: &max, Extra: map[string]any{"metadata": map[string]any{"user_id": "x"}}}}
	if _, err := buildBody(req, Config{BaseURL: "http://x"}); err == nil {
		t.Error("Extra overwrote metadata")
	}
}
