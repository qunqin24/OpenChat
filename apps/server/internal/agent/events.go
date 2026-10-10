package agent

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"github.com/qunqin/openchat/apps/server/internal/db/sqlc"
	"github.com/qunqin/openchat/apps/server/internal/llm"
)

// Event is one change a subscriber applies on top of its snapshot.
//
// Every event describes something already committed — there is no path by
// which a browser learns of progress the database does not hold. The HTTP layer
// maps these onto the contract's types; they are not the wire format.
type Event interface{ isEvent() }

// RunStarted: a run was admitted, with the user message that started it.
type RunStarted struct {
	Run   sqlc.Run
	Input sqlc.Message
}

// RunFinished: the run reached a terminal status. Its messages have all been
// announced by then.
type RunFinished struct{ Run sqlc.Run }

// GenerationStarted: a request attempt produced its first committed content.
// Whatever partial a subscriber holds from an earlier attempt is gone.
type GenerationStarted struct{ Attempt int }

// PartStarted opens content block Index of the response in flight. Part
// carries the block's kind and, for a tool call, whatever of its id and name is
// known so far.
type PartStarted struct {
	Index int
	Part  llm.Part
}

// PartDelta appends to block Index: text for text and reasoning, raw argument
// JSON for a tool call.
type PartDelta struct {
	Index int
	Delta string
}

// PartEnded closes block Index with its finished form: a tool call's parsed
// arguments and final name, a reasoning block's signature.
type PartEnded struct {
	Index int
	Part  llm.Part
}

// MessageAppended: a transcript row was written. An assistant row replaces the
// partial a subscriber was building.
type MessageAppended struct{ Message sqlc.Message }

// ToolStarted: a call's intent was recorded and the tool is running.
type ToolStarted struct {
	CallID    string
	Name      string
	Arguments json.RawMessage
}

// ToolFinished: a call's result row was written.
type ToolFinished struct {
	CallID  string
	Message uuid.UUID
}

// RetryScheduled: the request failed in a way worth retrying, and the next
// attempt starts at At.
type RetryScheduled struct {
	Attempt   int
	At        time.Time
	ErrorCode string
}

func (RunStarted) isEvent()        {}
func (RunFinished) isEvent()       {}
func (GenerationStarted) isEvent() {}
func (PartStarted) isEvent()       {}
func (PartDelta) isEvent()         {}
func (PartEnded) isEvent()         {}
func (MessageAppended) isEvent()   {}
func (ToolStarted) isEvent()       {}
func (ToolFinished) isEvent()      {}
func (RetryScheduled) isEvent()    {}

// Batch is the events of one commit. Version is the run's version after it, so
// a subscriber whose snapshot already reflects that commit can drop it.
type Batch struct {
	RunID   uuid.UUID
	Version int64
	Events  []Event
}
