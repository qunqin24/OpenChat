package tools

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/google/uuid"

	"github.com/qunqin24/openchat/apps/server/internal/agent"
	"github.com/qunqin24/openchat/apps/server/internal/llm"
	"github.com/qunqin24/openchat/apps/server/internal/memory"
)

// ToolMemory is the memory tool's name.
const ToolMemory = "memory"

// Memory is the `memory` tool: the model keeps notes about the user for later
// conversations - when the user asks, or states a lasting fact about themselves
// (the system prompt's Memory section, api/agent_setup.go, says when).
//
// It acts at once, with no confirmation to wait for; the reader is shown what
// changed in the reply and can undo it there. Everything it touches is the
// calling run's user's: the user comes from the run (agent.ScopeFrom), never
// from the arguments, and a memory is named by the short id its conversation's
// snapshot gave it, so an id from anywhere else means nothing.
type Memory struct {
	Store *memory.Store
}

var _ agent.Tool = Memory{}

// MemoryArgs are a call's arguments.
type MemoryArgs struct {
	Op      string `json:"op"`
	ID      string `json:"id"`
	Content string `json:"content"`
}

// MemoryDetails is what the interface is shown of a call: the change, and the
// log entry the reader's undo refers to.
type MemoryDetails struct {
	Op       string    `json:"op"`
	Ref      string    `json:"ref,omitempty"`
	MemoryID uuid.UUID `json:"memoryId"`
	EventID  uuid.UUID `json:"eventId"`
	Content  string    `json:"content,omitempty"`
	Before   string    `json:"before,omitempty"`
}

func (Memory) Definition() llm.Tool {
	return llm.Tool{
		Name: ToolMemory,
		// What to save, and when, is in the system prompt's Memory section
		// (api/agent_setup.go); this says how to use the tool.
		Description: "Save, change or remove a note about the user that will be given to you in future " +
			"conversations, only in the cases the Memory section of your instructions allows. It takes effect at " +
			"once; do not ask permission first. " +
			"op=add saves a new note (content). op=update rewrites a note (id, content); use it to merge a " +
			"related fact into an existing note rather than adding a near-duplicate. op=delete removes a note " +
			"(id), for one that is out of date or that the user asks you to forget. " +
			"Refer to a note by the id shown in your memory list, such as m3. " +
			"Each note is one complete third-person sentence about the user, in the user's language, at most " +
			"200 characters.",
		Parameters: json.RawMessage(`{
  "type": "object",
  "properties": {
    "op": {"type": "string", "enum": ["add", "update", "delete"]},
    "id": {"type": "string", "description": "The note's id, such as m3. For update and delete."},
    "content": {"type": "string", "description": "The note, one sentence. For add and update."}
  },
  "required": ["op"]
}`),
	}
}

// Replayable is false: a call that was running when the process died may have
// saved its note already.
func (Memory) Replayable() bool { return false }

func (m Memory) Execute(ctx context.Context, raw json.RawMessage) (agent.ToolResult, error) {
	scope, ok := agent.ScopeFrom(ctx)
	if !ok {
		return agent.ToolResult{}, errors.New("memory: no run in context")
	}
	var args MemoryArgs
	if err := agent.DecodeArgs(raw, &args); err != nil {
		return agent.ToolResult{}, err
	}
	// Offered at the start of the request, but switched off since is not
	// something the call may use.
	if on, err := m.Store.ToolAvailable(ctx, scope.UserID, scope.ConversationID); err != nil {
		return agent.ToolResult{}, err
	} else if !on {
		return agent.ToolResult{}, agent.Errorf("Memory is turned off for this conversation. Nothing was saved.")
	}
	by := memory.By{Source: memory.SourceModel, ConversationID: &scope.ConversationID}

	var (
		change memory.Change
		err    error
		text   string
	)
	switch op := strings.ToLower(strings.TrimSpace(args.Op)); op {
	case "add":
		change, err = m.Store.Add(ctx, scope.UserID, args.Content, by)
		if err == nil {
			text = "Saved as " + change.Ref + ": " + change.Memory.Content
		}
	case "update", "delete":
		var id uuid.UUID
		if id, err = m.Store.Resolve(ctx, scope.UserID, scope.ConversationID, args.ID); err != nil {
			break
		}
		if op == "update" {
			change, err = m.Store.Update(ctx, scope.UserID, id, args.Content, by)
			text = "Updated " + strings.ToLower(strings.TrimSpace(args.ID)) + ": " + change.Memory.Content
		} else {
			change, err = m.Store.Delete(ctx, scope.UserID, id, by)
			text = "Deleted " + strings.ToLower(strings.TrimSpace(args.ID)) + "."
		}
	default:
		return agent.ToolResult{}, agent.Errorf("invalid arguments: op must be add, update or delete")
	}
	if err != nil {
		return agent.ToolResult{}, modelError(err)
	}

	details := MemoryDetails{Op: strings.ToLower(strings.TrimSpace(args.Op)), Ref: change.Ref, MemoryID: change.Memory.ID, Content: change.Memory.Content}
	if details.Ref == "" {
		details.Ref = strings.ToLower(strings.TrimSpace(args.ID))
	}
	if change.Event != nil {
		details.EventID = change.Event.ID
	}
	if change.Before != nil {
		details.Before = *change.Before
	}
	return agent.ToolResult{Content: text, Details: details}, nil
}

// modelError says in words a model can act on why a call did nothing.
func modelError(err error) error {
	switch {
	case errors.Is(err, memory.ErrFull):
		return agent.Errorf("The memory list is full (%d notes). Nothing was saved. Update or delete a note that is "+
			"no longer needed or that this one can be merged into, then try again.", memory.MaxMemories)
	case errors.Is(err, memory.ErrDuplicate):
		return agent.Errorf("That is already remembered. Nothing was changed.")
	case errors.Is(err, memory.ErrEmpty):
		return agent.Errorf("invalid arguments: content is required")
	case errors.Is(err, memory.ErrTooLong):
		return agent.Errorf("invalid arguments: a note is at most %d characters; shorten it to one sentence", memory.MaxContent)
	case errors.Is(err, memory.ErrUnknownRef):
		return agent.Errorf("There is no note with that id in this conversation's memory list. Use an id from the " +
			"list in your instructions, or add a new note.")
	case errors.Is(err, memory.ErrNotFound):
		return agent.Errorf("That note no longer exists (the user may have deleted it). Nothing was changed.")
	}
	return err
}
