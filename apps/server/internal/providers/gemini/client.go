// Package gemini implements the Google Generative Language streaming protocol.
//
// It is the odd one of the three, and each difference is somewhere a habit
// carried over from the others produces a wrong answer rather than an error:
//
//   - The credential is in the QUERY STRING, so the request URL is itself a
//     secret. Nothing here may put a URL in an error, a log line or a wrapped
//     transport failure — *url.Error prints one in full, which is what sanitize
//     exists to undo.
//   - Each SSE frame is a whole GenerateContentResponse, not a delta. The parts
//     inside it are the new content; everything around them is restated.
//   - A part with "thought": true is reasoning. Reading it as text puts the
//     model's private deliberation into the answer the user sees.
//   - A function call arrives complete, with parsed arguments and no id.
package gemini

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/qunqin/openchat/apps/server/internal/llm"
)

// responseHeaderTimeout bounds how long an endpoint may take to start
// answering. There is deliberately no timeout on the body: a long answer is not
// a hung request, and what bounds the whole turn is the Run's own context.
const responseHeaderTimeout = 60 * time.Second

// maxErrorBodyBytes is how much of a failed response we read. Enough to explain
// the failure in a log, not enough for an HTML error page to fill memory.
const maxErrorBodyBytes = 8 << 10

const (
	// modelsPrefix is how this API spells a resource name — `models/x` is the
	// path the model lives at, not what the model is called. Everything else,
	// this product included, refers to it as `x`.
	modelsPrefix = "models/"

	// streamMethod is the method suffix a streaming turn calls. With alt=sse it
	// answers in server-sent events; without it, in a JSON array, which is not
	// a stream in any useful sense.
	streamMethod = ":streamGenerateContent"
)

type Config struct {
	// Provider is the configured identity — "google", "vertex", a gateway's
	// name. It is stamped on responses and selects nothing.
	Provider string

	// BaseURL is the API root, without /models.
	BaseURL string

	// APIKey is resolved during assembly. It travels in the query string, which
	// makes every URL built from it a credential.
	APIKey string

	// Headers are extra headers a gateway requires.
	Headers map[string]string

	// HTTPClient overrides the default, mainly so tests can serve fixtures.
	HTTPClient *http.Client
}

type Client struct {
	cfg  Config
	base *url.URL
	http *http.Client
}

var _ llm.ModelClient = (*Client)(nil)

func New(cfg Config) (*Client, error) {
	if cfg.BaseURL == "" {
		return nil, llm.Errorf(llm.ErrorProvider, nil, "gemini: base URL is required")
	}
	base, err := url.Parse(strings.TrimSuffix(cfg.BaseURL, "/"))
	if err != nil {
		return nil, llm.Errorf(llm.ErrorProvider, err, "gemini: base URL is not a URL")
	}

	httpClient := cfg.HTTPClient
	if httpClient == nil {
		transport := http.DefaultTransport.(*http.Transport).Clone()
		transport.ResponseHeaderTimeout = responseHeaderTimeout
		httpClient = &http.Client{Transport: transport}
	}

	return &Client{cfg: cfg, base: base, http: httpClient}, nil
}

func (c *Client) Stream(ctx context.Context, req llm.Request, emit func(llm.Event) error) (llm.ModelResponse, error) {
	// Returned alongside every early failure so the caller always gets a
	// response it can attribute, even when there is no content in it.
	empty := llm.ModelResponse{Provider: c.cfg.Provider, Model: req.Model}

	endpoint, err := c.endpoint(req.Model)
	if err != nil {
		return empty, err
	}
	body, err := buildBody(req)
	if err != nil {
		return empty, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return empty, llm.Errorf(llm.ErrorTransport, sanitize(err), "gemini: building the request")
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	for name, value := range c.cfg.Headers {
		httpReq.Header.Set(name, value)
	}

	resp, err := c.http.Do(httpReq)
	if err != nil {
		// Cancelled before the model said anything: there is nothing to keep,
		// and this is a stop rather than a failure.
		if ctxErr := ctx.Err(); ctxErr != nil {
			return empty, ctxErr
		}
		return empty, llm.Errorf(llm.ErrorTransport, sanitize(err), "gemini: calling the endpoint")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return empty, statusError(resp)
	}
	return decode(ctx, resp.Body, c.cfg, req.Model, emit)
}

// endpoint builds the streaming URL for one model. The key goes in the query
// string because that is where this API takes it, which is why the result is
// never allowed to appear in an error.
func (c *Client) endpoint(model string) (string, error) {
	resource := strings.TrimSpace(model)
	if resource == "" {
		return "", llm.Errorf(llm.ErrorProvider, nil, "gemini: the request names no model")
	}
	// A bare id is a model; anything already carrying a collection — a tuned
	// model, say — is a resource path and is left alone.
	if !strings.Contains(resource, "/") {
		resource = modelsPrefix + resource
	}

	u := *c.base
	u.Path = strings.TrimSuffix(u.Path, "/") + "/" + resource + streamMethod

	query := u.Query()
	query.Set("alt", "sse")
	if c.cfg.APIKey != "" {
		query.Set("key", c.cfg.APIKey)
	}
	u.RawQuery = query.Encode()
	return u.String(), nil
}

// sanitize strips the request URL out of a transport failure. On this protocol
// the URL is a credential, and *url.Error prints it whole — so the wrapper is
// dropped and only the cause it carried survives.
func sanitize(err error) error {
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return urlErr.Err
	}
	return err
}

// statusError maps a failed response onto a category the run layer can act on.
// It keeps the provider's own message and status — support will ask for them —
// and nothing else: the URL carries our credential, and the raw body is not
// something a browser should ever be shown.
func statusError(resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodyBytes))

	var payload struct {
		Error wireError `json:"error"`
	}
	_ = json.Unmarshal(body, &payload)

	err := errorFrom(payload.Error)
	if err.Message == "" {
		// Not JSON we recognise — a proxy's error page, most likely. A clipped
		// copy is what makes that diagnosable at all.
		err.Message = http.StatusText(resp.StatusCode) + ": " + clip(string(body))
	}

	// The status is more trustworthy than the body for the categories that
	// decide whether to retry, so it wins where the two overlap.
	switch {
	case resp.StatusCode == http.StatusUnauthorized, resp.StatusCode == http.StatusForbidden:
		err.Kind, err.Retryable = llm.ErrorAuth, false
	case resp.StatusCode == http.StatusTooManyRequests:
		err.Kind, err.Retryable = llm.ErrorRateLimit, true
	case resp.StatusCode >= 500:
		err.Kind, err.Retryable = llm.ErrorProvider, true
	case isContextLength(payload.Error):
		err.Kind, err.Retryable = llm.ErrorContextLength, false
	}
	return err
}

// errorFrom categorises this API's own status strings, which is what a
// mid-stream error object has instead of an HTTP code.
func errorFrom(e wireError) *llm.Error {
	out := &llm.Error{Kind: llm.ErrorProvider, Code: e.Status, Message: e.Message}
	switch e.Status {
	case "UNAUTHENTICATED", "PERMISSION_DENIED":
		out.Kind = llm.ErrorAuth
	case "RESOURCE_EXHAUSTED":
		out.Kind, out.Retryable = llm.ErrorRateLimit, true
	case "UNAVAILABLE", "INTERNAL", "DEADLINE_EXCEEDED":
		out.Kind, out.Retryable = llm.ErrorProvider, true
	case "INVALID_ARGUMENT":
		// This API answers a rejected credential with a 400 INVALID_ARGUMENT
		// rather than a 401, so the status alone would file the one failure an
		// operator has to fix under "the provider refused something".
		switch {
		case isContextLength(e):
			out.Kind = llm.ErrorContextLength
		case isBadKey(e):
			out.Kind = llm.ErrorAuth
		}
	}
	return out
}

func isBadKey(e wireError) bool {
	lowered := strings.ToLower(e.Message)
	return strings.Contains(lowered, "api key not valid") || strings.Contains(lowered, "api key expired")
}

// isContextLength recognises the one 4xx worth telling apart: a request the
// model could never have accepted, where retrying it unchanged is pointless and
// the fix belongs to the context builder. This API spells it in prose.
func isContextLength(e wireError) bool {
	lowered := strings.ToLower(e.Message)
	return strings.Contains(lowered, "exceeds the maximum number of tokens") ||
		strings.Contains(lowered, "input token count") ||
		strings.Contains(lowered, "maximum context length") ||
		strings.Contains(lowered, "context length exceeded")
}

func clip(s string) string {
	const max = 256
	s = strings.TrimSpace(s)
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}
