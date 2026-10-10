package api

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/qunqin/openchat/apps/server/internal/config"
	"github.com/qunqin/openchat/apps/server/internal/storage"
)

// cleanupFixture is a server on a real local disk, with helpers that lay down
// conversations, attachments and files and then age them: the passes are all
// about what is old enough, so the clock is moved by editing the rows and the
// file times rather than by waiting.
type cleanupFixture struct {
	t      *testing.T
	pool   *pgxpool.Pool
	local  *storage.Local
	server *Server
	user   uuid.UUID
}

func newCleanupFixture(t *testing.T) *cleanupFixture {
	t.Helper()
	pool := agentTestPool(t)
	local, err := storage.NewLocal(filepath.Join(t.TempDir(), "data"), []byte("test-secret"), "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = local.Close() })
	f := &cleanupFixture{t: t, pool: pool, local: local, user: uuid.New()}
	f.server = NewServer(&config.Config{}, pool, local, nil)
	f.exec(`INSERT INTO users (id, email, name) VALUES ($1, $2, 'cleanup')`, f.user, f.user.String()+"@test.invalid")
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, f.user) })
	return f
}

func (f *cleanupFixture) exec(sql string, args ...any) {
	f.t.Helper()
	if _, err := f.pool.Exec(context.Background(), sql, args...); err != nil {
		f.t.Fatalf("%s: %v", sql, err)
	}
}

// conversation inserts one, with a user message carrying an attachment whose
// file is on the disk, and returns the conversation id and the file's key.
func (f *cleanupFixture) conversation(temporary bool, age, deletedAge time.Duration) (uuid.UUID, string) {
	f.t.Helper()
	ctx := context.Background()
	var id uuid.UUID
	var deletedAt *time.Time
	if deletedAge > 0 {
		at := time.Now().Add(-deletedAge)
		deletedAt = &at
	}
	if err := f.pool.QueryRow(ctx, `INSERT INTO conversations (user_id, temporary, created_at, deleted_at)
		VALUES ($1, $2, $3, $4) RETURNING id`, f.user, temporary, time.Now().Add(-age), deletedAt).Scan(&id); err != nil {
		f.t.Fatal(err)
	}
	var message uuid.UUID
	if err := f.pool.QueryRow(ctx, `INSERT INTO messages (conversation_id, role, content) VALUES ($1, 'user', 'hi') RETURNING id`, id).Scan(&message); err != nil {
		f.t.Fatal(err)
	}
	// A run too: it cascades with the conversation, and must not stop it.
	f.exec(`INSERT INTO runs (conversation_id, user_id, input_message_id, model_id, status, finished_at)
		VALUES ($1, $2, $3, 'm', 'completed', now())`, id, f.user, message)
	key := f.file()
	f.exec(`INSERT INTO attachments (message_id, user_id, storage_key, filename, content_type, size_bytes)
		VALUES ($1, $2, $3, 'a.png', 'image/png', 4)`, message, f.user, key)
	return id, key
}

// upload inserts an attachment no message claimed, created age ago.
func (f *cleanupFixture) upload(age time.Duration) string {
	f.t.Helper()
	key := f.file()
	f.exec(`INSERT INTO attachments (user_id, storage_key, filename, content_type, size_bytes, created_at)
		VALUES ($1, $2, 'a.png', 'image/png', 4, $3)`, f.user, key, time.Now().Add(-age))
	return key
}

// file writes a new file under uploads/ and returns its key.
func (f *cleanupFixture) file() string {
	f.t.Helper()
	key := fmt.Sprintf("uploads/2026/10/10/%s.png", uuid.New())
	if _, err := f.local.Put(key, strings.NewReader("data"), 100); err != nil {
		f.t.Fatal(err)
	}
	return key
}

// age makes a file on the disk look as if it was written age ago.
func (f *cleanupFixture) age(key string, age time.Duration) {
	f.t.Helper()
	at := time.Now().Add(-age)
	if err := os.Chtimes(filepath.Join(f.local.Dir(), filepath.FromSlash(key)), at, at); err != nil {
		f.t.Fatal(err)
	}
}

func (f *cleanupFixture) conversationExists(id uuid.UUID) bool {
	f.t.Helper()
	var n int
	if err := f.pool.QueryRow(context.Background(), `SELECT count(*) FROM conversations WHERE id = $1`, id).Scan(&n); err != nil {
		f.t.Fatal(err)
	}
	return n == 1
}

func (f *cleanupFixture) rowExists(key string) bool {
	f.t.Helper()
	var n int
	if err := f.pool.QueryRow(context.Background(), `SELECT count(*) FROM attachments WHERE storage_key = $1`, key).Scan(&n); err != nil {
		f.t.Fatal(err)
	}
	return n == 1
}

func (f *cleanupFixture) fileExists(key string) bool { return f.local.Has(key) }

func TestCleanupTemporaryChatsTakeTheirFilesWithThem(t *testing.T) {
	f := newCleanupFixture(t)

	oldChat, oldFile := f.conversation(true, temporaryLifetime+time.Hour, 0)
	freshChat, freshFile := f.conversation(true, time.Hour, 0)
	// A permanent conversation of the same age is none of this pass's business.
	permanent, permanentFile := f.conversation(false, 3*temporaryLifetime, 0)

	f.server.CleanupNow(context.Background())

	if f.conversationExists(oldChat) || f.rowExists(oldFile) || f.fileExists(oldFile) {
		t.Error("an expired temporary chat left its conversation, attachment row or file behind")
	}
	if !f.conversationExists(freshChat) || !f.rowExists(freshFile) || !f.fileExists(freshFile) {
		t.Error("a temporary chat inside its day was touched")
	}
	if !f.conversationExists(permanent) || !f.rowExists(permanentFile) || !f.fileExists(permanentFile) {
		t.Error("a permanent conversation was touched")
	}
}

func TestCleanupPurgesDeletedConversationsAfterRetention(t *testing.T) {
	f := newCleanupFixture(t)

	past, pastFile := f.conversation(false, 90*24*time.Hour, deletedRetention+time.Hour)
	within, withinFile := f.conversation(false, 90*24*time.Hour, deletedRetention-time.Hour)
	live, liveFile := f.conversation(false, 90*24*time.Hour, 0)

	f.server.CleanupNow(context.Background())

	if f.conversationExists(past) || f.rowExists(pastFile) || f.fileExists(pastFile) {
		t.Error("a conversation deleted past the retention was kept")
	}
	if !f.conversationExists(within) || !f.rowExists(withinFile) || !f.fileExists(withinFile) {
		t.Error("a conversation deleted inside the retention was purged")
	}
	if !f.conversationExists(live) || !f.rowExists(liveFile) || !f.fileExists(liveFile) {
		t.Error("a conversation that was never deleted was purged")
	}
}

func TestCleanupWorksOffABacklogInBatches(t *testing.T) {
	f := newCleanupFixture(t)
	const n = cleanupBatch*2 + 5
	f.exec(`INSERT INTO conversations (user_id, temporary, created_at)
		SELECT $1, true, now() - interval '3 days' FROM generate_series(1, $2)`, f.user, n)

	f.server.CleanupNow(context.Background())

	var left int
	if err := f.pool.QueryRow(context.Background(), `SELECT count(*) FROM conversations WHERE user_id = $1`, f.user).Scan(&left); err != nil {
		t.Fatal(err)
	}
	if left != 0 {
		t.Errorf("%d of %d expired conversations are left after one pass", left, n)
	}
}

func TestCleanupRemovesUnclaimedUploads(t *testing.T) {
	f := newCleanupFixture(t)

	stale := f.upload(unclaimedUploadLifetime + time.Hour)
	fresh := f.upload(time.Hour)
	// An old upload that a message did claim is an attachment, not an upload.
	_, claimed := f.conversation(false, 3*unclaimedUploadLifetime, 0)
	f.exec(`UPDATE attachments SET created_at = now() - interval '5 days' WHERE storage_key = $1`, claimed)

	f.server.CleanupNow(context.Background())

	if f.rowExists(stale) || f.fileExists(stale) {
		t.Error("a stale unclaimed upload was kept")
	}
	if !f.rowExists(fresh) || !f.fileExists(fresh) {
		t.Error("an unclaimed upload from the last hour was removed")
	}
	if !f.rowExists(claimed) || !f.fileExists(claimed) {
		t.Error("an old attachment that belongs to a message was removed")
	}
}

func TestCleanupSweepsOrphanedFilesButNeverLiveOnes(t *testing.T) {
	f := newCleanupFixture(t)

	// What a cascade used to leave behind: a file with no row, long ago.
	orphan := f.file()
	f.age(orphan, orphanGrace+time.Hour)
	// A file with no row that is recent could be anybody's, and is left.
	recentOrphan := f.file()
	// A file with a row is live however old it is: a conversation from last
	// year, and an upload still waiting to be sent.
	_, oldLive := f.conversation(false, 400*24*time.Hour, 0)
	f.age(oldLive, 400*24*time.Hour)
	pending := f.upload(time.Hour)
	f.age(pending, 3*orphanGrace)
	// Something that is not an upload at all.
	if _, err := f.local.Put("keep/other.txt", strings.NewReader("x"), 10); err != nil {
		t.Fatal(err)
	}
	f.age("keep/other.txt", 10*orphanGrace)
	// An upload that died halfway, and one that may still be going.
	dir := filepath.Join(f.local.Dir(), "uploads", "2026", "10", "10")
	staleTemp, liveTemp := filepath.Join(dir, ".tmp-stale"), filepath.Join(dir, ".tmp-live")
	for _, p := range []string{staleTemp, liveTemp} {
		if err := os.WriteFile(p, []byte("half"), 0o640); err != nil {
			t.Fatal(err)
		}
	}
	long := time.Now().Add(-3 * orphanGrace)
	if err := os.Chtimes(staleTemp, long, long); err != nil {
		t.Fatal(err)
	}

	f.server.CleanupNow(context.Background())

	if f.fileExists(orphan) {
		t.Error("an old file with no attachment was kept")
	}
	if !f.fileExists(recentOrphan) {
		t.Error("a recent file with no attachment was removed")
	}
	if !f.fileExists(oldLive) || !f.rowExists(oldLive) {
		t.Error("a file that a live attachment names was removed")
	}
	if !f.fileExists(pending) || !f.rowExists(pending) {
		t.Error("the file of a pending upload was removed")
	}
	if !f.fileExists("keep/other.txt") {
		t.Error("a file outside uploads/ was swept")
	}
	if _, err := os.Stat(staleTemp); !errors.Is(err, os.ErrNotExist) {
		t.Error("a stale temporary file was kept")
	}
	if _, err := os.Stat(liveTemp); err != nil {
		t.Error("a recent temporary file was removed")
	}
}

// The files are best-effort: one that cannot be removed must not stop the rest,
// or the rows, from going.
func TestCleanupSurvivesAFileThatWillNotGo(t *testing.T) {
	f := newCleanupFixture(t)
	first, firstFile := f.conversation(true, 3*temporaryLifetime, 0)
	second, secondFile := f.conversation(true, 3*temporaryLifetime, 0)

	// A directory where the file should be: removing it fails (it is not empty).
	path := filepath.Join(f.local.Dir(), filepath.FromSlash(firstFile))
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(path, "inner"), 0o750); err != nil {
		t.Fatal(err)
	}

	f.server.CleanupNow(context.Background())

	if f.conversationExists(first) || f.conversationExists(second) || f.rowExists(firstFile) || f.rowExists(secondFile) {
		t.Error("rows were not deleted")
	}
	if f.fileExists(secondFile) {
		t.Error("the file after the failing one was not removed")
	}
}
