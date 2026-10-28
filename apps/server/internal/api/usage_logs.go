package api

import (
	"context"
	"math"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/qunqin24/openchat/apps/server/internal/db/sqlc"
	"github.com/qunqin24/openchat/apps/server/internal/httpx"
)

// ListUsageLogs pages the ledger of model requests, newest first, with totals
// for everything the filters match. Paged by cursor over (created_at, id), the
// order of the ledger's index: a request logged while an operator is reading
// cannot shift the pages, and the thousandth page costs what the first does.
func (s *Server) ListUsageLogs(w http.ResponseWriter, r *http.Request, params ListUsageLogsParams) {
	invalid := func(code, message string) {
		httpx.Fail(w, r, httpx.NewError(http.StatusBadRequest, code, message))
	}

	limit := clampLimit(params.Limit)
	cursorAt, cursorID, err := decodeCursor(params.Cursor)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}

	var (
		status, kind, model *string
	)
	if params.Status != nil {
		if !params.Status.Valid() {
			invalid("invalid_status", "unknown status: "+string(*params.Status))
			return
		}
		v := string(*params.Status)
		status = &v
	}
	if params.Kind != nil {
		if !params.Kind.Valid() {
			invalid("invalid_kind", "unknown kind: "+string(*params.Kind))
			return
		}
		v := string(*params.Kind)
		kind = &v
	}
	if params.Model != nil {
		if trimmed := strings.TrimSpace(*params.Model); trimmed != "" {
			model = &trimmed
		}
	}
	var from, to pgtype.Timestamptz
	if params.From != nil {
		from = pgtype.Timestamptz{Time: *params.From, Valid: true}
	}
	if params.To != nil {
		to = pgtype.Timestamptz{Time: *params.To, Valid: true}
	}
	userID, channelID := params.UserId, params.ChannelId

	rows, err := s.q.ListUsageLogs(r.Context(), sqlc.ListUsageLogsParams{
		UserID: userID, ModelID: model, ChannelID: channelID, Status: status, Kind: kind,
		FromTime: from, ToTime: to,
		CursorCreatedAt: cursorAt, CursorID: cursorID,
		// One more than the page, to learn whether another follows.
		RowLimit: limit + 1,
	})
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	sum, err := s.q.SummarizeUsageLogs(r.Context(), sqlc.SummarizeUsageLogsParams{
		UserID: userID, ModelID: model, ChannelID: channelID, Status: status, Kind: kind,
		FromTime: from, ToTime: to,
	})
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}

	var next *string
	if int32(len(rows)) > limit {
		rows = rows[:limit]
		last := rows[limit-1]
		cursor := encodeCursor(last.CreatedAt, last.ID)
		next = &cursor
	}
	items := make([]UsageLog, 0, len(rows))
	for _, row := range rows {
		items = append(items, s.toUsageLog(r.Context(), row))
	}
	httpx.JSON(w, http.StatusOK, UsageLogPage{
		Items:      items,
		NextCursor: next,
		Limit:      int(limit),
		Summary: UsageSummary{
			Requests:         sum.Requests,
			Errors:           sum.Errors,
			InputTokens:      sum.InputTokens,
			OutputTokens:     sum.OutputTokens,
			Cost:             sum.Cost,
			CacheReadTokens:  sum.CacheReadTokens,
			CacheWriteTokens: sum.CacheWriteTokens,
			CacheHitRate:     hitRate(sum.CacheReadTokens, sum.CachePromptTokens),
			CacheSavings:     savings(sum.CacheSavings),
		},
	})
}

func (s *Server) toUsageLog(ctx context.Context, row sqlc.ListUsageLogsRow) UsageLog {
	out := UsageLog{
		Id:               row.ID,
		CreatedAt:        row.CreatedAt.Time,
		Kind:             UsageKind(row.Kind),
		Status:           UsageStatus(row.Status),
		ErrorCode:        row.ErrorCode,
		ErrorMessage:     row.ErrorMessage,
		UserId:           row.UserID,
		UserName:         row.UserName,
		UserEmail:        row.UserEmail,
		UserAvatarUrl:    s.avatarURL(ctx, row.UserAvatarKey),
		ConversationId:   row.ConversationID,
		RunId:            row.RunID,
		ChannelId:        row.ChannelID,
		ChannelName:      row.ChannelName,
		Model:            row.ModelID,
		ServedModel:      row.ServedModel,
		InputTokens:      int64(row.InputTokens),
		OutputTokens:     int64(row.OutputTokens),
		ReasoningTokens:  int64(row.ReasoningTokens),
		CacheReadTokens:  int64(row.CacheReadTokens),
		CacheWriteTokens: int64(row.CacheWriteTokens),
		InputPrice:       numberPtr(row.InputPrice),
		OutputPrice:      numberPtr(row.OutputPrice),
		CacheReadPrice:   numberPtr(row.CacheReadPrice),
		CacheWritePrice:  numberPtr(row.CacheWritePrice),
		Cost:             numberPtr(row.Cost),
		DurationMs:       int64(row.DurationMs),
		Estimated:        row.Estimated,
	}
	if row.FirstTokenMs != nil {
		ms := int64(*row.FirstTokenMs)
		out.FirstTokenMs = &ms
	}
	// The account's name is written with every request (usage.Meter) and
	// says something only once the account is gone.
	if row.UserID == nil {
		out.DeletedUser = row.DeletedUser
	}
	return out
}

// numberPtr reads a nullable numeric column; NULL and anything that is not a
// finite number come out as nil.
func numberPtr(n pgtype.Numeric) *float64 {
	f, err := n.Float64Value()
	if err != nil || !f.Valid || math.IsNaN(f.Float64) || math.IsInf(f.Float64, 0) {
		return nil
	}
	return &f.Float64
}
