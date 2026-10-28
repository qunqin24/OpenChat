package imagesearch

import (
	"context"
	"errors"
	"sync"
	"time"
)

// Fallback asks several services in turn, so one that is down - Commons'
// search answers 503 for minutes at a time, Openverse does not answer from
// some networks - does not leave answers without pictures. It remembers,
// across searches, which services failed lately and asks those last.
type Fallback struct {
	// Rest is how long a service that failed is asked after the others; a
	// minute when zero.
	Rest time.Duration

	mu     sync.Mutex
	failed map[Searcher]time.Time
}

// Of is a search that asks the searchers in the order given, those that failed
// lately last. The next one is asked when one cannot search or finds nothing;
// nothing is found only when every one answered that, and the search is
// unavailable when any could not answer and none found a picture.
func (f *Fallback) Of(searchers ...Searcher) Searcher {
	if len(searchers) == 1 {
		return searchers[0]
	}
	return chain{f, searchers}
}

type chain struct {
	f         *Fallback
	searchers []Searcher
}

func (c chain) Search(ctx context.Context, query string) (*Image, error) {
	var failure error
	for _, searcher := range c.f.order(c.searchers, time.Now()) {
		found, err := searcher.Search(ctx, query)
		c.f.note(searcher, err, time.Now())
		switch {
		case err != nil:
			failure = errors.Join(failure, err)
		case found != nil:
			return found, nil
		}
		if ctx.Err() != nil {
			break
		}
	}
	return nil, failure
}

// order is the searchers as given, those resting after a failure moved last.
func (f *Fallback) order(searchers []Searcher, now time.Time) []Searcher {
	rest := f.Rest
	if rest == 0 {
		rest = time.Minute
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	ready := make([]Searcher, 0, len(searchers))
	var resting []Searcher
	for _, searcher := range searchers {
		if at, ok := f.failed[searcher]; ok && now.Sub(at) < rest {
			resting = append(resting, searcher)
		} else {
			ready = append(ready, searcher)
		}
	}
	return append(ready, resting...)
}

func (f *Fallback) note(searcher Searcher, err error, now time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err == nil {
		delete(f.failed, searcher)
		return
	}
	if f.failed == nil {
		f.failed = map[Searcher]time.Time{}
	}
	f.failed[searcher] = now
}
