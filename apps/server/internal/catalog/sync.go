package catalog

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// ErrUpstream marks a refresh that failed at the fetch rather than at the
// database. The two deserve different blame: one is models.dev being
// unreachable, the other is us.
var ErrUpstream = errors.New("catalogue fetch failed")

// Syncer replaces the local snapshot with what upstream is serving.
type Syncer struct {
	client *Client
	store  Store
	// now is a field so a test can decide what counts as stale without waiting
	// a day for it.
	now func() time.Time
}

func NewSyncer(client *Client, store Store) *Syncer {
	return &Syncer{client: client, store: store, now: time.Now}
}

// Refresh fetches the catalogue and replaces the snapshot.
//
// A fetch that fails leaves the previous snapshot in place and records why. A
// catalogue a week old still answers every question the panel asks of it; an
// empty one answers none. The state is returned alongside the error so a caller
// can report both what failed and what is still being served.
func (s *Syncer) Refresh(ctx context.Context) (State, error) {
	state, err := s.store.LoadState(ctx)
	if err != nil {
		return State{}, err
	}

	result, fetchErr := s.client.Fetch(ctx, state.ETag)
	if fetchErr != nil {
		fetchErr = fmt.Errorf("%w: %w", ErrUpstream, fetchErr)
		state.LastError = fetchErr.Error()
		if err := s.store.SaveState(ctx, state); err != nil {
			return state, errors.Join(fetchErr, err)
		}
		return state, fetchErr
	}

	// A 304 means the file we already parsed is the current one. Nothing to
	// replace, but the check did happen, so the snapshot is as fresh as if it
	// had been rewritten.
	if !result.NotModified {
		if err := s.store.Replace(ctx, result.Providers); err != nil {
			// Deliberately not recorded as a last error: the failure is ours,
			// and the place the note would go is the database that just refused
			// a write.
			return state, err
		}
	}

	state = State{FetchedAt: s.now(), ETag: result.ETag}
	if err := s.store.SaveState(ctx, state); err != nil {
		return state, err
	}
	return state, nil
}

// RefreshIfStale refreshes only when the snapshot is missing or older than
// StaleAfter, and reports the state either way.
func (s *Syncer) RefreshIfStale(ctx context.Context) (State, error) {
	state, err := s.store.LoadState(ctx)
	if err != nil {
		return State{}, err
	}
	if !state.Stale(s.now()) {
		return state, nil
	}
	return s.Refresh(ctx)
}
