package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/qunqin/openchat/apps/server/internal/agent"
	"github.com/qunqin/openchat/apps/server/internal/auth"
	"github.com/qunqin/openchat/apps/server/internal/db/sqlc"
	"github.com/qunqin/openchat/apps/server/internal/httpx"
	"github.com/qunqin/openchat/apps/server/internal/llm"
	"github.com/qunqin/openchat/apps/server/internal/tools"
)

// Answering the model's question.
//
// A run that calls ask_user stops there, `awaiting_input`, and holds nothing
// open: the call sits on the transcript with no result. The answer is a request
// like any other. It writes the result and starts a run that continues the
// one that asked — same input, `continues` the first run of the answer — so
// the reader sees one reply, and regenerating or editing it takes both away.
//
// Which question can be answered is a question about the transcript, not about
// a flag: it is the last visible row, an assistant row of a run that is
// awaiting input, calling ask_user with that id. Anything written after it —
// the user typing a message instead, an answer — makes it a question that was
// skipped or answered, and anything hidden by a redo is not visible to begin
// with. An unanswered call that is skipped is closed off when the next request
// is built (agent.BuildContext), so nothing is written to it.

var errNotPending = httpx.NewError(http.StatusConflict, "not_pending",
	"this question is no longer waiting for an answer")

// AnswerQuestion records the answers as the result of the model's ask_user
// call and starts the run that carries on.
func (s *Server) AnswerQuestion(w http.ResponseWriter, r *http.Request, conversationId ConversationId, callId string) {
	ctx := r.Context()
	user := auth.MustUser(ctx)

	var body AnswerQuestionRequest
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
	if found, ok := s.existingAnswer(ctx, conversationId, requestID); ok {
		httpx.JSON(w, http.StatusAccepted, found)
		return
	}

	asked, err := pendingQuestion(ctx, s.q, conversationId, callId)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	answers := make([]tools.Answer, 0, len(body.Answers))
	for _, a := range body.Answers {
		other := ""
		if a.Other != nil {
			other = *a.Other
		}
		answers = append(answers, tools.Answer{Selected: a.Selected, Other: other})
	}
	result, err := answerPart(asked.questions, callId, answers)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	choice, err := s.chooseRun(ctx, conv, body.Model, body.ReasoningEffort, body.WebSearch)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	// The answer joins the run that asked, or the run that began the answer it
	// is already part of.
	head := asked.run.ID
	if asked.run.Continues != nil {
		head = *asked.run.Continues
	}
	choice.continues = &head

	var (
		run     sqlc.Run
		written sqlc.Message
	)
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		// A switch of branch queued ahead of this may have taken the question
		// off the path; it is looked for again below, with the conversation held.
		if _, err := q.LockConversation(ctx, conversationId); err != nil {
			return err
		}
		// Two answers to one question queue here, and the second finds the
		// first's and is refused.
		if _, err := q.LockRun(ctx, asked.run.ID); err != nil {
			return err
		}
		if _, err := pendingQuestion(ctx, q, conversationId, callId); err != nil {
			return err
		}
		var err error
		run, written, err = startAnswerRun(ctx, q, conv, user.ID, asked.run.InputMessageID, requestID, choice, result)
		return err
	})
	if err != nil {
		s.failRun(w, r, conversationId, requestID, err)
		return
	}

	input, err := s.q.GetMessage(ctx, run.InputMessageID)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	s.startRun(conversationId, run, input, agent.MessageAppended{Message: written})

	page, err := s.messagePage(ctx, []sqlc.Message{input})
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusAccepted, AnswerQuestionResponse{Message: page.Items[0], Result: toMessage(written, nil), Run: toRun(run)})
}

// answerPart checks the answers against the questions asked and gives the tool
// result they are recorded as. A reply that does not fit is a 400.
func answerPart(questions tools.Questions, callID string, answers []tools.Answer) (llm.ToolResultPart, error) {
	out, err := questions.Result(answers)
	if errors.Is(err, tools.ErrInvalidAnswers) {
		return llm.ToolResultPart{}, httpx.NewFieldError("answers", "invalid_answers", err.Error())
	}
	if err != nil {
		return llm.ToolResultPart{}, err
	}
	result := llm.ToolResultPart{CallID: callID, Name: agent.ToolAskUser, Content: out.Content, Status: llm.ToolSucceeded}
	if details, err := json.Marshal(out.Details); err == nil {
		result.Details = details
	}
	return result, nil
}

// startAnswerRun starts the run that carries on from an answered question and
// writes the answer as its first row, inside the caller's transaction.
func startAnswerRun(ctx context.Context, q *sqlc.Queries, conv sqlc.Conversation, userID, inputID uuid.UUID, requestID string, choice runChoice, result llm.ToolResultPart) (sqlc.Run, sqlc.Message, error) {
	run, err := createRun(ctx, q, conv, userID, inputID, requestID, choice)
	if err != nil {
		return run, sqlc.Message{}, err
	}
	parts := []llm.Part{result}
	written, err := q.AppendEntry(ctx, sqlc.AppendEntryParams{
		ConversationID: conv.ID,
		Role:           sqlc.MessageRoleTool,
		Content:        agent.PlainText(parts),
		Parts:          agent.EncodeParts(parts),
		Status:         sqlc.MessageStatusComplete,
		RunID:          &run.ID,
	})
	return run, written, err
}

// asking is a question waiting for its answer.
type asking struct {
	// run is the run that asked: awaiting input.
	run       sqlc.Run
	questions tools.Questions
}

// pendingQuestion finds the ask_user call with this id if it is the one the
// conversation is waiting on (see the note at the top of the file).
func pendingQuestion(ctx context.Context, q *sqlc.Queries, conversationID uuid.UUID, callID string) (asking, error) {
	var out asking
	rows, err := q.ListRecentMessages(ctx, sqlc.ListRecentMessagesParams{ConversationID: conversationID, RowLimit: 1})
	if err != nil {
		return out, err
	}
	if len(rows) == 0 || rows[0].Role != sqlc.MessageRoleAssistant || rows[0].RunID == nil {
		return out, errNotPending
	}
	out.run, err = q.GetRun(ctx, *rows[0].RunID)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && out.run.Status != sqlc.RunStatusAwaitingInput) {
		return out, errNotPending
	}
	if err != nil {
		return out, err
	}
	parts, err := agent.DecodeParts(rows[0].Parts)
	if err != nil {
		return out, err
	}
	for _, part := range parts {
		call, ok := part.(llm.ToolCallPart)
		if !ok || call.ID != callID || call.Name != agent.ToolAskUser {
			continue
		}
		if out.questions, err = tools.ParseQuestions(call.Arguments); err != nil {
			return out, errNotPending
		}
		return out, nil
	}
	return out, errNotPending
}

// existingAnswer is the answer a retried request already started.
func (s *Server) existingAnswer(ctx context.Context, conversationID uuid.UUID, requestID string) (AnswerQuestionResponse, bool) {
	if requestID == "" {
		return AnswerQuestionResponse{}, false
	}
	run, err := s.q.GetRunByRequestID(ctx, sqlc.GetRunByRequestIDParams{ConversationID: conversationID, RequestID: &requestID})
	if err != nil || run.Continues == nil {
		return AnswerQuestionResponse{}, false
	}
	rows, err := s.q.ListRunMessages(ctx, sqlc.ListRunMessagesParams{RunID: &run.ID, InputMessageID: run.InputMessageID})
	if err != nil {
		return AnswerQuestionResponse{}, false
	}
	var input, result *sqlc.Message
	for i, row := range rows {
		switch {
		case row.ID == run.InputMessageID:
			input = &rows[i]
		case row.Role == sqlc.MessageRoleTool && result == nil:
			result = &rows[i]
		}
	}
	if input == nil || result == nil {
		return AnswerQuestionResponse{}, false
	}
	page, err := s.messagePage(ctx, []sqlc.Message{*input})
	if err != nil {
		return AnswerQuestionResponse{}, false
	}
	return AnswerQuestionResponse{Message: page.Items[0], Result: toMessage(*result, nil), Run: toRun(run)}, true
}
