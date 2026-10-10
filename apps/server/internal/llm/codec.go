package llm

import (
	"encoding/json"
	"fmt"
)

// The canonical JSON form of messages and parts: OpenChat's internal protocol.
//
// Every upstream format — OpenAI Chat Completions, Anthropic Messages, Gemini,
// and whatever is added next — is translated into these types by its adapter,
// and every layer above stores and transmits them in exactly this encoding: the
// transcript's `parts` column, a run's live partial, the events a browser
// receives. A new upstream format therefore means a new adapter and nothing
// else; a second encoding anywhere above would be a second protocol to keep in
// step with this one.
//
// The encoding is persisted, so it is a compatibility surface. Adding an
// optional field is safe. Renaming or removing one, or changing a `type`
// value, needs a data migration — which is why the set of part types is closed
// and an unknown one is an error rather than something skipped.

// partJSON is the union of every part's fields, discriminated by Type.
type partJSON struct {
	Type PartKind `json:"type"`

	// text, reasoning
	Text string `json:"text,omitempty"`

	// reasoning
	Signature string `json:"signature,omitempty"`
	Redacted  []byte `json:"redacted,omitempty"`

	// tool_call
	ID           string          `json:"id,omitempty"`
	Name         string          `json:"name,omitempty"`
	Arguments    json.RawMessage `json:"arguments,omitempty"`
	RawArguments string          `json:"rawArguments,omitempty"`

	// tool_result
	CallID     string           `json:"callId,omitempty"`
	Content    string           `json:"content,omitempty"`
	Status     ToolResultStatus `json:"status,omitempty"`
	Error      string           `json:"error,omitempty"`
	Details    json.RawMessage  `json:"details,omitempty"`
	DurationMS *int64           `json:"durationMs,omitempty"` // also reasoning

	// attachment. Only the reference is encoded: resolved bytes and signed URLs
	// are per request and must never be persisted or sent back to a browser.
	Ref       string `json:"ref,omitempty"`
	Filename  string `json:"filename,omitempty"`
	MediaType string `json:"mediaType,omitempty"`
}

// MarshalPart encodes one part.
func MarshalPart(part Part) ([]byte, error) {
	return json.Marshal(toPartJSON(part))
}

// UnmarshalPart decodes one part.
func UnmarshalPart(data []byte) (Part, error) {
	var raw partJSON
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("llm: decoding part: %w", err)
	}
	return raw.part()
}

func toPartJSON(part Part) partJSON {
	switch p := part.(type) {
	case TextPart:
		return partJSON{Type: PartText, Text: p.Text}
	case ReasoningPart:
		return partJSON{Type: PartReasoning, Text: p.Text, Signature: p.Signature, Redacted: p.Redacted, DurationMS: p.DurationMS}
	case ToolCallPart:
		return partJSON{Type: PartToolCall, ID: p.ID, Name: p.Name, Arguments: p.Arguments, RawArguments: p.RawArguments}
	case ToolResultPart:
		return partJSON{Type: PartToolResult, CallID: p.CallID, Name: p.Name, Content: p.Content,
			Status: p.Status, Error: p.Error, Details: p.Details, DurationMS: p.DurationMS}
	case AttachmentPart:
		return partJSON{Type: PartAttachment, Ref: p.Ref, Filename: p.Filename, MediaType: p.MediaType}
	}
	// Part is a closed set; this is unreachable for any value of it.
	panic(fmt.Sprintf("llm: unknown part %T", part))
}

func (raw partJSON) part() (Part, error) {
	switch raw.Type {
	case PartText:
		return TextPart{Text: raw.Text}, nil
	case PartReasoning:
		return ReasoningPart{Text: raw.Text, Signature: raw.Signature, Redacted: raw.Redacted, DurationMS: raw.DurationMS}, nil
	case PartToolCall:
		return ToolCallPart{ID: raw.ID, Name: raw.Name, Arguments: raw.Arguments, RawArguments: raw.RawArguments}, nil
	case PartToolResult:
		return ToolResultPart{CallID: raw.CallID, Name: raw.Name, Content: raw.Content, Status: raw.Status,
			Error: raw.Error, Details: raw.Details, DurationMS: raw.DurationMS}, nil
	case PartAttachment:
		return AttachmentPart{Ref: raw.Ref, Filename: raw.Filename, MediaType: raw.MediaType}, nil
	}
	return nil, fmt.Errorf("llm: unknown part type %q", raw.Type)
}

// Parts is an ordered list of parts with the canonical JSON encoding. Never
// encoded as null: an empty list is `[]`.
type Parts []Part

func (ps Parts) MarshalJSON() ([]byte, error) {
	out := make([]partJSON, 0, len(ps))
	for _, part := range ps {
		out = append(out, toPartJSON(part))
	}
	return json.Marshal(out)
}

func (ps *Parts) UnmarshalJSON(data []byte) error {
	var raw []partJSON
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("llm: decoding parts: %w", err)
	}
	parts := make(Parts, 0, len(raw))
	for _, r := range raw {
		part, err := r.part()
		if err != nil {
			return err
		}
		parts = append(parts, part)
	}
	*ps = parts
	return nil
}

// messageJSON is Message's canonical form.
type messageJSON struct {
	Role  Role  `json:"role"`
	Parts Parts `json:"parts"`
}

func (m Message) MarshalJSON() ([]byte, error) {
	return json.Marshal(messageJSON{Role: m.Role, Parts: m.Parts})
}

func (m *Message) UnmarshalJSON(data []byte) error {
	var raw messageJSON
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("llm: decoding message: %w", err)
	}
	m.Role, m.Parts = raw.Role, raw.Parts
	return nil
}
