package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/qunqin24/openchat/apps/server/internal/auth"
	"github.com/qunqin24/openchat/apps/server/internal/db/dbtest"
	"github.com/qunqin24/openchat/apps/server/internal/db/sqlc"
)

// Deleting an account: everything that is only its goes, its files with it,
// and its requests stay in the ledger under the name it had.
func TestDeleteUser(t *testing.T) {
	pool := dbtest.Pool(t, "api_delete_user")
	ctx := context.Background()
	q := sqlc.New(pool)

	tag := uuid.NewString()
	newUser := func(name string, role sqlc.UserRole) sqlc.User {
		var u sqlc.User
		if err := pool.QueryRow(ctx, `INSERT INTO users (email, name, role) VALUES ($1, $2, $3) RETURNING id, email, name, role`,
			name+tag+"@test.invalid", name+tag, role).Scan(&u.ID, &u.Email, &u.Name, &u.Role); err != nil {
			t.Fatal(err)
		}
		return u
	}
	admin := newUser("admin", sqlc.UserRoleAdmin)
	member := newUser("member", sqlc.UserRoleUser)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM users WHERE id = ANY($1)", []uuid.UUID{admin.ID, member.ID})
	})

	blobs := &memoryStore{blobs: map[string][]byte{}}
	server := &Server{pool: pool, q: q, store: blobs}

	// The member's conversation, with a file sent in it, and a request in the ledger.
	conv, err := q.CreateConversation(ctx, sqlc.CreateConversationParams{UserID: &member.ID, Title: "mine"})
	if err != nil {
		t.Fatal(err)
	}
	file, err := q.CreateAttachment(ctx, sqlc.CreateAttachmentParams{
		UserID: &member.ID, StorageKey: "uploads/" + uuid.NewString(), Filename: "a.png", ContentType: "image/png", SizeBytes: 4,
	})
	if err != nil {
		t.Fatal(err)
	}
	blobs.put(file.StorageKey, []byte("data"))
	input, err := appendInput(ctx, q, conv.ID, "look", inputParts("look", []sqlc.Attachment{file}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := q.AttachToMessage(ctx, sqlc.AttachToMessageParams{MessageID: &input.ID, UserID: &member.ID, Ids: []uuid.UUID{file.ID}}); err != nil {
		t.Fatal(err)
	}
	logID, err := q.InsertUsageLog(ctx, sqlc.InsertUsageLogParams{
		CreatedAt: pgtype.Timestamptz{Valid: true, Time: input.CreatedAt.Time}, Kind: "chat", Status: "ok",
		UserID: &member.ID, ConversationID: &conv.ID, ModelID: "m", Streamed: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	del := func(actor sqlc.User, id uuid.UUID) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodDelete, "/", nil)
		server.DeleteUser(rec, req.WithContext(auth.WithUser(req.Context(), actor)), id)
		return rec
	}

	t.Run("not your own, not one that is not there", func(t *testing.T) {
		if rec := del(admin, admin.ID); rec.Code != http.StatusConflict {
			t.Errorf("self: %d %s", rec.Code, rec.Body)
		}
		if rec := del(admin, uuid.New()); rec.Code != http.StatusNotFound {
			t.Errorf("missing: %d", rec.Code)
		}
	})

	t.Run("the account goes with what is only its", func(t *testing.T) {
		if rec := del(admin, member.ID); rec.Code != http.StatusNoContent {
			t.Fatalf("delete: %d %s", rec.Code, rec.Body)
		}
		var users, convs, files int
		_ = pool.QueryRow(ctx, `SELECT count(*) FROM users WHERE id = $1`, member.ID).Scan(&users)
		_ = pool.QueryRow(ctx, `SELECT count(*) FROM conversations WHERE id = $1`, conv.ID).Scan(&convs)
		_ = pool.QueryRow(ctx, `SELECT count(*) FROM attachments WHERE id = $1`, file.ID).Scan(&files)
		if users != 0 || convs != 0 || files != 0 {
			t.Errorf("left: users %d, conversations %d, attachments %d", users, convs, files)
		}
		if _, err := blobs.Read(ctx, file.StorageKey, 100); err == nil {
			t.Error("the stored file is still there")
		}
		var owner *uuid.UUID
		var label *string
		if err := pool.QueryRow(ctx, `SELECT user_id, deleted_user FROM usage_logs WHERE id = $1`, logID).Scan(&owner, &label); err != nil {
			t.Fatalf("the ledger row went: %v", err)
		}
		if owner != nil || label == nil || *label != member.Name {
			t.Errorf("ledger row: owner %v, label %v", owner, label)
		}
		if rec := del(admin, member.ID); rec.Code != http.StatusNotFound {
			t.Errorf("again: %d", rec.Code)
		}
	})
}
