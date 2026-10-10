package openai

import (
	"context"
	"net/http"
	"strings"

	"github.com/qunqin/openchat/apps/server/internal/llm"
)

// ResponsesClient speaks the OpenAI Responses wire protocol (POST /responses).
//
// It sits next to Client rather than inside it because the two protocols differ
// in shape, not in dialect: a flat item list instead of messages, typed events
// instead of choice deltas, and reasoning that comes back as an encrypted item
// the next request has to carry. Config is shared so that an operator
// configures one endpoint once and picks the protocol per model.
type ResponsesClient struct {
	cfg      Config
	endpoint string
	http     *http.Client
}

var _ llm.ModelClient = (*ResponsesClient)(nil)

// NewResponses builds a client for {BaseURL}/responses. Config.Compat is
// Chat Completions vocabulary and is ignored here.
func NewResponses(cfg Config) (*ResponsesClient, error) {
	if err := validateConfig(cfg); err != nil {
		return nil, err
	}
	return &ResponsesClient{
		cfg:      cfg,
		endpoint: strings.TrimSuffix(cfg.BaseURL, "/") + "/responses",
		http:     newHTTPClient(cfg),
	}, nil
}

func (c *ResponsesClient) Stream(ctx context.Context, req llm.Request, emit func(llm.Event) error) (llm.ModelResponse, error) {
	empty := llm.ModelResponse{Provider: c.cfg.Provider, Model: req.Model}

	body, err := buildResponsesBody(req, c.cfg)
	if err != nil {
		return empty, err
	}

	resp, err := openStream(ctx, c.http, c.endpoint, c.cfg, req.Session, body)
	if err != nil {
		return empty, err
	}
	defer resp.Body.Close()

	out, err := decodeResponses(ctx, resp.Body, c.cfg, req.Model, emit)
	// A failure reported inside the stream is the provider's text too.
	return out, scrub(err, c.cfg.APIKey)
}
