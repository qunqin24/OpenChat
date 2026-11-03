package secrets

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func key(t *testing.T) string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(b)
}

func TestSealOpen(t *testing.T) {
	box, err := New(key(t))
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := box.Seal([]byte("gho_token"))
	if err != nil || bytes.Contains(sealed, []byte("gho_token")) {
		t.Fatalf("sealed %q, %v", sealed, err)
	}
	if plain, err := box.Open(sealed); err != nil || string(plain) != "gho_token" {
		t.Fatalf("opened %q, %v", plain, err)
	}
	other, _ := New(key(t))
	if _, err := other.Open(sealed); err == nil {
		t.Fatal("another key opened it")
	}
	sealed[len(sealed)-1] ^= 1
	if _, err := box.Open(sealed); err == nil {
		t.Fatal("a changed value opened")
	}
	if _, err := (&Box{}).Seal([]byte("x")); !errors.Is(err, ErrNoKey) {
		t.Fatalf("no key: %v", err)
	}
	if _, err := New(base64.StdEncoding.EncodeToString([]byte("short"))); err == nil {
		t.Fatal("a short key was taken")
	}
}

// The first start makes a key in the data directory, readable only by the
// server; later starts read the same one; the environment wins over the file.
func TestLoad(t *testing.T) {
	t.Setenv(EnvKey, "")
	dir := t.TempDir()
	first, source, err := Load(dir)
	if err != nil || source != Generated || !first.Ready() {
		t.Fatalf("first load: %v %v", source, err)
	}
	info, err := os.Stat(filepath.Join(dir, KeyFile))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("key file: %v %v", info, err)
	}
	sealed, _ := first.Seal([]byte("token"))
	again, source, err := Load(dir)
	if err != nil || source != FromFile {
		t.Fatalf("second load: %v %v", source, err)
	}
	if plain, err := again.Open(sealed); err != nil || string(plain) != "token" {
		t.Fatal("the key read back is not the one made")
	}
	t.Setenv(EnvKey, key(t))
	if _, source, _ := Load(dir); source != FromEnv {
		t.Fatalf("with the variable set the key came from %s", source)
	}
	t.Setenv(EnvKey, "")
	if err := os.WriteFile(filepath.Join(dir, KeyFile), []byte("short"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Load(dir); err == nil {
		t.Fatal("a malformed key file was taken")
	}
}
