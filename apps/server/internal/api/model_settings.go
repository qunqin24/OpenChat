package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"

	"github.com/jackc/pgx/v5"

	"github.com/qunqin/openchat/apps/server/internal/auth"
	"github.com/qunqin/openchat/apps/server/internal/catalog"
	"github.com/qunqin/openchat/apps/server/internal/db/sqlc"
	"github.com/qunqin/openchat/apps/server/internal/httpx"
	"github.com/qunqin/openchat/apps/server/internal/llm"
)

// A model's settings are the catalogue's figures with the operator's decisions
// laid over them (catalog.Resolve). Everything that offers or calls a model
// goes through the resolved settings — the picker, the request check and the
// request itself — so a level or a tool an operator has withheld cannot be had
// by asking the API directly.

// UpdateModelSettings replaces the operator's settings for a model.
func (s *Server) UpdateModelSettings(w http.ResponseWriter, r *http.Request, modelId string) {
	var body ModelOverride
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httpx.Fail(w, r, httpx.NewError(http.StatusBadRequest, "invalid_body", "the body is not a model override"))
		return
	}
	params, err := overrideParams(modelId, body)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}

	// Nothing left to decide is the same as never having decided: no row, so
	// the model follows the catalogue the way an untouched one does.
	if params.Kind == nil && params.Reasoning == nil && params.ToolCall == nil &&
		params.InputModalities == nil && params.ContextLimit == nil && params.InputLimit == nil &&
		params.OutputLimit == nil && params.ReasoningEfforts == nil && !params.AdminOnly {
		if err := s.q.DeleteModelOverride(r.Context(), modelId); err != nil {
			httpx.Fail(w, r, err)
			return
		}
		httpx.JSON(w, http.StatusOK, ModelOverride{})
		return
	}

	row, err := s.q.SaveModelOverride(r.Context(), params)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, toModelOverride(row))
}

// ResetModelSettings returns a model to the catalogue for every setting.
func (s *Server) ResetModelSettings(w http.ResponseWriter, r *http.Request, modelId string) {
	if err := s.q.DeleteModelOverride(r.Context(), modelId); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func overrideParams(modelID string, body ModelOverride) (sqlc.SaveModelOverrideParams, error) {
	invalid := func(code, message string) error {
		return httpx.NewError(http.StatusBadRequest, code, message)
	}
	params := sqlc.SaveModelOverrideParams{
		ModelID:      modelID,
		Reasoning:    body.Reasoning,
		ToolCall:     body.ToolCall,
		ContextLimit: body.ContextLimit,
		InputLimit:   body.InputLimit,
		OutputLimit:  body.OutputLimit,
		AdminOnly:    body.AdminOnly != nil && *body.AdminOnly,
	}
	if body.Kind != nil {
		if !body.Kind.Valid() {
			return params, invalid("invalid_kind", "unknown model kind: "+string(*body.Kind))
		}
		kind := string(*body.Kind)
		params.Kind = &kind
	}
	for _, limit := range []*int64{body.ContextLimit, body.InputLimit, body.OutputLimit} {
		if limit != nil && *limit <= 0 {
			return params, invalid("invalid_limit", "a token limit must be positive")
		}
	}
	if body.InputModalities != nil {
		params.InputModalities = []string{}
		for _, modality := range *body.InputModalities {
			if !modality.Valid() {
				return params, invalid("invalid_modality", "unknown input modality: "+string(modality))
			}
			if !slices.Contains(params.InputModalities, string(modality)) {
				params.InputModalities = append(params.InputModalities, string(modality))
			}
		}
	}
	if body.ReasoningEfforts != nil {
		params.ReasoningEfforts = []string{}
		for _, effort := range *body.ReasoningEfforts {
			// `auto` sends nothing and is always available, so it is not
			// something an operator can allow or withhold.
			if !effort.Valid() || effort == ReasoningEffortAuto {
				return params, invalid("invalid_effort", "not a thinking level: "+string(effort))
			}
			if !slices.Contains(params.ReasoningEfforts, string(effort)) {
				params.ReasoningEfforts = append(params.ReasoningEfforts, string(effort))
			}
		}
	}
	return params, nil
}

func toModelOverride(row sqlc.ModelOverride) ModelOverride {
	out := ModelOverride{
		Reasoning:    row.Reasoning,
		ToolCall:     row.ToolCall,
		ContextLimit: row.ContextLimit,
		InputLimit:   row.InputLimit,
		OutputLimit:  row.OutputLimit,
	}
	if row.AdminOnly {
		out.AdminOnly = &row.AdminOnly
	}
	if row.Kind != nil {
		kind := ModelKind(*row.Kind)
		out.Kind = &kind
	}
	if row.InputModalities != nil {
		modalities := make([]InputModality, 0, len(row.InputModalities))
		for _, m := range row.InputModalities {
			modalities = append(modalities, InputModality(m))
		}
		out.InputModalities = &modalities
	}
	if row.ReasoningEfforts != nil {
		efforts := make([]ReasoningEffort, 0, len(row.ReasoningEfforts))
		for _, e := range row.ReasoningEfforts {
			efforts = append(efforts, ReasoningEffort(e))
		}
		out.ReasoningEfforts = &efforts
	}
	return out
}

func fromOverrideRow(row sqlc.ModelOverride) catalog.Override {
	out := catalog.Override{
		Reasoning:       row.Reasoning,
		ToolCall:        row.ToolCall,
		InputModalities: row.InputModalities,
		ContextLimit:    row.ContextLimit,
		InputLimit:      row.InputLimit,
		OutputLimit:     row.OutputLimit,
		AdminOnly:       row.AdminOnly,
	}
	if row.Kind != nil {
		kind := catalog.Kind(*row.Kind)
		out.Kind = &kind
	}
	if row.ReasoningEfforts != nil {
		out.ReasoningEfforts = make([]llm.ReasoningEffort, 0, len(row.ReasoningEfforts))
		for _, e := range row.ReasoningEfforts {
			out.ReasoningEfforts = append(out.ReasoningEfforts, llm.ReasoningEffort(e))
		}
	}
	return out
}

// servedDefaults reads the catalogue's figures off a served-model row. An
// empty provider id is a model the catalogue does not list.
func servedDefaults(row sqlc.ListServedModelsRow) catalog.Defaults {
	if row.ProviderID == "" {
		return catalog.UnknownDefaults(row.ModelID)
	}
	return catalog.Defaults{
		Kind:            catalog.DefaultKind(row.ModelID, row.OutputModalities),
		Reasoning:       row.Reasoning,
		ToolCall:        row.ToolCall,
		InputModalities: row.InputModalities,
		ContextLimit:    row.ContextLimit,
		InputLimit:      row.InputLimit,
		OutputLimit:     row.OutputLimit,
		Profile:         catalog.ParseReasoningOptions(row.ReasoningOptions),
	}
}

// loadModelSettings resolves one model's settings, for a request about to be
// made with it.
func loadModelSettings(ctx context.Context, q *sqlc.Queries, modelID string) (catalog.Settings, error) {
	defaults := catalog.UnknownDefaults(modelID)
	profile, err := q.ModelProfile(ctx, modelID)
	switch {
	case err == nil:
		defaults = catalog.Defaults{
			Kind:            catalog.DefaultKind(modelID, profile.OutputModalities),
			Reasoning:       profile.Reasoning,
			ToolCall:        profile.ToolCall,
			InputModalities: profile.InputModalities,
			ContextLimit:    profile.ContextLimit,
			InputLimit:      profile.InputLimit,
			OutputLimit:     profile.OutputLimit,
			Profile:         catalog.ParseReasoningOptions(profile.ReasoningOptions),
		}
	case !errors.Is(err, pgx.ErrNoRows):
		return catalog.Settings{}, err
	}

	var override catalog.Override
	row, err := q.GetModelOverride(ctx, modelID)
	switch {
	case err == nil:
		override = fromOverrideRow(row)
	case !errors.Is(err, pgx.ErrNoRows):
		return catalog.Settings{}, err
	}
	return catalog.Resolve(defaults, override), nil
}

// overridesFor reads the operator's settings for a set of models at once.
func (s *Server) overridesFor(ctx context.Context, ids []string) (map[string]sqlc.ModelOverride, error) {
	out := make(map[string]sqlc.ModelOverride, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := s.q.ListModelOverrides(ctx, ids)
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		out[row.ModelID] = row
	}
	return out, nil
}

// toModelSettings reports settings; levels are the ones to list, which for
// the catalogue's own defaults are every level the model has.
func toModelSettings(settings catalog.Settings, levels []llm.ReasoningEffort) ModelSettings {
	modalities := make([]InputModality, 0, len(settings.InputModalities))
	for _, m := range settings.InputModalities {
		modalities = append(modalities, InputModality(m))
	}
	return ModelSettings{
		Kind:             ModelKind(settings.Kind),
		Reasoning:        settings.Reasoning,
		ToolCall:         settings.ToolCall,
		InputModalities:  modalities,
		ContextLimit:     settings.ContextLimit,
		InputLimit:       settings.InputLimit,
		OutputLimit:      settings.OutputLimit,
		ReasoningEfforts: toEfforts(levels),
		AdminOnly:        settings.AdminOnly,
	}
}

func toEfforts(levels []llm.ReasoningEffort) []ReasoningEffort {
	out := make([]ReasoningEffort, 0, len(levels))
	for _, level := range levels {
		out = append(out, ReasoningEffort(level))
	}
	return out
}

// withSettings adds the catalogue's defaults, the operator's override and the
// result of the two to a listed model.
func withSettings(model *CatalogModel, row sqlc.ListServedModelsRow, override *sqlc.ModelOverride) {
	defaults := servedDefaults(row)
	var o catalog.Override
	out := ModelOverride{}
	if override != nil {
		o = fromOverrideRow(*override)
		out = toModelOverride(*override)
	}
	settings := catalog.Resolve(defaults, o)
	d := toModelSettings(catalog.Resolve(defaults, catalog.Override{}), defaults.Profile.Supported())
	e := toModelSettings(settings, settings.Profile.Levels())
	model.Defaults = &d
	model.Settings = &e
	model.Override = &out
	if row.InputLimit > 0 {
		inputLimit := row.InputLimit
		model.InputLimit = &inputLimit
	}
}

// chatModels are what people may pick in chat: served by an enabled channel,
// and text models by their settings. staff includes the models kept for
// administrators, for an administrator or the server's own tasks.
func (s *Server) chatModels(ctx context.Context, staff bool) ([]ChatModel, error) {
	enabled := true
	rows, err := s.q.ListServedModels(ctx, sqlc.ListServedModelsParams{
		Enabled:   &enabled,
		RowLimit:  chatModelLimit,
		RowOffset: 0,
	})
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ModelID)
	}
	overrides, err := s.overridesFor(ctx, ids)
	if err != nil {
		return nil, err
	}

	out := make([]ChatModel, 0, len(rows))
	for _, row := range rows {
		var o catalog.Override
		if override, ok := overrides[row.ModelID]; ok {
			o = fromOverrideRow(override)
		}
		settings := catalog.Resolve(servedDefaults(row), o)
		if settings.Kind != catalog.KindText || (settings.AdminOnly && !staff) {
			continue
		}
		view := toModelSettings(settings, settings.Profile.Levels())
		out = append(out, ChatModel{
			Id:               row.ModelID,
			Name:             row.Name,
			ProviderId:       row.ProviderID,
			Family:           row.Family,
			Reasoning:        settings.Reasoning,
			ToolCall:         settings.ToolCall,
			Attachment:       row.Attachment,
			ContextLimit:     settings.ContextLimit,
			OutputLimit:      settings.OutputLimit,
			InputModalities:  view.InputModalities,
			ReasoningEfforts: view.ReasoningEfforts,
		})
	}
	return out, nil
}

// isStaff is whether the request is an administrator's, who may use the
// models kept from everyone else.
func isStaff(ctx context.Context) bool {
	user, ok := auth.UserFrom(ctx)
	return ok && user.Role == sqlc.UserRoleAdmin
}
