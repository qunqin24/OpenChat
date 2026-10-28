package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/qunqin24/openchat/apps/server/internal/auth"
	"github.com/qunqin24/openchat/apps/server/internal/db/sqlc"
	"github.com/qunqin24/openchat/apps/server/internal/httpx"
	ledger "github.com/qunqin24/openchat/apps/server/internal/usage"
)

// Request traces, the 轨迹 view: what each model request of a conversation
// carried - system prompt, messages, tools, options - and what came back,
// beside its timing and tokens (usage_logs). A debugging aid for whoever runs
// the service, so:
//
//   - only an administrator's own requests are recorded (NewTracer), and an
//     administrator reads only their own conversations' (the query names the
//     owner): nobody, administrators included, reads another person's
//     conversation through it;
//   - they are kept for a week (traceRetention) and then deleted.

const (
	// traceRetention is how long a trace is kept.
	traceRetention = 7 * 24 * time.Hour
	// settingRequestTraces switches recording, and the view, on. Off unless
	// an administrator turns it on: it is for debugging, and keeps what
	// administrators ask word for word.
	settingRequestTraces = "request_traces"
)

func (s *Server) tracesOn(ctx context.Context) (bool, error) {
	return boolSettingOr(ctx, s.q, settingRequestTraces, false)
}

// NewTracer traces the requests of administrators, while traces are on.
func NewTracer(pool *pgxpool.Pool) ledger.Tracer {
	q := sqlc.New(pool)
	return func(ctx context.Context, userID uuid.UUID) bool {
		on, err := boolSettingOr(ctx, q, settingRequestTraces, false)
		if err != nil || !on {
			return false
		}
		admin, err := q.UserIsAdmin(ctx, userID)
		return err == nil && admin != nil && *admin
	}
}

func (s *Server) purgeTraces(ctx context.Context, before pgtype.Timestamptz) {
	total := int64(0)
	for range cleanupMaxBatches {
		n, err := s.q.DeleteOldRequestTraces(ctx, sqlc.DeleteOldRequestTracesParams{Before: before, RowLimit: cleanupBatch})
		if err != nil {
			slog.WarnContext(ctx, "delete old request traces", "error", err)
			break
		}
		total += n
		if n < cleanupBatch {
			break
		}
	}
	if total > 0 {
		slog.InfoContext(ctx, "deleted old request traces", "count", total)
	}
}

// GetConversationTrace lists the recorded requests of one of the caller's own
// conversations, oldest first. Mounted with the administrator routes.
func (s *Server) GetConversationTrace(w http.ResponseWriter, r *http.Request, conversationId ConversationId) {
	ctx := r.Context()
	user := auth.MustUser(ctx)
	if on, err := s.tracesOn(ctx); err != nil {
		httpx.Fail(w, r, err)
		return
	} else if !on {
		httpx.Fail(w, r, httpx.ErrNotFound)
		return
	}
	if err := s.conversationExists(ctx, conversationId); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	rows, err := s.q.ListConversationTrace(ctx, sqlc.ListConversationTraceParams{ConversationID: conversationId, UserID: user.ID})
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	out := ConversationTrace{Requests: make([]TraceRequest, 0, len(rows)), RetentionDays: int(traceRetention / (24 * time.Hour))}
	for _, row := range rows {
		item := TraceRequest{
			Id:               row.UsageLogID,
			RunId:            row.RunID,
			Kind:             row.Kind,
			Status:           TraceRequestStatus(row.Status),
			ErrorCode:        row.ErrorCode,
			ErrorMessage:     row.ErrorMessage,
			StartedAt:        row.CreatedAt.Time,
			DurationMs:       int(row.DurationMs),
			ModelId:          row.ModelID,
			ServedModel:      row.ServedModel,
			ChannelName:      row.ChannelName,
			InputTokens:      int(row.InputTokens),
			OutputTokens:     int(row.OutputTokens),
			ReasoningTokens:  int(row.ReasoningTokens),
			CacheReadTokens:  int(row.CacheReadTokens),
			CacheWriteTokens: int(row.CacheWriteTokens),
			Estimated:        row.Estimated,
			Cost:             numberPtr(row.Cost),
		}
		if row.FirstTokenMs != nil {
			ms := int(*row.FirstTokenMs)
			item.FirstTokenMs = &ms
		}
		if err := json.Unmarshal(row.Request, &item.Request); err != nil {
			httpx.Fail(w, r, err)
			return
		}
		if err := json.Unmarshal(row.Response, &item.Response); err != nil {
			httpx.Fail(w, r, err)
			return
		}
		out.Requests = append(out.Requests, item)
	}
	httpx.JSON(w, http.StatusOK, out)
}
