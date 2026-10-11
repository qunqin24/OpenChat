package storage

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// ContentPath is where the local driver's URLs point, under the API prefix. The
// object key follows it, S3 path-style.
const ContentPath = "/api/v1/files/content/"

var (
	// ErrInvalidKey is a key that is not a plain relative path inside the
	// storage directory.
	ErrInvalidKey = errors.New("storage: invalid object key")
	// ErrBadSignature is a URL whose signature does not match what was asked for.
	ErrBadSignature = errors.New("storage: bad signature")
	// ErrExpired is a correctly signed URL that is past its expiry.
	ErrExpired = errors.New("storage: signature expired")
)

// Local keeps objects as files under one directory on the server's disk. It is
// the driver for when there is no S3: the same presigned-URL flow, with URLs
// that point back at this API instead of at a bucket.
//
// The URLs are HMAC-signed over the method, the key and the expiry, so one that
// lets you read a file cannot be turned into a write, and neither works for any
// other file. Verifying them is up to the HTTP handlers; this type only issues
// and checks the signatures.
type Local struct {
	// root confines every file operation to the directory. Unlike checking the
	// path by hand, it also refuses to follow a symlink out of it.
	root      *os.Root
	dir       string
	secret    []byte
	publicURL string
	now       func() time.Time
}

// NewLocal opens the directory, creating it when it is missing. publicURL is
// the origin the issued URLs are under; empty issues them relative.
func NewLocal(dir string, secret []byte, publicURL string) (*Local, error) {
	if len(secret) == 0 {
		return nil, errors.New("storage: a local store needs a signing secret")
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, fmt.Errorf("storage: create %q: %w", dir, err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, fmt.Errorf("storage: open %q: %w", dir, err)
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		abs = dir
	}
	return &Local{root: root, dir: abs, secret: secret, publicURL: strings.TrimRight(publicURL, "/"), now: time.Now}, nil
}

// Dir is the absolute storage directory.
func (l *Local) Dir() string { return l.dir }

// Close releases the directory handle. Only for a store that was built to be
// thrown away, like a probe's; the live one is replaced, never closed, because
// a request may still be using it.
func (l *Local) Close() error { return l.root.Close() }

// checkKey accepts only keys that are plain relative paths: no empty, dot or
// dot-dot segments, nothing hidden (which is where temporary files live), no
// backslash or control character. os.Root would stop an escape anyway; this
// keeps the bad key from being issued a URL at all.
func checkKey(key string) error {
	if key == "" || len(key) > 1024 || strings.HasPrefix(key, "/") || !filepath.IsLocal(filepath.FromSlash(key)) {
		return ErrInvalidKey
	}
	for _, r := range key {
		if r < 0x20 || r == 0x7f || r == '\\' {
			return ErrInvalidKey
		}
	}
	for _, segment := range strings.Split(key, "/") {
		if segment == "" || strings.HasPrefix(segment, ".") {
			return ErrInvalidKey
		}
	}
	return nil
}

// sign is the signature for one method on one key until one moment.
func (l *Local) sign(method, key string, expires int64) string {
	mac := hmac.New(sha256.New, l.secret)
	// The prefix keeps a signature made for this purpose from being valid for
	// any other use of the same secret.
	_, _ = fmt.Fprintf(mac, "openchat-storage-v1\n%s\n%s\n%d", method, key, expires)
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// Verify checks the `expires` and `signature` query values of a request for
// method on key.
func (l *Local) Verify(method, key, expires, signature string) error {
	if err := checkKey(key); err != nil {
		return err
	}
	at, err := strconv.ParseInt(expires, 10, 64)
	if err != nil {
		return ErrBadSignature
	}
	// The signature first: whether a URL has expired is not something to tell
	// somebody who could not have made it.
	if !hmac.Equal([]byte(signature), []byte(l.sign(method, key, at))) {
		return ErrBadSignature
	}
	if l.now().Unix() > at {
		return ErrExpired
	}
	return nil
}

func (l *Local) presign(method, key string, ttl time.Duration) (string, error) {
	if err := checkKey(key); err != nil {
		return "", err
	}
	expires := l.now().Add(ttl).Unix()
	q := url.Values{"expires": {strconv.FormatInt(expires, 10)}, "signature": {l.sign(method, key, expires)}}
	return l.ObjectURL(key) + "?" + q.Encode(), nil
}

// PresignPut returns a URL the browser can PUT the file to. The content type is
// not part of the signature: the handler holds the upload to the one recorded
// when it was issued.
func (l *Local) PresignPut(_ context.Context, key, _ string, ttl time.Duration) (string, error) {
	return l.presign("PUT", key, ttl)
}

// PresignGet returns a time-limited read URL for a stored object.
func (l *Local) PresignGet(_ context.Context, key string, ttl time.Duration) (string, error) {
	return l.presign("GET", key, ttl)
}

// ObjectURL is the canonical (unsigned) location of a key.
func (l *Local) ObjectURL(key string) string {
	return l.publicURL + ContentPath + escapeKey(key)
}

// Put writes r as the object, refusing more than limit bytes. The bytes go to a
// temporary file that is renamed over the key when complete, so a reader sees
// the whole object or none of it, and an upload that fails leaves nothing.
func (l *Local) Put(key string, r io.Reader, limit int64) (int64, error) {
	return l.put(key, r, limit, false)
}

// ErrExists is a write-once object that has been written.
var ErrExists = errors.New("storage: the object already exists")

// PutNew is Put for an object that must not be replaced: it is committed only
// if nothing is at the key (ErrExists otherwise), and that check and the commit
// are one step, so of two writers racing for a key exactly one wins. A browser's
// upload is written this way (see the handler): the bytes content moderation
// reads are then the bytes that are there afterwards.
func (l *Local) PutNew(key string, r io.Reader, limit int64) (int64, error) {
	return l.put(key, r, limit, true)
}

func (l *Local) put(key string, r io.Reader, limit int64, exclusive bool) (int64, error) {
	if err := checkKey(key); err != nil {
		return 0, err
	}
	dir := path.Dir(key)
	if err := l.root.MkdirAll(dir, 0o750); err != nil {
		return 0, fmt.Errorf("storage: create directory for %q: %w", key, err)
	}

	var nonce [8]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return 0, err
	}
	tmp := path.Join(dir, tempPrefix+hex.EncodeToString(nonce[:]))
	f, err := l.root.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o640)
	if err != nil {
		return 0, fmt.Errorf("storage: create temporary file for %q: %w", key, err)
	}
	done := false
	defer func() {
		if !done {
			_ = f.Close()
			_ = l.root.Remove(tmp)
		}
	}()

	// One byte past the limit is enough to know the object is too big.
	n, err := io.Copy(f, io.LimitReader(r, limit+1))
	if err != nil {
		return 0, fmt.Errorf("storage: write %q: %w", key, err)
	}
	if n > limit {
		return 0, ErrTooLarge
	}
	if err := f.Sync(); err != nil {
		return 0, fmt.Errorf("storage: sync %q: %w", key, err)
	}
	if err := f.Close(); err != nil {
		return 0, fmt.Errorf("storage: close %q: %w", key, err)
	}
	if exclusive {
		// A hard link fails when the name is taken, which a rename would not.
		switch err := l.root.Link(tmp, key); {
		case err == nil:
			// The key and the temporary name are one file now; the name goes.
			done = true
			_ = l.root.Remove(tmp)
			return n, nil
		case errors.Is(err, fs.ErrExist):
			return 0, ErrExists
		case l.Has(key):
			return 0, ErrExists
		}
		// Links the filesystem does not do: the check and the rename are two
		// steps there, the most that can be had.
	}
	if err := l.root.Rename(tmp, key); err != nil {
		return 0, fmt.Errorf("storage: commit %q: %w", key, err)
	}
	done = true
	return n, nil
}

// PutObject stores data as the object; see Put for how it is written.
func (l *Local) PutObject(_ context.Context, key, _ string, data []byte) error {
	_, err := l.Put(key, bytes.NewReader(data), int64(len(data)))
	return err
}

// Open returns the object's file, for a handler that serves it.
func (l *Local) Open(key string) (*os.File, error) {
	if err := checkKey(key); err != nil {
		return nil, err
	}
	f, err := l.root.Open(key)
	if err != nil {
		return nil, fmt.Errorf("storage: open %q: %w", key, err)
	}
	info, err := f.Stat()
	if err == nil && !info.Mode().IsRegular() {
		err = fs.ErrNotExist
	}
	if err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("storage: open %q: %w", key, err)
	}
	return f, nil
}

// Size is the stored size of an object.
func (l *Local) Size(_ context.Context, key string) (int64, error) {
	f, err := l.Open(key)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return 0, fmt.Errorf("storage: stat %q: %w", key, err)
	}
	return info.Size(), nil
}

// Read returns an object's bytes, refusing one larger than limit rather than
// holding it in memory to find out how big it is.
func (l *Local) Read(_ context.Context, key string, limit int64) ([]byte, error) {
	f, err := l.Open(key)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, fmt.Errorf("storage: read %q: %w", key, err)
	}
	if int64(len(data)) > limit {
		return nil, ErrTooLarge
	}
	return data, nil
}

// Has reports whether the object exists as a regular file. It is a cheap stat,
// for choosing between stores before reading.
func (l *Local) Has(key string) bool {
	if checkKey(key) != nil {
		return false
	}
	info, err := l.root.Stat(key)
	return err == nil && info.Mode().IsRegular()
}

// Delete removes an object. One that is not there is not an error.
func (l *Local) Delete(_ context.Context, key string) error {
	if err := checkKey(key); err != nil {
		return err
	}
	if err := l.root.Remove(key); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("storage: delete %q: %w", key, err)
	}
	return nil
}

// Object is a stored file as Walk reports it.
type Object struct {
	Key     string
	Size    int64
	ModTime time.Time
}

// tempPrefix starts the name of a file Put is still writing.
const tempPrefix = ".tmp-"

// Walk calls fn for every regular file under the directory prefix, in key
// order. Files Put is still writing are left out, being no object yet, and so
// is a prefix that does not exist. An error from fn stops the walk and is
// returned.
func (l *Local) Walk(prefix string, fn func(Object) error) error {
	if err := checkKey(prefix); err != nil {
		return err
	}
	return fs.WalkDir(l.root.FS(), prefix, func(p string, d fs.DirEntry, err error) error {
		switch {
		case errors.Is(err, fs.ErrNotExist):
			// Gone since it was listed, or never there.
			return nil
		case err != nil:
			return err
		case d.IsDir() || !d.Type().IsRegular() || strings.HasPrefix(d.Name(), ".") || checkKey(p) != nil:
			return nil
		}
		info, err := d.Info()
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		} else if err != nil {
			return err
		}
		return fn(Object{Key: p, Size: info.Size(), ModTime: info.ModTime()})
	})
}

// RemoveStaleTemp removes the temporary files under prefix that were last
// written before the given time: what an upload leaves behind when the process
// dies in the middle of it. Put cleans up after every failure it survives, so
// anything old enough is not coming back to be renamed into place.
func (l *Local) RemoveStaleTemp(prefix string, before time.Time) (int, error) {
	if err := checkKey(prefix); err != nil {
		return 0, err
	}
	removed := 0
	err := fs.WalkDir(l.root.FS(), prefix, func(p string, d fs.DirEntry, err error) error {
		switch {
		case errors.Is(err, fs.ErrNotExist):
			return nil
		case err != nil:
			return err
		case d.IsDir() || !d.Type().IsRegular() || !strings.HasPrefix(d.Name(), tempPrefix):
			return nil
		}
		info, err := d.Info()
		if err != nil || !info.ModTime().Before(before) {
			return nil
		}
		if err := l.root.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("storage: remove %q: %w", p, err)
		}
		removed++
		return nil
	})
	return removed, err
}

// Probe checks that the directory can be written, read and deleted from, by
// doing each to a small object under probes/. The error is short and in
// English, for showing to the operator who configured the directory.
func (l *Local) Probe(ctx context.Context) error {
	key, payload := probeObject()
	if _, err := l.Put(key, bytes.NewReader(payload), int64(len(payload))); err != nil {
		return fmt.Errorf("write failed: %s", describeFileError(err))
	}
	defer func() { _ = l.Delete(ctx, key) }()
	got, err := l.Read(ctx, key, int64(len(payload)))
	if err != nil {
		return fmt.Errorf("read failed: %s", describeFileError(err))
	}
	if !bytes.Equal(got, payload) {
		return errors.New("read back different bytes than were written")
	}
	if err := l.Delete(ctx, key); err != nil {
		return fmt.Errorf("delete failed: %s", describeFileError(err))
	}
	return nil
}

// describeFileError is the operating system's reason for a failure, without the
// chain of operation names around it.
func describeFileError(err error) string {
	var pathErr *fs.PathError
	if errors.As(err, &pathErr) {
		return pathErr.Err.Error()
	}
	return err.Error()
}

// probeObject is a key under probes/ that no real attachment can have, with
// the bytes to store under it.
func probeObject() (string, []byte) {
	var nonce [8]byte
	_, _ = rand.Read(nonce[:])
	return "probes/" + hex.EncodeToString(nonce[:]) + ".txt", []byte("openchat storage probe")
}
