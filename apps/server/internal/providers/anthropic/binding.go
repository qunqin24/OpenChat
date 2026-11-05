package anthropic

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"slices"
	"strings"

	"github.com/qunqin24/openchat/apps/server/internal/llm"
)

// A thinking block is valid only in the conversation that produced it. Its
// signature records the system prompt, the tools and every message before it
// (earlier thinking blocks excepted), and a chain to the thinking block before
// it; a block sent back after any of that changed is refused with a 400 ("The
// block is bound to a different conversation"), which fails every request of
// the conversation from then on. Our requests do change between runs - a new
// release's system prompt or tools, web search switched on or off, the phone
// app's note, a compressed conversation's summary - so a block is sent back
// only where it is still valid, and the rest are left out, as the API's own
// prefix_mismatch_behavior "drop_block" would leave them. That setting needs a
// beta header gateways on this protocol may not pass, so the same check is
// made here instead.
//
// Each block records (llm.ReasoningPart.Binding) a fingerprint of what came
// before it as it was sent, and which thinking block it followed. On replay a
// block goes only when both still hold: the fingerprint of what now comes
// before it is the same, and the block it followed is the last one going
// before it. The fingerprint is stricter than the API's check (it sees the
// tools' order and the message boundaries too), so a block it keeps the API
// accepts; one it drops the API might have kept, which costs that thinking and
// no more. Blocks with no record - rows written before this - are left out.

const bindingVersion = "v1:"

// prefix is the running fingerprint of a request, block by block.
type prefix [sha256.Size]byte

func startPrefix(system string, tools []map[string]any) prefix {
	encoded, _ := json.Marshal(tools)
	h := sha256.New()
	h.Write([]byte("openchat/anthropic/binding\x00"))
	h.Write([]byte(system))
	h.Write([]byte{0})
	h.Write(encoded)
	var p prefix
	copy(p[:], h.Sum(nil))
	return p
}

func (p prefix) step(kind string, payload []byte) prefix {
	h := sha256.New()
	h.Write(p[:])
	h.Write([]byte(kind))
	h.Write([]byte{0})
	h.Write(payload)
	var next prefix
	copy(next[:], h.Sum(nil))
	return next
}

// open is the start of a message.
func (p prefix) open(role string) prefix { return p.step("message", []byte(role)) }

// add is a block that is part of the prefix. Thinking blocks are not.
func (p prefix) add(block wireContentBlock) prefix {
	if isThinking(block) {
		return p
	}
	block.binding = ""
	if block.Source != nil && block.Source.Data != "" {
		// A picture's or a document's bytes stand in by their digest: the
		// same for the same bytes, without encoding them all again.
		source := *block.Source
		sum := sha256.Sum256([]byte(source.Data))
		source.Data = hex.EncodeToString(sum[:])
		block.Source = &source
	}
	if len(block.Input) > 0 {
		// Arguments go through a jsonb column between the response and the
		// next request: compared as values, not as the bytes they were.
		block.Input = canonicalJSON(block.Input)
	}
	encoded, _ := json.Marshal(block)
	return p.step("block", encoded)
}

func isThinking(block wireContentBlock) bool {
	return block.Type == "thinking" || block.Type == "redacted_thinking"
}

// thinkingID names a thinking block for the chain: its signature, or a
// redacted block's data.
func thinkingID(block wireContentBlock) string {
	raw := block.Signature
	if block.Type == "redacted_thinking" {
		raw = block.Data
	}
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:8])
}

func bindingOf(p prefix, after string) string {
	return bindingVersion + hex.EncodeToString(p[:16]) + ":" + after
}

func canonicalJSON(raw json.RawMessage) json.RawMessage {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if decoder.Decode(&value) != nil {
		return raw
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return raw
	}
	return encoded
}

// binder is where a request leaves off: the fingerprint of all it sends, and
// the last thinking block in it. The response's blocks are bound from there.
type binder struct {
	at   prefix
	last string
	// left is how many of the conversation's thinking blocks were left out.
	left int
}

// keepValidThinking leaves out of messages the thinking blocks that are no
// longer valid where they stand (see above), and every one after a block left
// out but not a leading one, since the chain from it is broken too. A message
// left with nothing goes, and its neighbours, now of the same role, become one
// message - which changes what comes before the blocks after it, so the check
// runs again on the result until nothing more goes.
func keepValidThinking(system string, tools []map[string]any, messages []wireMessage, dropAll bool) ([]wireMessage, binder) {
	before := countThinking(messages)
	for {
		start := startPrefix(system, tools)
		out, end, emptied := filterThinking(start, messages, dropAll)
		if !emptied {
			end.left = before - countThinking(out)
			return out, end
		}
		messages = mergeRoles(out)
	}
}

func countThinking(messages []wireMessage) int {
	n := 0
	for _, message := range messages {
		for _, block := range message.Content {
			if isThinking(block) {
				n++
			}
		}
	}
	return n
}

func filterThinking(p prefix, messages []wireMessage, dropAll bool) (out []wireMessage, end binder, emptied bool) {
	out = make([]wireMessage, 0, len(messages))
	last := ""
	pending := pendingTurn(messages)
	for i, message := range messages {
		p = p.open(message.Role)
		blocks := make([]wireContentBlock, 0, len(message.Content))
		for _, block := range message.Content {
			if !isThinking(block) {
				p = p.add(block)
				blocks = append(blocks, block)
				continue
			}
			// The turn whose tool calls this request answers keeps its
			// thinking whatever its record says: models before the check
			// refuse a tool round whose turn does not start with it. Where the
			// API does check, a block it refuses brings the retry without
			// thinking (client.go), which it takes.
			valid := block.binding == bindingOf(p, last) || i == pending
			if dropAll || !valid {
				continue
			}
			last = thinkingID(block)
			blocks = append(blocks, block)
		}
		if len(blocks) == 0 {
			emptied = true
			continue
		}
		out = append(out, wireMessage{Role: message.Role, Content: blocks})
	}
	return out, binder{at: p, last: last}, emptied
}

// pendingTurn is the index of the assistant turn whose tool calls the
// request's last message answers, -1 when it answers none.
func pendingTurn(messages []wireMessage) int {
	n := len(messages)
	if n < 2 || messages[n-2].Role != "assistant" || messages[n-1].Role != "user" {
		return -1
	}
	for _, block := range messages[n-1].Content {
		if block.Type == "tool_result" {
			return n - 2
		}
	}
	return -1
}

func mergeRoles(messages []wireMessage) []wireMessage {
	out := make([]wireMessage, 0, len(messages))
	for _, message := range messages {
		out = appendMessage(out, message.Role, slices.Clone(message.Content))
	}
	return out
}

// appendMessage adds blocks to messages as a turn of role, joined to the last
// turn when that is of the same role: this protocol wants them alternating.
func appendMessage(messages []wireMessage, role string, blocks []wireContentBlock) []wireMessage {
	if last := len(messages) - 1; last >= 0 && messages[last].Role == role {
		messages[last].Content = append(messages[last].Content, blocks...)
		return messages
	}
	return append(messages, wireMessage{Role: role, Content: blocks})
}

// bind records on the response's thinking blocks the request they answer,
// block by block as they would be sent back (convertAssistant).
func (b binder) bind(parts []llm.Part) []llm.Part {
	p := b.at.open("assistant")
	last := b.last
	out := make([]llm.Part, len(parts))
	for i, part := range parts {
		out[i] = part
		switch v := part.(type) {
		case llm.ReasoningPart:
			if llm.ForeignSignature(v.Signature) {
				continue
			}
			var block wireContentBlock
			switch {
			case len(v.Redacted) > 0:
				block = wireContentBlock{Type: "redacted_thinking", Data: string(v.Redacted)}
			case v.Signature != "":
				block = wireContentBlock{Type: "thinking", Signature: v.Signature}
			default:
				continue // unsigned: never sent back
			}
			v.Binding = bindingOf(p, last)
			last = thinkingID(block)
			out[i] = v
		case llm.TextPart:
			if v.Text != "" {
				p = p.add(wireContentBlock{Type: "text", Text: v.Text})
			}
		case llm.ToolCallPart:
			p = p.add(wireContentBlock{Type: "tool_use", ID: v.ID, Name: v.Name, Input: v.Arguments})
		}
	}
	return out
}

// thinkingRefused is whether a request was refused over a thinking block's
// signature: bound to a conversation that has since changed, or not ours to
// send at all (issued to another organisation, through another channel).
func thinkingRefused(err error) bool {
	var modelErr *llm.Error
	if !errors.As(err, &modelErr) {
		return false
	}
	// "Invalid `signature` in `thinking` block", with or without "The block is
	// bound to a different conversation".
	message := strings.ToLower(modelErr.Message)
	return strings.Contains(message, "invalid") && strings.Contains(message, "signature") &&
		strings.Contains(message, "thinking")
}
