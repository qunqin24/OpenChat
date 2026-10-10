// Package api implements the generated ServerInterface on top of the database,
// the blob store, and the agent runner.
package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/qunqin/openchat/apps/server/internal/agent"
	"github.com/qunqin/openchat/apps/server/internal/auth"
	"github.com/qunqin/openchat/apps/server/internal/config"
	"github.com/qunqin/openchat/apps/server/internal/db/sqlc"
	"github.com/qunqin/openchat/apps/server/internal/httpx"
	"github.com/qunqin/openchat/apps/server/internal/storage"
	"github.com/qunqin/openchat/apps/server/internal/usage"
)

// Server wires the handlers to their dependencies.
type Server struct {
	cfg   *config.Config
	pool  *pgxpool.Pool
	q     *sqlc.Queries
	store blobStore
	// local is the store's local driver, which is what serves the signed URLs
	// of files kept on this server's disk. Nil when the store has none.
	local func() *storage.Local
	// storageSwitch is the store again when it can be reconfigured while
	// running, which is what the admin storage settings need; nil otherwise.
	storageSwitch *storage.Switch
	// storageMu serializes changes to the storage settings.
	storageMu sync.Mutex
	runner    *agent.Runner
	// avatars holds the read URLs issued for account pictures.
	avatars avatarCache
	// usage is the ledger of model requests, for the ones the server makes
	// itself (titles). Runs write theirs through the runner.
	usage *usage.Recorder
	// now is the clock the spending caps' periods are read from; time.Now
	// unless a test has set another.
	now func() time.Time
}

var _ ServerInterface = (*Server)(nil)

func NewServer(cfg *config.Config, pool *pgxpool.Pool, store storage.Backend, runner *agent.Runner) *Server {
	s := &Server{
		cfg:    cfg,
		pool:   pool,
		q:      sqlc.New(pool),
		store:  store,
		local:  func() *storage.Local { return nil },
		runner: runner,
		usage:  usage.NewRecorder(sqlc.New(pool)),
	}
	if runner != nil {
		// One ledger, and one set of holds against the caps, for every request
		// the process makes.
		s.usage = runner.Recorder()
	}
	switch store := store.(type) {
	case *storage.Switch:
		s.storageSwitch = store
		s.local = store.Local
	case *storage.Local:
		s.local = func() *storage.Local { return store }
	}
	return s
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

// validateUsername trims a username and rejects the two values the column will
// not hold: empty, and anything containing `@`.
//
// The `@` rule is what keeps usernames and email addresses in disjoint name
// spaces, which sign-in depends on — it takes either one in a single field. A
// CHECK constraint enforces the same rule, so this is the readable error rather
// than the guarantee.
func validateUsername(raw string) (string, error) {
	name := strings.TrimSpace(raw)
	switch {
	case name == "":
		return "", httpx.NewFieldError("name", "name_required", "a username is required")
	case strings.ContainsRune(name, '@'):
		return "", httpx.NewFieldError("name", "name_invalid", "a username cannot contain @")
	}
	return name, nil
}

// conflictingField names the input a duplicate-key error blames, or "" when the
// error is something else.
//
// Checking for a collision before writing would still race with a concurrent
// insert, so the unique index is the check and this reads its verdict. Two
// indexes can fire, and telling a form "that email is taken" when the username
// is what clashed sends the person to fix the wrong field.
func conflictingField(err error) string {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != uniqueViolation {
		return ""
	}
	switch pgErr.ConstraintName {
	case "users_name_lower_idx":
		return "name"
	case "users_email_lower_idx":
		return "email"
	default:
		// A unique index we have not taught this function about. Blaming a
		// field at random would be worse than owning up to a server error.
		return ""
	}
}

// duplicateError turns a duplicate-key error into the field error its form can
// show, or nil when the error was not a duplicate.
func duplicateError(err error) error {
	switch conflictingField(err) {
	case "email":
		return httpx.NewFieldError("email", "email_taken", "that email is already registered")
	case "name":
		return httpx.NewFieldError("name", "name_taken", "that username is already taken")
	default:
		return nil
	}
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
