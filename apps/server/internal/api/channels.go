package api

import (
	"net/http"
	"strings"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/qunqin/openchat/apps/server/internal/db/sqlc"
	"github.com/qunqin/openchat/apps/server/internal/httpx"
)

// keyPreviewLength is how much of a credential the UI gets to see: enough to
// tell two keys apart, not enough to be worth intercepting.
const keyPreviewLength = 4

// toChannel maps a row to the contract's shape, which deliberately has no field
// the credential could travel in.
func toChannel(row sqlc.Channel) Channel {
	channel := Channel{
		Id:         row.ID,
		Name:       row.Name,
		ProviderId: row.ProviderID,
		Enabled:    row.Enabled,
		HasKey:     row.ApiKey != "",
		SortOrder:  int(row.SortOrder),
		CreatedAt:  row.CreatedAt.Time,
	}
	if row.BaseUrl != "" {
		base := row.BaseUrl
		channel.BaseUrl = &base
	}
	if key := []rune(row.ApiKey); len(key) > keyPreviewLength {
		preview := "…" + string(key[len(key)-keyPreviewLength:])
		channel.KeyPreview = &preview
	}
	return channel
}

func (s *Server) ListChannels(w http.ResponseWriter, r *http.Request) {
	rows, err := s.q.ListChannels(r.Context())
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}

	channels := make([]Channel, 0, len(rows))
	for _, row := range rows {
		channels = append(channels, toChannel(row))
	}
	httpx.JSON(w, http.StatusOK, channels)
}

func (s *Server) CreateChannel(w http.ResponseWriter, r *http.Request) {
	var body CreateChannelRequest
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.Fail(w, r, err)
		return
	}

	name := strings.TrimSpace(body.Name)
	providerID := strings.TrimSpace(body.ProviderId)
	switch {
	case name == "":
		httpx.Fail(w, r, httpx.NewFieldError("name", "name_required", "a name is required"))
		return
	case providerID == "":
		httpx.Fail(w, r, httpx.NewFieldError("providerId", "provider_required", "a provider is required"))
		return
	}

	row, err := s.q.CreateChannel(r.Context(), sqlc.CreateChannelParams{
		Name:       name,
		ProviderID: providerID,
		BaseUrl:    strings.TrimSpace(derefOr(body.BaseUrl, "")),
		ApiKey:     strings.TrimSpace(derefOr(body.ApiKey, "")),
		Enabled:    derefOr(body.Enabled, true),
	})
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, toChannel(row))
}

func (s *Server) UpdateChannel(w http.ResponseWriter, r *http.Request, channelId openapi_types.UUID) {
	var body UpdateChannelRequest
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.Fail(w, r, err)
		return
	}

	params := sqlc.UpdateChannelParams{ID: channelId}

	if body.Name != nil {
		trimmed := strings.TrimSpace(*body.Name)
		if trimmed == "" {
			httpx.Fail(w, r, httpx.NewFieldError("name", "name_required", "a name is required"))
			return
		}
		params.Name = &trimmed
	}
	if body.ProviderId != nil {
		trimmed := strings.TrimSpace(*body.ProviderId)
		if trimmed == "" {
			httpx.Fail(w, r, httpx.NewFieldError("providerId", "provider_required", "a provider is required"))
			return
		}
		params.ProviderID = &trimmed
	}
	if body.BaseUrl != nil {
		trimmed := strings.TrimSpace(*body.BaseUrl)
		params.BaseUrl = &trimmed
	}
	// Absent keeps the stored credential; present-but-empty clears it. There is
	// no way to read one back, so a form that means "leave it alone" has to omit
	// the field rather than echo a placeholder.
	if body.ApiKey != nil {
		trimmed := strings.TrimSpace(*body.ApiKey)
		params.ApiKey = &trimmed
	}
	params.Enabled = body.Enabled
	if body.SortOrder != nil {
		order := int32(*body.SortOrder)
		params.SortOrder = &order
	}

	row, err := s.q.UpdateChannel(r.Context(), params)
	if err != nil {
		httpx.Fail(w, r, dbErr(err))
		return
	}
	httpx.JSON(w, http.StatusOK, toChannel(row))
}

func (s *Server) DeleteChannel(w http.ResponseWriter, r *http.Request, channelId openapi_types.UUID) {
	affected, err := s.q.DeleteChannel(r.Context(), channelId)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	// Reporting 404 rather than 204 for an id that was never there keeps
	// "deleted" meaning something the caller can act on.
	if affected == 0 {
		httpx.Fail(w, r, httpx.ErrNotFound)
		return
	}
	httpx.NoContent(w)
}

// derefOr reads through an optional field, falling back when it is absent.
func derefOr[T any](value *T, fallback T) T {
	if value == nil {
		return fallback
	}
	return *value
}
