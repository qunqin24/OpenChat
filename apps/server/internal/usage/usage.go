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
	"errors"
	"log/slog"
	"math"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/qunqin/openchat/apps/server/internal/db/sqlc"
	"github.com/qunqin/openchat/apps/server/internal/llm"
)

// Kind is what a request was for.
type Kind string

const (
	// KindChat is a model request of a run.
	KindChat Kind = "chat"
	// KindTitle is the background request that names a conversation.
	KindTitle Kind = "title"
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
	q *sqlc.Queries
}

func NewRecorder(q *sqlc.Queries) *Recorder { return &Recorder{q: q} }

// Call is what is known about a request before it is made.
type Call struct {
	Kind Kind
	// UserID is whose request it is: a run's user, or a conversation's owner
	// for its title. The zero value is nobody's - a guest's title, which has
	// no account to charge - and is recorded as no owner.
	UserID         uuid.UUID
	ConversationID *uuid.UUID
	RunID          *uuid.UUID
	ChannelID      *uuid.UUID
	// ModelID is the id the request asked for.
	ModelID string
}

// Meter times and then records one request.
type Meter struct {
	rec   *Recorder
	call  Call
	clock func() time.Time

	started time.Time

	mu    sync.Mutex
	first time.Time
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
		if ev.Type == llm.EventPartDelta {
			m.mu.Lock()
			if m.first.IsZero() {
				m.first = m.clock()
			}
			m.mu.Unlock()
		}
		if next == nil {
			return nil
		}
		return next(ev)
	}
}

// End writes the row for a request that has finished, however it finished.
// ctx is the request's own: when it was cancelled the request was stopped by
// the user (or the run's deadline), which is an ordinary ending and not a
// failure. The write itself never uses it, since a cancelled context is
// exactly when the row is still wanted.
func (m *Meter) End(ctx context.Context, resp llm.ModelResponse, err error) {
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

	var usage *llm.Usage
	if resp.Usage != nil {
		usage = resp.Usage
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
	if werr := m.rec.q.InsertUsageLog(wctx, row); werr != nil {
		slog.Warn("usage: record request", "kind", m.call.Kind, "model", m.call.ModelID, "error", werr)
	}
}

// price fills in the channel's name and the prices the model had when the
// request was made, and the cost that follows. A failed lookup leaves them
// out; the row is still worth having.
func (m *Meter) price(ctx context.Context, row *sqlc.InsertUsageLogParams, usage *llm.Usage) {
	p, err := m.rec.q.UsagePricing(ctx, sqlc.UsagePricingParams{ChannelID: m.call.ChannelID, ModelID: m.call.ModelID})
	if err != nil {
		slog.Warn("usage: look up prices", "model", m.call.ModelID, "error", err)
		return
	}
	if p.ChannelName != nil {
		row.ChannelName = *p.ChannelName
	}
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
