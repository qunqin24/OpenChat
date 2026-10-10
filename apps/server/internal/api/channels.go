package api

import (
	"context"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/qunqin/openchat/apps/server/internal/db/sqlc"
	"github.com/qunqin/openchat/apps/server/internal/httpx"
)

// keyPreviewLength is how much of a credential the UI gets to see: enough to
// tell two keys apart, not enough to be worth intercepting.
const keyPreviewLength = 4

// toChannel maps a row to the contract's shape, which deliberately has no field
// the credential could travel in.
//
// modelCount is passed in rather than read here: the list needs one grouped
// query for every channel, and a count per channel would be a query per row.
func toChannel(row sqlc.Channel, modelCount int64) Channel {
	wireApi, _ := wireApiFor(ChannelType(row.Type))

	channel := Channel{
		Id:   row.ID,
		Name: row.Name,
		Type: ChannelType(row.Type),
		// Derived, never stored: the type is what the operator chose, and the
		// protocol is a property of that choice. Storing both would let them
		// disagree, and the disagreement would be a channel speaking the wrong
		// protocol to a real endpoint.
		WireApi:   wireApi,
		ApiFormat: ApiFormat(row.ApiFormat),
		Enabled:   row.Enabled,
		HasKey:    row.ApiKey != "",
		// How many models this channel offers, which is how many are switched
		// on — a channel that lists two hundred models and offers none is
		// configured but not yet useful, and the number is what says so.
		ModelCount: int(modelCount),
		SortOrder:  int(row.SortOrder),
		CreatedAt:  row.CreatedAt.Time,
	}
	if row.BaseUrl != "" {
		base := row.BaseUrl
		channel.BaseUrl = &base
	}
	if preview := keyPreview(row.ApiKey); preview != "" {
		channel.KeyPreview = &preview
	}
	return channel
}

// keyPreview is a stored credential's last few characters, for telling two
// keys apart; empty for a key too short to show any of without showing most.
func keyPreview(key string) string {
	runes := []rune(key)
	if len(runes) <= keyPreviewLength {
		return ""
	}
	return "…" + string(runes[len(runes)-keyPreviewLength:])
}

func (s *Server) ListChannels(w http.ResponseWriter, r *http.Request) {
	rows, err := s.q.ListChannels(r.Context())
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}

	counts, err := s.enabledModelCounts(r.Context())
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}

	spend, err := s.q.ChannelSpend(r.Context())
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	spent := make(map[uuid.UUID]float64, len(spend))
	for _, row := range spend {
		spent[row.ChannelID] = row.Spent
	}

	channels := make([]Channel, 0, len(rows))
	for _, row := range rows {
		channel := toChannel(row, counts[row.ID])
		total := spent[row.ID]
		channel.Spent = &total
		channels = append(channels, channel)
	}
	httpx.JSON(w, http.StatusOK, channels)
}

// enabledModelCounts reads every channel's offered-model count in one query. A
// channel with none is absent from the map, which reads back as zero.
func (s *Server) enabledModelCounts(ctx context.Context) (map[uuid.UUID]int64, error) {
	rows, err := s.q.CountEnabledChannelModels(ctx)
	if err != nil {
		return nil, err
	}
	counts := make(map[uuid.UUID]int64, len(rows))
	for _, row := range rows {
		counts[row.ChannelID] = row.ModelCount
	}
	return counts, nil
}

func (s *Server) CreateChannel(w http.ResponseWriter, r *http.Request) {
	var body CreateChannelRequest
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.Fail(w, r, err)
		return
	}

	name := strings.TrimSpace(body.Name)
	if name == "" {
		httpx.Fail(w, r, httpx.NewFieldError("name", "name_required", "a name is required"))
		return
	}
	// The type carries the protocol with it, so this is the only thing the
	// operator has to get right.
	if _, ok := wireApiFor(body.Type); !ok {
		httpx.Fail(w, r, httpx.NewFieldError("type", "type_invalid", "a known channel type is required"))
		return
	}

	format := Chat
	if body.ApiFormat != nil {
		if !body.ApiFormat.Valid() {
			httpx.Fail(w, r, httpx.NewFieldError("apiFormat", "api_format_invalid", "the format is chat or responses"))
			return
		}
		format = *body.ApiFormat
	}

	models := derefOr(body.Models, []ChannelModelInput{})
	if err := validateChannelModels(models); err != nil {
		httpx.Fail(w, r, err)
		return
	}

	// The connection and the selected models are one operation. A failed model
	// insert must not leave a saved but empty channel behind.
	tx, err := s.pool.Begin(r.Context())
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()
	q := s.q.WithTx(tx)
	row, err := q.CreateChannel(r.Context(), sqlc.CreateChannelParams{
		Name:      name,
		Type:      sqlc.ChannelType(body.Type),
		BaseUrl:   strings.TrimSpace(derefOr(body.BaseUrl, "")),
		ApiKey:    strings.TrimSpace(derefOr(body.ApiKey, "")),
		Enabled:   derefOr(body.Enabled, true),
		ApiFormat: string(format),
	})
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	if len(models) > 0 {
		params := sqlc.CreateSelectedChannelModelsParams{ChannelID: row.ID}
		for _, model := range models {
			params.ModelIds = append(params.ModelIds, model.ModelId)
			params.DisplayNames = append(params.DisplayNames, model.DisplayName)
			params.Sources = append(params.Sources, string(model.Source))
		}
		if err := q.CreateSelectedChannelModels(r.Context(), params); err != nil {
			httpx.Fail(w, r, err)
			return
		}
	}
	if err := tx.Commit(r.Context()); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, toChannel(row, int64(len(models))))
}

func validateChannelModels(models []ChannelModelInput) error {
	if len(models) > 2000 {
		return httpx.NewFieldError("models", "models_invalid", "select at most 2000 models")
	}
	seen := make(map[string]bool, len(models))
	for i := range models {
		model := &models[i]
		model.ModelId = strings.TrimSpace(model.ModelId)
		model.DisplayName = strings.TrimSpace(model.DisplayName)
		if model.ModelId == "" || utf8.RuneCountInString(model.ModelId) > 200 ||
			utf8.RuneCountInString(model.DisplayName) > 500 ||
			(model.Source != ChannelModelInputSourceFetched && model.Source != ChannelModelInputSourceManual) ||
			seen[model.ModelId] {
			return httpx.NewFieldError("models", "models_invalid", "model selections must have unique ids and a valid source")
		}
		seen[model.ModelId] = true
	}
	return nil
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
	if body.Type != nil {
		if _, ok := wireApiFor(*body.Type); !ok {
			httpx.Fail(w, r, httpx.NewFieldError("type", "type_invalid", "a known channel type is required"))
			return
		}
		// Changing the type does not clear the stored models. They were fetched
		// from the old endpoint and may no longer be right, which the next
		// refresh settles; throwing them away here would also throw away which
		// ones the operator had switched on.
		kind := sqlc.ChannelType(*body.Type)
		params.Type = &kind
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
	if body.ApiFormat != nil {
		if !body.ApiFormat.Valid() {
			httpx.Fail(w, r, httpx.NewFieldError("apiFormat", "api_format_invalid", "the format is chat or responses"))
			return
		}
		format := string(*body.ApiFormat)
		params.ApiFormat = &format
	}
	if body.SortOrder != nil {
		order := int32(*body.SortOrder)
		params.SortOrder = &order
	}

	row, err := s.q.UpdateChannel(r.Context(), params)
	if err != nil {
		httpx.Fail(w, r, dbErr(err))
		return
	}

	count, err := s.q.CountEnabledChannelModelsFor(r.Context(), row.ID)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, toChannel(row, count))
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
