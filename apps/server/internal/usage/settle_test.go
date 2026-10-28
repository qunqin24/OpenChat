package usage

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/qunqin24/openchat/apps/server/internal/llm"
)

// settle is the whole rule for when a row is an estimate; the pg tests of the
// agent package show it end to end.
func TestSettleEstimatesOnlyWhatWasNotReported(t *testing.T) {
	text := func(s string) llm.Message {
		return llm.Message{Parts: []llm.Part{llm.TextPart{Text: s}}}
	}
	words := strings.Repeat("word ", 80) // 400 characters: 100 tokens, 125 widened
	stopped, cancel := context.WithCancel(context.Background())
	cancel()
	running := context.Background()
	broke := errors.New("reset")

	cases := []struct {
		name     string
		ctx      context.Context
		resp     llm.ModelResponse
		err      error
		alive    bool
		in, out  int
		estimate bool
	}{
		{"clean with usage is the provider's", running,
			llm.ModelResponse{Usage: &llm.Usage{InputTokens: 10, OutputTokens: 5}, Message: text(words)}, nil, true, 10, 5, false},
		{"clean without usage is estimated", running,
			llm.ModelResponse{Message: text(words)}, nil, true, 1000, 125, true},
		{"stopped without usage", stopped,
			llm.ModelResponse{Message: text(words)}, context.Canceled, true, 1000, 125, true},
		{"stopped before anything arrived is owed its input", stopped,
			llm.ModelResponse{}, context.Canceled, false, 1000, 0, true},
		{"input-only report keeps the input", stopped,
			llm.ModelResponse{Usage: &llm.Usage{InputTokens: 7, CacheReadTokens: 3}, Message: text(words)}, context.Canceled, true, 7, 125, true},
		{"output-only report keeps the output", stopped,
			llm.ModelResponse{Usage: &llm.Usage{OutputTokens: 9}, Message: text(words)}, context.Canceled, true, 1000, 9, true},
		{"a full report is final even when stopped", stopped,
			llm.ModelResponse{Usage: &llm.Usage{InputTokens: 7, OutputTokens: 9}, Message: text(words)}, context.Canceled, true, 7, 9, false},
		{"broke after it began", running,
			llm.ModelResponse{Message: text(words)}, broke, true, 1000, 125, true},
		{"refused before it began was not billed", running,
			llm.ModelResponse{}, broke, false, 0, 0, false},
	}
	for _, c := range cases {
		m := &Meter{call: Call{InputEstimate: 1000}, alive: c.alive}
		got, estimated := m.settle(c.ctx, c.resp, c.err)
		in, out := 0, 0
		if got != nil {
			in, out = got.InputTokens, got.OutputTokens
		}
		if in != c.in || out != c.out || estimated != c.estimate {
			t.Errorf("%s: in/out %d/%d estimated %v, want %d/%d %v", c.name, in, out, estimated, c.in, c.out, c.estimate)
		}
	}
}

// Reasoning and tool-call arguments are output too, and reasoning is reported
// as a part of it.
func TestSettleCountsReasoningAndToolArguments(t *testing.T) {
	m := &Meter{alive: true}
	got, estimated := m.settle(context.Background(), llm.ModelResponse{Message: llm.Message{Parts: []llm.Part{
		llm.ReasoningPart{Text: strings.Repeat("think ", 80)},
		llm.ToolCallPart{ID: "c", Name: "web_fetch", RawArguments: `{"url":"https://example.com/"}`},
	}}}, errors.New("reset"))
	if !estimated || got == nil || got.ReasoningTokens == 0 || got.OutputTokens <= got.ReasoningTokens {
		t.Fatalf("usage = %+v, estimated %v", got, estimated)
	}
}
