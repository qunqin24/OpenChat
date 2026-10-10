package api

import (
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/qunqin/openchat/apps/server/internal/auth"

	"github.com/qunqin/openchat/apps/server/internal/db/sqlc"
	"github.com/qunqin/openchat/apps/server/internal/httpx"
)

func (s *Server) ListConversations(w http.ResponseWriter, r *http.Request, params ListConversationsParams) {
	cursorAt, cursorID, err := decodeCursor(params.Cursor)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}

	user := auth.MustUser(r.Context())
	limit := clampLimit(params.Limit)
	if search := trimmed(params.Search); search != "" {
		s.searchConversations(w, r, user.ID, search, limit, cursorAt, cursorID)
		return
	}
	// Fetch one extra row to learn whether another page exists.
	rows, err := s.q.ListConversations(r.Context(), sqlc.ListConversationsParams{
		UserID:          &user.ID,
		CursorUpdatedAt: cursorAt,
		CursorID:        cursorID,
		RowLimit:        limit + 1,
	})
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}

	page := ConversationPage{Items: make([]Conversation, 0, limit)}
	if int32(len(rows)) > limit {
		last := rows[limit-1]
		next := encodeCursor(last.UpdatedAt, last.ID)
		page.NextCursor = &next
		rows = rows[:limit]
	}
	for _, row := range rows {
		page.Items = append(page.Items, toConversation(row))
	}
	httpx.JSON(w, http.StatusOK, page)
}

func (s *Server) CreateConversation(w http.ResponseWriter, r *http.Request) {
	var body CreateConversationRequest
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.Fail(w, r, err)
		return
	}

	title := defaultTitle
	if body.Title != nil && strings.TrimSpace(*body.Title) != "" {
		title = strings.TrimSpace(*body.Title)
	}

	var effort *string
	if body.ReasoningEffort != nil {
		var ok bool
		if effort, ok = effortColumn(*body.ReasoningEffort); !ok {
			httpx.Fail(w, r, errInvalidEffort)
			return
		}
	}

	user := auth.MustUser(r.Context())
	row, err := s.q.CreateConversation(r.Context(), sqlc.CreateConversationParams{
		UserID:    &user.ID,
		Title:     title,
		Model:     body.Model,
		Temporary: body.Temporary != nil && *body.Temporary,
	})
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	if effort != nil {
		if row, err = s.q.SetConversationReasoning(r.Context(), sqlc.SetConversationReasoningParams{
			ID: row.ID, UserID: &user.ID, ReasoningEffort: effort,
		}); err != nil {
			httpx.Fail(w, r, err)
			return
		}
	}
	if body.WebSearch != nil && *body.WebSearch != row.WebSearch {
		if row, err = s.q.SetConversationWebSearch(r.Context(), sqlc.SetConversationWebSearchParams{
			ID: row.ID, UserID: &user.ID, WebSearch: *body.WebSearch,
		}); err != nil {
			httpx.Fail(w, r, err)
			return
		}
	}
	httpx.JSON(w, http.StatusCreated, toConversation(row))
}

func (s *Server) GetConversation(w http.ResponseWriter, r *http.Request, conversationId ConversationId) {
	user := auth.MustUser(r.Context())
	row, err := s.q.GetConversation(r.Context(), sqlc.GetConversationParams{
		ID:     conversationId,
		UserID: &user.ID,
	})
	if err != nil {
		httpx.Fail(w, r, dbErr(err))
		return
	}
	httpx.JSON(w, http.StatusOK, toConversation(row))
}

func (s *Server) UpdateConversation(w http.ResponseWriter, r *http.Request, conversationId ConversationId) {
	var body UpdateConversationRequest
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	if body.Title == nil && body.Model == nil && body.ReasoningEffort == nil && body.WebSearch == nil {
		httpx.Fail(w, r, httpx.NewError(http.StatusBadRequest, "nothing_to_update",
			"give a title, a model, a reasoning effort or a web search choice"))
		return
	}
	var effort *string
	if body.ReasoningEffort != nil {
		var ok bool
		if effort, ok = effortColumn(*body.ReasoningEffort); !ok {
			httpx.Fail(w, r, errInvalidEffort)
			return
		}
	}
	if body.Title != nil && strings.TrimSpace(*body.Title) == "" {
		httpx.Fail(w, r, httpx.NewError(http.StatusBadRequest, "invalid_title", "title must not be empty"))
		return
	}
	if body.Model != nil && strings.TrimSpace(*body.Model) == "" {
		httpx.Fail(w, r, httpx.NewFieldError("model", "model_unavailable", "model must not be empty"))
		return
	}

	ctx := r.Context()
	user := auth.MustUser(ctx)
	row, err := s.q.GetConversation(ctx, sqlc.GetConversationParams{ID: conversationId, UserID: &user.ID})
	if err != nil {
		httpx.Fail(w, r, dbErr(err))
		return
	}
	if body.Title != nil {
		if row, err = s.q.UpdateConversationTitle(ctx, sqlc.UpdateConversationTitleParams{
			ID:     conversationId,
			UserID: &user.ID,
			Title:  strings.TrimSpace(*body.Title),
		}); err != nil {
			httpx.Fail(w, r, dbErr(err))
			return
		}
	}
	if body.Model != nil {
		// Not checked against what is served: the choice is remembered even
		// if its channel is briefly off, and the next run says if it still is.
		model := strings.TrimSpace(*body.Model)
		if row, err = s.q.SetConversationModel(ctx, sqlc.SetConversationModelParams{
			ID: conversationId, UserID: &user.ID, Model: &model,
		}); err != nil {
			httpx.Fail(w, r, dbErr(err))
			return
		}
	}
	if body.ReasoningEffort != nil {
		if row, err = s.q.SetConversationReasoning(ctx, sqlc.SetConversationReasoningParams{
			ID: conversationId, UserID: &user.ID, ReasoningEffort: effort,
		}); err != nil {
			httpx.Fail(w, r, dbErr(err))
			return
		}
	}
	if body.WebSearch != nil {
		if row, err = s.q.SetConversationWebSearch(ctx, sqlc.SetConversationWebSearchParams{
			ID: conversationId, UserID: &user.ID, WebSearch: *body.WebSearch,
		}); err != nil {
			httpx.Fail(w, r, dbErr(err))
			return
		}
	}
	httpx.JSON(w, http.StatusOK, toConversation(row))
}

func (s *Server) DeleteConversation(w http.ResponseWriter, r *http.Request, conversationId ConversationId) {
	user := auth.MustUser(r.Context())
	affected, err := s.q.SoftDeleteConversation(r.Context(), sqlc.SoftDeleteConversationParams{
		ID:     conversationId,
		UserID: &user.ID,
	})
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	if affected == 0 {
		httpx.Fail(w, r, httpx.ErrNotFound)
		return
	}
	httpx.NoContent(w)
}

// ListRunningConversations says which of the caller's conversations are being
// answered right now, for the sidebar to mark without following each one.
func (s *Server) ListRunningConversations(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())
	ids, err := s.q.ListRunningConversations(r.Context(), &user.ID)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	if ids == nil {
		ids = []uuid.UUID{}
	}
	httpx.JSON(w, http.StatusOK, RunningConversations{Ids: ids})
}
