// Package openai implements the OpenAI Chat Completions wire protocol.
//
// It is named after the protocol, not the vendor. DeepSeek, Groq, vLLM, Ollama
// and most gateways speak this format, and the whole point is that they arrive
// as configuration rather than as another adapter — Config.Provider is metadata
// stamped onto responses, and nothing in here branches on it. A provider that
// needs a different request shape gets a Compat flag, which is a thing an
// operator can read, not a special case buried in a parser.
package openai

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

// Compat is what differs between endpoints that all claim to speak this
// protocol. Every flag here exists because some real gateway needs it; none of
// them are inferred from the URL, because guessing would mean rewriting a
// deployment the operator configured on purpose.
type Compat struct {
	// OmitStreamOptions drops stream_options.include_usage, which a few
	// gateways reject outright. The cost is that the turn reports no usage.
	OmitStreamOptions bool

	// InferFinishReason accepts a stream that ends without saying why, and
	// concludes tool_calls or stop from what arrived. Off by default: see
	// assembler.finishReason.
	InferFinishReason bool

	// EchoReasoning replays the thinking of earlier turns to the model, in the
	// assistant message's `reasoning_content` field (never in `content`).
	// Off by default because most endpoints on this protocol reject their own
	// reasoning_content coming back in. DeepSeek requires it when tools are
	// offered and ignores it otherwise; GLM, Kimi and Qwen read it to keep
	// their reasoning coherent across a tool loop. Replaying every turn is
	// what they document, and costs nothing where it is ignored.
	EchoReasoning bool

	// MaxCompletionTokens names the output limit `max_completion_tokens`
	// instead of the deprecated `max_tokens`. OpenAI's reasoning models reject
	// the old name outright, and it counts the thinking in the limit, which
	// the old one did not always.
	MaxCompletionTokens bool

	// Thinking is how this vendor takes a thinking level. The zero value is
	// the OpenAI spelling, a plain `reasoning_effort`.
	Thinking ThinkingStyle
}

// ThinkingStyle names a vendor's way of asking for thinking on this protocol.
// The vendors agree on the protocol and disagree on this one knob, and sending
// the wrong spelling is a rejected request or a silently ignored one.
type ThinkingStyle string

const (
	// ThinkingEffort is `reasoning_effort: <level>`, OpenAI's spelling and the
	// default for every endpoint that has no style of its own.
	ThinkingEffort ThinkingStyle = ""
	// ThinkingDashScope adds `enable_thinking` to the effort, and takes a
	// token budget as `thinking_budget`.
	ThinkingDashScope ThinkingStyle = "dashscope"
	// ThinkingSwitch is DeepSeek's and GLM's: thinking is turned off with
	// `thinking: {type: disabled}`, since `none` is not one of their levels,
	// and the levels they do have are a plain `reasoning_effort`.
	ThinkingSwitch ThinkingStyle = "switch"
	// ThinkingMoonshot is Kimi: K3 takes `reasoning_effort`, the K2 models an
	// on/off `thinking: {type}` and no effort at all.
	ThinkingMoonshot ThinkingStyle = "moonshot"
	// ThinkingOpenRouter is the unified `reasoning` object, whose effort has
	// `max` and `none` (the top-level field has no `max`) and which can carry
	// a token budget.
	ThinkingOpenRouter ThinkingStyle = "openrouter"
)

type Config struct {
	// Provider is the configured identity — "deepseek", "openai", a local
	// gateway's name. It is stamped on responses and selects nothing.
	Provider string

	// BaseURL is the API root, without /chat/completions.
	BaseURL string

	// APIKey is resolved during assembly. It never comes from a request, and
	// never appears in an error.
	APIKey string

	// Headers are extra headers a gateway requires.
	Headers map[string]string

	// SessionHeader names the header a gateway reads the request's Session
	// from (OpenCode Go: x-opencode-session). Empty sends no session.
	SessionHeader string

	// HTTPClient overrides the default, mainly so tests can serve fixtures.
	HTTPClient *http.Client

	// MaxToolArgumentBytes caps a single tool call's accumulated arguments.
	// Zero uses the package default.
	MaxToolArgumentBytes int

	Compat Compat

	// Reasoning says the model is one that thinks, as the catalogue (and the
	// operator over it) has it. The Responses protocol rejects the reasoning
	// parameters on a model that does not think, so it is asked for only
	// where this, or an explicit level, says they apply. Chat Completions
	// ignores it.
	Reasoning bool
}

type Client struct {
	cfg      Config
	endpoint string
	http     *http.Client
}

var _ llm.ModelClient = (*Client)(nil)

func New(cfg Config) (*Client, error) {
	if err := validateConfig(cfg); err != nil {
		return nil, err
	}

	return &Client{
		cfg:      cfg,
		endpoint: strings.TrimSuffix(cfg.BaseURL, "/") + "/chat/completions",
		http:     newHTTPClient(cfg),
	}, nil
}

func validateConfig(cfg Config) error {
	if cfg.BaseURL == "" {
		return llm.Errorf(llm.ErrorProvider, nil, "openai: base URL is required")
	}
	if _, err := url.Parse(cfg.BaseURL); err != nil {
		return llm.Errorf(llm.ErrorProvider, err, "openai: base URL is not a URL")
	}
	return nil
}

// newHTTPClient is the configured client, or a default one whose only limit is
// how long the endpoint may take to start answering. Both adapters in this
// package share it so that "the endpoint hung" means the same thing in each.
func newHTTPClient(cfg Config) *http.Client {
	if cfg.HTTPClient != nil {
		return cfg.HTTPClient
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.ResponseHeaderTimeout = responseHeaderTimeout
	return &http.Client{Transport: transport}
}

func (c *Client) Stream(ctx context.Context, req llm.Request, emit func(llm.Event) error) (llm.ModelResponse, error) {
	// Returned alongside every early failure so the caller always gets a
	// response it can attribute, even when there is no content in it.
	empty := llm.ModelResponse{Provider: c.cfg.Provider, Model: req.Model}

	body, err := buildBody(req, c.cfg)
	if err != nil {
		return empty, err
	}

	resp, err := openStream(ctx, c.http, c.endpoint, c.cfg, req.Session, body)
	if err != nil {
		return empty, err
	}
	defer resp.Body.Close()

	return decode(ctx, resp.Body, c.cfg, req.Model, emit)
}

// openStream POSTs body and returns the response only when it is a 200; the
// caller closes it. Every other outcome is an error already mapped onto a kind
// the run layer can act on, and the response body is closed here.
func openStream(ctx context.Context, client *http.Client, endpoint string, cfg Config, session string, body []byte) (*http.Response, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, llm.Errorf(llm.ErrorTransport, err, "openai: building the request")
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	if cfg.APIKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	}
	for name, value := range cfg.Headers {
		httpReq.Header.Set(name, value)
	}
	if cfg.SessionHeader != "" && session != "" {
		httpReq.Header.Set(cfg.SessionHeader, session)
	}

	resp, err := client.Do(httpReq)
	if err != nil {
		// Cancelled before the model said anything: there is nothing to keep,
		// and this is a stop rather than a failure.
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		return nil, llm.Errorf(llm.ErrorTransport, err, "openai: calling the endpoint")
	}

	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		return nil, scrub(statusError(resp), cfg.APIKey)
	}
	return resp, nil
}

// scrub removes the credential from an error built out of a provider's own
// words. A gateway that echoes the Authorization header back in its error
// message would otherwise carry the key into logs and, eventually, a browser.
func scrub(err error, secret string) error {
	if secret == "" {
		return err
	}
	var modelErr *llm.Error
	if errors.As(err, &modelErr) {
		modelErr.Message = strings.ReplaceAll(modelErr.Message, secret, "[redacted]")
		modelErr.Code = strings.ReplaceAll(modelErr.Code, secret, "[redacted]")
	}
	return err
}

// statusError maps a failed response onto a category the run layer can act on.
// It keeps the provider's own message and code — support will ask for them —
// and nothing else: headers carry our credentials, and the raw body is not
// something a browser should ever be shown.
func statusError(resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodyBytes))

	var payload struct {
		Error wireError `json:"error"`
	}
	_ = json.Unmarshal(body, &payload)

	err := &llm.Error{Code: string(payload.Error.Code), Message: payload.Error.Message}
	if err.Message == "" {
		// Not JSON we recognise — a proxy's error page, most likely. A clipped
		// copy is what makes that diagnosable at all.
		err.Message = http.StatusText(resp.StatusCode) + ": " + clip(string(body))
	}

	switch {
	case resp.StatusCode == http.StatusUnauthorized, resp.StatusCode == http.StatusForbidden:
		err.Kind = llm.ErrorAuth
	case resp.StatusCode == http.StatusTooManyRequests:
		err.Kind, err.Retryable = llm.ErrorRateLimit, true
	case resp.StatusCode >= 500:
		err.Kind, err.Retryable = llm.ErrorProvider, true
	case isContextLength(payload.Error):
		err.Kind = llm.ErrorContextLength
	default:
		err.Kind = llm.ErrorProvider
	}
	return err
}

// isContextLength recognises the one 4xx worth telling apart: a request the
// model could never have accepted, where retrying it unchanged is pointless and
// the fix belongs to the context builder.
func isContextLength(e wireError) bool {
	if e.Code == "context_length_exceeded" || e.Type == "context_length_exceeded" {
		return true
	}
	lowered := strings.ToLower(e.Message)
	return strings.Contains(lowered, "maximum context length") || strings.Contains(lowered, "context length exceeded")
}

func clip(s string) string {
	const max = 256
	s = strings.TrimSpace(s)
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}
