package api

import (
	"context"
	"encoding/json"
	"fmt"
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

// Deleting a conversation stops the runs it has going. Before, the rows were
// only marked: the run went on calling the model, nobody could stop it (the
// stop endpoint no longer finds a deleted conversation), and it held its
// owner's run slot until the model finished. The runs are held open by an
// endpoint that waits (capFixture.hold), so they end only by being cancelled.

func (f *capFixture) deleteConversation(user sqlc.User, conv uuid.UUID) *httptest.ResponseRecorder {
	f.t.Helper()
	req := httptest.NewRequest(http.MethodDelete, "/", nil)
	rec := httptest.NewRecorder()
	f.server.DeleteConversation(rec, req.WithContext(auth.WithUser(req.Context(), user)), conv)
	return rec
}

func (f *capFixture) deleteAll(user sqlc.User) *httptest.ResponseRecorder {
	f.t.Helper()
	req := httptest.NewRequest(http.MethodDelete, "/", nil)
	rec := httptest.NewRecorder()
	f.server.DeleteAllMyConversations(rec, req.WithContext(auth.WithUser(req.Context(), user)))
	return rec
}

// waitNoneRunning waits until a conversation has no run going.
func (f *capFixture) waitNoneRunning(conv uuid.UUID) {
	f.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		var n int
		if err := f.pool.QueryRow(context.Background(),
			`SELECT count(*) FROM runs WHERE conversation_id = $1 AND status = 'running'`, conv).Scan(&n); err != nil {
			f.t.Fatal(err)
		}
		if n == 0 {
			return
		}
		if time.Now().After(deadline) {
			f.t.Fatalf("a run is still going in conversation %s", conv)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func (f *capFixture) runStatuses(conv uuid.UUID) (statuses []string) {
	f.t.Helper()
	rows, err := f.pool.Query(context.Background(), `SELECT status::text FROM runs WHERE conversation_id = $1`, conv)
	if err != nil {
		f.t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var s string
		_ = rows.Scan(&s)
		statuses = append(statuses, s)
	}
	return statuses
}

// With one slot: a run is going, its conversation is deleted, the run ends
// cancelled and the slot is free - the account can send again.
func TestDeletingAConversationStopsItsRun(t *testing.T) {
	f := newCapFixture(t, true)
	f.hold = make(chan struct{})
	defer close(f.hold)
	setLimit(t, f, settingMaxRuns, 1)

	conv := f.newConversation(f.member)
	sent := f.sendTo(f.member, conv)
	if sent.Code != http.StatusAccepted {
		t.Fatalf("send: %d %s", sent.Code, sent.Body)
	}
	f.waitHeld(1)

	other := f.newConversation(f.member)
	if rec := f.sendTo(f.member, other); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("the slot is taken: %d %s", rec.Code, rec.Body)
	}

	if rec := f.deleteConversation(f.member, conv.ID); rec.Code != http.StatusNoContent {
		t.Fatalf("delete: %d %s", rec.Code, rec.Body)
	}
	if status, _ := f.finished(sent); status != "cancelled" {
		t.Fatalf("the run of a deleted conversation ended %s, want cancelled", status)
	}
	// The model request was dropped, not left to finish.
	deadline := time.Now().Add(5 * time.Second)
	for f.held.Load() != 0 {
		if time.Now().After(deadline) {
			t.Fatal("the model request of the deleted conversation is still open")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if n := f.running(f.member); n != 0 {
		t.Fatalf("%d runs still count for the account", n)
	}

	// The slot is back.
	if rec := f.sendTo(f.member, other); rec.Code != http.StatusAccepted {
		t.Fatalf("send after the delete: %d %s", rec.Code, rec.Body)
	}

	// And the conversation is gone for its owner, stop included.
	var created CreateMessageResponse
	if err := json.Unmarshal(sent.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	w := httptest.NewRecorder()
	f.server.CancelRun(w, req.WithContext(auth.WithUser(req.Context(), f.member)), conv.ID, created.Run.Id)
	if w.Code != http.StatusNotFound {
		t.Fatalf("stop in a deleted conversation: %d", w.Code)
	}
	// Deleting twice finds nothing.
	if rec := f.deleteConversation(f.member, conv.ID); rec.Code != http.StatusNotFound {
		t.Fatalf("second delete: %d", rec.Code)
	}
}

// Someone else's conversation is not found, and its run is left alone.
func TestDeletingSomeoneElsesConversationStopsNothing(t *testing.T) {
	f := newCapFixture(t, true)
	f.hold = make(chan struct{})
	defer close(f.hold)

	conv := f.newConversation(f.admin)
	sent := f.sendTo(f.admin, conv)
	f.waitHeld(1)
	if rec := f.deleteConversation(f.member, conv.ID); rec.Code != http.StatusNotFound {
		t.Fatalf("a stranger's delete: %d", rec.Code)
	}
	time.Sleep(100 * time.Millisecond)
	if got := f.runStatuses(conv.ID); len(got) != 1 || got[0] != "running" {
		t.Fatalf("the run is %v, want it still running", got)
	}
	_ = sent
}

// Delete everything: every run of the caller's, in every conversation, is
// stopped; another account's are not.
func TestDeletingAllConversationsStopsTheirRuns(t *testing.T) {
	f := newCapFixture(t, true)
	f.hold = make(chan struct{})
	defer close(f.hold)
	setLimit(t, f, settingMaxRuns, 2)

	first, second, idle := f.newConversation(f.member), f.newConversation(f.member), f.newConversation(f.member)
	one, two := f.sendTo(f.member, first), f.sendTo(f.member, second)
	if one.Code != http.StatusAccepted || two.Code != http.StatusAccepted {
		t.Fatalf("sends: %d, %d", one.Code, two.Code)
	}
	theirs := f.newConversation(f.admin)
	admins := f.sendTo(f.admin, theirs)
	if admins.Code != http.StatusAccepted {
		t.Fatalf("administrator's send: %d", admins.Code)
	}
	f.waitHeld(3)

	rec := f.deleteAll(f.member)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete all: %d %s", rec.Code, rec.Body)
	}
	if !strings.Contains(rec.Body.String(), `"deleted":3`) {
		t.Fatalf("delete all said %s, want 3 deleted", rec.Body)
	}
	for _, sent := range []*httptest.ResponseRecorder{one, two} {
		if status, _ := f.finished(sent); status != "cancelled" {
			t.Fatalf("a run ended %s, want cancelled", status)
		}
	}
	f.waitNoneRunning(idle.ID)
	if n := f.running(f.member); n != 0 {
		t.Fatalf("%d runs still count for the account", n)
	}
	// The other account's run is untouched.
	time.Sleep(100 * time.Millisecond)
	if got := f.runStatuses(theirs.ID); len(got) != 1 || got[0] != "running" {
		t.Fatalf("another account's run is %v, want it still running", got)
	}
	// Both slots are free again.
	for i := range 2 {
		if rec := f.sendTo(f.member, f.newConversation(f.member)); rec.Code != http.StatusAccepted {
			t.Fatalf("send %d after delete all: %d %s", i, rec.Code, rec.Body)
		}
	}
}

// A send racing a delete: whichever gets the conversation's lock first, no run
// is left going in a deleted conversation - either the send is refused, or its
// run was started first and the delete stopped it.
func TestSendRacingDeleteLeavesNoRunInADeletedConversation(t *testing.T) {
	f := newCapFixture(t, true)
	f.hold = make(chan struct{})
	defer close(f.hold)
	setLimit(t, f, settingMaxRuns, 0)

	const rounds = 60
	var accepted, refused int
	for round := range rounds {
		conv := f.newConversation(f.member)
		var sent, deleted *httptest.ResponseRecorder
		var wg sync.WaitGroup
		start := make(chan struct{})
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			sent = f.sendTo(f.member, conv)
		}()
		go func() {
			defer wg.Done()
			<-start
			// Staggered across the rounds, so the delete lands before, in the
			// middle of and after the send's transaction.
			time.Sleep(time.Duration(round%10) * 400 * time.Microsecond)
			deleted = f.deleteConversation(f.member, conv.ID)
		}()
		close(start)
		wg.Wait()

		if deleted.Code != http.StatusNoContent {
			t.Fatalf("round %d: delete %d %s", round, deleted.Code, deleted.Body)
		}
		switch sent.Code {
		case http.StatusAccepted:
			accepted++
			// Started before the delete: the delete stopped it.
			if status, _ := f.finished(sent); status != "cancelled" {
				t.Fatalf("round %d: the run ended %s, want cancelled", round, status)
			}
		case http.StatusNotFound:
			refused++
			if messages, runs := f.rowsOf(conv.ID); messages != 0 || runs != 0 {
				t.Fatalf("round %d: a refused send left %d messages and %d runs", round, messages, runs)
			}
		default:
			t.Fatalf("round %d: send %d %s", round, sent.Code, sent.Body)
		}
		f.waitNoneRunning(conv.ID)
	}
	t.Logf("%d sends were accepted and then stopped, %d were refused", accepted, refused)
	if n := f.running(f.member); n != 0 {
		t.Fatalf("%d runs still going", n)
	}
}

// Delete all racing sends, likewise.
func TestSendsRacingDeleteAllLeaveNoRunInADeletedConversation(t *testing.T) {
	f := newCapFixture(t, true)
	f.hold = make(chan struct{})
	defer close(f.hold)
	setLimit(t, f, settingMaxRuns, 0)

	for round := range 10 {
		convs := make([]sqlc.Conversation, 6)
		for i := range convs {
			convs[i] = f.newConversation(f.member)
		}
		sends := make([]*httptest.ResponseRecorder, len(convs))
		var wg sync.WaitGroup
		start := make(chan struct{})
		for i := range convs {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				sends[i] = f.sendTo(f.member, convs[i])
			}()
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			time.Sleep(time.Duration(round%10) * 400 * time.Microsecond)
			if rec := f.deleteAll(f.member); rec.Code != http.StatusOK {
				t.Errorf("round %d: delete all %d %s", round, rec.Code, rec.Body)
			}
		}()
		close(start)
		wg.Wait()

		for i, sent := range sends {
			switch sent.Code {
			case http.StatusAccepted:
				if status, _ := f.finished(sent); status != "cancelled" {
					t.Fatalf("round %d, send %d: the run ended %s, want cancelled", round, i, status)
				}
			case http.StatusNotFound:
			default:
				t.Fatalf("round %d, send %d: %d %s", round, i, sent.Code, sent.Body)
			}
		}
		for _, conv := range convs {
			f.waitNoneRunning(conv.ID)
		}
	}
	if n := f.running(f.member); n != 0 {
		t.Fatalf("%d runs still going", n)
	}
}

// A request that has passed its checks and is waiting for the conversation's
// lock when the conversation is deleted is refused when it gets the lock:
// sending, regenerating and editing all re-check under it.
func TestRequestsQueuedBehindADeleteAreRefused(t *testing.T) {
	f := newCapFixture(t, true)
	ctx := context.Background()

	// A conversation with an answer, to regenerate and to edit.
	conv := f.newConversation(f.member)
	if status, _ := f.finished(f.sendTo(f.member, conv)); status != "completed" {
		t.Fatalf("setup run %s", status)
	}
	var input uuid.UUID
	if err := f.pool.QueryRow(ctx, `SELECT id FROM messages WHERE conversation_id = $1 AND role = 'user'`, conv.ID).Scan(&input); err != nil {
		t.Fatal(err)
	}
	runsBefore := len(f.runStatuses(conv.ID))

	body := fmt.Sprintf(`{"content":"again","model":%q}`, f.modelID)
	asMember := func(r *http.Request) *http.Request {
		return r.WithContext(auth.WithUser(r.Context(), f.member))
	}
	requests := map[string]func(w http.ResponseWriter){
		"send": func(w http.ResponseWriter) {
			f.server.CreateMessage(w, asMember(httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))), conv.ID)
		},
		"regenerate": func(w http.ResponseWriter) {
			f.server.RegenerateAnswer(w, asMember(httptest.NewRequest(http.MethodPost, "/", strings.NewReader(fmt.Sprintf(`{"model":%q}`, f.modelID)))), conv.ID)
		},
		"edit": func(w http.ResponseWriter) {
			f.server.EditMessage(w, asMember(httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))), conv.ID, input)
		},
	}

	// The lock is held by a transaction that deletes the conversation; the
	// requests are let in to wait for it first.
	tx, err := f.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := f.q.WithTx(tx).LockConversation(ctx, conv.ID); err != nil {
		t.Fatal(err)
	}

	recs := map[string]*httptest.ResponseRecorder{}
	var wg sync.WaitGroup
	for name, call := range requests {
		rec := httptest.NewRecorder()
		recs[name] = rec
		wg.Add(1)
		go func() {
			defer wg.Done()
			call(rec)
		}()
	}
	// Each is blocked on the lock, past the checks that read the conversation.
	// Watched from the transaction's own connection: the three requests each
	// hold one of the pool's, and on a small runner (2 CPUs: a pool of 4) one
	// more acquire would wait for ever. Inside a transaction pg_stat_activity
	// is a snapshot taken at first read, so it is cleared before each look.
	deadline := time.Now().Add(10 * time.Second)
	for {
		var waiting int
		if _, err := tx.Exec(ctx, `SELECT pg_stat_clear_snapshot()`); err != nil {
			t.Fatal(err)
		}
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM pg_stat_activity WHERE datname = current_database() AND wait_event_type = 'Lock' AND query ILIKE '%FOR UPDATE%'`).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting >= len(requests) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("only %d of %d requests are waiting for the lock", waiting, len(requests))
		}
		time.Sleep(10 * time.Millisecond)
	}

	if _, err := f.q.WithTx(tx).SoftDeleteConversation(ctx, sqlc.SoftDeleteConversationParams{ID: conv.ID, UserID: &f.member.ID}); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	wg.Wait()

	for name, rec := range recs {
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s after a delete: %d %s, want 404", name, rec.Code, rec.Body)
		}
	}
	if got := len(f.runStatuses(conv.ID)); got != runsBefore {
		t.Errorf("the conversation has %d runs, want the %d it had", got, runsBefore)
	}
	f.waitNoneRunning(conv.ID)
}
