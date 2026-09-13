package llm

// EventType is what a streaming event announces.
//
// Four of these describe endings, and they are four different types because
// they are four different facts. Collapsing any pair of them loses something
// the layer above needs:
//
//	EventPartEnd             one content block is final
//	EventResponseEnd         the model's single response is final
//	EventToolCallsRequested  a tool was asked for, another model turn follows
//	EventRunEnd              the whole Run is over
//
// A response that ends with tool calls is not a finished Run. A finished Run is
// not necessarily a response that ended cleanly. And a block that is final says
// nothing at all about whether the model is done talking.
type EventType string

const (
	// EventResponseStart says the turn is alive. The run layer uses it to move
	// the UI off "waiting" even when the first thing to arrive is reasoning it
	// may not be showing.
	EventResponseStart EventType = "response_start"

	EventPartStart EventType = "part_start"
	EventPartDelta EventType = "part_delta"
	EventPartEnd   EventType = "part_end"

	EventResponseEnd EventType = "response_end"

	// EventToolCallsRequested is emitted by the run layer, never by a provider:
	// only the runner knows whether it is going to take the model up on the
	// request, and a provider has no idea a next turn exists.
	EventToolCallsRequested EventType = "tool_calls_requested"

	// EventRunEnd is the Run's single terminal event, likewise the runner's.
	EventRunEnd EventType = "run_end"
)

// TerminatesRun reports whether this type ends the whole Run. Exactly one such
// event may be published per Run.
func (t EventType) TerminatesRun() bool { return t == EventRunEnd }

// RunStatus is where a Run ended up. It lives here rather than with the runner
// because EventRunEnd has to carry it and the storage layer has to persist it,
// and a status those two spell differently is a bug waiting to happen.
type RunStatus string

const (
	RunPending   RunStatus = "pending"
	RunRunning   RunStatus = "running"
	RunCompleted RunStatus = "completed"
	RunFailed    RunStatus = "failed"
	RunCancelled RunStatus = "cancelled"

	// RunInterrupted is a Run whose executor vanished — a process restart, not
	// a decision. It is distinct from cancelled because nobody chose it and
	// because it is the one terminal state that justifies offering a retry.
	RunInterrupted RunStatus = "interrupted"
)

func (s RunStatus) Terminal() bool {
	switch s {
	case RunCompleted, RunFailed, RunCancelled, RunInterrupted:
		return true
	}
	return false
}

// Event is one step of a turn as it happens. Which fields are set depends
// entirely on Type; the rest are zero.
type Event struct {
	Type EventType

	// Index identifies the content block within the current response. It is an
	// ordinal, not an identity: providers cannot mint ids that survive a
	// restart, so the run layer maps Index onto a durable part id and every
	// event for one block carries the same Index.
	Index int

	// PartKind says how to read Delta and what kind of block Index refers to.
	PartKind PartKind

	// Delta is the fragment that just arrived. For text and reasoning it is
	// plain text to append. For a tool call it is a raw JSON fragment that is
	// almost never valid JSON on its own: it exists so the UI can show the
	// arguments filling in, and parsing it to decide anything is exactly the
	// mistake this field's existence is meant to prevent.
	Delta string

	// ToolCallID and ToolName describe a tool block. Both may be empty on
	// EventPartStart — some providers send the name and id after the first
	// argument fragment — and are authoritative only on EventPartEnd.
	ToolCallID string
	ToolName   string

	// Part is the finished, immutable block. Set on EventPartEnd, and the only
	// place a tool call's strictly parsed arguments appear.
	Part Part

	// Response is the completed turn. Set on EventResponseEnd.
	Response *ModelResponse

	// ToolCalls are the calls the runner is about to execute. Set on
	// EventToolCallsRequested.
	ToolCalls []ToolCallPart

	// Status and Err describe the Run's terminal state. Set on EventRunEnd;
	// Err is nil unless Status is RunFailed.
	Status RunStatus
	Err    error
}

func ResponseStartEvent() Event {
	return Event{Type: EventResponseStart}
}

func PartStartEvent(index int, kind PartKind) Event {
	return Event{Type: EventPartStart, Index: index, PartKind: kind}
}

// ToolCallStartEvent opens a tool block. id and name are whatever is known so
// far, which is frequently nothing.
func ToolCallStartEvent(index int, id, name string) Event {
	return Event{Type: EventPartStart, Index: index, PartKind: PartToolCall, ToolCallID: id, ToolName: name}
}

func DeltaEvent(index int, kind PartKind, delta string) Event {
	return Event{Type: EventPartDelta, Index: index, PartKind: kind, Delta: delta}
}

func PartEndEvent(index int, part Part) Event {
	ev := Event{Type: EventPartEnd, Index: index, PartKind: part.Kind(), Part: part}
	if call, ok := part.(ToolCallPart); ok {
		ev.ToolCallID = call.ID
		ev.ToolName = call.Name
	}
	return ev
}

func ResponseEndEvent(resp ModelResponse) Event {
	return Event{Type: EventResponseEnd, Response: &resp}
}

func ToolCallsRequestedEvent(calls []ToolCallPart) Event {
	return Event{Type: EventToolCallsRequested, ToolCalls: calls}
}

func RunEndEvent(status RunStatus, err error) Event {
	return Event{Type: EventRunEnd, Status: status, Err: err}
}
