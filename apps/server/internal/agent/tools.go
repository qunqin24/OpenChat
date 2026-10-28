package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/qunqin24/openchat/apps/server/internal/llm"
)

// Tool is something a model may call during a run.
//
// Being offered to a model is visibility, not permission: the tool still checks
// its own arguments, and anything a call points at, when it executes.
type Tool interface {
	// Definition is what the model is told: a name, what it is for, and a JSON
	// Schema for its arguments.
	Definition() llm.Tool

	// Replayable reports whether a call that was running when the process died
	// may simply run again. True only for tools with no effect beyond reading —
	// a search, a page fetch. Anything else gets an `interrupted` result instead,
	// because the effect may already have happened (pi-durable's replay policy).
	Replayable() bool

	// Execute runs one call. args is the call's JSON object, already known to
	// parse. A returned error becomes an error result the model can read and
	// recover from; it never fails the run. ctx ends when the run is cancelled.
	Execute(ctx context.Context, args json.RawMessage) (ToolResult, error)
}

// ToolResult is what a call produced.
type ToolResult struct {
	// Content is what the model reads.
	Content string
	// IsError reports a failure without an error value, for a tool that has
	// something useful to say about why — "no results", "page too large".
	IsError bool
	// Details is for the UI only and never reaches a model: the result list of a
	// search, the title of a fetched page.
	Details any
}

// ToolError is an error whose message is meant for the model. Anything else a
// tool returns is logged and reported to the model generically, because an
// internal error string may carry a path, an address or a credential.
type ToolError struct{ Message string }

func (e *ToolError) Error() string { return e.Message }

// Errorf builds a ToolError.
func Errorf(format string, args ...any) error {
	return &ToolError{Message: fmt.Sprintf(format, args...)}
}

// DecodeArgs unmarshals a call's arguments, reporting a mismatch in words the
// model can act on. Unknown fields are allowed: models add them, and refusing a
// call over an extra key the tool ignores anyway helps nobody.
func DecodeArgs(raw json.RawMessage, into any) error {
	if err := json.Unmarshal(raw, into); err != nil {
		var typeErr *json.UnmarshalTypeError
		if errors.As(err, &typeErr) && typeErr.Field != "" {
			return Errorf("invalid arguments: %q must be a %s", typeErr.Field, typeErr.Type.Kind())
		}
		return Errorf("invalid arguments: %v", err)
	}
	return nil
}

// ToolAskUser is the tool that puts a question to the person. The runner
// knows it by name where it must: a call left without a result is read as a
// question that was skipped, not as a tool that was interrupted (BuildContext).
const ToolAskUser = "ask_user"

// UserTool is implemented by a tool the person answers, not the server: its
// calls are not executed. The run runs the other calls of the response, then
// stops with status `awaiting_input` and the call on the transcript with no
// result; the answer arrives as a request of its own, which writes the result
// and starts the run that goes on.
type UserTool interface {
	Tool
	// Check reports, in words the model can act on, why a call's arguments are
	// not a question that can be put. The call is then answered with that error
	// at once and the run goes on.
	Check(args json.RawMessage) error
}

// toolsByName indexes the tools offered to one request.
func toolsByName(tools []Tool) map[string]Tool {
	byName := make(map[string]Tool, len(tools))
	for _, tool := range tools {
		byName[tool.Definition().Name] = tool
	}
	return byName
}

func definitions(tools []Tool) []llm.Tool {
	defs := make([]llm.Tool, 0, len(tools))
	for _, tool := range tools {
		defs = append(defs, tool.Definition())
	}
	return defs
}

// WebTool is implemented by a tool that reaches the web. A run whose
// conversation has web access switched off is not offered these.
type WebTool interface {
	UsesWeb() bool
}

// withoutWeb drops the tools that reach the web.
func withoutWeb(tools []Tool) []Tool {
	out := tools[:0:0]
	for _, tool := range tools {
		if web, ok := tool.(WebTool); ok && web.UsesWeb() {
			continue
		}
		out = append(out, tool)
	}
	return out
}
