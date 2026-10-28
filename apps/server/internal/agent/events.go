package agent

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"github.com/qunqin24/openchat/apps/server/internal/db/sqlc"
	"github.com/qunqin24/openchat/apps/server/internal/llm"
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

// TitleChanged is the conversation's generated title, published after the run
// whose exchange it was made from has finished.
type TitleChanged struct{ Title string }

// TranscriptChanged: rows the conversation showed were deleted — a question and
// everything after it by an edit, an answer by a regeneration; either starts a
// run, announced in the same batch. The deletion is already committed; a
// subscriber that holds the transcript fetches it again and drops what it held
// that is gone. From is the first row deleted when a question was edited (that
// row and every row after it), so a subscriber can drop them at once, before
// the fetch has come back; nil for a regeneration, whose answer is whatever
// follows the question the run answers.
type TranscriptChanged struct{ From uuid.UUID }

// CompactionStarted: the run is compressing the conversation's context before
// its next request (see compaction.go). Live.Compacting is set until
// CompactionFinished.
type CompactionStarted struct{}

// CompactionFinished: the compression ended. Summary is the conversation's new
// summary, or nil when the compression failed or found nothing to do, in which
// case the request went out as it was.
type CompactionFinished struct{ Summary *Summary }

// CompactionChanged: the conversation's summary was changed by something other
// than a run - a person took it back. Summary is the one it has now, nil for
// none. Published outside any run (nil run id,
// version 0), like a title.
type CompactionChanged struct{ Summary *Summary }

// ConversationDeleted: the conversation was deleted, from this page or another
// one. Published outside any run (nil run id, version 0) after the delete
// committed, so a page following it can leave instead of showing a
// conversation that no longer exists.
type ConversationDeleted struct{}

func (ConversationDeleted) isEvent() {}

// SubagentsChanged: one of the conversation's sub-agents started, took a
// step or ended. Published outside any run (nil run id, version 0), on the
// conversation it works for; a page following that conversation reads its
// sub-agents again.
type SubagentsChanged struct{}

func (SubagentsChanged) isEvent() {}

func (CompactionStarted) isEvent()  {}
func (CompactionFinished) isEvent() {}
func (CompactionChanged) isEvent()  {}
func (RunStarted) isEvent()         {}
func (RunFinished) isEvent()        {}
func (GenerationStarted) isEvent()  {}
func (PartStarted) isEvent()        {}
func (PartDelta) isEvent()          {}
func (PartEnded) isEvent()          {}
func (MessageAppended) isEvent()    {}
func (ToolStarted) isEvent()        {}
func (ToolFinished) isEvent()       {}
func (RetryScheduled) isEvent()     {}
func (TitleChanged) isEvent()       {}
func (TranscriptChanged) isEvent()  {}

// Batch is the events of one commit. Version is the run's version after it, so
// a subscriber whose snapshot already reflects that commit can drop it.
type Batch struct {
	RunID   uuid.UUID
	Version int64
	Events  []Event
}
