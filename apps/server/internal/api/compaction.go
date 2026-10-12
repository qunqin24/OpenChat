package api

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/qunqin/openchat/apps/server/internal/agent"
	"github.com/qunqin/openchat/apps/server/internal/db/sqlc"
	"github.com/qunqin/openchat/apps/server/internal/httpx"
)

// Compressing a conversation's context (internal/agent/compaction.go).
//
// Only a run does it, by itself: before a request that would fill too much of
// the model's window, or once more after the model refused a request for its
// length. Nobody can ask for it; the handlers here read the summary and take it
// back. Compressing never changes the transcript, so the only state is the
// conversation's summary row, and the only thing announced is that row changing.

// settingCompressContext is the administrator's switch for compressing on
// its own. On unless turned off: it costs a model request now and then and
// saves the conversations that would otherwise stop at the window.
const settingCompressContext = "compress_context"

// NewCompression is the policy runs compress under: the administrator's
// switch, read before every request, and each model's window and limits as its
// settings resolve now.
func NewCompression(pool *pgxpool.Pool) *agent.Compression {
	q := sqlc.New(pool)
	return &agent.Compression{
		Auto: func(ctx context.Context) bool {
			on, err := boolSettingOr(ctx, q, settingCompressContext, true)
			if err != nil {
				slog.WarnContext(ctx, "read compress_context setting", "error", err)
				return true
			}
			return on
		},
		Profile: func(ctx context.Context, modelID string) (agent.ModelProfile, error) {
			return compressionProfile(ctx, q, modelID)
		},
	}
}

func compressionProfile(ctx context.Context, q *sqlc.Queries, modelID string) (agent.ModelProfile, error) {
	settings, err := loadModelSettings(ctx, q, modelID)
	if err != nil {
		return agent.ModelProfile{}, err
	}
	return agent.ModelProfile{
		ContextWindow: settings.ContextLimit,
		OutputLimit:   settings.OutputLimit,
		Effort:        leastThinking(ctx, q, modelID),
	}, nil
}

// toContextSummary maps a summary onto the contract.
func toContextSummary(s *agent.Summary) *ContextSummary {
	if s == nil {
		return nil
	}
	return &ContextSummary{
		Content:          s.Content,
		ThroughMessageId: s.ThroughID,
		KeptMessageId:    s.KeptID,
		Model:            s.ModelID,
		InputTokens:      s.InputTokens,
		OutputTokens:     s.OutputTokens,
		CreatedAt:        s.CreatedAt,
	}
}

// GetCompaction reads the conversation's summary, if it has one.
func (s *Server) GetCompaction(w http.ResponseWriter, r *http.Request, conversationId ConversationId) {
	ctx := r.Context()
	if err := s.conversationExists(ctx, conversationId); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	summary, err := agent.LoadSummary(ctx, s.q, conversationId)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, CompactionState{Summary: toContextSummary(summary)})
}

// UndoCompaction deletes the conversation's summary: the model is sent the
// whole conversation again, which was never removed.
func (s *Server) UndoCompaction(w http.ResponseWriter, r *http.Request, conversationId ConversationId) {
	ctx := r.Context()
	if err := s.conversationExists(ctx, conversationId); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	// Taking it back under a run would change the context in the middle of
	// its tool loop, between two requests that were meant to share a prefix.
	deleted := int64(0)
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		if _, err := lockLive(ctx, q, conversationId); err != nil {
			return err
		}
		if err := requireIdle(ctx, q, conversationId); err != nil {
			return err
		}
		var err error
		deleted, err = q.DeleteContextSummary(ctx, conversationId)
		return err
	})
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	if deleted > 0 {
		s.announceCompaction(conversationId, nil)
	}
	httpx.NoContent(w)
}

// announceCompaction tells the conversation's followers its summary changed.
// It belongs to no run: nil run id and version 0, like a title (a batch the
// followers apply whatever run state they hold).
func (s *Server) announceCompaction(conversationID uuid.UUID, summary *agent.Summary) {
	s.runner.Hub().Publish(conversationID, agent.Batch{
		Events: []agent.Event{agent.CompactionChanged{Summary: summary}},
	})
}
