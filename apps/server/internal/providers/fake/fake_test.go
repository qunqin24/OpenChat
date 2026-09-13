package fake

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"slices"
	"testing"

	"github.com/qunqin/openchat/apps/server/internal/llm"
	"github.com/qunqin/openchat/apps/server/internal/llm/llmtest"
)

func load(t *testing.T, name string) *Provider {
	t.Helper()
	provider, err := LoadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("LoadFile(%s): %v", name, err)
	}
	return provider
}

func TestProviderReplaysAFixtureWithNoNetwork(t *testing.T) {
	rec := &llmtest.Recorder{}
	resp, err := load(t, "tool_call.json").Stream(context.Background(), llm.Request{Model: "ignored"}, rec.Emit)
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	rec.CheckProviderInvariants(t)

	want := []llm.PartKind{llm.PartReasoning, llm.PartText, llm.PartToolCall}
	got := make([]llm.PartKind, 0, len(resp.Message.Parts))
	for _, part := range resp.Message.Parts {
		got = append(got, part.Kind())
	}
	if !slices.Equal(got, want) {
		t.Fatalf("parts = %v, want %v", got, want)
	}

	if text := resp.Message.Text(); text != "Looking that up for you." {
		t.Errorf("text = %q", text)
	}
	if resp.Model != "fake-reasoner" {
		t.Errorf("model = %q, the script's model wins over the requested one", resp.Model)
	}

	call := resp.Message.ToolCalls()[0]
	if !call.Parsed() {
		t.Fatalf("arguments did not parse: %q", call.RawArguments)
	}
	var args map[string]string
	if err := json.Unmarshal(call.Arguments, &args); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if args["query"] != "quarterly report" {
		t.Errorf("arguments = %v", args)
	}
	if resp.FinishReason != llm.FinishToolCalls {
		t.Errorf("finish reason = %q", resp.FinishReason)
	}
	if resp.Usage == nil || resp.Usage.CacheReadTokens != 180 {
		t.Errorf("usage = %+v", resp.Usage)
	}
}

func TestProviderCancelledMidScriptKeepsPartialContentAndEndsOnce(t *testing.T) {
	// The script waits a minute before its second block, so cancelling once the
	// first one closes is deterministic without any sleeping.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	rec := &llmtest.Recorder{}
	emit := func(ev llm.Event) error {
		if err := rec.Emit(ev); err != nil {
			return err
		}
		if ev.Type == llm.EventPartEnd && ev.Index == 0 {
			cancel()
		}
		return nil
	}

	resp, err := load(t, "slow_text.json").Stream(ctx, llm.Request{Model: "fake-1"}, emit)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
	rec.CheckProviderInvariants(t)

	if resp.FinishReason != llm.FinishCancelled {
		t.Errorf("finish reason = %q", resp.FinishReason)
	}
	if text := resp.Message.Text(); text != "An aperture is an opening " {
		t.Errorf("text = %q, the part already generated must survive the stop", text)
	}
	if ends := rec.OfType(llm.EventResponseEnd); len(ends) != 1 {
		t.Fatalf("response_end count = %d, want exactly one", len(ends))
	}
}

func TestProviderReportsAnInjectedFailureWithoutFinishingTheTurn(t *testing.T) {
	provider, err := New(Script{
		Provider:     "fake",
		Model:        "fake-1",
		FinishReason: "stop",
		Steps:        []Step{{Kind: StepText, Chunks: []string{"half an "}}},
		Failure:      &Failure{AfterStep: 1, Kind: string(llm.ErrorProvider), Message: "the model gave up"},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	rec := &llmtest.Recorder{}
	resp, err := provider.Stream(context.Background(), llm.Request{Model: "fake-1"}, rec.Emit)
	if !llm.IsKind(err, llm.ErrorProvider) {
		t.Fatalf("error = %v, want a provider error", err)
	}
	rec.CheckProviderInvariants(t)

	if len(rec.OfType(llm.EventResponseEnd)) != 0 {
		t.Error("a turn that broke has no boundary to publish")
	}
	if resp.FinishReason != "" {
		t.Errorf("finish reason = %q, want empty", resp.FinishReason)
	}
	if resp.Message.Text() != "half an " {
		t.Errorf("text = %q", resp.Message.Text())
	}
}

func TestProviderStopsWhenTheRunLayerCannotAcceptEvents(t *testing.T) {
	persistFailed := errors.New("persisting the delta failed")
	provider, err := New(Script{
		Model:        "fake-1",
		FinishReason: "stop",
		Steps:        []Step{{Kind: StepText, Chunks: []string{"a", "b", "c"}}},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	rec := &llmtest.Recorder{EmitErr: persistFailed, FailAt: 2}
	if _, err := provider.Stream(context.Background(), llm.Request{Model: "fake-1"}, rec.Emit); !errors.Is(err, persistFailed) {
		t.Fatalf("error = %v, want the run layer's own error", err)
	}
	if len(rec.OfType(llm.EventResponseEnd)) != 0 {
		t.Error("the turn must not be published as finished after a failed write")
	}
}

func TestLoadRejectsAScriptThatWouldTestNothing(t *testing.T) {
	tests := []struct {
		name   string
		script string
	}{
		{
			name:   "an unknown step kind",
			script: `{"model":"m","finish_reason":"stop","steps":[{"kind":"singing","chunks":["x"]}]}`,
		},
		{
			name:   "a tool call with no name",
			script: `{"model":"m","finish_reason":"stop","steps":[{"kind":"tool_call","chunks":["{}"]}]}`,
		},
		{
			name:   "a finish reason that is not one of ours",
			script: `{"model":"m","finish_reason":"done","steps":[]}`,
		},
		{
			name:   "no finish reason at all",
			script: `{"model":"m","steps":[]}`,
		},
		{
			name: "a misspelled field, which would silently replay nothing",
			// Strict decoding here, unlike the wire adapters: a fixture is ours
			// to get right, and one that quietly tests nothing is worse than
			// one that fails to load.
			script: `{"model":"m","finish_reason":"stop","stpes":[]}`,
		},
		{
			name:   "a failure scheduled past the end of the script",
			script: `{"model":"m","finish_reason":"stop","steps":[],"failure":{"after_step":3,"kind":"provider","message":"x"}}`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := Load([]byte(tc.script)); err == nil {
				t.Fatal("the script was accepted")
			}
		})
	}
}
