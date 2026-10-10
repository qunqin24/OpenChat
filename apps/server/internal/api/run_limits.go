package api

import (
	"context"
	"fmt"
	"net/http"

	"github.com/google/uuid"

	"github.com/qunqin/openchat/apps/server/internal/db/sqlc"
	"github.com/qunqin/openchat/apps/server/internal/httpx"
)

// How many runs may be going at once.
//
// A conversation runs one thing at a time, but nothing else bounded the runs
// of one account: someone could open a hundred conversations and send into all
// of them, each a stream of model requests that cost money and are not in the
// ledger until they end. A guest has no account at all and starts a run per
// request, so the same is a script. The limit is on runs *going* (status
// `running`), counted across all of an account's conversations - and, for a
// guest, across all of an address's.
//
// It is enforced where a run is created (createRun), inside the transaction
// that creates it, and so by every way of starting one: send, regenerate,
// edit, answering a question, and a guest's run. The transaction takes an
// advisory lock on the account (the address, for a guest) before it counts, and
// keeps it until it commits the new run; a second request for the same account
// waits for the lock and then counts the first one's run. Two requests with one
// slot left cannot both pass, on one server or several.
//
// A refusal rolls the transaction back, so nothing of the request is written.

const (
	// settingMaxRuns is how many runs one account may have going at once; zero
	// is no limit. Administrators are exempt.
	settingMaxRuns      = "max_concurrent_runs"
	defaultMaxRuns      = 3
	settingGuestMaxRuns = "guest_max_concurrent_runs"
	defaultGuestMaxRuns = 2

	// maxRunLimit is the most the limits can be set to; a ceiling against typos.
	maxRunLimit = 1000
)

// errTooManyRuns is the refusal. 429: the caller is fine and may try again
// when a run of theirs ends.
var errTooManyRuns = httpx.NewError(http.StatusTooManyRequests, "too_many_runs",
	"too many answers are being written at once; wait for one to finish or stop one")

// admitRun checks that userID (or the address ip, for a guest's run) has a slot
// for one more run, and holds the lock that keeps it until q's transaction ends.
// It must be called in the transaction that creates the run, before the run is
// inserted.
func admitRun(ctx context.Context, q *sqlc.Queries, userID uuid.UUID, ip string) error {
	if ip != "" {
		limit, err := intSettingOr(ctx, q, settingGuestMaxRuns, defaultGuestMaxRuns)
		if err != nil {
			return err
		}
		if limit <= 0 {
			return nil
		}
		if err := q.LockRunSlots(ctx, "ip:"+ip); err != nil {
			return err
		}
		running, err := q.CountIPRunningRuns(ctx, ip)
		if err != nil {
			return err
		}
		if running >= int64(limit) {
			return errTooManyRuns
		}
		return nil
	}

	user, err := q.GetUser(ctx, userID)
	if err != nil {
		return err
	}
	if user.Role == sqlc.UserRoleAdmin {
		return nil
	}
	limit, err := intSettingOr(ctx, q, settingMaxRuns, defaultMaxRuns)
	if err != nil {
		return err
	}
	if limit <= 0 {
		return nil
	}
	if err := q.LockRunSlots(ctx, userID.String()); err != nil {
		return err
	}
	running, err := q.CountUserRunningRuns(ctx, userID)
	if err != nil {
		return err
	}
	if running >= int64(limit) {
		return errTooManyRuns
	}
	return nil
}

// guestRunSlotFree is admitRun's count without the lock, for a guest's request
// to be turned away before its files are stored and its rows written. It can
// let one through that admitRun then refuses; it never refuses one that admitRun
// would let through, unless a run ends in between.
func (s *Server) guestRunSlotFree(ctx context.Context, ip string) error {
	limit, err := intSettingOr(ctx, s.q, settingGuestMaxRuns, defaultGuestMaxRuns)
	if err != nil || limit <= 0 {
		return err
	}
	running, err := s.q.CountIPRunningRuns(ctx, ip)
	if err != nil {
		return err
	}
	if running >= int64(limit) {
		return errTooManyRuns
	}
	return nil
}

// validRunLimit checks a run limit of an update before anything is saved.
func validRunLimit(values ...*int) error {
	for _, v := range values {
		if v != nil && (*v < 0 || *v > maxRunLimit) {
			return httpx.NewError(http.StatusBadRequest, "invalid_run_limit",
				fmt.Sprintf("a run limit must be between 0 and %d", maxRunLimit))
		}
	}
	return nil
}
