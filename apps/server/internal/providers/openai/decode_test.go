package openai

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

// The fixtures under testdata are real frame shapes, split where real endpoints
// split them. They are the whole point of this file: Phase A is judged on
// parsing, and parsing is only as correct as the samples it was checked against.

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
	return decodeReader(t, bytes.NewReader(fixture(t, name)), cfg)
}

func decodeReader(t *testing.T, r *bytes.Reader, cfg Config) (*llmtest.Recorder, llm.ModelResponse, error) {
	t.Helper()
	rec := &llmtest.Recorder{}
	resp, err := decode(context.Background(), r, cfg, "requested-model", rec.Emit)
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
	rec, resp, err := decodeFixture(t, "text.sse", Config{Provider: "openai"})
	if err != nil {
		t.Fatalf("decode: %v", err)
	}

	if got := resp.Message.Text(); got != "The aperture is open." {
		t.Errorf("text = %q", got)
	}
	if got, want := kinds(resp.Message.Parts), []llm.PartKind{llm.PartText}; !slices.Equal(got, want) {
		t.Errorf("parts = %v, want %v — the empty role-only delta must not open a block", got, want)
	}
	if resp.FinishReason != llm.FinishStop {
		t.Errorf("finish reason = %q", resp.FinishReason)
	}
	if resp.Model != "gpt-4o-mini" {
		t.Errorf("model = %q, want the one the provider echoed, not the one requested", resp.Model)
	}
	if resp.ResponseID != "chatcmpl-text" {
		t.Errorf("response id = %q", resp.ResponseID)
	}
	if resp.Usage != nil {
		t.Errorf("usage = %+v, want nil when the provider reported none", resp.Usage)
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

func TestStreamKeepsReasoningAndTextInTheOrderTheyArrived(t *testing.T) {
	rec, resp, err := decodeFixture(t, "reasoning.sse", Config{Provider: "deepseek"})
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
	if first, ok := resp.Message.Parts[0].(llm.ReasoningPart); !ok || first.Text != "The user asks about apertures." {
		t.Errorf("first reasoning block = %+v", resp.Message.Parts[0])
	}
}

func TestStreamReassemblesToolArgumentsSplitAcrossFrames(t *testing.T) {
	rec, resp, err := decodeFixture(t, "tool_call_split.sse", Config{Provider: "openai"})
	if err != nil {
		t.Fatalf("decode: %v", err)
	}

	if got, want := kinds(resp.Message.Parts), []llm.PartKind{llm.PartText, llm.PartToolCall}; !slices.Equal(got, want) {
		t.Fatalf("parts = %v, want %v", got, want)
	}
	call, ok := resp.Message.Parts[1].(llm.ToolCallPart)
	if !ok {
		t.Fatalf("second part is %T", resp.Message.Parts[1])
	}

	if call.Name != "search_notes" {
		t.Errorf("name = %q", call.Name)
	}
	if call.ID != "call_notes_1" {
		t.Errorf("id = %q — the id arrives on a later fragment and must still land on the call", call.ID)
	}
	if want := `{"query": "quarterly report", "limit": 5}`; call.RawArguments != want {
		t.Errorf("raw arguments = %q, want %q", call.RawArguments, want)
	}
	if !call.Parsed() {
		t.Fatal("complete arguments did not parse")
	}

	var args struct {
		Query string `json:"query"`
		Limit int    `json:"limit"`
	}
	if err := json.Unmarshal(call.Arguments, &args); err != nil {
		t.Fatalf("unmarshal arguments: %v", err)
	}
	if args.Query != "quarterly report" || args.Limit != 5 {
		t.Errorf("arguments = %+v", args)
	}

	// The id is authoritative only at the end: part_start goes out before the
	// provider has sent one, and consumers must not key on it there.
	starts := rec.OfType(llm.EventPartStart)
	toolStart := starts[len(starts)-1]
	if toolStart.PartKind != llm.PartToolCall || toolStart.ToolCallID != "" {
		t.Errorf("tool part_start = %+v, want an empty id", toolStart)
	}

	// Every argument fragment is display material, and none of them is valid
	// JSON on its own. That is exactly why nothing may parse them early.
	var fragments []string
	for _, ev := range rec.Events {
		if ev.Type == llm.EventPartDelta && ev.PartKind == llm.PartToolCall {
			fragments = append(fragments, ev.Delta)
		}
	}
	if len(fragments) < 3 {
		t.Fatalf("tool argument deltas = %d, want one per fixture fragment", len(fragments))
	}
	for _, fragment := range fragments {
		if json.Valid([]byte(fragment)) {
			t.Errorf("fragment %q parses on its own; the fixture no longer covers partial arguments", fragment)
		}
	}
	if got := strings.Join(fragments, ""); got != call.RawArguments {
		t.Errorf("fragments joined = %q, want the accumulated arguments %q", got, call.RawArguments)
	}
}

func TestStreamNeverInventsArgumentsForAnIncompleteToolCall(t *testing.T) {
	// The model stopped mid-object. Best-effort repair — or an empty `{}` — is
	// how a half-written call becomes an execution nobody asked for, so the
	// fragment comes back unparsed and the runner decides what to tell the
	// model. The stream itself is fine: this is the model misbehaving, not the
	// wire, and failing the turn here would hide that.
	_, resp, err := decodeFixture(t, "tool_call_broken_args.sse", Config{Provider: "openai"})
	if err != nil {
		t.Fatalf("decode: %v", err)
	}

	call := resp.Message.ToolCalls()[0]
	if call.Parsed() {
		t.Fatalf("incomplete arguments were accepted as complete: %s", call.Arguments)
	}
	if call.Arguments != nil {
		t.Errorf("arguments = %s, want nil", call.Arguments)
	}
	if want := `{"query": "quarterly`; call.RawArguments != want {
		t.Errorf("raw arguments = %q, want %q so the model can be told what was wrong", call.RawArguments, want)
	}
	if call.ID != "call_broken" || call.Name != "search_notes" {
		t.Errorf("call identity = %q / %q", call.ID, call.Name)
	}
	if resp.FinishReason != llm.FinishToolCalls {
		t.Errorf("finish reason = %q", resp.FinishReason)
	}
}

func TestStreamKeepsTwoToolCallsApart(t *testing.T) {
	_, resp, err := decodeFixture(t, "two_tool_calls.sse", Config{Provider: "openai"})
	if err != nil {
		t.Fatalf("decode: %v", err)
	}

	calls := resp.Message.ToolCalls()
	if len(calls) != 2 {
		t.Fatalf("tool calls = %d, want 2", len(calls))
	}
	// The second frame sends index 1 before index 0. Fragments follow the wire
	// index, never the order they happen to arrive in.
	if calls[0].ID != "call_a" || calls[0].RawArguments != `{"id": "n-17"}` {
		t.Errorf("first call = %+v", calls[0])
	}
	if calls[1].ID != "call_b" || calls[1].RawArguments != `{"query": "budget"}` {
		t.Errorf("second call = %+v", calls[1])
	}
	if resp.FinishReason != llm.FinishToolCalls {
		t.Errorf("finish reason = %q, want tool_calls", resp.FinishReason)
	}
}

func TestStreamSeparatesCachedTokensFromInput(t *testing.T) {
	_, resp, err := decodeFixture(t, "usage_finish.sse", Config{Provider: "openai"})
	if err != nil {
		t.Fatalf("decode: %v", err)
	}

	if resp.Usage == nil {
		t.Fatal("usage is nil")
	}
	// prompt_tokens is 1200 with 800 of them cached. Reporting 1200 as input
	// and 800 as cache would count the cached half twice.
	want := llm.Usage{InputTokens: 400, OutputTokens: 96, ReasoningTokens: 64, CacheReadTokens: 800}
	if *resp.Usage != want {
		t.Errorf("usage = %+v, want %+v", *resp.Usage, want)
	}
	if resp.FinishReason != llm.FinishLength || resp.RawFinishReason != "length" {
		t.Errorf("finish = %q / raw %q", resp.FinishReason, resp.RawFinishReason)
	}
	if resp.Model != "gpt-4o-mini-2024-07-18" {
		t.Errorf("model = %q, want the resolved one the provider served", resp.Model)
	}
}

func TestStreamWithoutAFinishReasonIsAProtocolErrorUnlessCompatAllowsIt(t *testing.T) {
	t.Run("rejected by default", func(t *testing.T) {
		rec, resp, err := decodeFixture(t, "no_finish_reason.sse", Config{Provider: "local"})
		if !llm.IsKind(err, llm.ErrorProtocol) {
			t.Fatalf("error = %v, want a protocol error", err)
		}
		if resp.FinishReason != "" {
			t.Errorf("finish reason = %q, want empty so nothing reads it as complete", resp.FinishReason)
		}
		if len(rec.OfType(llm.EventResponseEnd)) != 0 {
			t.Error("a stream that never said why it stopped must not publish a finished response")
		}
	})

	t.Run("inferred when the endpoint is known to omit it", func(t *testing.T) {
		cfg := Config{Provider: "local", Compat: Compat{InferFinishReason: true}}
		_, resp, err := decodeFixture(t, "no_finish_reason.sse", cfg)
		if err != nil {
			t.Fatalf("decode: %v", err)
		}
		if resp.FinishReason != llm.FinishStop {
			t.Errorf("finish reason = %q, want stop", resp.FinishReason)
		}
		if resp.RawFinishReason != "" {
			t.Errorf("raw finish reason = %q, want empty: we inferred it, the provider never said it", resp.RawFinishReason)
		}
	})
}

func TestStreamMalformedFrameIsAnErrorNotASilentTruncation(t *testing.T) {
	rec, resp, err := decodeFixture(t, "malformed.sse", Config{Provider: "openai"})
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
	// The frame after the broken one carries finish_reason "stop". Reading past
	// the hole would have turned a lost sentence into a complete answer.
	if strings.Contains(resp.Message.Text(), "stop") {
		t.Error("decoding continued past the malformed frame")
	}
}

func TestStreamCutOffMidFrameIsAnError(t *testing.T) {
	rec, resp, err := decodeFixture(t, "truncated.sse", Config{Provider: "openai"})
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

func TestStreamCancelledMidAnswerKeepsPartialContentAndEndsExactlyOnce(t *testing.T) {
	data := fixture(t, "long_text.sse")
	// Serve the first two frames, then block the way a live connection does
	// until the context is cancelled.
	cut := nthIndex(data, "data: ", 3)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	body := &blockingReader{data: data[:cut], ctx: ctx, onBlock: cancel}
	rec := &llmtest.Recorder{}
	resp, err := decode(ctx, body, Config{Provider: "openai"}, "requested-model", rec.Emit)
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
	rec := &llmtest.Recorder{EmitErr: persistFailed, FailAt: 3}

	resp, err := decode(context.Background(), bytes.NewReader(fixture(t, "text.sse")), Config{Provider: "openai"}, "m", rec.Emit)
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
	t.Run("CRLF line endings and heartbeats", func(t *testing.T) {
		_, resp, err := decodeFixture(t, "crlf.sse", Config{Provider: "openai"})
		if err != nil {
			t.Fatalf("decode: %v", err)
		}
		if got := resp.Message.Text(); got != "Windows line endings are still SSE." {
			t.Errorf("text = %q", got)
		}
	})

	t.Run("a frame split across many reads", func(t *testing.T) {
		// One byte at a time is the pathological version of what TCP does to
		// every stream: frames do not arrive whole.
		rec := &llmtest.Recorder{}
		reader := iotest.OneByteReader(bytes.NewReader(fixture(t, "text.sse")))
		resp, err := decode(context.Background(), reader, Config{Provider: "openai"}, "m", rec.Emit)
		if err != nil {
			t.Fatalf("decode: %v", err)
		}
		rec.CheckProviderInvariants(t)
		if got := resp.Message.Text(); got != "The aperture is open." {
			t.Errorf("text = %q", got)
		}
	})
}

func TestStreamErrorFrameBecomesAProviderError(t *testing.T) {
	// Several gateways report a mid-stream failure inside a data frame with a
	// 200 status, which is indistinguishable from success unless it is read.
	stream := "data: {\"error\":{\"message\":\"upstream model overloaded\",\"code\":\"server_overloaded\"}}\n\n"

	rec := &llmtest.Recorder{}
	_, err := decode(context.Background(), strings.NewReader(stream), Config{Provider: "gateway"}, "m", rec.Emit)

	var modelErr *llm.Error
	if !errors.As(err, &modelErr) {
		t.Fatalf("error = %v, want an *llm.Error", err)
	}
	if modelErr.Kind != llm.ErrorProvider || modelErr.Code != "server_overloaded" {
		t.Errorf("error = %+v", modelErr)
	}
	rec.CheckProviderInvariants(t)
}

func TestStreamRejectsUnboundedToolArguments(t *testing.T) {
	// A model looping on JSON is a real failure mode, and noticing it late
	// costs the whole process.
	var stream strings.Builder
	for range 4 {
		stream.WriteString(`data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_x","function":{"name":"f","arguments":"aaaaaaaaaa"}}]}}]}`)
		stream.WriteString("\n\n")
	}

	rec := &llmtest.Recorder{}
	_, err := decode(context.Background(), strings.NewReader(stream.String()), Config{MaxToolArgumentBytes: 25}, "m", rec.Emit)
	if !llm.IsKind(err, llm.ErrorProtocol) {
		t.Fatalf("error = %v, want a protocol error", err)
	}
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

func TestStreamErrorFrameAcceptsANumericCode(t *testing.T) {
	// OpenRouter reports the HTTP status as a number inside the frame. A code
	// that cannot be decoded used to take the whole frame, message included.
	stream := "data: {\"error\":{\"message\":\"Provider returned error\",\"code\":429}}\n\n"

	rec := &llmtest.Recorder{}
	_, err := decode(context.Background(), strings.NewReader(stream), Config{Provider: "openrouter"}, "m", rec.Emit)

	var modelErr *llm.Error
	if !errors.As(err, &modelErr) {
		t.Fatalf("error = %v, want an *llm.Error", err)
	}
	if modelErr.Message != "Provider returned error" || modelErr.Code != "429" {
		t.Errorf("error = %+v", modelErr)
	}
	if modelErr.Kind != llm.ErrorRateLimit || !modelErr.Retryable {
		t.Errorf("kind = %s retryable = %v, want a retryable rate limit", modelErr.Kind, modelErr.Retryable)
	}
}

func TestWireErrorCodeIsReadWhicheverWayItIsSent(t *testing.T) {
	for in, want := range map[string]wireCode{
		`{"code":"rate_limit_exceeded"}`: "rate_limit_exceeded",
		`{"code":429}`:                   "429",
		`{"code":null}`:                  "",
		`{"message":"no code"}`:          "",
		`{"code":{"odd":true}}`:          "",
	} {
		var e wireError
		if err := json.Unmarshal([]byte(in), &e); err != nil || e.Code != want {
			t.Errorf("%s: code = %q err = %v, want %q", in, e.Code, err, want)
		}
	}
}
