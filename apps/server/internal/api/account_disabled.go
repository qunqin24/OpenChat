package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/qunqin24/openchat/apps/server/internal/db/sqlc"
	"github.com/qunqin24/openchat/apps/server/internal/httpx"
)

// A disabled account is not signed out: its sessions are kept, and every
// request one of them makes is answered with 403 account_disabled and why
// (auth.Middleware.OnDisabled), so the page can show the person what happened
// instead of dropping them at the sign-in form. The session can do nothing
// else, and signs out as any other (Logout needs no live account). Enabling
// the account again lets the same session in. Signing in to a disabled
// account, once the password is proved, is answered the same way.

// disabledNoteRunes bounds what an administrator writes when disabling.
const disabledNoteRunes = 500

var errInvalidDisabledNote = httpx.NewFieldError("disabledNote", "invalid_disabled_note",
	"the note can be at most 500 characters")

// disabledNote is the note an update asks for: nil for none or blank.
func disabledNote(note *string) (*string, error) {
	if note == nil {
		return nil, nil
	}
	trimmed := strings.TrimSpace(*note)
	if trimmed == "" {
		return nil, nil
	}
	if utf8.RuneCountInString(trimmed) > disabledNoteRunes {
		return nil, errInvalidDisabledNote
	}
	return &trimmed, nil
}

// accountDisabled is the refusal a disabled account gets: who disabled it and
// when, the administrator's note, or for a moderation ban what set it off, and
// whom to ask. What cannot be read is left out; the refusal itself stands.
func (s *Server) accountDisabled(ctx context.Context, user sqlc.User) *httpx.APIError {
	details := AccountDisabled{Reason: DisabledReasonAdmin, DisabledAt: user.DisabledAt.Time, Admin: s.disablingAdmin(ctx, user)}
	if user.DisabledNote != nil && *user.DisabledNote != "" {
		details.Note = user.DisabledNote
	}
	if user.DisabledReason != nil && *user.DisabledReason == string(DisabledReasonModeration) {
		details.Reason = DisabledReasonModeration
		cfg, err := loadModeration(ctx, s.q)
		if err != nil {
			cfg = defaultModerationConfig()
		}
		summary, err := s.q.ModerationBanSummary(ctx, sqlc.ModerationBanSummaryParams{
			UserID:      &user.ID,
			Until:       user.DisabledAt,
			WindowHours: int32(cfg.StrikeWindowHours),
		})
		if err != nil {
			slog.WarnContext(ctx, "read what set a ban off", "error", err, "user_id", user.ID)
		} else {
			strikes, hours := int(summary.Strikes), cfg.StrikeWindowHours
			details.Strikes, details.WindowHours, details.Categories = &strikes, &hours, &summary.Categories
		}
	}
	refusal := *errAccountDisabled
	refusal.Details = detailsMap(details)
	return &refusal
}

// disablingAdmin is whom a disabled account is told to ask: the administrator
// who disabled it, while they still are one; otherwise (a moderation ban, or
// they are gone) the one any refused account is told of (contactAdmin).
func (s *Server) disablingAdmin(ctx context.Context, user sqlc.User) *AdminContact {
	if user.DisabledBy != nil {
		by, err := s.q.GetUser(ctx, *user.DisabledBy)
		if err == nil && by.Role == sqlc.UserRoleAdmin && !by.DisabledAt.Valid && !by.Pending {
			return &AdminContact{Name: by.Name, Email: by.Email}
		}
	}
	return s.contactAdmin(ctx)
}

// accountDisabledByID is accountDisabled for an account known by id; the bare
// refusal when it cannot be read.
func (s *Server) accountDisabledByID(ctx context.Context, id uuid.UUID) *httpx.APIError {
	user, err := s.q.GetUser(ctx, id)
	if err != nil || !user.DisabledAt.Valid {
		return errAccountDisabled
	}
	return s.accountDisabled(ctx, user)
}

// failDisabled answers a request of a disabled account's session.
func (s *Server) failDisabled(w http.ResponseWriter, r *http.Request, user sqlc.User) {
	httpx.Fail(w, r, s.accountDisabled(r.Context(), user))
}

// detailsMap is a typed body as an error's details.
func detailsMap(v any) map[string]any {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil
	}
	return out
}
