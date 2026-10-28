package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/qunqin24/openchat/apps/server/internal/auth"
	"github.com/qunqin24/openchat/apps/server/internal/db/sqlc"
)

// A person's files count against their quota when an upload is asked for, and
// again at the size it was stored at when a message claims it.
func TestStorageQuota(t *testing.T) {
	pool := agentTestPool(t)
	ctx := context.Background()
	q := sqlc.New(pool)
	tag := uuid.NewString()
	var user sqlc.User
	if err := pool.QueryRow(ctx, `INSERT INTO users (email, name) VALUES ($1, $2) RETURNING id, email, name, role`,
		"quota"+tag+"@test.invalid", "quota"+tag).Scan(&user.ID, &user.Email, &user.Name, &user.Role); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM users WHERE id = $1", user.ID) })
	blobs := &memoryStore{blobs: map[string][]byte{}}
	server := &Server{pool: pool, q: q, store: blobs}

	call := func(handler func(http.ResponseWriter, *http.Request), body string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
		handler(rec, req.WithContext(auth.WithUser(req.Context(), user)))
		return rec
	}
	upload := func(name string, announced int, stored []byte) (*httptest.ResponseRecorder, uuid.UUID) {
		rec := call(server.CreateUpload, fmt.Sprintf(`{"filename":%q,"contentType":"text/plain","size":%d}`, name, announced))
		if rec.Code != http.StatusCreated {
			return rec, uuid.Nil
		}
		var up Upload
		_ = json.Unmarshal(rec.Body.Bytes(), &up)
		var key string
		if err := pool.QueryRow(ctx, `SELECT storage_key FROM attachments WHERE id = $1`, up.Id).Scan(&key); err != nil {
			t.Fatal(err)
		}
		blobs.put(key, stored)
		return rec, up.Id
	}

	// The default is 5 GiB; this account has 100 bytes of its own.
	if quota, err := storageQuota(ctx, q, user.ID); err != nil || quota != defaultStorageQuota {
		t.Fatalf("default quota: %d %v", quota, err)
	}
	if err := setUserQuota(ctx, q, user.ID, 100); err != nil {
		t.Fatal(err)
	}
	if rec, _ := upload("a.txt", 60, make([]byte, 60)); rec.Code != http.StatusCreated {
		t.Fatalf("within the quota: %d %s", rec.Code, rec.Body)
	}
	if rec, _ := upload("b.txt", 60, make([]byte, 60)); rec.Code != http.StatusRequestEntityTooLarge ||
		!strings.Contains(rec.Body.String(), "storage_quota_exceeded") {
		t.Fatalf("past the quota: %d %s", rec.Code, rec.Body)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	server.GetMyStorage(rec, req.WithContext(auth.WithUser(req.Context(), user)))
	var mine MyStorage
	if err := json.Unmarshal(rec.Body.Bytes(), &mine); err != nil || mine.UsedBytes != 60 || mine.QuotaBytes != 100 ||
		mine.Files != 1 || len(mine.Kinds) != 1 || mine.Kinds[0].Kind != StorageKindUsageKindText {
		t.Fatalf("my storage: %s", rec.Body)
	}

	// An upload that said 10 bytes and stored 90 is counted at 90 when sent,
	// and refused for going past the quota; one that fits keeps its true size.
	_, liar := upload("c.txt", 10, []byte(strings.Repeat("x", 90)))
	if liar == uuid.Nil {
		t.Fatal("the small announcement was refused")
	}
	if _, _, err := server.admitAttachments(ctx, user.ID, "any-model", []uuid.UUID{liar}); err != errStorageFull {
		t.Fatalf("a larger upload than announced: %v", err)
	}
	if err := setUserQuota(ctx, q, user.ID, -1); err != nil {
		t.Fatal(err)
	}
	if _, _, err := server.admitAttachments(ctx, user.ID, "any-model", []uuid.UUID{liar}); err != nil {
		t.Fatalf("with the default quota: %v", err)
	}
	var size int64
	if err := pool.QueryRow(ctx, `SELECT size_bytes FROM attachments WHERE id = $1`, liar).Scan(&size); err != nil || size != 90 {
		t.Errorf("recorded size %d %v, want 90", size, err)
	}
}
