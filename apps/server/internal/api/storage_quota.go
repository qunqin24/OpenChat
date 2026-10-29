package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sort"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/qunqin24/openchat/apps/server/internal/auth"
	"github.com/qunqin24/openchat/apps/server/internal/db/sqlc"
	"github.com/qunqin24/openchat/apps/server/internal/httpx"
)

// How much each person may keep in files. Every upload counts - sent or not
// yet, in a kept conversation or a temporary one - since all of it is stored;
// an account has the instance's default (settingStorageQuota, 5 GiB unless an
// administrator changes it) or a quota of its own (users.storage_quota_bytes).
// An upload past it is refused when it is asked for (CreateUpload), with the
// size it declares - which is also the most it can store (the local PUT stops
// there, the bucket's signed URL carries it as Content-Length) - and again when
// a message claims it, with the size it was stored at (admitAttachments). Both
// count under LockUserStorage, so uploads at once cannot share the room left
// for one.
// Lowering a quota deletes nothing; the person makes room in the library.

const (
	settingStorageQuota = "storage_quota"
	defaultStorageQuota = int64(5) << 30
)

var errStorageFull = httpx.NewError(http.StatusRequestEntityTooLarge, "storage_quota_exceeded",
	"your files have used up your storage: delete some from the library to make room")

// defaultQuota is the instance's default quota.
func defaultQuota(ctx context.Context, q *sqlc.Queries) (int64, error) {
	raw, err := q.GetSetting(ctx, settingStorageQuota)
	if errors.Is(err, pgx.ErrNoRows) {
		return defaultStorageQuota, nil
	}
	if err != nil {
		return 0, err
	}
	var value int64
	if err := json.Unmarshal(raw, &value); err != nil || value < 0 {
		return defaultStorageQuota, nil
	}
	return value, nil
}

// storageQuota is how much an account may keep: its own quota or the default.
func storageQuota(ctx context.Context, q *sqlc.Queries, userID uuid.UUID) (int64, error) {
	own, err := q.GetUserStorageQuota(ctx, userID)
	if err != nil {
		return 0, err
	}
	if own != nil {
		return *own, nil
	}
	return defaultQuota(ctx, q)
}

// roomFor refuses, with the account's storage locked, an addition of bytes
// that would take its files past its quota. q is a transaction's: the lock
// holds until it ends, and whatever it adds is counted by the next.
func roomFor(ctx context.Context, q *sqlc.Queries, userID uuid.UUID, bytes int64) error {
	if err := q.LockUserStorage(ctx, userID); err != nil {
		return err
	}
	used, err := q.UserStorageUsed(ctx, &userID)
	if err != nil {
		return err
	}
	quota, err := storageQuota(ctx, q, userID)
	if err != nil {
		return err
	}
	if used+bytes > quota {
		return errStorageFull
	}
	return nil
}

// GetMyStorage is what the caller's files take, against their quota.
func (s *Server) GetMyStorage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user := auth.MustUser(ctx)
	rows, err := s.q.UserStorageByKind(ctx, &user.ID)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	quota, err := storageQuota(ctx, s.q, user.ID)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	out := MyStorage{QuotaBytes: quota, Kinds: []StorageKindUsage{}}
	for _, row := range rows {
		out.UsedBytes += row.Bytes
		out.Files += row.Files
		out.Kinds = append(out.Kinds, StorageKindUsage{Kind: StorageKindUsageKind(row.Kind), Files: row.Files, Bytes: row.Bytes})
	}
	sort.Slice(out.Kinds, func(i, j int) bool { return out.Kinds[i].Bytes > out.Kinds[j].Bytes })
	httpx.JSON(w, http.StatusOK, out)
}

func (s *Server) GetStorageQuota(w http.ResponseWriter, r *http.Request) {
	quota, err := defaultQuota(r.Context(), s.q)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, StorageQuotaSettings{DefaultBytes: quota})
}

func (s *Server) UpdateStorageQuota(w http.ResponseWriter, r *http.Request) {
	var body StorageQuotaSettings
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	if body.DefaultBytes < 0 {
		httpx.Fail(w, r, httpx.NewFieldError("defaultBytes", "invalid_quota", "a quota cannot be negative"))
		return
	}
	ctx := r.Context()
	value, _ := json.Marshal(body.DefaultBytes)
	if err := s.q.SetSetting(ctx, sqlc.SetSettingParams{Key: settingStorageQuota, Value: value}); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	slog.InfoContext(ctx, "storage quota changed", "default_bytes", body.DefaultBytes, "actor", auth.MustUser(ctx).Email)
	httpx.JSON(w, http.StatusOK, body)
}

// setUserQuota gives an account its own quota, or the default again for -1.
func setUserQuota(ctx context.Context, q *sqlc.Queries, userID uuid.UUID, bytes int64) error {
	if bytes < -1 {
		return httpx.NewFieldError("storageQuotaBytes", "invalid_quota", "a quota cannot be negative")
	}
	var own *int64
	if bytes >= 0 {
		own = &bytes
	}
	if err := q.SetUserStorageQuota(ctx, sqlc.SetUserStorageQuotaParams{ID: userID, StorageQuotaBytes: own}); err != nil {
		return fmt.Errorf("set storage quota: %w", err)
	}
	return nil
}
