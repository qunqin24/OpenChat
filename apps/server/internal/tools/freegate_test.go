package tools

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

// A service that rate-limited is left alone for the next searches, and when
// every service rests the search says for how long instead of asking them.
func TestFreeGateRestsAServiceThatRefused(t *testing.T) {
	var (
		mu    sync.Mutex
		calls []string
	)
	vendor := func(name string, err error) FreeVendor {
		return FreeVendor{Name: name, Search: func(context.Context, *http.Client, SearchRequest) ([]SearchResult, error) {
			mu.Lock()
			calls = append(calls, name)
			mu.Unlock()
			if err != nil {
				return nil, err
			}
			return []SearchResult{{Title: name}}, nil
		}}
	}
	now := time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC)
	gate := &FreeGate{MaxInFlight: 2, Wait: time.Second, now: func() time.Time { return now }}
	limited := &freeError{vendor: "a", msg: "HTTP 429", passOn: true, status: 429}
	search := FreeSearch{Gate: gate, Vendors: []FreeVendor{vendor("a", limited), vendor("b", nil)}}

	for range 4 {
		if _, err := search.Search(context.Background(), SearchRequest{Query: "q"}); err != nil {
			t.Fatal(err)
		}
	}
	asked := 0
	for _, c := range calls {
		if c == "a" {
			asked++
		}
	}
	if asked != 1 {
		t.Fatalf("the rate-limited service was asked %d times in a minute, want once (%v)", asked, calls)
	}

	// Both resting: no service is asked, and the model hears how long.
	gate.cool("b", 30*time.Second)
	calls = nil
	_, err := search.Search(context.Background(), SearchRequest{Query: "q"})
	if err == nil || !strings.Contains(err.Error(), "rate-limited") || len(calls) != 0 {
		t.Fatalf("all resting: err %v, calls %v", err, calls)
	}

	// A minute on, the first is asked again.
	now = now.Add(61 * time.Second)
	calls = nil
	if _, err := search.Search(context.Background(), SearchRequest{Query: "q"}); err == nil && len(calls) == 0 {
		t.Fatal("no service was asked once the rest was over")
	}
}

// A service with its requests in flight is waited for, not counted as failed.
func TestFreeGateWaitsForABusyService(t *testing.T) {
	gate := &FreeGate{MaxInFlight: 1, Wait: 2 * time.Second}
	release := make(chan struct{})
	started := make(chan struct{}, 2)
	slow := FreeVendor{Name: "only", Search: func(context.Context, *http.Client, SearchRequest) ([]SearchResult, error) {
		started <- struct{}{}
		<-release
		return []SearchResult{{Title: "x"}}, nil
	}}
	search := FreeSearch{Gate: gate, Vendors: []FreeVendor{slow}}
	errs := make(chan error, 2)
	for range 2 {
		go func() {
			_, err := search.Search(context.Background(), SearchRequest{Query: "q"})
			errs <- err
		}()
	}
	<-started
	time.Sleep(50 * time.Millisecond)
	close(release)
	for range 2 {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}
}

type memoryStore struct {
	mu   sync.Mutex
	rows map[string][]byte
}

func (m *memoryStore) GetSearch(_ context.Context, key string) ([]byte, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	raw, ok := m.rows[key]
	return raw, ok
}

func (m *memoryStore) PutSearch(_ context.Context, key string, raw []byte, _ time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rows[key] = raw
}

type countingSearcher struct{ n int }

func (c *countingSearcher) Search(context.Context, SearchRequest) ([]SearchResult, error) {
	c.n++
	return []SearchResult{{Title: "t", URL: "https://x.dev", Content: "page text", Published: "2026-01-02"}}, nil
}

// The same search, however spaced or cased, is answered from the store with
// the page text; a different period is not the same search.
func TestCachedSearch(t *testing.T) {
	backend := &countingSearcher{}
	search := CachedSearch{Searcher: backend, Store: &memoryStore{rows: map[string][]byte{}}, Backend: "free"}
	ctx := context.Background()
	if _, err := search.Search(ctx, SearchRequest{Query: "Go  generics", Limit: 5}); err != nil {
		t.Fatal(err)
	}
	got, err := search.Search(ctx, SearchRequest{Query: "go generics", Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	if backend.n != 1 || len(got) != 1 || got[0].Content != "page text" || got[0].Published != "2026-01-02" {
		t.Fatalf("backend asked %d times, got %+v", backend.n, got)
	}
	if _, err := search.Search(ctx, SearchRequest{Query: "go generics", Limit: 5, Recency: "week"}); err != nil {
		t.Fatal(err)
	}
	if backend.n != 2 {
		t.Fatal("a search limited to a period was answered with an unlimited one's results")
	}
	if searchTTL(SearchRequest{Recency: "day"}) != time.Hour {
		t.Fatal("a search of the last day is kept a day")
	}
}

func TestParsePlan(t *testing.T) {
	plan, err := ParsePlan([]byte(`{"title":"  Compare   X ","steps":["a"," ","b"]}`))
	if err != nil || plan.Title != "Compare X" || len(plan.Steps) != 2 {
		t.Fatalf("plan %+v err %v", plan, err)
	}
	for _, bad := range []string{`{"title":"","steps":["a","b"]}`, `{"title":"x","steps":["a"]}`, `{"title":"x","steps":["1","2","3","4","5","6","7","8","9"]}`} {
		if _, err := ParsePlan([]byte(bad)); err == nil {
			t.Fatalf("%s was accepted", bad)
		}
	}
}
