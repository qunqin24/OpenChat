package auth

import (
	"context"

	"github.com/qunqin/openchat/apps/server/internal/db/sqlc"
)

type contextKey struct{}

var userKey contextKey

// WithUser returns a context carrying the authenticated user.
func WithUser(ctx context.Context, user sqlc.User) context.Context {
	return context.WithValue(ctx, userKey, user)
}

// UserFrom returns the authenticated user, if the request went through Require.
func UserFrom(ctx context.Context) (sqlc.User, bool) {
	user, ok := ctx.Value(userKey).(sqlc.User)
	return user, ok
}

// MustUser returns the authenticated user and panics if the middleware did not
// run. Handlers behind Require can use it without an error branch that can
// never be taken; Recoverer turns a wiring mistake into a 500 rather than a
// silent nil user.
func MustUser(ctx context.Context) sqlc.User {
	user, ok := UserFrom(ctx)
	if !ok {
		panic("auth: no user on context — handler is not behind auth.Require")
	}
	return user
}
