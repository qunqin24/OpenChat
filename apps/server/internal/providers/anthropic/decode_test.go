package anthropic

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

// The fixtures under testdata are real frame shapes, split where this API
// splits them — named events and all. They are the whole point of this file:
// Phase A is judged on parsing, and parsing is only as correct as the samples
// it was checked against.

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
	rec, resp, err := decodeFixture(t, "text.sse", Config{Provider: "anthropic"})
	if err != nil {
		t.Fatalf("decode: %v", err)
	}

	if got := resp.Message.Text(); got != "The aperture is open." {
		t.Errorf("text = %q", got)
	}
	if got, want := kinds(resp.Message.Parts), []llm.PartKind{llm.PartText}; !slices.Equal(got, want) {
		t.Errorf("parts = %v, want %v", got, want)
	}
	if resp.FinishReason != llm.FinishStop || resp.RawFinishReason != "end_turn" {
		t.Errorf("finish = %q / raw %q", resp.FinishReason, resp.RawFinishReason)
	}
	if resp.Model != "claude-sonnet-4-5-20250929" {
		t.Errorf("model = %q, want the one the provider echoed, not the one requested", resp.Model)
	}
	if resp.ResponseID != "msg_text" {
		t.Errorf("response id = %q", resp.ResponseID)
	}

	// A ping is a heartbeat: it must not open a block, end one, or otherwise
	// show up in the stream the run layer sees.
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

func TestStreamKeepsThinkingAndTextInTheOrderTheyArrived(t *testing.T) {
	rec, resp, err := decodeFixture(t, "thinking.sse", Config{Provider: "anthropic"})
	if err != nil {
		t.Fatalf("decode: %v", err)
	}

	want := []llm.PartKind{llm.PartReasoning, llm.PartText, llm.PartReasoning, llm.PartText}
	if got := kinds(resp.Message.Parts); !slices.Equal(got, want) {
		t.Fatalf("parts = %v, want %v — merging the two kinds loses the order they appeared in", got, want)
	}

	if got := resp.Message.Text(); got != "An aperture is an opening. Its size is the f-number." {
		t.Errorf("text = %q, thinking must not leak into the answer", got)
	}
	if got, want := rec.Deltas(llm.PartReasoning), "The user asks about apertures.Should I add the f-number?"; got != want {
		t.Errorf("reasoning deltas = %q, want %q", got, want)
	}

	// The third block opens with its whole thought already on
	// content_block_start and never sends a delta. Content that arrives at the
	// start of a block is still content.
	third, ok := resp.Message.Parts[2].(llm.ReasoningPart)
	if !ok || third.Text != "Should I add the f-number?" {
		t.Errorf("third block = %+v", resp.Message.Parts[2])
	}
}

func TestStreamKeepsAThinkingSignatureOutOfTheVisibleStream(t *testing.T) {
	// Signature is the provider's attestation over the thought. It is opaque,
	// it is what the next turn has to hand back, and a UI that appended it to
	// the reasoning stream would show the user a page of base64 mid-thought.
	rec, resp, err := decodeFixture(t, "signature.sse", Config{Provider: "anthropic"})
	if err != nil {
		t.Fatalf("decode: %v", err)
	}

	reasoning, ok := resp.Message.Parts[0].(llm.ReasoningPart)
	if !ok {
		t.Fatalf("first part is %T", resp.Message.Parts[0])
	}
	if want := "ErUBCkYIBBgCIkDdX0pQbGFzdEhhbGY="; reasoning.Signature != want {
		t.Errorf("signature = %q, want the fragments joined into %q", reasoning.Signature, want)
	}
	if reasoning.Text != "Depth of field follows from the aperture." {
		t.Errorf("thinking text = %q", reasoning.Text)
	}
	if got := rec.Deltas(llm.PartReasoning); strings.Contains(got, "ErUB") {
		t.Errorf("reasoning deltas = %q, the signature reached the visible stream", got)
	}
	if reasoning.Redacted != nil {
		t.Errorf("redacted = %q, want nil when the provider redacted nothing", reasoning.Redacted)
	}
}

func TestStreamKeepsRedactedThinkingWithoutPretendingItIsReadable(t *testing.T) {
	// Encrypted reasoning arrives whole, has no text to show, and still has to
	// survive the round trip or the next turn loses the model's own context.
	rec, resp, err := decodeFixture(t, "redacted_thinking.sse", Config{Provider: "anthropic"})
	if err != nil {
		t.Fatalf("decode: %v", err)
	}

	reasoning, ok := resp.Message.Parts[0].(llm.ReasoningPart)
	if !ok {
		t.Fatalf("first part is %T", resp.Message.Parts[0])
	}
	if want := "EroCCoYBGAIiQL2Hh3cHNbYSZ0pQdmVyeS1zZWNyZXQ="; string(reasoning.Redacted) != want {
		t.Errorf("redacted = %q, want the blob verbatim %q", reasoning.Redacted, want)
	}
	if reasoning.Text != "" {
		t.Errorf("text = %q, there is nothing to display in a redacted thought", reasoning.Text)
	}
	if got := rec.Deltas(llm.PartReasoning); got != "" {
		t.Errorf("reasoning deltas = %q, want none: the blob is not something to render", got)
	}

	// The block is still a block: opened, closed, and in the transcript in the
	// place it happened.
	if got, want := kinds(resp.Message.Parts), []llm.PartKind{llm.PartReasoning, llm.PartText}; !slices.Equal(got, want) {
		t.Errorf("parts = %v, want %v", got, want)
	}
}

func TestStreamReassemblesToolArgumentsSplitAcrossFrames(t *testing.T) {
	rec, resp, err := decodeFixture(t, "tool_call.sse", Config{Provider: "anthropic"})
	if err != nil {
		t.Fatalf("decode: %v", err)
	}

	if got, want := kinds(resp.Message.Parts), []llm.PartKind{llm.PartText, llm.PartToolCall}; !slices.Equal(got, want) {
		t.Fatalf("parts = %v, want %v", got, want)
	}
	call := resp.Message.ToolCalls()[0]

	if call.ID != "toolu_notes_1" || call.Name != "search_notes" {
		t.Errorf("call identity = %q / %q", call.ID, call.Name)
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
	if resp.FinishReason != llm.FinishToolCalls {
		t.Errorf("finish reason = %q", resp.FinishReason)
	}

	// Unlike Chat Completions, this protocol names the call on
	// content_block_start, so part_start can already carry both.
	starts := rec.OfType(llm.EventPartStart)
	toolStart := starts[len(starts)-1]
	if toolStart.ToolCallID != "toolu_notes_1" || toolStart.ToolName != "search_notes" {
		t.Errorf("tool part_start = %+v, want the id and name this protocol sends up front", toolStart)
	}

	// Every argument fragment is display material, and none of them is valid
	// JSON on its own. That is exactly why nothing may parse them early.
	var fragments []string
	for _, ev := range rec.Events {
		if ev.Type == llm.EventPartDelta && ev.PartKind == llm.PartToolCall {
			fragments = append(fragments, ev.Delta)
		}
	}
	if len(fragments) != 3 {
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
	// how a half-written call becomes an execution nobody asked for.
	stream := strings.Join([]string{
		`event: message_start`,
		`data: {"type":"message_start","message":{"id":"msg_x","role":"assistant","model":"claude-sonnet-4-5","usage":{"input_tokens":5}}}`,
		``,
		`event: content_block_start`,
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"toolu_broken","name":"search_notes","input":{}}}`,
		``,
		`event: content_block_delta`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"query\": \"quarterly"}}`,
		``,
		`event: content_block_stop`,
		`data: {"type":"content_block_stop","index":0}`,
		``,
		`event: message_delta`,
		`data: {"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":12}}`,
		``,
		`event: message_stop`,
		`data: {"type":"message_stop"}`,
		``,
	}, "\n")

	rec := &llmtest.Recorder{}
	resp, err := decode(context.Background(), strings.NewReader(stream), Config{Provider: "anthropic"}, "m", rec.Emit)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	rec.CheckProviderInvariants(t)

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
}

func TestStreamCombinesTheTwoHalvesOfUsage(t *testing.T) {
	_, resp, err := decodeFixture(t, "usage_finish.sse", Config{Provider: "anthropic"})
	if err != nil {
		t.Fatalf("decode: %v", err)
	}

	if resp.Usage == nil {
		t.Fatal("usage is nil")
	}
	// input_tokens on this protocol sits *beside* the cache counters rather
	// than containing them, which is the opposite of Chat Completions. Nothing
	// is subtracted here; subtracting would under-report the real prompt.
	want := llm.Usage{InputTokens: 400, OutputTokens: 96, CacheReadTokens: 800, CacheWriteTokens: 120}
	if *resp.Usage != want {
		t.Errorf("usage = %+v, want %+v", *resp.Usage, want)
	}
	// The provisional output count from message_start (2) must not survive the
	// real one from message_delta.
	if resp.Usage.OutputTokens != 96 {
		t.Errorf("output tokens = %d, want message_delta's count", resp.Usage.OutputTokens)
	}
	if resp.FinishReason != llm.FinishLength || resp.RawFinishReason != "max_tokens" {
		t.Errorf("finish = %q / raw %q", resp.FinishReason, resp.RawFinishReason)
	}
}

func TestStreamMapsEveryStopReasonItAccepts(t *testing.T) {
	tests := []struct {
		raw  string
		want llm.FinishReason
	}{
		{"end_turn", llm.FinishStop},
		{"stop_sequence", llm.FinishStop},
		{"max_tokens", llm.FinishLength},
		{"tool_use", llm.FinishToolCalls},
		{"refusal", llm.FinishContentFilter},
		{"model_context_window_exceeded", llm.FinishLength},
		{"pause_turn", llm.FinishLength},
	}

	for _, tc := range tests {
		t.Run(tc.raw, func(t *testing.T) {
			a := &assembler{rawFinish: tc.raw}
			got, err := a.finishReason()
			if err != nil {
				t.Fatalf("finishReason: %v", err)
			}
			if got != tc.want {
				t.Errorf("finish reason = %q, want %q", got, tc.want)
			}
		})
	}

	t.Run("a stop reason nobody has seen before", func(t *testing.T) {
		// The content arrived; a label this code has not learned is not a
		// reason to throw it away.
		a := &assembler{rawFinish: "some_future_reason"}
		got, err := a.finishReason()
		if err != nil || got != llm.FinishStop {
			t.Fatalf("finish reason = %q, %v, want a plain stop", got, err)
		}
	})
}

func TestStreamWithoutAStopReasonIsAProtocolError(t *testing.T) {
	// A stream that simply stops is indistinguishable from one that was cut
	// off, and calling it "stop" is how a truncated answer is shown as whole.
	stream := strings.Join([]string{
		`event: message_start`,
		`data: {"type":"message_start","message":{"id":"msg_x","role":"assistant","model":"claude-sonnet-4-5","usage":{"input_tokens":5}}}`,
		``,
		`event: content_block_start`,
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
		``,
		`event: content_block_delta`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Gateways do this."}}`,
		``,
		`event: message_stop`,
		`data: {"type":"message_stop"}`,
		``,
	}, "\n")

	rec := &llmtest.Recorder{}
	resp, err := decode(context.Background(), strings.NewReader(stream), Config{Provider: "anthropic"}, "m", rec.Emit)
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
	rec, resp, err := decodeFixture(t, "malformed.sse", Config{Provider: "anthropic"})
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
	// The frames after the broken one carry the rest of the sentence and an
	// end_turn. Reading past the hole would have turned a lost clause into a
	// complete answer.
	if strings.Contains(resp.Message.Text(), "the rest of it") {
		t.Error("decoding continued past the malformed frame")
	}
}

func TestStreamCutOffMidFrameIsAnError(t *testing.T) {
	rec, resp, err := decodeFixture(t, "truncated.sse", Config{Provider: "anthropic"})
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

func TestStreamRejectsAStreamThatContradictsItself(t *testing.T) {
	tests := []struct {
		name   string
		frames []string
	}{
		{
			name: "a delta for a block that never started",
			frames: []string{
				`event: content_block_delta`,
				`data: {"type":"content_block_delta","index":4,"delta":{"type":"text_delta","text":"from nowhere"}}`,
			},
		},
		{
			name: "a text delta on a tool block",
			frames: []string{
				`event: content_block_start`,
				`data: {"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"toolu_1","name":"f","input":{}}}`,
				``,
				`event: content_block_delta`,
				`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"not arguments"}}`,
			},
		},
		{
			name: "a block type we never asked for",
			frames: []string{
				`event: content_block_start`,
				`data: {"type":"content_block_start","index":0,"content_block":{"type":"server_tool_use","id":"srvtoolu_1","name":"web_search"}}`,
			},
		},
		{
			name: "a delta type nobody has implemented",
			frames: []string{
				`event: content_block_start`,
				`data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
				``,
				`event: content_block_delta`,
				`data: {"type":"content_block_delta","index":0,"delta":{"type":"citations_delta","citation":{}}}`,
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			stream := strings.Join(append(tc.frames, "", ""), "\n")
			rec := &llmtest.Recorder{}
			_, err := decode(context.Background(), strings.NewReader(stream), Config{Provider: "anthropic"}, "m", rec.Emit)
			if !llm.IsKind(err, llm.ErrorProtocol) {
				t.Fatalf("error = %v, want a protocol error", err)
			}
			rec.CheckProviderInvariants(t)
			if len(rec.OfType(llm.EventResponseEnd)) != 0 {
				t.Error("a contradictory stream must not publish a finished response")
			}
		})
	}
}

func TestStreamIgnoresAnEventTypeItDoesNotKnow(t *testing.T) {
	// This protocol documents new event types as additive and asks clients to
	// ignore the ones they do not know. That promise is about events; an
	// unknown *content block* is content we failed to show, and fails above.
	stream := strings.Join([]string{
		`event: message_start`,
		`data: {"type":"message_start","message":{"id":"msg_x","role":"assistant","model":"claude-sonnet-4-5","usage":{"input_tokens":5}}}`,
		``,
		`event: some_future_event`,
		`data: {"type":"some_future_event","whatever":true}`,
		``,
		`event: content_block_start`,
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":"still fine"}}`,
		``,
		`event: content_block_stop`,
		`data: {"type":"content_block_stop","index":0}`,
		``,
		`event: message_delta`,
		`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":3}}`,
		``,
		`event: message_stop`,
		`data: {"type":"message_stop"}`,
		``,
	}, "\n")

	rec := &llmtest.Recorder{}
	resp, err := decode(context.Background(), strings.NewReader(stream), Config{Provider: "anthropic"}, "m", rec.Emit)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	rec.CheckProviderInvariants(t)
	if resp.Message.Text() != "still fine" {
		t.Errorf("text = %q", resp.Message.Text())
	}
}

func TestStreamDispatchesOnTheEventNameNotThePayload(t *testing.T) {
	// content_block_delta and message_delta both carry a `delta`. A decoder
	// that read the body and guessed from its shape would take a stop reason
	// for a text fragment, or the other way round.
	stream := strings.Join([]string{
		`event: message_start`,
		`data: {"type":"message_start","message":{"id":"msg_x","role":"assistant","model":"claude-sonnet-4-5","usage":{"input_tokens":5}}}`,
		``,
		`event: content_block_start`,
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
		``,
		`event: content_block_delta`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"answer"}}`,
		``,
		`event: content_block_stop`,
		`data: {"type":"content_block_stop","index":0}`,
		``,
		`event: message_delta`,
		`data: {"delta":{"stop_reason":"max_tokens","stop_sequence":null},"usage":{"output_tokens":4}}`,
		``,
		`event: message_stop`,
		`data: {}`,
		``,
	}, "\n")

	rec := &llmtest.Recorder{}
	resp, err := decode(context.Background(), strings.NewReader(stream), Config{Provider: "anthropic"}, "m", rec.Emit)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	rec.CheckProviderInvariants(t)

	// The last two frames carry no `type` at all: the name is the only thing
	// that said what they were.
	if resp.FinishReason != llm.FinishLength {
		t.Errorf("finish reason = %q, want the message_delta's stop reason", resp.FinishReason)
	}
	if resp.Message.Text() != "answer" {
		t.Errorf("text = %q, want only the content_block_delta's fragment", resp.Message.Text())
	}
}

func TestStreamFallsBackToThePayloadTypeWhenTheNameIsMissing(t *testing.T) {
	// Some relays forward the data lines and drop the event ones.
	stream := strings.Join([]string{
		`data: {"type":"message_start","message":{"id":"msg_relay","role":"assistant","model":"claude-sonnet-4-5","usage":{"input_tokens":5}}}`,
		``,
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":"relayed"}}`,
		``,
		`data: {"type":"content_block_stop","index":0}`,
		``,
		`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":2}}`,
		``,
		`data: {"type":"message_stop"}`,
		``,
	}, "\n")

	rec := &llmtest.Recorder{}
	resp, err := decode(context.Background(), strings.NewReader(stream), Config{Provider: "relay"}, "m", rec.Emit)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	rec.CheckProviderInvariants(t)
	if resp.Message.Text() != "relayed" || resp.FinishReason != llm.FinishStop {
		t.Errorf("response = %q / %q", resp.Message.Text(), resp.FinishReason)
	}
}

func TestStreamReadsAFinishedToolInputSentAtBlockStart(t *testing.T) {
	// Anthropic itself always sends `{}` here and streams the arguments. A
	// relay that puts the finished object on content_block_start instead is
	// real, and dropping it would execute a call with no arguments.
	stream := strings.Join([]string{
		`event: message_start`,
		`data: {"type":"message_start","message":{"id":"msg_x","role":"assistant","model":"claude-sonnet-4-5","usage":{"input_tokens":5}}}`,
		``,
		`event: content_block_start`,
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"toolu_1","name":"read_note","input":{"id":"n-17"}}}`,
		``,
		`event: content_block_stop`,
		`data: {"type":"content_block_stop","index":0}`,
		``,
		`event: message_delta`,
		`data: {"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":9}}`,
		``,
		`event: message_stop`,
		`data: {"type":"message_stop"}`,
		``,
	}, "\n")

	rec := &llmtest.Recorder{}
	resp, err := decode(context.Background(), strings.NewReader(stream), Config{Provider: "relay"}, "m", rec.Emit)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	rec.CheckProviderInvariants(t)

	call := resp.Message.ToolCalls()[0]
	if !call.Parsed() || string(call.Arguments) != `{"id":"n-17"}` {
		t.Errorf("arguments = %s, want the object the relay sent up front", call.Arguments)
	}
}

func TestStreamCancelledMidAnswerKeepsPartialContentAndEndsExactlyOnce(t *testing.T) {
	data := fixture(t, "long_text.sse")
	// Serve message_start, content_block_start and the first two deltas, then
	// block the way a live connection does until the context is cancelled.
	cut := nthIndex(data, "event: ", 5)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	body := &blockingReader{data: data[:cut], ctx: ctx, onBlock: cancel}
	rec := &llmtest.Recorder{}
	resp, err := decode(ctx, body, Config{Provider: "anthropic"}, "requested-model", rec.Emit)
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
	// message_start already arrived, so the input half of the usage is known
	// even though the turn never finished.
	if resp.Usage == nil || resp.Usage.InputTokens != 14 {
		t.Errorf("usage = %+v, want the half the provider already reported", resp.Usage)
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

	resp, err := decode(context.Background(), bytes.NewReader(fixture(t, "text.sse")), Config{Provider: "anthropic"}, "m", rec.Emit)
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

func TestStreamErrorEventBecomesACategorisedError(t *testing.T) {
	tests := []struct {
		name      string
		errorType string
		want      llm.ErrorKind
		retryable bool
	}{
		{"overloaded", "overloaded_error", llm.ErrorProvider, true},
		{"rate limited", "rate_limit_error", llm.ErrorRateLimit, true},
		{"bad key", "authentication_error", llm.ErrorAuth, false},
		{"anything else", "invalid_request_error", llm.ErrorProvider, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			stream := "event: error\ndata: {\"type\":\"error\",\"error\":{\"type\":\"" + tc.errorType + "\",\"message\":\"upstream said no\"}}\n\n"

			rec := &llmtest.Recorder{}
			_, err := decode(context.Background(), strings.NewReader(stream), Config{Provider: "anthropic"}, "m", rec.Emit)

			var modelErr *llm.Error
			if !errors.As(err, &modelErr) {
				t.Fatalf("error = %v, want an *llm.Error", err)
			}
			if modelErr.Kind != tc.want || modelErr.Code != tc.errorType {
				t.Errorf("error = %+v, want kind %s", modelErr, tc.want)
			}
			if modelErr.Retryable != tc.retryable {
				t.Errorf("retryable = %v, want %v", modelErr.Retryable, tc.retryable)
			}
			rec.CheckProviderInvariants(t)
		})
	}
}

func TestStreamHandlesRealWorldFraming(t *testing.T) {
	t.Run("a frame split across many reads", func(t *testing.T) {
		// One byte at a time is the pathological version of what TCP does to
		// every stream: frames do not arrive whole.
		rec := &llmtest.Recorder{}
		reader := iotest.OneByteReader(bytes.NewReader(fixture(t, "text.sse")))
		resp, err := decode(context.Background(), reader, Config{Provider: "anthropic"}, "m", rec.Emit)
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
		resp, err := decode(context.Background(), bytes.NewReader(crlf), Config{Provider: "anthropic"}, "m", rec.Emit)
		if err != nil {
			t.Fatalf("decode: %v", err)
		}
		rec.CheckProviderInvariants(t)
		if got := resp.Message.Text(); got != "The aperture is open." {
			t.Errorf("text = %q", got)
		}
	})
}

func TestStreamRejectsUnboundedToolArguments(t *testing.T) {
	// A model looping on JSON is a real failure mode, and noticing it late
	// costs the whole process.
	var stream strings.Builder
	stream.WriteString("event: content_block_start\n")
	stream.WriteString(`data: {"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"toolu_x","name":"f","input":{}}}`)
	stream.WriteString("\n\n")
	for range 4 {
		stream.WriteString("event: content_block_delta\n")
		stream.WriteString(`data: {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"aaaaaaaaaa"}}`)
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

func TestStreamKeepsASignatureOnlyThinkingBlock(t *testing.T) {
	// The current models omit the thinking text: the block opens empty, gets
	// only a signature_delta and closes. It has no text to show, but it is
	// still the model's thought and must survive to be replayed.
	stream := strings.Join([]string{
		`event: message_start`,
		`data: {"type":"message_start","message":{"id":"msg_x","role":"assistant","model":"claude-opus-5-5","usage":{"input_tokens":5}}}`,
		``,
		`event: content_block_start`,
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":"","signature":""}}`,
		``,
		`event: content_block_delta`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"EqQBsig"}}`,
		``,
		`event: content_block_stop`,
		`data: {"type":"content_block_stop","index":0}`,
		``,
		`event: content_block_start`,
		`data: {"type":"content_block_start","index":1,"content_block":{"type":"text","text":""}}`,
		``,
		`event: content_block_delta`,
		`data: {"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"hi"}}`,
		``,
		`event: content_block_stop`,
		`data: {"type":"content_block_stop","index":1}`,
		``,
		`event: message_delta`,
		`data: {"type":"message_delta","delta":{"stop_reason":"model_context_window_exceeded"},"usage":{"output_tokens":4}}`,
		``,
		`event: message_stop`,
		`data: {"type":"message_stop"}`,
		``,
	}, "\n")

	rec := &llmtest.Recorder{}
	resp, err := decode(context.Background(), strings.NewReader(stream), Config{Provider: "anthropic"}, "m", rec.Emit)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	rec.CheckProviderInvariants(t)

	reasoning, ok := resp.Message.Parts[0].(llm.ReasoningPart)
	if !ok || reasoning.Text != "" || reasoning.Signature != "EqQBsig" {
		t.Fatalf("first part = %#v, want an empty reasoning part carrying its signature", resp.Message.Parts[0])
	}
	if resp.FinishReason != llm.FinishLength || resp.RawFinishReason != "model_context_window_exceeded" {
		t.Errorf("finish = %q / raw %q, want length with the raw reason kept", resp.FinishReason, resp.RawFinishReason)
	}
}
