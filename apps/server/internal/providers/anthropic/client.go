// Package anthropic implements the Anthropic Messages wire protocol.
//
// Like the openai package it is named after the protocol rather than the
// vendor: Bedrock and Vertex serve the same request and stream shapes behind
// different credentials, and they should arrive as configuration rather than as
// another adapter.
//
// Three things separate it from Chat Completions, and all three are places
// where a careless mapping misreports something the user pays for:
//
//   - The stream dispatches on NAMED events, not on the shape of an anonymous
//     payload. content_block_delta and message_delta both carry a `delta`, and
//     they mean entirely different things.
//   - Usage is split across two events and its cache counters sit beside
//     input_tokens instead of inside it, which is the opposite of the
//     convention Chat Completions uses.
//   - The system prompt is a top-level field, not a message, and the messages
//     that remain have to alternate.
package anthropic

import (
	"bytes"
	"context"
	"encoding/json"
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

// defaultVersion is the anthropic-version every request must carry. It is
// pinned rather than tracked, for the same reason modellist pins it: the header
// is what keeps this parser reading the shape it was written for, so moving it
// is a deliberate edit with a fixture behind it.
const defaultVersion = "2023-06-01"

// defaultMaxTokens is what we ask for when nobody said. max_tokens is required
// by this protocol and optional in llm.Options, so some number has to be
// invented here; a deployment that cares should set Config.MaxTokens rather
// than discover this one by having an answer cut off at it.
const defaultMaxTokens = 4096

type Config struct {
	// Provider is the configured identity — "anthropic", "bedrock", a gateway's
	// name. It is stamped on responses and selects nothing.
	Provider string

	// BaseURL is the API root, without /messages.
	BaseURL string

	// APIKey is resolved during assembly. It never comes from a request, and
	// never appears in an error.
	APIKey string

	// Version overrides the anthropic-version header. Empty uses defaultVersion.
	Version string

	// SessionHeader names the header a gateway reads the request's Session
	// from (OpenCode Go: x-opencode-session). Empty sends no session.
	SessionHeader string

	// Headers are extra headers a gateway requires. They are set after ours, so
	// a deployment that must override anthropic-version can.
	Headers map[string]string

	// HTTPClient overrides the default, mainly so tests can serve fixtures.
	HTTPClient *http.Client

	// SendUser puts the request's end-user identifier (llm.Request.User) on the
	// wire as `metadata.user_id`. Off by default: a gateway other than
	// Anthropic's may reject a field it does not know, so the operator turns it
	// on per channel.
	SendUser bool

	// ExplicitCachingOnly leaves out the request-level cache_control and keeps
	// only the marker on the system prompt. Some Messages deployments (the
	// legacy Amazon Bedrock integration for Opus 4.6 and earlier) reject the
	// top-level field with a 400, and a gateway that is not known to forward it
	// is safer without.
	ExplicitCachingOnly bool

	// MaxTokens is the ceiling to send when the request does not name one. Zero
	// uses defaultMaxTokens.
	MaxTokens int

	// MaxToolArgumentBytes caps a single tool call's accumulated arguments.
	// Zero uses the package default.
	MaxToolArgumentBytes int
}

type Client struct {
	cfg      Config
	endpoint string
	http     *http.Client
}

var _ llm.ModelClient = (*Client)(nil)

func New(cfg Config) (*Client, error) {
	if cfg.BaseURL == "" {
		return nil, llm.Errorf(llm.ErrorProvider, nil, "anthropic: base URL is required")
	}
	if _, err := url.Parse(cfg.BaseURL); err != nil {
		return nil, llm.Errorf(llm.ErrorProvider, err, "anthropic: base URL is not a URL")
	}

	httpClient := cfg.HTTPClient
	if httpClient == nil {
		transport := http.DefaultTransport.(*http.Transport).Clone()
		transport.ResponseHeaderTimeout = responseHeaderTimeout
		httpClient = &http.Client{Transport: transport}
	}

	return &Client{
		cfg:      cfg,
		endpoint: strings.TrimSuffix(cfg.BaseURL, "/") + "/messages",
		http:     httpClient,
	}, nil
}

func (c *Client) Stream(ctx context.Context, req llm.Request, emit func(llm.Event) error) (llm.ModelResponse, error) {
	// Returned alongside every early failure so the caller always gets a
	// response it can attribute, even when there is no content in it.
	empty := llm.ModelResponse{Provider: c.cfg.Provider, Model: req.Model}

	body, err := buildBody(req, c.cfg)
	if err != nil {
		return empty, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return empty, llm.Errorf(llm.ErrorTransport, err, "anthropic: building the request")
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	httpReq.Header.Set("Anthropic-Version", c.version())
	if c.cfg.APIKey != "" {
		// Not Authorization: this API answers 401 to a bearer token.
		httpReq.Header.Set("X-Api-Key", c.cfg.APIKey)
	}
	for name, value := range c.cfg.Headers {
		httpReq.Header.Set(name, value)
	}
	if c.cfg.SessionHeader != "" && req.Session != "" {
		httpReq.Header.Set(c.cfg.SessionHeader, req.Session)
	}

	resp, err := c.http.Do(httpReq)
	if err != nil {
		// Cancelled before the model said anything: there is nothing to keep,
		// and this is a stop rather than a failure.
		if ctxErr := ctx.Err(); ctxErr != nil {
			return empty, ctxErr
		}
		return empty, llm.Errorf(llm.ErrorTransport, err, "anthropic: calling the endpoint")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return empty, statusError(resp)
	}
	return decode(ctx, resp.Body, c.cfg, req.Model, emit)
}

func (c *Client) version() string {
	if c.cfg.Version != "" {
		return c.cfg.Version
	}
	return defaultVersion
}

// statusError maps a failed response onto a category the run layer can act on.
// It keeps the provider's own message and error type — support will ask for
// them — and nothing else: headers carry our credentials, and the raw body is
// not something a browser should ever be shown.
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

	// The status is more trustworthy than the error type for the categories
	// that decide whether to retry, so it wins where the two overlap.
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

// errorFrom categorises this API's own error types, which is what a mid-stream
// error event has instead of a status code.
func errorFrom(e wireError) *llm.Error {
	out := &llm.Error{Kind: llm.ErrorProvider, Code: e.Type, Message: e.Message}
	switch e.Type {
	case "authentication_error", "permission_error":
		out.Kind = llm.ErrorAuth
	case "rate_limit_error":
		out.Kind, out.Retryable = llm.ErrorRateLimit, true
	case "overloaded_error", "api_error":
		out.Kind, out.Retryable = llm.ErrorProvider, true
	case "invalid_request_error":
		if isContextLength(e) {
			out.Kind = llm.ErrorContextLength
		}
	}
	return out
}

// isContextLength recognises the one 4xx worth telling apart: a request the
// model could never have accepted, where retrying it unchanged is pointless and
// the fix belongs to the context builder. This API spells it in prose.
func isContextLength(e wireError) bool {
	lowered := strings.ToLower(e.Message)
	return strings.Contains(lowered, "prompt is too long") ||
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
