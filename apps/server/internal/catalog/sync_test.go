package catalog

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"
)

// fakeStore stands in for the database.
//
// It keeps a settings map next to the snapshot for the same reason the schema
// has model_settings next to the catalogue tables: to make visible the thing a
// sync is not allowed to disturb.
type fakeStore struct {
	state     State
	providers []Provider
	settings  map[string]bool
	replaced  int
}

func (f *fakeStore) LoadState(context.Context) (State, error) { return f.state, nil }

func (f *fakeStore) SaveState(_ context.Context, state State) error {
	f.state = state
	return nil
}

func (f *fakeStore) Replace(_ context.Context, providers []Provider) error {
	f.providers = providers
	f.replaced++
	return nil
}

func syncer(t *testing.T, store Store, handler http.HandlerFunc) *Syncer {
	t.Helper()
	return NewSyncer(serveFixture(t, handler), store)
}

func servingFixture(t *testing.T) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", `"v2"`)
		_, _ = w.Write(fixture(t))
	}
}

func TestRefreshReplacesSnapshot(t *testing.T) {
	store := &fakeStore{}
	state, err := syncer(t, store, servingFixture(t)).Refresh(context.Background())
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}

	if store.replaced != 1 || len(store.providers) != 3 {
		t.Fatalf("snapshot = %d providers after %d replacements", len(store.providers), store.replaced)
	}
	if state.FetchedAt.IsZero() || state.ETag != `"v2"` {
		t.Errorf("state = %+v, want the fetch time and the new etag", state)
	}
	if state.LastError != "" {
		t.Errorf("lastError = %q after a successful refresh", state.LastError)
	}
}

// The point of the whole arrangement: an unreachable models.dev must cost us
// the refresh, not the catalogue.
func TestFailedFetchKeepsPreviousSnapshot(t *testing.T) {
	fetchedAt := time.Now().Add(-48 * time.Hour)
	previous := []Provider{{ID: "anthropic", Name: "Anthropic"}}
	store := &fakeStore{
		state:     State{FetchedAt: fetchedAt, ETag: `"v1"`},
		providers: previous,
	}

	state, err := syncer(t, store, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "down", http.StatusInternalServerError)
	}).Refresh(context.Background())

	if err == nil {
		t.Fatal("a failed fetch was reported as a successful refresh")
	}
	if !errors.Is(err, ErrUpstream) {
		t.Errorf("error = %v, want it marked as an upstream failure", err)
	}
	if store.replaced != 0 || len(store.providers) != 1 || store.providers[0].ID != "anthropic" {
		t.Errorf("snapshot = %+v, want the previous one untouched", store.providers)
	}
	if !state.FetchedAt.Equal(fetchedAt) || state.ETag != `"v1"` {
		t.Errorf("state = %+v, want the previous fetch time and etag kept", state)
	}
	if state.LastError == "" {
		t.Error("a failed fetch left nothing to explain itself with")
	}
	if store.state.LastError != state.LastError {
		t.Error("the failure was reported but not recorded")
	}
}

// model_settings is this installation's decision. A sync replaces a cache of
// somebody else's file and has no business switching anything on or off, not
// even for a model that has since vanished upstream.
func TestSyncLeavesModelSettingsAlone(t *testing.T) {
	store := &fakeStore{
		settings: map[string]bool{
			"openai/gpt-4o":     true,
			"legacy/model-gone": true,
		},
	}

	if _, err := syncer(t, store, servingFixture(t)).Refresh(context.Background()); err != nil {
		t.Fatalf("refresh: %v", err)
	}

	if len(store.settings) != 2 || !store.settings["openai/gpt-4o"] || !store.settings["legacy/model-gone"] {
		t.Errorf("settings = %v, want them untouched by the sync", store.settings)
	}
}

func TestNotModifiedKeepsSnapshot(t *testing.T) {
	store := &fakeStore{
		state:     State{FetchedAt: time.Now().Add(-48 * time.Hour), ETag: `"v1"`},
		providers: []Provider{{ID: "anthropic"}},
	}

	state, err := syncer(t, store, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotModified)
	}).Refresh(context.Background())
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}

	if store.replaced != 0 {
		t.Error("a 304 rewrote the snapshot it said was current")
	}
	// The check did happen, so the snapshot is as fresh as if it had been
	// replaced. Otherwise every refresh after the first would fetch again.
	if state.Stale(time.Now()) || state.ETag != `"v1"` {
		t.Errorf("state = %+v, want it fresh and still holding the etag", state)
	}
}

func TestRefreshIfStaleSkipsAFreshSnapshot(t *testing.T) {
	fetches := 0
	store := &fakeStore{state: State{FetchedAt: time.Now().Add(-time.Hour)}}
	s := syncer(t, store, func(w http.ResponseWriter, r *http.Request) {
		fetches++
		_, _ = w.Write(fixture(t))
	})

	if _, err := s.RefreshIfStale(context.Background()); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if fetches != 0 {
		t.Errorf("fetches = %d, want an hour-old snapshot left alone", fetches)
	}

	// One second past the threshold is enough; the boundary is what the boot
	// path turns on.
	store.state.FetchedAt = time.Now().Add(-StaleAfter - time.Second)
	if _, err := s.RefreshIfStale(context.Background()); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if fetches != 1 {
		t.Errorf("fetches = %d, want a stale snapshot refreshed", fetches)
	}
}

// A database that has never fetched the catalogue has no state at all, which
// has to read as stale or the boot refresh would never fire on a new install.
func TestEmptyStateIsStale(t *testing.T) {
	if !(State{}).Stale(time.Now()) {
		t.Error("a snapshot that was never fetched reported as fresh")
	}
}
