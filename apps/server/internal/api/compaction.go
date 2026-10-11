package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/qunqin/openchat/apps/server/internal/agent"
	"github.com/qunqin/openchat/apps/server/internal/auth"
	"github.com/qunqin/openchat/apps/server/internal/db/sqlc"
	"github.com/qunqin/openchat/apps/server/internal/httpx"
	ledger "github.com/qunqin/openchat/apps/server/internal/usage"
)

// Compressing a conversation's context (internal/agent/compaction.go).
//
// A run does it by itself before a request that would fill too much of the
// model's window; the handlers here are the same thing asked for by a person -
// from the context meter, or from the notice of an answer that failed for its
// length - and taken back. Compressing never changes the transcript, so the
// only state is the conversation's summary row, and the only thing announced
// is that row changing.

// settingCompressContext is the administrator's switch for compressing on
// its own. On unless turned off: it costs a model request now and then and
// saves the conversations that would otherwise stop at the window.
const settingCompressContext = "compress_context"

var (
	errNothingToCompact = httpx.NewError(http.StatusConflict, "nothing_to_compact",
		"there is nothing new to compress")
	errCompactionBusy = httpx.NewError(http.StatusConflict, "compaction_busy",
		"this conversation is being compressed already")
	errCompactionFailed = httpx.NewError(http.StatusBadGateway, "compaction_failed",
		"the model did not write a summary")
)

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

// CompactConversation summarises the conversation now, on a person's request.
func (s *Server) CompactConversation(w http.ResponseWriter, r *http.Request, conversationId ConversationId) {
	ctx := r.Context()
	user := auth.MustUser(ctx)
	conv, err := s.q.GetConversation(ctx, sqlc.GetConversationParams{ID: conversationId, UserID: &user.ID})
	if err != nil {
		httpx.Fail(w, r, dbErr(err))
		return
	}
	// A run adds to the conversation as it goes, and compresses it itself.
	if err := requireIdle(ctx, s.q, conversationId); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	// Nor while the model waits on a question: the summary would fold the
	// question in as skipped, and the answer, written after it, would then
	// reach the model as a result whose call it never sees.
	if waiting, err := questionWaiting(ctx, s.q, conversationId); err != nil {
		httpx.Fail(w, r, err)
		return
	} else if waiting {
		httpx.Fail(w, r, errQuestionWaiting)
		return
	}

	// The model that answers the conversation: its own, as a run would choose.
	modelID := ""
	if conv.Model != nil {
		modelID = *conv.Model
	}
	modelID, channelID, err := s.pickChannel(ctx, modelID, askerFrom(ctx))
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	client, err := NewModelResolver(s.pool).Client(ctx, &channelID, modelID)
	if err != nil {
		httpx.Fail(w, r, errModelUnavailable)
		return
	}
	profile, err := compressionProfile(ctx, s.q, modelID)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}

	summary, err := s.runner.Compactor().Compact(ctx, agent.CompactParams{
		ConversationID: conversationId,
		UserID:         user.ID,
		ChannelID:      &channelID,
		ModelID:        modelID,
		Client:         client,
		Profile:        profile,
		Mode:           agent.ModeManual,
		EndUser:        s.endUsers.Account(ctx, user.ID),
	})
	switch {
	case errors.Is(err, agent.ErrNothingToCompact):
		httpx.Fail(w, r, errNothingToCompact)
		return
	case errors.Is(err, agent.ErrCompactionBusy):
		httpx.Fail(w, r, errCompactionBusy)
		return
	case errors.Is(err, ledger.ErrCapacity):
		httpx.Fail(w, r, errModelCapacity)
		return
	case err != nil && ctx.Err() != nil:
		return // the person went away
	case err != nil:
		slog.WarnContext(ctx, "compression on request", "conversation_id", conversationId, "error", err)
		httpx.Fail(w, r, errCompactionFailed)
		return
	}
	s.announceCompaction(conversationId, summary)
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

// GuestCompact has the model write a guest's summary. The browser keeps the
// conversation and chooses what is covered; this is a model request and
// nothing else, billed to no one (as a guest's title is).
func (s *Server) GuestCompact(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var body GuestCompactRequest
	if err := decodeLenient(r, &body, guestBodyLimit); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	if len(body.Messages) > guestMaxRows*10 {
		httpx.Fail(w, r, guestBad("too_many_messages", "too many rows to summarise in one request"))
		return
	}
	ids := map[uuid.UUID]bool{}
	var total int64
	var rows []sqlc.Message
	for _, m := range body.Messages {
		row, err := guestRowOf(m, ids, map[uuid.UUID]bool{}, &total)
		if err != nil {
			httpx.Fail(w, r, err)
			return
		}
		// A file is named in the history and not read: nothing is stored here.
		for i := range row.files {
			row.files[i].data = nil
		}
		stop := row.stop
		rows = append(rows, sqlc.Message{
			ID: row.id, Role: row.role, Content: row.content, Parts: agent.EncodeParts(row.parts),
			StopReason: stop, CreatedAt: pgtype.Timestamptz{Time: row.at, Valid: !row.at.IsZero()},
		})
	}
	messages, err := agent.BuildContext(rows)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	if len(messages) == 0 {
		httpx.Fail(w, r, errNothingToCompact)
		return
	}
	if err := s.takeGuestRequest(ctx, clientIP(r)); err != nil {
		httpx.Fail(w, r, err)
		return
	}

	model := ""
	if body.Model != nil {
		model = strings.TrimSpace(*body.Model)
	}
	// As an ordinary user's: models kept for administrators are not offered.
	modelID, channelID, err := s.pickChannel(ctx, model, asker{})
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	client, err := NewModelResolver(s.pool).Client(ctx, &channelID, modelID)
	if err != nil {
		httpx.Fail(w, r, errModelUnavailable)
		return
	}
	profile, err := compressionProfile(ctx, s.q, modelID)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	content, in, out, err := s.runner.Compactor().Summarise(ctx, agent.CompactParams{
		ChannelID: &channelID, ModelID: modelID, Client: client, Profile: profile,
		EndUser: s.endUsers.Guest(ctx, clientIP(r)),
	}, derefOr(body.Existing, ""), derefOr(body.Latest, ""), messages)
	switch {
	case errors.Is(err, agent.ErrNothingToCompact):
		httpx.Fail(w, r, errNothingToCompact)
		return
	case errors.Is(err, ledger.ErrCapacity):
		httpx.Fail(w, r, errModelCapacity)
		return
	case err != nil && ctx.Err() != nil:
		return
	case err != nil:
		slog.WarnContext(ctx, "guest compression", "error", err)
		httpx.Fail(w, r, errCompactionFailed)
		return
	}
	httpx.JSON(w, http.StatusOK, GuestCompactResponse{Content: content, Model: modelID, InputTokens: in, OutputTokens: out})
}
