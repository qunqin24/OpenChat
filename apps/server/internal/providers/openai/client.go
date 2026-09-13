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

	// EchoReasoning replays reasoning blocks from history back to the model.
	// Off by default because most endpoints on this protocol reject their own
	// reasoning_content coming back in, and the ones that want it want it in a
	// specific shape.
	EchoReasoning bool
}

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

	// HTTPClient overrides the default, mainly so tests can serve fixtures.
	HTTPClient *http.Client

	// MaxToolArgumentBytes caps a single tool call's accumulated arguments.
	// Zero uses the package default.
	MaxToolArgumentBytes int

	Compat Compat
}

type Client struct {
	cfg      Config
	endpoint string
	http     *http.Client
}

var _ llm.ModelClient = (*Client)(nil)

func New(cfg Config) (*Client, error) {
	if cfg.BaseURL == "" {
		return nil, llm.Errorf(llm.ErrorProvider, nil, "openai: base URL is required")
	}
	if _, err := url.Parse(cfg.BaseURL); err != nil {
		return nil, llm.Errorf(llm.ErrorProvider, err, "openai: base URL is not a URL")
	}

	httpClient := cfg.HTTPClient
	if httpClient == nil {
		transport := http.DefaultTransport.(*http.Transport).Clone()
		transport.ResponseHeaderTimeout = responseHeaderTimeout
		httpClient = &http.Client{Transport: transport}
	}

	return &Client{
		cfg:      cfg,
		endpoint: strings.TrimSuffix(cfg.BaseURL, "/") + "/chat/completions",
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
		return empty, llm.Errorf(llm.ErrorTransport, err, "openai: building the request")
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	if c.cfg.APIKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)
	}
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
		return empty, llm.Errorf(llm.ErrorTransport, err, "openai: calling the endpoint")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return empty, statusError(resp)
	}
	return decode(ctx, resp.Body, c.cfg, req.Model, emit)
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

	err := &llm.Error{Code: payload.Error.Code, Message: payload.Error.Message}
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
