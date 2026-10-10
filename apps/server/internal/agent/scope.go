package agent

import (
	"context"

	"github.com/google/uuid"
)

// Scope says whose run a context belongs to. The runner puts it on the context
// it hands to the tool source, the system prompt and every tool call, because
// those are written as plain functions of a context and a tool is a value
// shared by every run: a tool that acts for one person — saving a memory — has
// to learn who from the call itself, never from its arguments.
type Scope struct {
	RunID          uuid.UUID
	UserID         uuid.UUID
	ConversationID uuid.UUID
}

type scopeKey struct{}

// WithScope returns ctx carrying the scope of a run.
func WithScope(ctx context.Context, scope Scope) context.Context {
	return context.WithValue(ctx, scopeKey{}, scope)
}

// ScopeFrom is the scope of the run ctx belongs to; ok is false outside a run.
func ScopeFrom(ctx context.Context) (Scope, bool) {
	scope, ok := ctx.Value(scopeKey{}).(Scope)
	return scope, ok
}
