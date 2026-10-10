package api

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

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
	if settings.TaskModel, err = stringSetting(ctx, s.q, settingTaskModel); err != nil {
		return settings, err
	}
	return settings, nil
}

func (s *Server) UpdateSystemSettings(w http.ResponseWriter, r *http.Request) {
	var body UpdateSystemSettingsRequest
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.Fail(w, r, err)
		return
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
			if _, _, err := s.pickChannel(r.Context(), task); err != nil {
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
