package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"

	"github.com/qunqin24/openchat/apps/server/internal/auth"
	"github.com/qunqin24/openchat/apps/server/internal/db/sqlc"
)

// How many runs one account may have going at once.
// The runs are held open by an endpoint that waits (capFixture.hold), so a run
// stays `running` for as long as a test wants.

// setLimit changes a setting for one test and removes it (back to its default)
// afterwards.
func setLimit(t *testing.T, f *capFixture, key string, value int) {
	t.Helper()
	ctx := context.Background()
	raw, _ := json.Marshal(value)
	if err := f.q.SetSetting(ctx, sqlc.SetSettingParams{Key: key, Value: raw}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = f.pool.Exec(context.Background(), `DELETE FROM app_settings WHERE key = $1`, key) })
}

func (f *capFixture) newConversation(user sqlc.User) sqlc.Conversation {
	f.t.Helper()
	conv, err := f.q.CreateConversation(context.Background(), sqlc.CreateConversationParams{UserID: &user.ID, Title: "limit"})
	if err != nil {
		f.t.Fatal(err)
	}
	return conv
}

func (f *capFixture) sendTo(user sqlc.User, conv sqlc.Conversation) *httptest.ResponseRecorder {
	f.t.Helper()
	body := fmt.Sprintf(`{"content":"hello","model":%q,"webSearch":true}`, f.modelID)
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	rec := httptest.NewRecorder()
	f.server.CreateMessage(rec, req.WithContext(auth.WithUser(req.Context(), user)), conv.ID)
	return rec
}

// rowsOf counts what a conversation holds.
func (f *capFixture) rowsOf(conv uuid.UUID) (messages, runs int) {
	f.t.Helper()
	ctx := context.Background()
	_ = f.pool.QueryRow(ctx, `SELECT count(*) FROM messages WHERE conversation_id = $1`, conv).Scan(&messages)
	_ = f.pool.QueryRow(ctx, `SELECT count(*) FROM runs WHERE conversation_id = $1`, conv).Scan(&runs)
	return
}

func (f *capFixture) running(user sqlc.User) (n int) {
	_ = f.pool.QueryRow(context.Background(), `SELECT count(*) FROM runs WHERE user_id = $1 AND status = 'running'`, user.ID).Scan(&n)
	return
}

// A run beyond the limit is refused before anything is written; ending or
// stopping one frees its slot; administrators are exempt; 0 is no limit.
func TestRunLimitRefusesBeforeWritingAnything(t *testing.T) {
	f := newCapFixture(t, true)
	f.hold = make(chan struct{})
	defer close(f.hold)
	setLimit(t, f, settingMaxRuns, 2)

	first := f.sendTo(f.member, f.newConversation(f.member))
	secondConv := f.newConversation(f.member)
	second := f.sendTo(f.member, secondConv)
	if first.Code != http.StatusAccepted || second.Code != http.StatusAccepted {
		t.Fatalf("within the limit: %d, %d", first.Code, second.Code)
	}
	f.waitHeld(2)

	third := f.newConversation(f.member)
	rec := f.sendTo(f.member, third)
	if rec.Code != http.StatusTooManyRequests || errorCodeOf(t, rec) != "too_many_runs" {
		t.Fatalf("beyond the limit: %d %s", rec.Code, rec.Body)
	}
	messages, runs := f.rowsOf(third.ID)
	after, _ := f.q.GetConversation(context.Background(), sqlc.GetConversationParams{ID: third.ID, UserID: &f.member.ID})
	if messages != 0 || runs != 0 || after.Model != nil || f.requests.Load() != 2 {
		t.Errorf("a refused send left %d messages, %d runs, model %v, %d requests", messages, runs, after.Model, f.requests.Load())
	}

	// Stopping one frees its slot at once.
	f.stop(f.member, secondConv.ID, second)
	f.finished(second)
	if rec := f.sendTo(f.member, third); rec.Code != http.StatusAccepted {
		t.Fatalf("after one was stopped: %d %s", rec.Code, rec.Body)
	}

	// An administrator is not counted and not refused.
	for i := range 4 {
		if rec := f.sendTo(f.admin, f.newConversation(f.admin)); rec.Code != http.StatusAccepted {
			t.Fatalf("administrator's run %d: %d %s", i, rec.Code, rec.Body)
		}
	}

	// 0 is unlimited.
	setLimit(t, f, settingMaxRuns, 0)
	for i := range 4 {
		if rec := f.sendTo(f.member, f.newConversation(f.member)); rec.Code != http.StatusAccepted {
			t.Fatalf("unlimited, run %d: %d %s", i, rec.Code, rec.Body)
		}
	}
}

// With nothing set, three runs at once: the default.
func TestRunLimitDefaultsToThree(t *testing.T) {
	f := newCapFixture(t, true)
	f.hold = make(chan struct{})
	defer close(f.hold)
	if _, err := f.pool.Exec(context.Background(), `DELETE FROM app_settings WHERE key = $1`, settingMaxRuns); err != nil {
		t.Fatal(err)
	}
	for i := range 3 {
		if rec := f.sendTo(f.member, f.newConversation(f.member)); rec.Code != http.StatusAccepted {
			t.Fatalf("run %d: %d %s", i, rec.Code, rec.Body)
		}
	}
	if rec := f.sendTo(f.member, f.newConversation(f.member)); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("the fourth: %d %s", rec.Code, rec.Body)
	}
}

// Sends racing for the last slot: exactly as many get through as there are
// slots, whatever the interleaving.
func TestRunLimitHoldsUnderParallelSends(t *testing.T) {
	for _, limit := range []int{1, 2, 3} {
		t.Run(fmt.Sprintf("limit %d", limit), func(t *testing.T) {
			f := newCapFixture(t, true)
			f.hold = make(chan struct{})
			defer close(f.hold)
			setLimit(t, f, settingMaxRuns, limit)

			const attempts = 16
			convs := make([]sqlc.Conversation, attempts)
			for i := range convs {
				convs[i] = f.newConversation(f.member)
			}
			var accepted, refused atomic.Int32
			var wg sync.WaitGroup
			start := make(chan struct{})
			for i := range attempts {
				wg.Add(1)
				go func() {
					defer wg.Done()
					<-start
					switch rec := f.sendTo(f.member, convs[i]); rec.Code {
					case http.StatusAccepted:
						accepted.Add(1)
					case http.StatusTooManyRequests:
						refused.Add(1)
					default:
						t.Errorf("send %d: %d %s", i, rec.Code, rec.Body)
					}
				}()
			}
			close(start)
			wg.Wait()
			if int(accepted.Load()) != limit || int(refused.Load()) != attempts-limit {
				t.Fatalf("%d accepted and %d refused with a limit of %d", accepted.Load(), refused.Load(), limit)
			}
			if got := f.running(f.member); got != limit {
				t.Errorf("%d runs going, want %d", got, limit)
			}
			// The refused ones left nothing behind.
			written := 0
			for _, conv := range convs {
				if m, _ := f.rowsOf(conv.ID); m > 0 {
					written++
				}
			}
			if written != limit {
				t.Errorf("%d conversations were written to, want %d", written, limit)
			}
		})
	}
}

// Slots come back when the runs end by themselves, too.
func TestRunLimitIsReleasedWhenRunsEnd(t *testing.T) {
	f := newCapFixture(t, true)
	f.hold = make(chan struct{})
	setLimit(t, f, settingMaxRuns, 2)

	var sent []*httptest.ResponseRecorder
	for range 2 {
		sent = append(sent, f.sendTo(f.member, f.newConversation(f.member)))
	}
	f.waitHeld(2)
	if rec := f.sendTo(f.member, f.newConversation(f.member)); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("at the limit: %d", rec.Code)
	}
	close(f.hold)
	for _, rec := range sent {
		if status, _ := f.finished(rec); status != "completed" {
			t.Fatalf("run %s", status)
		}
	}
	for range 2 {
		if rec := f.sendTo(f.member, f.newConversation(f.member)); rec.Code != http.StatusAccepted {
			t.Fatalf("after the runs ended: %d %s", rec.Code, rec.Body)
		}
	}
}

// Regenerating and editing start runs through the same place, and are refused
// the same way - with the conversation as it was, since the rows they delete
// are put back with the rest of the transaction.
func TestRunLimitAlsoRefusesRegenerateAndEdit(t *testing.T) {
	f := newCapFixture(t, true)
	setLimit(t, f, settingMaxRuns, 1)

	// A conversation with an answer, finished.
	conv := f.newConversation(f.member)
	if status, _ := f.finished(f.sendTo(f.member, conv)); status != "completed" {
		t.Fatalf("setup run %s", status)
	}
	var before []string
	list := func() []string {
		rows, err := f.pool.Query(context.Background(), `SELECT id::text FROM messages WHERE conversation_id = $1 ORDER BY created_at, id`, conv.ID)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		var ids []string
		for rows.Next() {
			var id string
			_ = rows.Scan(&id)
			ids = append(ids, id)
		}
		return ids
	}
	before = list()
	var input string
	if err := f.pool.QueryRow(context.Background(), `SELECT id::text FROM messages WHERE conversation_id = $1 AND role = 'user'`, conv.ID).Scan(&input); err != nil {
		t.Fatal(err)
	}

	// The slot is taken by a run in another conversation.
	f.hold = make(chan struct{})
	defer close(f.hold)
	if rec := f.sendTo(f.member, f.newConversation(f.member)); rec.Code != http.StatusAccepted {
		t.Fatal(rec.Code)
	}
	f.waitHeld(1)

	do := func(body string, call func(w http.ResponseWriter, r *http.Request)) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
		rec := httptest.NewRecorder()
		call(rec, req.WithContext(auth.WithUser(req.Context(), f.member)))
		return rec
	}
	regen := do(fmt.Sprintf(`{"model":%q}`, f.modelID), func(w http.ResponseWriter, r *http.Request) { f.server.RegenerateAnswer(w, r, conv.ID) })
	if regen.Code != http.StatusTooManyRequests || errorCodeOf(t, regen) != "too_many_runs" {
		t.Fatalf("regenerate: %d %s", regen.Code, regen.Body)
	}
	id, _ := uuid.Parse(input)
	edit := do(fmt.Sprintf(`{"content":"again","model":%q}`, f.modelID), func(w http.ResponseWriter, r *http.Request) { f.server.EditMessage(w, r, conv.ID, id) })
	if edit.Code != http.StatusTooManyRequests || errorCodeOf(t, edit) != "too_many_runs" {
		t.Fatalf("edit: %d %s", edit.Code, edit.Body)
	}
	if got := list(); strings.Join(got, ",") != strings.Join(before, ",") {
		t.Errorf("a refused redo changed the transcript: %v, was %v", got, before)
	}
}

// The limit is in settings: read with a default, validated, and saved.
func TestRunLimitSettings(t *testing.T) {
	f := newCapFixture(t, true)
	if _, err := f.pool.Exec(context.Background(), `DELETE FROM app_settings WHERE key = $1`, settingMaxRuns); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = f.pool.Exec(context.Background(), `DELETE FROM app_settings WHERE key = $1`, settingMaxRuns)
	})
	patch := func(body string) (int, SystemSettings) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPatch, "/", strings.NewReader(body))
		f.server.UpdateSystemSettings(rec, req.WithContext(auth.WithUser(req.Context(), f.admin)))
		var out SystemSettings
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		return rec.Code, out
	}
	code, out := patch(`{}`)
	if code != 200 || out.MaxConcurrentRuns != 3 {
		t.Fatalf("defaults: %d %+v", code, out)
	}
	code, out = patch(`{"maxConcurrentRuns":5}`)
	if code != 200 || out.MaxConcurrentRuns != 5 {
		t.Fatalf("update: %d %+v", code, out)
	}
	for _, body := range []string{`{"maxConcurrentRuns":-1}`, `{"maxConcurrentRuns":1001}`} {
		if code, _ := patch(body); code != http.StatusBadRequest {
			t.Errorf("%s: %d, want 400", body, code)
		}
	}
	if _, out = patch(`{}`); out.MaxConcurrentRuns != 5 {
		t.Errorf("a refused update changed the limit to %d", out.MaxConcurrentRuns)
	}
}
