// Package usage keeps the ledger of model requests: who asked which model
// through which channel, what it came back with, what that cost and how long it
// took. One row per request — a retried attempt is its own row, and so is a
// failure — written when the request ends and never changed afterwards.
//
// The ledger is an observer. Recording is best effort and bounded: a database
// that is slow or gone costs the operator a row, never the user an answer.
package usage

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"math"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/qunqin24/openchat/apps/server/internal/db/sqlc"
	"github.com/qunqin24/openchat/apps/server/internal/llm"
)

// Kind is what a request was for.
type Kind string

const (
	// KindChat is a model request of a run.
	KindChat Kind = "chat"
	// KindTitle is the background request that names a conversation.
	KindTitle Kind = "title"
	// KindCompression is the request that writes a conversation's context
	// summary, made with the conversation's own model.
	KindCompression Kind = "compression"
	// KindSubagent is a model request of a sub-agent's run.
	KindSubagent Kind = "subagent"
	// KindModeration is the request that has a model classify a message for
	// content moderation (api/moderation.go).
	KindModeration Kind = "moderation"
)

// Status is how a request ended.
type Status string

const (
	StatusOK    Status = "ok"
	StatusError Status = "error"
)

const (
	// writeTimeout bounds the lookup and the insert together. They normally
	// take a few milliseconds; a database that does not answer in this time is
	// not going to, and the run has waited long enough.
	writeTimeout = 5 * time.Second

	// maxMessageRunes keeps an error message to what fits in a table cell and a
	// tooltip. The provider's full text stays in the server log.
	maxMessageRunes = 300
)

// Recorder writes the ledger.
type Recorder struct {
	q        *sqlc.Queries
	reserver Reserver
	tracer   Tracer
}

// Tracer says whether a person's requests are traced: what each carried and
// what came back, kept beside its row for the 轨迹 view (request_traces).
// Asked once per request that offers a trace (Call.Request).
type Tracer func(ctx context.Context, userID uuid.UUID) bool

// SetTracer sets who decides which requests are traced. Called while the
// application is assembled; without one nothing is traced.
func (r *Recorder) SetTracer(t Tracer) {
	if r != nil {
		r.tracer = t
	}
}

func NewRecorder(q *sqlc.Queries) *Recorder { return &Recorder{q: q} }

// ErrCapacity is a request refused because the money it is expected to cost,
// on top of what the model has cost and what requests in flight are expected
// to, would reach the model's spending cap.
var ErrCapacity = errors.New("usage: the model's spending cap would be reached")

// Reserver holds money back for a request in flight. The ledger row of a
// request is written when it ends, so without it every request that starts
// before the first one ends is checked against the same spend; a Reserver
// counts the requests that are out as spent. It decides whether the request
// may go (ErrCapacity when not) and returns what releases the hold, which the
// Meter calls once the request's own row is written.
type Reserver interface {
	Reserve(ctx context.Context, call Call) (release func(), err error)
}

// SetReserver sets who holds money back for requests. Called while the
// application is assembled, before any request is made.
func (r *Recorder) SetReserver(rv Reserver) {
	if r != nil {
		r.reserver = rv
	}
}

// Call is what is known about a request before it is made.
type Call struct {
	Kind Kind
	// UserID is whose request it is: a run's user, or a conversation's owner
	// for its title. The zero value is nobody's and is recorded as no owner.
	UserID         uuid.UUID
	ConversationID *uuid.UUID
	RunID          *uuid.UUID
	ChannelID      *uuid.UUID
	// ModelID is the id the request asked for.
	ModelID string
	// InputEstimate is the request's estimated input tokens (llm.EstimateInput).
	// It prices a request whose provider never reports usage - one that was
	// stopped or broke before the usage frame - and is what a Reserver holds
	// back for it. Zero when the caller did not estimate.
	InputEstimate int
	// Request is the request as it is sent, for a trace (Tracer). Nil offers
	// none. Only its messages, tools and options are kept; files are kept as
	// the references they are in the message, never their bytes.
	Request *llm.Request
}

// Meter times and then records one request.
type Meter struct {
	rec   *Recorder
	call  Call
	clock func() time.Time

	started time.Time

	mu    sync.Mutex
	first time.Time
	// alive is set once the provider has sent anything at all: the request was
	// taken up, so whatever it cost is owed whether or not usage ever arrives.
	alive   bool
	release func()
	// labels are the channel's and account's names and the model's prices,
	// taken by Reserve while they still exist; End looks them up itself for a
	// request that was never reserved.
	labels *sqlc.UsagePricingRow
}

// Begin starts timing a request. A nil Recorder is a ledger that is switched
// off: the meter still passes events through, and End writes nothing. clock is the caller's idea of now — a test's
// fake one — and time.Now when nil.
func (r *Recorder) Begin(call Call, clock func() time.Time) *Meter {
	if clock == nil {
		clock = time.Now
	}
	return &Meter{rec: r, call: call, clock: clock, started: clock()}
}

// Wrap returns an event callback that notes when the first content arrives and
// passes every event on to next. "Content" is the first delta of any kind —
// text, reasoning or a tool call's arguments — because that is when the user
// has something to look at; the response-start and part-start events only say
// the connection is alive.
func (m *Meter) Wrap(next func(llm.Event) error) func(llm.Event) error {
	return func(ev llm.Event) error {
		m.mu.Lock()
		m.alive = true
		if ev.Type == llm.EventPartDelta && m.first.IsZero() {
			m.first = m.clock()
		}
		m.mu.Unlock()
		if next == nil {
			return nil
		}
		return next(ev)
	}
}

// Reserve asks the recorder's Reserver (if it has one) whether the request may
// go, and holds back what it is expected to cost until End. It returns
// ErrCapacity for a request that would take its model over the cap. Call it
// just before the request is made; the timing starts again here, so the check
// is not part of the request's duration.
func (m *Meter) Reserve(ctx context.Context) error {
	if m.rec == nil {
		return nil
	}
	if m.rec.q != nil {
		if labels, ok := m.lookup(ctx); ok {
			m.mu.Lock()
			m.labels = &labels
			m.mu.Unlock()
		}
	}
	if m.rec.reserver == nil {
		return nil
	}
	release, err := m.rec.reserver.Reserve(ctx, m.call)
	if err != nil {
		return err
	}
	m.mu.Lock()
	m.release = release
	m.started = m.clock()
	m.mu.Unlock()
	return nil
}

// Release gives back what Reserve held, if it still does. End does it too, once
// the row is written; a caller that can leave without calling End defers this.
func (m *Meter) Release() {
	m.mu.Lock()
	release := m.release
	m.release = nil
	m.mu.Unlock()
	if release != nil {
		release()
	}
}

// End writes the row for a request that has finished, however it finished.
// ctx is the request's own: when it was cancelled the request was stopped by
// the user (or the run's deadline), which is an ordinary ending and not a
// failure. The write itself never uses it, since a cancelled context is
// exactly when the row is still wanted.
//
// A request that was taken up and ended without the provider's usage - stopped
// by the user, broken mid-stream, or from a provider that does not report any -
// still cost what it cost. Its row then carries an estimate (see settle) and is
// flagged `estimated`: leaving it at nothing would let a request that is
// stopped just before the usage frame go on for free, as many times as anyone
// likes.
func (m *Meter) End(ctx context.Context, resp llm.ModelResponse, err error) {
	// After the row is written, never before: for as long as the request is
	// out, the money it will cost is either held or in the ledger.
	defer m.Release()
	if m.rec == nil || m.rec.q == nil {
		// No ledger configured (a Server assembled without one): timing still
		// works, nothing is written.
		return
	}
	ended := m.clock()
	row := sqlc.InsertUsageLogParams{
		CreatedAt:      pgtype.Timestamptz{Time: m.started, Valid: true},
		Kind:           string(m.call.Kind),
		Status:         string(StatusOK),
		UserID:         owner(m.call.UserID),
		ConversationID: m.call.ConversationID,
		RunID:          m.call.RunID,
		ChannelID:      m.call.ChannelID,
		ModelID:        m.call.ModelID,
		ServedModel:    resp.Model,
		Streamed:       true,
		DurationMs:     millis(ended.Sub(m.started)),
	}
	m.mu.Lock()
	if !m.first.IsZero() {
		ms := millis(m.first.Sub(m.started))
		row.FirstTokenMs = &ms
	}
	m.mu.Unlock()

	if err != nil && ctx.Err() == nil {
		code, message := describe(err)
		row.Status = string(StatusError)
		row.ErrorCode = &code
		row.ErrorMessage = &message
	}

	usage, estimated := m.settle(ctx, resp, err)
	row.Estimated = estimated
	if usage != nil {
		row.InputTokens = int32(clampInt(usage.InputTokens))
		row.OutputTokens = int32(clampInt(usage.OutputTokens))
		row.ReasoningTokens = int32(clampInt(usage.ReasoningTokens))
		row.CacheReadTokens = int32(clampInt(usage.CacheReadTokens))
		row.CacheWriteTokens = int32(clampInt(usage.CacheWriteTokens))
	}

	// Detached from the request's context but bounded, and last: everything
	// above is arithmetic.
	wctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), writeTimeout)
	defer cancel()
	m.price(wctx, &row, usage)
	id, werr := m.rec.q.InsertUsageLog(wctx, row)
	if pgErr := (*pgconn.PgError)(nil); errors.As(werr, &pgErr) && pgErr.Code == "23503" {
		// What the row names was deleted while this insert was being written;
		// once more, and the insert sees it gone (23503 is foreign_key_violation).
		id, werr = m.rec.q.InsertUsageLog(wctx, row)
	}
	if werr != nil {
		slog.Warn("usage: record request", "kind", m.call.Kind, "model", m.call.ModelID, "error", werr)
		return
	}
	m.trace(wctx, id, resp, ended)
}

// trace keeps what the request carried and what came back, when it offered a
// trace and its owner's requests are traced. Best effort: a trace that cannot
// be written is a debugging aid lost, never a request failed.
func (m *Meter) trace(ctx context.Context, usageLogID uuid.UUID, resp llm.ModelResponse, ended time.Time) {
	req := m.call.Request
	if req == nil || m.rec.tracer == nil || m.call.ConversationID == nil || m.call.UserID == uuid.Nil {
		return
	}
	if !m.rec.tracer(ctx, m.call.UserID) {
		return
	}
	tools := make([]traceTool, 0, len(req.Tools))
	for _, tool := range req.Tools {
		tools = append(tools, traceTool{Name: tool.Name, Description: tool.Description, Parameters: tool.Parameters})
	}
	request, err := json.Marshal(traceRequest{Messages: req.Messages, Tools: tools, Options: traceOptionsOf(req.Options)})
	if err != nil {
		slog.Warn("usage: encode trace request", "error", err)
		return
	}
	response, err := json.Marshal(traceResponse{
		Message:      resp.Message,
		FinishReason: string(resp.FinishReason),
		RawFinish:    resp.RawFinishReason,
		EndedAt:      ended,
	})
	if err != nil {
		slog.Warn("usage: encode trace response", "error", err)
		return
	}
	if err := m.rec.q.InsertRequestTrace(ctx, sqlc.InsertRequestTraceParams{
		UsageLogID: usageLogID, UserID: m.call.UserID, ConversationID: *m.call.ConversationID,
		Request: request, Response: response,
	}); err != nil {
		slog.Warn("usage: record trace", "error", err)
	}
}

// What a trace keeps of a request and its response. Messages encode as the
// transcript does (llm's codec), which carries a file as its reference.
type traceRequest struct {
	Messages []llm.Message `json:"messages"`
	Tools    []traceTool   `json:"tools"`
	Options  traceOptions  `json:"options"`
}

type traceTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

type traceOptions struct {
	MaxTokens   *int     `json:"maxTokens,omitempty"`
	Temperature *float64 `json:"temperature,omitempty"`
	ToolChoice  string   `json:"toolChoice,omitempty"`
	Reasoning   string   `json:"reasoning,omitempty"`
}

func traceOptionsOf(o llm.Options) traceOptions {
	out := traceOptions{MaxTokens: o.MaxTokens, Temperature: o.Temperature, ToolChoice: string(o.ToolChoice)}
	if o.Reasoning != nil {
		out.Reasoning = string(o.Reasoning.Effort)
	}
	return out
}

type traceResponse struct {
	Message      llm.Message `json:"message"`
	FinishReason string      `json:"finishReason,omitempty"`
	RawFinish    string      `json:"rawFinishReason,omitempty"`
	EndedAt      time.Time   `json:"endedAt"`
}

// settle is the usage a row is written with: the provider's when it reported
// it, else an estimate of what is known to have been spent.
//
// The provider's figure wins wherever it exists. A report that is only partial
// (Anthropic gives the input at the start of the message and the output at the
// end, so a stream stopped between has the first and not the second) is kept
// as it is and filled in only where it says nothing: no input at all becomes
// the request's input estimate, no output the estimate of what has streamed.
// A request that ended cleanly with usage is never touched.
//
// Only a request that was taken up has an estimate: one the provider refused
// before it started (a status error, no connection) was not billed. Stopping
// is always taken as taken up - the user pressed Stop after it was sent, and a
// provider bills the context it has read whether or not anyone waits for the
// answer.
func (m *Meter) settle(ctx context.Context, resp llm.ModelResponse, err error) (*llm.Usage, bool) {
	cancelled := ctx.Err() != nil
	if resp.Usage != nil && err == nil && !cancelled {
		return resp.Usage, false
	}
	m.mu.Lock()
	alive := m.alive
	m.mu.Unlock()
	if !cancelled && err != nil && !alive && len(resp.Message.Parts) == 0 {
		return resp.Usage, false
	}

	var usage llm.Usage
	if resp.Usage != nil {
		usage = *resp.Usage
	}
	estimated := false
	if usage.InputTokens+usage.CacheReadTokens+usage.CacheWriteTokens == 0 && m.call.InputEstimate > 0 {
		usage.InputTokens = m.call.InputEstimate
		estimated = true
	}
	if usage.OutputTokens == 0 {
		if output, reasoning := llm.EstimateOutput(resp.Message); output > 0 {
			usage.OutputTokens, usage.ReasoningTokens = output, min(reasoning, output)
			estimated = true
		}
	}
	if !estimated {
		return resp.Usage, false
	}
	return &usage, true
}

// price fills in the channel's name and the prices the model had when the
// request was made, and the cost that follows. A failed lookup leaves them
// out; the row is still worth having.
func (m *Meter) lookup(ctx context.Context) (sqlc.UsagePricingRow, bool) {
	p, err := m.rec.q.UsagePricing(ctx, sqlc.UsagePricingParams{ChannelID: m.call.ChannelID, UserID: owner(m.call.UserID), ModelID: m.call.ModelID})
	if err != nil {
		slog.Warn("usage: look up prices", "model", m.call.ModelID, "error", err)
		return p, false
	}
	return p, true
}

func (m *Meter) price(ctx context.Context, row *sqlc.InsertUsageLogParams, usage *llm.Usage) {
	m.mu.Lock()
	labels := m.labels
	m.mu.Unlock()
	if labels == nil {
		p, ok := m.lookup(ctx)
		if !ok {
			return
		}
		labels = &p
	}
	p := *labels
	if p.ChannelName != nil {
		row.ChannelName = *p.ChannelName
	}
	row.DeletedUser = p.UserName
	row.InputPrice = number(p.InputPrice)
	row.OutputPrice = number(p.OutputPrice)
	row.CacheReadPrice = number(p.CacheReadPrice)
	row.CacheWritePrice = number(p.CacheWritePrice)
	row.Cost = Cost(usage, Prices{
		Input: row.InputPrice, Output: row.OutputPrice,
		CacheRead: row.CacheReadPrice, CacheWrite: row.CacheWritePrice,
	})
}

// Prices are USD per million tokens. A nil price is one the catalogue did not
// give, which is not the same as free.
type Prices struct {
	Input, Output, CacheRead, CacheWrite *float64
}

// Cost is what a request cost in USD, or nil when that cannot be said: the
// provider reported no usage, or the model has no input and output price.
//
// Cached tokens are billed at their own price where the catalogue has one. A
// model with no cache price listed does not discount the cache, so those tokens
// are billed as ordinary input rather than as nothing. Reasoning tokens are
// already inside the output count (llm.Usage) and are not added again.
func Cost(usage *llm.Usage, prices Prices) *float64 {
	if usage == nil || prices.Input == nil || prices.Output == nil {
		return nil
	}
	cacheRead, cacheWrite := prices.CacheRead, prices.CacheWrite
	if cacheRead == nil {
		cacheRead = prices.Input
	}
	if cacheWrite == nil {
		cacheWrite = prices.Input
	}
	total := float64(usage.InputTokens)**prices.Input +
		float64(usage.OutputTokens)**prices.Output +
		float64(usage.CacheReadTokens)**cacheRead +
		float64(usage.CacheWriteTokens)**cacheWrite
	total /= 1_000_000
	return &total
}

func number(n pgtype.Numeric) *float64 {
	f, err := n.Float64Value()
	if err != nil || !f.Valid || math.IsNaN(f.Float64) || math.IsInf(f.Float64, 0) {
		return nil
	}
	return &f.Float64
}

func millis(d time.Duration) int32 {
	if d < 0 {
		return 0
	}
	return int32(min(d.Milliseconds(), math.MaxInt32))
}

func clampInt(n int) int {
	return max(0, min(n, math.MaxInt32))
}

// Error codes, spelled as the chat spells them: the admin page translates the
// same keys.
const (
	codeModelError     = "model_error"
	codeModelAuth      = "model_auth"
	codeRateLimited    = "model_rate_limited"
	codeContextTooLong = "context_too_long"
)

// describe classifies a failed request into the code the transcript shows and
// a short message safe to keep: the adapter's own words, without the wrapped
// transport error (which carries URLs), scrubbed of anything shaped like a
// credential and cut to length.
func describe(err error) (code, message string) {
	code = codeModelError
	var llmErr *llm.Error
	if errors.As(err, &llmErr) {
		switch llmErr.Kind {
		case llm.ErrorAuth:
			code = codeModelAuth
		case llm.ErrorRateLimit:
			code = codeRateLimited
		case llm.ErrorContextLength:
			code = codeContextTooLong
		}
		message = string(llmErr.Kind)
		if llmErr.Code != "" {
			message += " (" + llmErr.Code + ")"
		}
		if llmErr.Message != "" {
			message += ": " + llmErr.Message
		}
	} else {
		message = err.Error()
	}
	return code, clean(message)
}

var (
	// key=..., api_key: ..., Authorization: Bearer ... and the shapes vendors
	// give their keys. A provider that echoes a rejected key back in its error
	// is not rare, and this table is read by more people than the key is.
	secretAssign = regexp.MustCompile(`(?i)\b(api[-_]?key|key|token|secret|authorization)\b(["']?\s*[:=]\s*["']?)[^\s"',;&]+`)
	bearer       = regexp.MustCompile(`(?i)\b(bearer)\s+[^\s"',;&]+`)
	secretShape  = regexp.MustCompile(`\b(sk|pk|rk|ak)-[A-Za-z0-9_\-]{6,}|\bAIza[0-9A-Za-z_\-]{10,}|\b[A-Za-z0-9]{20,}\.[A-Za-z0-9]{6,}\b`)
	spaces       = regexp.MustCompile(`\s+`)
)

func clean(message string) string {
	message = bearer.ReplaceAllString(message, "${1} [redacted]")
	message = secretAssign.ReplaceAllString(message, "${1}${2}[redacted]")
	message = secretShape.ReplaceAllString(message, "[redacted]")
	message = strings.TrimSpace(spaces.ReplaceAllString(message, " "))
	if runes := []rune(message); len(runes) > maxMessageRunes {
		message = string(runes[:maxMessageRunes]) + "…"
	}
	return message
}

// owner is the ledger's user for a call: none for the zero value.
func owner(id uuid.UUID) *uuid.UUID {
	if id == uuid.Nil {
		return nil
	}
	return &id
}
