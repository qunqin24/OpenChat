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

	"github.com/qunqin24/openchat/apps/server/internal/auth"
	"github.com/qunqin24/openchat/apps/server/internal/db/sqlc"
)

// The transcript is paged from its end: the first page is the newest rows, a
// cursor walks back toward the start, and every page reads oldest first.
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
	insert := func(id uuid.UUID, role string, runID *uuid.UUID, createdAt time.Time) {
		t.Helper()
		if _, err := pool.Exec(ctx, `INSERT INTO messages (id, conversation_id, role, content, run_id, created_at, updated_at)
			VALUES ($1, $2, $3::message_role, $4, $5, $6, $6)`,
			id, conv.ID, role, id.String(), runID, createdAt); err != nil {
			t.Fatal(err)
		}
	}
	run := func(input uuid.UUID) uuid.UUID {
		t.Helper()
		id := uuid.New()
		if _, err := pool.Exec(ctx, `INSERT INTO runs (id, conversation_id, user_id, input_message_id, model_id, status, finished_at)
			VALUES ($1, $2, $3, $4, 'fake', 'completed', now())`,
			id, conv.ID, user.ID, input); err != nil {
			t.Fatal(err)
		}
		return id
	}

	// 130 turns of a question and its answer. Two rows of turn 30 share a
	// timestamp, so the order between them rests on the id.
	var shown []uuid.UUID
	for n := 0; n < 130; n++ {
		question, answer := uuid.New(), uuid.New()
		if n == 30 {
			// Fixed ids, so the tie-break is known.
			question = uuid.MustParse("00000000-0000-4000-8000-000000000001")
			answer = uuid.MustParse("00000000-0000-4000-8000-000000000002")
		}
		stamp := at()
		insert(question, "user", nil, stamp)
		r := run(question)
		if n == 30 {
			insert(answer, "assistant", &r, stamp)
		} else {
			insert(answer, "assistant", &r, at())
		}
		shown = append(shown, question, answer)
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
