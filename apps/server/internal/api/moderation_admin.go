package api

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/qunqin24/openchat/apps/server/internal/db/sqlc"
	"github.com/qunqin24/openchat/apps/server/internal/httpx"
)

// The moderation log, for administrators (the routes are in the admin group,
// which answers 404 to anyone else).

// ListModerationEvents pages the log of blocked messages, newest first, by
// cursor over (created_at, id).
func (s *Server) ListModerationEvents(w http.ResponseWriter, r *http.Request, params ListModerationEventsParams) {
	limit := clampLimit(params.Limit)
	cursorAt, cursorID, err := decodeCursor(params.Cursor)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	var category *string
	if params.Category != nil {
		if !params.Category.Valid() {
			httpx.Fail(w, r, httpx.NewError(http.StatusBadRequest, "invalid_category", "unknown category"))
			return
		}
		c := string(*params.Category)
		category = &c
	}

	rows, err := s.q.ListModerationEvents(r.Context(), sqlc.ListModerationEventsParams{
		CursorCreatedAt: cursorAt, CursorID: cursorID,
		UserID: params.UserId, Category: category,
		// One more than the page, to learn whether another follows.
		RowLimit: limit + 1,
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
	items := make([]ModerationEvent, 0, len(rows))
	for _, row := range rows {
		item := ModerationEvent{
			Id:             row.ID,
			CreatedAt:      row.CreatedAt.Time,
			UserId:         row.UserID,
			UserName:       row.UserName,
			UserEmail:      row.UserEmail,
			ConversationId: row.ConversationID,
			Source:         ModerationSource(row.Source),
			Mode:           ModerationEventMode(row.Mode),
			Categories:     row.Categories,
			Excerpt:        row.Excerpt,
		}
		if item.Categories == nil {
			item.Categories = []string{}
		}
		if row.UserID != nil {
			disabled := row.UserDisabled
			item.UserDisabled = &disabled
		}
		if row.Sanction != nil {
			sanction := ModerationSanction(*row.Sanction)
			item.Sanction = &sanction
		}
		items = append(items, item)
	}
	httpx.JSON(w, http.StatusOK, ModerationEventPage{Items: items, NextCursor: next, Limit: int(limit)})
}

// moderationRetention is how long a blocked message stays in the log.
const moderationRetention = 90 * 24 * time.Hour

// purgeModeration drops the log after 90 days, in batches of one statement with
// its age in the WHERE, so servers sharing the database may all run it.
func (s *Server) purgeModeration(ctx context.Context, now time.Time) {
	for range cleanupMaxBatches {
		n, err := s.q.PurgeModerationEvents(ctx, pgtype.Timestamptz{Time: now.Add(-moderationRetention), Valid: true})
		if err != nil {
			slog.WarnContext(ctx, "purge old moderation events", "error", err)
			break
		}
		if n > 0 {
			slog.InfoContext(ctx, "deleted old moderation events", "count", n)
		}
		if n < 5000 {
			break
		}
	}
}
