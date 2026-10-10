package api

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/qunqin/openchat/apps/server/internal/usage"
)

// Money held back for requests in flight against a cap. At $3 per million input
// tokens, an estimate of 40 000 input tokens is held at $0.12.

func (f *capFixture) reserve(estimate int, admin bool) (func(), error) {
	f.t.Helper()
	who := f.member.ID
	if admin {
		who = f.admin.ID
	}
	return f.reserver.Reserve(context.Background(), usage.Call{UserID: who, ModelID: f.modelID, InputEstimate: estimate})
}

func TestReservationsCountRequestsInFlightAgainstTheCap(t *testing.T) {
	f := newCapFixtureWith(t, true, true)
	f.mustSetCap(`{"capacity":0.25,"capacityPeriod":"total","capacityAdminExempt":true}`)
	f.spend(money("0.10"), time.Now().Add(-time.Minute), "chat", &f.member.ID)

	first, err := f.reserve(40_000, false) // 0.10 spent + 0.12 held
	if err != nil {
		t.Fatalf("the first request: %v", err)
	}
	if _, err := f.reserve(40_000, false); !errors.Is(err, usage.ErrCapacity) { // 0.10 + 0.12 + 0.12
		t.Fatalf("a second request with the first still out: %v, want the capacity error", err)
	}
	if got := f.reserver.Held(f.modelID); got != 120_000 {
		t.Errorf("held %d micro-dollars, want 120000: a refused request holds nothing", got)
	}

	// An administrator the cap exempts is let through - and held, since what
	// they spend counts against everyone else.
	adminHold, err := f.reserve(40_000, true)
	if err != nil {
		t.Fatalf("an exempt administrator: %v", err)
	}
	if got := f.reserver.Held(f.modelID); got != 240_000 {
		t.Errorf("held %d, want 240000 with the administrator's", got)
	}
	adminHold()

	// The first request ends - its row is written, its hold given back - and
	// there is room for another again.
	first()
	first() // idempotent
	if got := f.reserver.Held(f.modelID); got != 0 {
		t.Errorf("held %d after both were released", got)
	}
	second, err := f.reserve(40_000, false)
	if err != nil {
		t.Fatalf("after the first ended: %v", err)
	}
	second()
}

func TestReservationOfAnUncappedOrUnpricedModelHoldsNothing(t *testing.T) {
	uncapped := newCapFixtureWith(t, true, true)
	hold, err := uncapped.reserve(10_000_000, false)
	if err != nil {
		t.Fatal(err)
	}
	hold()
	if got := uncapped.reserver.Held(uncapped.modelID); got != 0 {
		t.Errorf("an uncapped model held %d", got)
	}

	// No price: its requests carry no cost, so there is nothing to hold or to
	// count, as for the cap itself.
	unpriced := newCapFixtureWith(t, false, true)
	unpriced.mustSetCap(`{"capacity":0.01,"capacityPeriod":"total"}`)
	if hold, err = unpriced.reserve(10_000_000, false); err != nil {
		t.Fatalf("an unpriced model: %v", err)
	}
	hold()
}

// Many requests starting together: only as many as fit under the cap are let
// go, however they interleave.
func TestReservationsAreRaceSafe(t *testing.T) {
	f := newCapFixtureWith(t, true, true)
	f.mustSetCap(`{"capacity":0.25,"capacityPeriod":"total"}`)

	var allowed, refused atomic.Int32
	var wg sync.WaitGroup
	start := make(chan struct{})
	for range 24 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			if _, err := f.reserve(30_000, false); err == nil { // $0.09 each
				allowed.Add(1)
			} else if errors.Is(err, usage.ErrCapacity) {
				refused.Add(1)
			} else {
				t.Error(err)
			}
		}()
	}
	close(start)
	wg.Wait()
	if allowed.Load() != 2 || refused.Load() != 22 {
		t.Fatalf("%d let go and %d refused; $0.25 holds two of $0.09 and not three", allowed.Load(), refused.Load())
	}
}

// A hold is gone after holdTTL even if nobody released it.
func TestAStaleReservationExpires(t *testing.T) {
	f := newCapFixtureWith(t, true, true)
	f.mustSetCap(`{"capacity":0.25,"capacityPeriod":"total"}`)
	if _, err := f.reserve(40_000, false); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	f.reserver.g.now = func() time.Time { return now.Add(holdTTL + time.Minute) }
	if got := f.reserver.Held(f.modelID); got != 0 {
		t.Errorf("a hold %d micro-dollars strong outlived its time", got)
	}
}

// End to end: a request is out and holds its cost, so a second run that would
// be the one to cross the cap is stopped before its request is made - which
// the ledger alone could not do, since nothing is written until a request ends.
func TestAConcurrentRunIsStoppedByTheHeldCost(t *testing.T) {
	f := newCapFixtureWith(t, true, true)
	f.hold = make(chan struct{})
	f.promptTokens = 10
	f.mustSetCap(`{"capacity":1,"capacityPeriod":"total"}`)

	first, _ := f.send(f.member)
	f.waitHeld(1)
	held := f.reserver.Held(f.modelID)
	if held <= 0 {
		t.Fatalf("a request in flight holds %d", held)
	}
	// Room for one request of that size and not two.
	f.mustSetCap(fmt.Sprintf(`{"capacity":%.6f,"capacityPeriod":"total"}`, float64(held)*1.5/1e6))

	second, conv := f.send(f.member)
	status, code := f.finished(second)
	if status != "failed" || code == nil || *code != "model_capacity_reached" {
		t.Fatalf("the second run is %s (%v), want failed with model_capacity_reached", status, code)
	}
	if f.requests.Load() != 1 {
		t.Errorf("the endpoint was asked %d times; the refused request must never be sent", f.requests.Load())
	}
	if rows := f.usageRows("chat"); len(rows) != 0 {
		t.Errorf("the refused request left %d ledger rows", len(rows))
	}
	_ = conv

	// The first finishes: its hold goes with its row, and the next run fits.
	close(f.hold)
	if status, _ := f.finished(first); status != "completed" {
		t.Fatalf("the first run is %s", status)
	}
	deadline := time.Now().Add(5 * time.Second)
	for f.reserver.Held(f.modelID) != 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if got := f.reserver.Held(f.modelID); got != 0 {
		t.Errorf("%d still held after every request ended", got)
	}
	if status, code := f.finished(mustSend(f)); status != "completed" {
		t.Errorf("a run after the first ended: %s (%v)", status, code)
	}
}
