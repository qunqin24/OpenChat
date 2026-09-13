package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/qunqin/openchat/apps/server/internal/catalog"
	"github.com/qunqin/openchat/apps/server/internal/db/sqlc"
	"github.com/qunqin/openchat/apps/server/internal/httpx"
)

// catalogSyncer builds the syncer for one call. It carries nothing but the pool
// and an HTTP client shared across the package, so building one per call costs
// an allocation and spares Server a field that only these handlers would read.
func (s *Server) catalogSyncer() *catalog.Syncer {
	return catalog.NewSyncer(catalog.NewClient(), catalog.NewStore(s.pool))
}

// StartCatalogRefresh brings the snapshot up to date in the background when it
// is missing or older than catalog.StaleAfter.
//
// It returns immediately and reports nothing to its caller. models.dev is
// somebody else's server, and a boot that waited on it would be a boot a third
// party could hold up; a failure is logged, recorded, and retryable from the
// admin panel.
func (s *Server) StartCatalogRefresh(ctx context.Context) {
	go func() {
		if _, err := s.catalogSyncer().RefreshIfStale(ctx); err != nil {
			slog.WarnContext(ctx, "catalog refresh on boot", "error", err)
		}
	}()
}

func (s *Server) GetCatalogStatus(w http.ResponseWriter, r *http.Request) {
	state, err := catalog.NewStore(s.pool).LoadState(r.Context())
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}

	status, err := s.catalogStatus(r.Context(), state)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, status)
}

func (s *Server) RefreshCatalog(w http.ResponseWriter, r *http.Request) {
	state, err := s.catalogSyncer().Refresh(r.Context())
	if err != nil {
		logWarn(r, "catalog refresh", err)
		// The snapshot being served is untouched either way, and GET
		// /admin/catalog will now explain why. This call still failed, though,
		// and answering 200 would leave the panel showing a success it did not
		// get.
		if !errors.Is(err, catalog.ErrUpstream) {
			httpx.Fail(w, r, err)
			return
		}
		httpx.Fail(w, r, httpx.NewError(http.StatusBadGateway, "catalog_unavailable",
			"could not reach models.dev; the previous catalogue is still being served"))
		return
	}

	status, err := s.catalogStatus(r.Context(), state)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, status)
}

// catalogStatus pairs the row counts with the refresh bookkeeping. The counts
// come from the tables rather than from the state, so a snapshot someone
// emptied by hand reports as empty rather than as whatever the last successful
// fetch wrote.
func (s *Server) catalogStatus(ctx context.Context, state catalog.State) (CatalogStatus, error) {
	providers, err := s.q.CountCatalogProviders(ctx)
	if err != nil {
		return CatalogStatus{}, err
	}
	models, err := s.q.CountCatalogModels(ctx)
	if err != nil {
		return CatalogStatus{}, err
	}

	status := CatalogStatus{
		ProviderCount: int(providers),
		ModelCount:    int(models),
		Stale:         state.Stale(time.Now()),
	}
	if !state.FetchedAt.IsZero() {
		fetchedAt := state.FetchedAt
		status.FetchedAt = &fetchedAt
	}
	if state.LastError != "" {
		lastError := state.LastError
		status.LastError = &lastError
	}
	return status, nil
}

func (s *Server) ListCatalogProviders(w http.ResponseWriter, r *http.Request) {
	rows, err := s.q.ListCatalogProviders(r.Context())
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}

	providers := make([]CatalogProvider, 0, len(rows))
	for _, row := range rows {
		provider := CatalogProvider{
			Id:         row.ID,
			Name:       row.Name,
			ModelCount: int(row.ModelCount),
		}
		if row.Doc != "" {
			doc := row.Doc
			provider.Doc = &doc
		}
		if len(row.Env) > 0 {
			env := row.Env
			provider.Env = &env
		}
		providers = append(providers, provider)
	}
	httpx.JSON(w, http.StatusOK, providers)
}

func (s *Server) ListCatalogModels(w http.ResponseWriter, r *http.Request, params ListCatalogModelsParams) {
	limit := clampLimit(params.Limit)

	page := int32(1)
	if params.Page != nil && *params.Page > 1 {
		page = int32(*params.Page)
	}

	var search *string
	if params.Search != nil {
		if trimmed := strings.TrimSpace(*params.Search); trimmed != "" {
			search = &trimmed
		}
	}

	var providerID *string
	if params.ProviderId != nil {
		if trimmed := strings.TrimSpace(*params.ProviderId); trimmed != "" {
			providerID = &trimmed
		}
	}

	// Each capability narrows the result, so two of them mean models that have
	// both rather than either.
	wanted := make(map[ListCatalogModelsParamsCapability]bool)
	if params.Capability != nil {
		for _, capability := range *params.Capability {
			if !capability.Valid() {
				httpx.Fail(w, r, httpx.NewError(http.StatusBadRequest, "invalid_capability",
					"unknown capability: "+string(capability)))
				return
			}
			wanted[capability] = true
		}
	}

	filter := sqlc.ListCatalogModelsParams{
		Search:                  search,
		ProviderID:              providerID,
		RequireReasoning:        wanted[ListCatalogModelsParamsCapabilityReasoning],
		RequireToolCall:         wanted[ListCatalogModelsParamsCapabilityToolCall],
		RequireAttachment:       wanted[ListCatalogModelsParamsCapabilityAttachment],
		RequireStructuredOutput: wanted[ListCatalogModelsParamsCapabilityStructuredOutput],
		Enabled:                 params.Enabled,
		RowLimit:                limit,
		RowOffset:               (page - 1) * limit,
	}

	total, err := s.q.CountCatalogModelsFiltered(r.Context(), sqlc.CountCatalogModelsFilteredParams{
		Search:                  filter.Search,
		ProviderID:              filter.ProviderID,
		RequireReasoning:        filter.RequireReasoning,
		RequireToolCall:         filter.RequireToolCall,
		RequireAttachment:       filter.RequireAttachment,
		RequireStructuredOutput: filter.RequireStructuredOutput,
		Enabled:                 filter.Enabled,
	})
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}

	rows, err := s.q.ListCatalogModels(r.Context(), filter)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}

	items := make([]CatalogModel, 0, len(rows))
	for _, row := range rows {
		items = append(items, toCatalogModel(row))
	}

	httpx.JSON(w, http.StatusOK, CatalogModelPage{
		Items: items,
		Total: total,
		Page:  int(page),
		Limit: int(limit),
	})
}

func (s *Server) UpdateCatalogModel(w http.ResponseWriter, r *http.Request, providerId string, modelId string) {
	var body UpdateCatalogModelJSONRequestBody
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	if body.Enabled == nil {
		httpx.Fail(w, r, httpx.NewError(http.StatusBadRequest, "nothing_to_update", "no changes were given"))
		return
	}

	affected, err := s.q.SetModelEnabled(r.Context(), sqlc.SetModelEnabledParams{
		ProviderID: providerId,
		ModelID:    modelId,
		Enabled:    *body.Enabled,
	})
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	// The statement writes nothing when the catalogue has no such model, which
	// is the existence check: a setting for a model nobody offers would be a row
	// nothing reads.
	if affected == 0 {
		httpx.Fail(w, r, httpx.ErrNotFound)
		return
	}

	row, err := s.q.GetCatalogModel(r.Context(), sqlc.GetCatalogModelParams{
		ProviderID: providerId,
		ModelID:    modelId,
	})
	if err != nil {
		httpx.Fail(w, r, dbErr(err))
		return
	}
	// Both queries select the same columns, so one mapper serves both rather
	// than two that have to be kept in step. If they ever diverge this stops
	// compiling, which is when you want to hear about it.
	httpx.JSON(w, http.StatusOK, toCatalogModel(sqlc.ListCatalogModelsRow(row)))
}

func toCatalogModel(row sqlc.ListCatalogModelsRow) CatalogModel {
	model := CatalogModel{
		ProviderId:       row.ProviderID,
		ProviderName:     row.ProviderName,
		ModelId:          row.ModelID,
		Name:             row.Name,
		Attachment:       row.Attachment,
		Reasoning:        row.Reasoning,
		ToolCall:         row.ToolCall,
		StructuredOutput: row.StructuredOutput,
		Enabled:          row.Enabled,
	}
	if row.Description != "" {
		description := row.Description
		model.Description = &description
	}
	if row.Family != "" {
		family := row.Family
		model.Family = &family
	}
	if row.Knowledge != "" {
		knowledge := row.Knowledge
		model.Knowledge = &knowledge
	}
	if row.ReleaseDate != "" {
		releaseDate := row.ReleaseDate
		model.ReleaseDate = &releaseDate
	}
	if len(row.InputModalities) > 0 {
		input := row.InputModalities
		model.InputModalities = &input
	}
	if len(row.OutputModalities) > 0 {
		output := row.OutputModalities
		model.OutputModalities = &output
	}
	// Zero in these columns means upstream stated nothing, which is far commoner
	// than a model that is genuinely free or unlimited, so it is reported as
	// absent rather than as a confident zero.
	if row.ContextLimit > 0 {
		contextLimit := row.ContextLimit
		model.ContextLimit = &contextLimit
	}
	if row.OutputLimit > 0 {
		outputLimit := row.OutputLimit
		model.OutputLimit = &outputLimit
	}
	if row.InputCost > 0 {
		inputCost := float64(row.InputCost)
		model.InputCost = &inputCost
	}
	if row.OutputCost > 0 {
		outputCost := float64(row.OutputCost)
		model.OutputCost = &outputCost
	}
	return model
}
