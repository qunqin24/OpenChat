package api

import (
	"context"
	"errors"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/qunqin/openchat/apps/server/internal/agent"
	"github.com/qunqin/openchat/apps/server/internal/db/sqlc"
	"github.com/qunqin/openchat/apps/server/internal/httpx"
	"github.com/qunqin/openchat/apps/server/internal/llm"
)

// A message that moderation refused is kept in its conversation.
//
// What is kept is the text of a refused *send*: a `user` row of status
// `blocked` at the end of the transcript, which the page shows as the person's
// message with the refusal under it, after a refresh and on their other
// devices as it did when it was refused. Nothing else about it is kept: no
// files are claimed (they stay the composer's, as they did), no run is
// started, and the model never sees the row - every query that builds a
// request, a summary, a title, a search or an export leaves status `blocked`
// out, and agent.BuildContext skips it a second time (see the note in
// db/queries/messages.sql). It is the end of the transcript only until the next
// send, which appends after it.
//
// A refused edit or answer keeps nothing: the person's words go back to the
// editor they came from.

// keepRefused stores a refused send and returns the answer to give: the same
// refusal, with the stored row's id in `details.messageId` when the refusal is
// a content_blocked one (the sanction that a message can earn - the sign-out of
// a disabled account - is answered as it is: the message is stored all the
// same, for the day the account is enabled again).
//
// Failing to store it never changes the answer. The refusal is what the person
// is owed; the message missing from the transcript is a pity, and is logged.
func (s *Server) keepRefused(ctx context.Context, conversationID uuid.UUID, content string, refusal error) error {
	// The row outlives the request: a page closed at the moment of the refusal
	// still finds it on the next visit.
	ctx = context.WithoutCancel(ctx)
	row, err := s.storeRefused(ctx, conversationID, content)
	if err != nil {
		slog.WarnContext(ctx, "keep a refused message", "conversation_id", conversationID, "error", err)
		return refusal
	}
	if row == nil {
		return refusal
	}
	// Told after the commit, so a page that fetches finds the row.
	s.runner.Hub().Publish(conversationID, agent.Batch{Events: []agent.Event{agent.TranscriptChanged{}}})

	var api *httpx.APIError
	if errors.As(refusal, &api) && api.Code == "content_blocked" {
		if api.Details == nil {
			api.Details = map[string]any{}
		}
		api.Details["messageId"] = row.ID
	}
	return refusal
}

// storeRefused writes the blocked row at the end of the conversation, under
// its lock. It writes nothing - and returns nil, nil - when a run is writing
// the conversation's end right now: the row would land in the middle of the
// reply being made, and a page that sent while one was running is not one this
// is meant for (the composer is closed then). The conversation having been
// deleted meanwhile is the same.
func (s *Server) storeRefused(ctx context.Context, conversationID uuid.UUID, content string) (*sqlc.Message, error) {
	var row *sqlc.Message
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		if _, err := lockLive(ctx, q, conversationID); err != nil {
			return err
		}
		if err := requireIdle(ctx, q, conversationID); err != nil {
			return err
		}
		written, err := q.AppendEntry(ctx, sqlc.AppendEntryParams{
			ConversationID: conversationID,
			Role:           sqlc.MessageRoleUser,
			Content:        content,
			Parts:          agent.EncodeParts([]llm.Part{llm.TextPart{Text: content}}),
			Status:         sqlc.MessageStatusBlocked,
		})
		row = &written
		return err
	})
	switch {
	case errors.Is(err, errConversationBusy), errors.Is(err, httpx.ErrNotFound):
		return nil, nil
	case err != nil:
		return nil, err
	}
	return row, nil
}
