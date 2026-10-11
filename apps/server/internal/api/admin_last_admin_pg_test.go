package api

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/qunqin/openchat/apps/server/internal/auth"
	"github.com/qunqin/openchat/apps/server/internal/db/dbtest"
	"github.com/qunqin/openchat/apps/server/internal/db/sqlc"
)

// "Keep the last admin" is a count followed by a write, so two admins removing
// each other at the same moment both counted two and both went through. Every
// change that can take an admin away now holds the admin-membership lock from
// the count to the commit; these tests fire such pairs at the same instant, many
// times, and check that one of them is always refused and an admin always
// remains.
//
// The count is of every enabled admin in the database, so these tests have a
// database of their own: another test's administrators would make "the last
// admin" never come up.

type adminRace struct {
	t      *testing.T
	server *Server
	ctx    context.Context
	// a and b are the two administrators of the round.
	a, b sqlc.User
}

func newAdminRace(t *testing.T) *adminRace {
	t.Helper()
	pool := dbtest.Pool(t, "api_admins")
	r := &adminRace{t: t, ctx: context.Background(), server: &Server{pool: pool, q: sqlc.New(pool)}}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE email LIKE '%@adminrace.invalid'`)
	})
	return r
}

func (r *adminRace) newAdmin() sqlc.User {
	r.t.Helper()
	var u sqlc.User
	tag := strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
	if err := r.server.pool.QueryRow(r.ctx,
		`INSERT INTO users (email, name, role) VALUES ($1, $2, 'admin') RETURNING id, email, name, role`,
		tag+"@adminrace.invalid", "race"+tag).Scan(&u.ID, &u.Email, &u.Name, &u.Role); err != nil {
		r.t.Fatal(err)
	}
	return u
}

// round starts over with exactly two enabled administrators: whatever an
// earlier round or run left is an ordinary user from here on.
func (r *adminRace) round() {
	r.t.Helper()
	if _, err := r.server.pool.Exec(r.ctx, `UPDATE users SET role = 'user' WHERE role = 'admin'`); err != nil {
		r.t.Fatal(err)
	}
	r.a, r.b = r.newAdmin(), r.newAdmin()
}

func (r *adminRace) admins() (n int) {
	r.t.Helper()
	if err := r.server.pool.QueryRow(r.ctx,
		`SELECT count(*) FROM users WHERE role = 'admin' AND disabled_at IS NULL`).Scan(&n); err != nil {
		r.t.Fatal(err)
	}
	return n
}

// patch is PATCH /admin/users/{target} as actor.
func (r *adminRace) patch(actor, target sqlc.User, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPatch, "/", strings.NewReader(body))
	rec := httptest.NewRecorder()
	r.server.UpdateUser(rec, req.WithContext(auth.WithUser(req.Context(), actor)), target.ID)
	return rec
}

// bulk is POST /admin/users/bulk as actor.
func (r *adminRace) bulk(actor sqlc.User, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	rec := httptest.NewRecorder()
	r.server.BulkUpdateUsers(rec, req.WithContext(auth.WithUser(req.Context(), actor)))
	return rec
}

func (r *adminRace) bulkOne(actor, target sqlc.User, change string) func() *httptest.ResponseRecorder {
	return func() *httptest.ResponseRecorder {
		return r.bulk(actor, fmt.Sprintf(`{"userIds":[%q],%s}`, target.ID, change))
	}
}

func (r *adminRace) patchOne(actor, target sqlc.User, body string) func() *httptest.ResponseRecorder {
	return func() *httptest.ResponseRecorder { return r.patch(actor, target, body) }
}

// together runs the calls at the same moment.
func together(calls ...func() *httptest.ResponseRecorder) []*httptest.ResponseRecorder {
	out := make([]*httptest.ResponseRecorder, len(calls))
	start := make(chan struct{})
	var ready, done sync.WaitGroup
	for i, call := range calls {
		ready.Add(1)
		done.Add(1)
		go func() {
			defer done.Done()
			ready.Done()
			<-start
			out[i] = call()
		}()
	}
	ready.Wait()
	close(start)
	done.Wait()
	return out
}

// exactlyOneRefused checks the pair: one went through, the other was refused
// with last_admin, and one administrator is left.
func (r *adminRace) exactlyOneRefused(round int, label string, recs []*httptest.ResponseRecorder) {
	r.t.Helper()
	ok, refused := 0, 0
	for _, rec := range recs {
		switch {
		case rec.Code == http.StatusOK:
			ok++
		case rec.Code == http.StatusConflict && errorCodeOf(r.t, rec) == "last_admin":
			refused++
		default:
			r.t.Errorf("%s, round %d: unexpected %d %s", label, round, rec.Code, rec.Body)
		}
	}
	if ok != 1 || refused != 1 {
		r.t.Errorf("%s, round %d: %d went through and %d were refused, want one of each", label, round, ok, refused)
	}
	if n := r.admins(); n != 1 {
		r.t.Fatalf("%s, round %d: %d enabled admins are left, want 1", label, round, n)
	}
}

const adminRaceRounds = 20

func TestConcurrentDemotionsKeepTheLastAdmin(t *testing.T) {
	r := newAdminRace(t)
	for round := range adminRaceRounds {
		r.round()
		recs := together(
			r.patchOne(r.a, r.b, `{"role":"user"}`),
			r.patchOne(r.b, r.a, `{"role":"user"}`),
		)
		r.exactlyOneRefused(round, "demote each other", recs)
	}
}

func TestConcurrentDisablesKeepTheLastAdmin(t *testing.T) {
	r := newAdminRace(t)
	for round := range adminRaceRounds {
		r.round()
		recs := together(
			r.patchOne(r.a, r.b, `{"disabled":true}`),
			r.patchOne(r.b, r.a, `{"disabled":true}`),
		)
		r.exactlyOneRefused(round, "disable each other", recs)
	}
}

// A demotion and a disable are the two ways out, and they queue behind each
// other too.
func TestConcurrentDemotionAndDisableKeepTheLastAdmin(t *testing.T) {
	r := newAdminRace(t)
	for round := range adminRaceRounds {
		r.round()
		recs := together(
			r.patchOne(r.a, r.b, `{"role":"user"}`),
			r.patchOne(r.b, r.a, `{"disabled":true}`),
		)
		r.exactlyOneRefused(round, "demote and disable", recs)
	}
}

func TestConcurrentBulkChangesKeepTheLastAdmin(t *testing.T) {
	r := newAdminRace(t)
	for round := range adminRaceRounds {
		r.round()
		recs := together(
			r.bulkOne(r.a, r.b, `"role":"user"`),
			r.bulkOne(r.b, r.a, `"role":"user"`),
		)
		r.exactlyOneRefused(round, "bulk demote", recs)
	}
	for round := range adminRaceRounds {
		r.round()
		recs := together(
			r.bulkOne(r.a, r.b, `"disabled":true`),
			r.bulkOne(r.b, r.a, `"disabled":true`),
		)
		r.exactlyOneRefused(round, "bulk disable", recs)
	}
}

// One batch and one single edit, against each other.
func TestConcurrentBulkAndSingleChangesKeepTheLastAdmin(t *testing.T) {
	r := newAdminRace(t)
	for round := range adminRaceRounds {
		r.round()
		recs := together(
			r.bulkOne(r.a, r.b, `"role":"user"`),
			r.patchOne(r.b, r.a, `{"disabled":true}`),
		)
		r.exactlyOneRefused(round, "bulk and single", recs)
	}
}

// The lock changes nothing for a change that leaves an admin, and the last
// one still cannot go: not by themselves, and not by an administrator whose
// own access was removed while the request was in flight.
func TestDemotingOneOfSeveralAdminsStillWorks(t *testing.T) {
	r := newAdminRace(t)
	r.round()
	c := r.newAdmin()
	if rec := r.patch(r.a, r.b, `{"role":"user"}`); rec.Code != http.StatusOK {
		t.Fatalf("demote one of three: %d %s", rec.Code, rec.Body)
	}
	if rec := r.bulk(r.a, fmt.Sprintf(`{"userIds":[%q],"disabled":true}`, c.ID)); rec.Code != http.StatusOK {
		t.Fatalf("disable one of two: %d %s", rec.Code, rec.Body)
	}
	if n := r.admins(); n != 1 {
		t.Fatalf("%d admins, want 1", n)
	}
	if rec := r.patch(r.a, r.a, `{"role":"user"}`); rec.Code != http.StatusConflict || errorCodeOf(t, rec) != "self_lockout" {
		t.Fatalf("self demotion: %d %s", rec.Code, rec.Body)
	}
	if rec := r.patch(r.b, r.a, `{"role":"user"}`); rec.Code != http.StatusConflict || errorCodeOf(t, rec) != "last_admin" {
		t.Fatalf("demoting the last: %d %s", rec.Code, rec.Body)
	}
	if n := r.admins(); n != 1 {
		t.Fatalf("%d admins after the refusals, want 1", n)
	}
}
