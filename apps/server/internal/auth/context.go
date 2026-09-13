package auth

import (
	"context"

	"github.com/google/uuid"

	"github.com/qunqin/openchat/apps/server/internal/db/sqlc"
)

// Distinct zero-sized types rather than values of one type, so the two keys
// cannot collide and neither can be forged from outside this package.
type (
	userKey    struct{}
	sessionKey struct{}
)

// WithUser returns a context carrying the authenticated user.
func WithUser(ctx context.Context, user sqlc.User) context.Context {
	return context.WithValue(ctx, userKey{}, user)
}

// UserFrom returns the authenticated user, if the request went through Require.
func UserFrom(ctx context.Context) (sqlc.User, bool) {
	user, ok := ctx.Value(userKey{}).(sqlc.User)
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

// WithSessionID returns a context carrying the id of the session the request
// arrived on, so a handler can spare it when revoking the others.
func WithSessionID(ctx context.Context, id uuid.UUID) context.Context {
	return context.WithValue(ctx, sessionKey{}, id)
}

// SessionIDFrom returns the current session's id, if the request went through
// Require.
func SessionIDFrom(ctx context.Context) (uuid.UUID, bool) {
	id, ok := ctx.Value(sessionKey{}).(uuid.UUID)
	return id, ok
}
