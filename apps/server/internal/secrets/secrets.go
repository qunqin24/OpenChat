// Package secrets seals what must not sit in the database in the clear: the
// tokens of people's own accounts that plugins act through, and the request
// headers administrators set for them (docs/design/plugins-plan.md §5).
//
// The key is made on the first start and kept in a file in the data
// directory (KeyFile, readable only by the server's user), or taken from
// OPENCHAT_SECRET_KEY when that is set - 32 bytes, base64. It is never put in
// the database beside what it seals: a key kept with the ciphertext would
// seal nothing against a leaked dump. Without a key (the file could not be
// written) the features that need it are off; the rest of the server runs as
// before. Lose the key and everything sealed with it is gone: people connect
// their accounts again - so the data directory is backed up with the database.
package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// EnvKey is the environment variable that, when set, holds the key instead of
// the file.
const EnvKey = "OPENCHAT_SECRET_KEY"

// KeyFile is the key's file in the data directory.
const KeyFile = "secret.key"

// version is the first byte of every sealed value: which key sealed it, so a
// new key can be brought in later while the old one still opens what it sealed.
const version byte = 1

// ErrNoKey is a seal or open asked of a box with no key.
var ErrNoKey = errors.New("secrets: " + EnvKey + " is not set")

// Box seals and opens values with the configured key. The zero Box, and a nil
// one, has no key: Ready is false and every seal and open fails with ErrNoKey.
type Box struct {
	aead cipher.AEAD
}

// Source says where a loaded key came from.
type Source string

const (
	FromEnv     Source = "environment"
	FromFile    Source = "file"
	Generated   Source = "generated"
	Unavailable Source = "unavailable"
)

// Load is the box of the key in the environment, else of the key in dir's
// KeyFile, else of a new key written there. A key that is there but not a
// 32-byte key is an error: the server should not start and seal nothing. A
// file that cannot be written is a box without a key, with why.
func Load(dir string) (*Box, Source, error) {
	if raw := strings.TrimSpace(os.Getenv(EnvKey)); raw != "" {
		box, err := New(raw)
		return box, FromEnv, err
	}
	path := filepath.Join(dir, KeyFile)
	if raw, err := os.ReadFile(path); err == nil {
		box, err := New(strings.TrimSpace(string(raw)))
		if err != nil {
			return nil, FromFile, fmt.Errorf("%s: %w", path, err)
		}
		return box, FromFile, nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return &Box{}, Unavailable, fmt.Errorf("reading %s: %w", path, err)
	}

	fresh := make([]byte, 32)
	if _, err := rand.Read(fresh); err != nil {
		return &Box{}, Unavailable, err
	}
	encoded := base64.StdEncoding.EncodeToString(fresh)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return &Box{}, Unavailable, fmt.Errorf("creating %s: %w", dir, err)
	}
	// O_EXCL: two servers starting on one volume at once make one key, and
	// the second reads the first's.
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, fs.ErrExist) {
		return Load(dir)
	}
	if err != nil {
		return &Box{}, Unavailable, fmt.Errorf("writing %s: %w", path, err)
	}
	if _, err := f.WriteString(encoded + "\n"); err != nil {
		_ = f.Close()
		_ = os.Remove(path)
		return &Box{}, Unavailable, fmt.Errorf("writing %s: %w", path, err)
	}
	if err := f.Close(); err != nil {
		return &Box{}, Unavailable, err
	}
	box, err := New(encoded)
	return box, Generated, err
}

// New is the box of a base64 key of 32 bytes.
func New(encoded string) (*Box, error) {
	key, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		if key, err = base64.RawURLEncoding.DecodeString(encoded); err != nil {
			return nil, fmt.Errorf("secrets: %s is not base64", EnvKey)
		}
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("secrets: %s must be 32 bytes, not %d (make one with: openssl rand -base64 32)", EnvKey, len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Box{aead: aead}, nil
}

// Ready reports whether the box has a key.
func (b *Box) Ready() bool { return b != nil && b.aead != nil }

// Seal is the value encrypted: the version, a random nonce, the ciphertext.
func (b *Box) Seal(plain []byte) ([]byte, error) {
	if !b.Ready() {
		return nil, ErrNoKey
	}
	nonce := make([]byte, b.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	out := append([]byte{version}, nonce...)
	return b.aead.Seal(out, nonce, plain, nil), nil
}

// Open is a sealed value decrypted. It fails for a value sealed with another
// key, or changed since.
func (b *Box) Open(sealed []byte) ([]byte, error) {
	if !b.Ready() {
		return nil, ErrNoKey
	}
	n := b.aead.NonceSize()
	if len(sealed) < 1+n || sealed[0] != version {
		return nil, errors.New("secrets: not a value this key sealed")
	}
	plain, err := b.aead.Open(nil, sealed[1:1+n], sealed[1+n:], nil)
	if err != nil {
		return nil, errors.New("secrets: the value does not open with this key")
	}
	return plain, nil
}
