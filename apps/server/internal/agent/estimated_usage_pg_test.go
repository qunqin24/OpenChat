package agent

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/qunqin24/openchat/apps/server/internal/db/sqlc"
	"github.com/qunqin24/openchat/apps/server/internal/llm"
	"github.com/qunqin24/openchat/apps/server/internal/providers/fake"
)

// A request that ends without the provider's usage - stopped, or broken - still
// cost what it cost, and the ledger says so with an estimate. These are the
// cases of that rule, end to end through a run.

// stopRun cancels a run the way the Stop button does.
func (f *fixture) stopRun(run sqlc.Run) {
	f.t.Helper()
	if _, err := f.q.RequestRunCancel(context.Background(), sqlc.RequestRunCancelParams{ID: run.ID, ConversationID: f.conv}); err != nil {
		f.t.Fatal(err)
	}
	f.runner.Cancel(run.ID)
	f.waitFor(run.ID, sqlc.RunStatusCancelled)
}

func (f *fixture) waitForModelCall() {
	f.t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for f.models.calls.Load() == 0 {
		if time.Now().After(deadline) {
			f.t.Fatal("the model was never asked")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func waitForPartial(t *testing.T, f *fixture, run sqlc.Run) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		r, _ := f.q.GetRun(context.Background(), run.ID)
		if live, _ := decodeLive(r.Live); live.Generation != nil && len(live.Generation.Parts) > 0 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("no partial was committed")
}

// A model whose usage frame never comes - the user stopped it first - was still
// paid for. The row has what was sent and what streamed, estimated, and says so.
func TestStoppedRunIsRecordedAsEstimated(t *testing.T) {
	f := newFixture(t, nil, script(t, fake.Script{Model: "m", FinishReason: "stop", Steps: []fake.Step{
		{Kind: fake.StepText, Chunks: []string{strings.Repeat("streamed words ", 40)}},
		{Kind: fake.StepText, Chunks: []string{"never"}, DelayMS: 5000},
	}}))
	run := f.admit("a question")
	f.runner.Start(run.ID)
	waitForPartial(t, f, run)
	f.stopRun(run)

	rows := f.ledger(run.ID)
	if len(rows) != 1 {
		t.Fatalf("%d rows, want 1: %+v", len(rows), rows)
	}
	r := rows[0]
	if r.status != "ok" || !r.estimated {
		t.Errorf("status %s, estimated %v; a stop is ok and its tokens are an estimate", r.status, r.estimated)
	}
	// 600 characters of text is 150 tokens, widened by a quarter; and the input
	// is the request that went out, which holds at least the question.
	if r.output < 150 || r.output > 400 || r.input <= 0 {
		t.Errorf("tokens in/out = %d/%d", r.input, r.output)
	}
}

// Stopped before the model said anything at all, the request was still sent:
// its input is owed, and there is no output to estimate.
func TestRunStoppedBeforeAnyOutputIsStillBilledItsInput(t *testing.T) {
	f := newFixture(t, nil, hang(t))
	run := f.admit(strings.Repeat("a long question ", 200))
	f.runner.Start(run.ID)
	f.waitForModelCall()
	f.stopRun(run)

	rows := f.ledger(run.ID)
	if len(rows) != 1 || !rows[0].estimated || rows[0].input < 800 || rows[0].output != 0 {
		t.Fatalf("rows = %+v", rows)
	}
}

// partialClient streams a word, then waits to be stopped and hands back
// whatever usage it was given: Anthropic's message_start knows the input and
// nothing else.
type partialClient struct{ usage *llm.Usage }

func (p partialClient) Stream(ctx context.Context, _ llm.Request, emit func(llm.Event) error) (llm.ModelResponse, error) {
	_ = emit(llm.ResponseStartEvent())
	_ = emit(llm.PartStartEvent(0, llm.PartText))
	_ = emit(llm.DeltaEvent(0, llm.PartText, "some words that were written"))
	<-ctx.Done()
	resp := llm.ModelResponse{Model: "m", FinishReason: llm.FinishCancelled, Usage: p.usage,
		Message: llm.Message{Role: llm.RoleAssistant, Parts: []llm.Part{llm.TextPart{Text: strings.Repeat("some words ", 50)}}}}
	return resp, ctx.Err()
}

func stoppedWith(t *testing.T, usage *llm.Usage) []ledgerRow {
	t.Helper()
	f := newFixture(t, nil, func() (llm.ModelClient, error) { return partialClient{usage: usage}, nil })
	run := f.admit("hi")
	f.runner.Start(run.ID)
	f.waitForModelCall()
	time.Sleep(50 * time.Millisecond)
	f.stopRun(run)
	return f.ledger(run.ID)
}

// What the provider reported wins, and is filled in only where it said nothing:
// an input-only report keeps its input and gains an estimated output.
func TestReportedUsageIsPreferredOverTheEstimate(t *testing.T) {
	rows := stoppedWith(t, &llm.Usage{InputTokens: 777, CacheReadTokens: 5})
	if len(rows) != 1 {
		t.Fatalf("rows = %+v", rows)
	}
	r := rows[0]
	if r.input != 777 || r.cacheRead != 5 {
		t.Errorf("the provider's input was replaced: %d (+%d cached)", r.input, r.cacheRead)
	}
	if r.output <= 0 || !r.estimated {
		t.Errorf("output %d, estimated %v: the missing output should be estimated", r.output, r.estimated)
	}
}

// A complete report is final, even on a stopped request, and nothing is estimated.
func TestCompleteReportedUsageIsNeverEstimated(t *testing.T) {
	rows := stoppedWith(t, &llm.Usage{InputTokens: 700, OutputTokens: 12})
	if len(rows) != 1 || rows[0].input != 700 || rows[0].output != 12 || rows[0].estimated {
		t.Fatalf("rows = %+v", rows)
	}
}

// An answered request with usage is exactly what the provider said, and so is
// one the provider refused before taking it up: nothing was spent, and nothing
// is estimated.
func TestAnsweredAndRefusedRequestsAreNotEstimated(t *testing.T) {
	f := newFixture(t, nil, script(t, fake.Script{Model: "m", FinishReason: "stop",
		Usage: &fake.Usage{InputTokens: 120, OutputTokens: 40},
		Steps: []fake.Step{{Kind: fake.StepText, Chunks: []string{"Hello"}}}}))
	run := f.admit("hi")
	f.runner.Start(run.ID)
	f.waitFor(run.ID, sqlc.RunStatusCompleted)
	if rows := f.ledger(run.ID); len(rows) != 1 || rows[0].estimated || rows[0].input != 120 {
		t.Errorf("answered: %+v", rows)
	}

	refused := newFixture(t, nil, func() (llm.ModelClient, error) {
		return nil, &llm.Error{Kind: llm.ErrorAuth, Code: "invalid_api_key", Message: "bad key"}
	})
	run = refused.admit("hi")
	refused.runner.Start(run.ID)
	refused.waitFor(run.ID, sqlc.RunStatusFailed)
	if rows := refused.ledger(run.ID); len(rows) != 1 || rows[0].estimated || rows[0].input != 0 || rows[0].status != "error" {
		t.Errorf("refused: %+v", rows)
	}
}

// A stream that broke after it began was taken up and billed: its partial text
// is estimated. (One that broke at once is the refused case above.)
func TestBrokenStreamIsEstimated(t *testing.T) {
	f := newFixtureWith(t, func(o *Options) { o.MaxRetries = -1 }, nil, script(t, fake.Script{Model: "m", FinishReason: "stop",
		Steps:   []fake.Step{{Kind: fake.StepText, Chunks: []string{strings.Repeat("words ", 100)}}},
		Failure: &fake.Failure{AfterStep: 1, Kind: string(llm.ErrorTransport), Message: "connection reset"}}))
	run := f.admit("hi")
	f.runner.Start(run.ID)
	f.waitFor(run.ID, sqlc.RunStatusFailed)
	rows := f.ledger(run.ID)
	if len(rows) != 1 || rows[0].status != "error" || !rows[0].estimated || rows[0].output < 100 || rows[0].input <= 0 {
		t.Fatalf("rows = %+v", rows)
	}
}

// A provider that never reports usage at all (some gateways) is no free pass
// either: a request that was answered without it is estimated.
func TestAnswerWithoutUsageIsEstimated(t *testing.T) {
	f := newFixture(t, nil, answer(t, strings.Repeat("a reply ", 100)))
	run := f.admit("hi")
	f.runner.Start(run.ID)
	f.waitFor(run.ID, sqlc.RunStatusCompleted)
	rows := f.ledger(run.ID)
	if len(rows) != 1 || !rows[0].estimated || rows[0].output < 100 || rows[0].input <= 0 {
		t.Fatalf("rows = %+v", rows)
	}
}
