// Package agent runs conversations: from a user's input, through every model
// request and tool call, to the final answer.
//
// The design follows pi-durable (github.com/earendil-works/pi, packages/durable),
// translated to Go and Postgres:
//
//   - Only committed state is observable. Progress is written to the database
//     first and published to subscribers after the commit, so a reload, a
//     second tab and a restarted process all see the same thing.
//   - Transcript rows are immutable. The response still streaming lives in the
//     run's live state, committed on a short throttle, and becomes a row once.
//   - A run resumes after a restart from its checkpoint. A tool call records its
//     intent before it runs; a call found mid-flight is run again only if the
//     tool is a pure read, and is otherwise reported to the model as interrupted.
//   - Subscribers get a snapshot and then deltas. A slow one is resynchronised
//     with a fresh snapshot rather than promised every intermediate step.
package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/qunqin24/openchat/apps/server/internal/db/sqlc"
	"github.com/qunqin24/openchat/apps/server/internal/llm"
	ledger "github.com/qunqin24/openchat/apps/server/internal/usage"
)

// ErrNoModel is a run whose pinned model can no longer be answered: the channel
// was deleted or disabled, or stopped offering the model.
var ErrNoModel = errors.New("agent: the model is no longer available")

// ModelResolver builds the client for a run's pinned channel and model.
type ModelResolver interface {
	Client(ctx context.Context, channelID *uuid.UUID, modelID string) (llm.ModelClient, error)
}

// ChannelFailover is implemented by a resolver that can move a run to another
// channel offering its model: the next in the serving order (priority, then
// weight) that the run has not tried, or nil when none is left.
type ChannelFailover interface {
	NextChannel(ctx context.Context, modelID string, tried []uuid.UUID) (*uuid.UUID, error)
}

// ToolCapable is implemented by a client that knows whether its model may be
// offered tools. A client that does not implement it is offered them.
type ToolCapable interface {
	CallsTools() bool
}

// ToolSource lists the tools a run may use right now. Asked once per request,
// so turning a tool on or off reaches runs already in progress at their next
// request.
type ToolSource func(ctx context.Context) []Tool

// Options are the run policy. Zero values take the defaults below.
type Options struct {
	// SystemPrompt renders the system prompt for a request made at now.
	SystemPrompt func(ctx context.Context, now time.Time, tools []Tool) string
	// MaxToolRounds bounds the tool rounds of one run. The request after the
	// last round forbids tool calls (tool choice none, same tool list) and
	// tells the model so, and it has to answer. Generous by default: a search
	// that needs a dozen pages read should not be cut short, and the bound is
	// there for a model stuck in a loop.
	MaxToolRounds int
	// MaxRetries is how many times a failed request is retried.
	MaxRetries int
	// PartialInterval is how often a streaming response is committed. A crash
	// loses at most this much of it.
	PartialInterval time.Duration
	// HistoryLimit is how many transcript rows a request is built from: a
	// safety cap and no more. What keeps a request within the model's window is
	// compression (Compression); a cap that bit would move the start of the
	// history with every new row, and with it the prefix a provider caches.
	HistoryLimit int32
	// Compression, when set, compresses a request that nears the model's
	// window before it is made. Nil never compresses on its own.
	Compression *Compression
	// ToolConcurrency bounds the calls of one round running at once.
	ToolConcurrency int
	// RequestOptions are passed to every model request.
	RequestOptions llm.Options
	// Blobs reads the files users attached. Without it a request goes out
	// with a note where each file would be.
	Blobs Blobs
	// Modalities lists what the model can be sent besides text. Without it
	// every kind of file is sent.
	Modalities func(ctx context.Context, modelID string) ([]string, error)
	// Capacity says whether modelID has reached its spending cap for the user
	// whose run this is (an administrator the cap exempts is let through, which
	// is the hook's to decide). Asked before every model request of a run; a
	// run that is refused ends with CodeModelCapacity. Without it there are no
	// caps.
	Capacity func(ctx context.Context, userID uuid.UUID, modelID string) (bool, error)
	// EndUser is the identifier of the person a run is for, put on every model
	// request the run makes (llm.Request.User). Without it requests carry none.
	EndUser func(ctx context.Context, run sqlc.Run) string
	// Reserver holds money back for the requests of capped models while they
	// are out (see api.SpendReserver), so the ledger, which is written when a
	// request ends, is not the only thing a request is checked against. Without
	// it a request is checked against the ledger alone. It serves every path
	// that records in the ledger: the run's requests, its compressions, and the
	// titles the server makes (api.NewServer takes the
	// runner's Recorder).
	Reserver ledger.Reserver
	// Tracer says whose requests are traced - what each carried and what came
	// back - for the 轨迹 view (api.NewTracer: administrators). Without it
	// nothing is.
	Tracer ledger.Tracer
}

func (o Options) withDefaults() Options {
	if o.SystemPrompt == nil {
		o.SystemPrompt = func(context.Context, time.Time, []Tool) string { return "" }
	}
	if o.MaxToolRounds <= 0 {
		o.MaxToolRounds = 100
	}
	if o.MaxRetries < 0 {
		o.MaxRetries = 0
	} else if o.MaxRetries == 0 {
		o.MaxRetries = 3
	}
	if o.PartialInterval <= 0 {
		o.PartialInterval = 150 * time.Millisecond
	}
	if o.HistoryLimit <= 0 {
		o.HistoryLimit = 2000
	}
	if o.ToolConcurrency <= 0 {
		o.ToolConcurrency = 4
	}
	return o
}

// Causes a run's context can end with. errShutdown is the process going away:
// it is not a decision about the run, so nothing terminal is written and the
// next process resumes it. errCancelled is the user pressing stop.
var (
	errShutdown  = errors.New("agent: shutting down")
	errCancelled = errors.New("agent: run cancelled")
)

// Runner executes runs, one goroutine each.
type Runner struct {
	pool   *pgxpool.Pool
	q      *sqlc.Queries
	hub    *Hub
	models ModelResolver
	tools  ToolSource
	opts   Options
	now    func() time.Time
	// usage is the ledger of model requests.
	usage *ledger.Recorder
	// compactor writes the summaries of long conversations.
	compactor *Compactor

	base context.Context
	stop context.CancelCauseFunc

	mu     sync.Mutex
	active map[uuid.UUID]context.CancelCauseFunc
	wg     sync.WaitGroup

	// afterRun is told about every run that finishes, once its end is
	// committed and published. Atomic because runs resumed at boot can finish
	// before the caller has set it.
	afterRun atomic.Pointer[func(sqlc.Run)]
	// moreTools are offered after the ToolSource's (AddTools).
	moreTools atomic.Pointer[ToolSource]
	// turnNote is told to each run's model with its input (SetTurnNote).
	turnNote atomic.Pointer[TurnNote]
	// subagents delivers what arrives for a run between its requests, and
	// records a sub-agent's report as its run ends (SetSubagents).
	subagents atomic.Pointer[Subagents]
}

// Subagents is the runner's side of sub-agents (api/subagents.go): what is
// written into a run's transcript between its requests, and what a sub-agent's
// run leaves behind when it ends. Both are called inside one of the run's own
// commits, with its queries, so what they write is the run's step and is
// published with it.
type Subagents interface {
	// Waiting says, cheaply and outside any transaction, whether anything
	// waits for the run: a report for the conversation it answers, a message
	// for the sub-agent it is.
	Waiting(ctx context.Context, run sqlc.Run, parent *uuid.UUID) (bool, error)
	// Take writes what waits for the run into its transcript and returns the
	// rows written. parent is set on a sub-agent's run.
	Take(ctx context.Context, q *sqlc.Queries, run sqlc.Run, parent *uuid.UUID) ([]sqlc.Message, error)
	// Ended records the report of a sub-agent's run that has just ended
	// (run is its terminal row), in the transaction that ends it.
	Ended(ctx context.Context, q *sqlc.Queries, run sqlc.Run, parent uuid.UUID) error
}

// SetSubagents hands the runner its side of sub-agents; like AddTools, it acts
// through the API server, which is made after the runner.
func (r *Runner) SetSubagents(s Subagents) {
	r.subagents.Store(&s)
}

// TurnNote is what a run's model should know that its transcript does not
// say - a document the person edited since the model last wrote it. It is
// asked once per run, at its first request, and added to the run's input
// message on every request of the run; it is never stored. Empty says nothing.
type TurnNote func(ctx context.Context) (string, error)

// AfterRun registers what to do once a run has finished — titling its
// conversation, say. It is called on the run's goroutine and must not block.
func (r *Runner) AfterRun(fn func(sqlc.Run)) {
	r.afterRun.Store(&fn)
}

// AddTools offers every run more tools, after the ToolSource's: the ones that
// act through the API server - scheduling a task - which is made after the
// runner. A run resumed before they are added goes without them that request.
func (r *Runner) AddTools(more ToolSource) {
	r.moreTools.Store(&more)
}

// SetTurnNote has every run's model told what note says, from the next run
// on: like AddTools, it acts through the API server, which is made after the
// runner.
func (r *Runner) SetTurnNote(note TurnNote) {
	r.turnNote.Store(&note)
}

// offered is every tool a request may use.
func (r *Runner) offered(ctx context.Context) []Tool {
	tools := r.tools(ctx)
	if more := r.moreTools.Load(); more != nil {
		tools = append(tools, (*more)(ctx)...)
	}
	return tools
}

func NewRunner(pool *pgxpool.Pool, hub *Hub, models ModelResolver, tools ToolSource, opts Options) *Runner {
	base, stop := context.WithCancelCause(context.Background())
	if tools == nil {
		tools = func(context.Context) []Tool { return nil }
	}
	recorder := ledger.NewRecorder(sqlc.New(pool))
	recorder.SetReserver(opts.Reserver)
	recorder.SetTracer(opts.Tracer)
	return &Runner{
		pool:      pool,
		q:         sqlc.New(pool),
		hub:       hub,
		models:    models,
		tools:     tools,
		opts:      opts.withDefaults(),
		now:       time.Now,
		usage:     recorder,
		compactor: NewCompactor(pool, recorder),
		base:      base,
		stop:      stop,
		active:    map[uuid.UUID]context.CancelCauseFunc{},
	}
}

// Recorder is the usage ledger the runner writes, which the server's own model
// requests share so that they are held back and counted in one place.
func (r *Runner) Recorder() *ledger.Recorder { return r.usage }

// Hub is where this runner publishes.
func (r *Runner) Hub() *Hub { return r.hub }

// Compactor is the runner's summary writer, for the compressions that are not
// part of a run: the ones a person asks for.
func (r *Runner) Compactor() *Compactor { return r.compactor }

// Start executes a run in the background. Starting one that is already running
// here is a no-op.
func (r *Runner) Start(runID uuid.UUID) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.active[runID]; ok {
		return
	}
	if r.base.Err() != nil {
		return // shutting down; the next process picks it up
	}
	ctx, cancel := context.WithCancelCause(r.base)
	r.active[runID] = cancel
	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		defer func() {
			r.mu.Lock()
			delete(r.active, runID)
			r.mu.Unlock()
			cancel(nil)
		}()
		r.execute(ctx, runID)
	}()
}

// Resume starts every run a previous process left unfinished.
func (r *Runner) Resume(ctx context.Context) error {
	runs, err := r.q.ListRunningRuns(ctx)
	if err != nil {
		return err
	}
	for _, run := range runs {
		slog.InfoContext(ctx, "resuming run", "run_id", run.ID, "conversation_id", run.ConversationID)
		r.Start(run.ID)
	}
	return nil
}

// Cancel stops a run. The caller has already recorded the request in the
// database, so a run that is not executing here — between processes, say —
// sees it when it next starts; Start is called for exactly that case.
func (r *Runner) Cancel(runID uuid.UUID) {
	r.mu.Lock()
	cancel, ok := r.active[runID]
	r.mu.Unlock()
	if ok {
		cancel(errCancelled)
		return
	}
	r.Start(runID)
}

// Shutdown stops every run without finishing it, and waits for the goroutines.
// The runs stay `running` and resume in the next process.
func (r *Runner) Shutdown(ctx context.Context) error {
	r.stop(errShutdown)
	done := make(chan struct{})
	go func() {
		r.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// execute drives one run through its phases until it is terminal or the
// process stops it.
func (r *Runner) execute(ctx context.Context, runID uuid.UUID) {
	ex, err := r.load(runID)
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			slog.Error("load run", "run_id", runID, "error", err)
		}
		return
	}
	if ex == nil {
		return // already terminal
	}
	ctx = WithScope(ctx, Scope{RunID: runID, UserID: ex.run.UserID, ConversationID: ex.run.ConversationID, Parent: ex.parent, State: &RunState{}})
	// A panic in a provider adapter, the summariser or anything else on this
	// goroutine would otherwise take the whole process down — and, the run
	// still `running`, take it down again on every restart that resumes it.
	// It ends this run as failed instead.
	defer func() {
		if p := recover(); p != nil {
			ex.logger().Error("run panicked", "panic", p, "stack", string(debug.Stack()))
			ex.fail(fmt.Errorf("panic: %v", p))
		}
	}()

	for {
		if errors.Is(context.Cause(ctx), errShutdown) {
			return
		}
		if ctx.Err() != nil || ex.run.CancelRequested {
			ex.fail(ex.finishCancelled())
			return
		}

		var done bool
		switch ex.cp.Phase {
		case PhaseTools:
			done, err = ex.runTools(ctx)
		default:
			done, err = ex.generate(ctx)
		}
		if err != nil {
			if errors.Is(context.Cause(ctx), errShutdown) {
				return
			}
			// A stop that lands between two steps fails whatever database call
			// or lookup comes next with "context canceled"; that is the stop,
			// not a fault, and the run ends cancelled like any other.
			if ctx.Err() != nil {
				ex.fail(ex.finishCancelled())
				return
			}
			ex.fail(err)
			return
		}
		if done {
			return
		}
	}
}

func (r *Runner) load(runID uuid.UUID) (*execution, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	run, err := r.q.GetRun(ctx, runID)
	if err != nil {
		return nil, err
	}
	if run.Status != sqlc.RunStatusRunning {
		return nil, nil
	}
	cp, err := decodeCheckpoint(run.Checkpoint)
	if err != nil {
		return nil, err
	}
	live, err := decodeLive(run.Live)
	if err != nil {
		return nil, err
	}
	conv, err := r.q.GetConversationKind(ctx, run.ConversationID)
	if err != nil {
		return nil, err
	}
	ex := &execution{r: r, run: run, cp: cp, live: live}
	if conv.Kind == "subagent" {
		ex.parent = conv.ParentID
	}
	return ex, nil
}

// ShuttingDown reports whether ctx, a run's or one of its tool calls', ended
// because the process is going away rather than because the run was stopped:
// a tool that waits on something should then leave it be, since the run and
// what it waits on both resume in the next process.
func ShuttingDown(ctx context.Context) bool {
	return errors.Is(context.Cause(ctx), errShutdown)
}
