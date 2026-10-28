package agent

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/qunqin24/openchat/apps/server/internal/db/sqlc"
	"github.com/qunqin24/openchat/apps/server/internal/llm"
)

// Summary is a conversation's compressed context (`context_summaries`): the
// summary of every row up to and including ThroughID, in the place of those
// rows, with the user message KeptID - the question being answered, when one
// lay in that range - sent as it was written.
//
// The rows themselves are never touched; the person still reads them all. A
// summary changes only what a model is sent.
type Summary struct {
	ConversationID uuid.UUID
	ThroughID      uuid.UUID
	// ThroughAt is when the last row covered was written: where the rows a
	// request still carries begin.
	ThroughAt    time.Time
	KeptID       *uuid.UUID
	Content      string
	ModelID      string
	InputTokens  int
	OutputTokens int
	CreatedAt    time.Time
}

// Tags the summary is wrapped in when it is put in front of a model.
const (
	summaryOpen  = "<compressed_history_summary>"
	summaryClose = "</compressed_history_summary>"
)

// summaryNote is said inside the tags, since the system prompt is not changed
// to mention them: it must stay the same bytes whether or not a conversation has
// been compressed.
const summaryNote = "The earlier part of this conversation was compressed into the summary below. " +
	"Treat it as background: the messages that follow are the conversation as it stands."

// SummaryMessage is the summary as a model is sent it: a user message, which is
// the one role every provider takes at the head of a conversation and in front
// of another user message (the adapters merge adjacent turns of one role).
func SummaryMessage(content string) llm.Message {
	return llm.TextMessage(llm.RoleUser, summaryOpen+"\n"+summaryNote+"\n\n"+strings.TrimSpace(content)+"\n"+summaryClose)
}

func summaryOf(row sqlc.GetContextSummaryRow) *Summary {
	return &Summary{
		ConversationID: row.ConversationID,
		ThroughID:      row.ThroughMessageID,
		ThroughAt:      row.ThroughCreatedAt.Time,
		KeptID:         row.KeptMessageID,
		Content:        row.Content,
		ModelID:        row.ModelID,
		InputTokens:    int(row.InputTokens),
		OutputTokens:   int(row.OutputTokens),
		CreatedAt:      row.CreatedAt.Time,
	}
}

// LoadSummary reads a conversation's summary; nil when it has none.
func LoadSummary(ctx context.Context, q *sqlc.Queries, conversationID uuid.UUID) (*Summary, error) {
	row, err := q.GetContextSummary(ctx, conversationID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return summaryOf(row), nil
}

// LoadContextRows reads what a request is built from: the conversation's
// summary, if it has one, and the rows that go with it - the kept question
// first, then every row after the last one the summary covers, at most limit of
// them (the newest, when there are more). Without a summary, the newest limit
// rows of the conversation.
func LoadContextRows(ctx context.Context, q *sqlc.Queries, conversationID uuid.UUID, limit int32) (*Summary, []sqlc.Message, error) {
	summary, err := LoadSummary(ctx, q, conversationID)
	if err != nil {
		return nil, nil, err
	}
	params := sqlc.ListMessagesAfterParams{ConversationID: conversationID, RowLimit: limit}
	if summary != nil {
		params.AfterCreatedAt = pgtype.Timestamptz{Time: summary.ThroughAt, Valid: true}
		params.AfterID = &summary.ThroughID
	}
	rows, err := q.ListMessagesAfter(ctx, params)
	if err != nil {
		return nil, nil, err
	}
	if summary != nil && summary.KeptID != nil {
		kept, err := q.GetMessage(ctx, *summary.KeptID)
		if err != nil {
			return nil, nil, err
		}
		rows = append([]sqlc.Message{kept}, rows...)
	}
	return summary, rows, nil
}

// BuildRequestContext is BuildContext with the summary in front: the messages of
// a request after the system prompt.
func BuildRequestContext(summary *Summary, rows []sqlc.Message) ([]llm.Message, error) {
	messages, err := BuildContext(rows)
	if err != nil {
		return nil, err
	}
	if summary != nil {
		messages = append([]llm.Message{SummaryMessage(summary.Content)}, messages...)
	}
	return messages, nil
}
