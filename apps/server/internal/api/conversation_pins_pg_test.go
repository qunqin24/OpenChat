package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/qunqin24/openchat/apps/server/internal/auth"
	"github.com/qunqin24/openchat/apps/server/internal/db/sqlc"
)

// A pinned conversation is listed on its own, the latest pinned first, and
// pinning it is not activity: its place by date in the list stays.
func TestConversationPins(t *testing.T) {
	pool := agentTestPool(t)
	ctx := context.Background()
	q := sqlc.New(pool)
	tag := uuid.NewString()
	newUser := func(name string) sqlc.User {
		var u sqlc.User
		if err := pool.QueryRow(ctx, `INSERT INTO users (email, name) VALUES ($1, $2) RETURNING id, email, name, role`,
			name+tag+"@test.invalid", name+tag).Scan(&u.ID, &u.Email, &u.Name, &u.Role); err != nil {
			t.Fatal(err)
		}
		return u
	}
	alice, mallory := newUser("alice"), newUser("mallory")
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM users WHERE id = ANY($1)", []uuid.UUID{alice.ID, mallory.ID})
	})
	server := &Server{pool: pool, q: q}
	conv := func(title string, temporary bool) sqlc.Conversation {
		c, err := q.CreateConversation(ctx, sqlc.CreateConversationParams{UserID: &alice.ID, Title: title, Temporary: temporary})
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	first, second, temp := conv("first", false), conv("second", false), conv("temp", true)

	patch := func(user sqlc.User, id uuid.UUID, body string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPatch, "/", strings.NewReader(body))
		server.UpdateConversation(rec, req.WithContext(auth.WithUser(req.Context(), user)), id)
		return rec
	}
	pinned := func() []string {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		yes := true
		server.ListConversations(rec, req.WithContext(auth.WithUser(req.Context(), alice)), ListConversationsParams{Pinned: &yes})
		var page ConversationPage
		if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
			t.Fatal(err)
		}
		titles := make([]string, len(page.Items))
		for i, item := range page.Items {
			titles[i] = item.Title
		}
		return titles
	}

	for _, c := range []sqlc.Conversation{first, second} {
		if rec := patch(alice, c.ID, `{"pinned":true}`); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"pinnedAt"`) {
			t.Fatalf("pin %s: %d %s", c.Title, rec.Code, rec.Body)
		}
	}
	if got := strings.Join(pinned(), ","); got != "second,first" {
		t.Errorf("pinned: %s", got)
	}
	var updated sqlc.Conversation
	if updated, _ = q.GetConversation(ctx, sqlc.GetConversationParams{ID: first.ID, UserID: &alice.ID}); !updated.UpdatedAt.Time.Equal(first.UpdatedAt.Time) {
		t.Error("pinning moved the conversation's activity")
	}
	if rec := patch(alice, first.ID, `{"pinned":false}`); rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), `"pinnedAt"`) {
		t.Fatalf("unpin: %d %s", rec.Code, rec.Body)
	}
	if got := strings.Join(pinned(), ","); got != "second" {
		t.Errorf("after unpinning: %s", got)
	}
	if rec := patch(alice, temp.ID, `{"pinned":true}`); rec.Code != http.StatusBadRequest {
		t.Errorf("a temporary chat pinned: %d", rec.Code)
	}
	if rec := patch(mallory, second.ID, `{"pinned":false}`); rec.Code != http.StatusNotFound {
		t.Errorf("another person's conversation: %d", rec.Code)
	}
}
