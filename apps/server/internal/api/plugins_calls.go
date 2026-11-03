package api

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/jackc/pgx/v5"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/qunqin24/openchat/apps/server/internal/auth"
	"github.com/qunqin24/openchat/apps/server/internal/db/sqlc"
	"github.com/qunqin24/openchat/apps/server/internal/httpx"
)

// ListPluginCalls is the administrators' record of plugin calls (plugin_calls):
// never what went in or came out.
func (s *Server) ListPluginCalls(w http.ResponseWriter, r *http.Request, params ListPluginCallsParams) {
	limit := clampLimit(params.Limit)
	var before *int64
	if params.Cursor != nil && *params.Cursor != "" {
		id, err := strconv.ParseInt(*params.Cursor, 10, 64)
		if err != nil {
			httpx.Fail(w, r, httpx.NewError(http.StatusBadRequest, "invalid_cursor", "the cursor is not one this list gave"))
			return
		}
		before = &id
	}
	rows, err := s.q.ListPluginCalls(r.Context(), sqlc.ListPluginCallsParams{
		Before: before, PluginID: params.PluginId, FailedOnly: params.Failed != nil && *params.Failed,
		// One more than the page, to learn whether another follows.
		RowLimit: limit + 1,
	})
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	out := PluginCallLogPage{Items: make([]PluginCallLog, 0, len(rows)), Limit: int(limit)}
	if int32(len(rows)) > limit {
		rows = rows[:limit]
		cursor := strconv.FormatInt(rows[limit-1].ID, 10)
		out.NextCursor = &cursor
	}
	for _, row := range rows {
		out.Items = append(out.Items, PluginCallLog{
			Id: row.ID, CreatedAt: row.CreatedAt.Time, UserId: row.UserID, UserName: row.UserName, UserEmail: row.UserEmail,
			ConversationId: row.ConversationID, PluginId: row.PluginID, Plugin: row.PluginSlug, PluginName: row.PluginName,
			Tool: row.Tool, Ok: row.Ok, Outcome: row.Outcome, Confirmed: row.Confirmed, DurationMs: int(row.DurationMs),
		})
	}
	httpx.JSON(w, http.StatusOK, out)
}

// GetToolImage serves a picture a tool answered with, to its conversation's owner.
func (s *Server) GetToolImage(w http.ResponseWriter, r *http.Request, conversationId openapi_types.UUID, imageId openapi_types.UUID) {
	row, err := s.q.GetToolImage(r.Context(), sqlc.GetToolImageParams{ID: imageId, UserID: auth.MustUser(r.Context()).ID})
	if errors.Is(err, pgx.ErrNoRows) {
		httpx.Fail(w, r, httpx.NewError(http.StatusNotFound, "not_found", "no such image"))
		return
	}
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	w.Header().Set("Content-Type", row.MediaType)
	w.Header().Set("Cache-Control", "private, max-age=86400, immutable")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write(row.Data)
}
