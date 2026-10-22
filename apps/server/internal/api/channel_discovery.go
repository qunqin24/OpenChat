package api

import (
	"context"
	"log/slog"
	"net/http"
	"strings"

	"github.com/qunqin/openchat/apps/server/internal/httpx"
	"github.com/qunqin/openchat/apps/server/internal/providers/modellist"
)

// PreviewChannelModels does not write anything. Cancelling channel creation
// after a preview must not leave an empty channel or a stored credential behind.
func (s *Server) PreviewChannelModels(w http.ResponseWriter, r *http.Request) {
	var body ChannelConnection
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	models, err := discoverChannelModels(r.Context(), body.Type,
		derefOr(body.BaseUrl, ""), derefOr(body.ApiKey, ""))
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}

	items := make([]ChannelModelInput, 0, len(models))
	for _, model := range models {
		items = append(items, ChannelModelInput{
			ModelId: model.ID, DisplayName: model.DisplayName, Source: ChannelModelInputSourceFetched,
		})
	}
	httpx.JSON(w, http.StatusOK, items)
}

// Shared by preview and refresh: a blank URL means this vendor's endpoint,
// not the protocol's endpoint (DeepSeek also speaks OpenAI, for example).
func discoverChannelModels(ctx context.Context, kind ChannelType, baseURL, key string) ([]modellist.Model, error) {
	wire, ok := wireApiFor(kind)
	if !ok {
		return nil, httpx.NewFieldError("type", "type_invalid", "a known channel type is required")
	}
	baseURL = endpointFor(kind, baseURL)
	// A relay has no default. Falling through to the OpenAI adapter's default
	// would send its key to an endpoint the operator did not select.
	if baseURL == "" {
		return nil, errChannelBaseURL
	}
	lister, err := modellist.For(modellist.Kind(wire), modellist.Config{
		BaseURL: baseURL, APIKey: strings.TrimSpace(key), AllowNoKey: keyOptionalFor(kind),
	})
	if err != nil {
		return nil, refreshError(err)
	}
	models, err := lister.List(ctx)
	if err != nil {
		// modellist redacts credentials; never log the submitted connection.
		slog.WarnContext(ctx, "channel model discovery", "type", kind, "error", err)
		return nil, refreshError(err)
	}
	if len(models) == 0 {
		return nil, errChannelListedNothing
	}
	return models, nil
}
