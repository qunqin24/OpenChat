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

	filter := modelFilter{
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

	// "Our models" means the ones a configured channel actually serves, and
	// that is the only list this endpoint has ever had to give. Browsing the
	// whole upstream catalogue was a second scope here once; it answered a
	// question the channel form answers better, so the catalogue now reaches
	// this page only as decoration on the rows below.
	rows, total, err := s.modelPage(r.Context(), filter)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}

	// One lookup for the page rather than a join, which would multiply a model
	// by its channel count and break both paging and total.
	byModel, err := s.channelsByModel(r.Context(), rows)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}

	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ModelID)
	}
	overrides, err := s.overridesFor(r.Context(), ids)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}

	// The operator's view of each cap: what is spent against it, whoever it
	// applies to.
	spend, err := s.capacity().states(r.Context(), capacityRules(overrides))
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}

	items := make([]CatalogModel, 0, len(rows))
	for _, row := range rows {
		model := toCatalogModel(row)
		var override *sqlc.ModelOverride
		if o, ok := overrides[row.ModelID]; ok {
			override = &o
		}
		withSettings(&model, row, override)
		if state, ok := spend[row.ModelID]; ok {
			capacity := toModelCapacity(state)
			model.Capacity = &capacity
		}
		// Always a list, never absent: "no channel serves this" is an answer,
		// and a missing field would read as "not asked".
		channels := byModel[row.ModelID]
		if channels == nil {
			channels = []ModelChannel{}
		}
		model.Channels = &channels
		items = append(items, model)
	}

	httpx.JSON(w, http.StatusOK, CatalogModelPage{
		Items: items,
		Total: total,
		Page:  int(page),
		Limit: int(limit),
	})
}

// modelFilter is what a listing and its count ask in common; see modelPage.
type modelFilter struct {
	Search                  *string
	ProviderID              *string
	RequireReasoning        bool
	RequireToolCall         bool
	RequireAttachment       bool
	RequireStructuredOutput bool
	Enabled                 *bool
	RowLimit                int32
	RowOffset               int32
}

// modelPage reads one page of served models and its total.
//
// It exists so that the filter is copied into the two generated params structs
// in one place. They are two types with the same fields, and spelling the copy
// out at each call site is how a list and its count start disagreeing about
// what a filter means — which surfaces as paging that lands on a page the list
// cannot fill.
func (s *Server) modelPage(
	ctx context.Context, f modelFilter,
) ([]sqlc.ListServedModelsRow, int64, error) {
	total, err := s.q.CountServedModels(ctx, sqlc.CountServedModelsParams{
		Search:                  f.Search,
		ProviderID:              f.ProviderID,
		RequireReasoning:        f.RequireReasoning,
		RequireToolCall:         f.RequireToolCall,
		RequireAttachment:       f.RequireAttachment,
		RequireStructuredOutput: f.RequireStructuredOutput,
		Enabled:                 f.Enabled,
	})
	if err != nil {
		return nil, 0, err
	}
	rows, err := s.q.ListServedModels(ctx, sqlc.ListServedModelsParams{
		Search:                  f.Search,
		ProviderID:              f.ProviderID,
		RequireReasoning:        f.RequireReasoning,
		RequireToolCall:         f.RequireToolCall,
		RequireAttachment:       f.RequireAttachment,
		RequireStructuredOutput: f.RequireStructuredOutput,
		Enabled:                 f.Enabled,
		RowLimit:                f.RowLimit,
		RowOffset:               f.RowOffset,
	})
	if err != nil {
		return nil, 0, err
	}
	return rows, total, nil
}

// channelsByModel names, for each model on this page, the channels that serve
// it — asked of the channels' own model lists, which is the only thing that
// knows. Every model on the page has at least one, because being served is what
// put it there; the lookup is still what names them, and a channel that has
// since gone is simply absent from the answer.
func (s *Server) channelsByModel(
	ctx context.Context, rows []sqlc.ListServedModelsRow,
) (map[string][]ModelChannel, error) {
	seen := make(map[string]struct{}, len(rows))
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		if _, ok := seen[row.ModelID]; ok {
			continue
		}
		seen[row.ModelID] = struct{}{}
		ids = append(ids, row.ModelID)
	}
	if len(ids) == 0 {
		return nil, nil
	}

	channels, err := s.q.ListChannelsServingModels(ctx, ids)
	if err != nil {
		return nil, err
	}

	byModel := make(map[string][]ModelChannel, len(ids))
	for _, c := range channels {
		byModel[c.ModelID] = append(byModel[c.ModelID], ModelChannel{
			Id:      c.ID,
			Name:    c.Name,
			Enabled: c.Enabled,
		})
	}
	return byModel, nil
}

func toCatalogModel(row sqlc.ListServedModelsRow) CatalogModel {
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
	// absent rather than as a confident zero. A model the catalogue has never
	// heard of arrives with every one of these at zero, and the card is expected
	// to show a model with no figures rather than a model that costs nothing.
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
