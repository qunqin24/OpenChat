package openai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/qunqin24/openchat/apps/server/internal/llm"
)

// sentBody is the request body a client puts on the wire.
func sentBody(t *testing.T, newClient func(Config) (llm.ModelClient, error), cfg Config, req llm.Request) map[string]any {
	t.Helper()
	var raw []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ = io.ReadAll(r.Body)
		http.Error(w, `{"error":{"message":"stop here"}}`, http.StatusBadRequest)
	}))
	defer srv.Close()
	cfg.BaseURL, cfg.APIKey, cfg.HTTPClient = srv.URL, "k", srv.Client()
	client, err := newClient(cfg)
	if err != nil {
		t.Fatal(err)
	}
	req.Messages = []llm.Message{llm.TextMessage(llm.RoleUser, "hi")}
	_, _ = client.Stream(context.Background(), req, func(llm.Event) error { return nil })
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("the request body is not JSON: %v (%q)", err, raw)
	}
	return body
}

// The end-user identifier goes out as safety_identifier on both request
// formats, but only for a channel that takes it and a request that has one.
func TestSafetyIdentifierFollowsTheChannelSwitch(t *testing.T) {
	formats := map[string]func(Config) (llm.ModelClient, error){
		"chat":      func(c Config) (llm.ModelClient, error) { return New(c) },
		"responses": func(c Config) (llm.ModelClient, error) { return NewResponses(c) },
	}
	for name, newClient := range formats {
		on := sentBody(t, newClient, Config{Provider: "openai", SendUser: true}, llm.Request{Model: "m", User: "abc123"})
		if on["safety_identifier"] != "abc123" {
			t.Errorf("%s: switch on: safety_identifier = %v", name, on["safety_identifier"])
		}
		off := sentBody(t, newClient, Config{Provider: "relay"}, llm.Request{Model: "m", User: "abc123"})
		if _, ok := off["safety_identifier"]; ok {
			t.Errorf("%s: switch off: the field was sent: %v", name, off)
		}
		none := sentBody(t, newClient, Config{Provider: "openai", SendUser: true}, llm.Request{Model: "m"})
		if _, ok := none["safety_identifier"]; ok {
			t.Errorf("%s: no identifier: the field was sent: %v", name, none)
		}
	}
}

// A vendor knob may not overwrite the identifier.
func TestExtraCannotOverwriteTheSafetyIdentifier(t *testing.T) {
	req := llm.Request{Model: "m", Messages: []llm.Message{llm.TextMessage(llm.RoleUser, "hi")},
		Options: llm.Options{Extra: map[string]any{"safety_identifier": "x"}}}
	if _, err := buildBody(req, Config{BaseURL: "http://x"}); err == nil {
		t.Error("chat: Extra overwrote safety_identifier")
	}
	if _, err := buildResponsesBody(req, Config{BaseURL: "http://x"}); err == nil {
		t.Error("responses: Extra overwrote safety_identifier")
	}
}
