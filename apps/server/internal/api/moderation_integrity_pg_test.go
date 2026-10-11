package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/qunqin/openchat/apps/server/internal/auth"
	"github.com/qunqin/openchat/apps/server/internal/db/sqlc"
)

// Integrity of the moderation machinery itself: the sanction against a
// promotion, the save of the settings, the settings that cannot be read.

// A sanction reads the role under the lock every change of an administrator's
// access takes. The promotion is held open (it has the lock and has written the
// role, not committed); the sanction must wait for it, and then see an
// administrator - which is only logged. Without the lock it read a member, and
// disabled the account once the promotion let go of the row.
func TestSanctionWaitsForAPromotionInFlight(t *testing.T) {
	h, _ := newModerationHarness(t, `,"strikes":1`)
	ctx := context.Background()

	tx, err := h.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := h.q.WithTx(tx).LockAdminMembership(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `UPDATE users SET role = 'admin' WHERE id = $1`, h.user.ID); err != nil {
		t.Fatal(err)
	}

	cfg, err := loadModeration(ctx, h.q)
	if err != nil {
		t.Fatal(err)
	}
	who := moderationSubject{userID: h.user.ID, source: ModerationSourceSend} // the request saw a member
	done := make(chan moderationSanction, 1)
	go func() {
		done <- h.server.recordBlocked(ctx, cfg, who, "BLOCKME", []string{"violence/graphic"})
	}()

	select {
	case got := <-done:
		t.Fatalf("the sanction did not wait for the promotion: %+v", got)
	case <-time.After(300 * time.Millisecond):
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-done:
		if got.disabled {
			t.Error("an account promoted to administrator was disabled")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the sanction never finished")
	}
	if h.count(`SELECT count(*) FROM users WHERE id = $1 AND disabled_at IS NOT NULL`, h.user.ID) != 0 {
		t.Error("an administrator was disabled")
	}
	for _, e := range h.events() {
		if e.Sanction != nil {
			t.Errorf("the event carries the sanction %q", *e.Sanction)
		}
	}
}

// Promotions and sanctions raced: neither deadlocks, and what is left is what
// one order of the two would have left - promoted first (an enabled
// administrator, only logged) or sanctioned first (disabled, then promoted by
// the administrator who chose to).
func TestSanctionAndPromotionRaceWithoutDeadlock(t *testing.T) {
	h, _ := newModerationHarness(t, `,"strikes":1`)
	ctx := context.Background()
	cfg, err := loadModeration(ctx, h.q)
	if err != nil {
		t.Fatal(err)
	}
	var actor sqlc.User
	tag := uuid.NewString()
	if err := h.pool.QueryRow(ctx, `INSERT INTO users (email, name, role) VALUES ($1, $2, 'admin') RETURNING id, email, name, role`,
		tag+"@test.invalid", "actor-"+tag).Scan(&actor.ID, &actor.Email, &actor.Name, &actor.Role); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = h.pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, actor.ID) })

	for round := range 30 {
		var id uuid.UUID
		rt := uuid.NewString()
		if err := h.pool.QueryRow(ctx, `INSERT INTO users (email, name) VALUES ($1, $2) RETURNING id`,
			rt+"@test.invalid", "race-"+rt).Scan(&id); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			_, _ = h.pool.Exec(context.Background(), `DELETE FROM moderation_events WHERE user_id = $1`, id)
			_, _ = h.pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, id)
		})

		start := make(chan struct{})
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPatch, "/", strings.NewReader(`{"role":"admin"}`))
			h.server.UpdateUser(rec, req.WithContext(auth.WithUser(req.Context(), actor)), id)
			if rec.Code != http.StatusOK {
				t.Errorf("round %d: promotion %d %s", round, rec.Code, rec.Body)
			}
		}()
		go func() {
			defer wg.Done()
			<-start
			h.server.recordBlocked(ctx, cfg, moderationSubject{userID: id, source: ModerationSourceSend}, "BLOCKME", []string{"violence/graphic"})
		}()
		close(start)
		finished := make(chan struct{})
		go func() { wg.Wait(); close(finished) }()
		select {
		case <-finished:
		case <-time.After(20 * time.Second):
			t.Fatalf("round %d: deadlock", round)
		}

		var (
			role     string
			disabled bool
			sanction *string
		)
		if err := h.pool.QueryRow(ctx, `SELECT u.role::text, u.disabled_at IS NOT NULL, (SELECT e.sanction FROM moderation_events e WHERE e.user_id = u.id)
			FROM users u WHERE u.id = $1`, id).Scan(&role, &disabled, &sanction); err != nil {
			t.Fatal(err)
		}
		switch {
		case role == "admin" && !disabled && sanction == nil: // promoted first
		case role == "admin" && disabled && sanction != nil && *sanction == "disabled": // sanctioned first
		default:
			t.Fatalf("round %d: role %s, disabled %v, sanction %v", round, role, disabled, sanction)
		}
	}
}
