package api

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/qunqin24/openchat/apps/server/internal/auth"
	"github.com/qunqin24/openchat/apps/server/internal/db/sqlc"
	"github.com/qunqin24/openchat/apps/server/internal/httpx"
)

var errSelfDelete = httpx.NewError(http.StatusConflict, "self_delete", "you cannot delete your own account")

// DeleteUser deletes an account for good, with everything that is only its
// (DeleteAccount): an administrator's way to remove a person, where disabling
// keeps them. Its requests stay in the usage log under the name it had.
//
// The steps are the ones every deletion here takes. In one transaction, under
// the admin-membership lock (as any change that can take an administrator
// away): the runs it has going are marked stopped, its ledger rows labelled,
// the account deleted with its storage keys handed back, and the enabled
// administrators counted on what the commit would leave. After the commit the
// runner is told to stop those runs, and the files - attachments and picture -
// are removed, best effort; the orphan sweep is the backstop for the disk.
func (s *Server) DeleteUser(w http.ResponseWriter, r *http.Request, userId openapi_types.UUID) {
	ctx := r.Context()
	actor := auth.MustUser(ctx)
	if userId == actor.ID {
		httpx.Fail(w, r, errSelfDelete)
		return
	}

	var (
		stopped []uuid.UUID
		files   []string
		target  sqlc.User
	)
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		if err := q.LockAdminMembership(ctx); err != nil {
			return err
		}
		var err error
		if target, err = q.GetUser(ctx, userId); err != nil {
			return dbErr(err)
		}
		if stopped, err = q.CancelUserRuns(ctx, userId); err != nil {
			return err
		}
		if err := q.LabelDeletedUserUsage(ctx, sqlc.LabelDeletedUserUsageParams{UserID: &userId, Name: &target.Name}); err != nil {
			return err
		}
		gone, err := q.DeleteAccount(ctx, &userId)
		if err != nil {
			return dbErr(err)
		}
		files = gone.StorageKeys
		if gone.AvatarKey != nil {
			files = append(files, *gone.AvatarKey)
		}
		if target.Role == sqlc.UserRoleAdmin && !target.DisabledAt.Valid {
			admins, err := q.CountAdmins(ctx)
			if err != nil {
				return err
			}
			if admins == 0 {
				return errLastAdmin
			}
		}
		return nil
	})
	if err != nil {
		if !errors.Is(err, httpx.ErrNotFound) {
			var api *httpx.APIError
			if !errors.As(err, &api) {
				slog.ErrorContext(ctx, "delete account", "user_id", userId, "error", err)
			}
		}
		httpx.Fail(w, r, err)
		return
	}

	for _, id := range stopped {
		s.runner.Cancel(id)
	}
	removed := s.removeFiles(ctx, files)
	slog.InfoContext(ctx, "account deleted",
		"user_id", userId, "role", target.Role, "runs_stopped", len(stopped), "files", removed, "actor", actor.Email)
	w.WriteHeader(http.StatusNoContent)
}
