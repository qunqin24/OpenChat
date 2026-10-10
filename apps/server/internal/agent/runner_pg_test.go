package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/qunqin/openchat/apps/server/internal/db/dbtest"
	"github.com/qunqin/openchat/apps/server/internal/db/sqlc"
	"github.com/qunqin/openchat/apps/server/internal/llm"
	"github.com/qunqin/openchat/apps/server/internal/providers/fake"
)

// These tests need a Postgres server and skip without one:
//
//	TEST_DATABASE_URL=postgres://... go test ./internal/agent/
//
// They run in a database of their own, <name in the URL>_agent, so they never
// meet another package's rows. Each test creates its own user and conversation,
// and nothing is cleaned up between runs.
func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx := context.Background()
	pool := dbtest.Pool(t, "agent")
	// Resume picks up every running run in the database, so one left behind by
	// another test — or a previous, interrupted `go test` — would be executed
	// against this test's scripted model.
	if _, err := pool.Exec(ctx, settleRunning); err != nil {
		t.Fatalf("settle leftover runs: %v", err)
	}
	return pool
}

const settleRunning = `UPDATE runs SET status = 'cancelled', finished_at = now() WHERE status = 'running'`

// scripted answers each request with the next script, in order.
type scripted struct {
	mu      sync.Mutex
	steps   []func() (llm.ModelClient, error)
	calls   atomic.Int32
	lastReq llm.Request
	reqs    []llm.Request
}

func (s *scripted) Client(context.Context, *uuid.UUID, string) (llm.ModelClient, error) {
	return s, nil
}

func (s *scripted) Stream(ctx context.Context, req llm.Request, emit func(llm.Event) error) (llm.ModelResponse, error) {
	s.mu.Lock()
	s.lastReq = req
	s.reqs = append(s.reqs, req)
	if len(s.steps) == 0 {
		s.mu.Unlock()
		return llm.ModelResponse{}, errors.New("scripted: no more responses")
	}
	next := s.steps[0]
	s.steps = s.steps[1:]
	s.mu.Unlock()
	s.calls.Add(1)

	client, err := next()
	if err != nil {
		return llm.ModelResponse{}, err
	}
	return client.Stream(ctx, req, emit)
}

func script(t *testing.T, sc fake.Script) func() (llm.ModelClient, error) {
	t.Helper()
	p, err := fake.New(sc)
	if err != nil {
		t.Fatal(err)
	}
	return func() (llm.ModelClient, error) { return p, nil }
}

func answer(t *testing.T, chunks ...string) func() (llm.ModelClient, error) {
	return script(t, fake.Script{Model: "m", FinishReason: "stop",
		Steps: []fake.Step{{Kind: fake.StepText, Chunks: chunks}}})
}

func toolCall(t *testing.T, id, name, args string) func() (llm.ModelClient, error) {
	return script(t, fake.Script{Model: "m", FinishReason: "tool_calls",
		Steps: []fake.Step{{Kind: fake.StepToolCall, ToolCallID: id, ToolName: name, Chunks: []string{args}}}})
}

// hang is a model that has not said a word by the time it is stopped.
func hang(t *testing.T) func() (llm.ModelClient, error) {
	t.Helper()
	return func() (llm.ModelClient, error) { return hangingClient{}, nil }
}

type hangingClient struct{}

func (hangingClient) Stream(ctx context.Context, _ llm.Request, _ func(llm.Event) error) (llm.ModelResponse, error) {
	<-ctx.Done()
	return llm.ModelResponse{}, ctx.Err()
}

// echoTool returns its arguments, counting calls.
type echoTool struct {
	name   string
	replay bool
	calls  atomic.Int32
	block  chan struct{} // when set, Execute waits on it or the context
}

func (e *echoTool) Definition() llm.Tool {
	return llm.Tool{Name: e.name, Description: "echo", Parameters: json.RawMessage(`{"type":"object"}`)}
}
func (e *echoTool) Replayable() bool { return e.replay }
func (e *echoTool) Execute(ctx context.Context, args json.RawMessage) (ToolResult, error) {
	e.calls.Add(1)
	if e.block != nil {
		select {
		case <-e.block:
		case <-ctx.Done():
			return ToolResult{}, ctx.Err()
		}
	}
	return ToolResult{Content: "echo " + string(args), Details: map[string]string{"seen": string(args)}}, nil
}

type fixture struct {
	t      *testing.T
	pool   *pgxpool.Pool
	q      *sqlc.Queries
	user   uuid.UUID
	conv   uuid.UUID
	runner *Runner
	models *scripted
	sub    *Subscription
}

func newFixture(t *testing.T, tools []Tool, steps ...func() (llm.ModelClient, error)) *fixture {
	t.Helper()
	pool := testPool(t)
	ctx := context.Background()
	f := &fixture{t: t, pool: pool, q: sqlc.New(pool), models: &scripted{steps: steps}}

	tag := uuid.NewString()
	if err := pool.QueryRow(ctx, `INSERT INTO users (email, name) VALUES ($1, $2) RETURNING id`,
		tag+"@test.invalid", "agent-"+tag).Scan(&f.user); err != nil {
		t.Fatal(err)
	}
	conv, err := f.q.CreateConversation(ctx, sqlc.CreateConversationParams{UserID: &f.user, Title: "t"})
	if err != nil {
		t.Fatal(err)
	}
	f.conv = conv.ID

	// Registered first so it runs last, after the runners have stopped.
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), settleRunning)
	})

	hub := NewHub()
	f.sub = hub.Subscribe(f.conv)
	t.Cleanup(f.sub.Close)
	f.runner = NewRunner(pool, hub, f.models, func(context.Context) []Tool { return tools },
		Options{PartialInterval: 5 * time.Millisecond, MaxRetries: 2})
	t.Cleanup(func() { _ = f.runner.Shutdown(context.Background()) })
	return f
}

func (f *fixture) newRunner(tools []Tool) *Runner {
	return NewRunner(f.pool, f.runner.hub, f.models, func(context.Context) []Tool { return tools },
		Options{PartialInterval: 5 * time.Millisecond, MaxRetries: 2})
}

// admit does what the HTTP handler does: the input row and the run in one go.
func (f *fixture) admit(text string) sqlc.Run {
	f.t.Helper()
	ctx := context.Background()
	input := f.appendInput(text)
	run, err := f.q.CreateRun(ctx, sqlc.CreateRunParams{
		ConversationID: f.conv, UserID: f.user, InputMessageID: input.ID, ModelID: "m", WebSearch: true,
	})
	if err != nil {
		f.t.Fatal(err)
	}
	return run
}

// appendInput writes a user message after the conversation's current leaf and
// becomes the new one, as the handlers do.
func (f *fixture) appendInput(text string) sqlc.Message {
	f.t.Helper()
	ctx := context.Background()
	var parent *uuid.UUID
	if leaf, err := f.q.GetCurrentInput(ctx, f.conv); err == nil {
		parent = &leaf.ID
	}
	input, err := f.q.AppendEntry(ctx, sqlc.AppendEntryParams{
		ConversationID: f.conv,
		Role:           sqlc.MessageRoleUser,
		Content:        text,
		Parts:          EncodeParts([]llm.Part{llm.TextPart{Text: text}}),
		Status:         sqlc.MessageStatusComplete,
		ParentInputID:  parent,
	})
	if err != nil {
		f.t.Fatal(err)
	}
	if err := f.q.SetCurrentInput(ctx, sqlc.SetCurrentInputParams{ConversationID: f.conv, InputID: &input.ID}); err != nil {
		f.t.Fatal(err)
	}
	return input
}

func (f *fixture) waitFor(runID uuid.UUID, status sqlc.RunStatus) sqlc.Run {
	f.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		run, err := f.q.GetRun(context.Background(), runID)
		if err != nil {
			f.t.Fatal(err)
		}
		if run.Status == status {
			return run
		}
		time.Sleep(10 * time.Millisecond)
	}
	run, _ := f.q.GetRun(context.Background(), runID)
	f.t.Fatalf("run did not reach %s; it is %s (error %v)", status, run.Status, run.ErrorCode)
	return run
}

// transcript is each row as role:stop:text-or-part-summary.
func (f *fixture) transcript() []string {
	f.t.Helper()
	rows, err := f.q.ListRecentMessages(context.Background(), sqlc.ListRecentMessagesParams{ConversationID: f.conv, RowLimit: 100})
	if err != nil {
		f.t.Fatal(err)
	}
	var out []string
	for _, r := range rows {
		parts, err := DecodeParts(r.Parts)
		if err != nil {
			f.t.Fatal(err)
		}
		s := string(r.Role)
		if r.StopReason != nil {
			s += ":" + *r.StopReason
		}
		for _, p := range parts {
			switch p := p.(type) {
			case llm.TextPart:
				s += " " + p.Text
			case llm.ToolCallPart:
				s += " call:" + p.Name
			case llm.ToolResultPart:
				s += " result:" + string(p.Status)
			}
		}
		out = append(out, s)
	}
	return out
}

func (f *fixture) assertTranscript(want ...string) {
	f.t.Helper()
	got := f.transcript()
	if len(got) != len(want) {
		f.t.Fatalf("transcript: got %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			f.t.Fatalf("row %d: got %q, want %q\nall: %q", i, got[i], want[i], got)
		}
	}
}

// events drains what the subscriber has received so far.
func (f *fixture) events() []Batch {
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	var all []Batch
	for {
		batches, _, err := f.sub.Next(ctx)
		if err != nil {
			return all
		}
		all = append(all, batches...)
	}
}

func TestRunAnswers(t *testing.T) {
	f := newFixture(t, nil, answer(t, "Hel", "lo"))
	run := f.admit("hi")
	f.runner.Start(run.ID)
	f.waitFor(run.ID, sqlc.RunStatusCompleted)

	f.assertTranscript("user hi", "assistant:stop Hello")

	// Every batch is newer than the last, and the stream ends with the run.
	var last int64
	var finished bool
	for _, b := range f.events() {
		if b.Version <= last {
			t.Fatalf("version went from %d to %d", last, b.Version)
		}
		last = b.Version
		for _, e := range b.Events {
			if _, ok := e.(RunFinished); ok {
				finished = true
			}
		}
	}
	if !finished {
		t.Fatal("no RunFinished event")
	}
}

func TestRunToolRound(t *testing.T) {
	echo := &echoTool{name: "echo", replay: true}
	f := newFixture(t, []Tool{echo},
		toolCall(t, "c1", "echo", `{"q":1}`),
		answer(t, "done"))
	run := f.admit("use the tool")
	f.runner.Start(run.ID)
	f.waitFor(run.ID, sqlc.RunStatusCompleted)

	f.assertTranscript(
		"user use the tool",
		"assistant:tool_calls call:echo",
		"tool result:succeeded",
		"assistant:stop done")
	if echo.calls.Load() != 1 {
		t.Fatalf("tool ran %d times", echo.calls.Load())
	}
	// The second request carried the call and its result.
	req := f.models.lastReq
	if n := len(req.Messages); n < 3 || req.Messages[n-1].Role != llm.RoleTool {
		t.Fatalf("second request did not end with the tool result: %+v", req.Messages)
	}
}

func TestRunUnknownToolIsAnsweredNotExecuted(t *testing.T) {
	echo := &echoTool{name: "echo"}
	f := newFixture(t, []Tool{echo},
		toolCall(t, "c1", "missing", `{}`),
		answer(t, "ok"))
	run := f.admit("go")
	f.runner.Start(run.ID)
	f.waitFor(run.ID, sqlc.RunStatusCompleted)
	f.assertTranscript("user go", "assistant:tool_calls call:missing", "tool result:failed", "assistant:stop ok")
}

func TestRunTruncatedCallsAreNotExecuted(t *testing.T) {
	echo := &echoTool{name: "echo"}
	f := newFixture(t, []Tool{echo},
		script(t, fake.Script{Model: "m", FinishReason: "length",
			Steps: []fake.Step{{Kind: fake.StepToolCall, ToolCallID: "c1", ToolName: "echo", Chunks: []string{`{"q":1}`}}}}),
		answer(t, "ok"))
	run := f.admit("go")
	f.runner.Start(run.ID)
	f.waitFor(run.ID, sqlc.RunStatusCompleted)
	if echo.calls.Load() != 0 {
		t.Fatal("a call from a truncated response was executed")
	}
	f.assertTranscript("user go", "assistant:length call:echo", "tool result:failed", "assistant:stop ok")
}

func TestRunRetriesTransportErrors(t *testing.T) {
	f := newFixture(t, nil,
		func() (llm.ModelClient, error) {
			return nil, &llm.Error{Kind: llm.ErrorTransport, Message: "connection reset"}
		},
		answer(t, "recovered"))
	run := f.admit("hi")
	f.runner.Start(run.ID)
	f.waitFor(run.ID, sqlc.RunStatusCompleted)
	// The failed attempt produced nothing, so it left no row.
	f.assertTranscript("user hi", "assistant:stop recovered")
}

func TestRunFailsOnAuthErrors(t *testing.T) {
	f := newFixture(t, nil, func() (llm.ModelClient, error) {
		return nil, &llm.Error{Kind: llm.ErrorAuth, Message: "bad key"}
	})
	run := f.admit("hi")
	f.runner.Start(run.ID)
	finished := f.waitFor(run.ID, sqlc.RunStatusFailed)
	if finished.ErrorCode == nil || *finished.ErrorCode != CodeModelAuth {
		t.Fatalf("error code %v", finished.ErrorCode)
	}
	if f.models.calls.Load() != 1 {
		t.Fatalf("an auth error was retried: %d requests", f.models.calls.Load())
	}
}

func TestRunCancelMidStream(t *testing.T) {
	f := newFixture(t, nil, script(t, fake.Script{Model: "m", FinishReason: "stop", Steps: []fake.Step{
		{Kind: fake.StepText, Chunks: []string{"partial "}},
		{Kind: fake.StepText, Chunks: []string{"never"}, DelayMS: 5000},
	}}))
	run := f.admit("hi")
	f.runner.Start(run.ID)

	// Wait for the partial to be committed, then stop.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		r, _ := f.q.GetRun(context.Background(), run.ID)
		if live, _ := decodeLive(r.Live); live.Generation != nil && len(live.Generation.Parts) > 0 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if _, err := f.q.RequestRunCancel(context.Background(), sqlc.RequestRunCancelParams{ID: run.ID, ConversationID: f.conv}); err != nil {
		t.Fatal(err)
	}
	f.runner.Cancel(run.ID)
	f.waitFor(run.ID, sqlc.RunStatusCancelled)
	f.assertTranscript("user hi", "assistant:cancelled partial ")
}

func TestRunCancelDuringTool(t *testing.T) {
	echo := &echoTool{name: "echo", block: make(chan struct{})}
	f := newFixture(t, []Tool{echo}, toolCall(t, "c1", "echo", `{}`))
	run := f.admit("go")
	f.runner.Start(run.ID)

	deadline := time.Now().Add(3 * time.Second)
	for echo.calls.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	f.runner.Cancel(run.ID)
	f.waitFor(run.ID, sqlc.RunStatusCancelled)
	// The turn ends saying it was stopped: an empty cancelled reply.
	f.assertTranscript("user go", "assistant:tool_calls call:echo", "tool result:cancelled", "assistant:cancelled")
}

// Stopped before the model said anything, a run still leaves a reply — an
// empty cancelled one — so the turn shows it was stopped and can be redone.
func TestRunCancelBeforeFirstToken(t *testing.T) {
	f := newFixture(t, nil, hang(t))
	run := f.admit("go")
	f.runner.Start(run.ID)
	time.Sleep(50 * time.Millisecond)
	f.runner.Cancel(run.ID)
	f.waitFor(run.ID, sqlc.RunStatusCancelled)
	f.assertTranscript("user go", "assistant:cancelled")
}

// A process that dies while a tool runs leaves the slot `running`. The next
// one reruns a replayable tool and reports any other as interrupted.
func TestRunResumesToolRound(t *testing.T) {
	for _, replay := range []bool{true, false} {
		name := "unsafe"
		if replay {
			name = "replayable"
		}
		t.Run(name, func(t *testing.T) {
			blocking := &echoTool{name: "echo", replay: replay, block: make(chan struct{})}
			f := newFixture(t, []Tool{blocking}, toolCall(t, "c1", "echo", `{"n":1}`), answer(t, "after restart"))
			run := f.admit("go")
			f.runner.Start(run.ID)

			deadline := time.Now().Add(3 * time.Second)
			for blocking.calls.Load() == 0 && time.Now().Before(deadline) {
				time.Sleep(5 * time.Millisecond)
			}
			// The process goes away mid-call.
			if err := f.runner.Shutdown(context.Background()); err != nil {
				t.Fatal(err)
			}
			if r, _ := f.q.GetRun(context.Background(), run.ID); r.Status != sqlc.RunStatusRunning {
				t.Fatalf("shutdown finished the run: %s", r.Status)
			}

			fresh := &echoTool{name: "echo", replay: replay}
			next := f.newRunner([]Tool{fresh})
			t.Cleanup(func() { _ = next.Shutdown(context.Background()) })
			if err := next.Resume(context.Background()); err != nil {
				t.Fatal(err)
			}
			f.waitFor(run.ID, sqlc.RunStatusCompleted)

			if replay {
				if fresh.calls.Load() != 1 {
					t.Fatalf("replayable tool ran %d times after restart", fresh.calls.Load())
				}
				f.assertTranscript("user go", "assistant:tool_calls call:echo", "tool result:succeeded", "assistant:stop after restart")
			} else {
				if fresh.calls.Load() != 0 {
					t.Fatal("a non-replayable tool ran again after restart")
				}
				f.assertTranscript("user go", "assistant:tool_calls call:echo", "tool result:interrupted", "assistant:stop after restart")
			}
		})
	}
}

// A response being streamed when the process died is kept as a cancelled row,
// and the request is made again.
func TestRunResumesMidStream(t *testing.T) {
	f := newFixture(t, nil, script(t, fake.Script{Model: "m", FinishReason: "stop", Steps: []fake.Step{
		{Kind: fake.StepText, Chunks: []string{"first try "}},
		{Kind: fake.StepText, Chunks: []string{"never"}, DelayMS: 5000},
	}}), answer(t, "second try"))
	run := f.admit("hi")
	f.runner.Start(run.ID)

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		r, _ := f.q.GetRun(context.Background(), run.ID)
		if live, _ := decodeLive(r.Live); live.Generation != nil && len(live.Generation.Parts) > 0 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if err := f.runner.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}

	next := f.newRunner(nil)
	t.Cleanup(func() { _ = next.Shutdown(context.Background()) })
	if err := next.Resume(context.Background()); err != nil {
		t.Fatal(err)
	}
	f.waitFor(run.ID, sqlc.RunStatusCompleted)
	f.assertTranscript("user hi", "assistant:cancelled first try ", "assistant:stop second try")
}

func TestOneActiveRunPerConversation(t *testing.T) {
	f := newFixture(t, nil)
	f.admit("first")
	ctx := context.Background()
	input := f.appendInput("second")
	if _, err := f.q.CreateRun(ctx, sqlc.CreateRunParams{
		ConversationID: f.conv, UserID: f.user, InputMessageID: input.ID, ModelID: "m",
	}); err == nil {
		t.Fatal("a second running run was admitted")
	}
}

func TestRunPassesItsReasoningEffort(t *testing.T) {
	f := newFixture(t, nil, answer(t, "ok"))
	run := f.admit("think hard")
	high := "high"
	if _, err := f.pool.Exec(context.Background(), `UPDATE runs SET reasoning_effort = $1 WHERE id = $2`, high, run.ID); err != nil {
		t.Fatal(err)
	}
	f.runner.Start(run.ID)
	f.waitFor(run.ID, sqlc.RunStatusCompleted)
	if r := f.models.lastReq.Options.Reasoning; r == nil || r.Effort != llm.EffortHigh {
		t.Fatalf("request reasoning = %+v", r)
	}
}

// webTool is a tool that reaches the web.
type webTool struct{ echoTool }

func (*webTool) UsesWeb() bool { return true }

func TestRunWithoutWebIsOfferedNoWebTools(t *testing.T) {
	web := &webTool{echoTool{name: "web_search"}}
	local := &echoTool{name: "calc"}
	f := newFixture(t, []Tool{web, local}, answer(t, "ok"), answer(t, "ok"))

	on := f.admit("search, please")
	f.runner.Start(on.ID)
	f.waitFor(on.ID, sqlc.RunStatusCompleted)
	if n := len(f.models.lastReq.Tools); n != 2 {
		t.Fatalf("web on: %d tools offered, want both", n)
	}

	off := f.admit("no web this time")
	if _, err := f.pool.Exec(context.Background(), `UPDATE runs SET web_search = false WHERE id = $1`, off.ID); err != nil {
		t.Fatal(err)
	}
	f.runner.Start(off.ID)
	f.waitFor(off.ID, sqlc.RunStatusCompleted)
	if tools := f.models.lastReq.Tools; len(tools) != 1 || tools[0].Name != "calc" {
		t.Fatalf("web off: offered %+v, want only calc", tools)
	}
}

// The request after the last tool round forbids a tool call; it does not take
// the tools away. The tool list and the system prompt are what a provider's
// cache and a replayed thinking block are checked against, so they must be the
// ones every earlier request of the run carried.
func TestRunLastRoundKeepsTheToolsAndForbidsCalling(t *testing.T) {
	echo := &echoTool{name: "echo", replay: true}
	f := newFixture(t, []Tool{echo},
		toolCall(t, "c1", "echo", `{}`),
		answer(t, "done"))
	runner := NewRunner(f.pool, f.runner.hub, f.models, func(context.Context) []Tool { return []Tool{echo} },
		Options{PartialInterval: 5 * time.Millisecond, MaxToolRounds: 1,
			SystemPrompt: func(_ context.Context, _ time.Time, tools []Tool) string {
				return fmt.Sprintf("prompt with %d tools", len(tools))
			}})
	t.Cleanup(func() { _ = runner.Shutdown(context.Background()) })

	run := f.admit("go")
	runner.Start(run.ID)
	f.waitFor(run.ID, sqlc.RunStatusCompleted)

	if len(f.models.reqs) != 2 {
		t.Fatalf("%d requests, want 2", len(f.models.reqs))
	}
	first, last := f.models.reqs[0], f.models.reqs[1]
	if first.Options.ToolChoice != llm.ToolChoiceAuto {
		t.Errorf("first request tool choice = %q, want the default", first.Options.ToolChoice)
	}
	if last.Options.ToolChoice != llm.ToolChoiceNone {
		t.Errorf("last request tool choice = %q, want none", last.Options.ToolChoice)
	}
	if len(last.Tools) != 1 || last.Tools[0].Name != "echo" {
		t.Errorf("last request tools = %+v, want the same list as the first", last.Tools)
	}
	if first.Messages[0].Text() != last.Messages[0].Text() {
		t.Errorf("system prompt changed between rounds: %q then %q", first.Messages[0].Text(), last.Messages[0].Text())
	}
}

// A model that calls a tool on the last round regardless of the choice it was
// given is answered with a refusal, and the run ends.
func TestRunLastRoundCallIsRefused(t *testing.T) {
	echo := &echoTool{name: "echo"}
	f := newFixture(t, []Tool{echo},
		toolCall(t, "c1", "echo", `{}`),
		toolCall(t, "c2", "echo", `{}`))
	runner := NewRunner(f.pool, f.runner.hub, f.models, func(context.Context) []Tool { return []Tool{echo} },
		Options{PartialInterval: 5 * time.Millisecond, MaxToolRounds: 1})
	t.Cleanup(func() { _ = runner.Shutdown(context.Background()) })

	run := f.admit("go")
	runner.Start(run.ID)
	f.waitFor(run.ID, sqlc.RunStatusCompleted)
	if echo.calls.Load() != 1 {
		t.Fatalf("tool ran %d times, want only the first round's", echo.calls.Load())
	}
	f.assertTranscript("user go", "assistant:tool_calls call:echo", "tool result:succeeded",
		"assistant:tool_calls call:echo", "tool result:failed")
}
