package gemini

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
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
		Provider: "google",
		BaseURL:  "https://generative.example.test/v1beta/",
		APIKey:   "AIza-test",
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
		Model:    "gemini-2.5-flash",
		Messages: []llm.Message{llm.TextMessage(llm.RoleUser, "hello")},
	}, rec.Emit)
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	rec.CheckProviderInvariants(t)

	if got, want := captured.URL.Path, "/v1beta/models/gemini-2.5-flash:streamGenerateContent"; got != want {
		t.Errorf("path = %q, want %q", got, want)
	}
	query := captured.URL.Query()
	// Without alt=sse this endpoint answers with a JSON array, which is not a
	// stream in any useful sense.
	if query.Get("alt") != "sse" {
		t.Errorf("alt = %q", query.Get("alt"))
	}
	// The credential is in the query string on this protocol, which is what
	// makes every URL built from it a secret.
	if query.Get("key") != "AIza-test" {
		t.Errorf("key = %q", query.Get("key"))
	}
	if captured.Header.Get("Authorization") != "" {
		t.Error("a bearer token was sent, which this API does not take")
	}
	if got := captured.Header.Get("X-Gateway"); got != "internal" {
		t.Errorf("configured header missing, got %q", got)
	}

	var sent map[string]any
	if err := json.Unmarshal(body, &sent); err != nil {
		t.Fatalf("request body: %v", err)
	}
	if sent["contents"] == nil {
		t.Error("the history was not sent")
	}
	if resp.Message.Text() != "The aperture is open." {
		t.Errorf("text = %q", resp.Message.Text())
	}
	if resp.Provider != "google" {
		t.Errorf("provider = %q, want the configured identity", resp.Provider)
	}
}

func TestClientSpellsTheModelAsAResourcePath(t *testing.T) {
	tests := []struct {
		name  string
		model string
		want  string
	}{
		{
			name:  "a bare id becomes a model resource",
			model: "gemini-2.5-flash",
			want:  "/v1beta/models/gemini-2.5-flash:streamGenerateContent",
		},
		{
			name:  "an id that already names its collection is left alone",
			model: "tunedModels/my-tuning-7x",
			want:  "/v1beta/tunedModels/my-tuning-7x:streamGenerateContent",
		},
		{
			name:  "the models/ prefix is not doubled",
			model: "models/gemini-2.5-pro",
			want:  "/v1beta/models/gemini-2.5-pro:streamGenerateContent",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			client, err := New(Config{Provider: "google", BaseURL: "https://generative.example.test/v1beta"})
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			endpoint, err := client.endpoint(tc.model)
			if err != nil {
				t.Fatalf("endpoint: %v", err)
			}
			parsed, err := url.Parse(endpoint)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			if parsed.Path != tc.want {
				t.Errorf("path = %q, want %q", parsed.Path, tc.want)
			}
		})
	}
}

func TestClientNeverLetsTheKeyOutOfTheRequest(t *testing.T) {
	// The key lives in the URL here, and *url.Error prints the URL it failed
	// on in full. An unsanitised transport failure would put the credential in
	// every log line that error reaches.
	const key = "AIza-super-secret"

	t.Run("a transport failure", func(t *testing.T) {
		client, err := New(Config{
			Provider: "google",
			BaseURL:  "https://generative.example.test/v1beta",
			APIKey:   key,
			HTTPClient: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				return nil, errors.New("dial tcp: connection refused")
			})},
		})
		if err != nil {
			t.Fatalf("New: %v", err)
		}

		rec := &llmtest.Recorder{}
		_, err = client.Stream(context.Background(), llm.Request{
			Model:    "gemini-2.5-flash",
			Messages: []llm.Message{llm.TextMessage(llm.RoleUser, "hello")},
		}, rec.Emit)
		if !llm.IsKind(err, llm.ErrorTransport) {
			t.Fatalf("error = %v, want a transport error", err)
		}
		if strings.Contains(err.Error(), key) {
			t.Fatalf("the credential reached the error message: %v", err)
		}
		if strings.Contains(err.Error(), "generative.example.test") {
			t.Errorf("the request URL reached the error message, and on this protocol that is the credential: %v", err)
		}
	})

	t.Run("a failed status", func(t *testing.T) {
		client, err := New(Config{
			Provider: "google",
			BaseURL:  "https://generative.example.test/v1beta",
			APIKey:   key,
			HTTPClient: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				return respond(http.StatusForbidden, "application/json",
					`{"error":{"code":403,"message":"API key not valid","status":"PERMISSION_DENIED"}}`), nil
			})},
		})
		if err != nil {
			t.Fatalf("New: %v", err)
		}

		rec := &llmtest.Recorder{}
		_, err = client.Stream(context.Background(), llm.Request{
			Model:    "gemini-2.5-flash",
			Messages: []llm.Message{llm.TextMessage(llm.RoleUser, "hello")},
		}, rec.Emit)
		if !llm.IsKind(err, llm.ErrorAuth) {
			t.Fatalf("error = %v, want an auth error", err)
		}
		if strings.Contains(err.Error(), key) {
			t.Fatalf("the credential reached the error message: %v", err)
		}
	})
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
			// This API answers a rejected credential with a 400, so the status
			// alone would file the one failure an operator has to fix under
			// "the provider refused something".
			name:   "a bad key is the operator's problem even at 400",
			status: http.StatusBadRequest,
			body:   `{"error":{"code":400,"message":"API key not valid. Please pass a valid API key.","status":"INVALID_ARGUMENT"}}`,
			want:   llm.ErrorAuth,
		},
		{
			name:   "a rejected key by status",
			status: http.StatusForbidden,
			body:   `{"error":{"code":403,"message":"permission denied","status":"PERMISSION_DENIED"}}`,
			want:   llm.ErrorAuth,
		},
		{
			name:   "anything else is the provider refusing",
			status: http.StatusBadRequest,
			body:   `{"error":{"code":400,"message":"Unknown name \"thinkingConfg\"","status":"INVALID_ARGUMENT"}}`,
			want:   llm.ErrorProvider,
		},
		{
			name:      "a quota is worth waiting out",
			status:    http.StatusTooManyRequests,
			body:      `{"error":{"code":429,"message":"Quota exceeded","status":"RESOURCE_EXHAUSTED"}}`,
			want:      llm.ErrorRateLimit,
			retryable: true,
		},
		{
			name:      "a server error may be transient",
			status:    http.StatusServiceUnavailable,
			body:      `{"error":{"code":503,"message":"The model is overloaded","status":"UNAVAILABLE"}}`,
			want:      llm.ErrorProvider,
			retryable: true,
		},
		{
			name:   "an oversized context needs a different context, not a retry",
			status: http.StatusBadRequest,
			body:   `{"error":{"code":400,"message":"The input token count (1200000) exceeds the maximum number of tokens allowed (1048576)","status":"INVALID_ARGUMENT"}}`,
			want:   llm.ErrorContextLength,
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
				Provider: "google",
				BaseURL:  "https://generative.example.test/v1beta",
				APIKey:   "AIza-secret",
				HTTPClient: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
					return respond(tc.status, "application/json", tc.body), nil
				})},
			})
			if err != nil {
				t.Fatalf("New: %v", err)
			}

			rec := &llmtest.Recorder{}
			_, err = client.Stream(context.Background(), llm.Request{
				Model:    "gemini-2.5-flash",
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
			if strings.Contains(err.Error(), "AIza-secret") {
				t.Error("the credential reached the error message")
			}
			if len(rec.Events) != 0 {
				t.Errorf("events = %v, want none before the stream started", rec.Types())
			}
		})
	}
}

func TestNewRejectsAConfigWithoutAnEndpoint(t *testing.T) {
	if _, err := New(Config{Provider: "google"}); err == nil {
		t.Fatal("a client with no base URL was accepted")
	}
}
