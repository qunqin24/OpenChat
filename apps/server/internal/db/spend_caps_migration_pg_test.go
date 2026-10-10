package db_test

import (
	"context"
	"strings"
	"testing"

	"github.com/pressly/goose/v3"
)

// The spending-caps migration (20261024000001) over models and a ledger as the
// version before it left them: nobody's model becomes capped, and the sum the
// cap runs can be answered from the index alone.

const (
	beforeSpendCaps = 20261019000001
	spendCaps       = 20261024000001
)

func TestSpendCapsMigrationLeavesModelsUncapped(t *testing.T) {
	s := newScratchAt(t, beforeSpendCaps)
	ctx := context.Background()

	// Models with settings of their own, and a ledger that has cost money: a
	// cap that appeared by itself would count all of it.
	s.exec(`INSERT INTO model_overrides (model_id, admin_only, context_limit) VALUES ('kept', true, 4096), ('plain', false, NULL)`)
	s.exec(`INSERT INTO usage_logs (kind, status, model_id, cost) VALUES ('chat', 'ok', 'kept', 1000), ('chat', 'ok', 'plain', 1000)`)

	if err := goose.UpToContext(ctx, s.sql, ".", spendCaps); err != nil {
		t.Fatalf("migrate up: %v", err)
	}

	rows, err := s.pool.Query(ctx, `
		SELECT model_id, capacity::text, capacity_period, capacity_admin_exempt, capacity_since::text, admin_only, context_limit
		FROM model_overrides ORDER BY model_id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	seen := 0
	for rows.Next() {
		var (
			id, period       string
			capacity, since  *string
			exempt, adminOnl bool
			contextLimit     *int64
		)
		if err := rows.Scan(&id, &capacity, &period, &exempt, &since, &adminOnl, &contextLimit); err != nil {
			t.Fatal(err)
		}
		if capacity != nil || since != nil || period != "total" || !exempt {
			t.Errorf("%s: capacity=%v since=%v period=%s exempt=%v; want no cap, total, exempt", id, capacity, since, period, exempt)
		}
		// What was there is untouched.
		if (id == "kept") != adminOnl || (id == "kept") != (contextLimit != nil) {
			t.Errorf("%s: admin_only=%v context_limit=%v changed", id, adminOnl, contextLimit)
		}
		seen++
	}
	if seen != 2 {
		t.Fatalf("%d override rows, want 2", seen)
	}

	// The model's id and a time range, with the cost carried in the index: the
	// cap's sum is read without touching the table.
	var def string
	if err := s.pool.QueryRow(ctx, `SELECT indexdef FROM pg_indexes WHERE indexname = 'usage_logs_model_idx'`).Scan(&def); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(def, "(model_id, created_at DESC) INCLUDE (cost)") {
		t.Errorf("usage_logs_model_idx = %s", def)
	}
	if _, err := s.pool.Exec(ctx, `VACUUM ANALYZE usage_logs`); err != nil {
		t.Fatal(err)
	}
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Release()
	// The table is two rows; the planner would rather read it whole. What is
	// being asked is whether the index can answer the query, not what it prefers.
	if _, err := conn.Exec(ctx, `SET enable_seqscan = off`); err != nil {
		t.Fatal(err)
	}
	var plan strings.Builder
	planRows, err := conn.Query(ctx, `EXPLAIN SELECT coalesce(sum(cost), 0) FROM usage_logs WHERE model_id = 'kept' AND created_at >= now() - interval '1 day'`)
	if err != nil {
		t.Fatal(err)
	}
	for planRows.Next() {
		var line string
		_ = planRows.Scan(&line)
		plan.WriteString(line + "\n")
	}
	planRows.Close()
	if !strings.Contains(plan.String(), "Index Only Scan using usage_logs_model_idx") {
		t.Errorf("the cap's sum is not an index-only scan:\n%s", plan.String())
	}

	// And down again: the columns go, the index is the plain one.
	if err := goose.DownToContext(ctx, s.sql, ".", beforeSpendCaps); err != nil {
		t.Fatalf("migrate down: %v", err)
	}
	var columns int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM information_schema.columns WHERE table_name = 'model_overrides' AND column_name LIKE 'capacity%'`).Scan(&columns); err != nil || columns != 0 {
		t.Errorf("%d capacity columns after rolling back, %v", columns, err)
	}
	if err := s.pool.QueryRow(ctx, `SELECT indexdef FROM pg_indexes WHERE indexname = 'usage_logs_model_idx'`).Scan(&def); err != nil || strings.Contains(def, "INCLUDE") {
		t.Errorf("usage_logs_model_idx after rolling back = %s, %v", def, err)
	}
	if err := goose.UpToContext(ctx, s.sql, ".", spendCaps); err != nil {
		t.Fatalf("migrate up again: %v", err)
	}
}
