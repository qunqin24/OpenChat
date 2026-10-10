package storage

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeS3 is a bucket in a map, standing in for the S3 side of a Switch.
type fakeS3 struct {
	name  string
	mu    sync.Mutex
	blobs map[string][]byte
}

func newFakeS3(name string) *fakeS3 { return &fakeS3{name: name, blobs: map[string][]byte{}} }

func (f *fakeS3) PresignPut(_ context.Context, key, _ string, _ time.Duration) (string, error) {
	return "https://" + f.name + ".test/put/" + key, nil
}

func (f *fakeS3) PresignGet(_ context.Context, key string, _ time.Duration) (string, error) {
	return "https://" + f.name + ".test/get/" + key, nil
}

func (f *fakeS3) ObjectURL(key string) string { return "https://" + f.name + ".test/" + key }

func (f *fakeS3) Size(_ context.Context, key string) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	data, ok := f.blobs[key]
	if !ok {
		return 0, errors.New("no such key")
	}
	return int64(len(data)), nil
}

func (f *fakeS3) Read(_ context.Context, key string, _ int64) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	data, ok := f.blobs[key]
	if !ok {
		return nil, errors.New("no such key")
	}
	return data, nil
}

// newTestSwitch is a Switch whose S3 side is the returned fake; a bucket named
// "broken" fails to build.
func newTestSwitch(t *testing.T) (*Switch, *fakeS3) {
	t.Helper()
	fake := newFakeS3("bucket")
	sw := NewSwitch([]byte("test-secret"))
	sw.newS3 = func(_ context.Context, cfg S3Settings) (Backend, error) {
		if cfg.Bucket == "broken" {
			return nil, errors.New("cannot build the bucket")
		}
		return fake, nil
	}
	return sw, fake
}

func localSettings(dir string) Settings {
	s := DefaultSettings()
	s.LocalDir = dir
	return s
}

func s3Settings(dir string) Settings {
	s := localSettings(dir)
	s.Driver = DriverS3
	s.S3.Bucket = "bucket"
	return s
}

func TestSwitchRoutesNewUploadsToTheCurrentDriver(t *testing.T) {
	sw, _ := newTestSwitch(t)
	ctx := context.Background()
	dir := filepath.Join(t.TempDir(), "files")

	if err := sw.Apply(ctx, localSettings(dir)); err != nil {
		t.Fatal(err)
	}
	if sw.Driver() != DriverLocal {
		t.Errorf("driver = %q", sw.Driver())
	}
	put, err := sw.PresignPut(ctx, "uploads/a.png", "image/png", time.Minute)
	if err != nil || !strings.HasPrefix(put, ContentPath+"uploads/a.png?") {
		t.Errorf("local PresignPut = %q, %v", put, err)
	}
	if url := sw.ObjectURL("uploads/a.png"); !strings.HasPrefix(url, ContentPath) {
		t.Errorf("local ObjectURL = %q", url)
	}

	// The change takes effect on the next call, with no restart.
	if err := sw.Apply(ctx, s3Settings(dir)); err != nil {
		t.Fatal(err)
	}
	if sw.Driver() != DriverS3 {
		t.Errorf("driver = %q", sw.Driver())
	}
	put, _ = sw.PresignPut(ctx, "uploads/a.png", "image/png", time.Minute)
	if put != "https://bucket.test/put/uploads/a.png" {
		t.Errorf("S3 PresignPut = %q", put)
	}
	if url := sw.ObjectURL("uploads/a.png"); url != "https://bucket.test/uploads/a.png" {
		t.Errorf("S3 ObjectURL = %q", url)
	}

	// Back to the disk: the bucket stays configured, so the driver, not the
	// bucket, decides where uploads go.
	if err := sw.Apply(ctx, func() Settings { s := s3Settings(dir); s.Driver = DriverLocal; return s }()); err != nil {
		t.Fatal(err)
	}
	if put, _ = sw.PresignPut(ctx, "uploads/a.png", "image/png", time.Minute); !strings.HasPrefix(put, ContentPath) {
		t.Errorf("PresignPut after switching back = %q", put)
	}
}

func TestSwitchReadsFallThroughLocalThenS3(t *testing.T) {
	sw, fake := newTestSwitch(t)
	ctx := context.Background()
	if err := sw.Apply(ctx, s3Settings(filepath.Join(t.TempDir(), "files"))); err != nil {
		t.Fatal(err)
	}
	fake.blobs["uploads/remote.png"] = []byte("from the bucket")
	if _, err := sw.Local().Put("uploads/local.png", strings.NewReader("from the disk"), 100); err != nil {
		t.Fatal(err)
	}

	// A file on the disk stays readable while the driver is S3.
	if size, err := sw.Size(ctx, "uploads/local.png"); err != nil || size != int64(len("from the disk")) {
		t.Errorf("Size(local) = %d, %v", size, err)
	}
	if data, err := sw.Read(ctx, "uploads/local.png", 100); err != nil || string(data) != "from the disk" {
		t.Errorf("Read(local) = %q, %v", data, err)
	}
	if url, _ := sw.PresignGet(ctx, "uploads/local.png", time.Minute); !strings.HasPrefix(url, ContentPath) {
		t.Errorf("PresignGet(local) = %q", url)
	}

	// Anything not on the disk is asked of the bucket.
	if data, err := sw.Read(ctx, "uploads/remote.png", 100); err != nil || string(data) != "from the bucket" {
		t.Errorf("Read(remote) = %q, %v", data, err)
	}
	if url, _ := sw.PresignGet(ctx, "uploads/remote.png", time.Minute); url != "https://bucket.test/get/uploads/remote.png" {
		t.Errorf("PresignGet(remote) = %q", url)
	}

	// And the same holds with the driver back on the disk, which is the point
	// of keeping the bucket configured.
	s := s3Settings(sw.Local().Dir())
	s.Driver = DriverLocal
	if err := sw.Apply(ctx, s); err != nil {
		t.Fatal(err)
	}
	if data, err := sw.Read(ctx, "uploads/remote.png", 100); err != nil || string(data) != "from the bucket" {
		t.Errorf("Read(remote) on the local driver = %q, %v", data, err)
	}
}

func TestSwitchWithoutABucketReadsOnlyTheDisk(t *testing.T) {
	sw, _ := newTestSwitch(t)
	ctx := context.Background()
	if err := sw.Apply(ctx, localSettings(filepath.Join(t.TempDir(), "files"))); err != nil {
		t.Fatal(err)
	}
	// The local store reports the miss.
	if _, err := sw.Size(ctx, "uploads/nope.png"); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("Size of a missing file = %v", err)
	}
	if _, err := sw.Read(ctx, "uploads/nope.png", 10); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("Read of a missing file = %v", err)
	}
}

func TestSwitchApplyFailureKeepsTheOldDrivers(t *testing.T) {
	sw, fake := newTestSwitch(t)
	ctx := context.Background()
	dir := filepath.Join(t.TempDir(), "files")
	if err := sw.Apply(ctx, s3Settings(dir)); err != nil {
		t.Fatal(err)
	}
	before := sw.Local()
	fake.blobs["uploads/remote.png"] = []byte("x")

	// A directory that cannot exist: its parent is a file.
	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	bad := []struct {
		name     string
		settings Settings
	}{
		{"an unusable directory", func() Settings { s := localSettings(filepath.Join(blocker, "sub")); return s }()},
		{"a bucket that will not build", func() Settings { s := s3Settings(dir); s.S3.Bucket = "broken"; return s }()},
		{"an S3 driver with no bucket", func() Settings { s := localSettings(dir); s.Driver = DriverS3; return s }()},
		{"an unknown driver", func() Settings { s := localSettings(dir); s.Driver = "ftp"; return s }()},
	}
	for _, tc := range bad {
		if err := sw.Apply(ctx, tc.settings); err == nil {
			t.Errorf("%s: Apply succeeded", tc.name)
		}
		if sw.Driver() != DriverS3 || sw.Local() != before {
			t.Errorf("%s: the old drivers were not kept", tc.name)
		}
		if data, err := sw.Read(ctx, "uploads/remote.png", 10); err != nil || string(data) != "x" {
			t.Errorf("%s: Read = %q, %v", tc.name, data, err)
		}
	}
}

func TestSwitchApplyReplacesTheDirectory(t *testing.T) {
	sw, _ := newTestSwitch(t)
	ctx := context.Background()
	first := filepath.Join(t.TempDir(), "first")
	second := filepath.Join(t.TempDir(), "second")
	if err := sw.Apply(ctx, localSettings(first)); err != nil {
		t.Fatal(err)
	}
	// The same settings reuse the same store.
	local := sw.Local()
	if err := sw.Apply(ctx, localSettings(first)); err != nil || sw.Local() != local {
		t.Errorf("unchanged settings built a new store (%v)", err)
	}
	if err := sw.Apply(ctx, localSettings(second)); err != nil {
		t.Fatal(err)
	}
	if sw.Local() == local || !strings.HasSuffix(sw.Local().Dir(), "second") {
		t.Errorf("Local dir = %q", sw.Local().Dir())
	}
	if _, err := sw.Local().Put("uploads/x.png", strings.NewReader("x"), 10); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(second, "uploads", "x.png")); err != nil {
		t.Errorf("a new upload did not land in the new directory: %v", err)
	}
	// Changing the directory does not move files.
	if _, err := sw.Size(ctx, "uploads/old.png"); err == nil {
		t.Error("a file from the previous directory is still found")
	}
}

func TestSwitchProbeLocal(t *testing.T) {
	sw, _ := newTestSwitch(t)
	dir := filepath.Join(t.TempDir(), "probe")
	if err := sw.Probe(context.Background(), localSettings(dir)); err != nil {
		t.Fatalf("Probe: %v", err)
	}
	// The probe cleans up after itself.
	entries, err := os.ReadDir(filepath.Join(dir, "probes"))
	if err != nil || len(entries) != 0 {
		t.Errorf("probes/ = %v, %v", entries, err)
	}
	// And did not touch the Switch.
	if sw.Local() != nil {
		t.Error("Probe applied the settings")
	}

	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	err = sw.Probe(context.Background(), localSettings(filepath.Join(blocker, "sub")))
	if err == nil || strings.Contains(err.Error(), "storage:") {
		t.Errorf("Probe of an unusable directory = %v", err)
	}
}

func TestLocalHasAndDelete(t *testing.T) {
	l := newTestLocal(t)
	if l.Has("uploads/a.png") || l.Has("../escape") || l.Has("") {
		t.Error("Has is true for something that is not there")
	}
	if _, err := l.Put("uploads/a.png", bytes.NewReader([]byte("x")), 10); err != nil {
		t.Fatal(err)
	}
	if !l.Has("uploads/a.png") || l.Has("uploads") {
		t.Error("Has is wrong for a file or a directory")
	}
	if err := l.Delete("uploads/a.png"); err != nil || l.Has("uploads/a.png") {
		t.Errorf("Delete = %v", err)
	}
	if err := l.Delete("uploads/a.png"); err != nil {
		t.Errorf("Delete of a missing object = %v", err)
	}
}
