package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/qunqin/openchat/apps/server/internal/db/sqlc"
	"github.com/qunqin/openchat/apps/server/internal/httpx"
	"github.com/qunqin/openchat/apps/server/internal/providers/modellist"
)

// The errors a refresh can end in. They are separate codes rather than one
// "refresh failed" because they are separate things for the operator to do:
// store a key, fix a URL, fix the key, or wait for somebody else's server.
//
// None of them carries anything the endpoint said. What the endpoint said goes
// to the log, where the credential has already been scrubbed out of it; what
// the operator gets is a status and a reason the panel can translate.
var (
	errChannelKeyMissing = httpx.NewError(http.StatusBadRequest, "channel_key_missing",
		"this channel has no stored key to authenticate with")
	errChannelBaseURL = httpx.NewFieldError("baseUrl", "channel_base_url_invalid",
		"the base URL must be an http or https address")
	errChannelKindUnsupported = httpx.NewError(http.StatusBadRequest, "channel_kind_unsupported",
		"listing models is not implemented for this kind of channel")
	errChannelKeyRejected = httpx.NewError(http.StatusBadGateway, "channel_key_rejected",
		"the endpoint rejected the stored key")
	errChannelRedirected = httpx.NewError(http.StatusBadGateway, "channel_redirect_refused",
		"the endpoint redirected to another host, which would have sent the key there")
	errChannelUnreadable = httpx.NewError(http.StatusBadGateway, "channel_response_invalid",
		"the endpoint's answer was not a model list")
	errChannelUnreachable = httpx.NewError(http.StatusBadGateway, "channel_unreachable",
		"the endpoint could not be reached")
	errChannelListedNothing = httpx.NewError(http.StatusBadGateway, "channel_listed_nothing",
		"the endpoint listed no models, so nothing was changed")
	errModelExists = httpx.NewError(http.StatusConflict, "model_exists",
		"this channel already lists that model")
	errModelNotManual = httpx.NewError(http.StatusConflict, "model_not_manual",
		"this model came from the endpoint's own list; refresh to remove it")
)

func (s *Server) ListChannelModels(w http.ResponseWriter, r *http.Request, channelId openapi_types.UUID) {
	channel, err := s.q.GetChannel(r.Context(), channelId)
	if err != nil {
		httpx.Fail(w, r, dbErr(err))
		return
	}

	rows, err := s.q.ListChannelModels(r.Context(), channelId)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}

	models, err := s.channelModels(r.Context(), channel, rows)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, models)
}

func (s *Server) AddChannelModel(w http.ResponseWriter, r *http.Request, channelId openapi_types.UUID) {
	var body AddChannelModelJSONRequestBody
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.Fail(w, r, err)
		return
	}

	modelID := strings.TrimSpace(body.ModelId)
	if modelID == "" {
		httpx.Fail(w, r, httpx.NewFieldError("modelId", "model_required", "a model id is required"))
		return
	}

	// Checked rather than left to the foreign key: an unknown channel is a 404,
	// and a constraint violation would arrive as a 500.
	channel, err := s.q.GetChannel(r.Context(), channelId)
	if err != nil {
		httpx.Fail(w, r, dbErr(err))
		return
	}

	row, err := s.q.AddManualChannelModel(r.Context(), sqlc.AddManualChannelModelParams{
		ChannelID: channelId,
		ModelID:   modelID,
		// The endpoint names its own models; an operator typing one in is
		// naming the id, and the id is what gets shown when there is no name.
		DisplayName: "",
	})
	if err != nil {
		// The insert does nothing on conflict, so no row back means the model
		// is already listed — under either source.
		if errors.Is(err, pgx.ErrNoRows) {
			httpx.Fail(w, r, errModelExists)
			return
		}
		httpx.Fail(w, r, err)
		return
	}

	models, err := s.channelModels(r.Context(), channel, []sqlc.ChannelModel{row})
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, models[0])
}

func (s *Server) UpdateChannelModel(
	w http.ResponseWriter, r *http.Request, channelId openapi_types.UUID, modelId string,
) {
	var body UpdateChannelModelJSONRequestBody
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	if body.Enabled == nil {
		httpx.Fail(w, r, httpx.NewError(http.StatusBadRequest, "nothing_to_update", "no changes were given"))
		return
	}

	channel, err := s.q.GetChannel(r.Context(), channelId)
	if err != nil {
		httpx.Fail(w, r, dbErr(err))
		return
	}

	// The channel id is part of the statement rather than a check around it, so
	// a model id from another channel is simply not there.
	row, err := s.q.SetChannelModelEnabled(r.Context(), sqlc.SetChannelModelEnabledParams{
		ChannelID: channelId,
		ModelID:   modelId,
		Enabled:   *body.Enabled,
	})
	if err != nil {
		httpx.Fail(w, r, dbErr(err))
		return
	}

	models, err := s.channelModels(r.Context(), channel, []sqlc.ChannelModel{row})
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, models[0])
}

func (s *Server) DeleteChannelModel(
	w http.ResponseWriter, r *http.Request, channelId openapi_types.UUID, modelId string,
) {
	affected, err := s.q.DeleteManualChannelModel(r.Context(), sqlc.DeleteManualChannelModelParams{
		ChannelID: channelId,
		ModelID:   modelId,
	})
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	if affected > 0 {
		httpx.NoContent(w)
		return
	}

	// Nothing was deleted for one of two reasons, and they are worth telling
	// apart: a fetched row exists but is not the operator's to remove, and
	// hiding that behind a 404 would read as the button doing nothing.
	if _, err := s.q.GetChannelModel(r.Context(), sqlc.GetChannelModelParams{
		ChannelID: channelId,
		ModelID:   modelId,
	}); err == nil {
		httpx.Fail(w, r, errModelNotManual)
		return
	}
	httpx.Fail(w, r, httpx.ErrNotFound)
}

// RefreshChannelModels asks the endpoint what it serves and stores the answer.
//
// This is the first outbound call the server makes on an operator's behalf, so
// the order here matters: everything that can be decided locally is decided
// before anything leaves the machine, and everything that comes back is treated
// as somebody else's data.
func (s *Server) RefreshChannelModels(w http.ResponseWriter, r *http.Request, channelId openapi_types.UUID) {
	channel, err := s.q.GetChannel(r.Context(), channelId)
	if err != nil {
		httpx.Fail(w, r, dbErr(err))
		return
	}

	// Before the request, not after it fails: a channel with no key has nothing
	// to authenticate with, and sending an unauthenticated request would tell a
	// third party that this installation exists for no benefit at all.
	if strings.TrimSpace(channel.ApiKey) == "" {
		httpx.Fail(w, r, errChannelKeyMissing)
		return
	}

	// The protocol comes from the type, never from a column: storing both would
	// let them disagree, and a channel speaking the wrong protocol to a real
	// endpoint is the failure that follows.
	wireApi, ok := wireApiFor(ChannelType(channel.Type))
	if !ok {
		httpx.Fail(w, r, httpx.NewError(http.StatusInternalServerError, "channel_type_unknown", "unknown channel type"))
		return
	}

	lister, err := modellist.For(modellist.Kind(wireApi), modellist.Config{
		BaseURL: channel.BaseUrl,
		APIKey:  channel.ApiKey,
	})
	if err != nil {
		s.logChannelFailure(r, channel, err)
		httpx.Fail(w, r, refreshError(err))
		return
	}

	models, err := lister.List(r.Context())
	if err != nil {
		s.logChannelFailure(r, channel, err)
		httpx.Fail(w, r, refreshError(err))
		return
	}

	// An endpoint that lists nothing is far likelier to be misconfigured than
	// to be serving no models, and storing that answer would prune every
	// fetched row — taking the operator's enabled set with it, irreversibly.
	// Refusing costs a retry; believing it costs the configuration.
	if len(models) == 0 {
		httpx.Fail(w, r, errChannelListedNothing)
		return
	}

	result, err := s.storeChannelModels(r.Context(), channelId, models)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, result)
}

// storeChannelModels writes one fetched list as a single transaction, so a
// reader sees the list before or after the refresh and never the gap where the
// new rows are in and the stale ones are not yet out.
func (s *Server) storeChannelModels(
	ctx context.Context, channelID uuid.UUID, models []modellist.Model,
) (ChannelModelRefresh, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return ChannelModelRefresh{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	q := s.q.WithTx(tx)

	// Read first, inside the transaction, so "added" counts rows that were not
	// there when this refresh started rather than rows a concurrent one wrote.
	existing, err := q.ListChannelModels(ctx, channelID)
	if err != nil {
		return ChannelModelRefresh{}, err
	}
	known := make(map[string]struct{}, len(existing))
	for _, row := range existing {
		known[row.ModelID] = struct{}{}
	}

	ids := make([]string, len(models))
	names := make([]string, len(models))
	added := 0
	for i, model := range models {
		ids[i], names[i] = model.ID, model.DisplayName
		if _, ok := known[model.ID]; !ok {
			added++
		}
	}

	if err := q.UpsertFetchedChannelModels(ctx, sqlc.UpsertFetchedChannelModelsParams{
		ChannelID:    channelID,
		ModelIds:     ids,
		DisplayNames: names,
	}); err != nil {
		return ChannelModelRefresh{}, err
	}

	removed, err := q.DeleteStaleChannelModels(ctx, sqlc.DeleteStaleChannelModelsParams{
		ChannelID: channelID,
		ModelIds:  ids,
	})
	if err != nil {
		return ChannelModelRefresh{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return ChannelModelRefresh{}, err
	}

	// Total is what the endpoint listed, which is the number the operator can
	// check against their provider's console. It is not the row count: manual
	// rows the endpoint does not mention are still offered by this channel.
	return ChannelModelRefresh{Total: len(models), Added: added, Removed: int(removed)}, nil
}

// channelModels maps rows to the contract's shape, decorating each with what
// the catalogue knows about it.
func (s *Server) channelModels(
	ctx context.Context, channel sqlc.Channel, rows []sqlc.ChannelModel,
) ([]ChannelModel, error) {
	decorations, err := s.catalogForChannel(ctx, channel, rows)
	if err != nil {
		return nil, err
	}

	models := make([]ChannelModel, 0, len(rows))
	for _, row := range rows {
		models = append(models, toChannelModel(row, decorations[row.ModelID]))
	}
	return models, nil
}

// catalogForChannel looks up the catalogue entries for one channel's models in
// a single read.
//
// A model the catalogue has never heard of gets nothing back, and that is
// ordinary: the endpoint is the authority on which models exist, and models.dev
// only decorates them.
func (s *Server) catalogForChannel(
	ctx context.Context, channel sqlc.Channel, rows []sqlc.ChannelModel,
) (map[string]*CatalogModel, error) {
	if len(rows) == 0 {
		return nil, nil
	}
	wireApi, _ := wireApiFor(ChannelType(channel.Type))

	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ModelID)
	}

	entries, err := s.q.ListCatalogModelsForChannel(ctx, sqlc.ListCatalogModelsForChannelParams{
		WireApi:  string(wireApi),
		ModelIds: ids,
	})
	if err != nil {
		return nil, err
	}

	decorations := make(map[string]*CatalogModel, len(entries))
	for _, entry := range entries {
		// Both queries select the same columns, so one mapper serves both. If
		// they ever diverge this stops compiling, which is when you want to
		// hear about it.
		model := toCatalogModel(sqlc.ListCatalogModelsRow(entry))
		decorations[entry.ModelID] = &model
	}
	return decorations, nil
}

func toChannelModel(row sqlc.ChannelModel, catalog *CatalogModel) ChannelModel {
	model := ChannelModel{
		ModelId: row.ModelID,
		// The column holds what the endpoint called it, which is often nothing.
		// The id is the fallback here rather than at write time, so a later
		// refresh that does supply a name replaces a blank, not a guess.
		DisplayName: row.DisplayName,
		Enabled:     row.Enabled,
		Source:      ChannelModelSource(row.Source),
		Catalog:     catalog,
	}
	if model.DisplayName == "" {
		model.DisplayName = row.ModelID
	}
	if row.FetchedAt.Valid {
		fetchedAt := row.FetchedAt.Time
		model.FetchedAt = &fetchedAt
	}
	return model
}

// refreshError translates a listing failure into something the panel can show.
//
// Deliberately lossy: the operator is told which kind of failure it was and,
// for an upstream status, nothing more than that. The endpoint's own words are
// unlocalised, unvetted, and written by whoever is actually answering at that
// address, so they stay in the log.
func refreshError(err error) error {
	var statusErr *modellist.StatusError
	switch {
	case errors.Is(err, modellist.ErrNoKey):
		return errChannelKeyMissing
	case errors.Is(err, modellist.ErrBaseURL):
		return errChannelBaseURL
	case errors.Is(err, modellist.ErrUnknownKind):
		return errChannelKindUnsupported
	case errors.Is(err, modellist.ErrRedirect):
		return errChannelRedirected
	case errors.Is(err, modellist.ErrResponse), errors.Is(err, modellist.ErrTooManyPages):
		return errChannelUnreadable
	case errors.As(err, &statusErr):
		if statusErr.Unauthorized() {
			return errChannelKeyRejected
		}
		// The status is the one detail from upstream worth passing on: it is a
		// number, it says what to do next, and it cannot contain a credential.
		return httpx.NewError(http.StatusBadGateway, "channel_upstream_error",
			"the endpoint answered "+statusText(statusErr.StatusCode))
	default:
		// A dial failure, a TLS failure, a deadline. The message is already
		// redacted by modellist, but it still names hosts and timings that the
		// log is the place for.
		return errChannelUnreachable
	}
}

func statusText(code int) string {
	if text := http.StatusText(code); text != "" {
		return strings.ToLower(text)
	}
	return "an unexpected status"
}

// logChannelFailure records what the operator is not told.
//
// The channel is named by id and type, never by key: modellist builds its
// errors to be safe to print, and this is the one place that prints them.
func (s *Server) logChannelFailure(r *http.Request, channel sqlc.Channel, err error) {
	slog.WarnContext(r.Context(), "channel model refresh",
		"error", err,
		"channel_id", channel.ID,
		"type", string(channel.Type),
	)
}
