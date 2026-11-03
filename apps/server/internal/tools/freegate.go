package tools

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/qunqin24/openchat/apps/server/internal/agent"
)

// FreeGate paces the keyless search services for the whole process. Their
// free tiers are rate-limited per caller address, and every user of this
// server shares its one address, so a deep research's researchers searching
// at once would otherwise trip them all (docs/design/deep-research-plan.md §2).
//
// Each service gets a few requests in flight at most, and one that refused or
// failed is left alone for a while - a rate limit for a minute, a refusal of
// anonymous callers for ten - instead of being asked again by the next search.
type FreeGate struct {
	// MaxInFlight is how many requests one service may have going at once.
	MaxInFlight int
	// Wait bounds how long a search waits for a service with room before it
	// gives up; services that are cooling down are not waited for.
	Wait time.Duration

	mu    sync.Mutex
	busy  map[string]int
	until map[string]time.Time
	now   func() time.Time
}

// DefaultFreeGate is the process's gate, shared by every search.
var DefaultFreeGate = &FreeGate{MaxInFlight: 2, Wait: 15 * time.Second}

func (g *FreeGate) clock() time.Time {
	if g.now != nil {
		return g.now()
	}
	return time.Now()
}

// take claims room on a service. cooling is how long it is still left alone,
// zero when it is merely busy.
func (g *FreeGate) take(name string) (ok bool, cooling time.Duration) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.busy == nil {
		g.busy, g.until = map[string]int{}, map[string]time.Time{}
	}
	if left := g.until[name].Sub(g.clock()); left > 0 {
		return false, left
	}
	if g.busy[name] >= max(g.MaxInFlight, 1) {
		return false, 0
	}
	g.busy[name]++
	return true, 0
}

func (g *FreeGate) release(name string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.busy[name]--
}

// cool leaves a service alone for d.
func (g *FreeGate) cool(name string, d time.Duration) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if until := g.clock().Add(d); until.After(g.until[name]) {
		g.until[name] = until
	}
}

// coolFor is how long a service that failed so is left alone.
func coolFor(err error) time.Duration {
	var vendorErr *freeError
	if !errors.As(err, &vendorErr) {
		return 0
	}
	switch {
	case vendorErr.status == 401 || vendorErr.status == 402 || vendorErr.status == 403:
		return 10 * time.Minute
	case vendorErr.status == 429 || rateLimited.MatchString(vendorErr.msg):
		return time.Minute
	case vendorErr.passOn:
		return 15 * time.Second
	}
	return 0
}

// errSearchCooling is every service resting: the model is told to read what it
// has rather than search again at once.
func errSearchCooling(wait time.Duration) error {
	return agent.Errorf("Web search is rate-limited right now; it should be available again in about %d seconds. "+
		"Meanwhile work with the sources you already have, or read pages you know of with web_fetch.",
		int(wait.Round(time.Second)/time.Second)+1)
}

// pause waits a moment for room on a busy service, or until ctx ends.
func pause(ctx context.Context) error {
	t := time.NewTimer(250 * time.Millisecond)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

