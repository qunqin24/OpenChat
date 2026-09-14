package anthropic

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"

	"github.com/qunqin/openchat/apps/server/internal/llm"
	"github.com/qunqin/openchat/apps/server/internal/providers/sse"
)

// The event names this protocol dispatches on. They are the reason this decoder
// looks nothing like the Chat Completions one: two different events carry a
// field called `delta`, and only the name says which is which.
const (
	eventMessageStart      = "message_start"
	eventContentBlockStart = "content_block_start"
	eventContentBlockDelta = "content_block_delta"
	eventContentBlockStop  = "content_block_stop"
	eventMessageDelta      = "message_delta"
	eventMessageStop       = "message_stop"
	eventPing              = "ping"
	eventError             = "error"
)

// defaultMaxToolArgumentBytes bounds the arguments of a single tool call. A
// model stuck in a loop emitting JSON is a real failure mode, and the cost of
// noticing it late is the whole process.
const defaultMaxToolArgumentBytes = 1 << 20

// block is one content block being assembled. Text, thinking and tool calls
// share a struct because the difference between them is which fields are used,
// and keeping them together is what makes the open-block bookkeeping one map.
type block struct {
	index int
	kind  llm.PartKind

	text strings.Builder

	// signature accumulates signature_delta. It is the provider's attestation
	// over the thinking text, and it is never emitted as a delta: it is an
	// opaque blob, and a UI appending it to the reasoning stream would show the
	// user a page of base64 in the middle of a thought.
	signature strings.Builder

	// redacted is thinking the provider encrypted. It arrives whole on
	// content_block_start with no deltas behind it, because there is nothing to
	// display — only something that has to survive the round trip.
	redacted []byte

	callID string
	name   string
	args   strings.Builder

	closed bool
}

// part freezes the block. For a tool call this is the one and only place the
// arguments are parsed: every earlier look at them was a fragment.
func (b *block) part() llm.Part {
	switch b.kind {
	case llm.PartReasoning:
		return llm.ReasoningPart{Text: b.text.String(), Signature: b.signature.String(), Redacted: b.redacted}
	case llm.PartToolCall:
		return b.toolCall()
	default:
		return llm.TextPart{Text: b.text.String()}
	}
}

func (b *block) toolCall() llm.ToolCallPart {
	call := llm.ToolCallPart{ID: b.callID, Name: b.name, RawArguments: b.args.String()}

	trimmed := strings.TrimSpace(call.RawArguments)
	switch {
	case trimmed == "":
		// A call with no arguments at all. Whether that is legal depends on the
		// tool's schema, so it stays empty instead of being promoted to `{}`:
		// inventing arguments is how a best-effort parse turns into an
		// execution nobody asked for.
	case !json.Valid([]byte(trimmed)):
		// Kept as RawArguments only. The runner can tell the model its
		// arguments were malformed; it cannot execute them.
	default:
		call.Arguments = json.RawMessage(trimmed)
	}
	return call
}

// emitFailure marks an error as the run layer's rather than the stream's, so
// the top of decode can hand it back untouched instead of dressing it up as a
// protocol problem.
type emitFailure struct{ err error }

func (e emitFailure) Error() string { return e.err.Error() }
func (e emitFailure) Unwrap() error { return e.err }

func unwrapEmit(err error) error {
	var failure emitFailure
	if errors.As(err, &failure) {
		return failure.err
	}
	return err
}

type assembler struct {
	cfg  Config
	emit func(llm.Event) error

	blocks []*block
	// open maps the wire's own block index onto ours. They usually agree, but
	// ours is an ordinal over the blocks we actually opened and the wire's is
	// the endpoint's bookkeeping; tying them together is the endpoint's job to
	// get right, not ours to assume.
	open map[int]*block

	// usage is assembled from two events. message_start knows the input half,
	// message_delta the output half, and neither is a complete answer alone.
	usage     llm.Usage
	haveUsage bool

	rawFinish string
	started   bool
	resp      llm.ModelResponse
}

// decode turns one Messages SSE body into events and a response.
//
// model is what was asked for: it seeds ModelResponse.Model so that an endpoint
// which never echoes a model still yields the best answer available, and is
// overwritten the moment the stream says otherwise.
func decode(ctx context.Context, body io.Reader, cfg Config, model string, emit func(llm.Event) error) (llm.ModelResponse, error) {
	a := &assembler{cfg: cfg, emit: emit, open: map[int]*block{}}
	a.resp.Provider = cfg.Provider
	a.resp.Model = model

	scanner := sse.NewScanner(body)
	for {
		// Checked between frames as well as inside the read: a stream that
		// keeps arriving would otherwise run to completion after Stop.
		if err := ctx.Err(); err != nil {
			return a.cancel(err)
		}
		if !scanner.Scan() {
			break
		}

		frame := scanner.Event()
		data := strings.TrimSpace(frame.Data)
		if data == "" {
			continue
		}
		done, err := a.frame(frame.Name, []byte(data))
		if err != nil {
			return a.abort(unwrapEmit(err))
		}
		if done {
			return a.finish()
		}
	}

	if err := scanner.Err(); err != nil {
		// A read that fails while the context is done is the cancellation
		// arriving, not a broken endpoint.
		if ctx.Err() != nil {
			return a.cancel(ctx.Err())
		}
		if errors.Is(err, sse.ErrTruncated) {
			return a.abort(llm.Errorf(llm.ErrorProtocol, err, "the model stream was cut off"))
		}
		return a.abort(llm.Errorf(llm.ErrorTransport, err, "reading the model stream"))
	}

	// A stream that ran out without message_stop. finish() still refuses it if
	// no stop reason ever arrived.
	return a.finish()
}

func (a *assembler) send(ev llm.Event) error {
	if err := a.emit(ev); err != nil {
		return emitFailure{err: err}
	}
	return nil
}

// frame handles one dispatched event and reports whether the turn is over.
//
// The name is what decides, not the payload: content_block_delta and
// message_delta both carry a `delta` object and mean completely different
// things, so reading the body first and guessing from its shape is how a stop
// reason turns into a text fragment. The payload's own `type` is only a
// fallback, for relays that forward the data lines and drop the event ones.
func (a *assembler) frame(name string, data []byte) (bool, error) {
	var ev streamEvent
	if err := json.Unmarshal(data, &ev); err != nil {
		// A frame we cannot read is a hole in the answer. Skipping it would
		// leave the user with text that silently lost a sentence.
		return false, llm.Errorf(llm.ErrorProtocol, err, "frame is not valid JSON")
	}
	if name == "" {
		name = ev.Type
	}

	switch name {
	case eventError:
		if ev.Error == nil {
			return false, llm.Errorf(llm.ErrorProtocol, nil, "error event carried no error")
		}
		return false, errorFrom(*ev.Error)
	case eventPing:
		// A heartbeat. It says the connection is alive and nothing else.
		return false, nil
	case eventMessageStart:
		return false, a.messageStart(ev)
	case eventContentBlockStart:
		return false, a.blockStart(ev)
	case eventContentBlockDelta:
		return false, a.blockDelta(ev)
	case eventContentBlockStop:
		return false, a.blockStop(ev)
	case eventMessageDelta:
		return false, a.messageDelta(ev)
	case eventMessageStop:
		return true, nil
	default:
		// This protocol documents new event types as additive and says clients
		// must ignore the ones they do not know. That is a promise about
		// *events*; an unknown content block is a different matter, because it
		// would be content we silently failed to show — see blockStart.
		return false, nil
	}
}

// begin announces the turn once. It is called from every event that can carry
// content rather than from message_start alone: a relay that drops the opening
// event still produces blocks, and a block that arrives before anything said
// the turn was alive leaves the UI sitting on "waiting" while text streams in.
func (a *assembler) begin() error {
	if a.started {
		return nil
	}
	a.started = true
	return a.send(llm.ResponseStartEvent())
}

func (a *assembler) messageStart(ev streamEvent) error {
	if err := a.begin(); err != nil {
		return err
	}
	if ev.Message == nil {
		return llm.Errorf(llm.ErrorProtocol, nil, "message_start carried no message")
	}
	if ev.Message.ID != "" {
		a.resp.ResponseID = ev.Message.ID
	}
	if ev.Message.Model != "" {
		a.resp.Model = ev.Message.Model
	}
	if u := ev.Message.Usage; u != nil {
		a.haveUsage = true
		// The input half, and only the input half. output_tokens here is the
		// count so far — a 1 or a 2 — and message_delta replaces it.
		a.usage.InputTokens = u.InputTokens
		a.usage.CacheReadTokens = u.CacheReadInputTokens
		a.usage.CacheWriteTokens = u.CacheCreationInputTokens
	}
	return nil
}

func (a *assembler) blockStart(ev streamEvent) error {
	if err := a.begin(); err != nil {
		return err
	}
	if ev.Index == nil || ev.ContentBlock == nil {
		return llm.Errorf(llm.ErrorProtocol, nil, "content_block_start is missing its index or its block")
	}
	if _, dup := a.open[*ev.Index]; dup {
		return llm.Errorf(llm.ErrorProtocol, nil, "block %d started twice", *ev.Index)
	}

	cb := ev.ContentBlock
	var kind llm.PartKind
	switch cb.Type {
	case "text":
		kind = llm.PartText
	case "thinking", "redacted_thinking":
		kind = llm.PartReasoning
	case "tool_use":
		kind = llm.PartToolCall
	default:
		// We never ask for the features that produce any other block type, so
		// one arriving is the stream not being what it claims to be. Dropping
		// it would be content the user paid for and never saw.
		return llm.Errorf(llm.ErrorProtocol, nil, "unknown content block type %q", cb.Type)
	}

	b := &block{index: len(a.blocks), kind: kind}
	a.blocks = append(a.blocks, b)
	a.open[*ev.Index] = b

	start := llm.PartStartEvent(b.index, kind)
	if kind == llm.PartToolCall {
		b.callID, b.name = cb.ID, cb.Name
		// Unlike Chat Completions, this protocol sends the id and the name up
		// front, so part_start can carry both.
		start = llm.ToolCallStartEvent(b.index, b.callID, b.name)
		// `input` is `{}` on a streamed tool_use and the arguments follow as
		// fragments. A relay that puts the finished object here instead is the
		// one case worth reading, and reading it costs nothing.
		if seed := strings.TrimSpace(string(cb.Input)); seed != "" && seed != "{}" && seed != "null" {
			b.args.WriteString(seed)
		}
	}
	if err := a.send(start); err != nil {
		return err
	}

	// A block can open with content already in it. Text and thinking normally
	// open empty, but redacted_thinking arrives entirely here.
	if len(cb.Data) > 0 {
		b.redacted = []byte(cb.Data)
	}
	if cb.Signature != "" {
		b.signature.WriteString(cb.Signature)
	}
	switch {
	case kind == llm.PartText && cb.Text != "":
		return a.textDelta(b, cb.Text)
	case kind == llm.PartReasoning && cb.Thinking != "":
		return a.textDelta(b, cb.Thinking)
	}
	return nil
}

func (a *assembler) blockDelta(ev streamEvent) error {
	if ev.Index == nil || ev.Delta == nil {
		return llm.Errorf(llm.ErrorProtocol, nil, "content_block_delta is missing its index or its delta")
	}
	b, known := a.open[*ev.Index]
	if !known {
		return llm.Errorf(llm.ErrorProtocol, nil, "delta for block %d, which never started", *ev.Index)
	}

	d := ev.Delta
	switch d.Type {
	case "text_delta":
		if b.kind != llm.PartText {
			return a.mismatch(d.Type, b)
		}
		return a.textDelta(b, d.Text)

	case "thinking_delta":
		if b.kind != llm.PartReasoning {
			return a.mismatch(d.Type, b)
		}
		return a.textDelta(b, d.Thinking)

	case "signature_delta":
		if b.kind != llm.PartReasoning {
			return a.mismatch(d.Type, b)
		}
		// Accumulated, never emitted. It is not something to render, it is
		// something the provider will want handed back verbatim next turn.
		b.signature.WriteString(d.Signature)
		return nil

	case "input_json_delta":
		if b.kind != llm.PartToolCall {
			return a.mismatch(d.Type, b)
		}
		return a.toolDelta(b, d.PartialJSON)

	default:
		// Unlike an unknown event, an unknown delta belongs to a block that is
		// already open and whose content we are on the hook for.
		return llm.Errorf(llm.ErrorProtocol, nil, "unknown delta type %q on block %d", d.Type, *ev.Index)
	}
}

func (a *assembler) mismatch(deltaType string, b *block) error {
	return llm.Errorf(llm.ErrorProtocol, nil, "%s arrived on a %s block", deltaType, b.kind)
}

func (a *assembler) textDelta(b *block, text string) error {
	if text == "" {
		return nil
	}
	b.text.WriteString(text)
	return a.send(llm.DeltaEvent(b.index, b.kind, text))
}

func (a *assembler) toolDelta(b *block, fragment string) error {
	if fragment == "" {
		return nil
	}
	if limit := a.maxToolArgumentBytes(); b.args.Len()+len(fragment) > limit {
		return llm.Errorf(llm.ErrorProtocol, nil, "tool call %q arguments exceed %d bytes", b.name, limit)
	}
	b.args.WriteString(fragment)

	// The fragment goes out as-is. It is usually not valid JSON on its own, and
	// it is here so the UI can show the arguments filling in — never so anyone
	// can decide something from a half-finished object.
	ev := llm.DeltaEvent(b.index, llm.PartToolCall, fragment)
	ev.ToolCallID, ev.ToolName = b.callID, b.name
	return a.send(ev)
}

func (a *assembler) maxToolArgumentBytes() int {
	if a.cfg.MaxToolArgumentBytes > 0 {
		return a.cfg.MaxToolArgumentBytes
	}
	return defaultMaxToolArgumentBytes
}

func (a *assembler) blockStop(ev streamEvent) error {
	if ev.Index == nil {
		return llm.Errorf(llm.ErrorProtocol, nil, "content_block_stop is missing its index")
	}
	b, known := a.open[*ev.Index]
	if !known {
		return llm.Errorf(llm.ErrorProtocol, nil, "stop for block %d, which never started", *ev.Index)
	}
	delete(a.open, *ev.Index)
	return a.closeBlock(b)
}

func (a *assembler) messageDelta(ev streamEvent) error {
	if ev.Delta != nil && ev.Delta.StopReason != "" {
		a.rawFinish = ev.Delta.StopReason
	}
	if u := ev.Usage; u != nil {
		a.haveUsage = true
		// The output half. Cumulative for the whole message, so it replaces
		// rather than adds to whatever message_start guessed.
		a.usage.OutputTokens = u.OutputTokens

		// A message_delta may repeat the input side on some deployments. Taking
		// it only when it is non-zero keeps a deployment that omits it from
		// zeroing what message_start already told us.
		if u.InputTokens > 0 {
			a.usage.InputTokens = u.InputTokens
		}
		if u.CacheReadInputTokens > 0 {
			a.usage.CacheReadTokens = u.CacheReadInputTokens
		}
		if u.CacheCreationInputTokens > 0 {
			a.usage.CacheWriteTokens = u.CacheCreationInputTokens
		}
	}
	return nil
}

func (a *assembler) closeBlock(b *block) error {
	if b.closed {
		return nil
	}
	b.closed = true
	return a.send(llm.PartEndEvent(b.index, b.part()))
}

// closeAll ends every open block in the order they were opened, so the parts of
// the message read the way they were generated.
func (a *assembler) closeAll() error {
	clear(a.open)
	for _, b := range a.blocks {
		if err := a.closeBlock(b); err != nil {
			return err
		}
	}
	return nil
}

// response is the turn as it stands right now, partial or not.
func (a *assembler) response() llm.ModelResponse {
	resp := a.resp
	parts := make([]llm.Part, 0, len(a.blocks))
	for _, b := range a.blocks {
		parts = append(parts, b.part())
	}
	resp.Message = llm.Message{Role: llm.RoleAssistant, Parts: parts}
	if a.haveUsage {
		usage := a.usage
		resp.Usage = &usage
	}
	return resp
}

func (a *assembler) finish() (llm.ModelResponse, error) {
	if err := a.closeAll(); err != nil {
		return a.response(), unwrapEmit(err)
	}
	reason, err := a.finishReason()
	if err != nil {
		return a.response(), err
	}

	a.resp.FinishReason = reason
	a.resp.RawFinishReason = a.rawFinish
	resp := a.response()
	if err := a.send(llm.ResponseEndEvent(resp)); err != nil {
		return resp, unwrapEmit(err)
	}
	return resp, nil
}

// cancel ends the turn the way a Stop should: every block that arrived is
// closed, the content is kept, and exactly one response_end goes out — a
// cancelled turn and a completed one look identical to whatever is counting
// terminal events, which is what makes "one terminal state" checkable.
func (a *assembler) cancel(cause error) (llm.ModelResponse, error) {
	if err := a.closeAll(); err != nil {
		return a.response(), unwrapEmit(err)
	}

	a.resp.FinishReason = llm.FinishCancelled
	a.resp.RawFinishReason = a.rawFinish
	resp := a.response()
	if err := a.send(llm.ResponseEndEvent(resp)); err != nil {
		return resp, unwrapEmit(err)
	}
	return resp, cause
}

// abort is the broken-stream path. No blocks are closed and no response_end is
// emitted, because a stream that died mid-frame has no boundary worth
// publishing. What arrived still comes back, with FinishReason left empty so
// that nothing downstream can mistake it for a finished answer.
func (a *assembler) abort(err error) (llm.ModelResponse, error) {
	return a.response(), err
}

func (a *assembler) finishReason() (llm.FinishReason, error) {
	switch a.rawFinish {
	case "end_turn", "stop_sequence":
		// A stop sequence is a stop the caller asked for. RawFinishReason keeps
		// which of the two it was, because our mapping cannot.
		return llm.FinishStop, nil
	case "max_tokens":
		return llm.FinishLength, nil
	case "tool_use":
		return llm.FinishToolCalls, nil
	case "refusal":
		return llm.FinishContentFilter, nil
	case "":
		// A stream that simply stops is indistinguishable from one that was cut
		// off, and treating the two alike is how a truncated answer gets
		// presented as a complete one.
		return "", llm.Errorf(llm.ErrorProtocol, nil, "stream ended without a stop reason")
	default:
		return "", llm.Errorf(llm.ErrorProtocol, nil, "unknown stop reason %q", a.rawFinish)
	}
}
