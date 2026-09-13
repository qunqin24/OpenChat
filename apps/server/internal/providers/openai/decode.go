package openai

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"

	"github.com/qunqin/openchat/apps/server/internal/llm"
	"github.com/qunqin/openchat/apps/server/internal/providers/sse"
)

// doneSentinel is the frame this protocol ends with. It is not JSON, so it has
// to be checked before decoding anything.
const doneSentinel = "[DONE]"

// defaultMaxToolArgumentBytes bounds the arguments of a single tool call. A
// model stuck in a loop emitting JSON is a real failure mode, and the cost of
// noticing it late is the whole process.
const defaultMaxToolArgumentBytes = 1 << 20

// block is one content block being assembled. All three kinds share a struct
// because the difference between them is which fields are used, and keeping
// them together is what makes the open-block bookkeeping a single map.
type block struct {
	index int
	kind  llm.PartKind

	text strings.Builder

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
		return llm.ReasoningPart{Text: b.text.String()}
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
	active *block         // the open text or reasoning block, if any
	tools  map[int]*block // wire tool-call index -> block

	rawFinish string
	started   bool
	resp      llm.ModelResponse
}

// decode turns one Chat Completions SSE body into events and a response.
//
// model is what was asked for: it seeds ModelResponse.Model so that an endpoint
// which never echoes a model still yields the best answer available, and is
// overwritten the moment the stream says otherwise.
func decode(ctx context.Context, body io.Reader, cfg Config, model string, emit func(llm.Event) error) (llm.ModelResponse, error) {
	a := &assembler{cfg: cfg, emit: emit, tools: map[int]*block{}}
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

		data := strings.TrimSpace(scanner.Event().Data)
		switch {
		case data == "":
			continue
		case data == doneSentinel:
			return a.finish()
		}
		if err := a.chunk([]byte(data)); err != nil {
			return a.abort(unwrapEmit(err))
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

	// Some endpoints just close the connection instead of sending [DONE].
	return a.finish()
}

func (a *assembler) send(ev llm.Event) error {
	if err := a.emit(ev); err != nil {
		return emitFailure{err: err}
	}
	return nil
}

func (a *assembler) chunk(data []byte) error {
	var c chunk
	if err := json.Unmarshal(data, &c); err != nil {
		// A frame we cannot read is a hole in the answer. Skipping it would
		// leave the user with text that silently lost a sentence.
		return llm.Errorf(llm.ErrorProtocol, err, "frame is not valid JSON")
	}
	if c.Error != nil {
		return &llm.Error{Kind: llm.ErrorProvider, Code: c.Error.Code, Message: c.Error.Message}
	}

	if !a.started {
		a.started = true
		if err := a.send(llm.ResponseStartEvent()); err != nil {
			return err
		}
	}
	if c.ID != "" {
		a.resp.ResponseID = c.ID
	}
	if c.Model != "" {
		a.resp.Model = c.Model
	}
	if c.Usage != nil {
		a.resp.Usage = convertUsage(c.Usage)
	}

	for _, choice := range c.Choices {
		// We never ask for more than one completion, so anything beyond the
		// first is an answer to a question we did not put. Merging it into the
		// same blocks would interleave two different replies.
		if choice.Index != 0 {
			continue
		}
		if err := a.choice(choice); err != nil {
			return err
		}
	}
	return nil
}

func (a *assembler) choice(ch chunkChoice) error {
	if reasoning := reasoningDelta(ch.Delta); reasoning != "" {
		if err := a.textDelta(llm.PartReasoning, reasoning); err != nil {
			return err
		}
	}
	// Empty content deltas are the role-only preamble most endpoints open with.
	// Opening a block for them would produce an empty part in the history.
	if ch.Delta.Content != nil && *ch.Delta.Content != "" {
		if err := a.textDelta(llm.PartText, *ch.Delta.Content); err != nil {
			return err
		}
	}
	for _, call := range ch.Delta.ToolCalls {
		if err := a.toolDelta(call); err != nil {
			return err
		}
	}
	if ch.FinishReason != nil && *ch.FinishReason != "" {
		a.rawFinish = *ch.FinishReason
	}
	return nil
}

func reasoningDelta(d delta) string {
	if d.ReasoningContent != nil && *d.ReasoningContent != "" {
		return *d.ReasoningContent
	}
	if d.Reasoning != nil {
		return *d.Reasoning
	}
	return ""
}

func (a *assembler) open(kind llm.PartKind) *block {
	b := &block{index: len(a.blocks), kind: kind}
	a.blocks = append(a.blocks, b)
	return b
}

func (a *assembler) textDelta(kind llm.PartKind, text string) error {
	if a.active != nil && a.active.kind != kind {
		// The model switched between thinking and answering. That ends the
		// block: merging the two would lose the order they appeared in, which
		// is the one thing a reader of the transcript needs.
		if err := a.closeActive(); err != nil {
			return err
		}
	}
	if a.active == nil {
		b := a.open(kind)
		if err := a.send(llm.PartStartEvent(b.index, kind)); err != nil {
			return err
		}
		a.active = b
	}

	a.active.text.WriteString(text)
	return a.send(llm.DeltaEvent(a.active.index, kind, text))
}

func (a *assembler) toolDelta(call toolCallDelta) error {
	// A tool call ends whatever the model was saying; text after it opens a
	// fresh block.
	if err := a.closeActive(); err != nil {
		return err
	}

	index := 0
	if call.Index != nil {
		index = *call.Index
	}

	b, known := a.tools[index]
	if !known {
		b = a.open(llm.PartToolCall)
		b.callID, b.name = call.ID, call.Function.Name
		a.tools[index] = b
		// The id and name on part_start are whatever is known right now, which
		// is frequently nothing: several endpoints send them after the first
		// argument fragment. part_end carries the authoritative pair.
		if err := a.send(llm.ToolCallStartEvent(b.index, b.callID, b.name)); err != nil {
			return err
		}
	} else {
		if call.ID != "" {
			b.callID = call.ID
		}
		if call.Function.Name != "" {
			b.name = call.Function.Name
		}
	}

	if call.Function.Arguments == "" {
		return nil
	}
	if limit := a.maxToolArgumentBytes(); b.args.Len()+len(call.Function.Arguments) > limit {
		return llm.Errorf(llm.ErrorProtocol, nil, "tool call %q arguments exceed %d bytes", b.name, limit)
	}
	b.args.WriteString(call.Function.Arguments)

	// The fragment goes out as-is. It is usually not valid JSON on its own, and
	// it is here so the UI can show the arguments filling in — never so anyone
	// can decide something from a half-finished object.
	ev := llm.DeltaEvent(b.index, llm.PartToolCall, call.Function.Arguments)
	ev.ToolCallID, ev.ToolName = b.callID, b.name
	return a.send(ev)
}

func (a *assembler) maxToolArgumentBytes() int {
	if a.cfg.MaxToolArgumentBytes > 0 {
		return a.cfg.MaxToolArgumentBytes
	}
	return defaultMaxToolArgumentBytes
}

func (a *assembler) closeActive() error {
	if a.active == nil {
		return nil
	}
	b := a.active
	a.active = nil
	return a.closeBlock(b)
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
	a.active = nil
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
	case "stop", "end_turn":
		return llm.FinishStop, nil
	case "length", "max_tokens":
		return llm.FinishLength, nil
	case "tool_calls", "function_call":
		return llm.FinishToolCalls, nil
	case "content_filter":
		return llm.FinishContentFilter, nil
	}

	// Inference is opt-in per endpoint. A stream that simply stops is
	// indistinguishable from one that was cut off, so guessing "stop" by
	// default is how a truncated answer gets presented as a complete one.
	if !a.cfg.Compat.InferFinishReason {
		if a.rawFinish == "" {
			return "", llm.Errorf(llm.ErrorProtocol, nil, "stream ended without a finish reason")
		}
		return "", llm.Errorf(llm.ErrorProtocol, nil, "unknown finish reason %q", a.rawFinish)
	}
	if len(a.tools) > 0 {
		return llm.FinishToolCalls, nil
	}
	return llm.FinishStop, nil
}

func convertUsage(u *wireUsage) *llm.Usage {
	cached := 0
	if u.PromptTokensDetails != nil {
		cached = u.PromptTokensDetails.CachedTokens
	}
	if u.PromptCacheHitTokens > 0 {
		cached = u.PromptCacheHitTokens
	}

	// prompt_tokens on this protocol already contains the cached half, so the
	// cache is subtracted out rather than added on. llm.Usage.InputTokens is
	// the part that was really re-read, which is what makes summing the fields
	// safe instead of producing a bill twice the size of the real one.
	usage := &llm.Usage{
		InputTokens:     max(u.PromptTokens-cached, 0),
		OutputTokens:    u.CompletionTokens,
		CacheReadTokens: cached,
	}
	if u.CompletionTokensDetails != nil {
		usage.ReasoningTokens = u.CompletionTokensDetails.ReasoningTokens
	}
	return usage
}
