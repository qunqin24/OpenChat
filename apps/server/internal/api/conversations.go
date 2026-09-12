package api

import (
	"net/http"
	"strings"

	"github.com/qunqin/openchat/apps/server/internal/db/sqlc"
	"github.com/qunqin/openchat/apps/server/internal/httpx"
)

func (s *Server) ListConversations(w http.ResponseWriter, r *http.Request, params ListConversationsParams) {
	cursorAt, cursorID, err := decodeCursor(params.Cursor)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}

	limit := clampLimit(params.Limit)
	// Fetch one extra row to learn whether another page exists.
	rows, err := s.q.ListConversations(r.Context(), sqlc.ListConversationsParams{
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

	title := "New chat"
	if body.Title != nil && strings.TrimSpace(*body.Title) != "" {
		title = strings.TrimSpace(*body.Title)
	}

	row, err := s.q.CreateConversation(r.Context(), sqlc.CreateConversationParams{
		Title: title,
		Model: body.Model,
	})
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, toConversation(row))
}

func (s *Server) GetConversation(w http.ResponseWriter, r *http.Request, conversationId ConversationId) {
	row, err := s.q.GetConversation(r.Context(), conversationId)
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
	if body.Title == nil || strings.TrimSpace(*body.Title) == "" {
		httpx.Fail(w, r, httpx.NewError(http.StatusBadRequest, "invalid_title", "title must not be empty"))
		return
	}

	row, err := s.q.UpdateConversationTitle(r.Context(), sqlc.UpdateConversationTitleParams{
		ID:    conversationId,
		Title: strings.TrimSpace(*body.Title),
	})
	if err != nil {
		httpx.Fail(w, r, dbErr(err))
		return
	}
	httpx.JSON(w, http.StatusOK, toConversation(row))
}

func (s *Server) DeleteConversation(w http.ResponseWriter, r *http.Request, conversationId ConversationId) {
	affected, err := s.q.SoftDeleteConversation(r.Context(), conversationId)
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
