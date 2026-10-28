package agent

import (
	"context"
	"sync"

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
	// Parent is set on a sub-agent's run: the conversation it works for. Its
	// own conversation (ConversationID) is the hidden child.
	Parent *uuid.UUID
	// State is the run's own, shared by its tool calls and its turn note.
	State *RunState
}

// Subagent reports whether the scope is a sub-agent's run.
func (s Scope) Subagent() bool { return s.Parent != nil }

// RunState is what one run keeps between its tool calls - which version of a
// document the model has been shown, say. It lives as long as the run's
// execution in this process: a run resumed after a restart starts it empty, so
// what it holds must be safe to forget. Nil holds nothing.
type RunState struct {
	m sync.Map
}

// Load is what key holds, if anything.
func (s *RunState) Load(key any) (any, bool) {
	if s == nil {
		return nil, false
	}
	return s.m.Load(key)
}

// Store sets what key holds.
func (s *RunState) Store(key, value any) {
	if s != nil {
		s.m.Store(key, value)
	}
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

type callKey struct{}

// withCall returns ctx carrying the id of the tool call it executes.
func withCall(ctx context.Context, callID string) context.Context {
	return context.WithValue(ctx, callKey{}, callID)
}

// ToolCallID is the id of the tool call ctx executes, "" outside one: what a
// tool that must not act twice for one call keys its effect by.
func ToolCallID(ctx context.Context) string {
	id, _ := ctx.Value(callKey{}).(string)
	return id
}
