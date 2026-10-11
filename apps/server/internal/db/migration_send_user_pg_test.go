package db_test

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/qunqin/openchat/apps/server/internal/db/dbtest"
	"github.com/qunqin/openchat/apps/server/internal/db/migrations"
)

// The migration that adds the per-channel "send the user identifier" switch
// (20261025000002) gives channels as they were the default a new one would
// get: on for OpenAI's and Anthropic's own endpoints, off for every relay.

const (
	beforeChannelSendUser = 20261025000001
	channelSendUser       = 20261025000002
)

func TestChannelSendUserMigrationBackfillsByEndpoint(t *testing.T) {
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
	if err := goose.UpToContext(ctx, conn, ".", beforeChannelSendUser); err != nil {
		t.Fatalf("migrate to the version before: %v", err)
	}

	channels := map[string]struct {
		kind, baseURL string
		want          bool
	}{
		"openai default":             {"openai", "", true},
		"anthropic default":          {"anthropic", "", true},
		"openai on its own host":     {"openai", "https://api.openai.com/v1", true},
		"relay pointed at anthropic": {"openai-compatible", "HTTPS://API.ANTHROPIC.COM/v1/", true},
		"openai through a relay":     {"openai", "https://relay.example.com/v1", false},
		"look-alike host":            {"openai-compatible", "https://api.openai.com.evil.example/v1", false},
		"host only in the path":      {"openai-compatible", "https://relay.example.com/api.openai.com", false},
		"deepseek default":           {"deepseek", "", false},
		"gemini default":             {"gemini", "", false},
		"relay with no URL":          {"openai-compatible", "", false},
	}
	ids := map[string]uuid.UUID{}
	for label, c := range channels {
		id := uuid.New()
		ids[label] = id
		if _, err := pool.Exec(ctx, `INSERT INTO channels (id, name, type, base_url) VALUES ($1, $2, $3::channel_type, $4)`,
			id, label, c.kind, c.baseURL); err != nil {
			t.Fatalf("%s: %v", label, err)
		}
	}

	if err := goose.UpToContext(ctx, conn, ".", channelSendUser); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	for label, c := range channels {
		var got bool
		if err := pool.QueryRow(ctx, `SELECT send_user_id FROM channels WHERE id = $1`, ids[label]).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got != c.want {
			t.Errorf("%s: send_user_id = %v, want %v", label, got, c.want)
		}
	}
}
