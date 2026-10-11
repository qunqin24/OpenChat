package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/qunqin/openchat/apps/server/internal/auth"
	"github.com/qunqin/openchat/apps/server/internal/db/sqlc"
	"github.com/qunqin/openchat/apps/server/internal/httpx"
)

// The moderation log and the guest bans, for administrators (the routes are in
// the admin group, which answers 404 to anyone else).

// ListModerationEvents pages the log of blocked messages, newest first, by
// cursor over (created_at, id).
func (s *Server) ListModerationEvents(w http.ResponseWriter, r *http.Request, params ListModerationEventsParams) {
	limit := clampLimit(params.Limit)
	cursorAt, cursorID, err := decodeCursor(params.Cursor)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	var guestIP, category *string
	if params.GuestIp != nil {
		if trimmed := strings.TrimSpace(*params.GuestIp); trimmed != "" {
			guestIP = &trimmed
		}
	}
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
		UserID: params.UserId, GuestIp: guestIP, Category: category,
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
			GuestIp:        row.GuestIp,
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
		if row.GuestIp != nil {
			banned := row.IpBanned
			item.IpBanned = &banned
		}
		if row.Sanction != nil {
			sanction := ModerationSanction(*row.Sanction)
			item.Sanction = &sanction
		}
		items = append(items, item)
	}
	httpx.JSON(w, http.StatusOK, ModerationEventPage{Items: items, NextCursor: next, Limit: int(limit)})
}

func toGuestBan(row sqlc.GuestBan) GuestBan {
	out := GuestBan{Ip: row.Ip, CreatedAt: row.CreatedAt.Time, Reason: row.Reason, EventId: row.EventID}
	if row.ExpiresAt.Valid {
		t := row.ExpiresAt.Time
		out.ExpiresAt = &t
	}
	return out
}

// ListGuestBans lists the addresses banned now.
func (s *Server) ListGuestBans(w http.ResponseWriter, r *http.Request) {
	rows, err := s.q.ListGuestBans(r.Context())
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	out := make([]GuestBan, 0, len(rows))
	for _, row := range rows {
		out = append(out, toGuestBan(row))
	}
	httpx.JSON(w, http.StatusOK, out)
}

// CreateGuestBan bans the address a logged message came from, by hand.
func (s *Server) CreateGuestBan(w http.ResponseWriter, r *http.Request) {
	var body CreateGuestBanJSONBody
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	ctx := r.Context()
	event, err := s.q.GetModerationEvent(ctx, body.EventId)
	if err != nil {
		httpx.Fail(w, r, dbErr(err))
		return
	}
	if event.GuestIp == nil {
		httpx.Fail(w, r, httpx.NewError(http.StatusConflict, "not_a_guest",
			"this message is an account's; disable the account instead"))
		return
	}
	cfg, err := loadModeration(ctx, s.q)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	var ban sqlc.GuestBan
	var expires pgtype.Timestamptz
	if cfg.GuestBanHours > 0 {
		expires = pgtype.Timestamptz{Time: time.Now().Add(time.Duration(cfg.GuestBanHours) * time.Hour), Valid: true}
	}
	ban, err = s.q.UpsertGuestBan(ctx, sqlc.UpsertGuestBanParams{
		Ip: *event.GuestIp, ExpiresAt: expires, Reason: "banned by an administrator", EventID: &event.ID,
	})
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	slog.InfoContext(ctx, "guest address banned", "ip", ban.Ip, "actor", auth.MustUser(ctx).Email)
	httpx.JSON(w, http.StatusOK, toGuestBan(ban))
}

// DeleteGuestBan lifts the ban on an address.
func (s *Server) DeleteGuestBan(w http.ResponseWriter, r *http.Request, params DeleteGuestBanParams) {
	n, err := s.q.DeleteGuestBan(r.Context(), strings.TrimSpace(params.Ip))
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	if n == 0 {
		httpx.Fail(w, r, httpx.ErrNotFound)
		return
	}
	slog.InfoContext(r.Context(), "guest address unbanned", "ip", params.Ip, "actor", auth.MustUser(r.Context()).Email)
	httpx.NoContent(w)
}

// ------------------------------------------------------------ enforcement

// errGuestBanned is the refusal of a guest request from a banned address.
func errGuestBanned(until *time.Time) *httpx.APIError {
	err := httpx.NewError(http.StatusForbidden, "guest_banned", "this network address is banned from guest mode")
	// `until` is always present: a time, or null for a permanent ban.
	var shown any
	if until != nil {
		shown = until.UTC().Format(time.RFC3339)
	}
	err.Details = map[string]any{"until": shown}
	return err
}

// guestBan is the ban in force on the request's address, if any. A lookup that
// fails is no ban: it cannot be told, and the request's own work would fail on
// the same database anyway.
func (s *Server) guestBan(r *http.Request) (*sqlc.GuestBan, bool) {
	ban, err := s.q.GetGuestBan(r.Context(), clientIP(r))
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			slog.WarnContext(r.Context(), "read guest ban", "error", err)
		}
		return nil, false
	}
	return &ban, true
}

// rejectBannedGuests answers 403 guest_banned to every guest request from a
// banned address. A ban past its expiry is ignored whether or not the cleanup
// has removed its row.
func (s *Server) rejectBannedGuests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ban, banned := s.guestBan(r); banned {
			var until *time.Time
			if ban.ExpiresAt.Valid {
				t := ban.ExpiresAt.Time
				until = &t
			}
			httpx.Fail(w, r, errGuestBanned(until))
			return
		}
		next.ServeHTTP(w, r)
	})
}

// moderationRetention is how long a blocked message stays in the log.
const moderationRetention = 90 * 24 * time.Hour

// purgeModeration drops what is past its keeping: the log after 90 days and the
// bans that have expired. Each is one statement with its age in the WHERE, so
// servers sharing the database may all run it.
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
	if n, err := s.q.PurgeExpiredGuestBans(ctx); err != nil {
		slog.WarnContext(ctx, "purge expired guest bans", "error", err)
	} else if n > 0 {
		slog.InfoContext(ctx, "deleted expired guest bans", "count", n)
	}
}
