package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/qunqin24/openchat/apps/server/internal/agent"
	"github.com/qunqin24/openchat/apps/server/internal/db/sqlc"
	"github.com/qunqin24/openchat/apps/server/internal/httpx"
	"github.com/qunqin24/openchat/apps/server/internal/llm"
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
// db/queries/messages.sql).
//
// It is also where the conversation stops. While the last row is a refused
// message nothing is written after it - no send, no regeneration, no answer to
// a question (requireRevised) - or the model would seem to have answered words
// it never saw. The way on is to reword the refused message: the edit endpoint
// takes it like any question, and the new words, checked again, go at the end
// in its place. An edit of an earlier question deletes the refused message
// along with everything else after that question, which is another way on.
//
// A refused edit of an ordinary question, and a refused answer, keep nothing:
// the person's words go back to the editor they came from. A refused edit of
// the refused message itself replaces it with what was last tried
// (keepRefusedEdit), so the transcript shows the words the model was spared.

// errReviseRefused is the answer to a request that would go on past a refused
// message.
var errReviseRefused = httpx.NewError(http.StatusConflict, "revise_refused",
	"the last message was refused by content moderation; reword it before going on")

// requireRevised refuses a conversation whose last row is a refused message,
// with the conversation held (lockLive): what is seen here stays true until the
// caller commits. Run without the lock it is only an early answer, to spare a
// moderation call and a strike for a request that cannot be taken anyway.
func requireRevised(ctx context.Context, q *sqlc.Queries, conversationID uuid.UUID) error {
	last, err := q.GetLastMessage(ctx, conversationID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if last.Status == sqlc.MessageStatusBlocked {
		return errReviseRefused
	}
	return nil
}

// keepRefused stores a refused send and returns the answer to give: the same
// refusal, with the stored row's id in `details.messageId` when the refusal is
// a content_blocked one (the sanction that a message can earn - the sign-out of
// a disabled account - is answered as it is: the message is stored all the
// same, for the day the account is enabled again).
//
// Failing to store it never changes the answer. The refusal is what the person
// is owed; the message missing from the transcript is a pity, and is logged.
func (s *Server) keepRefused(ctx context.Context, conversationID uuid.UUID, content string, refusal error) error {
	return s.keep(ctx, conversationID, content, nil, refusal)
}

// keepRefusedEdit is keepRefused for the words that were meant to replace a
// refused message (the edit of a row of status `blocked`): the new words take
// its place at the end, so the transcript shows what was tried last, and the
// old row is gone. All in one transaction, or - when the refused message is no
// longer the last row, a run is writing, or it was deleted meanwhile - nothing
// at all: an edit that failed deletes nothing. Other tabs are told which row
// went (`transcript_changed` from it).
func (s *Server) keepRefusedEdit(ctx context.Context, conversationID uuid.UUID, replaced sqlc.Message, content string, refusal error) error {
	return s.keep(ctx, conversationID, content, &replaced, refusal)
}

func (s *Server) keep(ctx context.Context, conversationID uuid.UUID, content string, replaced *sqlc.Message, refusal error) error {
	// The row outlives the request: a page closed at the moment of the refusal
	// still finds it on the next visit.
	ctx = context.WithoutCancel(ctx)
	row, err := s.storeRefused(ctx, conversationID, content, replaced)
	if err != nil {
		slog.WarnContext(ctx, "keep a refused message", "conversation_id", conversationID, "error", err)
		return refusal
	}
	if row == nil {
		return refusal
	}
	// Told after the commit, so a page that fetches finds the row.
	changed := agent.TranscriptChanged{}
	if replaced != nil {
		changed.From = replaced.ID
	}
	s.runner.Hub().Publish(conversationID, agent.Batch{Events: []agent.Event{changed}})

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
// deleted meanwhile is the same, and so is one that already ends on a refused
// message: a second does not go after it (two sends raced and both were
// refused; the page is told of the first, and revises that).
//
// With `replaced` it takes that refused message's place instead, deleting it
// and writing the new one in the same transaction - and only while it is still
// the last row: deleting from it would take whatever came after.
func (s *Server) storeRefused(ctx context.Context, conversationID uuid.UUID, content string, replaced *sqlc.Message) (*sqlc.Message, error) {
	var row *sqlc.Message
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		if _, err := lockLive(ctx, q, conversationID); err != nil {
			return err
		}
		if err := requireIdle(ctx, q, conversationID); err != nil {
			return err
		}
		if replaced == nil {
			if err := requireRevised(ctx, q, conversationID); err != nil {
				return err
			}
		} else {
			last, err := q.GetLastMessage(ctx, conversationID)
			if err != nil {
				return err
			}
			if last.ID != replaced.ID || last.Status != sqlc.MessageStatusBlocked {
				return errReviseRefused
			}
			// A refused message carries no files, so there are no keys to hand back.
			if _, err := q.DeleteTranscriptFrom(ctx, sqlc.DeleteTranscriptFromParams{
				ConversationID: conversationID, FromCreatedAt: replaced.CreatedAt, FromID: replaced.ID, KeepID: uuid.Nil,
			}); err != nil {
				return err
			}
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
	case errors.Is(err, errConversationBusy), errors.Is(err, errReviseRefused), errors.Is(err, httpx.ErrNotFound), errors.Is(err, pgx.ErrNoRows):
		return nil, nil
	case err != nil:
		return nil, err
	}
	return row, nil
}
