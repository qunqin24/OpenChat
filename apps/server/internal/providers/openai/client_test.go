package openai

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/qunqin/openchat/apps/server/internal/llm"
	"github.com/qunqin/openchat/apps/server/internal/llm/llmtest"
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
		Provider: "openai",
		BaseURL:  "https://api.example.test/v1/",
		APIKey:   "sk-test",
		Headers:  map[string]string{"X-Gateway": "internal"},
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
		Model:    "gpt-4o-mini",
		Messages: []llm.Message{llm.TextMessage(llm.RoleUser, "hello")},
	}, rec.Emit)
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	rec.CheckProviderInvariants(t)

	if got, want := captured.URL.String(), "https://api.example.test/v1/chat/completions"; got != want {
		t.Errorf("URL = %q, want %q", got, want)
	}
	if got := captured.Header.Get("Authorization"); got != "Bearer sk-test" {
		t.Errorf("Authorization = %q", got)
	}
	if got := captured.Header.Get("Accept"); got != "text/event-stream" {
		t.Errorf("Accept = %q", got)
	}
	if got := captured.Header.Get("X-Gateway"); got != "internal" {
		t.Errorf("configured header missing, got %q", got)
	}

	var sent map[string]any
	if err := json.Unmarshal(body, &sent); err != nil {
		t.Fatalf("request body: %v", err)
	}
	if sent["stream"] != true {
		t.Errorf("stream = %v, want true", sent["stream"])
	}
	if options, ok := sent["stream_options"].(map[string]any); !ok || options["include_usage"] != true {
		t.Errorf("stream_options = %v — without it the turn reports no usage", sent["stream_options"])
	}
	if resp.Message.Text() != "The aperture is open." {
		t.Errorf("text = %q", resp.Message.Text())
	}
	if resp.Provider != "openai" {
		t.Errorf("provider = %q, want the configured identity", resp.Provider)
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
			body:   `{"error":{"message":"Incorrect API key","code":"invalid_api_key"}}`,
			want:   llm.ErrorAuth,
		},
		{
			name:      "a rate limit is worth waiting out",
			status:    http.StatusTooManyRequests,
			body:      `{"error":{"message":"Rate limit reached"}}`,
			want:      llm.ErrorRateLimit,
			retryable: true,
		},
		{
			name:      "a server error may be transient",
			status:    http.StatusBadGateway,
			body:      `{"error":{"message":"upstream unavailable"}}`,
			want:      llm.ErrorProvider,
			retryable: true,
		},
		{
			name:   "an oversized context needs a different context, not a retry",
			status: http.StatusBadRequest,
			body:   `{"error":{"message":"This model's maximum context length is 128000 tokens","code":"context_length_exceeded"}}`,
			want:   llm.ErrorContextLength,
		},
		{
			name:   "anything else is the provider refusing",
			status: http.StatusBadRequest,
			body:   `{"error":{"message":"unknown parameter"}}`,
			want:   llm.ErrorProvider,
		},
		{
			name:      "a proxy's HTML page is still diagnosable",
			status:    http.StatusServiceUnavailable,
			body:      "<html><body>503 Service Unavailable</body></html>",
			want:      llm.ErrorProvider,
			retryable: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			client, err := New(Config{
				Provider: "openai",
				BaseURL:  "https://api.example.test/v1",
				APIKey:   "sk-secret",
				HTTPClient: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
					return respond(tc.status, "application/json", tc.body), nil
				})},
			})
			if err != nil {
				t.Fatalf("New: %v", err)
			}

			rec := &llmtest.Recorder{}
			_, err = client.Stream(context.Background(), llm.Request{
				Model:    "gpt-4o-mini",
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
			if strings.Contains(err.Error(), "sk-secret") {
				t.Error("the credential reached the error message")
			}
			if len(rec.Events) != 0 {
				t.Errorf("events = %v, want none before the stream started", rec.Types())
			}
		})
	}
}

func TestNewRejectsAConfigWithoutAnEndpoint(t *testing.T) {
	if _, err := New(Config{Provider: "openai"}); err == nil {
		t.Fatal("a client with no base URL was accepted")
	}
}
