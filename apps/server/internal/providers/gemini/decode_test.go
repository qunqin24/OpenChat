package gemini

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/qunqin24/openchat/apps/server/internal/llm"
	"github.com/qunqin24/openchat/apps/server/internal/llm/llmtest"
)

// The fixtures under testdata are real frame shapes: whole response objects,
// one per frame, the way alt=sse serves them. They are the whole point of this
// file — Phase A is judged on parsing, and parsing is only as correct as the
// samples it was checked against.

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("reading fixture: %v", err)
	}
	return data
}

func decodeFixture(t *testing.T, name string, cfg Config) (*llmtest.Recorder, llm.ModelResponse, error) {
	t.Helper()
	rec := &llmtest.Recorder{}
	resp, err := decode(context.Background(), bytes.NewReader(fixture(t, name)), cfg, "requested-model", rec.Emit)
	rec.CheckProviderInvariants(t)
	return rec, resp, err
}

func kinds(parts []llm.Part) []llm.PartKind {
	out := make([]llm.PartKind, 0, len(parts))
	for _, part := range parts {
		out = append(out, part.Kind())
	}
	return out
}

func TestStreamAssemblesPlainText(t *testing.T) {
	rec, resp, err := decodeFixture(t, "text.sse", Config{Provider: "google"})
	if err != nil {
		t.Fatalf("decode: %v", err)
	}

	if got := resp.Message.Text(); got != "The aperture is open." {
		t.Errorf("text = %q", got)
	}
	if got, want := kinds(resp.Message.Parts), []llm.PartKind{llm.PartText}; !slices.Equal(got, want) {
		t.Errorf("parts = %v, want %v — the empty trailing part must not open a block", got, want)
	}
	if resp.FinishReason != llm.FinishStop || resp.RawFinishReason != "STOP" {
		t.Errorf("finish = %q / raw %q", resp.FinishReason, resp.RawFinishReason)
	}
	if resp.Model != "gemini-2.5-flash" {
		t.Errorf("model = %q, want the one the provider echoed, not the one requested", resp.Model)
	}
	if resp.ResponseID != "resp-text" {
		t.Errorf("response id = %q", resp.ResponseID)
	}

	want := []llm.EventType{
		llm.EventResponseStart,
		llm.EventPartStart,
		llm.EventPartDelta,
		llm.EventPartDelta,
		llm.EventPartEnd,
		llm.EventResponseEnd,
	}
	if got := rec.Types(); !slices.Equal(got, want) {
		t.Errorf("events = %v, want %v", got, want)
	}
}

func TestStreamReadsAThoughtPartAsReasoningNotAsTheAnswer(t *testing.T) {
	// `"thought": true` is the only thing separating the model's private
	// deliberation from the answer. Missing it publishes the deliberation.
	rec, resp, err := decodeFixture(t, "thinking.sse", Config{Provider: "google"})
	if err != nil {
		t.Fatalf("decode: %v", err)
	}

	want := []llm.PartKind{llm.PartReasoning, llm.PartText, llm.PartReasoning, llm.PartText}
	if got := kinds(resp.Message.Parts); !slices.Equal(got, want) {
		t.Fatalf("parts = %v, want %v — merging the two kinds loses the order they appeared in", got, want)
	}

	if got := resp.Message.Text(); got != "An aperture is an opening. Its size is the f-number." {
		t.Errorf("text = %q, reasoning must not leak into the answer", got)
	}
	if got, want := rec.Deltas(llm.PartReasoning), "The user asks about apertures.Should I add the f-number?"; got != want {
		t.Errorf("reasoning deltas = %q, want %q", got, want)
	}

	// A thoughtSignature on a thought part has a field of its own to land in.
	first, ok := resp.Message.Parts[0].(llm.ReasoningPart)
	if !ok {
		t.Fatalf("first part is %T", resp.Message.Parts[0])
	}
	if first.Signature != llm.SignatureGemini+"CvUBAdHtim8=" {
		t.Errorf("signature = %q", first.Signature)
	}
	if strings.Contains(rec.Deltas(llm.PartReasoning), "CvUB") {
		t.Error("the signature reached the visible reasoning stream")
	}
}

func TestStreamTakesAFunctionCallWholeRatherThanInFragments(t *testing.T) {
	rec, resp, err := decodeFixture(t, "tool_call.sse", Config{Provider: "google"})
	if err != nil {
		t.Fatalf("decode: %v", err)
	}

	if got, want := kinds(resp.Message.Parts), []llm.PartKind{llm.PartText, llm.PartToolCall, llm.PartToolCall}; !slices.Equal(got, want) {
		t.Fatalf("parts = %v, want %v", got, want)
	}
	calls := resp.Message.ToolCalls()

	// Arguments and RawArguments are the same bytes here, and that is the
	// truthful answer: nothing was accumulated, so "what arrived" and "what
	// parsed" are genuinely one thing.
	if !calls[0].Parsed() {
		t.Fatal("a complete call did not parse")
	}
	if string(calls[0].Arguments) != calls[0].RawArguments {
		t.Errorf("arguments = %s but raw = %s; on this protocol they cannot differ", calls[0].Arguments, calls[0].RawArguments)
	}
	var args struct {
		Query string `json:"query"`
		Limit int    `json:"limit"`
	}
	if err := json.Unmarshal(calls[0].Arguments, &args); err != nil {
		t.Fatalf("unmarshal arguments: %v", err)
	}
	if args.Query != "quarterly report" || args.Limit != 5 {
		t.Errorf("arguments = %+v", args)
	}

	// The protocol supplies no id, so one is minted from the call's position
	// in the turn and its name. Deterministic, unique within the turn, and
	// marked as ours so it never travels back on the wire.
	if calls[0].ID != "gemini:0:search_notes" || calls[1].ID != "gemini:1:read_note" {
		t.Errorf("ids = %q / %q", calls[0].ID, calls[1].ID)
	}
	if !isSynthetic(calls[0].ID) {
		t.Error("a minted id is not recognisable as one, so it would be echoed back to the provider")
	}

	// A call arrives whole, so its block is born finished: opened, written and
	// closed before the next part is looked at. Leaving it open would promise
	// fragments that are never coming.
	var order []llm.EventType
	for _, ev := range rec.Events {
		if ev.PartKind == llm.PartToolCall {
			order = append(order, ev.Type)
		}
	}
	want := []llm.EventType{
		llm.EventPartStart, llm.EventPartDelta, llm.EventPartEnd,
		llm.EventPartStart, llm.EventPartDelta, llm.EventPartEnd,
	}
	if !slices.Equal(order, want) {
		t.Errorf("tool events = %v, want %v", order, want)
	}

	// This protocol never says "tool_calls"; a turn that ends in calls still
	// says STOP, and the runner has to know another turn follows.
	if resp.FinishReason != llm.FinishToolCalls {
		t.Errorf("finish reason = %q, want tool_calls derived from what arrived", resp.FinishReason)
	}
	if resp.RawFinishReason != "STOP" {
		t.Errorf("raw finish reason = %q, want the provider's own word kept", resp.RawFinishReason)
	}
}

func TestStreamReadsAbsentArgumentsAsNoArgumentsNotAsATruncation(t *testing.T) {
	// The one place this protocol is safer than the fragment ones: a call is
	// whole the moment it lands, so an absent `args` means the tool takes none.
	stream := `data: {"candidates":[{"content":{"parts":[{"functionCall":{"name":"list_notes"}}],"role":"model"},"finishReason":"STOP","index":0}]}` + "\n\n"

	rec := &llmtest.Recorder{}
	resp, err := decode(context.Background(), strings.NewReader(stream), Config{Provider: "google"}, "m", rec.Emit)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	rec.CheckProviderInvariants(t)

	call := resp.Message.ToolCalls()[0]
	if !call.Parsed() || string(call.Arguments) != "{}" {
		t.Errorf("arguments = %s, want an empty object: the call is complete, it just takes nothing", call.Arguments)
	}
	if call.RawArguments != "{}" {
		t.Errorf("raw arguments = %q", call.RawArguments)
	}
}

func TestStreamKeepsAProviderSuppliedCallID(t *testing.T) {
	// Newer deployments do send one. It is the provider's own bookkeeping and
	// is worth more than anything we could mint, so it wins.
	stream := `data: {"candidates":[{"content":{"parts":[{"functionCall":{"id":"fc_abc123","name":"read_note","args":{"id":"n-1"}}}],"role":"model"},"finishReason":"STOP","index":0}]}` + "\n\n"

	rec := &llmtest.Recorder{}
	resp, err := decode(context.Background(), strings.NewReader(stream), Config{Provider: "google"}, "m", rec.Emit)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	rec.CheckProviderInvariants(t)

	call := resp.Message.ToolCalls()[0]
	if call.ID != "fc_abc123" {
		t.Errorf("id = %q, want the provider's own", call.ID)
	}
	if isSynthetic(call.ID) {
		t.Error("a provider id was mistaken for a minted one and would not be echoed back")
	}
}

func TestStreamPutsAFunctionCallSignatureOnTheCallItself(t *testing.T) {
	// Gemini 3 400s the next request of a tool loop unless the signature comes
	// back on this very call, so it has to travel with the ToolCallPart.
	stream := `data: {"candidates":[{"content":{"parts":[{"functionCall":{"name":"read_note","args":{"id":"n-1"}},"thoughtSignature":"CvUBsig="},{"functionCall":{"name":"read_note","args":{"id":"n-2"}}}],"role":"model"},"finishReason":"STOP","index":0}]}` + "\n\n"

	rec := &llmtest.Recorder{}
	resp, err := decode(context.Background(), strings.NewReader(stream), Config{Provider: "google"}, "m", rec.Emit)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	rec.CheckProviderInvariants(t)

	calls := resp.Message.ToolCalls()
	if len(calls) != 2 {
		t.Fatalf("calls = %v", calls)
	}
	if want := llm.SignatureGemini + "CvUBsig="; calls[0].Signature != want {
		t.Errorf("signature = %q, want %q", calls[0].Signature, want)
	}
	if calls[1].Signature != "" {
		t.Errorf("second parallel call signature = %q, only the first carries one", calls[1].Signature)
	}
	// The part_end event is what the run layer stores, so it must carry it too.
	var ended llm.ToolCallPart
	for _, ev := range rec.OfType(llm.EventPartEnd) {
		if call, ok := ev.Part.(llm.ToolCallPart); ok && call.Signature != "" {
			ended = call
		}
	}
	if ended.Signature != calls[0].Signature {
		t.Errorf("part_end signature = %q", ended.Signature)
	}
	if resp.Metadata != nil {
		t.Errorf("metadata = %v, signatures no longer live there", resp.Metadata)
	}
}

func TestStreamCombinesThoughtsIntoTheOutputTotalAndSplitsOutTheCache(t *testing.T) {
	_, resp, err := decodeFixture(t, "usage_finish.sse", Config{Provider: "google"})
	if err != nil {
		t.Fatalf("decode: %v", err)
	}

	if resp.Usage == nil {
		t.Fatal("usage is nil")
	}
	// promptTokenCount is 1200 with 800 of it cached — the Chat Completions
	// convention, not the Messages one — so the cache is subtracted out.
	// candidatesTokenCount counts only the visible answer, so the 64 thought
	// tokens are added to it and then named as the reasoning subset.
	want := llm.Usage{InputTokens: 400, OutputTokens: 160, ReasoningTokens: 64, CacheReadTokens: 800}
	if *resp.Usage != want {
		t.Errorf("usage = %+v, want %+v", *resp.Usage, want)
	}
	if resp.FinishReason != llm.FinishLength || resp.RawFinishReason != "MAX_TOKENS" {
		t.Errorf("finish = %q / raw %q", resp.FinishReason, resp.RawFinishReason)
	}
}

func TestStreamTreatsASafetyStopAsWithheldContentNotACleanFinish(t *testing.T) {
	// The provider stopped the turn over what was being said. Calling it a
	// stop would present half an answer as a finished one; the run layer needs
	// to be able to tell the user why the rest never came.
	rec, resp, err := decodeFixture(t, "safety.sse", Config{Provider: "google"})
	if err != nil {
		t.Fatalf("decode: %v", err)
	}

	if resp.FinishReason != llm.FinishContentFilter {
		t.Errorf("finish reason = %q, want content_filter", resp.FinishReason)
	}
	if resp.RawFinishReason != "SAFETY" {
		t.Errorf("raw finish reason = %q — our mapping is lossy, the original is not", resp.RawFinishReason)
	}
	if got := resp.Message.Text(); got != "Here is how you " {
		t.Errorf("text = %q, what arrived before the stop is still what the user saw", got)
	}
	// A stopped turn is still a turn that ended: the block closes and exactly
	// one response_end goes out.
	if ends := rec.OfType(llm.EventResponseEnd); len(ends) != 1 {
		t.Errorf("response_end count = %d, want exactly one", len(ends))
	}
}

func TestStreamMapsEveryFinishReasonItAccepts(t *testing.T) {
	tests := []struct {
		raw   string
		tools int
		want  llm.FinishReason
	}{
		{raw: "STOP", want: llm.FinishStop},
		{raw: "STOP", tools: 1, want: llm.FinishToolCalls},
		{raw: "MAX_TOKENS", want: llm.FinishLength},
		{raw: "SAFETY", want: llm.FinishContentFilter},
		{raw: "RECITATION", want: llm.FinishContentFilter},
		{raw: "PROHIBITED_CONTENT", want: llm.FinishContentFilter},
		{raw: "IMAGE_RECITATION", want: llm.FinishContentFilter},
		{raw: "ESCALATION", want: llm.FinishContentFilter},
	}

	for _, tc := range tests {
		t.Run(tc.raw, func(t *testing.T) {
			a := &assembler{rawFinish: tc.raw, toolCount: tc.tools}
			got, err := a.finishReason()
			if err != nil {
				t.Fatalf("finishReason: %v", err)
			}
			if got != tc.want {
				t.Errorf("finish reason = %q, want %q", got, tc.want)
			}
		})
	}

	for _, raw := range []string{
		"MALFORMED_FUNCTION_CALL", "MALFORMED_RESPONSE", "UNEXPECTED_TOOL_CALL", "TOO_MANY_TOOL_CALLS",
		"MISSING_THOUGHT_SIGNATURE", "LANGUAGE", "OTHER", "PUP_LIMITED_DISABLED",
	} {
		t.Run(raw+" is the provider failing", func(t *testing.T) {
			// There is no FinishReason for "the turn ended in something
			// unusable", and both nearby ones would lie about it.
			a := &assembler{rawFinish: raw}
			_, err := a.finishReason()
			if !llm.IsKind(err, llm.ErrorProvider) {
				t.Fatalf("error = %v, want a provider error: the stream was fine, the turn was not", err)
			}
			var failure *llm.Error
			if !errors.As(err, &failure) || failure.Code != raw {
				t.Errorf("error = %v, want the raw reason %q in its code", err, raw)
			}
		})
	}

	t.Run("a finish reason nobody has seen before", func(t *testing.T) {
		a := &assembler{rawFinish: "SOMETHING_NEW"}
		if _, err := a.finishReason(); !llm.IsKind(err, llm.ErrorProtocol) {
			t.Fatalf("error = %v, want a protocol error rather than a guess", err)
		}
	})
}

func TestStreamWithoutAFinishReasonIsAProtocolError(t *testing.T) {
	// This protocol has no terminator frame, so a stream that stops early is
	// indistinguishable from one that finished — unless the reason is required.
	stream := `data: {"candidates":[{"content":{"parts":[{"text":"cut short"}],"role":"model"},"index":0}]}` + "\n\n"

	rec := &llmtest.Recorder{}
	resp, err := decode(context.Background(), strings.NewReader(stream), Config{Provider: "google"}, "m", rec.Emit)
	rec.CheckProviderInvariants(t)

	if !llm.IsKind(err, llm.ErrorProtocol) {
		t.Fatalf("error = %v, want a protocol error", err)
	}
	if resp.FinishReason != "" {
		t.Errorf("finish reason = %q, want empty so nothing reads it as complete", resp.FinishReason)
	}
	if len(rec.OfType(llm.EventResponseEnd)) != 0 {
		t.Error("a stream that never said why it stopped must not publish a finished response")
	}
}

func TestStreamMalformedFrameIsAnErrorNotASilentTruncation(t *testing.T) {
	rec, resp, err := decodeFixture(t, "malformed.sse", Config{Provider: "google"})
	if !llm.IsKind(err, llm.ErrorProtocol) {
		t.Fatalf("error = %v, want a protocol error", err)
	}
	if got := resp.Message.Text(); got != "Half a sentence" {
		t.Errorf("text = %q, the content that did arrive must survive", got)
	}
	if resp.FinishReason != "" {
		t.Errorf("finish reason = %q, want empty", resp.FinishReason)
	}
	if len(rec.OfType(llm.EventResponseEnd)) != 0 {
		t.Error("a broken stream has no boundary to publish")
	}
	// The frame after the broken one carries the rest of the sentence and a
	// STOP. Reading past the hole would have turned a lost clause into a
	// complete answer.
	if strings.Contains(resp.Message.Text(), "the rest of it") {
		t.Error("decoding continued past the malformed frame")
	}
}

func TestStreamCutOffMidFrameIsAnError(t *testing.T) {
	rec, resp, err := decodeFixture(t, "truncated.sse", Config{Provider: "google"})
	if !llm.IsKind(err, llm.ErrorProtocol) {
		t.Fatalf("error = %v, want a protocol error", err)
	}
	if got := resp.Message.Text(); got != "The connection died" {
		t.Errorf("text = %q", got)
	}
	if len(rec.OfType(llm.EventResponseEnd)) != 0 {
		t.Error("a cut-off stream must not publish a finished response")
	}
}

func TestStreamRejectsAPartItCannotRepresent(t *testing.T) {
	// Generated audio or an image. We never ask for those modalities, so one
	// arriving is the stream not being what it claims to be — and dropping it
	// would be content the user paid for and never saw.
	stream := `data: {"candidates":[{"content":{"parts":[{"inlineData":{"mimeType":"image/png","data":"iVBORw0K"}}],"role":"model"},"finishReason":"STOP","index":0}]}` + "\n\n"

	rec := &llmtest.Recorder{}
	_, err := decode(context.Background(), strings.NewReader(stream), Config{Provider: "google"}, "m", rec.Emit)
	if !llm.IsKind(err, llm.ErrorProtocol) {
		t.Fatalf("error = %v, want a protocol error", err)
	}
	rec.CheckProviderInvariants(t)
	if len(rec.OfType(llm.EventResponseEnd)) != 0 {
		t.Error("a stream carrying content we cannot represent must not publish a finished response")
	}
}

func TestStreamRejectsArgumentsThatAreNotAnObject(t *testing.T) {
	stream := `data: {"candidates":[{"content":{"parts":[{"functionCall":{"name":"f","args":"not an object"}}],"role":"model"},"finishReason":"STOP","index":0}]}` + "\n\n"

	rec := &llmtest.Recorder{}
	_, err := decode(context.Background(), strings.NewReader(stream), Config{Provider: "google"}, "m", rec.Emit)
	if !llm.IsKind(err, llm.ErrorProtocol) {
		t.Fatalf("error = %v, want a protocol error", err)
	}
	rec.CheckProviderInvariants(t)
}

func TestStreamReportsABlockedPromptRatherThanAnEmptyAnswer(t *testing.T) {
	// The prompt was refused before the model saw it. There is no turn and no
	// finish reason; publishing it as an empty answer would look like a model
	// with nothing to say.
	stream := `data: {"promptFeedback":{"blockReason":"SAFETY","safetyRatings":[]}}` + "\n\n"

	rec := &llmtest.Recorder{}
	_, err := decode(context.Background(), strings.NewReader(stream), Config{Provider: "google"}, "m", rec.Emit)

	var modelErr *llm.Error
	if !errors.As(err, &modelErr) {
		t.Fatalf("error = %v, want an *llm.Error", err)
	}
	if modelErr.Kind != llm.ErrorProvider || modelErr.Code != "SAFETY" {
		t.Errorf("error = %+v", modelErr)
	}
	rec.CheckProviderInvariants(t)
	if len(rec.Events) != 0 {
		t.Errorf("events = %v, want none: nothing was ever generated", rec.Types())
	}
}

func TestStreamErrorFrameBecomesACategorisedError(t *testing.T) {
	tests := []struct {
		name      string
		status    string
		want      llm.ErrorKind
		retryable bool
	}{
		{"quota", "RESOURCE_EXHAUSTED", llm.ErrorRateLimit, true},
		{"bad key", "UNAUTHENTICATED", llm.ErrorAuth, false},
		{"upstream trouble", "UNAVAILABLE", llm.ErrorProvider, true},
		{"anything else", "INVALID_ARGUMENT", llm.ErrorProvider, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			stream := `data: {"error":{"code":429,"message":"upstream said no","status":"` + tc.status + `"}}` + "\n\n"

			rec := &llmtest.Recorder{}
			_, err := decode(context.Background(), strings.NewReader(stream), Config{Provider: "google"}, "m", rec.Emit)

			var modelErr *llm.Error
			if !errors.As(err, &modelErr) {
				t.Fatalf("error = %v, want an *llm.Error", err)
			}
			if modelErr.Kind != tc.want || modelErr.Code != tc.status {
				t.Errorf("error = %+v, want kind %s", modelErr, tc.want)
			}
			if modelErr.Retryable != tc.retryable {
				t.Errorf("retryable = %v, want %v", modelErr.Retryable, tc.retryable)
			}
			rec.CheckProviderInvariants(t)
		})
	}
}

func TestStreamCancelledMidAnswerKeepsPartialContentAndEndsExactlyOnce(t *testing.T) {
	data := fixture(t, "long_text.sse")
	// Serve the first two frames, then block the way a live connection does
	// until the context is cancelled.
	cut := nthIndex(data, "data: ", 3)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	body := &blockingReader{data: data[:cut], ctx: ctx, onBlock: cancel}
	rec := &llmtest.Recorder{}
	resp, err := decode(ctx, body, Config{Provider: "google"}, "requested-model", rec.Emit)
	rec.CheckProviderInvariants(t)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled so a stop is not read as a failure", err)
	}
	if resp.FinishReason != llm.FinishCancelled {
		t.Errorf("finish reason = %q, want cancelled", resp.FinishReason)
	}
	if got := resp.Message.Text(); got != "An aperture is an opening " {
		t.Errorf("text = %q, the tokens already paid for must survive the stop", got)
	}

	ends := rec.OfType(llm.EventResponseEnd)
	if len(ends) != 1 {
		t.Fatalf("response_end count = %d, want exactly one", len(ends))
	}
	if ends[0].Response.FinishReason != llm.FinishCancelled {
		t.Errorf("published finish reason = %q", ends[0].Response.FinishReason)
	}
	if partEnds := rec.OfType(llm.EventPartEnd); len(partEnds) != 1 {
		t.Errorf("part_end count = %d, want the open block closed", len(partEnds))
	}
}

func TestStreamStopsWhenTheRunLayerCannotAcceptEvents(t *testing.T) {
	// emit belongs to the run layer, so its failure is a failed write, not a
	// browser hanging up: the stream stops and the error comes back untouched.
	persistFailed := errors.New("persisting the delta failed")
	rec := &llmtest.Recorder{EmitErr: persistFailed, FailAt: 2}

	resp, err := decode(context.Background(), bytes.NewReader(fixture(t, "text.sse")), Config{Provider: "google"}, "m", rec.Emit)
	if !errors.Is(err, persistFailed) {
		t.Fatalf("error = %v, want the run layer's own error", err)
	}
	rec.CheckProviderInvariants(t)
	if len(rec.OfType(llm.EventResponseEnd)) != 0 {
		t.Error("the turn must not be published as finished after a failed write")
	}
	if resp.Message.Text() == "" {
		t.Error("what was already decoded should still come back")
	}
}

func TestStreamHandlesRealWorldFraming(t *testing.T) {
	t.Run("a frame split across many reads", func(t *testing.T) {
		// One byte at a time is the pathological version of what TCP does to
		// every stream: frames do not arrive whole.
		rec := &llmtest.Recorder{}
		reader := iotest.OneByteReader(bytes.NewReader(fixture(t, "text.sse")))
		resp, err := decode(context.Background(), reader, Config{Provider: "google"}, "m", rec.Emit)
		if err != nil {
			t.Fatalf("decode: %v", err)
		}
		rec.CheckProviderInvariants(t)
		if got := resp.Message.Text(); got != "The aperture is open." {
			t.Errorf("text = %q", got)
		}
	})

	t.Run("CRLF line endings", func(t *testing.T) {
		crlf := bytes.ReplaceAll(fixture(t, "text.sse"), []byte("\n"), []byte("\r\n"))
		rec := &llmtest.Recorder{}
		resp, err := decode(context.Background(), bytes.NewReader(crlf), Config{Provider: "google"}, "m", rec.Emit)
		if err != nil {
			t.Fatalf("decode: %v", err)
		}
		rec.CheckProviderInvariants(t)
		if got := resp.Message.Text(); got != "The aperture is open." {
			t.Errorf("text = %q", got)
		}
	})
}

// nthIndex returns the offset of the nth occurrence of sep.
func nthIndex(data []byte, sep string, n int) int {
	offset := 0
	for range n {
		i := bytes.Index(data[offset:], []byte(sep))
		if i < 0 {
			return len(data)
		}
		offset += i + len(sep)
	}
	return offset - len(sep)
}

// blockingReader serves a prefix and then waits, which is how a real body
// behaves between tokens: net/http unblocks the read when the request context
// is cancelled, so this does the same.
type blockingReader struct {
	data    []byte
	ctx     context.Context
	onBlock func()
	off     int
}

func (r *blockingReader) Read(p []byte) (int, error) {
	if r.off >= len(r.data) {
		if r.onBlock != nil {
			r.onBlock()
			r.onBlock = nil
		}
		<-r.ctx.Done()
		return 0, r.ctx.Err()
	}
	n := copy(p, r.data[r.off:])
	r.off += n
	return n, nil
}

func TestStreamKeepsAThoughtSignatureOnAnswerText(t *testing.T) {
	// Without a function call the signature rides the last part of the reply,
	// and while streaming that part can be an empty text. It belongs to the
	// answer text it follows, so it is kept for the next request.
	// https://ai.google.dev/gemini-api/docs/generate-content/thought-signatures
	rec, resp, err := decodeFixture(t, "text_signature.sse", Config{Provider: "google"})
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got, want := kinds(resp.Message.Parts), []llm.PartKind{llm.PartReasoning, llm.PartText}; !slices.Equal(got, want) {
		t.Fatalf("parts = %v, want %v — the empty signature part must not open a block of its own", got, want)
	}
	text := resp.Message.Parts[1].(llm.TextPart)
	if text.Text != "is 42. Done." {
		t.Errorf("text = %q", text.Text)
	}
	// One attestation per reply: the last one is kept, never glued to another.
	if want := llm.SignatureGemini + "LastSig="; text.Signature != want {
		t.Errorf("signature = %q, want %q", text.Signature, want)
	}
	if strings.Contains(rec.Deltas(llm.PartText), "Sig") {
		t.Error("the signature reached the visible answer")
	}
	// The part_end event is what the run layer stores.
	var ended llm.TextPart
	for _, ev := range rec.OfType(llm.EventPartEnd) {
		if p, ok := ev.Part.(llm.TextPart); ok {
			ended = p
		}
	}
	if ended.Signature != text.Signature {
		t.Errorf("part_end signature = %q, want %q", ended.Signature, text.Signature)
	}
}
