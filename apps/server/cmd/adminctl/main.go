// Command adminctl creates or updates an account straight in the database.
//
//	go run ./cmd/adminctl -email you@example.com -name You -password 'secret123'
//
// It exists because there is no other way in once you are locked out. The
// public register endpoint only makes the FIRST account an admin; every later
// one is a plain user, and promoting it needs an admin session — which is
// exactly what you do not have if you have forgotten the password.
//
// Run against an address that already exists, it resets that account instead:
// same hash settings, and the account is re-enabled.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"

	"github.com/qunqin/openchat/apps/server/internal/auth"
)

func main() {
	email := flag.String("email", "", "address to sign in with")
	name := flag.String("name", "", "display name (defaults to the local part of the address)")
	password := flag.String("password", "", "new password")
	role := flag.String("role", "admin", "admin or user")
	flag.Parse()

	if err := run(*email, *name, *password, *role); err != nil {
		fmt.Fprintln(os.Stderr, "adminctl:", err)
		os.Exit(1)
	}
}

func run(email, name, password, role string) error {
	email = strings.TrimSpace(email)
	switch {
	case email == "":
		return fmt.Errorf("-email is required")
	case len(password) < 8:
		return fmt.Errorf("-password must be at least 8 characters")
	case role != "admin" && role != "user":
		return fmt.Errorf("-role must be admin or user, got %q", role)
	}
	if name == "" {
		name, _, _ = strings.Cut(email, "@")
	}

	// Same two candidates the server itself loads, so this picks up the same
	// DATABASE_URL whether it is run from the repo root or from apps/server.
	for _, path := range []string{".env", "../../.env"} {
		_ = godotenv.Load(path)
	}
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		return fmt.Errorf("DATABASE_URL is not set (looked in the environment and .env)")
	}

	// Hashed with the server's own settings, so the row is byte-for-byte what
	// registration would have written.
	hash, err := auth.HashPassword(password)
	if err != nil {
		return err
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return err
	}
	defer pool.Close()

	// Conflict target is the expression index that enforces case-insensitive
	// uniqueness, so running this twice updates the account rather than failing.
	var id string
	var created bool
	err = pool.QueryRow(ctx, `
		INSERT INTO users (email, name, password_hash, role)
		VALUES ($1, $2, $3, $4::user_role)
		ON CONFLICT (lower(email)) DO UPDATE
		   SET name          = EXCLUDED.name,
		       password_hash = EXCLUDED.password_hash,
		       role          = EXCLUDED.role,
		       disabled_at   = NULL,
		       updated_at    = now()
		RETURNING id, (xmax = 0)`, email, name, hash, role).Scan(&id, &created)
	if err != nil {
		return err
	}

	// A password change has to take the old sessions with it, the same as the
	// API does — otherwise the credential is replaced but the access is not.
	if !created {
		if _, err := pool.Exec(ctx, `DELETE FROM sessions WHERE user_id = $1`, id); err != nil {
			return err
		}
	}

	verb := "updated"
	if created {
		verb = "created"
	}
	fmt.Printf("%s %s (%s) as %s\n", verb, email, id, role)
	return nil
}
