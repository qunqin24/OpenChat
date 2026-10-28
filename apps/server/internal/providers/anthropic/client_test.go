package anthropic

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/qunqin24/openchat/apps/server/internal/llm"
	"github.com/qunqin24/openchat/apps/server/internal/llm/llmtest"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func respond(status int, contentType, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{contentType}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func TestClientPostsAStreamingRequestAndParsesTheReply(t *testing.T) {
	var captured *http.Request
	var body []byte

	client, err := New(Config{
		Provider: "anthropic",
		BaseURL:  "https://api.example.test/v1/",
		APIKey:   "sk-ant-test",
		Headers:  map[string]string{"Anthropic-Beta": "context-1m-2025-08-07"},
		HTTPClient: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			captured = r
			body, _ = io.ReadAll(r.Body)
			return respond(http.StatusOK, "text/event-stream", string(fixture(t, "text.sse"))), nil
		})},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	rec := &llmtest.Recorder{}
	resp, err := client.Stream(context.Background(), llm.Request{
		Model:    "claude-sonnet-4-5",
		Messages: []llm.Message{llm.TextMessage(llm.RoleUser, "hello")},
	}, rec.Emit)
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	rec.CheckProviderInvariants(t)

	if got, want := captured.URL.String(), "https://api.example.test/v1/messages"; got != want {
		t.Errorf("URL = %q, want %q", got, want)
	}
	// Not Authorization: this API answers 401 to a bearer token.
	if got := captured.Header.Get("X-Api-Key"); got != "sk-ant-test" {
		t.Errorf("x-api-key = %q", got)
	}
	if captured.Header.Get("Authorization") != "" {
		t.Error("a bearer token was sent, which this API rejects")
	}
	if got := captured.Header.Get("Anthropic-Version"); got != defaultVersion {
		t.Errorf("anthropic-version = %q, want %q", got, defaultVersion)
	}
	if got := captured.Header.Get("Content-Type"); got != "application/json" {
		t.Errorf("content-type = %q", got)
	}
	if got := captured.Header.Get("Anthropic-Beta"); got != "context-1m-2025-08-07" {
		t.Errorf("configured header missing, got %q", got)
	}

	var sent map[string]any
	if err := json.Unmarshal(body, &sent); err != nil {
		t.Fatalf("request body: %v", err)
	}
	if sent["stream"] != true {
		t.Errorf("stream = %v, want true", sent["stream"])
	}
	if sent["max_tokens"] == nil {
		t.Error("max_tokens is required by this protocol and was not sent")
	}
	if resp.Message.Text() != "The aperture is open." {
		t.Errorf("text = %q", resp.Message.Text())
	}
	if resp.Provider != "anthropic" {
		t.Errorf("provider = %q, want the configured identity", resp.Provider)
	}
}

func TestClientLetsAGatewayOverrideTheVersionHeader(t *testing.T) {
	var captured *http.Request
	client, err := New(Config{
		Provider: "bedrock-gateway",
		BaseURL:  "https://gateway.example.test",
		Version:  "2024-01-01",
		HTTPClient: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			captured = r
			return respond(http.StatusOK, "text/event-stream", string(fixture(t, "text.sse"))), nil
		})},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	rec := &llmtest.Recorder{}
	if _, err := client.Stream(context.Background(), llm.Request{
		Model:    "claude-sonnet-4-5",
		Messages: []llm.Message{llm.TextMessage(llm.RoleUser, "hello")},
	}, rec.Emit); err != nil {
		t.Fatalf("Stream: %v", err)
	}
	if got := captured.Header.Get("Anthropic-Version"); got != "2024-01-01" {
		t.Errorf("anthropic-version = %q", got)
	}
}

func TestClientMapsFailedStatusesOntoActionableKinds(t *testing.T) {
	tests := []struct {
		name      string
		status    int
		body      string
		want      llm.ErrorKind
		retryable bool
	}{
		{
			name:   "a bad key is the operator's problem",
			status: http.StatusUnauthorized,
			body:   `{"type":"error","error":{"type":"authentication_error","message":"invalid x-api-key"}}`,
			want:   llm.ErrorAuth,
		},
		{
			name:      "a rate limit is worth waiting out",
			status:    http.StatusTooManyRequests,
			body:      `{"type":"error","error":{"type":"rate_limit_error","message":"slow down"}}`,
			want:      llm.ErrorRateLimit,
			retryable: true,
		},
		{
			name:      "an overloaded model may recover",
			status:    http.StatusServiceUnavailable,
			body:      `{"type":"error","error":{"type":"overloaded_error","message":"Overloaded"}}`,
			want:      llm.ErrorProvider,
			retryable: true,
		},
		{
			name:   "an oversized context needs a different context, not a retry",
			status: http.StatusBadRequest,
			body:   `{"type":"error","error":{"type":"invalid_request_error","message":"prompt is too long: 210000 tokens > 200000 maximum"}}`,
			want:   llm.ErrorContextLength,
		},
		{
			name:   "anything else is the provider refusing",
			status: http.StatusBadRequest,
			body:   `{"type":"error","error":{"type":"invalid_request_error","message":"max_tokens is required"}}`,
			want:   llm.ErrorProvider,
		},
		{
			name:      "a proxy's HTML page is still diagnosable",
			status:    http.StatusBadGateway,
			body:      "<html><body>502 Bad Gateway</body></html>",
			want:      llm.ErrorProvider,
			retryable: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			client, err := New(Config{
				Provider: "anthropic",
				BaseURL:  "https://api.example.test/v1",
				APIKey:   "sk-ant-secret",
				HTTPClient: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
					return respond(tc.status, "application/json", tc.body), nil
				})},
			})
			if err != nil {
				t.Fatalf("New: %v", err)
			}

			rec := &llmtest.Recorder{}
			_, err = client.Stream(context.Background(), llm.Request{
				Model:    "claude-sonnet-4-5",
				Messages: []llm.Message{llm.TextMessage(llm.RoleUser, "hello")},
			}, rec.Emit)

			if !llm.IsKind(err, tc.want) {
				t.Fatalf("error = %v, want kind %s", err, tc.want)
			}
			var modelErr *llm.Error
			if !errors.As(err, &modelErr) {
				t.Fatalf("error = %v, want an *llm.Error", err)
			}
			if modelErr.Retryable != tc.retryable {
				t.Errorf("retryable = %v, want %v", modelErr.Retryable, tc.retryable)
			}
			if strings.Contains(err.Error(), "sk-ant-secret") {
				t.Error("the credential reached the error message")
			}
			if len(rec.Events) != 0 {
				t.Errorf("events = %v, want none before the stream started", rec.Types())
			}
		})
	}
}

func TestNewRejectsAConfigWithoutAnEndpoint(t *testing.T) {
	if _, err := New(Config{Provider: "anthropic"}); err == nil {
		t.Fatal("a client with no base URL was accepted")
	}
}
