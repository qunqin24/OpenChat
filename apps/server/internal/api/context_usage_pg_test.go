package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/qunqin/openchat/apps/server/internal/auth"
	"github.com/qunqin/openchat/apps/server/internal/db/sqlc"
)

func TestConversationContextIsTheLatestAnsweredRequest(t *testing.T) {
	pool := agentTestPool(t)
	ctx := context.Background()
	q := sqlc.New(pool)

	newUser := func() sqlc.User {
		tag := uuid.NewString()
		var user sqlc.User
		if err := pool.QueryRow(ctx, `INSERT INTO users (email, name) VALUES ($1, $2) RETURNING id, email, name, role`,
			tag+"@test.invalid", "ctx-"+tag).Scan(&user.ID, &user.Email, &user.Name, &user.Role); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM users WHERE id = $1", user.ID) })
		return user
	}
	owner, stranger := newUser(), newUser()
	conv, err := q.CreateConversation(ctx, sqlc.CreateConversationParams{UserID: &owner.ID, Title: "ctx"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM usage_logs WHERE conversation_id = $1", conv.ID)
	})

	server := &Server{pool: pool, q: q}
	get := func(as sqlc.User) (int, ContextUsage) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		server.GetConversationContext(rec, req.WithContext(auth.WithUser(req.Context(), as)), conv.ID)
		var out ContextUsage
		if rec.Code == http.StatusOK {
			if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
				t.Fatal(err)
			}
		}
		return rec.Code, out
	}

	// Nothing answered yet: no figure.
	if code, got := get(owner); code != http.StatusOK || got.UsedTokens != nil {
		t.Fatalf("empty conversation: %d %+v", code, got)
	}

	base := time.Now().Add(-time.Minute)
	log := func(at time.Duration, kind, status, model string, in, cacheRead, cacheWrite, out int32) {
		if err := q.InsertUsageLog(ctx, sqlc.InsertUsageLogParams{
			CreatedAt: pgtype.Timestamptz{Time: base.Add(at), Valid: true}, Kind: kind, Status: status,
			UserID: &owner.ID, ConversationID: &conv.ID, ChannelName: "c", ModelID: model, ServedModel: model,
			InputTokens: in, CacheReadTokens: cacheRead, CacheWriteTokens: cacheWrite, OutputTokens: out,
		}); err != nil {
			t.Fatal(err)
		}
	}
	log(1*time.Second, "chat", "ok", "m-old", 100, 0, 0, 10)
	log(2*time.Second, "chat", "ok", "m-new", 1000, 30000, 500, 700)
	// Neither a later failure nor a later title request is the conversation's context.
	log(3*time.Second, "chat", "error", "m-new", 0, 0, 0, 0)
	log(4*time.Second, "title", "ok", "m-task", 40, 0, 0, 5)

	code, got := get(owner)
	if code != http.StatusOK || got.UsedTokens == nil || *got.UsedTokens != 1000+30000+500+700 || got.Model == nil || *got.Model != "m-new" {
		t.Fatalf("latest answered request: %d %+v", code, got)
	}

	// Another user's conversation does not exist.
	if code, _ := get(stranger); code != http.StatusNotFound {
		t.Fatalf("stranger got %d, want 404", code)
	}
}
