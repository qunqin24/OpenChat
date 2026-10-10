package api

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/qunqin/openchat/apps/server/internal/agent"
	"github.com/qunqin/openchat/apps/server/internal/auth"
	"github.com/qunqin/openchat/apps/server/internal/db/sqlc"
	"github.com/qunqin/openchat/apps/server/internal/httpx"
	"github.com/qunqin/openchat/apps/server/internal/llm"
)

// Answering a message again, as it was asked or reworded.
//
// A conversation is a tree of questions (messages.parent_input_id) and the
// reader is on one path through it, ending at the conversation's current leaf.
// Two things can be redone, and they are different moves:
//
//   - An answer. Regenerating starts another run on the same input and marks
//     the earlier ones superseded, which takes their rows out of the transcript
//     and out of every later model request without touching them. The reader
//     can switch back (ChooseAnswer), but only on the last input of the path —
//     an earlier one has later turns built on the answer it shows, and swapping
//     that answer out from under them would leave the conversation answering
//     things nobody said.
//   - A question. Editing any question on the path writes the new wording as a
//     sibling (same parent, edit_of the first version) and moves the leaf to
//     it. What followed the old wording is not deleted, only left off the path;
//     switching to a version (SwitchVersion) puts the reader back on the branch
//     under it, as it was left.

var (
	errNothingToAnswer = httpx.NewError(http.StatusNotFound, "nothing_to_answer",
		"this conversation has no message to answer")
	errRunNotFound = httpx.NewError(http.StatusNotFound, "run_not_found", "no such run in this conversation")
	errNotLatest   = httpx.NewError(http.StatusConflict, "not_latest",
		"only the last message's answer can be switched")
	errMessageNotFound = httpx.NewError(http.StatusNotFound, "message_not_found", "no such message in this conversation")
	errNotOnPath       = httpx.NewError(http.StatusConflict, "not_on_path",
		"that message is not on the branch being shown")
)

// appendInput writes a user message after `parent` (the input it follows, nil
// for a conversation's first) and makes it the conversation's current leaf. The
// caller holds the conversation (LockConversation), so the leaf it read is the
// leaf it replaces.
func appendInput(ctx context.Context, q *sqlc.Queries, conversationID uuid.UUID, parent *uuid.UUID, content string, parts []llm.Part, editOf *uuid.UUID) (sqlc.Message, error) {
	input, err := q.AppendEntry(ctx, sqlc.AppendEntryParams{
		ConversationID: conversationID,
		Role:           sqlc.MessageRoleUser,
		Content:        content,
		Parts:          agent.EncodeParts(parts),
		Status:         sqlc.MessageStatusComplete,
		EditOf:         editOf,
		ParentInputID:  parent,
	})
	if err != nil {
		return input, err
	}
	return input, q.SetCurrentInput(ctx, sqlc.SetCurrentInputParams{ConversationID: conversationID, InputID: &input.ID})
}

// RegenerateAnswer starts another run on the last user message of the branch shown.
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

	input, err := s.q.GetCurrentInput(ctx, conversationId)
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

	var run sqlc.Run
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		if err := stillCurrent(ctx, q, conversationId, input.ID); err != nil {
			return err
		}
		// Every answer so far steps aside. A run still going is not among
		// them for long: the one-active-run index refuses the insert below and
		// takes this back with it.
		if err := q.ChooseAnswer(ctx, sqlc.ChooseAnswerParams{InputID: input.ID}); err != nil {
			return err
		}
		run, err = createRun(ctx, q, conv, user.ID, input.ID, requestID, choice)
		return err
	})
	if err != nil {
		s.failRun(w, r, conversationId, requestID, err)
		return
	}
	s.startRun(conversationId, run, input)

	page, err := s.messagePage(ctx, []sqlc.Message{input})
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusAccepted, CreateMessageResponse{Message: page.Items[0], Run: toRun(run)})
}

// ChooseAnswer shows another answer to the last question of the branch shown.
func (s *Server) ChooseAnswer(w http.ResponseWriter, r *http.Request, conversationId ConversationId, runId openapi_types.UUID) {
	ctx := r.Context()
	if err := s.conversationExists(ctx, conversationId); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	run, err := s.q.GetRun(ctx, runId)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && run.ConversationID != conversationId) {
		httpx.Fail(w, r, errRunNotFound)
		return
	}
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}

	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		if err := stillCurrent(ctx, q, conversationId, run.InputMessageID); err != nil {
			return err
		}
		if err := q.ChooseAnswer(ctx, sqlc.ChooseAnswerParams{Keep: &run.ID, InputID: run.InputMessageID}); err != nil {
			return err
		}
		// Checked after the update, which waits on a regenerate that got to
		// these rows first: a run started meanwhile is the answer being
		// written, and hiding it is not this request's call.
		if _, err := q.GetActiveRun(ctx, conversationId); err == nil {
			return errConversationBusy
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		return nil
	})
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}

	rows, err := s.q.ListRunMessages(ctx, sqlc.ListRunMessagesParams{RunID: &run.ID, InputMessageID: run.InputMessageID})
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	page, err := s.messagePage(ctx, rows)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, page)
}

// stillCurrent is the check that an input is still the end of the branch shown,
// made with the conversation held: the handlers look before they lock, and
// another request may have moved the leaf in between.
func stillCurrent(ctx context.Context, q *sqlc.Queries, conversationID, inputID uuid.UUID) error {
	locked, err := q.LockConversation(ctx, conversationID)
	if err != nil {
		return err
	}
	if locked.CurrentInputID == nil || *locked.CurrentInputID != inputID {
		return errNotLatest
	}
	return nil
}

// EditMessage asks a question again in new words: a new user message beside
// the one edited, the conversation's new leaf, and the run that answers it.
// Any question on the branch shown can be edited, not only the last.
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

	edited, err := s.pathInput(ctx, conversationId, messageId)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	choice, err := s.chooseRun(ctx, conv, body.Model, body.ReasoningEffort, body.WebSearch)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}

	// The new wording, and the files the question carried: an edit changes
	// what was asked, not what was attached. They stay claimed by the first
	// version and are resolved by reference, as any message's are.
	parts := []llm.Part{llm.TextPart{Text: content}}
	if old, err := agent.DecodeParts(edited.Parts); err == nil {
		for _, part := range old {
			if file, ok := part.(llm.AttachmentPart); ok {
				parts = append(parts, file)
			}
		}
	}
	root := rootOf(edited)

	var (
		input sqlc.Message
		run   sqlc.Run
	)
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		// Held, then looked at again: a switch of branch queued ahead of this
		// may have taken the question off the path. A run still going is
		// refused below, as when sending.
		if _, err := q.LockConversation(ctx, conversationId); err != nil {
			return err
		}
		if _, err := s.pathInputWith(ctx, q, conversationId, messageId); err != nil {
			return err
		}
		var err error
		input, err = appendInput(ctx, q, conversationId, edited.ParentInputID, content, parts, &root)
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
	s.startRun(conversationId, run, input)

	page, err := s.messagePage(ctx, []sqlc.Message{input})
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusAccepted, CreateMessageResponse{Message: page.Items[0], Run: toRun(run)})
}

// pathInput is a user message of the conversation that is on the branch shown:
// 404 for one that is not the conversation's, 409 for one that is off the path.
func (s *Server) pathInput(ctx context.Context, conversationID uuid.UUID, messageID uuid.UUID) (sqlc.Message, error) {
	return s.pathInputWith(ctx, s.q, conversationID, messageID)
}

func (s *Server) pathInputWith(ctx context.Context, q *sqlc.Queries, conversationID uuid.UUID, messageID uuid.UUID) (sqlc.Message, error) {
	input, err := q.GetInput(ctx, sqlc.GetInputParams{ID: messageID, ConversationID: conversationID})
	if errors.Is(err, pgx.ErrNoRows) {
		return input, errMessageNotFound
	}
	if err != nil {
		return input, err
	}
	on, err := q.InputOnPath(ctx, sqlc.InputOnPathParams{ConversationID: conversationID, ID: messageID})
	if err != nil {
		return input, err
	}
	if !on {
		return input, errNotOnPath
	}
	return input, nil
}

// SwitchVersion moves the conversation to another version of a question and,
// with it, to the branch that was last worked on below that version.
func (s *Server) SwitchVersion(w http.ResponseWriter, r *http.Request, conversationId ConversationId, messageId openapi_types.UUID, params SwitchVersionParams) {
	ctx := r.Context()
	if err := s.conversationExists(ctx, conversationId); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	target, err := s.q.GetInput(ctx, sqlc.GetInputParams{ID: messageId, ConversationID: conversationId})
	if errors.Is(err, pgx.ErrNoRows) {
		httpx.Fail(w, r, errMessageNotFound)
		return
	}
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}

	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		// Nothing is running while the branch moves: a run writes under the
		// leaf it was started on, and the reader would be looking elsewhere.
		if _, err := q.LockConversation(ctx, conversationId); err != nil {
			return err
		}
		if _, err := q.GetActiveRun(ctx, conversationId); err == nil {
			return errConversationBusy
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		// Only a version of a question on the path can be switched to: the
		// pager offers nothing else, and a question deep in another branch
		// would land the reader somewhere they have no way back from.
		found, err := q.PathHasVersionOf(ctx, sqlc.PathHasVersionOfParams{
			ConversationID: conversationId, ParentInputID: target.ParentInputID,
		})
		if err != nil {
			return err
		}
		if !found {
			return errNotOnPath
		}
		on, err := q.InputOnPath(ctx, sqlc.InputOnPathParams{ConversationID: conversationId, ID: target.ID})
		if err != nil || on {
			return err // already there: the same branch is shown again
		}
		leaf, err := q.PickBranchLeaf(ctx, target.ID)
		if err != nil {
			return err
		}
		return q.SetCurrentInput(ctx, sqlc.SetCurrentInputParams{ConversationID: conversationId, InputID: &leaf})
	})
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}

	page, err := s.transcriptPage(ctx, conversationId, pgtype.Timestamptz{}, nil, clampLimit(params.Limit))
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, page)
}

// messagePage maps rows for a client: each with its files, and each question
// with its answers' versions and the versions of the question itself.
func (s *Server) messagePage(ctx context.Context, rows []sqlc.Message) (MessagePage, error) {
	page := MessagePage{Items: make([]Message, 0, len(rows))}
	attached, err := s.attachmentsFor(ctx, rows)
	if err != nil {
		return page, err
	}
	answers, branches, err := s.questionVersions(ctx, rows)
	if err != nil {
		return page, err
	}
	for _, row := range rows {
		m := toMessage(row, attached[row.ID])
		if runs := answers[row.ID]; len(runs) > 1 {
			m.Versions = &runs
		}
		if siblings := branches[row.ID]; len(siblings) > 1 {
			m.Branches = &siblings
		}
		page.Items = append(page.Items, m)
	}
	return page, nil
}

// rootOf is the first version of a question: the message itself, or the one it
// is an edit of.
func rootOf(m sqlc.Message) uuid.UUID {
	if m.EditOf != nil {
		return *m.EditOf
	}
	return m.ID
}

// questionVersions looks up, for each user message among rows, every run that
// answered it (oldest first) and every version of the question — itself and
// its siblings, oldest first.
func (s *Server) questionVersions(ctx context.Context, rows []sqlc.Message) (answers, branches map[uuid.UUID][]uuid.UUID, err error) {
	var ids []uuid.UUID
	for _, row := range rows {
		if row.Role == sqlc.MessageRoleUser {
			ids = append(ids, row.ID)
		}
	}
	if len(ids) == 0 {
		return nil, nil, nil
	}
	found, err := s.q.ListAnswers(ctx, ids)
	if err != nil {
		return nil, nil, err
	}
	answers = make(map[uuid.UUID][]uuid.UUID, len(ids))
	for _, a := range found {
		answers[a.Input] = append(answers[a.Input], a.ID)
	}
	versions, err := s.q.ListVersions(ctx, ids)
	if err != nil {
		return nil, nil, err
	}
	branches = make(map[uuid.UUID][]uuid.UUID, len(ids))
	for _, v := range versions {
		branches[v.MessageID] = append(branches[v.MessageID], v.VersionID)
	}
	return answers, branches, nil
}

// trimmed is an optional string's value without surrounding space, or "".
func trimmed(s *string) string {
	if s == nil {
		return ""
	}
	return strings.TrimSpace(*s)
}
