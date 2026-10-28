package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/qunqin24/openchat/apps/server/internal/llm"
	"github.com/qunqin24/openchat/apps/server/internal/llm/llmtest"
)

// OpenRouter's reasoning_details have to come back unmodified for Claude's
// signed thinking and the encrypted state of OpenAI and Gemini to survive a
// tool loop: https://openrouter.ai/docs/guides/best-practices/reasoning-tokens

var openRouterCompat = Compat{ReasoningDetails: true, Thinking: ThinkingOpenRouter}

func decodeDetails(t *testing.T, cfg Config) (*llmtest.Recorder, llm.ModelResponse) {
	t.Helper()
	rec := &llmtest.Recorder{}
	resp, err := decode(context.Background(), bytes.NewReader(fixture(t, "openrouter_details.sse")), cfg,
		"anthropic/claude-sonnet-4.5", rec.Emit)
	rec.CheckProviderInvariants(t)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	return rec, resp
}

func TestStreamKeepsReasoningDetailsOnTheThinkingBlock(t *testing.T) {
	rec, resp := decodeDetails(t, Config{Provider: "openrouter", Compat: openRouterCompat})

	want := []llm.PartKind{llm.PartReasoning, llm.PartText, llm.PartToolCall}
	if got := kinds(resp.Message.Parts); len(got) != len(want) || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Fatalf("parts = %v, want %v", got, want)
	}
	reasoning := resp.Message.Parts[0].(llm.ReasoningPart)
	if reasoning.Text != "The user wants the weather." {
		t.Errorf("shown reasoning = %q", reasoning.Text)
	}
	if got, want := reasoning.Signature, llm.SignatureOpenRouter+"anthropic/claude-sonnet-4.5"; got != want {
		t.Errorf("signature = %q, want %q", got, want)
	}
	// The streamed fragments are joined into the shape a non-streaming
	// response has: one reasoning.text carrying its signature, then the
	// encrypted state, with fields we do not know about intact.
	var stored []map[string]any
	if err := json.Unmarshal(reasoning.Redacted, &stored); err != nil {
		t.Fatalf("stored details: %v", err)
	}
	if len(stored) != 2 {
		t.Fatalf("stored = %s, want the joined text and the encrypted detail", reasoning.Redacted)
	}
	if stored[0]["type"] != "reasoning.text" || stored[0]["text"] != "The user wants the weather." ||
		stored[0]["signature"] != "EqQBsignature==" || stored[0]["format"] != "anthropic-claude-v1" || stored[0]["index"] != float64(0) {
		t.Errorf("first detail = %v", stored[0])
	}
	if stored[1]["type"] != "reasoning.encrypted" || stored[1]["data"] != "opaque-state" ||
		stored[1]["id"] != "tool_toolu_1" || stored[1]["format"] != "google-gemini-v1" {
		t.Errorf("second detail = %v", stored[1])
	}
	if vendor, _ := stored[1]["vendor_field"].(map[string]any); vendor["keep"] != true {
		t.Errorf("a field we do not model was dropped: %v", stored[1])
	}

	// What is displayed is the plain reasoning string once, not also the
	// details' copy of it, and never the signature.
	if got := rec.Deltas(llm.PartReasoning); got != "The user wants the weather." {
		t.Errorf("reasoning deltas = %q", got)
	}
}

func TestStreamIgnoresReasoningDetailsWhereTheyAreNotReplayed(t *testing.T) {
	// Stored only for an endpoint that will be sent them back; a relay that
	// happens to emit the field gets a plain reasoning part.
	_, resp := decodeDetails(t, Config{Provider: "relay"})
	reasoning := resp.Message.Parts[0].(llm.ReasoningPart)
	if reasoning.Signature != "" || len(reasoning.Redacted) != 0 {
		t.Errorf("reasoning = %+v, want no signature and no blob", reasoning)
	}
	if reasoning.Text != "The user wants the weather." {
		t.Errorf("text = %q", reasoning.Text)
	}
}

func TestStreamShowsDetailsThatCameWithoutAReasoningString(t *testing.T) {
	// OpenAI models through OpenRouter send a summary and the encrypted state
	// only as details. The summary is the thinking to show; an encrypted detail
	// that arrives after the answer began joins the thinking block, not a new
	// part in the middle of the answer.
	stream := "data: " + `{"id":"g","model":"openai/gpt-5","choices":[{"index":0,"delta":{"reasoning_details":[{"type":"reasoning.summary","summary":"Plan: ","id":"rs_1","format":"openai-responses-v1","index":0}]}}]}` + "\n\n" +
		"data: " + `{"id":"g","model":"openai/gpt-5","choices":[{"index":0,"delta":{"reasoning_details":[{"type":"reasoning.summary","summary":"add.","id":"rs_1","format":"openai-responses-v1","index":0}]}}]}` + "\n\n" +
		"data: " + `{"id":"g","model":"openai/gpt-5","choices":[{"index":0,"delta":{"content":"4"}}]}` + "\n\n" +
		"data: " + `{"id":"g","model":"openai/gpt-5","choices":[{"index":0,"delta":{"reasoning_details":[{"type":"reasoning.encrypted","data":"enc","id":"rs_1","format":"openai-responses-v1","index":1}]},"finish_reason":"stop"}]}` + "\n\n" +
		"data: [DONE]\n\n"

	rec := &llmtest.Recorder{}
	resp, err := decode(context.Background(), strings.NewReader(stream), Config{Provider: "openrouter", Compat: openRouterCompat}, "openai/gpt-5", rec.Emit)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	rec.CheckProviderInvariants(t)

	if got := kinds(resp.Message.Parts); len(got) != 2 || got[0] != llm.PartReasoning || got[1] != llm.PartText {
		t.Fatalf("parts = %v", got)
	}
	reasoning := resp.Message.Parts[0].(llm.ReasoningPart)
	if reasoning.Text != "Plan: add." {
		t.Errorf("shown reasoning = %q", reasoning.Text)
	}
	var stored []map[string]any
	if err := json.Unmarshal(reasoning.Redacted, &stored); err != nil || len(stored) != 2 ||
		stored[0]["summary"] != "Plan: add." || stored[1]["data"] != "enc" {
		t.Errorf("stored = %s (%v)", reasoning.Redacted, err)
	}
}

func TestStreamGivesAnEncryptedOnlyReasoningABlockOfItsOwn(t *testing.T) {
	stream := "data: " + `{"id":"g","model":"google/gemini","choices":[{"index":0,"delta":{"reasoning_details":[{"type":"reasoning.encrypted","data":"sig","id":"tool_1","format":"google-gemini-v1","index":0}],"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"f","arguments":"{}"}}]},"finish_reason":"tool_calls"}]}` + "\n\n" +
		"data: [DONE]\n\n"
	rec := &llmtest.Recorder{}
	resp, err := decode(context.Background(), strings.NewReader(stream), Config{Provider: "openrouter", Compat: openRouterCompat}, "google/gemini", rec.Emit)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	rec.CheckProviderInvariants(t)
	if got := kinds(resp.Message.Parts); len(got) != 2 || got[0] != llm.PartReasoning || got[1] != llm.PartToolCall {
		t.Fatalf("parts = %v, want the encrypted reasoning before the call", got)
	}
	if reasoning := resp.Message.Parts[0].(llm.ReasoningPart); reasoning.Text != "" || len(reasoning.Redacted) == 0 {
		t.Errorf("reasoning = %+v", reasoning)
	}
}

func TestStreamSurvivesReasoningDetailsInAnotherShape(t *testing.T) {
	stream := "data: " + `{"id":"g","model":"m","choices":[{"index":0,"delta":{"content":"hi","reasoning_details":"not an array"},"finish_reason":"stop"}]}` + "\n\ndata: [DONE]\n\n"
	rec := &llmtest.Recorder{}
	resp, err := decode(context.Background(), strings.NewReader(stream), Config{Provider: "openrouter", Compat: openRouterCompat}, "m", rec.Emit)
	if err != nil || resp.Message.Text() != "hi" {
		t.Fatalf("resp = %q, err = %v, want the answer despite the odd field", resp.Message.Text(), err)
	}
}

func replayMessages(t *testing.T, cfg Config, model string, parts ...llm.Part) []map[string]any {
	t.Helper()
	body, err := buildBody(llm.Request{Model: model, Messages: []llm.Message{
		llm.TextMessage(llm.RoleUser, "hi"),
		{Role: llm.RoleAssistant, Parts: parts},
	}}, cfg)
	if err != nil {
		t.Fatalf("buildBody: %v", err)
	}
	var sent struct {
		Messages []map[string]any `json:"messages"`
	}
	if err := json.Unmarshal(body, &sent); err != nil {
		t.Fatal(err)
	}
	return sent.Messages
}

func TestBuildBodyReplaysReasoningDetailsUnmodified(t *testing.T) {
	_, resp := decodeDetails(t, Config{Provider: "openrouter", Compat: openRouterCompat})
	parts := resp.Message.Parts
	stored := string(parts[0].(llm.ReasoningPart).Redacted)

	cfg := Config{Compat: Compat{ReasoningDetails: true, EchoReasoning: true}}
	sent := replayMessages(t, cfg, "anthropic/claude-sonnet-4.5", parts...)[1]

	raw, _ := json.Marshal(sent["reasoning_details"])
	if string(raw) != stored {
		t.Errorf("reasoning_details = %s\nwant the stored %s", raw, stored)
	}
	// They replace the plain string: one record of the thinking, not two.
	if _, has := sent["reasoning_content"]; has {
		t.Errorf("reasoning_content sent beside reasoning_details: %v", sent["reasoning_content"])
	}
	if sent["content"] != "Checking." || sent["tool_calls"] == nil {
		t.Errorf("message = %v", sent)
	}
}

func TestBuildBodyOnlyReplaysReasoningDetailsToTheModelThatWroteThem(t *testing.T) {
	_, resp := decodeDetails(t, Config{Provider: "openrouter", Compat: openRouterCompat})
	parts := resp.Message.Parts
	cfg := Config{Compat: Compat{ReasoningDetails: true}}

	if sent := replayMessages(t, cfg, "openai/gpt-5", parts...)[1]; sent["reasoning_details"] != nil {
		t.Errorf("another model was sent Claude's signed thinking: %v", sent["reasoning_details"])
	}
	if sent := replayMessages(t, Config{}, "anthropic/claude-sonnet-4.5", parts...)[1]; sent["reasoning_details"] != nil {
		t.Errorf("an endpoint that does not take the field was sent it: %v", sent["reasoning_details"])
	}
	// Nor does the blob leak out as reasoning_content on an endpoint that
	// echoes plain reasoning.
	echo := Config{Compat: Compat{EchoReasoning: true}}
	if sent := replayMessages(t, echo, "anthropic/claude-sonnet-4.5", parts...)[1]; sent["reasoning_content"] != nil {
		t.Errorf("reasoning_content = %v", sent["reasoning_content"])
	}
}
