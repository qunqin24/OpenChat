package auth

import (
	"strings"
	"testing"
)

func TestHashAndVerify(t *testing.T) {
	hash, err := HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}

	if err := VerifyPassword("correct horse battery staple", hash); err != nil {
		t.Errorf("VerifyPassword with the right password: %v", err)
	}
	if err := VerifyPassword("wrong password", hash); err != ErrMismatch {
		t.Errorf("VerifyPassword with a wrong password = %v, want ErrMismatch", err)
	}
}

func TestHashIsSalted(t *testing.T) {
	first, _ := HashPassword("same")
	second, _ := HashPassword("same")

	if first == second {
		t.Error("two hashes of the same password are identical — the salt is not random")
	}
}

func TestHashIsSelfDescribing(t *testing.T) {
	hash, _ := HashPassword("x")

	// $argon2id$v=19$m=...,t=...,p=...$salt$key
	if parts := strings.Split(hash, "$"); len(parts) != 6 || parts[1] != "argon2id" {
		t.Fatalf("unexpected hash layout: %q", hash)
	}
	if !strings.Contains(hash, "m=65536,t=3,p=4") {
		t.Errorf("cost parameters missing from %q", hash)
	}
}

func TestVerifyRejectsMalformed(t *testing.T) {
	for _, hash := range []string{
		"",
		"not-a-hash",
		"$argon2id$v=19$m=65536,t=3,p=4$onlyfourparts",
		"$bcrypt$v=19$m=65536,t=3,p=4$c2FsdA$a2V5",
		"$argon2id$v=99$m=65536,t=3,p=4$c2FsdA$a2V5",
	} {
		if err := VerifyPassword("x", hash); err != ErrInvalidHash {
			t.Errorf("VerifyPassword(%q) = %v, want ErrInvalidHash", hash, err)
		}
	}
}

func TestTokenHashIsStable(t *testing.T) {
	token, hash, err := NewToken()
	if err != nil {
		t.Fatalf("NewToken: %v", err)
	}
	if len(token) < 40 {
		t.Errorf("token is only %d chars, expected 32 bytes of entropy", len(token))
	}
	if got := HashToken(token); string(got) != string(hash) {
		t.Error("HashToken does not reproduce the hash returned by NewToken")
	}
}

func TestTokensAreUnique(t *testing.T) {
	seen := make(map[string]bool, 100)
	for range 100 {
		token, _, err := NewToken()
		if err != nil {
			t.Fatalf("NewToken: %v", err)
		}
		if seen[token] {
			t.Fatal("NewToken returned a duplicate")
		}
		seen[token] = true
	}
}
