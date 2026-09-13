package api

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5"

	"github.com/qunqin/openchat/apps/server/internal/auth"
	"github.com/qunqin/openchat/apps/server/internal/db/sqlc"
	"github.com/qunqin/openchat/apps/server/internal/httpx"
	"github.com/qunqin/openchat/apps/server/internal/logging"
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
	raw, err := s.q.GetSetting(ctx, key)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	var value bool
	if err := json.Unmarshal(raw, &value); err != nil {
		return false, err
	}
	return value, nil
}

func (s *Server) GetSystemSettings(w http.ResponseWriter, r *http.Request) {
	// Read the live level rather than the column: they agree, and if they ever
	// disagree the one that matters is the one actually filtering log lines.
	httpx.JSON(w, http.StatusOK, SystemSettings{DebugLogging: logging.Debug()})
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

	httpx.JSON(w, http.StatusOK, SystemSettings{DebugLogging: logging.Debug()})
}
