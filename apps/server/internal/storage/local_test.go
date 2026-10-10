package storage

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newTestLocal(t *testing.T) *Local {
	t.Helper()
	l, err := NewLocal(filepath.Join(t.TempDir(), "uploads"), []byte("test-secret"), "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.root.Close() })
	return l
}

func TestLocalPutReadSize(t *testing.T) {
	l := newTestLocal(t)
	ctx := context.Background()
	key := "uploads/2026/10/10/abc.png"
	data := []byte("\x89PNG hello")

	n, err := l.Put(key, bytes.NewReader(data), 1<<20)
	if err != nil || n != int64(len(data)) {
		t.Fatalf("Put = %d, %v", n, err)
	}
	if size, err := l.Size(ctx, key); err != nil || size != int64(len(data)) {
		t.Errorf("Size = %d, %v", size, err)
	}
	got, err := l.Read(ctx, key, 1<<20)
	if err != nil || !bytes.Equal(got, data) {
		t.Errorf("Read = %q, %v", got, err)
	}
	if _, err := l.Read(ctx, key, int64(len(data))-1); !errors.Is(err, ErrTooLarge) {
		t.Errorf("Read over the limit: %v", err)
	}
	if _, err := l.Size(ctx, "uploads/missing.png"); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("Size of a missing object: %v", err)
	}

	// Overwriting replaces the whole object.
	if _, err := l.Put(key, strings.NewReader("new"), 10); err != nil {
		t.Fatal(err)
	}
	if got, _ := l.Read(ctx, key, 10); string(got) != "new" {
		t.Errorf("after overwrite: %q", got)
	}
}

func TestLocalPutRefusesOversizeAndLeavesNothing(t *testing.T) {
	l := newTestLocal(t)
	key := "uploads/2026/10/10/big.png"

	if _, err := l.Put(key, bytes.NewReader(make([]byte, 11)), 10); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("Put over the limit: %v", err)
	}
	// Exactly the limit is fine.
	if _, err := l.Put("uploads/ok.png", bytes.NewReader(make([]byte, 10)), 10); err != nil {
		t.Fatalf("Put at the limit: %v", err)
	}

	// Neither the object nor a temporary file is left from the refused one.
	var left []string
	_ = filepath.WalkDir(l.Dir(), func(p string, d fs.DirEntry, _ error) error {
		if !d.IsDir() {
			rel, _ := filepath.Rel(l.Dir(), p)
			left = append(left, rel)
		}
		return nil
	})
	if len(left) != 1 || left[0] != filepath.Join("uploads", "ok.png") {
		t.Errorf("files on disk: %v", left)
	}
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, errors.New("connection reset") }

func TestLocalPutIsAtomic(t *testing.T) {
	l := newTestLocal(t)
	key := "uploads/a.png"
	if _, err := l.Put(key, strings.NewReader("old"), 10); err != nil {
		t.Fatal(err)
	}
	// An upload that dies halfway leaves the previous object alone.
	if _, err := l.Put(key, failingReader{}, 10); err == nil {
		t.Fatal("a failed read was reported as success")
	}
	if got, _ := l.Read(context.Background(), key, 10); string(got) != "old" {
		t.Errorf("the object is %q after a failed overwrite", got)
	}
	entries, _ := os.ReadDir(filepath.Join(l.Dir(), "uploads"))
	if len(entries) != 1 {
		t.Errorf("a temporary file was left behind: %v", entries)
	}
}

func TestLocalRejectsKeysOutsideTheDirectory(t *testing.T) {
	l := newTestLocal(t)
	ctx := context.Background()

	// A file next to the storage directory that a traversal would reach.
	secret := filepath.Join(filepath.Dir(l.Dir()), "secret.txt")
	if err := os.WriteFile(secret, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}

	for _, key := range []string{
		"", "/etc/passwd", "../secret.txt", "uploads/../../secret.txt", "uploads/./a.png",
		"uploads//a.png", "uploads/a.png/", ".hidden", "uploads/.tmp-1", "a\\..\\b", "a\x00b", "uploads/a\nb",
		strings.Repeat("a", 1025),
	} {
		if _, err := l.Put(key, strings.NewReader("x"), 10); !errors.Is(err, ErrInvalidKey) {
			t.Errorf("Put(%q): %v", key, err)
		}
		if _, err := l.Read(ctx, key, 10); !errors.Is(err, ErrInvalidKey) {
			t.Errorf("Read(%q): %v", key, err)
		}
		if _, err := l.Size(ctx, key); !errors.Is(err, ErrInvalidKey) {
			t.Errorf("Size(%q): %v", key, err)
		}
		if _, err := l.PresignGet(ctx, key, time.Minute); !errors.Is(err, ErrInvalidKey) {
			t.Errorf("PresignGet(%q): %v", key, err)
		}
	}
	if got, _ := os.ReadFile(secret); string(got) != "secret" {
		t.Errorf("the file outside the directory is now %q", got)
	}
}

func TestLocalDoesNotFollowSymlinksOut(t *testing.T) {
	l := newTestLocal(t)
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.png"), []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	// A link inside the storage directory that points out of it, as a
	// directory and as a file.
	if err := os.Symlink(outside, filepath.Join(l.Dir(), "escape")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "secret.png"), filepath.Join(l.Dir(), "link.png")); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	for _, key := range []string{"escape/secret.png", "link.png"} {
		if _, err := l.Read(ctx, key, 100); err == nil {
			t.Errorf("Read(%q) followed a symlink out of the directory", key)
		}
	}
	if _, err := l.Put("escape/planted.png", strings.NewReader("x"), 10); err == nil {
		t.Error("Put wrote through a symlink out of the directory")
	}
	if _, err := os.Stat(filepath.Join(outside, "planted.png")); err == nil {
		t.Error("a file was planted outside the directory")
	}
}

func TestLocalSignatures(t *testing.T) {
	l := newTestLocal(t)
	ctx := context.Background()
	now := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	l.now = func() time.Time { return now }
	key := "uploads/2026/10/10/abc.png"

	signed, err := l.PresignPut(ctx, key, "image/png", 15*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(signed)
	if err != nil {
		t.Fatal(err)
	}
	if u.Path != ContentPath+key || u.Host != "" {
		t.Errorf("URL = %q, want a path under %s", signed, ContentPath)
	}
	expires, sig := u.Query().Get("expires"), u.Query().Get("signature")

	if err := l.Verify("PUT", key, expires, sig); err != nil {
		t.Errorf("a fresh signature: %v", err)
	}

	for name, check := range map[string]error{
		"another method":    l.Verify("GET", key, expires, sig),
		"another key":       l.Verify("PUT", "uploads/2026/10/10/other.png", expires, sig),
		"a later expiry":    l.Verify("PUT", key, "9999999999", sig),
		"no signature":      l.Verify("PUT", key, expires, ""),
		"a forged one":      l.Verify("PUT", key, expires, strings.Repeat("A", len(sig))),
		"no expiry":         l.Verify("PUT", key, "", sig),
		"a garbled expiry":  l.Verify("PUT", key, "soon", sig),
		"a traversing key":  l.Verify("PUT", "../"+key, expires, sig),
		"another secret":    otherSecret(t, l).Verify("PUT", key, expires, sig),
		"a get signature":   l.Verify("PUT", key, expires, mustSign(t, l, "GET", key, now.Add(time.Minute).Unix())),
		"truncated":         l.Verify("PUT", key, expires, sig[:len(sig)-1]),
		"an appended byte":  l.Verify("PUT", key, expires, sig+"A"),
		"a negative expiry": l.Verify("PUT", key, "-1", sig),
	} {
		if !errors.Is(check, ErrBadSignature) && !errors.Is(check, ErrInvalidKey) {
			t.Errorf("%s was accepted or misreported: %v", name, check)
		}
	}

	// Right up to the expiry it works; after, it is expired, not forged.
	l.now = func() time.Time { return now.Add(15 * time.Minute) }
	if err := l.Verify("PUT", key, expires, sig); err != nil {
		t.Errorf("at the expiry: %v", err)
	}
	l.now = func() time.Time { return now.Add(15*time.Minute + time.Second) }
	if err := l.Verify("PUT", key, expires, sig); !errors.Is(err, ErrExpired) {
		t.Errorf("after the expiry: %v", err)
	}
}

func otherSecret(t *testing.T, l *Local) *Local {
	t.Helper()
	other := *l
	other.secret = []byte("another-secret")
	return &other
}

func mustSign(t *testing.T, l *Local, method, key string, expires int64) string {
	t.Helper()
	return l.sign(method, key, expires)
}

func TestLocalURLs(t *testing.T) {
	l, err := NewLocal(t.TempDir(), []byte("s"), "https://chat.example.com/")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.root.Close() })
	if got, want := l.ObjectURL("uploads/a b.png"), "https://chat.example.com/api/v1/files/content/uploads/a%20b.png"; got != want {
		t.Errorf("ObjectURL = %q, want %q", got, want)
	}
	if _, err := NewLocal(t.TempDir(), nil, ""); err == nil {
		t.Error("a store without a secret was accepted")
	}
}

// PutObject is how the server stores bytes it holds itself (a guest's files):
// the object is whole afterwards, replaces an earlier one, and an empty one is
// still an object.
func TestLocalPutObject(t *testing.T) {
	l := newTestLocal(t)
	ctx := context.Background()
	key := "uploads/guest/2026/10/10/a.png"
	if err := l.PutObject(ctx, key, "image/png", []byte("first")); err != nil {
		t.Fatal(err)
	}
	if err := l.PutObject(ctx, key, "image/png", []byte("second")); err != nil {
		t.Fatal(err)
	}
	if got, err := l.Read(ctx, key, 1<<20); err != nil || string(got) != "second" {
		t.Errorf("Read = %q, %v", got, err)
	}
	if err := l.PutObject(ctx, "uploads/guest/empty", "", nil); err != nil || !l.Has("uploads/guest/empty") {
		t.Errorf("an empty object: %v", err)
	}
	if err := l.PutObject(ctx, "../outside", "", []byte("x")); !errors.Is(err, ErrInvalidKey) {
		t.Errorf("a key outside the directory: %v", err)
	}
}
