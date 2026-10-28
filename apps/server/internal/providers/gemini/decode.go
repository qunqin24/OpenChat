package gemini

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"strings"

	"github.com/qunqin24/openchat/apps/server/internal/llm"
	"github.com/qunqin24/openchat/apps/server/internal/providers/sse"
)

// syntheticIDPrefix marks a tool call id this adapter minted because the
// protocol supplied none.
//
// The prefix is load-bearing rather than decorative: on the way back out, a
// functionResponse is matched to its call by NAME, and an id that the provider
// never issued would be a claim about its own bookkeeping that it cannot check.
// The prefix is how request.go tells an id we invented from one a newer
// deployment actually sent, so that only the second kind is echoed.
const syntheticIDPrefix = "gemini:"

// block is one content block being assembled. Text, reasoning and tool calls
// share a struct because the difference between them is which fields are used.
type block struct {
	index int
	kind  llm.PartKind

	text      strings.Builder
	signature strings.Builder

	callID string
	name   string
	args   string

	// callSignature is the thoughtSignature that came on the functionCall part.
	// It is kept apart from signature, which belongs to a reasoning or text
	// block: only a call's is mandatory to send back.
	callSignature string
}

func (b *block) part() llm.Part {
	switch b.kind {
	case llm.PartReasoning:
		// Prefixed like a call's, so the Anthropic adapter, which replays any
		// signature it does not recognise as its own, can tell this one is not.
		return llm.ReasoningPart{Text: b.text.String(), Signature: callSignature(b.signature.String())}
	case llm.PartToolCall:
		// Arguments and RawArguments are the same bytes on this protocol, and
		// that is the honest answer rather than a shortcut: the two fields
		// exist to separate "what parsed" from "what arrived", and here a call
		// arrives whole or not at all. Anything that reached this point was
		// checked to be a JSON object in functionCall.
		return llm.ToolCallPart{
			ID:           b.callID,
			Name:         b.name,
			Arguments:    json.RawMessage(b.args),
			RawArguments: b.args,
			Signature:    callSignature(b.callSignature),
		}
	default:
		return llm.TextPart{Text: b.text.String(), Signature: callSignature(b.signature.String())}
	}
}

// setSignature files a thoughtSignature that arrived on a text part. A
// reasoning block's are appended, the way its text is. On answer text the last
// one wins: it is a single attestation the API puts on the final part of a
// reply (it may arrive on an empty text part while streaming), and gluing two
// together would corrupt both.
// https://ai.google.dev/gemini-api/docs/generate-content/thought-signatures
func (b *block) setSignature(signature string, kind llm.PartKind) {
	if kind != llm.PartReasoning {
		b.signature.Reset()
	}
	b.signature.WriteString(signature)
}

// callSignature namespaces a thoughtSignature for storage, on a tool call or a
// reasoning block. The prefix is what stops another adapter replaying it, and
// what lets request.go tell a signature this API issued from one it did not.
// Reasoning is never read back here, so only a call's and a text part's are
// ever stripped.
func callSignature(raw string) string {
	if raw == "" {
		return ""
	}
	return llm.SignatureGemini + raw
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
	active *block // the open text or reasoning block, if any

	// toolCount is the ordinal a synthesised id is built from. It counts calls
	// within this turn, which is the only scope a call and its result are ever
	// matched in.
	toolCount int

	rawFinish string
	started   bool
	resp      llm.ModelResponse
}

// decode turns one streamGenerateContent SSE body into events and a response.
//
// model is what was asked for: it seeds ModelResponse.Model so that an endpoint
// which never echoes a model still yields the best answer available, and is
// overwritten the moment the stream says otherwise.
func decode(ctx context.Context, body io.Reader, cfg Config, model string, emit func(llm.Event) error) (llm.ModelResponse, error) {
	a := &assembler{cfg: cfg, emit: emit}
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
		if data == "" {
			continue
		}
		if err := a.frame([]byte(data)); err != nil {
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

	// This protocol has no terminator frame: the stream simply ends. finish()
	// still refuses a stream that never said why it stopped.
	return a.finish()
}

func (a *assembler) send(ev llm.Event) error {
	if err := a.emit(ev); err != nil {
		return emitFailure{err: err}
	}
	return nil
}

func (a *assembler) frame(data []byte) error {
	var frame generateResponse
	if err := json.Unmarshal(data, &frame); err != nil {
		// A frame we cannot read is a hole in the answer. Skipping it would
		// leave the user with text that silently lost a sentence.
		return llm.Errorf(llm.ErrorProtocol, err, "frame is not valid JSON")
	}
	if frame.Error != nil {
		return errorFrom(*frame.Error)
	}
	if frame.PromptFeedback != nil && frame.PromptFeedback.BlockReason != "" {
		// The prompt was refused before the model ever saw it. There is no
		// turn, no finish reason and nothing to publish; reporting it as an
		// empty answer would look like a model with nothing to say.
		return &llm.Error{
			Kind:    llm.ErrorProvider,
			Code:    frame.PromptFeedback.BlockReason,
			Message: "the provider refused the prompt before generating anything",
		}
	}

	if !a.started {
		a.started = true
		if err := a.send(llm.ResponseStartEvent()); err != nil {
			return err
		}
	}
	if frame.ResponseID != "" {
		a.resp.ResponseID = frame.ResponseID
	}
	if frame.ModelVersion != "" {
		a.resp.Model = frame.ModelVersion
	}
	if frame.UsageMetadata != nil {
		// Restated in full on every frame, so the latest one is the answer.
		a.resp.Usage = convertUsage(frame.UsageMetadata)
	}

	for _, c := range frame.Candidates {
		// We never ask for more than one candidate, so anything beyond the
		// first is an answer to a question we did not put. Merging it into the
		// same blocks would interleave two different replies.
		if c.Index != 0 {
			continue
		}
		if err := a.candidate(c); err != nil {
			return err
		}
	}
	return nil
}

func (a *assembler) candidate(c candidate) error {
	for _, part := range c.Content.Parts {
		switch {
		case part.FunctionCall != nil:
			if err := a.functionCall(*part.FunctionCall, part.ThoughtSignature); err != nil {
				return err
			}
		case part.Text != nil:
			// This is the line that decides whether the model's private
			// reasoning ends up in the visible answer.
			kind := llm.PartText
			if part.Thought {
				kind = llm.PartReasoning
			}
			if err := a.textPart(kind, *part.Text, part.ThoughtSignature); err != nil {
				return err
			}
		case part.ThoughtSignature != "":
			// An attestation with nothing attached. It is not content, so it
			// never opens a block of its own.
			a.attachSignature(part.ThoughtSignature)
		default:
			// A part shaped like nothing we know — generated audio or an
			// image, most likely. We never ask for those modalities, so one
			// arriving is the stream not being what it claims to be, and
			// dropping it would be content the user paid for and never saw.
			return llm.Errorf(llm.ErrorProtocol, nil, "a response part carried neither text nor a function call")
		}
	}
	if c.FinishReason != "" {
		a.rawFinish = c.FinishReason
	}
	return nil
}

func (a *assembler) open(kind llm.PartKind) *block {
	b := &block{index: len(a.blocks), kind: kind}
	a.blocks = append(a.blocks, b)
	return b
}

func (a *assembler) textPart(kind llm.PartKind, text, signature string) error {
	if text == "" && signature == "" {
		// An empty text part is the preamble some deployments open with.
		// Opening a block for it would produce an empty part in the history.
		return nil
	}
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

	if signature != "" {
		a.active.setSignature(signature, kind)
	}
	if text == "" {
		return nil
	}
	a.active.text.WriteString(text)
	return a.send(llm.DeltaEvent(a.active.index, kind, text))
}

// functionCall turns one complete call into a block that is born finished.
//
// Nothing accumulates here: the arguments arrive parsed, so the block is opened
// and closed inside this call. Leaving it open would tell the UI that fragments
// are still coming when none ever will be.
func (a *assembler) functionCall(call wireFunctionCall, signature string) error {
	// A tool call ends whatever the model was saying; text after it opens a
	// fresh block.
	if err := a.closeActive(); err != nil {
		return err
	}
	if call.Name == "" {
		return llm.Errorf(llm.ErrorProtocol, nil, "a function call arrived without a name")
	}

	args := strings.TrimSpace(string(call.Args))
	switch {
	case args == "" || args == "null":
		// The one place this protocol is *safer* than the fragment ones. A
		// call here is whole the moment it lands, so absent arguments mean
		// "this call takes none" rather than "the arguments were cut off", and
		// `{}` is the faithful reading rather than an invention.
		args = "{}"
	case args[0] != '{':
		// Every tool schema is an object. Anything else would be handed to an
		// executor that cannot make sense of it.
		return llm.Errorf(llm.ErrorProtocol, nil, "function call %q carried arguments that are not an object", call.Name)
	}

	b := a.open(llm.PartToolCall)
	b.name = call.Name
	b.args = args
	b.callID = call.ID
	if b.callID == "" {
		b.callID = syntheticID(a.toolCount, call.Name)
	}
	a.toolCount++

	if err := a.send(llm.ToolCallStartEvent(b.index, b.callID, b.name)); err != nil {
		return err
	}
	// One delta carrying the whole object. It is display material like every
	// other delta; the authoritative copy is on part_end.
	ev := llm.DeltaEvent(b.index, llm.PartToolCall, args)
	ev.ToolCallID, ev.ToolName = b.callID, b.name
	if err := a.send(ev); err != nil {
		return err
	}
	b.callSignature = signature
	return a.send(llm.PartEndEvent(b.index, b.part()))
}

// syntheticID mints an id for a call the provider did not name one.
//
// It is the ordinal of the call within this turn plus the function's own name:
// deterministic, so decoding the same response twice produces the same ids, and
// unique within the turn, which is the only scope a call and its result are
// matched in. The prefix marks it as ours so it never travels back on the wire.
func syntheticID(ordinal int, name string) string {
	return syntheticIDPrefix + strconv.Itoa(ordinal) + ":" + name
}

// attachSignature files a signature that arrived on its own. It belongs to
// whatever block the model was in the middle of; with no block at all there is
// nothing it attests to, and keeping it would mean inventing one. Only a
// reasoning or answer-text block has a place for one: a function call's arrives
// on the call's own part.
func (a *assembler) attachSignature(signature string) {
	if len(a.blocks) == 0 {
		return
	}
	if b := a.blocks[len(a.blocks)-1]; b.kind != llm.PartToolCall {
		b.setSignature(signature, b.kind)
	}
}

func (a *assembler) closeActive() error {
	if a.active == nil {
		return nil
	}
	b := a.active
	a.active = nil
	return a.send(llm.PartEndEvent(b.index, b.part()))
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
	if err := a.closeActive(); err != nil {
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
	if err := a.closeActive(); err != nil {
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
	case "STOP":
		// This protocol has no "the model asked for a tool" stop reason: a turn
		// that ends in function calls still says STOP. Reading it off what
		// actually arrived is not the guesswork the Chat Completions adapter
		// refuses — the reason is here, it just does not distinguish, and the
		// runner has to know whether another turn follows.
		if a.toolCount > 0 {
			return llm.FinishToolCalls, nil
		}
		return llm.FinishStop, nil

	case "MAX_TOKENS":
		return llm.FinishLength, nil

	case "SAFETY", "RECITATION", "BLOCKLIST", "PROHIBITED_CONTENT", "SPII",
		"IMAGE_SAFETY", "IMAGE_PROHIBITED_CONTENT", "IMAGE_RECITATION", "ESCALATION":
		// The provider stopped the turn over what was being said, which is
		// exactly what FinishContentFilter means. Calling any of these a
		// "stop" would present withheld content as a finished answer, and they
		// stay apart from each other in RawFinishReason, where the difference
		// between a safety hit and a recitation hit survives.
		return llm.FinishContentFilter, nil

	case "MALFORMED_FUNCTION_CALL", "MALFORMED_RESPONSE", "UNEXPECTED_TOOL_CALL", "TOO_MANY_TOOL_CALLS",
		"MISSING_THOUGHT_SIGNATURE", "LANGUAGE", "OTHER", "PUP_LIMITED_DISABLED":
		// The turn ended in something unusable. There is no FinishReason for
		// that, and the nearest ones are both lies: FinishStop would show a
		// broken turn as a complete answer, FinishToolCalls would send the
		// runner looking for a call that does not exist. The stream itself was
		// well formed, so this is the provider failing rather than a protocol
		// violation, and the raw word goes in Code because it is what tells an
		// operator a rejected history (MISSING_THOUGHT_SIGNATURE) from a model
		// that merely mangled a call.
		return "", &llm.Error{
			Kind:    llm.ErrorProvider,
			Code:    a.rawFinish,
			Message: "the provider ended the turn without a usable answer",
		}

	case "":
		// A stream that simply stops is indistinguishable from one that was cut
		// off, and treating the two alike is how a truncated answer gets
		// presented as a complete one.
		return "", llm.Errorf(llm.ErrorProtocol, nil, "stream ended without a finish reason")

	default:
		return "", llm.Errorf(llm.ErrorProtocol, nil, "unknown finish reason %q", a.rawFinish)
	}
}

func convertUsage(u *usageMetadata) *llm.Usage {
	cached := u.CachedContentTokenCount

	return &llm.Usage{
		// promptTokenCount already contains the cached half on this protocol —
		// as on Chat Completions, and unlike the Messages API, where the cache
		// counters sit beside the input total instead of inside it. Subtracting
		// is what makes summing the fields safe rather than producing a bill
		// twice the size of the real one.
		InputTokens: max(u.PromptTokenCount-cached, 0),

		// candidatesTokenCount is the visible answer and thoughtsTokenCount is
		// counted beside it, so OutputTokens — which is documented as
		// everything generated, reasoning included — is their sum.
		OutputTokens:    u.CandidatesTokenCount + u.ThoughtsTokenCount,
		ReasoningTokens: u.ThoughtsTokenCount,

		CacheReadTokens: cached,
		// There is no write counter to map: a cache is created here by a
		// separate API call, so a generate response never reports one.
	}
}
