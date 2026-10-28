package api

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/qunqin24/openchat/apps/server/internal/agent"
	"github.com/qunqin24/openchat/apps/server/internal/auth"
	"github.com/qunqin24/openchat/apps/server/internal/db/sqlc"
	"github.com/qunqin24/openchat/apps/server/internal/httpx"
	"github.com/qunqin24/openchat/apps/server/internal/llm"
)

// Answering a message again, as it was asked or reworded.
//
// A conversation is a line of rows in the order they were written, and what is
// replaced is gone: nothing is kept to go back to. Two things can be redone,
// and both delete before they write, in the transaction that starts the run
// and under the conversation's lock (LockConversation), so that nothing else
// sees the conversation between the two:
//
//   - An answer. Regenerating deletes the rows after the last question and the
//     runs that made them - the runs that carried on after a question the model
//     asked included - and starts another run on the same question.
//   - A question. Editing any question deletes it and every row after it, and
//     writes the new wording at the end as a message of its own, which a run
//     answers as if the conversation were picked up again from there. The files
//     the question carried move to the new message; those of the questions
//     deleted with it are removed once the transaction has committed.
//
// Both refuse a conversation that is answering: the run being written would be
// deleted from under itself.

var (
	errNothingToAnswer = httpx.NewError(http.StatusNotFound, "nothing_to_answer",
		"this conversation has no message to answer")
	errNotLatest = httpx.NewError(http.StatusConflict, "not_latest",
		"only the last message's answer can be redone")
	errMessageNotFound = httpx.NewError(http.StatusNotFound, "message_not_found", "no such message in this conversation")

	errAttachmentsNotEditable = httpx.NewFieldError("attachmentIds", "attachments_not_editable",
		"only a message that moderation refused takes files when it is edited; any other keeps its own")
)

// appendInput writes a user message at the end of a conversation.
func appendInput(ctx context.Context, q *sqlc.Queries, conversationID uuid.UUID, content string, parts []llm.Part) (sqlc.Message, error) {
	return q.AppendEntry(ctx, sqlc.AppendEntryParams{
		ConversationID: conversationID,
		Role:           sqlc.MessageRoleUser,
		Content:        content,
		Parts:          agent.EncodeParts(parts),
		Status:         sqlc.MessageStatusComplete,
	})
}

// requireIdle refuses a conversation that is answering, with the conversation
// held: a run is started only by a request that holds it, so what is seen here
// stays true until the caller commits.
func requireIdle(ctx context.Context, q *sqlc.Queries, conversationID uuid.UUID) error {
	if _, err := q.GetActiveRun(ctx, conversationID); err == nil {
		return errConversationBusy
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	return nil
}

// RegenerateAnswer deletes the answer to the last user message and starts
// another run on it.
func (s *Server) RegenerateAnswer(w http.ResponseWriter, r *http.Request, conversationId ConversationId) {
	ctx := r.Context()
	user := auth.MustUser(ctx)

	var body RegenerateRequest
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	conv, err := s.q.GetConversation(ctx, sqlc.GetConversationParams{ID: conversationId, UserID: &user.ID})
	if err != nil {
		httpx.Fail(w, r, dbErr(err))
		return
	}
	requestID := trimmed(body.RequestId)
	if found, ok := s.existingRun(ctx, conversationId, requestID); ok {
		httpx.JSON(w, http.StatusAccepted, found)
		return
	}

	// A refused message at the end is reworded first (moderation_kept.go); the
	// question before it is not the one to answer again.
	if err := requireRevised(ctx, s.q, conversationId); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	input, err := s.q.GetLastInput(ctx, conversationId)
	if errors.Is(err, pgx.ErrNoRows) {
		httpx.Fail(w, r, errNothingToAnswer)
		return
	}
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	choice, err := s.chooseRun(ctx, conv, body.Model, body.ReasoningEffort, body.WebSearch)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	// The conversation's first run takes its memory snapshot here, with whatever
	// it carries that was never moderated checked first (moderation_text.go).
	if err := s.prepareSnapshot(ctx, user, conv); err != nil {
		httpx.Fail(w, r, err)
		return
	}

	// The answer's sub-agents go with it (conversations.launched_by cascades);
	// their runs going now are stopped once it is gone.
	subagents := s.runningSubagents(ctx, conversationId)
	var run sqlc.Run
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		if _, err := lockLive(ctx, q, conversationId); err != nil {
			return err
		}
		if err := requireIdle(ctx, q, conversationId); err != nil {
			return err
		}
		if err := requireRevised(ctx, q, conversationId); err != nil {
			return err
		}
		// The question was looked up before the lock: a message sent since is
		// the last now, and the answer the caller meant to redo is not.
		last, err := q.GetLastInput(ctx, conversationId)
		if errors.Is(err, pgx.ErrNoRows) || (err == nil && last.ID != input.ID) {
			return errNotLatest
		}
		if err != nil {
			return err
		}
		if err := q.DeleteAnswerTo(ctx, sqlc.DeleteAnswerToParams{ConversationID: conversationId, InputID: input.ID}); err != nil {
			return err
		}
		run, err = createRun(ctx, q, conv, user.ID, input.ID, requestID, choice)
		return err
	})
	if err != nil {
		s.failRun(w, r, conversationId, requestID, err)
		return
	}
	s.cancelGoneRuns(context.WithoutCancel(ctx), subagents)
	// Other tabs hold the answer that was just deleted.
	s.startRun(conversationId, run, input, agent.TranscriptChanged{})
	s.subagentsChanged(conversationId)

	page, err := s.messagePage(ctx, []sqlc.Message{input})
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusAccepted, CreateMessageResponse{Message: page.Items[0], Run: toRun(run)})
}

// EditMessage asks a question again in new words: the question and everything
// after it are deleted, the new wording is written at the end, and a run
// answers it. Any question can be edited, not only the last.
func (s *Server) EditMessage(w http.ResponseWriter, r *http.Request, conversationId ConversationId, messageId openapi_types.UUID) {
	ctx := r.Context()
	user := auth.MustUser(ctx)

	var body EditMessageRequest
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	content := strings.TrimSpace(body.Content)
	if content == "" {
		httpx.Fail(w, r, httpx.NewError(http.StatusBadRequest, "empty_content", "content must not be empty"))
		return
	}
	conv, err := s.q.GetConversation(ctx, sqlc.GetConversationParams{ID: conversationId, UserID: &user.ID})
	if err != nil {
		httpx.Fail(w, r, dbErr(err))
		return
	}
	requestID := trimmed(body.RequestId)
	if found, ok := s.existingRun(ctx, conversationId, requestID); ok {
		httpx.JSON(w, http.StatusAccepted, found)
		return
	}
	target, err := s.q.GetInput(ctx, sqlc.GetInputParams{ID: messageId, ConversationID: conversationId})
	if err != nil {
		httpx.Fail(w, r, inputErr(err))
		return
	}
	// A message that was refused is the one an edit can send files with: it
	// carried none (they stayed the composer's), so a reworded one takes the
	// composer's. Any other keeps its own.
	refusedTarget := target.Status == sqlc.MessageStatusBlocked
	var ids []uuid.UUID
	if body.AttachmentIds != nil {
		ids = *body.AttachmentIds
	}
	if len(ids) > 0 && !refusedTarget {
		httpx.Fail(w, r, errAttachmentsNotEditable)
		return
	}
	choice, err := s.chooseRun(ctx, conv, body.Model, body.ReasoningEffort, body.WebSearch)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	files, err := s.admitAttachments(ctx, user.ID, choice.modelID, ids)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	// The new wording, and any files this edit brings: those of a question that
	// was sent were checked when it was first sent. As a send is, with the
	// pictures read once and what was read recorded (attachment_digest.go).
	in, digests := s.checkedFiles(content, files)
	if refused, err := s.moderated(ctx, s.accountSubject(ctx, user, conversationId, ModerationSourceEdit), in); err != nil {
		// The words that were refused replace the refused message they were to
		// reword, so the transcript shows what was tried last.
		if refused && refusedTarget {
			err = s.keepRefusedEdit(ctx, conversationId, target, content, err)
		}
		httpx.Fail(w, r, err)
		return
	}
	// The conversation's first run takes its memory snapshot here, with whatever
	// it carries that was never moderated checked first (moderation_text.go).
	if err := s.prepareSnapshot(ctx, user, conv); err != nil {
		httpx.Fail(w, r, err)
		return
	}

	// Sub-agents launched by the answers deleted go with them; their runs
	// going now are stopped once they are gone.
	subagents := s.runningSubagents(ctx, conversationId)
	var (
		input sqlc.Message
		run   sqlc.Run
		keys  []string
	)
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		if _, err := lockLive(ctx, q, conversationId); err != nil {
			return err
		}
		if err := requireIdle(ctx, q, conversationId); err != nil {
			return err
		}
		// Read again with the conversation held: an edit queued ahead of this
		// may have deleted the question.
		edited, err := q.GetInput(ctx, sqlc.GetInputParams{ID: messageId, ConversationID: conversationId})
		if err != nil {
			return inputErr(err)
		}

		// The new wording, and the files the question carried: an edit changes
		// what was asked, not what was attached.
		parts := inputPartsChecked(content, files, digests)
		if old, err := agent.DecodeParts(edited.Parts); err == nil {
			for _, part := range old {
				if file, ok := part.(llm.AttachmentPart); ok {
					parts = append(parts, file)
				}
			}
		}
		input, err = appendInput(ctx, q, conversationId, content, parts)
		if err != nil {
			return err
		}
		if len(files) > 0 {
			// The claim re-checks what admission saw, as a send's does.
			claimed, err := q.AttachToMessage(ctx, sqlc.AttachToMessageParams{
				MessageID: &input.ID, UserID: &user.ID, Ids: attachmentIDs(files),
			})
			if err != nil {
				return err
			}
			if claimed != int64(len(files)) {
				return errAttachmentUnavailable
			}
		}
		// The files change hands before the rows are deleted, or they would
		// go with the question they were attached to.
		if err := q.MoveAttachments(ctx, sqlc.MoveAttachmentsParams{FromID: &edited.ID, ToID: &input.ID}); err != nil {
			return err
		}
		keys, err = q.DeleteTranscriptFrom(ctx, sqlc.DeleteTranscriptFromParams{
			ConversationID: conversationId, FromCreatedAt: edited.CreatedAt, FromID: edited.ID, KeepID: input.ID,
		})
		if err != nil {
			return err
		}
		run, err = createRun(ctx, q, conv, user.ID, input.ID, requestID, choice)
		return err
	})
	if err != nil {
		s.failRun(w, r, conversationId, requestID, err)
		return
	}
	// Rows are gone, so the files they named are wanted by nobody.
	s.removeFiles(context.WithoutCancel(ctx), keys)
	s.cancelGoneRuns(context.WithoutCancel(ctx), subagents)
	s.subagentsChanged(conversationId)
	// Other tabs showing this conversation hold what was deleted; they are told
	// in the batch that starts the run.
	s.startRun(conversationId, run, input, agent.TranscriptChanged{From: messageId})

	page, err := s.messagePage(ctx, []sqlc.Message{input})
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusAccepted, CreateMessageResponse{Message: page.Items[0], Run: toRun(run)})
}

// inputErr is the error for a user message that is not there.
func inputErr(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return errMessageNotFound
	}
	return err
}

// messagePage maps rows for a client, each with its files.
func (s *Server) messagePage(ctx context.Context, rows []sqlc.Message) (MessagePage, error) {
	page := MessagePage{Items: make([]Message, 0, len(rows))}
	attached, err := s.attachmentsFor(ctx, rows)
	if err != nil {
		return page, err
	}
	for _, row := range rows {
		page.Items = append(page.Items, messageWithFiles(row, attached))
	}
	return page, nil
}

// trimmed is an optional string's value without surrounding space, or "".
func trimmed(s *string) string {
	if s == nil {
		return ""
	}
	return strings.TrimSpace(*s)
}
