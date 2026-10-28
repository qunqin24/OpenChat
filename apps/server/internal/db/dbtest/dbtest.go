// Package dbtest gives each test package a database of its own.
//
// `go test ./...` runs packages in parallel, and tests that share a database
// trip over each other: the agent tests cancel every running run they find,
// including the ones another package's tests just created. A database per
// package removes the sharing instead of asking every test to be careful.
package dbtest

import (
	"context"
	"errors"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/qunqin24/openchat/apps/server/internal/db"
)

// URLVar names the server the database-backed tests run on. The older
// per-suite names are still read, so a setup that has one of them keeps
// working; they all mean the same thing now.
const URLVar = "TEST_DATABASE_URL"

var legacyVars = []string{"AGENT_TEST_DATABASE_URL", "CHANNEL_TEST_DATABASE_URL", "CATALOG_TEST_DATABASE_URL"}

// URL is the server's address from the environment, or "" when none is set.
func URL() string {
	for _, name := range append([]string{URLVar}, legacyVars...) {
		if v := os.Getenv(name); v != "" {
			return v
		}
	}
	return ""
}

// Pool returns a migrated pool on the database named <name in the URL>_<pkg>,
// created on first use. The URL is only the address of a server (and the
// credentials to create databases on it): nothing but the name is read from it,
// and the database it names is never written to. Skips without a URL.
//
// The database is kept between runs, so a second run starts from the first's
// leftovers: tests have to cope with rows that are not theirs, which they must
// anyway when a developer points them at a database that has been used.
func Pool(t testing.TB, pkg string) *pgxpool.Pool {
	t.Helper()
	dsn := URL()
	if dsn == "" {
		t.Skipf("set %s to run the database-backed tests", URLVar)
	}
	ctx := context.Background()

	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("parse %s: %v", URLVar, err)
	}
	name := databaseName(cfg.ConnConfig.Database, pkg)
	if err := ensureDatabase(ctx, cfg.ConnConfig, name); err != nil {
		t.Fatalf("prepare database %s: %v", name, err)
	}

	own := cfg.Copy()
	own.ConnConfig.Database = name
	pool, err := pgxpool.NewWithConfig(ctx, own)
	if err != nil {
		t.Fatalf("connect to %s: %v", name, err)
	}
	t.Cleanup(pool.Close)
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate %s: %v", name, err)
	}
	return pool
}

var unsafeName = regexp.MustCompile(`[^a-z0-9_]+`)

// databaseName is base_pkg, as an identifier that needs no quoting. Postgres
// cuts names at 63 bytes, so a long base is shortened from the left of the
// package part rather than risk two packages ending up with the same one.
func databaseName(base, pkg string) string {
	clean := func(s string) string { return strings.Trim(unsafeName.ReplaceAllString(strings.ToLower(s), "_"), "_") }
	base, pkg = clean(base), clean(pkg)
	if base == "" {
		base = "openchat_test"
	}
	if room := 63 - len(pkg) - 1; len(base) > room && room > 0 {
		base = base[:room]
	}
	return base + "_" + pkg
}

// ensureDatabase creates name when it is missing. Packages are tested in
// parallel and each makes its own, so creation is serialized with an advisory
// lock: CREATE DATABASE copies a template and fails if two copy it at once.
func ensureDatabase(ctx context.Context, base *pgx.ConnConfig, name string) error {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	conn, err := ConnectServer(ctx, base)
	if err != nil {
		return err
	}
	defer conn.Close(context.Background())

	var exists bool
	if err := conn.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_database WHERE datname = $1)`, name).Scan(&exists); err != nil || exists {
		return err
	}
	if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock(hashtext('openchat-test-databases'))`); err != nil {
		return err
	}
	defer func() {
		_, _ = conn.Exec(context.Background(), `SELECT pg_advisory_unlock(hashtext('openchat-test-databases'))`)
	}()
	// Another package may have created it while this one waited for the lock.
	if err := conn.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_database WHERE datname = $1)`, name).Scan(&exists); err != nil || exists {
		return err
	}
	_, err = conn.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{name}.Sanitize())
	return err
}

// ConnectServer connects for server-level work (creating and dropping
// databases) as the configured user. The configured database only names a
// prefix and need not exist — `make test` points at openchat_test, which
// nothing creates — so a missing one falls back to the maintenance database.
func ConnectServer(ctx context.Context, cfg *pgx.ConnConfig) (*pgx.Conn, error) {
	conn, err := pgx.ConnectConfig(ctx, cfg)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "3D000" {
		fallback := cfg.Copy()
		fallback.Database = "postgres"
		return pgx.ConnectConfig(ctx, fallback)
	}
	return conn, err
}
