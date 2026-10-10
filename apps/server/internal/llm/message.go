// Package llm is the vocabulary every layer uses to talk about a model turn:
// the messages that go in, the parts that come back, the events that describe a
// stream still in flight, and the single interface a provider adapter fills in.
//
// It knows nothing about SQL, HTTP handlers, cookies or any individual vendor,
// and it must stay that way. Everything above depends on these types; nothing
// here depends on anything above. That is what lets the run loop be tested
// without a database and a provider be tested without a browser.
package llm

import "encoding/json"

type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"

	// RoleTool carries tool results back to the model. It is its own role
	// rather than a field on the user turn because every wire protocol we
	// intend to speak models it that way.
	RoleTool Role = "tool"
)

// PartKind names the concrete type behind a Part. It exists so that events,
// storage and the UI can branch on a block without a type switch over a
// package they should not have to import.
type PartKind string

const (
	PartText       PartKind = "text"
	PartReasoning  PartKind = "reasoning"
	PartToolCall   PartKind = "tool_call"
	PartToolResult PartKind = "tool_result"
	PartAttachment PartKind = "attachment"
)

// Part is one block of a message. A single assistant turn interleaves text,
// reasoning and tool calls, so a message is an ordered list of parts rather
// than a string: flattening them loses both the order the user sees and the
// distinction between what may be rendered and what may be replayed to a model.
//
// The set is closed — isPart is unexported — so a provider adapter cannot
// invent a block type the run layer and the UI have no handling for.
type Part interface {
	Kind() PartKind
	isPart()
}

// TextPart is the answer itself: the only kind of block that is always safe to
// show and always safe to replay.
type TextPart struct {
	Text string
}

// ReasoningPart is the model thinking out loud. Signature and Redacted are kept
// apart from Text on purpose: they are three different things, and a provider
// that hands back a signature it later wants to verify cannot be served by one
// generic string field.
type ReasoningPart struct {
	Text string

	// Signature is the provider's attestation over Text. It is opaque and only
	// meaningful to the provider that issued it — replaying it to a different
	// vendor is at best ignored and at worst a rejected request.
	Signature string

	// Redacted holds reasoning the provider encrypted or withheld. Text is then
	// empty: there is nothing to display, but the blob still has to survive the
	// round trip or the next turn loses the model's own context.
	Redacted []byte

	// DurationMS is how long the model spent on this block, from its first
	// token to its last, measured by us as it streamed. Nil when nobody timed
	// it: rows written before it was recorded, and replies that did not stream.
	// Never sent to a provider.
	DurationMS *int64
}

// ToolCallPart is the model asking for a tool. Arguments and RawArguments are
// separate because they answer different questions: may this be executed, and
// what exactly did the model send.
type ToolCallPart struct {
	// ID is the provider's handle for this call. A ToolResultPart is matched
	// back to it by this value, never by position.
	ID   string
	Name string

	// Arguments is the complete, valid JSON the model finished sending. It is
	// nil when nothing parsed, so an unparsable call can never be mistaken for
	// a call with no arguments.
	Arguments json.RawMessage

	// RawArguments is everything that arrived, parsed or not. When Arguments is
	// nil this is what lets the runner tell the model what was wrong instead of
	// executing a guess.
	RawArguments string
}

// Parsed reports whether the arguments are complete and valid JSON. Nothing may
// be executed on a call where this is false.
func (p ToolCallPart) Parsed() bool { return len(p.Arguments) > 0 }

// ToolResultStatus is how a tool call ended. A missing result is expressed
// here, never faked as an empty success — the model would happily build an
// answer on top of a success that never happened.
type ToolResultStatus string

const (
	ToolSucceeded ToolResultStatus = "succeeded"
	// ToolFailed is a tool that ran and refused, which is ordinary context for
	// the next turn rather than a reason to end the Run.
	ToolFailed ToolResultStatus = "failed"
	// ToolCancelled is a tool stopped by the user or by the Run's deadline.
	ToolCancelled ToolResultStatus = "cancelled"
	// ToolInterrupted is a tool whose executor disappeared: nobody knows
	// whether it finished, which is not the same as knowing it failed.
	ToolInterrupted ToolResultStatus = "interrupted"
)

type ToolResultPart struct {
	// CallID is the ToolCallPart.ID this answers.
	CallID string
	Name   string
	// Content is what the model gets to read.
	Content string
	Status  ToolResultStatus
	// Error explains a non-succeeded status to the operator and, once mapped,
	// to the user. It is not the same string as Content.
	Error string

	// Details is structured data for the UI — a search's result list, a fetched
	// page's title. Adapters never send it: what the model reads is Content.
	Details json.RawMessage
	// DurationMS is how long the tool ran, when it ran at all. UI only.
	DurationMS *int64
}

// AttachmentPart references a file the user attached. It is a reference by
// design: llm must not know where bytes live or who is allowed to read them.
// The context builder resolves Ref within the requesting user's scope and fills
// in Data or URL before a request ever reaches a provider — an adapter that
// receives an unresolved attachment rejects the request rather than guessing.
type AttachmentPart struct {
	// Ref is an opaque handle assigned by the storage layer. A model never
	// supplies one: that would be a path for reading someone else's files.
	Ref       string
	Filename  string
	MediaType string

	// Data is the resolved content when the builder chose to inline it.
	Data []byte
	// URL is the resolved location when the builder chose to hand the provider
	// a link instead. At most one of Data and URL is set.
	URL string
}

func (TextPart) Kind() PartKind       { return PartText }
func (ReasoningPart) Kind() PartKind  { return PartReasoning }
func (ToolCallPart) Kind() PartKind   { return PartToolCall }
func (ToolResultPart) Kind() PartKind { return PartToolResult }
func (AttachmentPart) Kind() PartKind { return PartAttachment }

func (TextPart) isPart()       {}
func (ReasoningPart) isPart()  {}
func (ToolCallPart) isPart()   {}
func (ToolResultPart) isPart() {}
func (AttachmentPart) isPart() {}

// Message is one turn of the conversation.
//
// It carries no provenance (which model, which run): that belongs to the stored
// record and to ModelResponse. Keeping it out here means the same type can be
// built from the database, from a provider stream, or from a literal in a test
// without three different notions of "where did this come from".
type Message struct {
	Role  Role
	Parts []Part
}

// TextMessage is the common case: one role, one block of text.
func TextMessage(role Role, text string) Message {
	return Message{Role: role, Parts: []Part{TextPart{Text: text}}}
}

// Text concatenates the text blocks in order. Reasoning is deliberately left
// out: it is not the answer, and several providers reject it on the way back in.
func (m Message) Text() string {
	var b []byte
	for _, part := range m.Parts {
		if text, ok := part.(TextPart); ok {
			b = append(b, text.Text...)
		}
	}
	return string(b)
}

// ToolCalls returns the calls this message asks for, in order.
func (m Message) ToolCalls() []ToolCallPart {
	var calls []ToolCallPart
	for _, part := range m.Parts {
		if call, ok := part.(ToolCallPart); ok {
			calls = append(calls, call)
		}
	}
	return calls
}
