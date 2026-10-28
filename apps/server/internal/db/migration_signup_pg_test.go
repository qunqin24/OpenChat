package db_test

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/qunqin24/openchat/apps/server/internal/db/dbtest"
	"github.com/qunqin24/openchat/apps/server/internal/db/migrations"
)

// The sign-up control migration (20261022000001) against accounts as the
// version before it left them: every one of them must count as approved, or
// the people already using an install would be locked out by an upgrade.

const (
	beforeSignupControl = 20261019000001
	signupControl       = 20261022000001
)

func TestSignupControlMigrationLeavesAccountsApproved(t *testing.T) {
	ctx := context.Background()
	pool, conn := migrationDatabase(t)
	if err := goose.UpToContext(ctx, conn, ".", beforeSignupControl); err != nil {
		t.Fatalf("migrate to the version before: %v", err)
	}

	// Accounts of every kind the old version had: an administrator, members,
	// one switched off, one that has never signed in with a password, a guest.
	insert := func(label, role string, disabled, guest bool) uuid.UUID {
		id := uuid.New()
		if _, err := pool.Exec(ctx, `
			INSERT INTO users (id, email, name, role, password_hash, disabled_at, guest)
			VALUES ($1, $2, $3, $4::user_role, 'x', CASE WHEN $5 THEN now() END, $6)`,
			id, label+id.String()+"@test.invalid", label+id.String()[:8], role, disabled, guest); err != nil {
			t.Fatal(err)
		}
		return id
	}
	ids := []uuid.UUID{
		insert("admin", "admin", false, false),
		insert("member", "user", false, false),
		insert("member", "user", false, false),
		insert("off", "user", true, false),
		insert("guest", "user", false, true),
	}

	if err := goose.UpToContext(ctx, conn, ".", signupControl); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	for _, id := range ids {
		var pending bool
		if err := pool.QueryRow(ctx, `SELECT pending FROM users WHERE id = $1`, id).Scan(&pending); err != nil {
			t.Fatal(err)
		}
		if pending {
			t.Errorf("account %v is pending after the migration", id)
		}
	}
	// And the session lookup - which excludes pending accounts - still finds them.
	var members int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM users WHERE id = ANY($1) AND NOT pending AND disabled_at IS NULL`, ids).Scan(&members); err != nil || members != 4 {
		t.Errorf("enabled and approved accounts: %d (%v), want 4", members, err)
	}
	// An account inserted without saying (how an administrator's are, and how
	// the old version's code would) is approved.
	late := uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO users (id, email, name) VALUES ($1, $2, 'late')`, late, late.String()+"@test.invalid"); err != nil {
		t.Fatal(err)
	}
	var pending bool
	if err := pool.QueryRow(ctx, `SELECT pending FROM users WHERE id = $1`, late).Scan(&pending); err != nil || pending {
		t.Errorf("a new row defaults to pending=%v (%v)", pending, err)
	}
	// The attempts table is there for the limits to use.
	if _, err := pool.Exec(ctx, `INSERT INTO auth_attempts (kind, ip) VALUES ('login', '203.0.113.1')`); err != nil {
		t.Errorf("auth_attempts: %v", err)
	}

	// And it goes back: the accounts stay, the table and the column go.
	if err := goose.DownToContext(ctx, conn, ".", beforeSignupControl); err != nil {
		t.Fatalf("roll back: %v", err)
	}
	assertNoColumn(t, conn, "users", "pending")
	var kept int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM users WHERE id = ANY($1)`, ids).Scan(&kept); err != nil || kept != len(ids) {
		t.Errorf("accounts after rolling back: %d (%v), want %d", kept, err, len(ids))
	}
}

// migrationDatabase is an empty scratch database for running migrations on,
// dropped when the test ends, with goose set up to read the embedded files.
func migrationDatabase(t *testing.T) (*pgxpool.Pool, *sql.DB) {
	t.Helper()
	dsn := dbtest.URL()
	if dsn == "" {
		t.Skip("set TEST_DATABASE_URL to run the migration tests")
	}
	ctx := context.Background()
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	admin, err := dbtest.ConnectServer(ctx, cfg.ConnConfig)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	name := "openchat_mig_" + strings.ReplaceAll(uuid.NewString()[:8], "-", "")
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+name); err != nil {
		_ = admin.Close(ctx)
		t.Skipf("cannot create a scratch database: %v", err)
	}
	t.Cleanup(func() {
		_, _ = admin.Exec(context.Background(), "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)")
		_ = admin.Close(context.Background())
	})
	own := cfg.Copy()
	own.ConnConfig.Database = name
	pool, err := pgxpool.NewWithConfig(ctx, own)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	conn := stdlib.OpenDBFromPool(pool)
	t.Cleanup(func() { _ = conn.Close() })

	goose.SetBaseFS(migrations.FS)
	if err := goose.SetDialect("postgres"); err != nil {
		t.Fatal(err)
	}
	goose.SetLogger(goose.NopLogger())
	return pool, conn
}

func assertNoColumn(t *testing.T, conn *sql.DB, table, column string) {
	t.Helper()
	var n int
	if err := conn.QueryRow(`SELECT count(*) FROM information_schema.columns WHERE table_name = $1 AND column_name = $2`, table, column).Scan(&n); err != nil || n != 0 {
		t.Errorf("column %s.%s still exists after rolling back (%v)", table, column, err)
	}
}
