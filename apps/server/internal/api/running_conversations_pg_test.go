package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/google/uuid"

	"github.com/qunqin24/openchat/apps/server/internal/auth"
	"github.com/qunqin24/openchat/apps/server/internal/db/sqlc"
)

// Only the caller's listed conversations with a run in progress are running.
func TestListRunningConversations(t *testing.T) {
	pool := agentTestPool(t)
	ctx := context.Background()
	q := sqlc.New(pool)
	server := &Server{pool: pool, q: q}

	tag := uuid.NewString()[:8]
	newUser := func(name string) sqlc.User {
		var u sqlc.User
		if err := pool.QueryRow(ctx, `INSERT INTO users (email, name) VALUES ($1, $2) RETURNING id, email, name, role`,
			name+tag+"@test.invalid", name+tag).Scan(&u.ID, &u.Email, &u.Name, &u.Role); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM users WHERE id = $1", u.ID) })
		return u
	}
	alice, bob := newUser("alice"), newUser("bob")
	withRun := func(owner sqlc.User, temporary bool, status string) uuid.UUID {
		c, err := q.CreateConversation(ctx, sqlc.CreateConversationParams{UserID: &owner.ID, Title: "t", Temporary: temporary})
		if err != nil {
			t.Fatal(err)
		}
		var input uuid.UUID
		if err := pool.QueryRow(ctx, `INSERT INTO messages (conversation_id, role, content) VALUES ($1, 'user', 'hi') RETURNING id`, c.ID).Scan(&input); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO runs (conversation_id, user_id, input_message_id, model_id, status, finished_at) VALUES ($1, $2, $3, 'm', $4::run_status, CASE WHEN $4::run_status = 'running' THEN NULL ELSE now() END)`,
			c.ID, owner.ID, input, status); err != nil {
			t.Fatal(err)
		}
		return c.ID
	}
	running := withRun(alice, false, "running")
	finished := withRun(alice, false, "completed")
	waiting := withRun(alice, false, "awaiting_input")
	temporary := withRun(alice, true, "running")
	deleted := withRun(alice, false, "running")
	if _, err := pool.Exec(ctx, `UPDATE conversations SET deleted_at = now() WHERE id = $1`, deleted); err != nil {
		t.Fatal(err)
	}
	someoneElses := withRun(bob, false, "running")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	server.ListRunningConversations(rec, req.WithContext(auth.WithUser(req.Context(), alice)))
	var got RunningConversations
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if !slices.Contains(got.Ids, running) {
		t.Error("the running conversation is missing")
	}
	for name, id := range map[string]uuid.UUID{"finished": finished, "waiting for an answer": waiting,
		"temporary": temporary, "deleted": deleted, "someone else's": someoneElses} {
		if slices.Contains(got.Ids, id) {
			t.Errorf("a %s conversation is listed as running", name)
		}
	}
}
