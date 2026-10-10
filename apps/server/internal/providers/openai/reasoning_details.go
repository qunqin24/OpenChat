package openai

import (
	"bytes"
	"encoding/json"
	"strings"

	"github.com/qunqin/openchat/apps/server/internal/llm"
)

// OpenRouter returns what a model reasoned as `reasoning_details`: an array of
// objects of type reasoning.text (with the signature Claude needs back),
// reasoning.summary, or reasoning.encrypted (OpenAI's and Gemini's opaque
// state), each tagged with the `format` it came in. They have to go back
// unmodified on the same assistant message, in the same order, or a Claude
// tool loop loses its signed thinking and the others their encrypted state:
// https://openrouter.ai/docs/guides/best-practices/reasoning-tokens
//
// The objects are kept as maps of raw JSON rather than a struct, so a field
// OpenRouter adds tomorrow still comes back exactly as it went out.

type reasoningDetail map[string]json.RawMessage

// detailTypes whose fragments stream in pieces and are joined back into one.
const (
	detailText    = "reasoning.text"
	detailSummary = "reasoning.summary"
)

// parseDetails reads one chunk's reasoning_details. Anything that is not an
// array of objects yields nothing: it is a side channel, and a gateway that
// sends it in another shape must not take the frame's answer down with it.
func parseDetails(raw json.RawMessage) []reasoningDetail {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || raw[0] != '[' {
		return nil
	}
	var list []reasoningDetail
	if err := json.Unmarshal(raw, &list); err != nil {
		return nil
	}
	out := list[:0]
	for _, detail := range list {
		if detail != nil {
			out = append(out, detail)
		}
	}
	return out
}

func (d reasoningDetail) str(key string) string {
	var s string
	if raw, ok := d[key]; ok {
		_ = json.Unmarshal(raw, &s)
	}
	return s
}

// visibleText is what of this detail may be shown as the model's thinking: the
// text of a reasoning.text, the summary of a reasoning.summary, nothing of an
// encrypted one.
func (d reasoningDetail) visibleText() string {
	switch d.str("type") {
	case detailText:
		return d.str("text")
	case detailSummary:
		return d.str("summary")
	}
	return ""
}

// textOf is the visible text of a chunk's details, for a chunk that carries
// them without the plain `reasoning` string beside it.
func textOf(details []reasoningDetail) string {
	var b strings.Builder
	for _, detail := range details {
		b.WriteString(detail.visibleText())
	}
	return b.String()
}

// textKey is the field a streamed detail of this type grows in.
func (d reasoningDetail) textKey() string {
	switch d.str("type") {
	case detailText:
		return "text"
	case detailSummary:
		return "summary"
	}
	return ""
}

// continues reports whether next is the following fragment of d: the same
// kind of detail, in the same format, with the same id and index, and not yet
// closed by a signature. The signature is what ends a thinking block, so a
// second block of the same shape starts a detail of its own.
func (d reasoningDetail) continues(next reasoningDetail) bool {
	key := d.textKey()
	if key == "" || next.textKey() != key {
		return false
	}
	if d.str("signature") != "" {
		return false
	}
	for _, field := range []string{"format", "id", "index"} {
		if !bytes.Equal(bytes.TrimSpace(d[field]), bytes.TrimSpace(next[field])) {
			return false
		}
	}
	return true
}

// absorb joins a following fragment into d: its text appended, its signature
// taken when it carries one (it arrives on the last fragment of a block).
func (d reasoningDetail) absorb(next reasoningDetail) {
	key := d.textKey()
	joined, _ := json.Marshal(d.str(key) + next.str(key))
	d[key] = joined
	if sig := next.str("signature"); sig != "" {
		d["signature"] = next["signature"]
	}
}

// detailList is the details of one reasoning block, with streamed fragments
// already joined, which is the shape a non-streaming response has them in.
type detailList []reasoningDetail

func (l detailList) add(fragments []reasoningDetail) detailList {
	for _, fragment := range fragments {
		if n := len(l); n > 0 && l[n-1].continues(fragment) {
			l[n-1].absorb(fragment)
			continue
		}
		l = append(l, fragment)
	}
	return l
}

// replay is the stored form: a JSON array, as OpenRouter sent it.
func (l detailList) replay() []byte {
	data, err := json.Marshal([]reasoningDetail(l))
	if err != nil {
		return nil
	}
	return data
}

// detailsSignature namespaces stored details by the model they came from. A
// model other than the one that wrote them is not sent them: signed Claude
// thinking or one vendor's encrypted state means nothing to another.
func detailsSignature(model string) string { return llm.SignatureOpenRouter + model }

// storedDetails collects the details an assistant message carries for model,
// in order, ready to go back as its `reasoning_details`.
func storedDetails(message llm.Message, model string) json.RawMessage {
	var all []json.RawMessage
	for _, part := range message.Parts {
		p, ok := part.(llm.ReasoningPart)
		if !ok || len(p.Redacted) == 0 || p.Signature != detailsSignature(model) {
			continue
		}
		var items []json.RawMessage
		if json.Unmarshal(p.Redacted, &items) != nil {
			continue
		}
		all = append(all, items...)
	}
	if len(all) == 0 {
		return nil
	}
	data, err := json.Marshal(all)
	if err != nil {
		return nil
	}
	return data
}
