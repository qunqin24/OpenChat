package anthropic

import "encoding/json"

// The structs here mirror the Messages API. Like the Chat Completions ones they
// are decoded leniently — unknown fields are ignored, because this API gains
// them regularly and refusing a frame over one would break a working endpoint.
// Frames that are not valid JSON at all are a different matter and do fail; see
// decode.go.

// streamEvent is the union of every named event this protocol dispatches. The
// name on the `event:` line says which fields are meaningful, so this is one
// struct rather than eight: decoding is a single Unmarshal and the dispatch
// happens on the name, not on which pointer happens to be non-nil.
type streamEvent struct {
	// Type is the payload's own copy of the event name. It is the fallback for
	// relays that forward the data lines and drop the `event:` ones.
	Type string `json:"type"`

	// Message arrives on message_start and carries the turn's identity and the
	// input half of the usage.
	Message *streamMessage `json:"message"`

	// Index is the content block this event is about, on content_block_start,
	// content_block_delta and content_block_stop. It is a pointer because block
	// zero is a real block and an absent index is a broken frame.
	Index        *int          `json:"index"`
	ContentBlock *contentBlock `json:"content_block"`

	// Delta is a content_block_delta's fragment or a message_delta's verdict.
	// The two share the field because the protocol does.
	Delta *eventDelta `json:"delta"`

	// Usage arrives on message_delta and holds only the output half.
	Usage *wireUsage `json:"usage"`

	Error *wireError `json:"error"`
}

type streamMessage struct {
	ID    string     `json:"id"`
	Model string     `json:"model"`
	Role  string     `json:"role"`
	Usage *wireUsage `json:"usage"`
}

// contentBlock is the opening shape of a block. Which fields are set depends on
// Type: text, thinking, redacted_thinking or tool_use.
type contentBlock struct {
	Type string `json:"type"`

	Text string `json:"text"`

	Thinking  string `json:"thinking"`
	Signature string `json:"signature"`

	// Data is redacted_thinking's whole payload: encrypted reasoning that
	// arrives complete, with no deltas behind it and nothing to display.
	Data string `json:"data"`

	ID   string `json:"id"`
	Name string `json:"name"`

	// Input is `{}` on a streamed tool_use — the arguments come as
	// input_json_delta fragments. A relay that sends the finished object here
	// instead is the one case where it is worth reading.
	Input json.RawMessage `json:"input"`
}

type eventDelta struct {
	// Type is text_delta, thinking_delta, signature_delta or input_json_delta
	// on a content_block_delta, and empty on a message_delta.
	Type string `json:"type"`

	Text        string `json:"text"`
	Thinking    string `json:"thinking"`
	Signature   string `json:"signature"`
	PartialJSON string `json:"partial_json"`

	// StopReason and StopSequence belong to message_delta: this protocol says
	// why it stopped once, at the end, for the whole message.
	StopReason   string `json:"stop_reason"`
	StopSequence string `json:"stop_sequence"`
}

// wireUsage is reported in two halves. message_start carries the input side,
// message_delta the output side, and the cache counters sit *beside*
// input_tokens rather than inside it — the opposite of Chat Completions. See
// convertUsage.
type wireUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`

	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
}

type wireError struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

// wireMessage is the request-side message. Content is always a block list: this
// protocol accepts a bare string too, but every turn we send that is not plain
// text needs blocks anyway, and one shape is one thing to get right.
type wireMessage struct {
	Role    string             `json:"role"`
	Content []wireContentBlock `json:"content"`
}

type wireContentBlock struct {
	Type string `json:"type"`

	Text string `json:"text,omitempty"`

	// Thinking is a pointer so that a signed block with no text still sends the
	// field: omitempty would drop an empty string, and the API requires it.
	Thinking  *string `json:"thinking,omitempty"`
	Signature string  `json:"signature,omitempty"`
	Data      string  `json:"data,omitempty"`

	ID    string          `json:"id,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`

	ToolUseID string `json:"tool_use_id,omitempty"`

	// Content is a tool_result's payload. It is a separate field from Text
	// because a tool_result block has no text field at all.
	Content string `json:"content,omitempty"`
	IsError bool   `json:"is_error,omitempty"`

	Source *wireSource `json:"source,omitempty"`
}

// wireSource is how image and document blocks carry their bytes.
type wireSource struct {
	Type      string `json:"type"`
	MediaType string `json:"media_type,omitempty"`
	Data      string `json:"data,omitempty"`
	URL       string `json:"url,omitempty"`
}
