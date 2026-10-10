package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/qunqin/openchat/apps/server/internal/auth"
	"github.com/qunqin/openchat/apps/server/internal/db/sqlc"
)

// The transcript is paged from its end: the first page is the newest rows, a
// cursor walks back toward the start, every page reads oldest first, and the
// rows a regenerated answer hid, and a branch the reader is not on, stay hidden
// on every page.
func TestListMessagesPagesFromTheEnd(t *testing.T) {
	pool := agentTestPool(t)
	ctx := context.Background()
	q := sqlc.New(pool)

	tag := uuid.NewString()
	var user sqlc.User
	if err := pool.QueryRow(ctx, `INSERT INTO users (email, name) VALUES ($1, $2) RETURNING id, email, name, role`,
		tag+"@test.invalid", "paging-"+tag).Scan(&user.ID, &user.Email, &user.Name, &user.Role); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM users WHERE id = $1", user.ID) })
	conv, err := q.CreateConversation(ctx, sqlc.CreateConversationParams{UserID: &user.ID, Title: "paging"})
	if err != nil {
		t.Fatal(err)
	}

	base := time.Now().Add(-time.Hour).Truncate(time.Millisecond)
	tick := 0
	at := func() time.Time { tick++; return base.Add(time.Duration(tick) * time.Second) }
	insert := func(id uuid.UUID, role string, runID *uuid.UUID, createdAt time.Time, parent *uuid.UUID) {
		t.Helper()
		if _, err := pool.Exec(ctx, `INSERT INTO messages (id, conversation_id, role, content, run_id, parent_input_id, created_at, updated_at)
			VALUES ($1, $2, $3::message_role, $4, $5, $6, $7, $7)`,
			id, conv.ID, role, id.String(), runID, parent, createdAt); err != nil {
			t.Fatal(err)
		}
	}
	run := func(input uuid.UUID, superseded bool) uuid.UUID {
		t.Helper()
		id := uuid.New()
		if _, err := pool.Exec(ctx, `INSERT INTO runs (id, conversation_id, user_id, input_message_id, model_id, status, finished_at, superseded)
			VALUES ($1, $2, $3, $4, 'fake', 'completed', now(), $5)`,
			id, conv.ID, user.ID, input, superseded); err != nil {
			t.Fatal(err)
		}
		return id
	}

	// 130 shown turns of a question and its answer, with hidden rows scattered
	// among them: a regenerated answer under turn 10, and after turn 60 another
	// version of the next question with its answer (an edit, off the path).
	// Two rows of turn 30 share a timestamp, so the order between them rests on
	// the id.
	var (
		shown  []uuid.UUID
		parent *uuid.UUID
	)
	for n := 0; n < 130; n++ {
		question, answer := uuid.New(), uuid.New()
		if n == 30 {
			// Fixed ids, so the tie-break is known.
			question = uuid.MustParse("00000000-0000-4000-8000-000000000001")
			answer = uuid.MustParse("00000000-0000-4000-8000-000000000002")
		}
		stamp := at()
		if n == 61 {
			edited := uuid.New()
			insert(edited, "user", nil, at(), parent)
			gone := run(edited, false)
			insert(uuid.New(), "assistant", &gone, at(), nil)
		}
		insert(question, "user", nil, stamp, parent)
		parent = &question
		r := run(question, false)
		if n == 10 {
			old := run(question, true)
			insert(uuid.New(), "assistant", &old, at(), nil)
		}
		if n == 30 {
			insert(answer, "assistant", &r, stamp, nil)
		} else {
			insert(answer, "assistant", &r, at(), nil)
		}
		shown = append(shown, question, answer)
	}
	if err := q.SetCurrentInput(ctx, sqlc.SetCurrentInputParams{ConversationID: conv.ID, InputID: parent}); err != nil {
		t.Fatal(err)
	}

	server := &Server{pool: pool, q: q}
	page := func(before *string, limit *int) MessagePage {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req = req.WithContext(auth.WithUser(req.Context(), user))
		rec := httptest.NewRecorder()
		server.ListMessages(rec, req, conv.ID, ListMessagesParams{Before: before, Limit: limit})
		if rec.Code != http.StatusOK {
			t.Fatalf("list: %d %s", rec.Code, rec.Body)
		}
		var out MessagePage
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	ids := func(p MessagePage) []uuid.UUID {
		var out []uuid.UUID
		for _, m := range p.Items {
			out = append(out, uuid.UUID(m.Id))
		}
		return out
	}

	// The tied pair reads question first, as the ids order them.
	if tie := slices.Index(shown, uuid.MustParse("00000000-0000-4000-8000-000000000001")); shown[tie+1] != uuid.MustParse("00000000-0000-4000-8000-000000000002") {
		t.Fatal("test setup: the tied pair is not in id order")
	}

	// The first page is the newest rows, oldest first.
	first := page(nil, nil)
	if got, want := ids(first), shown[len(shown)-50:]; !slices.Equal(got, want) {
		t.Fatalf("first page is not the newest 50 rows in order:\n got %v\nwant %v", got, want)
	}
	if first.OlderCursor == nil {
		t.Fatal("first page has no older cursor")
	}

	// Walking back reaches the start with no gap and no repeat, at any page size.
	for _, size := range []int{100, 50, 7, 1} {
		limit := size
		var walked []uuid.UUID
		p := page(nil, &limit)
		if len(p.Items) != size {
			t.Fatalf("limit %d: first page has %d rows", size, len(p.Items))
		}
		for pages := 0; ; pages++ {
			if pages > len(shown) {
				t.Fatal("the cursor never reaches the start")
			}
			walked = append(ids(p), walked...)
			if p.OlderCursor == nil {
				break
			}
			if len(p.Items) != size {
				t.Fatalf("limit %d: a page with an older one behind it has %d rows", size, len(p.Items))
			}
			p = page(p.OlderCursor, &limit)
		}
		if !slices.Equal(walked, shown) {
			t.Fatalf("limit %d: walking back gave %d rows, want the %d shown ones in order", size, len(walked), len(shown))
		}
	}

	// A conversation that fits one page has no older cursor.
	limit := 100
	p := page(nil, &limit)
	for range 2 {
		p = page(p.OlderCursor, &limit)
	}
	if p.OlderCursor != nil || len(p.Items) != len(shown)-200 {
		t.Fatalf("last page: %d rows, cursor %v", len(p.Items), p.OlderCursor)
	}
	short := 5
	exact := page(nil, &short)
	if len(exact.Items) != 5 || exact.OlderCursor == nil {
		t.Fatalf("short page: %d rows, cursor %v", len(exact.Items), exact.OlderCursor)
	}

	// A cursor that is not one is refused.
	bad := "not-a-cursor"
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req = req.WithContext(auth.WithUser(req.Context(), user))
	rec := httptest.NewRecorder()
	server.ListMessages(rec, req, conv.ID, ListMessagesParams{Before: &bad})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad cursor: %d", rec.Code)
	}
}

// Paging follows the branch shown: a cursor walks back along the path, never
// onto rows of a branch that is off it, even where the two were written
// interleaved in time — and a switch of branch is a switch of what the
// walk finds.
func TestListMessagesFollowsTheBranchShown(t *testing.T) {
	pool := agentTestPool(t)
	ctx := context.Background()
	q := sqlc.New(pool)

	tag := uuid.NewString()
	var user sqlc.User
	if err := pool.QueryRow(ctx, `INSERT INTO users (email, name) VALUES ($1, $2) RETURNING id, email, name, role`,
		tag+"@test.invalid", "branch-paging-"+tag).Scan(&user.ID, &user.Email, &user.Name, &user.Role); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM users WHERE id = $1", user.ID) })
	conv, err := q.CreateConversation(ctx, sqlc.CreateConversationParams{UserID: &user.ID, Title: "branch paging"})
	if err != nil {
		t.Fatal(err)
	}

	base := time.Now().Add(-time.Hour).Truncate(time.Millisecond)
	tick := 0
	// turn writes a question under parent and its answer, and returns both ids.
	turn := func(parent *uuid.UUID) (question, answer uuid.UUID) {
		t.Helper()
		question, answer = uuid.New(), uuid.New()
		run := uuid.New()
		tick++
		if _, err := pool.Exec(ctx, `INSERT INTO messages (id, conversation_id, role, content, parent_input_id, created_at, updated_at)
			VALUES ($1, $2, 'user', $5, $3, $4, $4)`,
			question, conv.ID, parent, base.Add(time.Duration(tick)*time.Second), question.String()); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO runs (id, conversation_id, user_id, input_message_id, model_id, status, finished_at)
			VALUES ($1, $2, $3, $4, 'fake', 'completed', now())`, run, conv.ID, user.ID, question); err != nil {
			t.Fatal(err)
		}
		tick++
		if _, err := pool.Exec(ctx, `INSERT INTO messages (id, conversation_id, role, content, run_id, created_at, updated_at)
			VALUES ($1, $2, 'assistant', $5, $3, $4, $4)`,
			answer, conv.ID, run, base.Add(time.Duration(tick)*time.Second), answer.String()); err != nil {
			t.Fatal(err)
		}
		return question, answer
	}

	// A trunk of 20 turns, then two branches from its end written alternately.
	var trunk []uuid.UUID
	var parent *uuid.UUID
	for range 20 {
		qn, an := turn(parent)
		trunk = append(trunk, qn, an)
		parent = &qn
	}
	var left, right []uuid.UUID
	var leftParent, rightParent = parent, parent
	var leftFirst, rightFirst uuid.UUID
	for n := range 45 {
		lq, la := turn(leftParent)
		left = append(left, lq, la)
		leftParent = &lq
		if n == 0 {
			leftFirst = lq
		}
		if n < 30 {
			rq, ra := turn(rightParent)
			right = append(right, rq, ra)
			rightParent = &rq
			if n == 0 {
				rightFirst = rq
			}
		}
	}
	if err := q.SetCurrentInput(ctx, sqlc.SetCurrentInputParams{ConversationID: conv.ID, InputID: leftParent}); err != nil {
		t.Fatal(err)
	}

	server := &Server{pool: pool, q: q}
	page := func(before *string, limit int) MessagePage {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req = req.WithContext(auth.WithUser(req.Context(), user))
		rec := httptest.NewRecorder()
		server.ListMessages(rec, req, conv.ID, ListMessagesParams{Before: before, Limit: &limit})
		if rec.Code != http.StatusOK {
			t.Fatalf("list: %d %s", rec.Code, rec.Body)
		}
		var out MessagePage
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	walk := func(limit int) []uuid.UUID {
		t.Helper()
		var walked []uuid.UUID
		p := page(nil, limit)
		for range 1000 {
			for i := len(p.Items) - 1; i >= 0; i-- {
				walked = append([]uuid.UUID{uuid.UUID(p.Items[i].Id)}, walked...)
			}
			if p.OlderCursor == nil {
				return walked
			}
			p = page(p.OlderCursor, limit)
		}
		t.Fatal("the cursor never reaches the start")
		return nil
	}

	for _, limit := range []int{100, 13, 1} {
		if got, want := walk(limit), append(slices.Clone(trunk), left...); !slices.Equal(got, want) {
			t.Fatalf("limit %d, left branch: walked %d rows, want %d along the path", limit, len(got), len(want))
		}
	}

	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req = req.WithContext(auth.WithUser(req.Context(), user))
	rec := httptest.NewRecorder()
	server.SwitchVersion(rec, req, conv.ID, rightFirst, SwitchVersionParams{})
	if rec.Code != http.StatusOK {
		t.Fatalf("switch: %d %s", rec.Code, rec.Body)
	}
	var switched MessagePage
	if err := json.Unmarshal(rec.Body.Bytes(), &switched); err != nil {
		t.Fatal(err)
	}
	if want := append(slices.Clone(trunk), right...); len(switched.Items) != 50 || uuid.UUID(switched.Items[49].Id) != want[len(want)-1] {
		t.Fatalf("switch's page: %d rows, ends %v, want the right branch's end %v", len(switched.Items), switched.Items[len(switched.Items)-1].Id, want[len(want)-1])
	}
	for _, limit := range []int{100, 9} {
		if got, want := walk(limit), append(slices.Clone(trunk), right...); !slices.Equal(got, want) {
			t.Fatalf("limit %d, right branch: walked %d rows, want %d along the path", limit, len(got), len(want))
		}
	}

	// And back: the left branch, to its end.
	rec = httptest.NewRecorder()
	server.SwitchVersion(rec, req, conv.ID, leftFirst, SwitchVersionParams{})
	if rec.Code != http.StatusOK {
		t.Fatalf("switch back: %d %s", rec.Code, rec.Body)
	}
	if got, want := walk(50), append(slices.Clone(trunk), left...); !slices.Equal(got, want) {
		t.Fatalf("after switching back, walked %d rows, want %d", len(got), len(want))
	}
}
