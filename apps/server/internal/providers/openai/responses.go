package openai

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
	"sync"

	"github.com/qunqin24/openchat/apps/server/internal/llm"
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

	// A reasoning summary is the one parameter here an organisation can be
	// refused: OpenAI asks some to complete organisation verification first.
	// https://developers.openai.com/api/docs/guides/reasoning (Reasoning
	// summaries). Once refused, the model is asked without it from then on.
	refusal := summaryRefusalKey(c.endpoint, c.cfg.APIKey, req.Model)
	_, refused := summaryRefused.Load(refusal)
	summary := !refused

	body, err := buildResponsesBodyWith(req, c.cfg, summary)
	if err != nil {
		return empty, err
	}

	resp, err := openStream(ctx, c.http, c.endpoint, c.cfg, req.Session, body)
	if err != nil && summary && asksForReasoning(req, c.cfg) && isSummaryRefusal(err) {
		// Nothing was generated or billed, so asking again is honest. Once.
		summaryRefused.Store(refusal, struct{}{})
		if body, err = buildResponsesBodyWith(req, c.cfg, false); err != nil {
			return empty, err
		}
		resp, err = openStream(ctx, c.http, c.endpoint, c.cfg, req.Session, body)
	}
	if err != nil {
		return empty, err
	}
	defer resp.Body.Close()

	out, err := decodeResponses(ctx, resp.Body, c.cfg, req.Model, emit)
	// A failure reported inside the stream is the provider's text too.
	return out, scrub(err, c.cfg.APIKey)
}

// summaryRefused remembers, for the life of the process, which models an
// organisation was refused a reasoning summary for. A client is built per run,
// so this is keyed by what identifies the organisation (endpoint and a digest
// of the key) rather than held on the client; one refusal then spares every
// later request its wasted round trip.
var summaryRefused sync.Map // summaryRefusalKey -> struct{}

func summaryRefusalKey(endpoint, apiKey, model string) string {
	digest := sha256.Sum256([]byte(apiKey))
	return endpoint + "\x00" + hex.EncodeToString(digest[:8]) + "\x00" + model
}

// isSummaryRefusal recognises OpenAI's answer to a reasoning summary asked of
// an organisation that is not verified: a 400 whose message says the
// organisation "must be verified to generate reasoning summaries" (param
// `reasoning.summary`). The message is what is stable across SDKs and
// gateways; a 400 about anything else is left alone, as is the related
// "must be verified to stream this model", which no retry here can fix.
func isSummaryRefusal(err error) bool {
	var failure *llm.Error
	if !errors.As(err, &failure) || failure.Kind != llm.ErrorProvider {
		return false
	}
	message := strings.ToLower(failure.Message)
	return strings.Contains(message, "summar") &&
		(strings.Contains(message, "verif") || failure.Code == "unsupported_value")
}
