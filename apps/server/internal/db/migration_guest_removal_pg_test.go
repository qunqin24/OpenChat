package db_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/pressly/goose/v3"
)

// The migration that removes guest mode (20261028014955), on a database as an
// upgraded server has it: guest rows left behind by a crashed run, guests'
// moderation log and bans, and the guest settings. What was only the guests'
// goes; the accounts and everything of theirs stay.

const (
	beforeGuestRemoval = 20261028010018
	guestRemoval       = 20261028014955
)

func TestGuestRemovalMigration(t *testing.T) {
	ctx := context.Background()
	pool, conn := migrationDatabase(t)
	if err := goose.UpToContext(ctx, conn, ".", beforeGuestRemoval); err != nil {
		t.Fatalf("migrate to the version before: %v", err)
	}

	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
	}
	count := func(sql string, args ...any) (n int) {
		t.Helper()
		if err := pool.QueryRow(ctx, sql, args...).Scan(&n); err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
		return n
	}

	account, guest := uuid.New(), uuid.New()
	exec(`INSERT INTO users (id, email, name, role) VALUES ($1, 'a@test.invalid', 'a', 'admin')`, account)
	exec(`INSERT INTO users (id, email, name, guest) VALUES ($1, 'g@guest.invalid', 'g', true)`, guest)
	mine, theirs := uuid.New(), uuid.New()
	exec(`INSERT INTO conversations (id, user_id) VALUES ($1, $2), ($3, $4)`, mine, account, theirs, guest)
	exec(`INSERT INTO usage_logs (kind, status, model_id, user_id, cost) VALUES ('chat', 'ok', 'm', $1, 0.5), ('chat', 'ok', 'm', $2, 0.25)`, account, guest)

	accountEvent := uuid.New()
	exec(`INSERT INTO moderation_events (id, user_id, source, mode, categories, excerpt, sanction)
	      VALUES ($1, $2, 'send', 'openai', '{}', 'x', 'disabled')`, accountEvent, account)
	exec(`INSERT INTO moderation_events (guest_ip, source, mode, categories, excerpt, sanction)
	      VALUES ('203.0.113.1', 'guest', 'openai', '{}', 'y', 'banned')`)
	exec(`INSERT INTO guest_bans (ip) VALUES ('203.0.113.1')`)
	exec(`INSERT INTO guest_requests (ip) VALUES ('203.0.113.1')`)
	exec(`INSERT INTO app_settings (key, value) VALUES ('guest_mode', 'true'), ('guest_ban_hours', '24'), ('max_concurrent_runs', '4')`)

	if err := goose.UpToContext(ctx, conn, ".", guestRemoval); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	if count(`SELECT count(*) FROM users WHERE id = $1`, account) != 1 || count(`SELECT count(*) FROM users WHERE id = $1`, guest) != 0 {
		t.Error("the account should stay and the guest go")
	}
	if count(`SELECT count(*) FROM conversations WHERE id = $1`, mine) != 1 || count(`SELECT count(*) FROM conversations WHERE id = $1`, theirs) != 0 {
		t.Error("the account's conversation should stay and the guest's go")
	}
	// Spend is counted from the ledger: the guest's request stays, ownerless.
	if count(`SELECT count(*) FROM usage_logs WHERE model_id = 'm'`) != 2 || count(`SELECT count(*) FROM usage_logs WHERE model_id = 'm' AND user_id IS NULL`) != 1 {
		t.Error("the ledger lost a request or kept the guest as its owner")
	}
	if count(`SELECT count(*) FROM moderation_events`) != 1 || count(`SELECT count(*) FROM moderation_events WHERE id = $1`, accountEvent) != 1 {
		t.Error("the moderation log should keep only the account's event")
	}
	if count(`SELECT count(*) FROM app_settings WHERE key LIKE 'guest%'`) != 0 || count(`SELECT count(*) FROM app_settings WHERE key = 'max_concurrent_runs'`) != 1 {
		t.Error("the guest settings should go and the others stay")
	}
	assertNoColumn(t, conn, "users", "guest")
	assertNoColumn(t, conn, "runs", "client_ip")
	assertNoColumn(t, conn, "moderation_events", "guest_ip")
	for _, table := range []string{"guest_bans", "guest_requests"} {
		if count(`SELECT count(*) FROM information_schema.tables WHERE table_name = $1`, table) != 0 {
			t.Errorf("table %s is still there", table)
		}
	}
	if _, err := pool.Exec(ctx, `INSERT INTO moderation_events (user_id, source, mode, categories, excerpt) VALUES ($1, 'guest', 'openai', '{}', 'z')`, account); err == nil {
		t.Error("the log still takes a guest's source")
	}

	// And it goes back, empty: the columns and tables return, the rows do not.
	if err := goose.DownToContext(ctx, conn, ".", beforeGuestRemoval); err != nil {
		t.Fatalf("roll back: %v", err)
	}
	if count(`SELECT count(*) FROM users WHERE guest`) != 0 || count(`SELECT count(*) FROM guest_bans`) != 0 || count(`SELECT count(*) FROM runs WHERE client_ip IS NOT NULL`) != 0 {
		t.Error("rolling back brought rows back")
	}
}
