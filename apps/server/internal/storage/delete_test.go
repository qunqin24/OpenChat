package storage

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestSwitchDeleteRemovesFromBothStores(t *testing.T) {
	sw, fake := newTestSwitch(t)
	ctx := context.Background()
	if err := sw.Apply(ctx, s3Settings(filepath.Join(t.TempDir(), "files"))); err != nil {
		t.Fatal(err)
	}
	// One file on each side, and one that a switch back and forth left on both.
	fake.blobs["uploads/remote.png"] = []byte("bucket")
	fake.blobs["uploads/both.png"] = []byte("bucket")
	for _, key := range []string{"uploads/local.png", "uploads/both.png"} {
		if _, err := sw.Local().Put(key, strings.NewReader("disk"), 100); err != nil {
			t.Fatal(err)
		}
	}

	for _, key := range []string{"uploads/remote.png", "uploads/local.png", "uploads/both.png", "uploads/never-existed.png"} {
		if err := sw.Delete(ctx, key); err != nil {
			t.Errorf("Delete(%s) = %v", key, err)
		}
	}
	if len(fake.blobs) != 0 {
		t.Errorf("the bucket still holds %d objects", len(fake.blobs))
	}
	for _, key := range []string{"uploads/local.png", "uploads/both.png"} {
		if sw.Local().Has(key) {
			t.Errorf("%s is still on the disk", key)
		}
	}
}

func TestSwitchDeleteWithoutABucketOnlyTouchesTheDisk(t *testing.T) {
	sw := NewSwitch([]byte("test-secret"))
	ctx := context.Background()
	if err := sw.Apply(ctx, localSettings(filepath.Join(t.TempDir(), "files"))); err != nil {
		t.Fatal(err)
	}
	if _, err := sw.Local().Put("uploads/a.png", strings.NewReader("x"), 10); err != nil {
		t.Fatal(err)
	}
	if err := sw.Delete(ctx, "uploads/a.png"); err != nil || sw.Local().Has("uploads/a.png") {
		t.Errorf("Delete = %v", err)
	}
	if err := sw.Delete(ctx, "../escape"); !errors.Is(err, ErrInvalidKey) {
		t.Errorf("Delete of an invalid key = %v", err)
	}
}

// A failing bucket must not stop the disk copy from going, and is still
// reported.
func TestSwitchDeleteTriesBothAndReportsFailure(t *testing.T) {
	sw, fake := newTestSwitch(t)
	ctx := context.Background()
	if err := sw.Apply(ctx, s3Settings(filepath.Join(t.TempDir(), "files"))); err != nil {
		t.Fatal(err)
	}
	if _, err := sw.Local().Put("uploads/a.png", strings.NewReader("x"), 10); err != nil {
		t.Fatal(err)
	}
	fake.deleteErr = errors.New("bucket unreachable")
	err := sw.Delete(ctx, "uploads/a.png")
	if err == nil || !strings.Contains(err.Error(), "bucket unreachable") {
		t.Errorf("Delete = %v, want the bucket's error", err)
	}
	if sw.Local().Has("uploads/a.png") {
		t.Error("the disk copy survived a failing bucket")
	}
}

// The S3 driver, against a server that speaks enough of the protocol to see
// what it is asked.
func TestStoreDeleteSendsDeleteObject(t *testing.T) {
	var mu sync.Mutex
	var seen []string
	status := http.StatusNoContent
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen = append(seen, r.Method+" "+r.URL.Path)
		code := status
		mu.Unlock()
		w.WriteHeader(code)
	}))
	t.Cleanup(srv.Close)

	store, err := New(context.Background(), S3Settings{
		Endpoint: srv.URL, Region: "us-east-1", Bucket: "bkt", PathStyle: true,
		AccessKeyID: "id", SecretAccessKey: "secret",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Delete(context.Background(), "uploads/2026/10/10/a b.png"); err != nil {
		t.Fatalf("Delete = %v", err)
	}
	if len(seen) != 1 || seen[0] != "DELETE /bkt/uploads/2026/10/10/a b.png" {
		t.Errorf("requests = %q", seen)
	}

	mu.Lock()
	status = http.StatusForbidden
	mu.Unlock()
	if err := store.Delete(context.Background(), "uploads/x.png"); err == nil {
		t.Error("a refused delete reported no error")
	}
}

func TestLocalWalkListsFilesUnderThePrefix(t *testing.T) {
	l := newTestLocal(t)
	for _, key := range []string{"uploads/2026/10/10/a.png", "uploads/2026/10/11/b.pdf", "probes/p.txt"} {
		if _, err := l.Put(key, strings.NewReader("data"), 10); err != nil {
			t.Fatal(err)
		}
	}
	// A temporary file is not an object.
	if err := os.WriteFile(filepath.Join(l.Dir(), "uploads", "2026", "10", "10", ".tmp-1234"), []byte("half"), 0o640); err != nil {
		t.Fatal(err)
	}

	var keys []string
	err := l.Walk("uploads", func(o Object) error {
		keys = append(keys, o.Key)
		if o.Size != 4 || time.Since(o.ModTime) > time.Minute {
			t.Errorf("object = %+v", o)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(keys, ","); got != "uploads/2026/10/10/a.png,uploads/2026/10/11/b.pdf" {
		t.Errorf("Walk = %s", got)
	}

	if err := l.Walk("nothing-here", func(Object) error { t.Error("called for a missing prefix"); return nil }); err != nil {
		t.Errorf("Walk of a missing prefix = %v", err)
	}
	if err := l.Walk("../out", func(Object) error { return nil }); !errors.Is(err, ErrInvalidKey) {
		t.Errorf("Walk of an invalid prefix = %v", err)
	}
	stop := errors.New("stop")
	if err := l.Walk("uploads", func(Object) error { return stop }); !errors.Is(err, stop) {
		t.Errorf("Walk did not return the callback's error: %v", err)
	}
}

func TestLocalRemoveStaleTemp(t *testing.T) {
	l := newTestLocal(t)
	if _, err := l.Put("uploads/2026/10/10/real.png", strings.NewReader("data"), 10); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(l.Dir(), "uploads", "2026", "10", "10")
	old, fresh := filepath.Join(dir, ".tmp-old"), filepath.Join(dir, ".tmp-fresh")
	for _, p := range []string{old, fresh} {
		if err := os.WriteFile(p, []byte("half"), 0o640); err != nil {
			t.Fatal(err)
		}
	}
	long := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(old, long, long); err != nil {
		t.Fatal(err)
	}

	n, err := l.RemoveStaleTemp("uploads", time.Now().Add(-24*time.Hour))
	if err != nil || n != 1 {
		t.Fatalf("RemoveStaleTemp = %d, %v", n, err)
	}
	if _, err := os.Stat(old); !errors.Is(err, os.ErrNotExist) {
		t.Error("the old temporary file survived")
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Error("a recent temporary file, possibly being written, was removed")
	}
	if !l.Has("uploads/2026/10/10/real.png") {
		t.Error("a real object was removed")
	}
}
