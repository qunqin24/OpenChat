package api

import (
	"context"
	"errors"
	"math"
	"math/big"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/qunqin24/openchat/apps/server/internal/db/sqlc"
	"github.com/qunqin24/openchat/apps/server/internal/httpx"
)

// A model's spending cap.
//
// The operator gives a model a capacity in USD and a period (total, monthly,
// daily). What counts against it is the model's `cost` in the usage ledger
// since the period began: every logged request for that model, chat or title,
// from every user, administrators included. Once that reaches the
// capacity the model cannot start a run or make another request inside one,
// for anyone the cap applies to: everybody, except administrators when the
// cap's exemption switch is on (their spend still counts).
//
// Two choices worth knowing before changing anything here:
//
//   - The cap counts by the *requested* model id (usage_logs.model_id), not by
//     usage_logs.served_model. The requested id is how a model is offered,
//     picked, stored on a conversation and configured here, and it is what the
//     operator's settings are keyed on. served_model is whatever string the
//     provider echoes back (a dated variant, a relay's own name); counting by
//     it would let one configured model spend under several names, or a cap
//     be missed for a relay that echoes nothing.
//   - The ledger is written when a request ends, so a request in flight is not
//     in the sum yet. The check before a run starts and before each request
//     therefore sees only what has ended; what is out is held back separately
//     (spend_reserve.go), and how many runs can be out at once is bounded
//     (run_limits.go). What is left over is the output of requests in flight.
//
// A model whose requests carry no cost (no input or output price in the
// catalogue) has a sum of zero: its cap can never trigger. The settings panel
// says so. A request whose provider reported no usage is not such a request: it
// is estimated and priced like the rest (usage.Meter).

// errModelCapacity is the refusal of a model that has reached its cap. It is
// 409, not 403: nothing is wrong with the caller, the model is unavailable
// until the period turns or an operator acts.
var errModelCapacity = httpx.NewError(http.StatusConflict, "model_capacity_reached",
	"this model has reached its maximum capacity")

// capacityMicros is the precision a cap is entered with: millionths of a dollar.
const capacityMicros = 6

// maxCapacity is the largest cap accepted, in USD. Far beyond any real budget;
// it exists so the micro-dollar integer below cannot overflow.
const maxCapacity = 1e9

// capacityRule is a model's cap as stored.
type capacityRule struct {
	limit  pgtype.Numeric
	period CapacityPeriod
	// exempt lets administrators through.
	exempt bool
	// since is where a total cap started counting; nil counts all history.
	since *time.Time
}

// capacityOf reads the cap off an override row; ok is false when it has none.
func capacityOf(row sqlc.ModelOverride) (capacityRule, bool) {
	if !row.Capacity.Valid {
		return capacityRule{}, false
	}
	rule := capacityRule{
		limit:  row.Capacity,
		period: CapacityPeriod(row.CapacityPeriod),
		exempt: row.CapacityAdminExempt,
	}
	if row.CapacitySince.Valid {
		since := row.CapacitySince.Time
		rule.since = &since
	}
	return rule, true
}

// appliesTo is whether the cap holds for a requester.
func (r capacityRule) appliesTo(admin bool) bool { return !(admin && r.exempt) }

// windowStart is where the spend counted against the cap begins at now, or nil
// for all of the model's history.
//
// Daily and monthly periods begin at local midnight of the day or the first of
// the month, in now's own zone (the server's, time.Local, set by TZ). They are
// built with time.Date rather than by subtracting a duration, so a day that is
// 23 or 25 hours long where clocks change still starts at its midnight.
func (r capacityRule) windowStart(now time.Time) *time.Time {
	year, month, day := now.Date()
	switch r.period {
	case Daily:
		start := time.Date(year, month, day, 0, 0, 0, 0, now.Location())
		return &start
	case Monthly:
		start := time.Date(year, month, 1, 0, 0, 0, 0, now.Location())
		return &start
	}
	return r.since
}

// capacityState is a capped model's spend.
type capacityState struct {
	spent   pgtype.Numeric
	reached bool
	// start is the beginning of the window that was counted; nil is all history.
	start *time.Time
}

// capacityGuard answers whether models are at their caps.
type capacityGuard struct {
	q   *sqlc.Queries
	now func() time.Time
}

func newCapacityGuard(q *sqlc.Queries) capacityGuard {
	return capacityGuard{q: q, now: time.Now}
}

// capacity is the server's guard, on its own queries and clock.
func (s *Server) capacity() capacityGuard {
	g := newCapacityGuard(s.q)
	if s.now != nil {
		g.now = s.now
	}
	return g
}

// states measures the spend of every capped model in rules with one query.
func (g capacityGuard) states(ctx context.Context, rules map[string]capacityRule) (map[string]capacityState, error) {
	out := make(map[string]capacityState, len(rules))
	if len(rules) == 0 {
		return out, nil
	}
	now := g.now()
	params := sqlc.ModelSpendsParams{}
	starts := make(map[string]*time.Time, len(rules))
	for id, rule := range rules {
		start := rule.windowStart(now)
		starts[id] = start
		params.ModelIds = append(params.ModelIds, id)
		params.Since = append(params.Since, pgtype.Timestamptz{Time: derefTime(start), Valid: start != nil})
		params.Capacity = append(params.Capacity, rule.limit)
	}
	rows, err := g.q.ModelSpends(ctx, params)
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		out[row.ModelID] = capacityState{spent: row.Spent, reached: row.Reached, start: starts[row.ModelID]}
	}
	return out, nil
}

func derefTime(t *time.Time) time.Time {
	if t == nil {
		return time.Time{}
	}
	return *t
}

// reached is whether modelID has hit its cap for a requester: an administrator
// or not. A model with no cap, or one that exempts this requester, has not.
func (g capacityGuard) reached(ctx context.Context, modelID string, admin bool) (bool, error) {
	row, err := g.q.GetModelOverride(ctx, modelID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	rule, ok := capacityOf(row)
	if !ok || !rule.appliesTo(admin) {
		return false, nil
	}
	states, err := g.states(ctx, map[string]capacityRule{modelID: rule})
	if err != nil {
		return false, err
	}
	return states[modelID].reached, nil
}

// isAdmin is whether a user is an administrator. The zero id and a user that
// is gone are not.
func (g capacityGuard) isAdmin(ctx context.Context, userID uuid.UUID) (bool, error) {
	if userID == uuid.Nil {
		return false, nil
	}
	user, err := g.q.GetUser(ctx, userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return user.Role == sqlc.UserRoleAdmin, nil
}

// NewCapacityCheck is the runner's cap check: whether the model a run is about
// to call has reached its cap for the run's owner. Asked before every model
// request of a run, so a long tool loop stops at the cap.
func NewCapacityCheck(pool *pgxpool.Pool) func(ctx context.Context, userID uuid.UUID, modelID string) (bool, error) {
	g := newCapacityGuard(sqlc.New(pool))
	return func(ctx context.Context, userID uuid.UUID, modelID string) (bool, error) {
		admin, err := g.isAdmin(ctx, userID)
		if err != nil {
			return false, err
		}
		return g.reached(ctx, modelID, admin)
	}
}

// asker is who a model is wanted for.
type asker struct {
	// staff sees the models kept for administrators: an administrator, or one
	// of the server's own tasks, which no person asked for.
	staff bool
	// admin is an administrator, so a cap that exempts them lets them through.
	// A server task that acts for a person is an administrator's only when the
	// person is.
	admin bool
	// ignoreCap asks only whether the model can be answered, not whether it
	// has money left - saving the task model, say.
	ignoreCap bool
}

// askerFrom is the person behind a request.
func askerFrom(ctx context.Context) asker {
	staff := isStaff(ctx)
	return asker{staff: staff, admin: staff}
}

// capacityRules reads the caps off a set of override rows.
func capacityRules(overrides map[string]sqlc.ModelOverride) map[string]capacityRule {
	rules := make(map[string]capacityRule)
	for id, row := range overrides {
		if rule, ok := capacityOf(row); ok {
			rules[id] = rule
		}
	}
	return rules
}

// capacityParams turns the settings body's cap into the columns. A cap cleared
// (or never set) writes the defaults for the period and the exemption: they
// mean nothing without one, and no state should hide behind an uncapped model.
func capacityParams(body ModelOverride) (limit pgtype.Numeric, period string, exempt bool, err error) {
	period, exempt = string(Total), true
	if body.Capacity == nil {
		return limit, period, exempt, nil
	}
	invalid := func(message string) error {
		return httpx.NewFieldError("capacity", "invalid_capacity", message)
	}
	value := *body.Capacity
	if math.IsNaN(value) || math.IsInf(value, 0) || value <= 0 || value > maxCapacity {
		return limit, period, exempt, invalid("a spending cap must be a positive amount")
	}
	// Whole millionths of a dollar, built as an integer: the cap is compared
	// with the ledger's numeric sum and must carry no float tail.
	scaled := value * math.Pow10(capacityMicros)
	micros := math.Round(scaled)
	if math.Abs(scaled-micros) > 1e-3 {
		return limit, period, exempt, invalid("a spending cap has at most six decimals")
	}
	if micros < 1 {
		return limit, period, exempt, invalid("a spending cap must be a positive amount")
	}
	limit = pgtype.Numeric{Int: big.NewInt(int64(micros)), Exp: -capacityMicros, Valid: true}

	if body.CapacityPeriod != nil {
		if !body.CapacityPeriod.Valid() {
			return limit, period, exempt, httpx.NewFieldError("capacityPeriod", "invalid_capacity_period",
				"unknown period: "+string(*body.CapacityPeriod))
		}
		period = string(*body.CapacityPeriod)
	}
	if body.CapacityAdminExempt != nil {
		exempt = *body.CapacityAdminExempt
	}
	return limit, period, exempt, nil
}

// numberOf is a numeric as the float the API reports it as.
func numberOf(n pgtype.Numeric) float64 {
	f, err := n.Float64Value()
	if err != nil || !f.Valid {
		return 0
	}
	return f.Float64
}

// toModelCapacity is how much of a cap is used, as the API reports it.
func toModelCapacity(state capacityState) ModelCapacity {
	return ModelCapacity{
		Spent:       numberOf(state.spent),
		AtCapacity:  state.reached,
		WindowStart: state.start,
	}
}

// ResetModelCapacity counts a total cap again from now.
func (s *Server) ResetModelCapacity(w http.ResponseWriter, r *http.Request, modelId string) {
	if _, err := s.q.ResetModelCapacity(r.Context(), sqlc.ResetModelCapacityParams{
		ModelID: modelId,
		Since:   pgtype.Timestamptz{Time: s.capacity().now(), Valid: true},
	}); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// refusedForCapacity is whether the requester in ctx is refused modelID for
// having reached its cap.
func (s *Server) refusedForCapacity(ctx context.Context, modelID string, who asker) error {
	if who.ignoreCap {
		return nil
	}
	reached, err := s.capacity().reached(ctx, modelID, who.admin)
	if err != nil {
		return err
	}
	if reached {
		return errModelCapacity
	}
	return nil
}
