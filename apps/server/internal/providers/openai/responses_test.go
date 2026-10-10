package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/qunqin/openchat/apps/server/internal/llm"
	"github.com/qunqin/openchat/apps/server/internal/llm/llmtest"
)

// --- request mapping ---------------------------------------------------------

func buildResponses(t *testing.T, req llm.Request) map[string]any {
	t.Helper()
	return buildResponsesWith(t, req, Config{Reasoning: true})
}

func buildResponsesWith(t *testing.T, req llm.Request, cfg Config) map[string]any {
	t.Helper()
	body, err := buildResponsesBody(req, cfg)
	if err != nil {
		t.Fatalf("buildResponsesBody: %v", err)
	}
	var sent map[string]any
	if err := json.Unmarshal(body, &sent); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	return sent
}

// canon re-encodes v so that a want written as a JSON literal compares equal to
// a body that went through json.Unmarshal.
func canon(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func jsonEq(t *testing.T, name string, got any, want string) {
	t.Helper()
	var w any
	if err := json.Unmarshal([]byte(want), &w); err != nil {
		t.Fatalf("%s: bad want literal: %v", name, err)
	}
	if canon(t, got) != canon(t, w) {
		t.Errorf("%s =\n  %s\nwant\n  %s", name, canon(t, got), canon(t, w))
	}
}

func TestResponsesBodyEnvelope(t *testing.T) {
	temp, topP, max := 0.2, 0.9, 512
	sent := buildResponses(t, llm.Request{
		Model: "gpt-5",
		Messages: []llm.Message{
			llm.TextMessage(llm.RoleSystem, "be brief"),
			llm.TextMessage(llm.RoleSystem, "answer in English"),
			llm.TextMessage(llm.RoleUser, "hi"),
		},
		Tools: []llm.Tool{
			{Name: "search", Description: "search notes", Parameters: json.RawMessage(`{"type":"object","properties":{"q":{"type":"string"}}}`)},
			{Name: "ping"},
		},
		Options: llm.Options{
			Temperature: &temp, TopP: &topP, MaxTokens: &max,
			Stop:       []string{"END"},
			ToolChoice: llm.ToolChoiceRequired,
			Reasoning:  &llm.ReasoningOptions{Effort: llm.EffortHigh},
		},
	})

	if sent["model"] != "gpt-5" || sent["stream"] != true || sent["store"] != false {
		t.Errorf("envelope = %v", sent)
	}
	jsonEq(t, "include", sent["include"], `["reasoning.encrypted_content"]`)
	if sent["instructions"] != "be brief\n\nanswer in English" {
		t.Errorf("instructions = %q", sent["instructions"])
	}
	jsonEq(t, "input", sent["input"], `[{"type":"message","role":"user","content":[{"type":"input_text","text":"hi"}]}]`)
	jsonEq(t, "tools", sent["tools"], `[
		{"type":"function","name":"search","description":"search notes","strict":false,
		 "parameters":{"type":"object","properties":{"q":{"type":"string"}}}},
		{"type":"function","name":"ping","description":"","strict":false,
		 "parameters":{"type":"object","properties":{}}}]`)
	if sent["tool_choice"] != "required" {
		t.Errorf("tool_choice = %v", sent["tool_choice"])
	}
	if sent["max_output_tokens"] != float64(512) || sent["temperature"] != 0.2 || sent["top_p"] != 0.9 {
		t.Errorf("sampling = %v", sent)
	}
	jsonEq(t, "reasoning", sent["reasoning"], `{"effort":"high","summary":"auto"}`)
	for _, key := range []string{"max_tokens", "stop", "messages", "stream_options"} {
		if _, ok := sent[key]; ok {
			t.Errorf("%s leaked in from the Chat Completions shape", key)
		}
	}
}

func TestResponsesBodyOmitsWhatWasNotAsked(t *testing.T) {
	sent := buildResponsesWith(t, llm.Request{Model: "m", Messages: []llm.Message{llm.TextMessage(llm.RoleUser, "hi")}}, Config{})
	for _, key := range []string{"instructions", "tools", "tool_choice", "reasoning", "include", "max_output_tokens", "temperature", "top_p"} {
		if _, ok := sent[key]; ok {
			t.Errorf("%s was sent without being asked for", key)
		}
	}
}

func TestResponsesBodyExtraCannotOverwriteReservedFields(t *testing.T) {
	for _, key := range []string{"model", "input", "instructions", "stream", "tools", "tool_choice", "store", "include"} {
		_, err := buildResponsesBody(llm.Request{
			Model:    "m",
			Messages: []llm.Message{llm.TextMessage(llm.RoleUser, "hi")},
			Options:  llm.Options{Extra: map[string]any{key: "x"}},
		}, Config{})
		if err == nil {
			t.Errorf("Extra[%q] was allowed to overwrite a request field", key)
		}
	}
	sent := buildResponses(t, llm.Request{
		Model:    "m",
		Messages: []llm.Message{llm.TextMessage(llm.RoleUser, "hi")},
		Options:  llm.Options{Extra: map[string]any{"service_tier": "flex"}},
	})
	if sent["service_tier"] != "flex" {
		t.Errorf("a vendor knob was dropped: %v", sent)
	}
}

func TestResponsesHistoryRoundTripsToolUseAndReasoning(t *testing.T) {
	blob := []byte("gAAAA_blob")
	history := []llm.Message{
		llm.TextMessage(llm.RoleUser, "find apertures"),
		{Role: llm.RoleAssistant, Parts: []llm.Part{
			// Summary text with no blob is foreign to the endpoint and is dropped.
			llm.ReasoningPart{Text: "summary only"},
			// A blob from another model, or another protocol, is dropped too.
			llm.ReasoningPart{Redacted: []byte("anthropic-blob")},
			llm.ReasoningPart{Text: "other model", Redacted: []byte("old"), Signature: responsesSignaturePrefix + "grok-4"},
			llm.ReasoningPart{Text: "shown to the user", Redacted: blob, Signature: responsesSignaturePrefix + "gpt-5"},
			llm.TextPart{Text: "searching"},
			llm.ToolCallPart{ID: "call_1", Name: "search", Arguments: json.RawMessage(`{"q":"x"}`), RawArguments: `{"q":"x"}`},
			llm.ToolCallPart{ID: "call_2", Name: "broken", RawArguments: `{"q":`},
			llm.ToolCallPart{ID: "call_3", Name: "noargs"},
		}},
		{Role: llm.RoleTool, Parts: []llm.Part{
			llm.ToolResultPart{CallID: "call_1", Name: "search", Content: "three notes", Status: llm.ToolSucceeded},
			llm.ToolResultPart{CallID: "call_2", Name: "broken", Status: llm.ToolFailed, Error: "bad arguments"},
		}},
		llm.TextMessage(llm.RoleAssistant, "done"),
	}
	sent := buildResponses(t, llm.Request{Model: "gpt-5", Messages: history})

	jsonEq(t, "input", sent["input"], `[
		{"type":"message","role":"user","content":[{"type":"input_text","text":"find apertures"}]},
		{"type":"reasoning","summary":[],"encrypted_content":"gAAAA_blob"},
		{"type":"message","role":"assistant","content":[{"type":"output_text","text":"searching"}]},
		{"type":"function_call","call_id":"call_1","name":"search","arguments":"{\"q\":\"x\"}"},
		{"type":"function_call","call_id":"call_2","name":"broken","arguments":"{\"q\":"},
		{"type":"function_call","call_id":"call_3","name":"noargs","arguments":"{}"},
		{"type":"function_call_output","call_id":"call_1","output":"three notes"},
		{"type":"function_call_output","call_id":"call_2","output":"tool failed: bad arguments"},
		{"type":"message","role":"assistant","content":[{"type":"output_text","text":"done"}]}]`)
}

func TestResponsesAttachments(t *testing.T) {
	png := []byte{0x89, 'P', 'N', 'G'}
	pdf := []byte("%PDF-1.7")
	sent := buildResponses(t, llm.Request{Model: "m", Messages: []llm.Message{{
		Role: llm.RoleUser,
		Parts: []llm.Part{
			llm.TextPart{Text: "look at "},
			llm.TextPart{Text: "these"},
			llm.AttachmentPart{Ref: "a1", MediaType: "image/png", Data: png},
			llm.AttachmentPart{Ref: "a2", MediaType: "image/jpeg", URL: "https://cdn.test/x.jpg"},
			llm.AttachmentPart{Ref: "a3", MediaType: "application/pdf", Filename: "spec.pdf", Data: pdf},
			llm.AttachmentPart{Ref: "a4", MediaType: "application/pdf", Data: pdf},
		},
	}}})

	jsonEq(t, "input", sent["input"], `[{"type":"message","role":"user","content":[
		{"type":"input_text","text":"look at these"},
		{"type":"input_image","image_url":"data:image/png;base64,iVBORw=="},
		{"type":"input_image","image_url":"https://cdn.test/x.jpg"},
		{"type":"input_file","filename":"spec.pdf","file_data":"data:application/pdf;base64,JVBERi0xLjc="},
		{"type":"input_file","filename":"document.pdf","file_data":"data:application/pdf;base64,JVBERi0xLjc="}]}]`)
}

func TestResponsesRejectsWhatItCannotSend(t *testing.T) {
	user := func(parts ...llm.Part) llm.Request {
		return llm.Request{Model: "m", Messages: []llm.Message{{Role: llm.RoleUser, Parts: parts}}}
	}
	tests := map[string]llm.Request{
		"no model":                  {Messages: []llm.Message{llm.TextMessage(llm.RoleUser, "hi")}},
		"unresolved attachment":     user(llm.AttachmentPart{Ref: "a", MediaType: "image/png"}),
		"unsupported media type":    user(llm.AttachmentPart{MediaType: "audio/mpeg", Data: []byte{1}}),
		"linked pdf":                user(llm.AttachmentPart{MediaType: "application/pdf", URL: "https://x"}),
		"tool call from the user":   user(llm.ToolCallPart{ID: "c", Name: "n"}),
		"tool call without an id":   {Model: "m", Messages: []llm.Message{{Role: llm.RoleAssistant, Parts: []llm.Part{llm.ToolCallPart{Name: "n"}}}}},
		"result without a call id":  {Model: "m", Messages: []llm.Message{{Role: llm.RoleTool, Parts: []llm.Part{llm.ToolResultPart{Name: "n"}}}}},
		"empty assistant turn":      {Model: "m", Messages: []llm.Message{{Role: llm.RoleAssistant, Parts: []llm.Part{llm.ReasoningPart{Text: "only thoughts"}}}}},
		"unknown role":              {Model: "m", Messages: []llm.Message{{Role: "wizard"}}},
		"attachment in system turn": {Model: "m", Messages: []llm.Message{{Role: llm.RoleSystem, Parts: []llm.Part{llm.AttachmentPart{}}}}},
	}
	for name, req := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := buildResponsesBody(req, Config{}); !llm.IsKind(err, llm.ErrorProvider) {
				t.Errorf("error = %v, want a provider error", err)
			}
		})
	}
}

// --- stream decoding ---------------------------------------------------------

func decodeResponsesFixture(t *testing.T, name string) (*llmtest.Recorder, llm.ModelResponse, error) {
	t.Helper()
	return decodeResponsesReader(t, bytes.NewReader(fixture(t, name)))
}

func decodeResponsesReader(t *testing.T, r io.Reader) (*llmtest.Recorder, llm.ModelResponse, error) {
	t.Helper()
	rec := &llmtest.Recorder{}
	resp, err := decodeResponses(context.Background(), r, Config{Provider: "opencode-go"}, "gpt-5", rec.Emit)
	rec.CheckProviderInvariants(t)
	return rec, resp, err
}

func TestResponsesStreamAssemblesPlainText(t *testing.T) {
	rec, resp, err := decodeResponsesFixture(t, "responses_text.sse")
	if err != nil {
		t.Fatalf("decode: %v", err)
	}

	if resp.Message.Text() != "The aperture is open." {
		t.Errorf("text = %q", resp.Message.Text())
	}
	if got, want := kinds(resp.Message.Parts), []llm.PartKind{llm.PartText}; !slices.Equal(got, want) {
		t.Errorf("parts = %v, want %v", got, want)
	}
	if resp.FinishReason != llm.FinishStop || resp.RawFinishReason != "completed" {
		t.Errorf("finish = %q / %q", resp.FinishReason, resp.RawFinishReason)
	}
	if resp.Model != "gpt-5-2025-08-07" || resp.ResponseID != "resp_text" || resp.Provider != "opencode-go" {
		t.Errorf("identity = %q %q %q", resp.Model, resp.ResponseID, resp.Provider)
	}
	if want := (&llm.Usage{InputTokens: 14, OutputTokens: 6}); resp.Usage == nil || *resp.Usage != *want {
		t.Errorf("usage = %+v, want %+v", resp.Usage, want)
	}
	want := []llm.EventType{
		llm.EventResponseStart,
		llm.EventPartStart, llm.EventPartDelta, llm.EventPartDelta, llm.EventPartEnd,
		llm.EventResponseEnd,
	}
	if got := rec.Types(); !slices.Equal(got, want) {
		t.Errorf("events = %v, want %v", got, want)
	}
	if got := rec.Deltas(llm.PartText); got != "The aperture is open." {
		t.Errorf("deltas = %q", got)
	}
}

func TestResponsesStreamKeepsReasoningSummaryAndEncryptedContent(t *testing.T) {
	rec, resp, err := decodeResponsesFixture(t, "responses_reasoning.sse")
	if err != nil {
		t.Fatalf("decode: %v", err)
	}

	if got, want := kinds(resp.Message.Parts), []llm.PartKind{llm.PartReasoning, llm.PartText}; !slices.Equal(got, want) {
		t.Fatalf("parts = %v, want %v", got, want)
	}
	reasoning := resp.Message.Parts[0].(llm.ReasoningPart)
	if want := "**Weighing** the question\n\nChecking the f-number."; reasoning.Text != want {
		t.Errorf("summary = %q, want %q — sections stay apart", reasoning.Text, want)
	}
	if string(reasoning.Redacted) != "gAAAAABo_encrypted_reasoning_blob_one==" {
		t.Errorf("encrypted content = %q", reasoning.Redacted)
	}
	if reasoning.Signature != "openai-responses:gpt-5" {
		t.Errorf("signature = %q", reasoning.Signature)
	}
	if resp.Message.Text() != "An aperture is an opening." {
		t.Errorf("reasoning leaked into the answer: %q", resp.Message.Text())
	}
	if got := rec.Deltas(llm.PartReasoning); got != reasoning.Text {
		t.Errorf("streamed reasoning = %q, want what the part holds", got)
	}
	// 30 in of which 10 were cached; 120 out of which 96 were reasoning.
	if want := (&llm.Usage{InputTokens: 20, OutputTokens: 120, ReasoningTokens: 96, CacheReadTokens: 10}); resp.Usage == nil || *resp.Usage != *want {
		t.Errorf("usage = %+v, want %+v", resp.Usage, want)
	}

	// The part that came out is what the next request replays.
	next := buildResponses(t, llm.Request{Model: "gpt-5", Messages: []llm.Message{
		llm.TextMessage(llm.RoleUser, "again"),
		resp.Message,
	}})
	jsonEq(t, "replayed input", next["input"], `[
		{"type":"message","role":"user","content":[{"type":"input_text","text":"again"}]},
		{"type":"reasoning","summary":[],"encrypted_content":"gAAAAABo_encrypted_reasoning_blob_one=="},
		{"type":"message","role":"assistant","content":[{"type":"output_text","text":"An aperture is an opening."}]}]`)

	// And it survives the canonical encoding the transcript is stored in.
	stored, err := json.Marshal(resp.Message)
	if err != nil {
		t.Fatal(err)
	}
	var restored llm.Message
	if err := json.Unmarshal(stored, &restored); err != nil {
		t.Fatal(err)
	}
	again := buildResponses(t, llm.Request{Model: "gpt-5", Messages: []llm.Message{llm.TextMessage(llm.RoleUser, "again"), restored}})
	if canon(t, again["input"]) != canon(t, next["input"]) {
		t.Error("the replayed history changed after a storage round trip")
	}
}

func TestResponsesStreamKeepsAnEncryptedOnlyReasoningItem(t *testing.T) {
	rec, resp, err := decodeResponsesFixture(t, "responses_tool_call.sse")
	if err != nil {
		t.Fatalf("decode: %v", err)
	}

	if got, want := kinds(resp.Message.Parts), []llm.PartKind{llm.PartReasoning, llm.PartToolCall}; !slices.Equal(got, want) {
		t.Fatalf("parts = %v, want %v", got, want)
	}
	reasoning := resp.Message.Parts[0].(llm.ReasoningPart)
	if reasoning.Text != "" || string(reasoning.Redacted) != "gAAAAABo_encrypted_reasoning_blob_two==" {
		t.Errorf("reasoning = %+v", reasoning)
	}

	call := resp.Message.Parts[1].(llm.ToolCallPart)
	if call.ID != "call_search1" || call.Name != "search" || string(call.Arguments) != `{"q":"aperture","limit":3}` {
		t.Errorf("call = %+v", call)
	}
	if !call.Parsed() {
		t.Error("complete arguments were not parsed")
	}
	if resp.FinishReason != llm.FinishToolCalls {
		t.Errorf("finish = %q, want tool_calls", resp.FinishReason)
	}

	var starts []llm.Event
	for _, ev := range rec.Events {
		if ev.Type == llm.EventPartStart && ev.PartKind == llm.PartToolCall {
			starts = append(starts, ev)
		}
	}
	if len(starts) != 1 || starts[0].ToolCallID != "call_search1" || starts[0].ToolName != "search" || starts[0].Index != 1 {
		t.Errorf("tool part_start = %+v — id and name are known from output_item.added", starts)
	}
	if got := rec.Deltas(llm.PartToolCall); got != `{"q":"aperture","limit":3}` {
		t.Errorf("argument deltas = %q", got)
	}
	for _, ev := range rec.OfType(llm.EventPartDelta) {
		if ev.PartKind == llm.PartToolCall && (ev.ToolCallID != "call_search1" || ev.ToolName != "search") {
			t.Errorf("delta lost its call identity: %+v", ev)
		}
	}
	end := rec.OfType(llm.EventPartEnd)
	if len(end) != 2 || end[1].ToolCallID != "call_search1" || end[1].ToolName != "search" {
		t.Errorf("part_end events = %+v", end)
	}
}

func TestResponsesStreamKeepsParallelToolCallsApart(t *testing.T) {
	rec, resp, err := decodeResponsesFixture(t, "responses_two_tools.sse")
	if err != nil {
		t.Fatalf("decode: %v", err)
	}

	if got, want := kinds(resp.Message.Parts), []llm.PartKind{llm.PartText, llm.PartToolCall, llm.PartToolCall}; !slices.Equal(got, want) {
		t.Fatalf("parts = %v, want %v", got, want)
	}
	a := resp.Message.Parts[1].(llm.ToolCallPart)
	b := resp.Message.Parts[2].(llm.ToolCallPart)
	if a.ID != "call_a" || a.Name != "search" || string(a.Arguments) != `{"q":"lens"}` {
		t.Errorf("first call = %+v", a)
	}
	if b.ID != "call_b" || b.Name != "fetch" || string(b.Arguments) != `{"url":"https://example.test/x"}` {
		t.Errorf("second call = %+v", b)
	}
	if resp.FinishReason != llm.FinishToolCalls {
		t.Errorf("finish = %q", resp.FinishReason)
	}

	// The block indexes the runner keys on are dense and in generation order,
	// even though the wire's output_index would have been equally usable.
	var indexes []int
	for _, ev := range rec.OfType(llm.EventPartStart) {
		indexes = append(indexes, ev.Index)
	}
	if !slices.Equal(indexes, []int{0, 1, 2}) {
		t.Errorf("part indexes = %v", indexes)
	}
}

func TestResponsesStreamKeepsMalformedToolArgumentsRaw(t *testing.T) {
	stream := sseFrames(
		`{"type":"response.output_item.added","output_index":0,"item":{"type":"function_call","call_id":"c1","name":"search"}}`,
		`{"type":"response.function_call_arguments.delta","output_index":0,"delta":"{\"q\":"}`,
		`{"type":"response.output_item.done","output_index":0,"item":{"type":"function_call","call_id":"c1","name":"search","arguments":"{\"q\":"}}`,
		`{"type":"response.completed","response":{"status":"completed"}}`,
	)
	_, resp, err := decodeResponsesReader(t, strings.NewReader(stream))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	call := resp.Message.Parts[0].(llm.ToolCallPart)
	if call.Parsed() || call.RawArguments != `{"q":` {
		t.Errorf("call = %+v, want raw arguments kept and nothing parsed", call)
	}
}

func TestResponsesStreamTakesFinishedItemsWhenNoDeltasCame(t *testing.T) {
	stream := sseFrames(
		`{"type":"response.output_item.added","output_index":0,"item":{"type":"message"}}`,
		`{"type":"response.output_item.done","output_index":0,"item":{"type":"message","content":[{"type":"output_text","text":"all at once"}]}}`,
		`{"type":"response.output_item.done","output_index":1,"item":{"type":"function_call","call_id":"c","name":"n","arguments":"{}"}}`,
		`{"type":"response.completed","response":{"status":"completed"}}`,
	)
	rec, resp, err := decodeResponsesReader(t, strings.NewReader(stream))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Message.Text() != "all at once" || len(resp.Message.ToolCalls()) != 1 {
		t.Errorf("message = %+v", resp.Message)
	}
	if rec.Deltas(llm.PartText) != "all at once" {
		t.Errorf("the finished item was not streamed as a delta: %q", rec.Deltas(llm.PartText))
	}
}

func TestResponsesStreamIgnoresUnknownEventsAndEmptyItems(t *testing.T) {
	stream := sseFrames(
		`{"type":"response.created","response":{"id":"r"}}`,
		`{"type":"response.web_search_call.searching","output_index":0}`,
		`{"type":"response.something_new","payload":{"a":1}}`,
		`{"type":"response.output_item.added","output_index":1,"item":{"type":"reasoning"}}`,
		`{"type":"response.output_item.done","output_index":1,"item":{"type":"reasoning","summary":[]}}`,
		`{"type":"response.output_item.added","output_index":2,"item":{"type":"message"}}`,
		`{"type":"response.output_item.done","output_index":2,"item":{"type":"message","content":[]}}`,
		`{"type":"response.completed","response":{"status":"completed"}}`,
	)
	_, resp, err := decodeResponsesReader(t, strings.NewReader(stream))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Message.Parts) != 0 {
		t.Errorf("parts = %v, want none — empty items must not reach the history", kinds(resp.Message.Parts))
	}
}

func TestResponsesStreamIncompleteIsLength(t *testing.T) {
	rec, resp, err := decodeResponsesFixture(t, "responses_incomplete.sse")
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.FinishReason != llm.FinishLength || resp.RawFinishReason != "incomplete:max_output_tokens" {
		t.Errorf("finish = %q / %q", resp.FinishReason, resp.RawFinishReason)
	}
	if resp.Message.Text() != "The answer is cut sh" {
		t.Errorf("text = %q", resp.Message.Text())
	}
	if resp.Usage == nil || resp.Usage.OutputTokens != 16 {
		t.Errorf("usage = %+v", resp.Usage)
	}
	if n := len(rec.OfType(llm.EventResponseEnd)); n != 1 {
		t.Errorf("response_end events = %d, want 1 — a length stop is a finished turn", n)
	}
}

func TestResponsesStreamIncompleteForContentFilter(t *testing.T) {
	stream := sseFrames(`{"type":"response.incomplete","response":{"status":"incomplete","incomplete_details":{"reason":"content_filter"}}}`)
	_, resp, err := decodeResponsesReader(t, strings.NewReader(stream))
	if err != nil || resp.FinishReason != llm.FinishContentFilter {
		t.Errorf("finish = %q err = %v", resp.FinishReason, err)
	}
}

func TestResponsesStreamReportsInBandFailures(t *testing.T) {
	tests := []struct {
		name      string
		fixture   string
		kind      llm.ErrorKind
		code      string
		retryable bool
		keeps     string
	}{
		{"response.failed", "responses_failed.sse", llm.ErrorProvider, "server_error", true, "Partial"},
		{"error event", "responses_error.sse", llm.ErrorContextLength, "context_length_exceeded", false, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec, resp, err := decodeResponsesFixture(t, tc.fixture)
			if !llm.IsKind(err, tc.kind) {
				t.Fatalf("error = %v, want %s", err, tc.kind)
			}
			var modelErr *llm.Error
			errors.As(err, &modelErr)
			if modelErr.Code != tc.code || modelErr.Retryable != tc.retryable {
				t.Errorf("error = %+v", modelErr)
			}
			if resp.Message.Text() != tc.keeps {
				t.Errorf("partial text = %q, want %q", resp.Message.Text(), tc.keeps)
			}
			if resp.FinishReason != "" {
				t.Errorf("finish = %q, a failed turn has no finish reason", resp.FinishReason)
			}
			if n := len(rec.OfType(llm.EventResponseEnd)); n != 0 {
				t.Errorf("a failed turn emitted response_end %d times", n)
			}
		})
	}
}

func TestResponsesStreamMapsErrorEventShapes(t *testing.T) {
	tests := []struct {
		name  string
		frame string
		kind  llm.ErrorKind
	}{
		{"nested error", `{"type":"error","error":{"message":"bad key","code":"invalid_api_key"}}`, llm.ErrorAuth},
		{"rate limit", `{"type":"error","code":"rate_limit_exceeded","message":"slow down"}`, llm.ErrorRateLimit},
		{"failed with context length", `{"type":"response.failed","response":{"status":"failed","error":{"code":"context_length_exceeded","message":"too long"}}}`, llm.ErrorContextLength},
		{"failed status on a completed event", `{"type":"response.completed","response":{"status":"failed","error":{"code":"x","message":"boom"}}}`, llm.ErrorProvider},
		{"failed with no detail", `{"type":"response.failed","response":{"status":"failed"}}`, llm.ErrorProvider},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := decodeResponsesReader(t, strings.NewReader(sseFrames(tc.frame)))
			if !llm.IsKind(err, tc.kind) {
				t.Errorf("error = %v, want %s", err, tc.kind)
			}
		})
	}
}

func TestResponsesStreamWithoutATerminalEventIsNeverASuccess(t *testing.T) {
	tests := []struct {
		name    string
		fixture string
		kind    llm.ErrorKind
	}{
		{"closed between frames", "responses_truncated.sse", llm.ErrorProtocol},
		{"closed mid-frame", "responses_truncated_midline.sse", llm.ErrorProtocol},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec, resp, err := decodeResponsesFixture(t, tc.fixture)
			if !llm.IsKind(err, tc.kind) {
				t.Fatalf("error = %v, want %s", err, tc.kind)
			}
			if resp.FinishReason != "" {
				t.Errorf("finish = %q, a cut-off stream has no finish reason", resp.FinishReason)
			}
			if n := len(rec.OfType(llm.EventResponseEnd)); n != 0 {
				t.Errorf("response_end emitted %d times for a stream that never finished", n)
			}
		})
	}

	_, resp, _ := decodeResponsesFixture(t, "responses_truncated.sse")
	if resp.Message.Text() != "The connection died" {
		t.Errorf("what arrived was discarded: %q", resp.Message.Text())
	}
}

func TestResponsesStreamReadFailureIsTransport(t *testing.T) {
	reader := io.MultiReader(strings.NewReader("data: {\"type\":\"response.created\"}\n\n"), iotest.ErrReader(errors.New("connection reset")))
	_, _, err := decodeResponsesReader(t, reader)
	if !llm.IsKind(err, llm.ErrorTransport) {
		t.Errorf("error = %v, want transport", err)
	}
}

func TestResponsesStreamRejectsFramesThatAreNotJSON(t *testing.T) {
	_, _, err := decodeResponsesReader(t, strings.NewReader("data: {not json\n\n"))
	if !llm.IsKind(err, llm.ErrorProtocol) {
		t.Errorf("error = %v, want protocol", err)
	}
}

func TestResponsesStreamCapsToolArguments(t *testing.T) {
	stream := sseFrames(
		`{"type":"response.output_item.added","output_index":0,"item":{"type":"function_call","call_id":"c","name":"n"}}`,
		`{"type":"response.function_call_arguments.delta","output_index":0,"delta":"0123456789"}`,
	)
	rec := &llmtest.Recorder{}
	_, err := decodeResponses(context.Background(), strings.NewReader(stream), Config{MaxToolArgumentBytes: 5}, "m", rec.Emit)
	if !llm.IsKind(err, llm.ErrorProtocol) {
		t.Errorf("error = %v, want protocol", err)
	}
}

func TestResponsesStreamCancellationKeepsWhatArrived(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	rec := &llmtest.Recorder{}
	emit := func(ev llm.Event) error {
		_ = rec.Emit(ev)
		if ev.Type == llm.EventPartDelta {
			cancel()
		}
		return nil
	}
	resp, err := decodeResponses(ctx, bytes.NewReader(fixture(t, "responses_text.sse")), Config{}, "gpt-5", emit)
	rec.CheckProviderInvariants(t)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
	if resp.FinishReason != llm.FinishCancelled || resp.Message.Text() != "The aperture" {
		t.Errorf("response = %q %q", resp.FinishReason, resp.Message.Text())
	}
	if n := len(rec.OfType(llm.EventResponseEnd)); n != 1 {
		t.Errorf("response_end events = %d, want 1", n)
	}
}

func TestResponsesStreamEmitFailureAbortsTheStream(t *testing.T) {
	boom := errors.New("write failed")
	rec := &llmtest.Recorder{EmitErr: boom, FailAt: 2}
	_, err := decodeResponses(context.Background(), bytes.NewReader(fixture(t, "responses_text.sse")), Config{}, "m", rec.Emit)
	if !errors.Is(err, boom) {
		t.Errorf("error = %v, want the emit failure unchanged", err)
	}
}

// sseFrames wraps JSON objects as an SSE body, one event each.
func sseFrames(frames ...string) string {
	var b strings.Builder
	for _, f := range frames {
		b.WriteString("data: " + f + "\n\n")
	}
	return b.String()
}

// --- client ------------------------------------------------------------------

func TestResponsesClientPostsAStreamingRequest(t *testing.T) {
	var captured *http.Request
	var body []byte

	client, err := NewResponses(Config{
		Provider: "opencode-go",
		BaseURL:  "https://opencode.test/zen/go/v1/",
		APIKey:   "sk-test",
		Headers:  map[string]string{"X-Gateway": "internal"},
		HTTPClient: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			captured = r
			body, _ = io.ReadAll(r.Body)
			return respond(http.StatusOK, "text/event-stream", string(fixture(t, "responses_text.sse"))), nil
		})},
	})
	if err != nil {
		t.Fatalf("NewResponses: %v", err)
	}

	rec := &llmtest.Recorder{}
	resp, err := client.Stream(context.Background(), llm.Request{
		Model:    "gpt-5",
		Messages: []llm.Message{llm.TextMessage(llm.RoleUser, "hello")},
	}, rec.Emit)
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	rec.CheckProviderInvariants(t)

	if got, want := captured.URL.String(), "https://opencode.test/zen/go/v1/responses"; got != want {
		t.Errorf("URL = %q, want %q", got, want)
	}
	if captured.Method != http.MethodPost {
		t.Errorf("method = %s", captured.Method)
	}
	if got := captured.Header.Get("Authorization"); got != "Bearer sk-test" {
		t.Errorf("Authorization = %q", got)
	}
	if got := captured.Header.Get("Accept"); got != "text/event-stream" {
		t.Errorf("Accept = %q", got)
	}
	if got := captured.Header.Get("X-Gateway"); got != "internal" {
		t.Errorf("configured header missing, got %q", got)
	}
	var sent map[string]any
	if err := json.Unmarshal(body, &sent); err != nil {
		t.Fatalf("request body: %v", err)
	}
	if sent["stream"] != true || sent["store"] != false {
		t.Errorf("body = %v", sent)
	}
	if resp.Message.Text() != "The aperture is open." || resp.Provider != "opencode-go" {
		t.Errorf("response = %q from %q", resp.Message.Text(), resp.Provider)
	}
}

func TestResponsesClientMapsFailedStatusesOntoActionableKinds(t *testing.T) {
	tests := []struct {
		name      string
		status    int
		body      string
		want      llm.ErrorKind
		retryable bool
	}{
		{"401", http.StatusUnauthorized, `{"error":{"message":"Incorrect API key provided: sk-secret","code":"invalid_api_key"}}`, llm.ErrorAuth, false},
		{"429", http.StatusTooManyRequests, `{"error":{"message":"Rate limit reached","code":"rate_limit_exceeded"}}`, llm.ErrorRateLimit, true},
		{"400 context length by code", http.StatusBadRequest, `{"error":{"message":"Your input exceeds the context window of this model.","code":"context_length_exceeded"}}`, llm.ErrorContextLength, false},
		{"400 context length by message", http.StatusBadRequest, `{"error":{"message":"This model's maximum context length is 400000 tokens."}}`, llm.ErrorContextLength, false},
		{"400 anything else", http.StatusBadRequest, `{"error":{"message":"Unsupported parameter: 'temperature'"}}`, llm.ErrorProvider, false},
		{"502", http.StatusBadGateway, `<html>bad gateway</html>`, llm.ErrorProvider, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			client, err := NewResponses(Config{
				Provider: "opencode-go",
				BaseURL:  "https://opencode.test/v1",
				APIKey:   "sk-secret",
				HTTPClient: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
					return respond(tc.status, "application/json", tc.body), nil
				})},
			})
			if err != nil {
				t.Fatalf("NewResponses: %v", err)
			}

			rec := &llmtest.Recorder{}
			resp, err := client.Stream(context.Background(), llm.Request{
				Model:    "gpt-5",
				Messages: []llm.Message{llm.TextMessage(llm.RoleUser, "hello")},
			}, rec.Emit)

			var modelErr *llm.Error
			if !errors.As(err, &modelErr) || modelErr.Kind != tc.want {
				t.Fatalf("error = %v, want kind %s", err, tc.want)
			}
			if modelErr.Retryable != tc.retryable {
				t.Errorf("retryable = %v, want %v", modelErr.Retryable, tc.retryable)
			}
			if strings.Contains(err.Error(), "sk-secret") {
				t.Errorf("the credential reached the error message: %v", err)
			}
			if len(rec.Events) != 0 {
				t.Errorf("events = %v, want none before the stream started", rec.Types())
			}
			if resp.Provider != "opencode-go" || resp.Model != "gpt-5" {
				t.Errorf("response = %+v, want it attributable even when empty", resp)
			}
		})
	}
}

func TestResponsesClientKeepsTheKeyOutOfInBandErrors(t *testing.T) {
	client, err := NewResponses(Config{
		BaseURL: "https://opencode.test/v1",
		APIKey:  "sk-secret-key-123",
		HTTPClient: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			// The fixture's failure message echoes the key.
			return respond(http.StatusOK, "text/event-stream", string(fixture(t, "responses_failed.sse"))), nil
		})},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Stream(context.Background(), llm.Request{
		Model: "m", Messages: []llm.Message{llm.TextMessage(llm.RoleUser, "hi")},
	}, (&llmtest.Recorder{}).Emit)
	if err == nil {
		t.Fatal("want the stream's failure")
	}
	if strings.Contains(err.Error(), "sk-secret-key-123") || !strings.Contains(err.Error(), "[redacted]") {
		t.Errorf("error = %v", err)
	}
}

func TestResponsesClientKeepsTheKeyOutOfTransportErrors(t *testing.T) {
	client, err := NewResponses(Config{
		BaseURL: "https://opencode.test/v1",
		APIKey:  "sk-secret",
		HTTPClient: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, errors.New("dial tcp: connection refused")
		})},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Stream(context.Background(), llm.Request{
		Model: "m", Messages: []llm.Message{llm.TextMessage(llm.RoleUser, "hi")},
	}, (&llmtest.Recorder{}).Emit)
	if !llm.IsKind(err, llm.ErrorTransport) {
		t.Fatalf("error = %v, want transport", err)
	}
	if strings.Contains(err.Error(), "sk-secret") {
		t.Errorf("the credential reached the error message: %v", err)
	}
}

func TestResponsesClientCancelledBeforeAnswerReturnsTheContextError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	client, _ := NewResponses(Config{
		BaseURL: "https://opencode.test/v1",
		HTTPClient: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			cancel()
			return nil, r.Context().Err()
		})},
	})
	_, err := client.Stream(ctx, llm.Request{Model: "m", Messages: []llm.Message{llm.TextMessage(llm.RoleUser, "hi")}}, (&llmtest.Recorder{}).Emit)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("error = %v, want context.Canceled", err)
	}
}

func TestNewResponsesRejectsAConfigWithoutAnEndpoint(t *testing.T) {
	if _, err := NewResponses(Config{Provider: "opencode-go"}); err == nil {
		t.Fatal("a client with no base URL was accepted")
	}
}

func TestResponsesBodyAsksForReasoningOnlyWhereItApplies(t *testing.T) {
	// gpt-4.1 answers 400 to include/reasoning, so a model that does not think
	// gets neither; one that does gets its summary even with no level chosen.
	plain := llm.Request{Model: "m", Messages: []llm.Message{llm.TextMessage(llm.RoleUser, "hi")}}
	withEffort := plain
	withEffort.Options.Reasoning = &llm.ReasoningOptions{Effort: llm.EffortLow}

	tests := []struct {
		name          string
		req           llm.Request
		cfg           Config
		wantReasoning string // "" means the key must be absent
		wantInclude   bool
	}{
		{"non-reasoning model, no level", plain, Config{}, "", false},
		{"reasoning model, no level", plain, Config{Reasoning: true}, `{"summary":"auto"}`, true},
		{"reasoning model, a level", withEffort, Config{Reasoning: true}, `{"effort":"low","summary":"auto"}`, true},
		{"level on a model the catalogue does not know", withEffort, Config{}, `{"effort":"low","summary":"auto"}`, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sent := buildResponsesWith(t, tc.req, tc.cfg)
			if tc.wantReasoning == "" {
				if _, has := sent["reasoning"]; has {
					t.Errorf("reasoning = %v", sent["reasoning"])
				}
			} else {
				jsonEq(t, "reasoning", sent["reasoning"], tc.wantReasoning)
			}
			_, has := sent["include"]
			if has != tc.wantInclude {
				t.Errorf("include present = %v, want %v", has, tc.wantInclude)
			}
		})
	}
}

func TestResponsesStreamKeepsAnAssistantMessagesPhase(t *testing.T) {
	stream := sseFrames(
		`{"type":"response.output_text.delta","output_index":0,"delta":"Checking."}`,
		`{"type":"response.output_item.done","output_index":0,"item":{"type":"message","phase":"commentary","content":[{"type":"output_text","text":"Checking."}]}}`,
		`{"type":"response.output_item.added","output_index":1,"item":{"type":"function_call","call_id":"c1","name":"search"}}`,
		`{"type":"response.output_item.done","output_index":1,"item":{"type":"function_call","call_id":"c1","name":"search","arguments":"{}"}}`,
		`{"type":"response.output_item.done","output_index":2,"item":{"type":"message","phase":"final_answer","content":[{"type":"output_text","text":"Done."}]}}`,
		`{"type":"response.completed","response":{"status":"completed"}}`,
	)
	rec, resp, err := decodeResponsesReader(t, strings.NewReader(stream))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	first, last := resp.Message.Parts[0].(llm.TextPart), resp.Message.Parts[2].(llm.TextPart)
	if first.Phase != "commentary" || last.Phase != "final_answer" {
		t.Errorf("phases = %q / %q", first.Phase, last.Phase)
	}
	// The finished block the run layer persists carries it too.
	if end := rec.OfType(llm.EventPartEnd); end[0].Part.(llm.TextPart).Phase != "commentary" {
		t.Errorf("part_end = %+v", end[0].Part)
	}
}

func TestResponsesHistoryResendsPhase(t *testing.T) {
	history := []llm.Message{
		llm.TextMessage(llm.RoleUser, "go"),
		{Role: llm.RoleAssistant, Parts: []llm.Part{
			llm.TextPart{Text: "Checking.", Phase: "commentary"},
			llm.TextPart{Text: " Still.", Phase: "commentary"},
			llm.ToolCallPart{ID: "c1", Name: "search", Arguments: json.RawMessage(`{}`), RawArguments: `{}`},
			llm.TextPart{Text: "Done.", Phase: "final_answer"},
			// Whatever another provider recorded is not a phase this API knows.
			llm.TextPart{Text: "Extra.", Phase: "thinking-aloud"},
		}},
	}
	sent := buildResponses(t, llm.Request{Model: "gpt-5.3-codex", Messages: history})
	jsonEq(t, "input", sent["input"], `[
		{"type":"message","role":"user","content":[{"type":"input_text","text":"go"}]},
		{"type":"message","role":"assistant","phase":"commentary","content":[{"type":"output_text","text":"Checking. Still."}]},
		{"type":"function_call","call_id":"c1","name":"search","arguments":"{}"},
		{"type":"message","role":"assistant","phase":"final_answer","content":[{"type":"output_text","text":"Done."}]},
		{"type":"message","role":"assistant","content":[{"type":"output_text","text":"Extra."}]}]`)
}

func TestResponsesStreamTreatsACancelledResponseAsAnError(t *testing.T) {
	// Nothing here starts a background response, so a cancelled one is somebody
	// else's stop; passing it off as a finished answer would save half a reply.
	stream := sseFrames(
		`{"type":"response.output_text.delta","output_index":0,"delta":"Half"}`,
		`{"type":"response.incomplete","response":{"status":"cancelled"}}`,
	)
	_, _, err := decodeResponsesReader(t, strings.NewReader(stream))
	if !llm.IsKind(err, llm.ErrorProvider) {
		t.Errorf("error = %v, want a provider error", err)
	}
}
