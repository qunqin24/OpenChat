package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/qunqin24/openchat/apps/server/internal/db/sqlc"
	"github.com/qunqin24/openchat/apps/server/internal/llm"
	ledger "github.com/qunqin24/openchat/apps/server/internal/usage"
)

// Error codes a run or a failed response can end with. They are machine codes
// the UI localises; a provider's own error text stays in the server log.
const (
	CodeNoModel        = "no_model"
	CodeModelError     = "model_error"
	CodeModelAuth      = "model_auth"
	CodeRateLimited    = "model_rate_limited"
	CodeContextTooLong = "context_too_long"
	CodeInternal       = "internal"
	// CodeModelCapacity is a run stopped before a request because its model
	// has reached its spending cap (Options.Capacity).
	CodeModelCapacity = "model_capacity_reached"
	// CodeToolMarkup is an answer that wrote its tool calls out as text
	// (llm.HideToolMarkup), which were cut from it: it did not finish.
	CodeToolMarkup = "tool_markup"
)

// HideLeakedMarkup cuts leaked tool-call markup from an assistant row's text
// parts, dropping any it empties, for rows written before the stream hid it.
// leaked says whether there was any.
func HideLeakedMarkup(parts []llm.Part) (out []llm.Part, leaked bool) {
	out = make([]llm.Part, 0, len(parts))
	for _, part := range parts {
		text, ok := part.(llm.TextPart)
		if !ok {
			out = append(out, part)
			continue
		}
		clean, found := llm.StripToolMarkup(text.Text)
		if !found {
			out = append(out, part)
			continue
		}
		leaked = true
		if strings.TrimSpace(clean) != "" {
			text.Text = clean
			out = append(out, text)
		}
	}
	return out, leaked
}

// Texts a model reads in place of a result. English, because they are context
// for a model, not text for a person.
const (
	resultCancelled   = "The run was stopped before this tool finished."
	resultTruncated   = "This tool call was not executed: the response hit the output token limit, so its arguments may be incomplete. Issue the call again with complete arguments."
	resultUnknownTool = "There is no tool named %q."
	resultBadArgs     = "This tool call was not executed: its arguments are not valid JSON."
	resultBudget      = "This tool call was not executed: the tool call limit for this reply has been reached. Answer with what you have."
	resultToolFailed  = "The tool failed unexpectedly."
	resultUnavailable = "This tool is no longer available."
	finalRoundNote    = "The tool call limit for this reply has been reached, and tools are now disabled. Do not call any tools. Answer the user now in text, using what you have already found; if something could not be done, say what is missing."
	resultOneAsk      = "This ask_user call was not executed and the user never saw it: a reply can carry only one ask_user call, and an earlier call in this reply is the one the user is answering. Do not call ask_user again for this. Next time, put all of your questions (up to four) together in a single ask_user call."
)

// commitTimeout bounds one transaction. Commits use their own context, not the
// run's: recording that a run was cancelled must not itself be cancelled.
const commitTimeout = 15 * time.Second

// execution is one run being driven by one goroutine — plus, during a tool
// round, the goroutines of its calls.
type execution struct {
	r *Runner

	// mu guards run, cp and live, and is held across a commit and its
	// publication, so batches reach subscribers in commit order even when
	// several tool calls finish at once.
	mu   sync.Mutex
	run  sqlc.Run
	cp   Checkpoint
	live Live

	// files resolves the attachments of each request; used by the run loop only.
	files *attachmentResolver

	// compression is what the run has done about the size of its context; used
	// by the run loop only.
	compression runCompression

	// tried holds the channels this run has failed over from since its last
	// successful request (failover); used by the run loop only.
	tried []uuid.UUID

	// parent is set on a sub-agent's run: the conversation it works for.
	parent *uuid.UUID

	// note is the run's turn note (Runner.SetTurnNote), asked once: noted
	// says it has been. Used by the run loop only.
	note  string
	noted bool
}

// change is the body of one commit. It runs inside the transaction, may append
// rows through q, and mutates ex.cp and ex.live, which the commit then saves.
// A non-nil finish ends the run instead.
type change func(ctx context.Context, q *sqlc.Queries) (events []Event, finish *finish, err error)

type finish struct {
	status sqlc.RunStatus
	code   string
}

// commit writes one step of the run and publishes its events. A failed commit
// leaves the in-memory state ahead of the database, so callers treat it as the
// end of this execution: the next process resumes from what was stored.
func (ex *execution) commit(fn change) error {
	ex.mu.Lock()
	defer ex.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), commitTimeout)
	defer cancel()

	var (
		events []Event
		run    sqlc.Run
		done   bool
	)
	err := pgx.BeginFunc(ctx, ex.r.pool, func(tx pgx.Tx) error {
		q := ex.r.q.WithTx(tx)
		var (
			fin *finish
			err error
		)
		events, fin, err = fn(ctx, q)
		if err != nil {
			return err
		}
		if fin != nil {
			var code *string
			if fin.code != "" {
				code = &fin.code
			}
			run, err = q.FinishRun(ctx, sqlc.FinishRunParams{ID: ex.run.ID, Status: fin.status, ErrorCode: code})
			if err != nil {
				return err
			}
			done = true
			// A sub-agent's run leaves its report in the transaction that ends
			// it, so a report is never lost between the end and its record.
			if hook := ex.r.subagents.Load(); hook != nil && ex.parent != nil {
				err = (*hook).Ended(ctx, q, run, *ex.parent)
			}
		} else {
			run, err = q.SaveRunState(ctx, sqlc.SaveRunStateParams{
				ID:         ex.run.ID,
				Checkpoint: mustJSON(ex.cp),
				Live:       mustJSON(ex.live),
			})
		}
		return err
	})
	if err != nil {
		return fmt.Errorf("agent: committing run %s: %w", ex.run.ID, err)
	}

	ex.run = run
	if done {
		ex.live = Live{}
		events = append(events, RunFinished{Run: run})
	}
	// A commit with nothing to report — the checkpoint between a tool round
	// and the next request — still bumps the version, but sends nothing.
	if len(events) > 0 {
		ex.r.hub.Publish(run.ConversationID, Batch{RunID: run.ID, Version: run.Version, Events: events})
	}
	if fn := ex.r.afterRun.Load(); done && fn != nil {
		(*fn)(run)
	}
	return nil
}

// fail ends the run after an error nothing else handled. Best effort: if the
// database is what failed, the run stays `running` and the next process
// resumes it, which is the better outcome anyway.
func (ex *execution) fail(err error) {
	if err == nil {
		return
	}
	// A run deleted under it - a sub-agent whose launching answer was edited
	// away, say - has nothing left to record anything on.
	if _, getErr := ex.r.q.GetRun(context.Background(), ex.run.ID); errors.Is(getErr, pgx.ErrNoRows) {
		ex.logger().Info("run ended: it was deleted")
		return
	}
	ex.logger().Error("run failed", "error", err)
	if commitErr := ex.failWithRow(CodeInternal); commitErr != nil {
		ex.logger().Error("record run failure", "error", commitErr)
	}
}

// failWithRow ends the run failed with an empty failed response carrying the
// code, so the transcript says why the question has no answer — the run's own
// error code is gone from view once the run is no longer the active one.
func (ex *execution) failWithRow(code string) error {
	return ex.commit(func(ctx context.Context, q *sqlc.Queries) ([]Event, *finish, error) {
		msg, err := ex.appendMessage(ctx, q, newRow{role: sqlc.MessageRoleAssistant, stop: StopError, code: code})
		if err != nil {
			return nil, nil, err
		}
		return []Event{MessageAppended{Message: msg}}, &finish{status: sqlc.RunStatusFailed, code: code}, nil
	})
}

// logger is the log as this run writes it: every line says which run, in
// which conversation, for whom, on which model and channel — the questions a
// failure report starts with, answered without a database lookup.
func (ex *execution) logger() *slog.Logger {
	attrs := []any{"run_id", ex.run.ID, "conversation_id", ex.run.ConversationID, "user_id", ex.run.UserID, "model", ex.run.ModelID}
	if ex.run.ChannelID != nil {
		attrs = append(attrs, "channel_id", *ex.run.ChannelID)
	}
	return slog.With(attrs...)
}

// executeTool runs a tool, turning a panic in it into an error: a tool runs on
// a goroutine of its own, where nothing else would catch it before it took the
// process down.
func executeTool(ctx context.Context, tool Tool, args json.RawMessage) (out ToolResult, err error) {
	defer func() {
		if p := recover(); p != nil {
			slog.ErrorContext(ctx, "tool panicked", "panic", p, "stack", string(debug.Stack()))
			err = fmt.Errorf("tool panicked: %v", p)
		}
	}()
	return tool.Execute(ctx, args)
}

// appendMessage writes one transcript row of this run.
func (ex *execution) appendMessage(ctx context.Context, q *sqlc.Queries, row newRow) (sqlc.Message, error) {
	status := sqlc.MessageStatusComplete
	if row.stop == StopError {
		status = sqlc.MessageStatusFailed
	}
	params := sqlc.AppendEntryParams{
		ConversationID: ex.run.ConversationID,
		Role:           row.role,
		Content:        PlainText(row.parts),
		Parts:          EncodeParts(row.parts),
		Status:         status,
		RunID:          &ex.run.ID,
	}
	if row.stop != "" {
		params.StopReason = &row.stop
	}
	if row.code != "" {
		params.ErrorCode = &row.code
	}
	if row.role == sqlc.MessageRoleAssistant {
		// The id the run asked for, not the one the provider echoed back: the
		// echo is often a dated variant ("gpt-4o-2024-08-06") the catalogue and
		// the model picker do not know, and the row is labelled with it.
		params.Model = &ex.run.ModelID
	}
	if resp := row.resp; resp != nil {
		if resp.Provider != "" {
			params.Provider = &resp.Provider
		}
		if resp.Usage != nil {
			params.Usage = mustJSON(usageJSON(*resp.Usage))
		}
	}
	return q.AppendEntry(ctx, params)
}

type newRow struct {
	role  sqlc.MessageRole
	parts []llm.Part
	stop  string
	code  string
	resp  *llm.ModelResponse
}

// toolRow is a tool result row.
func toolRow(result llm.ToolResultPart) newRow {
	return newRow{role: sqlc.MessageRoleTool, parts: []llm.Part{result}}
}

func failedResult(callID, name, content string, status llm.ToolResultStatus) llm.ToolResultPart {
	return llm.ToolResultPart{CallID: callID, Name: name, Content: content, Status: status, Error: content}
}

// usage is the stored form of llm.Usage.
type usage struct {
	InputTokens      int `json:"inputTokens"`
	OutputTokens     int `json:"outputTokens"`
	ReasoningTokens  int `json:"reasoningTokens,omitempty"`
	CacheReadTokens  int `json:"cacheReadTokens,omitempty"`
	CacheWriteTokens int `json:"cacheWriteTokens,omitempty"`
}

func usageJSON(u llm.Usage) usage {
	return usage{
		InputTokens:      u.InputTokens,
		OutputTokens:     u.OutputTokens,
		ReasoningTokens:  u.ReasoningTokens,
		CacheReadTokens:  u.CacheReadTokens,
		CacheWriteTokens: u.CacheWriteTokens,
	}
}

// ---------------------------------------------------------------- generation

// generate makes one model request and classifies its outcome. done reports
// that the run reached a terminal status.
func (ex *execution) generate(ctx context.Context) (done bool, err error) {
	r := ex.r

	// A partial committed by an attempt that never finished — the process died
	// mid-stream — becomes a cancelled row before anything else happens, so
	// what the user saw does not silently vanish.
	if g := ex.live.Generation; g != nil && len(g.Parts) > 0 {
		err := ex.commit(func(ctx context.Context, q *sqlc.Queries) ([]Event, *finish, error) {
			msg, err := ex.appendMessage(ctx, q, newRow{role: sqlc.MessageRoleAssistant, parts: g.Parts, stop: StopCancelled})
			if err != nil {
				return nil, nil, err
			}
			ex.live.Generation = &LiveGeneration{Attempt: g.Attempt, Retry: g.Retry}
			return []Event{MessageAppended{Message: msg}}, nil, nil
		})
		if err != nil {
			return false, err
		}
	}

	// A backoff survives a restart: it is in the live state, with its deadline.
	if g := ex.live.Generation; g != nil && g.Retry != nil {
		if wait := time.Until(g.Retry.At); wait > 0 {
			timer := time.NewTimer(wait)
			select {
			case <-ctx.Done():
				timer.Stop()
				return false, nil // the loop decides between cancel and shutdown
			case <-timer.C:
			}
		}
	}

	// The model's spending cap, asked before every request - not only the
	// run's first - so a long tool loop stops at the cap. A run stopped here
	// ends with an empty failed response carrying the code, as for a model
	// that is gone. What it costs to go on is not known until the request ends
	// (the ledger is written then), so this sees only what has ended; the
	// request itself is held back against the cap just before it is made
	// (meter.Reserve below), which is what stops requests that start together.
	if check := r.opts.Capacity; check != nil {
		reached, err := check(ctx, ex.run.UserID, ex.run.ModelID)
		if err != nil && ctx.Err() != nil {
			return false, nil // stopped meanwhile; the loop ends the run as it should
		}
		if err != nil {
			// Not being able to tell is not permission to spend: the run fails as internal.
			return false, fmt.Errorf("agent: checking the capacity of %s: %w", ex.run.ModelID, err)
		}
		if reached {
			slog.Info("run stopped: the model has reached its spending cap", "run_id", ex.run.ID, "model", ex.run.ModelID)
			return true, ex.failWithRow(CodeModelCapacity)
		}
	}

	client, err := r.models.Client(ctx, ex.run.ChannelID, ex.run.ModelID)
	if err != nil && ctx.Err() != nil {
		// Stopped (or shutting down) while the model was being looked up: that
		// is no failure of the model, and the loop ends the run as it should.
		return false, nil
	}
	if err != nil {
		code := CodeModelError
		if errors.Is(err, ErrNoModel) {
			// The pinned channel was switched off or deleted, or stopped
			// offering the model: another channel of the model takes the run.
			if moved, err := ex.failover(ctx, err); moved || err != nil {
				return false, err
			}
			code = CodeNoModel
		} else {
			ex.logger().Error("resolve model", "error", err)
		}
		return true, ex.failWithRow(code)
	}

	// What the request offers, and its system prompt, are settled before its
	// messages: whether the messages fit the window depends on both.
	offered := r.offered(ctx)
	if capable, ok := client.(ToolCapable); ok && !capable.CallsTools() {
		offered = nil
	}
	if !ex.run.WebSearch {
		offered = withoutWeb(offered)
	}
	system := r.opts.SystemPrompt(ctx, r.now(), offered)

	// A process that died while compressing left the flag up.
	if err := ex.settleCompaction(); err != nil {
		return false, err
	}
	// What arrived for the run since its last request - a sub-agent's report,
	// the main agent's message to a sub-agent - goes into the transcript now,
	// between the last tool round and this request, and the request reads it.
	if err := ex.takeInbox(ctx); err != nil {
		return false, err
	}
	summary, rows, err := LoadContextRows(ctx, r.q, ex.run.ConversationID, r.opts.HistoryLimit)
	if err != nil {
		return false, err
	}
	// Before every request - the first, and each after a tool round - the
	// context is compressed if it has grown too large for the window. A failed
	// compression is the request going out as it is.
	wrote, stop, err := ex.compress(ctx, client, system, definitions(offered), summary, rows, false)
	if err != nil {
		return false, err
	}
	if stop {
		return false, nil // the loop decides between cancel and shutdown
	}
	if wrote {
		if summary, rows, err = LoadContextRows(ctx, r.q, ex.run.ConversationID, r.opts.HistoryLimit); err != nil {
			return false, err
		}
		ex.noteCompressed(system, offered, summary, rows)
	}
	messages, err := BuildRequestContext(summary, ex.withNote(ctx, rows))
	if err != nil {
		return false, err
	}
	if messages, err = ex.withAttachments(ctx, messages); err != nil {
		return false, err
	}
	if system != "" {
		messages = append([]llm.Message{llm.TextMessage(llm.RoleSystem, system)}, messages...)
	}

	options := r.opts.RequestOptions
	if effort := ex.run.ReasoningEffort; effort != nil {
		options.Reasoning = &llm.ReasoningOptions{Effort: llm.ReasoningEffort(*effort)}
	}
	// The request after the last tool round must answer, and says so with a
	// tool choice rather than by leaving the tools out. The tools and the system
	// prompt are part of what a provider's cache and a replayed thinking block
	// are checked against, and a history full of tool calls with no tool list
	// is itself refused by some providers; forbidding the call changes nothing
	// that came before it.
	final := ex.cp.Round >= r.opts.MaxToolRounds
	if final && len(offered) > 0 {
		options.ToolChoice = llm.ToolChoiceNone
		// Said in words too, as OpenCode does at its step limit. A model told
		// only by the tool choice may still write a call — as text, which is
		// how DeepSeek's markup ended up in an answer — where one told plainly
		// answers. A note on this request alone, never stored.
		messages = append(messages, llm.TextMessage(llm.RoleUser, finalRoundNote))
	}

	request := llm.Request{
		Model:    ex.run.ModelID,
		Messages: messages,
		Tools:    definitions(offered),
		Options:  options,
		Session:  ex.run.ConversationID.String(),
	}
	request.User = ex.endUser(ctx)
	// A sub-agent's requests are its own kind, on the conversation it works
	// for: spend on sub-agents is seen apart, where the person's chat is.
	kind, conversationID := ledger.KindChat, ex.run.ConversationID
	if ex.parent != nil {
		kind, conversationID = ledger.KindSubagent, *ex.parent
	}
	meter := r.usage.Begin(ledger.Call{
		Kind:           kind,
		UserID:         ex.run.UserID,
		ConversationID: &conversationID,
		RunID:          &ex.run.ID,
		ChannelID:      ex.run.ChannelID,
		ModelID:        ex.run.ModelID,
		// What the request is expected to cost if the provider never says: the
		// money held back for it while it is out, and its price if it is stopped.
		InputEstimate: llm.EstimateInput(request),
		// Kept for the 轨迹 view when the run's user is traced (Options.Tracer).
		Request: &request,
	}, r.now)
	// The ledger row is written when the request ends, so the money it will
	// cost is held back until then: without it every request that starts before
	// the first one ends sees the same spend and passes the cap together.
	defer meter.Release()
	if err := meter.Reserve(ctx); err != nil {
		if ctx.Err() != nil {
			return false, nil // stopped meanwhile; the loop ends the run as it should
		}
		if errors.Is(err, ledger.ErrCapacity) {
			slog.Info("run stopped: this request would take the model over its spending cap", "run_id", ex.run.ID, "model", ex.run.ModelID)
			return true, ex.failWithRow(CodeModelCapacity)
		}
		return false, fmt.Errorf("agent: reserving the cost of a request to %s: %w", ex.run.ModelID, err)
	}
	// Made after the reservation, not before: the throttle is a goroutine, and
	// every way out above would have to remember to stop it. From here on the
	// only exit before `stream.close()` is the request itself returning.
	stream := newPartialStream(ctx, ex, ex.cp.Attempt, r.opts.PartialInterval)
	resp, streamErr := client.Stream(ctx, request, meter.Wrap(stream.emit))
	stream.close()
	resp.Message.Parts = stream.stampAll(resp.Message.Parts)

	if errors.Is(context.Cause(ctx), errShutdown) {
		// Not an outcome: the next process makes this request again, and an
		// attempt cut off by the process going away has no usage to record.
		return false, nil
	}
	// Every other ending is a request that was made, and goes in the ledger
	// before the commit that classifies it: a crash between the two leaves the
	// row of a request that really was sent, which is the honest outcome.
	meter.End(ctx, resp, streamErr)
	if ctx.Err() != nil {
		// Stopped by the user. The returned response is authoritative — it
		// holds everything generated, including what the throttle had not
		// committed yet.
		ex.mu.Lock()
		ex.live.Generation = &LiveGeneration{Attempt: ex.cp.Attempt, Parts: resp.Message.Parts}
		ex.mu.Unlock()
		return true, ex.finishCancelled()
	}
	if streamErr != nil {
		// A refusal for the request's length is answered by compressing and
		// asking again, once; the loop makes the request again.
		if retry, err := ex.recoverContextLength(ctx, client, system, offered, resp, streamErr); err != nil || retry {
			return false, err
		}
		// A request that failed before the model wrote anything is made again
		// at once on another channel of the model, if there is one: nobody has
		// seen a word of it, so nothing on the page changes but the wait. One
		// that wrote something stays where it is, retried as before - a second
		// channel would start the answer over.
		if len(resp.Message.Parts) == 0 && failsOver(streamErr) {
			if moved, err := ex.failover(ctx, streamErr); moved || err != nil {
				return false, err
			}
		}
		return ex.requestFailed(resp, streamErr)
	}
	// A channel that answered is where the run stays; any it failed over from
	// may be tried again after a later failure.
	ex.tried = ex.tried[:0]
	return ex.responded(resp, offered, final)
}

// failsOver: every failure of a request is one another channel may not have -
// a key that stopped working, a relay that is down or rate-limited, a provider
// error - except a context too long for the model, which every channel of it
// would refuse alike.
// withNote is rows with the run's turn note added to its input message, the
// note asked for at the run's first request and the same on every request
// after it, so the run's requests share their prefix. A note that cannot be
// had is left out: it is help, not part of the conversation.
func (ex *execution) withNote(ctx context.Context, rows []sqlc.Message) []sqlc.Message {
	if !ex.noted {
		if hook := ex.r.turnNote.Load(); hook != nil {
			ex.noted = true
			note, err := (*hook)(ctx)
			if err != nil && ctx.Err() == nil {
				ex.logger().Warn("turn note", "error", err)
			}
			ex.note = note
		}
	}
	if ex.note == "" {
		return rows
	}
	for i, row := range rows {
		if row.ID != ex.run.InputMessageID || row.Role != sqlc.MessageRoleUser {
			continue
		}
		parts, err := DecodeParts(row.Parts)
		if err != nil {
			return rows // BuildContext reports it
		}
		if len(parts) == 0 && row.Content != "" {
			parts = llm.Parts{llm.TextPart{Text: row.Content}}
		}
		out := slices.Clone(rows)
		out[i].Parts = EncodeParts(append(slices.Clone([]llm.Part(parts)), llm.TextPart{Text: "\n\n" + ex.note})) // apart from what they wrote
		return out
	}
	return rows
}

func failsOver(err error) bool {
	var llmErr *llm.Error
	if errors.As(err, &llmErr) && llmErr.Kind == llm.ErrorContextLength {
		return false
	}
	return true
}

// failover moves the run to the next channel of its model it has not tried
// since its last successful request, and reports whether it did. Moved, the
// loop makes the same request again at once there; not moved (no other
// channel, or the resolver cannot fail over), the failure is handled as before.
// The run's pinned channel changes with it, so a request after a restart, the
// usage log and compression all go to the new one.
func (ex *execution) failover(ctx context.Context, cause error) (bool, error) {
	failover, ok := ex.r.models.(ChannelFailover)
	if !ok || ctx.Err() != nil {
		return false, nil
	}
	from := ex.run.ChannelID
	if from != nil && !slices.Contains(ex.tried, *from) {
		ex.tried = append(ex.tried, *from)
	}
	next, err := failover.NextChannel(ctx, ex.run.ModelID, ex.tried)
	if err != nil {
		if ctx.Err() != nil {
			return false, nil
		}
		// Not being able to look is no reason to fail the run: it is handled
		// as if there were no other channel.
		ex.logger().Error("looking for another channel", "error", err)
		return false, nil
	}
	if next == nil {
		return false, nil
	}
	if err := ex.r.q.MoveRunChannel(ctx, sqlc.MoveRunChannelParams{ID: ex.run.ID, ChannelID: next}); err != nil {
		if ctx.Err() != nil {
			return false, nil
		}
		return false, err
	}
	ex.mu.Lock()
	ex.run.ChannelID = next
	ex.mu.Unlock()
	ex.logger().Warn("model request failed; failing over to another channel", "from_channel", from, "to_channel", *next, "error", cause)
	return true, nil
}

// endUser is the identifier of the person the run is for, or "".
func (ex *execution) endUser(ctx context.Context) string {
	if ex.r.opts.EndUser == nil {
		return ""
	}
	return ex.r.opts.EndUser(ctx, ex.run)
}

// requestFailed records a failed attempt and either schedules the next one or
// ends the run.
func (ex *execution) requestFailed(resp llm.ModelResponse, streamErr error) (bool, error) {
	r := ex.r
	code := errorCode(streamErr)
	ex.logger().Warn("model request failed", "attempt", ex.cp.Attempt, "code", code, "error", streamErr)

	if retryable(streamErr) && ex.cp.Attempt <= r.opts.MaxRetries {
		at := r.now().Add(retryDelay(ex.cp.Attempt))
		return false, ex.commit(func(ctx context.Context, q *sqlc.Queries) ([]Event, *finish, error) {
			var events []Event
			// An attempt that produced nothing leaves no row: an empty failed
			// bubble before the retry's answer is noise. One that produced text
			// keeps it, marked failed, because the user already saw it.
			if len(resp.Message.Parts) > 0 {
				msg, err := ex.appendMessage(ctx, q, newRow{role: sqlc.MessageRoleAssistant,
					parts: resp.Message.Parts, stop: StopError, code: code, resp: &resp})
				if err != nil {
					return nil, nil, err
				}
				events = append(events, MessageAppended{Message: msg})
			}
			ex.cp.Attempt++
			ex.live.Generation = &LiveGeneration{Attempt: ex.cp.Attempt, Retry: &Retry{At: at, ErrorCode: code}}
			return append(events, RetryScheduled{Attempt: ex.cp.Attempt, At: at, ErrorCode: code}), nil, nil
		})
	}

	return true, ex.commit(func(ctx context.Context, q *sqlc.Queries) ([]Event, *finish, error) {
		msg, err := ex.appendMessage(ctx, q, newRow{role: sqlc.MessageRoleAssistant,
			parts: resp.Message.Parts, stop: StopError, code: code, resp: &resp})
		if err != nil {
			return nil, nil, err
		}
		return []Event{MessageAppended{Message: msg}}, &finish{status: sqlc.RunStatusFailed, code: code}, nil
	})
}

// responded classifies a completed response: an answer ends the run, tool
// calls start a round.
func (ex *execution) responded(resp llm.ModelResponse, offered []Tool, final bool) (bool, error) {
	parts := resp.Message.Parts
	calls := resp.Message.ToolCalls()
	stop := stopReason(resp.FinishReason, len(calls) > 0)

	if len(calls) == 0 {
		row := newRow{role: sqlc.MessageRoleAssistant, parts: parts, stop: stop, resp: &resp}
		if resp.LeakedToolCalls {
			// What the model meant to do next never happened: shown as an
			// answer that did not finish, and kept out of the next request.
			row.stop, row.code = StopError, CodeToolMarkup
		}
		return true, ex.commit(func(ctx context.Context, q *sqlc.Queries) ([]Event, *finish, error) {
			msg, err := ex.appendMessage(ctx, q, row)
			if err != nil {
				return nil, nil, err
			}
			return []Event{MessageAppended{Message: msg}}, &finish{status: sqlc.RunStatusCompleted}, nil
		})
	}

	// Calls that will not run are answered in the same commit as the response
	// that made them, so the transcript never holds a call without a result
	// longer than one commit.
	byName := toolsByName(offered)
	truncated := resp.FinishReason == llm.FinishLength
	// Out of rounds, or nothing was on offer. A model that calls a tool
	// regardless of the choice it was given (some gateways ignore it) gets the
	// refusal below instead of a round.
	exhausted := final || len(offered) == 0

	var slots []ToolSlot
	var immediate []llm.ToolResultPart
	asking := false // a call to a UserTool stands, and the run ends on it
	for _, call := range calls {
		switch {
		case truncated:
			// Arguments cut off by the token limit can parse and still be
			// incomplete. None of them are safe to execute (pi agent-loop).
			immediate = append(immediate, failedResult(call.ID, call.Name, resultTruncated, llm.ToolFailed))
		case exhausted:
			immediate = append(immediate, failedResult(call.ID, call.Name, resultBudget, llm.ToolFailed))
		case byName[call.Name] == nil:
			immediate = append(immediate, failedResult(call.ID, call.Name, fmt.Sprintf(resultUnknownTool, call.Name), llm.ToolFailed))
		case !call.Parsed():
			immediate = append(immediate, failedResult(call.ID, call.Name, resultBadArgs, llm.ToolFailed))
		default:
			user, ok := byName[call.Name].(UserTool)
			if !ok {
				slots = append(slots, ToolSlot{CallID: call.ID, Name: call.Name, Status: SlotPending, Arguments: call.Arguments})
				break
			}
			// The person's tool is not run: it waits, once, if what it asks is askable.
			if err := user.Check(call.Arguments); err != nil {
				var toolErr *ToolError
				message := resultToolFailed
				if errors.As(err, &toolErr) {
					message = toolErr.Message
				}
				immediate = append(immediate, failedResult(call.ID, call.Name, message, llm.ToolFailed))
			} else if asking {
				immediate = append(immediate, failedResult(call.ID, call.Name, resultOneAsk, llm.ToolFailed))
			} else {
				asking = true
				slots = append(slots, ToolSlot{CallID: call.ID, Name: call.Name, Status: SlotAwaiting, Arguments: call.Arguments})
			}
		}
	}
	pending := 0
	for _, slot := range slots {
		if slot.Status == SlotPending {
			pending++
		}
	}
	// Nothing left to run and a question to put: the response is the run's last.
	awaiting := asking && pending == 0

	return exhausted || awaiting, ex.commit(func(ctx context.Context, q *sqlc.Queries) ([]Event, *finish, error) {
		msg, err := ex.appendMessage(ctx, q, newRow{role: sqlc.MessageRoleAssistant, parts: parts, stop: stop, resp: &resp})
		if err != nil {
			return nil, nil, err
		}
		events := []Event{MessageAppended{Message: msg}}
		for _, result := range immediate {
			row, err := ex.appendMessage(ctx, q, toolRow(result))
			if err != nil {
				return nil, nil, err
			}
			events = append(events, MessageAppended{Message: row})
		}

		ex.live = Live{}
		if exhausted {
			// The model was not allowed a tool and called one anyway. Asking
			// again invites the same; the run ends on what it has.
			return events, &finish{status: sqlc.RunStatusCompleted}, nil
		}
		if awaiting {
			return events, &finish{status: sqlc.RunStatusAwaitingInput}, nil
		}
		if len(slots) == 0 {
			// Every call was answered above; the model reads the errors next.
			ex.cp = Checkpoint{Phase: PhasePrepare, Attempt: 1, Round: ex.cp.Round + 1}
			return events, nil, nil
		}
		id := msg.ID
		ex.cp = Checkpoint{Phase: PhaseTools, Attempt: 1, Round: ex.cp.Round, Assistant: &id}
		ex.live.Tools = slots
		return events, nil, nil
	})
}

// ---------------------------------------------------------------- tools

// runTools executes the current round's calls and hands the run back to
// generation.
func (ex *execution) runTools(ctx context.Context) (bool, error) {
	r := ex.r
	available := r.offered(ctx)
	if !ex.run.WebSearch {
		available = withoutWeb(available)
	}
	byName := toolsByName(available)

	ex.mu.Lock()
	slots := append([]ToolSlot(nil), ex.live.Tools...)
	ex.mu.Unlock()

	var toRun []ToolSlot
	for _, slot := range slots {
		switch slot.Status {
		case SlotPending:
			toRun = append(toRun, slot)
		case SlotRunning:
			// Found mid-flight after a restart: the effect may have happened.
			if tool := byName[slot.Name]; tool != nil && tool.Replayable() {
				toRun = append(toRun, slot)
				continue
			}
			result := failedResult(slot.CallID, slot.Name, interruptedResult, llm.ToolInterrupted)
			if err := ex.recordResult(slot.CallID, result); err != nil {
				return false, err
			}
		}
	}

	var (
		wg       sync.WaitGroup
		sem      = make(chan struct{}, r.opts.ToolConcurrency)
		errMu    sync.Mutex
		firstErr error
	)
	for _, slot := range toRun {
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				return
			}
			defer func() { <-sem }()
			if err := ex.runCall(ctx, slot, byName[slot.Name]); err != nil {
				errMu.Lock()
				if firstErr == nil {
					firstErr = err
				}
				errMu.Unlock()
			}
		}()
	}
	wg.Wait()

	if firstErr != nil {
		return false, firstErr
	}
	if ctx.Err() != nil {
		return false, nil // the loop finishes a cancel, and leaves a shutdown alone
	}

	// The round's other calls are in; a question among them is the run's end.
	for _, slot := range slots {
		if slot.Status == SlotAwaiting {
			return true, ex.commit(func(context.Context, *sqlc.Queries) ([]Event, *finish, error) {
				return nil, &finish{status: sqlc.RunStatusAwaitingInput}, nil
			})
		}
	}

	return false, ex.commit(func(context.Context, *sqlc.Queries) ([]Event, *finish, error) {
		ex.live = Live{}
		ex.cp = Checkpoint{Phase: PhasePrepare, Attempt: 1, Round: ex.cp.Round + 1}
		return nil, nil, nil
	})
}

// runCall is the effect sandwich for one call: commit the intent, execute,
// commit the result.
func (ex *execution) runCall(ctx context.Context, slot ToolSlot, tool Tool) error {
	if tool == nil {
		return ex.recordResult(slot.CallID, failedResult(slot.CallID, slot.Name, resultUnavailable, llm.ToolFailed))
	}

	err := ex.commit(func(context.Context, *sqlc.Queries) ([]Event, *finish, error) {
		ex.setSlot(slot.CallID, func(s *ToolSlot) { s.Status = SlotRunning })
		return []Event{ToolStarted{CallID: slot.CallID, Name: slot.Name, Arguments: slot.Arguments}}, nil, nil
	})
	if err != nil {
		return err
	}

	started := time.Now()
	out, execErr := executeTool(withCall(ctx, slot.CallID), tool, slot.Arguments)
	elapsed := time.Since(started).Milliseconds()

	if errors.Is(context.Cause(ctx), errShutdown) {
		// Not a result: the slot stays running, and the next process decides by
		// the tool's replay policy.
		return nil
	}

	var result llm.ToolResultPart
	switch {
	case ctx.Err() != nil:
		result = failedResult(slot.CallID, slot.Name, resultCancelled, llm.ToolCancelled)
	case execErr != nil:
		var toolErr *ToolError
		message := resultToolFailed
		if errors.As(execErr, &toolErr) {
			message = toolErr.Message
		} else {
			ex.logger().Warn("tool failed", "tool", slot.Name, "error", execErr)
		}
		result = failedResult(slot.CallID, slot.Name, message, llm.ToolFailed)
	default:
		status := llm.ToolSucceeded
		if out.IsError {
			status = llm.ToolFailed
		}
		result = llm.ToolResultPart{CallID: slot.CallID, Name: slot.Name, Content: out.Content, Status: status}
		if out.IsError {
			result.Error = out.Content
		}
		if out.Details != nil {
			details, err := json.Marshal(out.Details)
			if err == nil {
				result.Details = details
			}
		}
	}
	result.DurationMS = &elapsed
	return ex.recordResult(slot.CallID, result)
}

// recordResult writes a call's result row and marks its slot done.
func (ex *execution) recordResult(callID string, result llm.ToolResultPart) error {
	return ex.commit(func(ctx context.Context, q *sqlc.Queries) ([]Event, *finish, error) {
		msg, err := ex.appendMessage(ctx, q, toolRow(result))
		if err != nil {
			return nil, nil, err
		}
		id := msg.ID
		ex.setSlot(callID, func(s *ToolSlot) {
			s.Status = SlotDone
			s.Message = &id
		})
		return []Event{MessageAppended{Message: msg}, ToolFinished{CallID: callID, Message: id}}, nil, nil
	})
}

// setSlot edits a slot of the current round. Caller holds ex.mu.
func (ex *execution) setSlot(callID string, edit func(*ToolSlot)) {
	for i := range ex.live.Tools {
		if ex.live.Tools[i].CallID == callID {
			edit(&ex.live.Tools[i])
			return
		}
	}
}

// ---------------------------------------------------------------- endings

// finishCancelled ends a run the user stopped. Calls that never got a result
// are answered as cancelled, so the transcript is complete for the next run;
// then what was generated stays, as a cancelled row — an empty one when
// nothing was. A reply stopped before its first token still leaves a turn on
// the screen, saying it was stopped and offering to answer again; without
// the row the question would stand alone with nothing to act on. The context
// builder never sends cancelled rows to a model.
func (ex *execution) finishCancelled() error {
	return ex.commit(func(ctx context.Context, q *sqlc.Queries) ([]Event, *finish, error) {
		var events []Event
		for _, slot := range ex.live.Tools {
			if slot.Status == SlotDone {
				continue
			}
			msg, err := ex.appendMessage(ctx, q, toolRow(failedResult(slot.CallID, slot.Name, resultCancelled, llm.ToolCancelled)))
			if err != nil {
				return nil, nil, err
			}
			events = append(events, MessageAppended{Message: msg}, ToolFinished{CallID: slot.CallID, Message: msg.ID})
		}
		parts := []llm.Part{}
		if g := ex.live.Generation; g != nil {
			parts = g.Parts
		}
		msg, err := ex.appendMessage(ctx, q, newRow{role: sqlc.MessageRoleAssistant, parts: parts, stop: StopCancelled})
		if err != nil {
			return nil, nil, err
		}
		events = append(events, MessageAppended{Message: msg})
		return events, &finish{status: sqlc.RunStatusCancelled}, nil
	})
}

// ---------------------------------------------------------------- classification

func stopReason(reason llm.FinishReason, hasCalls bool) string {
	switch reason {
	case llm.FinishLength:
		return StopLength
	case llm.FinishContentFilter:
		return StopContentFilter
	case llm.FinishCancelled:
		return StopCancelled
	case llm.FinishToolCalls:
		return StopToolCalls
	}
	if hasCalls {
		// Some providers report a plain stop on a response that called tools.
		return StopToolCalls
	}
	return StopStop
}

func errorCode(err error) string {
	var llmErr *llm.Error
	if !errors.As(err, &llmErr) {
		return CodeModelError
	}
	switch llmErr.Kind {
	case llm.ErrorAuth:
		return CodeModelAuth
	case llm.ErrorRateLimit:
		return CodeRateLimited
	case llm.ErrorContextLength:
		return CodeContextTooLong
	}
	return CodeModelError
}

// retryable: the request never reached the model, the provider asked us to
// slow down, or it said itself that trying again is reasonable. A credential
// or a context that is too long fails the same way every time.
func retryable(err error) bool {
	var llmErr *llm.Error
	if !errors.As(err, &llmErr) {
		return false
	}
	switch llmErr.Kind {
	case llm.ErrorTransport, llm.ErrorRateLimit:
		return true
	case llm.ErrorAuth, llm.ErrorContextLength, llm.ErrorProtocol:
		return false
	}
	return llmErr.Retryable
}

func retryDelay(attempt int) time.Duration {
	delay := time.Second << (attempt - 1)
	if delay > 30*time.Second || delay <= 0 {
		delay = 30 * time.Second
	}
	return delay
}

// ---------------------------------------------------------------- partial stream

// partialStream collects a response as it streams and commits it on a
// throttle, publishing the deltas since the previous commit with it.
type partialStream struct {
	ex      *execution
	attempt int

	mu      sync.Mutex
	parts   []llm.Part
	pending []Event
	dirty   bool
	started bool

	// lastEvent is when the provider last sent anything, to notice a stream
	// that goes quiet part-way through.
	lastEvent time.Time

	// thinking times each reasoning block, by part index, from its first event
	// to its end — its own end event, or the first event of a later part,
	// whichever comes first: not every provider closes a block before the next
	// one begins.
	thinking map[int]*span

	stop chan struct{}
	done chan struct{}
}

type span struct{ from, to time.Time }

// newPartialStream starts the throttle of one request. Its goroutine ends on
// close, and also when ctx does, so a stream that is never closed (a path
// added later, a panic unwinding) cannot outlive the request it was made for.
func newPartialStream(ctx context.Context, ex *execution, attempt int, interval time.Duration) *partialStream {
	s := &partialStream{ex: ex, attempt: attempt, thinking: map[int]*span{}, stop: make(chan struct{}), done: make(chan struct{})}
	go func() {
		defer close(s.done)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-s.stop:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.flush()
			}
		}
	}()
	return s
}

// close stops the throttle and waits for a flush in progress, so nothing
// lands after the commit that classifies the response.
func (s *partialStream) close() {
	close(s.stop)
	<-s.done
}

// emit is the provider's event callback. It never fails the stream: a
// subscriber is not watching the provider, and the commit that matters comes
// after the stream either way.
func (s *partialStream) emit(ev llm.Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.time(ev)
	s.noticePause()
	switch ev.Type {
	case llm.EventPartStart:
		s.grow(ev.Index)
		part := emptyPart(ev.PartKind, ev.ToolCallID, ev.ToolName)
		s.parts[ev.Index] = part
		s.pending = append(s.pending, PartStarted{Index: ev.Index, Part: part})
	case llm.EventPartDelta:
		s.grow(ev.Index)
		s.parts[ev.Index] = appendDelta(s.parts[ev.Index], ev.PartKind, ev.Delta)
		if n := len(s.pending); n > 0 {
			if last, ok := s.pending[n-1].(PartDelta); ok && last.Index == ev.Index {
				last.Delta += ev.Delta
				s.pending[n-1] = last
				break
			}
		}
		s.pending = append(s.pending, PartDelta{Index: ev.Index, Delta: ev.Delta})
	case llm.EventPartEnd:
		s.grow(ev.Index)
		// The finished block carries its time, so a page watching sees it the
		// moment the thinking stops, not only once the reply is saved.
		ev.Part = s.stamp(ev.Index, ev.Part)
		s.parts[ev.Index] = ev.Part
		s.pending = append(s.pending, PartEnded{Index: ev.Index, Part: ev.Part})
	default:
		return nil
	}
	s.dirty = true
	return nil
}

// slowPartialCommit is how long a throttled commit may take before it is
// logged; it normally takes a few milliseconds.
const slowPartialCommit = 500 * time.Millisecond

// streamPause is how long a provider may go quiet mid-response before it is
// logged. Once the first token is in, tokens come every few tens of
// milliseconds; a gap this long shows on the page as the answer stopping.
const streamPause = 3 * time.Second

// noticePause logs a provider that went quiet part-way through a response, so
// a reply that stalled on screen can be told apart from one that stalled on
// the way to it.
func (s *partialStream) noticePause() {
	now := s.ex.r.now()
	if !s.lastEvent.IsZero() {
		if gap := now.Sub(s.lastEvent); gap >= streamPause {
			s.ex.logger().Warn("model stream paused", "seconds", gap.Round(100*time.Millisecond).Seconds())
		}
	}
	s.lastEvent = now
}

// time notes when reasoning blocks start and stop.
func (s *partialStream) time(ev llm.Event) {
	if ev.Type != llm.EventPartStart && ev.Type != llm.EventPartDelta && ev.Type != llm.EventPartEnd {
		return
	}
	now := s.ex.r.now()
	for index, t := range s.thinking {
		if index != ev.Index && t.to.IsZero() {
			t.to = now
		}
	}
	if ev.PartKind == llm.PartReasoning || isReasoning(ev.Part) {
		t := s.thinking[ev.Index]
		if t == nil {
			t = &span{from: now}
			s.thinking[ev.Index] = t
		}
		if ev.Type == llm.EventPartEnd && t.to.IsZero() {
			t.to = now
		}
	}
}

func isReasoning(part llm.Part) bool {
	_, ok := part.(llm.ReasoningPart)
	return ok
}

// stamp sets a reasoning part's duration from its timing, if it was timed.
func (s *partialStream) stamp(index int, part llm.Part) llm.Part {
	p, ok := part.(llm.ReasoningPart)
	t := s.thinking[index]
	if !ok || t == nil {
		return part
	}
	to := t.to
	if to.IsZero() {
		to = s.ex.r.now()
	}
	ms := max(to.Sub(t.from).Milliseconds(), 0)
	p.DurationMS = &ms
	return p
}

// stampAll times the reasoning in a finished response. Its parts are the
// provider's own record, indexed as its events were.
func (s *partialStream) stampAll(parts []llm.Part) []llm.Part {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]llm.Part, len(parts))
	for i, part := range parts {
		out[i] = s.stamp(i, part)
	}
	return out
}

func (s *partialStream) grow(index int) {
	for len(s.parts) <= index {
		s.parts = append(s.parts, llm.TextPart{})
	}
}

func (s *partialStream) flush() {
	s.mu.Lock()
	if !s.dirty {
		s.mu.Unlock()
		return
	}
	parts := append([]llm.Part(nil), s.parts...)
	events := s.pending
	s.pending, s.dirty = nil, false
	if !s.started {
		s.started = true
		events = append([]Event{GenerationStarted{Attempt: s.attempt}}, events...)
	}
	s.mu.Unlock()

	started := time.Now()
	err := s.ex.commit(func(context.Context, *sqlc.Queries) ([]Event, *finish, error) {
		s.ex.live.Generation = &LiveGeneration{Attempt: s.attempt, Parts: parts}
		return events, nil, nil
	})
	// Nothing reaches the page until this commit does, so a slow one is the
	// answer pausing on screen while the model keeps going.
	if took := time.Since(started); took >= slowPartialCommit {
		s.ex.logger().Warn("slow partial commit", "ms", took.Milliseconds())
	}
	if err != nil {
		// The final commit carries the whole response regardless; a missed
		// partial costs only liveness.
		s.ex.logger().Warn("commit partial response", "error", err)
	}
}

func emptyPart(kind llm.PartKind, id, name string) llm.Part {
	switch kind {
	case llm.PartReasoning:
		return llm.ReasoningPart{}
	case llm.PartToolCall:
		return llm.ToolCallPart{ID: id, Name: name}
	}
	return llm.TextPart{}
}

func appendDelta(part llm.Part, kind llm.PartKind, delta string) llm.Part {
	switch p := part.(type) {
	case llm.TextPart:
		if kind == llm.PartReasoning {
			return llm.ReasoningPart{Text: p.Text + delta}
		}
		p.Text += delta
		return p
	case llm.ReasoningPart:
		p.Text += delta
		return p
	case llm.ToolCallPart:
		p.RawArguments += delta
		return p
	}
	return part
}

// ---------------------------------------------------------------- inbox

// takeInbox writes what arrived for the run since its last request into its
// transcript (Runner.SetSubagents): the reports of the conversation's
// background sub-agents, or, on a sub-agent's run, the main agent's messages
// to it. Asked before every request; a commit only when something is there.
func (ex *execution) takeInbox(ctx context.Context) error {
	hook := ex.r.subagents.Load()
	if hook == nil {
		return nil
	}
	waiting, err := (*hook).Waiting(ctx, ex.run, ex.parent)
	if err != nil {
		if ctx.Err() != nil {
			return nil // the loop decides between cancel and shutdown
		}
		return err
	}
	if !waiting {
		return nil
	}
	return ex.commit(func(ctx context.Context, q *sqlc.Queries) ([]Event, *finish, error) {
		rows, err := (*hook).Take(ctx, q, ex.run, ex.parent)
		if err != nil {
			return nil, nil, err
		}
		events := make([]Event, 0, len(rows))
		for _, row := range rows {
			events = append(events, MessageAppended{Message: row})
		}
		return events, nil, nil
	})
}
