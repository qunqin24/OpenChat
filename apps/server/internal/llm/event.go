package llm

// EventType is what a streaming event announces.
//
// Two of these describe endings, and they are different facts: EventPartEnd
// says one content block is final, EventResponseEnd that the model's single
// response is. A block that is final says nothing about whether the model is
// done talking, and a response that ended is not a finished run: the tool
// loop and the run belong to the layer above (internal/agent), which has its
// own events.
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
)

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
