package auth

import (
	"errors"
	"net/http"

	"github.com/jackc/pgx/v5"

	"github.com/qunqin/openchat/apps/server/internal/db/sqlc"
	"github.com/qunqin/openchat/apps/server/internal/httpx"
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
			httpx.Fail(w, r, httpx.ErrUnauthorized)
			return
		}

		row, err := m.q.GetSessionUser(r.Context(), HashToken(token))
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				// Expired, revoked, or the account was disabled. Clear the
				// cookie so the browser stops sending a token that is dead.
				ClearCookie(w, r.TLS != nil)
				httpx.Fail(w, r, httpx.ErrUnauthorized)
				return
			}
			httpx.Fail(w, r, err)
			return
		}

		next.ServeHTTP(w, r.WithContext(WithUser(r.Context(), row.User)))
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
			// Deliberately "not found", not "forbidden": a 403 confirms the
			// admin API exists to someone who has no business knowing.
			httpx.Fail(w, r, httpx.ErrNotFound)
			return
		}
		next.ServeHTTP(w, r)
	})
}
