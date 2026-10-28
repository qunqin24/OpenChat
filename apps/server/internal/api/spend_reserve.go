package api

import (
	"context"
	"errors"
	"math"
	"math/big"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/qunqin24/openchat/apps/server/internal/db/sqlc"
	"github.com/qunqin24/openchat/apps/server/internal/usage"
)

// Money held back for requests in flight against a spending cap.
//
// The ledger row of a request is written when the request ends, so the sum a
// cap is checked against leaves out everything that is still out. Left at that,
// every request that starts before the first one ends is checked against the
// same spend and all of them pass - and one account can have many going (a
// run per conversation), and a script many more. A request to a capped model
// therefore holds back what it is expected to cost for as long as it is out,
// and is let go only if the spend so far, what is held for the others and its
// own expected cost together stay under the cap.
//
// What is held is the *input* cost: the request's estimated input tokens
// (llm.EstimateInput, widened) at the model's catalogue input price. That is
// the part known before the request is made and the part a large context
// spends whether or not the answer is ever read. The output is not held: it is
// unknown, and the concurrency limit (runs per account, per address) is what
// bounds how many answers can be growing at once. The cached part is held at
// the full input price, so a request on a warm cache is held for more than it
// will cost - the safe side.
//
// The holds are in this process's memory. That is a decision, not an
// oversight: it needs no table and no cleanup, cannot leave a stale row
// behind, and is exact for a single server, which is how this is deployed.
// With several servers on one database each sees only its own holds and the
// overshoot is bounded by the requests in flight on the others; the
// concurrency limit, which is in the database, still holds across them. A hold
// that was never released (it always is: the meter releases it when its row is
// written, and callers defer the release) expires after holdTTL.
type SpendReserver struct {
	g capacityGuard

	// mu is held over the whole of a reservation, the database read included:
	// the read must see every row written before the holds it adds to were
	// given back, and give-backs wait for the same lock.
	mu   sync.Mutex
	held map[string]map[uint64]hold
	next uint64
}

type hold struct {
	micros int64
	at     time.Time
}

// holdTTL is how long a hold can stand. No request takes this long; it only
// keeps a bug from leaving a model held for good.
const holdTTL = time.Hour

// NewSpendReserver holds money back against the caps in pool's database.
func NewSpendReserver(pool *pgxpool.Pool) *SpendReserver {
	return &SpendReserver{g: newCapacityGuard(sqlc.New(pool)), held: map[string]map[uint64]hold{}}
}

var _ usage.Reserver = (*SpendReserver)(nil)

// Reserve implements usage.Reserver.
func (r *SpendReserver) Reserve(ctx context.Context, call usage.Call) (func(), error) {
	nothing := func() {}
	if call.ModelID == "" {
		return nothing, nil
	}
	row, err := r.g.q.GetModelOverride(ctx, call.ModelID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nothing, nil
	}
	if err != nil {
		return nil, err
	}
	rule, capped := capacityOf(row)
	if !capped {
		return nothing, nil
	}

	// What the request is expected to cost: nothing to hold for a model with
	// no input price, whose requests carry no cost for the cap to count.
	prices, err := r.g.q.UsagePricing(ctx, sqlc.UsagePricingParams{ChannelID: call.ChannelID, ModelID: call.ModelID})
	if err != nil {
		return nil, err
	}
	micros := int64(0)
	if prices.InputPrice.Valid {
		// USD per million tokens is micro-dollars per token.
		micros = int64(math.Ceil(float64(call.InputEstimate) * numberOf(prices.InputPrice)))
	}
	if micros <= 0 {
		return nothing, nil
	}

	admin, err := r.g.isAdmin(ctx, call.UserID)
	if err != nil {
		return nil, err
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	now := r.g.now()
	others := r.sum(call.ModelID, now)
	// The cap holds for this requester unless it exempts them; their request is
	// held either way, because their spend counts against everyone else's.
	if rule.appliesTo(admin) {
		reached, err := r.g.q.ModelWouldReach(ctx, sqlc.ModelWouldReachParams{
			ModelID:  call.ModelID,
			Since:    pgtype.Timestamptz{Time: derefTime(rule.windowStart(now)), Valid: rule.windowStart(now) != nil},
			Held:     microsNumeric(others + micros),
			Capacity: rule.limit,
		})
		if err != nil {
			return nil, err
		}
		if reached {
			return nil, usage.ErrCapacity
		}
	}

	r.next++
	id := r.next
	if r.held[call.ModelID] == nil {
		r.held[call.ModelID] = map[uint64]hold{}
	}
	r.held[call.ModelID][id] = hold{micros: micros, at: now}
	var once sync.Once
	return func() {
		once.Do(func() {
			r.mu.Lock()
			defer r.mu.Unlock()
			delete(r.held[call.ModelID], id)
			if len(r.held[call.ModelID]) == 0 {
				delete(r.held, call.ModelID)
			}
		})
	}, nil
}

// sum is what is held for a model, dropping holds past their time.
func (r *SpendReserver) sum(modelID string, now time.Time) int64 {
	var total int64
	for id, h := range r.held[modelID] {
		if now.Sub(h.at) > holdTTL {
			delete(r.held[modelID], id)
			continue
		}
		total += h.micros
	}
	return total
}

// Held is what is held for a model in micro-dollars, for tests and the log.
func (r *SpendReserver) Held(modelID string) int64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.sum(modelID, r.g.now())
}

// microsNumeric is a whole number of millionths of a dollar as a numeric.
func microsNumeric(micros int64) pgtype.Numeric {
	return pgtype.Numeric{Int: big.NewInt(micros), Exp: -capacityMicros, Valid: true}
}
