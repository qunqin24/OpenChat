package api

import (
	"errors"
	"net/http"

	"github.com/jackc/pgx/v5"

	"github.com/qunqin/openchat/apps/server/internal/auth"
	"github.com/qunqin/openchat/apps/server/internal/db/sqlc"
	"github.com/qunqin/openchat/apps/server/internal/httpx"
)

// GetConversationContext reports how much of the model's context window the
// conversation fills, for the composer's meter. The figure is what the latest
// answered request actually took, as the provider counted it (usage_logs),
// rather than an estimate from the transcript: it includes the system prompt,
// tool definitions and attachments that no client-side count could see.
func (s *Server) GetConversationContext(w http.ResponseWriter, r *http.Request, conversationId ConversationId) {
	ctx := r.Context()
	user := auth.MustUser(ctx)
	if _, err := s.q.GetConversation(ctx, sqlc.GetConversationParams{ID: conversationId, UserID: &user.ID}); err != nil {
		httpx.Fail(w, r, dbErr(err))
		return
	}
	row, err := s.q.LatestContextTokens(ctx, &conversationId)
	if errors.Is(err, pgx.ErrNoRows) {
		httpx.JSON(w, http.StatusOK, ContextUsage{})
		return
	}
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, ContextUsage{UsedTokens: &row.Tokens, Model: &row.ModelID})
}
