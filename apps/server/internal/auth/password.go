// Package auth holds password hashing, session tokens, and the middleware that
// turns a cookie into a user on the request context.
package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// Argon2id parameters, at the OWASP-recommended settings for interactive
// logins. They are encoded into every hash, so raising them later still
// verifies passwords stored under the old cost.
const (
	argonMemory      = 64 * 1024 // KiB
	argonIterations  = 3
	argonParallelism = 4
	argonSaltLength  = 16
	argonKeyLength   = 32
)

var (
	ErrInvalidHash = errors.New("auth: password hash is malformed")
	ErrMismatch    = errors.New("auth: password does not match")
)

// HashPassword returns a self-describing argon2id hash:
// $argon2id$v=19$m=...,t=...,p=...$<salt>$<key>
func HashPassword(password string) (string, error) {
	salt := make([]byte, argonSaltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("auth: read salt: %w", err)
	}

	key := argon2.IDKey([]byte(password), salt, argonIterations, argonMemory, argonParallelism, argonKeyLength)

	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argonMemory, argonIterations, argonParallelism,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	), nil
}

// VerifyPassword reports whether password matches encoded. It returns
// ErrMismatch for a wrong password and ErrInvalidHash for an unreadable record,
// so callers can tell a failed login from a corrupt row.
func VerifyPassword(password, encoded string) error {
	memory, iterations, parallelism, salt, key, err := decodeHash(encoded)
	if err != nil {
		return err
	}

	candidate := argon2.IDKey([]byte(password), salt, iterations, memory, parallelism, uint32(len(key)))

	// Constant time: a byte-by-byte compare leaks how much of the hash matched.
	if subtle.ConstantTimeCompare(key, candidate) != 1 {
		return ErrMismatch
	}
	return nil
}

func decodeHash(encoded string) (memory uint32, iterations uint32, parallelism uint8, salt, key []byte, err error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return 0, 0, 0, nil, nil, ErrInvalidHash
	}

	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return 0, 0, 0, nil, nil, ErrInvalidHash
	}
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &iterations, &parallelism); err != nil {
		return 0, 0, 0, nil, nil, ErrInvalidHash
	}

	if salt, err = base64.RawStdEncoding.DecodeString(parts[4]); err != nil {
		return 0, 0, 0, nil, nil, ErrInvalidHash
	}
	if key, err = base64.RawStdEncoding.DecodeString(parts[5]); err != nil {
		return 0, 0, 0, nil, nil, ErrInvalidHash
	}
	return memory, iterations, parallelism, salt, key, nil
}

// DummyVerify burns roughly the same time as a real check. Login calls it when
// the address is unknown, so the response time does not reveal which addresses
// are registered.
func DummyVerify(password string) {
	salt := make([]byte, argonSaltLength)
	argon2.IDKey([]byte(password), salt, argonIterations, argonMemory, argonParallelism, argonKeyLength)
}
