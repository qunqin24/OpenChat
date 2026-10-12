package api

import (
	"errors"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/qunqin/openchat/apps/server/internal/agent"
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
	used, estimated, err := s.afterCompaction(r, conversationId, row)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	out := ContextUsage{UsedTokens: &used, Model: &row.ModelID}
	if estimated {
		out.Estimated = &estimated
	}
	httpx.JSON(w, http.StatusOK, out)
}

// afterCompaction is the figure to show once the conversation has been
// compressed since its latest request: that request's count still holds what
// the summary has replaced, and the next one - which will report the real
// figure - may be a while off. So it is estimated: the request's fixed part
// (system prompt, tools, attachments - what the provider counted beyond the
// transcript), plus the context as it now stands (the summary, the kept
// question, the rows after it). Never more than the count it replaces. With no
// newer summary it is the provider's count as it is.
func (s *Server) afterCompaction(r *http.Request, conversationID uuid.UUID, latest sqlc.LatestContextTokensRow) (int64, bool, error) {
	ctx := r.Context()
	summary, rows, err := agent.LoadContextRows(ctx, s.q, conversationID, contextHistoryLimit)
	if err != nil {
		return 0, false, err
	}
	if summary == nil || !summary.CreatedAt.After(latest.CreatedAt.Time) {
		return latest.Tokens, false, nil
	}
	// What the latest request carried of the transcript, roughly: every row
	// written by the time it was logged. (Under an earlier summary it carried
	// less; the fixed part then comes out smaller, never below nothing.)
	all, err := s.q.ListMessagesAfter(ctx, sqlc.ListMessagesAfterParams{ConversationID: conversationID, RowLimit: contextHistoryLimit})
	if err != nil {
		return 0, false, err
	}
	carried := 0
	for _, row := range all {
		if !row.CreatedAt.Time.After(latest.CreatedAt.Time) {
			carried += agent.EstimateRow(row)
		}
	}
	now := agent.EstimateText(summary.Content)
	for _, row := range rows {
		now += agent.EstimateRow(row)
	}
	fixed := max(latest.Tokens-int64(carried), 0)
	return min(fixed+int64(now), latest.Tokens), true, nil
}

// contextHistoryLimit bounds the rows read for the estimate, as a run's own
// HistoryLimit bounds a request.
const contextHistoryLimit = 2000
