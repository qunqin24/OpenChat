package api

import (
	"math"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/qunqin/openchat/apps/server/internal/db/sqlc"
	"github.com/qunqin/openchat/apps/server/internal/httpx"
)

// ListUsageLogs pages the ledger of model requests, newest first, with totals
// for everything the filters match.
func (s *Server) ListUsageLogs(w http.ResponseWriter, r *http.Request, params ListUsageLogsParams) {
	invalid := func(code, message string) {
		httpx.Fail(w, r, httpx.NewError(http.StatusBadRequest, code, message))
	}

	limit := clampLimit(params.Limit)
	page := int32(1)
	if params.Page != nil && *params.Page > 1 {
		page = int32(min(*params.Page, math.MaxInt32/int(limit)))
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
		RowLimit: limit, RowOffset: (page - 1) * limit,
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

	items := make([]UsageLog, 0, len(rows))
	for _, row := range rows {
		items = append(items, toUsageLog(row))
	}
	httpx.JSON(w, http.StatusOK, UsageLogPage{
		Items: items,
		Total: sum.Requests,
		Page:  int(page),
		Limit: int(limit),
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

func toUsageLog(row sqlc.ListUsageLogsRow) UsageLog {
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
		Streamed:         row.Streamed,
		DurationMs:       int64(row.DurationMs),
	}
	if row.FirstTokenMs != nil {
		ms := int64(*row.FirstTokenMs)
		out.FirstTokenMs = &ms
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
