package auth

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5"

	"github.com/qunqin/openchat/apps/server/internal/db/sqlc"
	"github.com/qunqin/openchat/apps/server/internal/httpx"
	"github.com/qunqin/openchat/apps/server/internal/logging"
)

// Middleware turns the session cookie into a user on the request context.
type Middleware struct {
	q *sqlc.Queries
}

func NewMiddleware(q *sqlc.Queries) *Middleware { return &Middleware{q: q} }

// Require rejects anything without a valid session. A disabled account fails
// here too, because the session lookup joins on `disabled_at IS NULL`.
func (m *Middleware) Require(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := TokenFromRequest(r)
		if token == "" {
			// A 401 with no explanation is the single most common thing to be
			// puzzled by here, and the three causes are indistinguishable from
			// the outside: no cookie at all, an expired or revoked session, or a
			// disabled account. Debug logging tells them apart.
			slog.DebugContext(r.Context(), "rejected: no session cookie", "path", r.URL.Path)
			httpx.Fail(w, r, httpx.ErrUnauthorized)
			return
		}

		row, err := m.q.GetSessionUser(r.Context(), HashToken(token))
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				// Expired, revoked, or the account was disabled. Clear the
				// cookie so the browser stops sending a token that is dead.
				slog.DebugContext(r.Context(), "rejected: session not valid",
					"path", r.URL.Path,
					"reason", "expired, revoked, or the account is disabled")
				ClearCookie(w, r.TLS != nil)
				httpx.Fail(w, r, httpx.ErrUnauthorized)
				return
			}
			httpx.Fail(w, r, err)
			return
		}

		ctx := WithUser(r.Context(), row.User)
		ctx = WithSessionID(ctx, row.SessionID)
		// From here on every line this request logs says whose it was.
		logging.Annotate(ctx, slog.String("user_id", row.User.ID.String()))
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RequireAdmin gates the admin API. It must be mounted behind Require.
func (m *Middleware) RequireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, ok := UserFrom(r.Context())
		if !ok {
			httpx.Fail(w, r, httpx.ErrUnauthorized)
			return
		}
		if user.Role != sqlc.UserRoleAdmin {
			slog.DebugContext(r.Context(), "rejected: not an admin",
				"path", r.URL.Path, "user", user.Email, "role", user.Role)
			// Deliberately "not found", not "forbidden": a 403 confirms the
			// admin API exists to someone who has no business knowing.
			httpx.Fail(w, r, httpx.ErrNotFound)
			return
		}
		next.ServeHTTP(w, r)
	})
}
