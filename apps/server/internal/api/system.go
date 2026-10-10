package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/qunqin/openchat/apps/server/internal/auth"
	"github.com/qunqin/openchat/apps/server/internal/db/sqlc"
	"github.com/qunqin/openchat/apps/server/internal/httpx"
	"github.com/qunqin/openchat/apps/server/internal/logging"
	"github.com/qunqin/openchat/apps/server/internal/memory"
)

// settingDebugLogging is the app_settings key holding the log-level switch.
const settingDebugLogging = "debug_logging"

// LoadSettings applies the stored settings to the running process. Called once
// at boot, so a switch left on survives the restart it was flipped before.
func (s *Server) LoadSettings(ctx context.Context) {
	on, err := s.boolSetting(ctx, settingDebugLogging)
	if err != nil {
		// A missing or unreadable setting is not worth refusing to start over;
		// the default is the quiet one.
		slog.WarnContext(ctx, "read debug_logging setting", "error", err)
		return
	}
	logging.SetDebug(on)
	if on {
		slog.InfoContext(ctx, "debug logging is on", "source", "app_settings")
	}
}

func (s *Server) boolSetting(ctx context.Context, key string) (bool, error) {
	return boolSetting(ctx, s.q, key)
}

func boolSetting(ctx context.Context, q *sqlc.Queries, key string) (bool, error) {
	return boolSettingOr(ctx, q, key, false)
}

// boolSettingOr is a switch that has not been set to what it should be: a
// feature that is on until an administrator turns it off.
func boolSettingOr(ctx context.Context, q *sqlc.Queries, key string, unset bool) (bool, error) {
	raw, err := q.GetSetting(ctx, key)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return unset, nil
		}
		return unset, err
	}
	var value bool
	if err := json.Unmarshal(raw, &value); err != nil {
		return false, err
	}
	return value, nil
}

func (s *Server) GetSystemSettings(w http.ResponseWriter, r *http.Request) {
	settings, err := s.systemSettings(r.Context())
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, settings)
}

func (s *Server) systemSettings(ctx context.Context) (SystemSettings, error) {
	// Read the live level rather than the column: they agree, and if they ever
	// disagree the one that matters is the one actually filtering log lines.
	settings := SystemSettings{DebugLogging: logging.Debug()}
	generativeUI, err := boolSetting(ctx, s.q, settingGenerativeUI)
	if err != nil {
		return settings, err
	}
	settings.GenerativeUi = generativeUI
	if settings.AskUser, err = boolSettingOr(ctx, s.q, settingAskUser, true); err != nil {
		return settings, err
	}
	if settings.Memory, err = boolSettingOr(ctx, s.q, memory.SettingEnabled, true); err != nil {
		return settings, err
	}
	if settings.AvatarUploads, err = boolSettingOr(ctx, s.q, settingAvatarUploads, false); err != nil {
		return settings, err
	}
	if settings.TaskModel, err = stringSetting(ctx, s.q, settingTaskModel); err != nil {
		return settings, err
	}
	if err := guestSettings(ctx, s.q, &settings); err != nil {
		return settings, err
	}
	keep := retentionOf(ctx, s.q)
	settings.DeletedRetentionDays = int(keep.deleted / (24 * time.Hour))
	settings.UnsentUploadHours = int(keep.unsentUploads / time.Hour)
	settings.OrphanFileHours = int(keep.orphanFiles / time.Hour)
	return settings, nil
}

func (s *Server) UpdateSystemSettings(w http.ResponseWriter, r *http.Request) {
	var body UpdateSystemSettingsRequest
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.Fail(w, r, err)
		return
	}

	// The guest limits likewise: nothing is saved if one of them is out of range.
	if err := validGuestLimits(body.GuestHourlyLimit, body.GuestDailyLimit); err != nil {
		httpx.Fail(w, r, err)
		return
	}

	// The periods are checked together before anything is saved, so a form
	// that sends all three with one out of range changes none of them.
	periods := []struct {
		value    *int
		key      string
		min, max int
	}{
		{body.DeletedRetentionDays, settingDeletedRetentionDays, minRetentionDays, maxRetentionDays},
		{body.UnsentUploadHours, settingUnsentUploadHours, minCleanupHours, maxCleanupHours},
		{body.OrphanFileHours, settingOrphanFileHours, minCleanupHours, maxCleanupHours},
	}
	for _, p := range periods {
		if p.value != nil && (*p.value < p.min || *p.value > p.max) {
			httpx.Fail(w, r, httpx.NewError(http.StatusBadRequest, "invalid_retention",
				fmt.Sprintf("%s must be between %d and %d", p.key, p.min, p.max)))
			return
		}
	}
	for _, p := range periods {
		if p.value == nil {
			continue
		}
		value, _ := json.Marshal(*p.value)
		if err := s.q.SetSetting(r.Context(), sqlc.SetSettingParams{Key: p.key, Value: value}); err != nil {
			httpx.Fail(w, r, err)
			return
		}
		slog.InfoContext(r.Context(), "cleanup period changed",
			"setting", p.key, "value", *p.value, "actor", auth.MustUser(r.Context()).Email)
	}

	if body.DebugLogging != nil {
		value, err := json.Marshal(*body.DebugLogging)
		if err != nil {
			httpx.Fail(w, r, err)
			return
		}
		// Persisted before applied: a switch that took effect but was not saved
		// would come back off after a restart, with nothing to explain why.
		if err := s.q.SetSetting(r.Context(), sqlc.SetSettingParams{Key: settingDebugLogging, Value: value}); err != nil {
			httpx.Fail(w, r, err)
			return
		}
		logging.SetDebug(*body.DebugLogging)

		// Logged at Info so the line survives switching debug back off, which is
		// the case where you most want to know when it happened.
		slog.InfoContext(r.Context(), "debug logging changed",
			"enabled", *body.DebugLogging, "actor", auth.MustUser(r.Context()).Email)
	}

	if body.GenerativeUi != nil {
		value, _ := json.Marshal(*body.GenerativeUi)
		if err := s.q.SetSetting(r.Context(), sqlc.SetSettingParams{Key: settingGenerativeUI, Value: value}); err != nil {
			httpx.Fail(w, r, err)
			return
		}
		slog.InfoContext(r.Context(), "generative ui changed",
			"enabled", *body.GenerativeUi, "actor", auth.MustUser(r.Context()).Email)
	}

	if body.AskUser != nil {
		value, _ := json.Marshal(*body.AskUser)
		if err := s.q.SetSetting(r.Context(), sqlc.SetSettingParams{Key: settingAskUser, Value: value}); err != nil {
			httpx.Fail(w, r, err)
			return
		}
		slog.InfoContext(r.Context(), "ask_user changed",
			"enabled", *body.AskUser, "actor", auth.MustUser(r.Context()).Email)
	}

	if body.AvatarUploads != nil {
		value, _ := json.Marshal(*body.AvatarUploads)
		if err := s.q.SetSetting(r.Context(), sqlc.SetSettingParams{Key: settingAvatarUploads, Value: value}); err != nil {
			httpx.Fail(w, r, err)
			return
		}
		slog.InfoContext(r.Context(), "avatar uploads changed",
			"enabled", *body.AvatarUploads, "actor", auth.MustUser(r.Context()).Email)
	}

	for _, g := range []struct {
		key   string
		value any
		log   string
	}{
		{settingGuestMode, body.GuestMode, "guest mode changed"},
		{settingGuestHourlyLimit, body.GuestHourlyLimit, "guest hourly limit changed"},
		{settingGuestDailyLimit, body.GuestDailyLimit, "guest daily limit changed"},
	} {
		var (
			value []byte
			shown any
		)
		switch v := g.value.(type) {
		case *bool:
			if v == nil {
				continue
			}
			value, _ = json.Marshal(*v)
			shown = *v
		case *int:
			if v == nil {
				continue
			}
			value, _ = json.Marshal(*v)
			shown = *v
		}
		if err := s.q.SetSetting(r.Context(), sqlc.SetSettingParams{Key: g.key, Value: value}); err != nil {
			httpx.Fail(w, r, err)
			return
		}
		slog.InfoContext(r.Context(), g.log, "value", shown, "actor", auth.MustUser(r.Context()).Email)
	}

	if body.Memory != nil {
		value, _ := json.Marshal(*body.Memory)
		if err := s.q.SetSetting(r.Context(), sqlc.SetSettingParams{Key: memory.SettingEnabled, Value: value}); err != nil {
			httpx.Fail(w, r, err)
			return
		}
		slog.InfoContext(r.Context(), "memory changed",
			"enabled", *body.Memory, "actor", auth.MustUser(r.Context()).Email)
	}

	if body.TaskModel != nil {
		task := strings.TrimSpace(*body.TaskModel)
		// Only a model something can answer with; empty is each
		// conversation's own, and always available.
		if task != "" {
			if _, _, err := s.pickChannel(r.Context(), task, true); err != nil {
				httpx.Fail(w, r, err)
				return
			}
		}
		value, _ := json.Marshal(task)
		if err := s.q.SetSetting(r.Context(), sqlc.SetSettingParams{Key: settingTaskModel, Value: value}); err != nil {
			httpx.Fail(w, r, err)
			return
		}
		slog.InfoContext(r.Context(), "task model changed", "model", task, "actor", auth.MustUser(r.Context()).Email)
	}

	settings, err := s.systemSettings(r.Context())
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, settings)
}

// settingGenerativeUI turns interface components on: with it, every request's
// system prompt teaches the openchat-ui block. Off by default — it costs a few
// thousand prompt tokens on every request whether or not a reply uses it.
const settingGenerativeUI = "generative_ui"

// settingAskUser offers models the ask_user tool. On unless an administrator
// has turned it off: it costs a short tool definition per request and nothing
// when a model has no question to ask.
const settingAskUser = "ask_user"

// settingAvatarUploads lets people upload a picture of their own. Off unless
// an administrator turns it on; without one, the first letter of the name is
// shown, and a picture already set can still be removed.
const settingAvatarUploads = "avatar_uploads"

func stringSetting(ctx context.Context, q *sqlc.Queries, key string) (string, error) {
	raw, err := q.GetSetting(ctx, key)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", nil
		}
		return "", err
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", err
	}
	return value, nil
}
