package api

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/qunqin/openchat/apps/server/internal/agent"
	"github.com/qunqin/openchat/apps/server/internal/catalog"
	"github.com/qunqin/openchat/apps/server/internal/db/sqlc"
	"github.com/qunqin/openchat/apps/server/internal/llm"
	"github.com/qunqin/openchat/apps/server/internal/providers/anthropic"
	"github.com/qunqin/openchat/apps/server/internal/providers/gemini"
	"github.com/qunqin/openchat/apps/server/internal/providers/openai"
)

// ModelResolver turns a run's pinned channel and model into a client speaking
// that channel's protocol.
//
// It lives here rather than in the agent package because the channel type
// table does: which protocol a vendor speaks and where its default endpoint is
// are this package's facts, and the agent layer only ever sees llm.ModelClient.
type ModelResolver struct {
	q *sqlc.Queries
}

var _ agent.ModelResolver = (*ModelResolver)(nil)

func NewModelResolver(pool *pgxpool.Pool) *ModelResolver {
	return &ModelResolver{q: sqlc.New(pool)}
}

func (m *ModelResolver) Client(ctx context.Context, channelID *uuid.UUID, modelID string) (llm.ModelClient, error) {
	if channelID == nil {
		return nil, agent.ErrNoModel
	}
	channel, err := m.q.GetServingChannel(ctx, sqlc.GetServingChannelParams{ChannelID: *channelID, ModelID: modelID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, agent.ErrNoModel
		}
		return nil, err
	}
	// The model's settings — the catalogue's figures with the operator's
	// decisions over them — shape every request, and say whether it thinks.
	settings, err := loadModelSettings(ctx, m.q, modelID)
	if err != nil {
		return nil, err
	}
	client, err := clientFor(channel, modelID, settings.Reasoning)
	if err != nil {
		return nil, err
	}
	wire, _ := wireForModel(ChannelType(channel.Type), channel.ApiFormat, modelID)
	return &catalogShaped{inner: client, settings: settings, anthropic: wire == wireAnthropic}, nil
}

// A Messages API request is given max_tokens from the catalogue's output limit,
// capped at anthropic.ThinkingMaxTokens for the effort asked for (32000, and
// 64000 at xhigh and max). The API requires the field, and the adapter's own
// default (4096) is too little for a current model, which thinks by default and
// spends the thinking from the same allowance.

// catalogShaped fits each request to its model before the adapter sees it.
//
// A run asks for a thinking level by name. Whether the model takes that level
// as an effort or as a token budget is the catalogue's to say — sending the
// wrong one is a rejected request — so this turns the name into the form the
// model takes, and drops a level the model does not have or the operator does
// not allow. An output limit the operator set goes with every request; the
// catalogue's own goes only where the protocol requires one.
type catalogShaped struct {
	inner     llm.ModelClient
	settings  catalog.Settings
	anthropic bool
}

var _ agent.ToolCapable = (*catalogShaped)(nil)

// CallsTools reports whether the model may be offered tools. The runner asks
// before it builds the request, so the system prompt does not describe tools
// the model will not be given.
func (c *catalogShaped) CallsTools() bool { return c.settings.ToolCall }

func (c *catalogShaped) Stream(ctx context.Context, req llm.Request, emit func(llm.Event) error) (llm.ModelResponse, error) {
	if r := req.Options.Reasoning; r != nil && r.Effort != "" {
		profile := c.settings.Profile
		if c.anthropic && anthropic.ExtendedThinkingOnly(req.Model) {
			// Claude 4.5 and earlier reject adaptive thinking, whatever effort
			// levels the catalogue lists for them.
			profile = profile.BudgetOnly()
		}
		req.Options.Reasoning = profile.Options(r.Effort)
	}
	if !c.settings.ToolCall {
		req.Tools = nil
	}
	limit := c.settings.OutputLimit
	switch {
	case limit <= 0:
	case c.settings.OutputLimitSet:
		if req.Options.MaxTokens == nil || int64(*req.Options.MaxTokens) > limit {
			capped := int(limit)
			req.Options.MaxTokens = &capped
		}
	case c.anthropic && req.Options.MaxTokens == nil:
		var effort llm.ReasoningEffort
		if r := req.Options.Reasoning; r != nil {
			effort = r.Effort
		}
		capped := int(min(limit, int64(anthropic.ThinkingMaxTokens(effort))))
		req.Options.MaxTokens = &capped
	}
	return c.inner.Stream(ctx, req, emit)
}

// clientFor builds the adapter for a channel's requests for modelID. A new
// protocol is one more case here and one more adapter package; nothing above
// the llm interface changes. Most channel types speak one protocol for every
// model; a gateway's type can route each model to the API that serves it.
//
// thinks is whether the model reasons, which the Responses API needs to know
// before it is sent the parameters that only a reasoning model accepts.
func clientFor(channel sqlc.Channel, modelID string, thinks bool) (llm.ModelClient, error) {
	kind := ChannelType(channel.Type)
	wire, ok := wireForModel(kind, channel.ApiFormat, modelID)
	if !ok {
		return nil, fmt.Errorf("api: channel %s has unknown type %q", channel.ID, channel.Type)
	}
	baseURL := strings.TrimSpace(channel.BaseUrl)
	if baseURL == "" {
		baseURL = defaultEndpointFor(kind)
	}
	if baseURL == "" {
		// The relay type has no default, and guessing one would send the
		// operator's key somewhere they did not choose.
		return nil, agent.ErrNoModel
	}
	provider := string(channel.Type)
	var headers map[string]string
	var sessionHeader string
	if gateway := gatewayFor(kind); gateway != nil {
		headers = map[string]string{"User-Agent": gateway.UserAgent}
		sessionHeader = gateway.SessionHeader
	}

	switch wire {
	case wireAnthropic:
		return anthropic.New(anthropic.Config{Provider: provider, BaseURL: baseURL, APIKey: channel.ApiKey,
			Headers: headers, SessionHeader: sessionHeader})
	case wireGemini:
		return gemini.New(gemini.Config{Provider: provider, BaseURL: baseURL, APIKey: channel.ApiKey})
	case wireResponses:
		return openai.NewResponses(openai.Config{Provider: provider, BaseURL: baseURL, APIKey: channel.ApiKey,
			Headers: headers, SessionHeader: sessionHeader, Reasoning: thinks})
	default:
		return openai.New(openai.Config{Provider: provider, BaseURL: baseURL, APIKey: channel.ApiKey,
			Headers: headers, SessionHeader: sessionHeader, Compat: compatFor(kind)})
	}
}

// pickChannel chooses where a new run of modelID is answered from. An empty
// modelID means "whatever is available": the conversation never chose. Only a
// model offered in chat may be picked — one an operator has marked as an
// embedding model, say, is unavailable however it is asked for, and one kept
// for administrators is unavailable unless staff: an administrator's request,
// or the server's own task that no person asked for.
func (s *Server) pickChannel(ctx context.Context, modelID string, staff bool) (string, uuid.UUID, error) {
	if modelID == "" {
		models, err := s.chatModels(ctx, staff)
		if err != nil {
			return "", uuid.Nil, err
		}
		if len(models) == 0 {
			return "", uuid.Nil, errNoModels
		}
		modelID = models[0].Id
	} else {
		settings, err := loadModelSettings(ctx, s.q, modelID)
		if err != nil {
			return "", uuid.Nil, err
		}
		if settings.Kind != catalog.KindText || (settings.AdminOnly && !staff) {
			return "", uuid.Nil, errModelUnavailable
		}
	}
	channelID, err := s.q.PickServingChannel(ctx, modelID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", uuid.Nil, errModelUnavailable
		}
		return "", uuid.Nil, err
	}
	return modelID, channelID, nil
}
