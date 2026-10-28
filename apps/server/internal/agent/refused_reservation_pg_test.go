package agent

import (
	"context"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/qunqin24/openchat/apps/server/internal/db/sqlc"
	ledger "github.com/qunqin24/openchat/apps/server/internal/usage"
)

type refusingReserver struct{ asked atomic.Int32 }

func (r *refusingReserver) Reserve(context.Context, ledger.Call) (func(), error) {
	r.asked.Add(1)
	return nil, ledger.ErrCapacity
}

// settledGoroutines is the goroutine count once it has stopped moving.
func settledGoroutines() int {
	last := -1
	for stable := 0; stable < 10; {
		runtime.GC()
		time.Sleep(20 * time.Millisecond)
		if n := runtime.NumGoroutine(); n == last {
			stable++
		} else {
			last, stable = n, 0
		}
	}
	return last
}

// A request refused for the model's spending cap makes no model call and must
// leave nothing running: the throttle goroutine of its partial stream used to
// be started before the reservation and never stopped when it was refused.
func TestRefusedReservationLeavesNoGoroutine(t *testing.T) {
	reserver := &refusingReserver{}
	f := newFixtureWith(t, func(o *Options) { o.Reserver = reserver }, nil)
	one := func() {
		run := f.admit("hi")
		f.runner.Start(run.ID)
		got := f.waitFor(run.ID, sqlc.RunStatusFailed)
		if got.ErrorCode == nil || *got.ErrorCode != CodeModelCapacity {
			t.Fatalf("run ended with %v, want %s", got.ErrorCode, CodeModelCapacity)
		}
	}
	one() // warm up: the pool, the hub and the runner start what they keep
	before := settledGoroutines()
	const attempts = 6
	for range attempts {
		one()
	}
	after := settledGoroutines()
	if after > before {
		buf := make([]byte, 1<<16)
		t.Fatalf("goroutines: %d before %d refused requests, %d after\n%s", before, attempts, after, buf[:runtime.Stack(buf, true)])
	}
	if got := reserver.asked.Load(); got != attempts+1 {
		t.Errorf("reserver asked %d times, want %d", got, attempts+1)
	}
	if got := f.models.calls.Load(); got != 0 {
		t.Errorf("%d model calls for refused requests", got)
	}
}

// The throttle ends with the request's context even when nobody closes it.
func TestPartialStreamEndsWithItsContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	s := newPartialStream(ctx, nil, 0, time.Hour)
	cancel()
	select {
	case <-s.done:
	case <-time.After(2 * time.Second):
		t.Fatal("the throttle goroutine outlived its context")
	}
}
