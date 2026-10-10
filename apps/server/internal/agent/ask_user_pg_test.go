package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/qunqin/openchat/apps/server/internal/db/sqlc"
	"github.com/qunqin/openchat/apps/server/internal/llm"
	"github.com/qunqin/openchat/apps/server/internal/providers/fake"
)

// askTool stands in for tools.AskUser, which this package cannot import: a
// UserTool whose calls are valid when they have a "questions" key.
type askTool struct{}

func (askTool) Definition() llm.Tool {
	return llm.Tool{Name: ToolAskUser, Description: "ask", Parameters: json.RawMessage(`{"type":"object"}`)}
}
func (askTool) Replayable() bool { return false }
func (askTool) Execute(context.Context, json.RawMessage) (ToolResult, error) {
	panic("a UserTool is never executed")
}
func (askTool) Check(args json.RawMessage) error {
	if !strings.Contains(string(args), "questions") {
		return Errorf("invalid arguments: ask at least one question")
	}
	return nil
}

const askArgs = `{"questions":[{"question":"Which?","header":"Pick","options":[{"label":"A"},{"label":"B"}]}]}`

// A call to the user's tool ends the run on the spot: finished as
// awaiting_input, the call on the transcript, no result, nothing running.
func TestAskUserEndsTheRunAwaitingInput(t *testing.T) {
	f := newFixture(t, []Tool{askTool{}}, toolCall(t, "q1", ToolAskUser, askArgs))
	run := f.admit("help me choose")
	f.runner.Start(run.ID)
	done := f.waitFor(run.ID, sqlc.RunStatusAwaitingInput)

	if !done.FinishedAt.Valid {
		t.Fatal("an awaiting run is not finished")
	}
	f.assertTranscript("user help me choose", "assistant:tool_calls call:ask_user")
	if f.models.calls.Load() != 1 {
		t.Fatalf("%d model requests, want 1", f.models.calls.Load())
	}
	// It no longer holds the conversation's one active-run slot.
	if _, err := f.q.GetActiveRun(context.Background(), f.conv); err == nil {
		t.Fatal("an awaiting run is still the active one")
	}
	var finished bool
	for _, b := range f.events() {
		for _, e := range b.Events {
			if rf, ok := e.(RunFinished); ok && rf.Run.Status == sqlc.RunStatusAwaitingInput {
				finished = true
			}
		}
	}
	if !finished {
		t.Fatal("no RunFinished event with the awaiting status")
	}
}

// The response's other calls are run first, then the run stops.
func TestAskUserRunsTheOtherCallsFirst(t *testing.T) {
	echo := &echoTool{name: "echo", replay: true}
	f := newFixture(t, []Tool{echo, askTool{}}, script(t, fake.Script{Model: "m", FinishReason: "tool_calls", Steps: []fake.Step{
		{Kind: fake.StepToolCall, ToolCallID: "q1", ToolName: ToolAskUser, Chunks: []string{askArgs}},
		{Kind: fake.StepToolCall, ToolCallID: "e1", ToolName: "echo", Chunks: []string{`{"x":1}`}},
	}}))
	run := f.admit("go")
	f.runner.Start(run.ID)
	f.waitFor(run.ID, sqlc.RunStatusAwaitingInput)

	if echo.calls.Load() != 1 {
		t.Fatalf("echo ran %d times, want 1", echo.calls.Load())
	}
	f.assertTranscript("user go", "assistant:tool_calls call:ask_user call:echo", "tool result:succeeded")
}

// A question that cannot be put is answered with the reason, and the model
// tries again: the run is not stopped over it.
func TestAskUserWithBadArgumentsIsAnsweredAndTheRunGoesOn(t *testing.T) {
	f := newFixture(t, []Tool{askTool{}},
		toolCall(t, "q1", ToolAskUser, `{"q":1}`),
		answer(t, "never mind"))
	run := f.admit("go")
	f.runner.Start(run.ID)
	f.waitFor(run.ID, sqlc.RunStatusCompleted)
	f.assertTranscript("user go", "assistant:tool_calls call:ask_user", "tool result:failed", "assistant:stop never mind")
}

// Only one call can wait: the model is told to put every question in one.
func TestAskUserSecondCallInOneResponseIsRefused(t *testing.T) {
	f := newFixture(t, []Tool{askTool{}}, script(t, fake.Script{Model: "m", FinishReason: "tool_calls", Steps: []fake.Step{
		{Kind: fake.StepToolCall, ToolCallID: "q1", ToolName: ToolAskUser, Chunks: []string{askArgs}},
		{Kind: fake.StepToolCall, ToolCallID: "q2", ToolName: ToolAskUser, Chunks: []string{askArgs}},
	}}))
	run := f.admit("go")
	f.runner.Start(run.ID)
	f.waitFor(run.ID, sqlc.RunStatusAwaitingInput)
	f.assertTranscript("user go", "assistant:tool_calls call:ask_user call:ask_user", "tool result:failed")
}

// Stopping while the response's other calls run drops the question too: the
// transcript closes every call, and the turn ends stopped.
func TestAskUserIsCancelledWithTheRoundItIsIn(t *testing.T) {
	echo := &echoTool{name: "echo", block: make(chan struct{})}
	f := newFixture(t, []Tool{echo, askTool{}}, script(t, fake.Script{Model: "m", FinishReason: "tool_calls", Steps: []fake.Step{
		{Kind: fake.StepToolCall, ToolCallID: "q1", ToolName: ToolAskUser, Chunks: []string{askArgs}},
		{Kind: fake.StepToolCall, ToolCallID: "e1", ToolName: "echo", Chunks: []string{`{}`}},
	}}))
	run := f.admit("go")
	f.runner.Start(run.ID)
	deadline := time.Now().Add(3 * time.Second)
	for echo.calls.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	f.runner.Cancel(run.ID)
	f.waitFor(run.ID, sqlc.RunStatusCancelled)
	f.assertTranscript("user go", "assistant:tool_calls call:ask_user call:echo",
		"tool result:cancelled", "tool result:cancelled", "assistant:cancelled")
}

// The last round still forbids calling, the question tool included: it is on
// the list (the provider's cache is keyed on it) but a call is refused.
func TestAskUserOnTheLastRoundIsRefused(t *testing.T) {
	echo := &echoTool{name: "echo", replay: true}
	f := newFixture(t, []Tool{echo, askTool{}},
		toolCall(t, "c1", "echo", `{}`),
		toolCall(t, "q1", ToolAskUser, askArgs))
	runner := NewRunner(f.pool, f.runner.hub, f.models, func(context.Context) []Tool { return []Tool{echo, askTool{}} },
		Options{PartialInterval: 5 * time.Millisecond, MaxToolRounds: 1})
	t.Cleanup(func() { _ = runner.Shutdown(context.Background()) })

	run := f.admit("go")
	runner.Start(run.ID)
	f.waitFor(run.ID, sqlc.RunStatusCompleted)
	last := f.models.reqs[len(f.models.reqs)-1]
	if last.Options.ToolChoice != llm.ToolChoiceNone || len(last.Tools) != 2 {
		t.Fatalf("last request: tool choice %q with %d tools", last.Options.ToolChoice, len(last.Tools))
	}
	f.assertTranscript("user go", "assistant:tool_calls call:echo", "tool result:succeeded",
		"assistant:tool_calls call:ask_user", "tool result:failed")
}
