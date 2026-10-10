package api

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/qunqin/openchat/apps/server/internal/db/sqlc"
)

// How long the cleanup keeps what it removes (cleanup.go), as an administrator
// sets it in System settings. Stored as whole days or hours — the units the
// settings page asks in — in app_settings; unset is the default in cleanup.go.
const (
	settingDeletedRetentionDays = "cleanup_deleted_retention_days"
	settingUnsentUploadHours    = "cleanup_unsent_upload_hours"
	settingOrphanFileHours      = "cleanup_orphan_file_hours"
)

// The ranges a setting may take. A day is the shortest sensible retention for
// deleted conversations; an hour still covers any upload in progress, which
// takes minutes at most. Ten years and a year are ceilings against typos.
const (
	minRetentionDays, maxRetentionDays = 1, 3650
	minCleanupHours, maxCleanupHours   = 1, 8760
)

type retention struct {
	deleted       time.Duration
	unsentUploads time.Duration
	orphanFiles   time.Duration
}

// retentionOf reads the three periods for one cleanup pass. A setting that
// cannot be read is logged and replaced by its default: a pass with the
// default is better than none, and the defaults are what ran before these
// were settings at all.
func retentionOf(ctx context.Context, q *sqlc.Queries) retention {
	read := func(key string, unset int, unit time.Duration) time.Duration {
		n, err := intSettingOr(ctx, q, key, unset)
		if err != nil {
			slog.WarnContext(ctx, "read cleanup setting", "key", key, "error", err)
			n = unset
		}
		return time.Duration(n) * unit
	}
	return retention{
		deleted:       read(settingDeletedRetentionDays, int(deletedRetention/(24*time.Hour)), 24*time.Hour),
		unsentUploads: read(settingUnsentUploadHours, int(unclaimedUploadLifetime/time.Hour), time.Hour),
		orphanFiles:   read(settingOrphanFileHours, int(orphanGrace/time.Hour), time.Hour),
	}
}

func intSettingOr(ctx context.Context, q *sqlc.Queries, key string, unset int) (int, error) {
	raw, err := q.GetSetting(ctx, key)
	if errors.Is(err, pgx.ErrNoRows) {
		return unset, nil
	}
	if err != nil {
		return unset, err
	}
	var value int
	if err := json.Unmarshal(raw, &value); err != nil {
		return unset, err
	}
	return value, nil
}
