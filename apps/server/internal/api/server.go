// Package api implements the generated ServerInterface on top of the database,
// the blob store, and the chat provider.
package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/qunqin/openchat/apps/server/internal/auth"
	"github.com/qunqin/openchat/apps/server/internal/chat"
	"github.com/qunqin/openchat/apps/server/internal/config"
	"github.com/qunqin/openchat/apps/server/internal/db/sqlc"
	"github.com/qunqin/openchat/apps/server/internal/httpx"
	"github.com/qunqin/openchat/apps/server/internal/storage"
)

// Server wires the handlers to their dependencies.
type Server struct {
	cfg      *config.Config
	pool     *pgxpool.Pool
	q        *sqlc.Queries
	store    *storage.Store
	provider chat.Provider
}

var _ ServerInterface = (*Server)(nil)

func NewServer(cfg *config.Config, pool *pgxpool.Pool, store *storage.Store, provider chat.Provider) *Server {
	return &Server{
		cfg:      cfg,
		pool:     pool,
		q:        sqlc.New(pool),
		store:    store,
		provider: provider,
	}
}

// GetHealth reports liveness and, best-effort, database reachability.
func (s *Server) GetHealth(w http.ResponseWriter, r *http.Request) {
	status := HealthStatus("ok")
	if err := s.pool.Ping(r.Context()); err != nil {
		status = HealthStatus("degraded")
	}
	httpx.JSON(w, http.StatusOK, Health{Status: status, Version: s.cfg.Version})
}

// logWarn records a failure that should not fail the request.
func logWarn(r *http.Request, msg string, err error) {
	slog.WarnContext(r.Context(), msg, "error", err, "path", r.URL.Path)
}

// dbErr maps pgx sentinel errors onto the contract's error shape.
func dbErr(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return httpx.ErrNotFound
	}
	return err
}

// isUniqueViolation reports whether the error is Postgres rejecting a duplicate
// key — the only way an email collision surfaces, since checking first would
// still race with a concurrent insert.
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == uniqueViolation
}

// conversationExists returns httpx.ErrNotFound when the id is unknown, deleted,
// or owned by someone else — the three are indistinguishable to the caller by
// design.
func (s *Server) conversationExists(ctx context.Context, id ConversationId) error {
	user := auth.MustUser(ctx)
	if _, err := s.q.GetConversation(ctx, sqlc.GetConversationParams{ID: id, UserID: &user.ID}); err != nil {
		return dbErr(err)
	}
	return nil
}
