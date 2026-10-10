package db_test

import (
	"context"
	"database/sql"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/qunqin/openchat/apps/server/internal/db/dbtest"
	"github.com/qunqin/openchat/apps/server/internal/db/migrations"
)

// The branches migration (20261015000001) against data shaped the way the
// versions before it left it. Needs a server that lets the test make a
// database of its own (TEST_DATABASE_URL names where; the database in it
// is only connected to in order to create the other).

const (
	beforeBranches = 20261014000001
	branches       = 20261015000001
)

// oldTranscript is what the version before the migration showed: the rows of
// ListMessages as it was then.
const oldTranscript = `
SELECT m.id
FROM messages m
WHERE m.conversation_id = $1
  AND NOT EXISTS (SELECT 1 FROM runs r WHERE r.id = m.run_id AND r.superseded)
  AND NOT (m.role = 'user'
    AND EXISTS (SELECT 1 FROM runs r WHERE r.input_message_id = m.id)
    AND NOT EXISTS (SELECT 1 FROM runs r WHERE r.input_message_id = m.id AND NOT r.superseded))
ORDER BY m.created_at, m.id`

const newTranscript = `
SELECT m.id FROM messages m
WHERE m.id IN (SELECT conversation_transcript($1::uuid))
ORDER BY m.created_at, m.id`

type scratch struct {
	t    *testing.T
	pool *pgxpool.Pool
	sql  *sql.DB
	tick int
	user uuid.UUID
}

func newScratch(t *testing.T) *scratch { return newScratchAt(t, beforeBranches) }

// newScratchAt is a scratch database migrated to a version and no further.
func newScratchAt(t *testing.T, version int64) *scratch {
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
	s := &scratch{t: t, pool: pool, sql: stdlib.OpenDBFromPool(pool)}
	t.Cleanup(func() { _ = s.sql.Close() })

	goose.SetBaseFS(migrations.FS)
	if err := goose.SetDialect("postgres"); err != nil {
		t.Fatal(err)
	}
	goose.SetLogger(goose.NopLogger())
	if err := goose.UpToContext(ctx, s.sql, ".", version); err != nil {
		t.Fatalf("migrate to the version before: %v", err)
	}
	s.user = uuid.New()
	s.exec(`INSERT INTO users (id, email, name) VALUES ($1, $2, 'u')`, s.user, s.user.String()+"@test.invalid")
	return s
}

func (s *scratch) exec(q string, args ...any) {
	s.t.Helper()
	if _, err := s.pool.Exec(context.Background(), q, args...); err != nil {
		s.t.Fatalf("%s: %v", q, err)
	}
}

func (s *scratch) conversation() uuid.UUID {
	id := uuid.New()
	s.exec(`INSERT INTO conversations (id, user_id, title) VALUES ($1, $2, 'c')`, id, s.user)
	return id
}

func (s *scratch) at() time.Time {
	s.tick++
	return time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC).Add(time.Duration(s.tick) * time.Second)
}

// input writes a user message as the old code did: `editOf` the turn's first
// question for an edit.
func (s *scratch) input(conv uuid.UUID, content string, editOf *uuid.UUID) uuid.UUID {
	id := uuid.New()
	ts := s.at()
	s.exec(`INSERT INTO messages (id, conversation_id, role, content, edit_of, created_at, updated_at)
		VALUES ($1, $2, 'user', $3, $4, $5, $5)`, id, conv, content, editOf, ts)
	return id
}

// run writes a run on an input and its answer row; superseded as the old
// ChooseAnswer would have left it.
func (s *scratch) run(conv, input uuid.UUID, answer string, superseded bool, continues *uuid.UUID) uuid.UUID {
	id := uuid.New()
	status := "completed"
	s.exec(`INSERT INTO runs (id, conversation_id, user_id, input_message_id, model_id, status, finished_at, superseded, continues, created_at)
		VALUES ($1, $2, $3, $4, 'fake', $5::run_status, now(), $6, $7, $8)`,
		id, conv, s.user, input, status, superseded, continues, s.at())
	s.exec(`INSERT INTO messages (conversation_id, role, content, run_id, created_at, updated_at)
		VALUES ($1, 'assistant', $2, $3, $4, $4)`, conv, answer, id, s.at())
	return id
}

func (s *scratch) contents(query string, conv uuid.UUID) []string {
	s.t.Helper()
	rows, err := s.pool.Query(context.Background(),
		`SELECT m.content FROM (`+query+`) t JOIN messages m ON m.id = t.id ORDER BY m.created_at, m.id`, conv)
	if err != nil {
		s.t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			s.t.Fatal(err)
		}
		out = append(out, c)
	}
	return out
}

func (s *scratch) migrate() {
	s.t.Helper()
	if err := goose.UpToContext(context.Background(), s.sql, ".", branches); err != nil {
		s.t.Fatalf("migrate: %v", err)
	}
}

// All the shapes of data in one database, migrated once: the transcript of each
// conversation is exactly what it was.
func TestBackfillReproducesTheTranscriptShown(t *testing.T) {
	s := newScratch(t)

	// A plain conversation.
	plain := s.conversation()
	p1 := s.input(plain, "p1", nil)
	s.run(plain, p1, "a-p1", false, nil)
	p2 := s.input(plain, "p2", nil)
	s.run(plain, p2, "a-p2", false, nil)

	// The last message edited: the original and its answer are hidden.
	edited := s.conversation()
	e1 := s.input(edited, "e1", nil)
	s.run(edited, e1, "a-e1", false, nil)
	e2 := s.input(edited, "e2", nil)
	s.run(edited, e2, "a-e2", true, nil)
	e2b := s.input(edited, "e2b", &e2)
	s.run(edited, e2b, "a-e2b", false, nil)

	// An edit in the middle, with turns asked after it, and the edit edited again.
	middle := s.conversation()
	m1 := s.input(middle, "m1", nil)
	s.run(middle, m1, "a-m1", false, nil)
	m2 := s.input(middle, "m2", nil)
	s.run(middle, m2, "a-m2", true, nil)
	m2b := s.input(middle, "m2b", &m2)
	s.run(middle, m2b, "a-m2b", true, nil)
	m2c := s.input(middle, "m2c", &m2)
	s.run(middle, m2c, "a-m2c", false, nil)
	m3 := s.input(middle, "m3", nil)
	s.run(middle, m3, "a-m3", false, nil)

	// Regenerated answers, on a middle turn and a switched-back last one.
	regen := s.conversation()
	r1 := s.input(regen, "r1", nil)
	s.run(regen, r1, "a-r1-old", true, nil)
	s.run(regen, r1, "a-r1", false, nil)
	r2 := s.input(regen, "r2", nil)
	s.run(regen, r2, "a-r2", false, nil)
	s.run(regen, r2, "a-r2-new", true, nil) // chosen back to the first

	// An answer that stopped to ask and went on, then was regenerated.
	asked := s.conversation()
	k1 := s.input(asked, "k1", nil)
	head := s.run(asked, k1, "a-k1-ask", true, nil)
	s.run(asked, k1, "a-k1-went-on", true, &head)
	head2 := s.run(asked, k1, "a-k1-ask-again", false, nil)
	s.run(asked, k1, "a-k1-went-on-again", false, &head2)
	k2 := s.input(asked, "k2", nil)
	s.run(asked, k2, "a-k2", false, nil)

	// A question never answered (a conversation from before runs existed).
	legacy := s.conversation()
	l1 := s.input(legacy, "l1", nil)
	s.exec(`INSERT INTO messages (conversation_id, role, content, created_at, updated_at) VALUES ($1, 'assistant', 'a-l1', $2, $2)`, legacy, s.at())
	l2 := s.input(legacy, "l2", nil)
	s.exec(`INSERT INTO messages (conversation_id, role, content, created_at, updated_at) VALUES ($1, 'assistant', 'a-l2', $2, $2)`, legacy, s.at())
	_, _ = l1, l2

	empty := s.conversation()

	want := map[uuid.UUID][]string{
		plain:  {"p1", "a-p1", "p2", "a-p2"},
		edited: {"e1", "a-e1", "e2b", "a-e2b"},
		middle: {"m1", "a-m1", "m2c", "a-m2c", "m3", "a-m3"},
		regen:  {"r1", "a-r1", "r2", "a-r2"},
		asked:  {"k1", "a-k1-ask-again", "a-k1-went-on-again", "k2", "a-k2"},
		legacy: {"l1", "a-l1", "l2", "a-l2"},
		empty:  nil,
	}
	for conv, w := range want {
		if got := s.contents(oldTranscript, conv); !slices.Equal(got, w) && len(w) > 0 {
			t.Fatalf("setup: the old transcript of %v is %q, want %q", conv, got, w)
		}
	}
	s.migrate()
	for conv, w := range want {
		if got := s.contents(newTranscript, conv); !slices.Equal(got, w) && (len(w) > 0 || len(got) > 0) {
			t.Fatalf("conversation %v after the migration: %q, want %q", conv, got, w)
		}
	}

	ctx := context.Background()
	parentOf := func(id uuid.UUID) *uuid.UUID {
		var p *uuid.UUID
		if err := s.pool.QueryRow(ctx, `SELECT parent_input_id FROM messages WHERE id = $1`, id).Scan(&p); err != nil {
			t.Fatal(err)
		}
		return p
	}
	leafOf := func(conv uuid.UUID) *uuid.UUID {
		var p *uuid.UUID
		if err := s.pool.QueryRow(ctx, `SELECT current_input_id FROM conversations WHERE id = $1`, conv).Scan(&p); err != nil {
			t.Fatal(err)
		}
		return p
	}
	is := func(what string, got *uuid.UUID, want uuid.UUID) {
		t.Helper()
		if got == nil || *got != want {
			t.Fatalf("%s: %v, want %v", what, got, want)
		}
	}

	// The tree: edits are siblings of the question they reword; what was asked
	// after hangs from the version that was shown; the leaf is the last shown.
	is("p2's parent", parentOf(p2), p1)
	if parentOf(p1) != nil {
		t.Fatal("the first question has a parent")
	}
	is("plain leaf", leafOf(plain), p2)

	is("e2's parent", parentOf(e2), e1)
	is("e2b's parent", parentOf(e2b), e1)
	is("edited leaf", leafOf(edited), e2b)

	is("m2b's parent", parentOf(m2b), m1)
	is("m2c's parent", parentOf(m2c), m1)
	is("m3's parent", parentOf(m3), m2c)
	is("middle leaf", leafOf(middle), m3)

	is("r2's parent", parentOf(r2), r1)
	is("regenerated leaf", leafOf(regen), r2)
	is("k2's parent", parentOf(k2), k1)
	is("legacy leaf", leafOf(legacy), l2)
	if leafOf(empty) != nil {
		t.Fatal("an empty conversation has a leaf")
	}

	// A version set aside shows an answer when switched to: the newest it had.
	var shown int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM runs WHERE input_message_id = $1 AND NOT superseded`, e2).Scan(&shown); err != nil || shown != 1 {
		t.Fatalf("the replaced question e2 has %d shown answers (%v), want 1", shown, err)
	}
	// m2b was the middle of three versions; it comes back with its answer too.
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM runs WHERE input_message_id = ANY ($1) AND NOT superseded`, []uuid.UUID{m2, m2b}).Scan(&shown); err != nil || shown != 2 {
		t.Fatalf("m2 and m2b have %d shown answers (%v), want one each", shown, err)
	}

	// Switching to a hidden version brings back its own answer.
	for _, version := range []uuid.UUID{e2, m2, m2b} {
		var leaf uuid.UUID
		if err := s.pool.QueryRow(ctx, `UPDATE conversations SET current_input_id = $1 WHERE id = (SELECT conversation_id FROM messages WHERE id = $1) RETURNING current_input_id`, version).Scan(&leaf); err != nil {
			t.Fatal(err)
		}
		var conv uuid.UUID
		_ = s.pool.QueryRow(ctx, `SELECT conversation_id FROM messages WHERE id = $1`, version).Scan(&conv)
		got := s.contents(newTranscript, conv)
		if len(got) < 2 || !strings.HasPrefix(got[len(got)-1], "a-") {
			t.Fatalf("version %v shows %q: no answer", version, got)
		}
	}
}

// Rolling back leaves what was shown: the path, its sibling versions hidden,
// and nothing of the branches that were off it.
func TestRollbackKeepsTheBranchShown(t *testing.T) {
	s := newScratch(t)
	conv := s.conversation()
	c1 := s.input(conv, "c1", nil)
	s.run(conv, c1, "a-c1", false, nil)
	c2 := s.input(conv, "c2", nil)
	s.run(conv, c2, "a-c2", false, nil)
	c3 := s.input(conv, "c3", nil)
	s.run(conv, c3, "a-c3", false, nil)
	s.migrate()

	// Branch at c2 as the new code does: a sibling with the same parent, its
	// own turn after it; the reader is on it.
	ctx := context.Background()
	c2b := uuid.New()
	s.exec(`INSERT INTO messages (id, conversation_id, role, content, edit_of, parent_input_id, created_at, updated_at)
		VALUES ($1, $2, 'user', 'c2b', $3, $4, $5, $5)`, c2b, conv, c2, c1, s.at())
	s.run(conv, c2b, "a-c2b", false, nil)
	c3b := uuid.New()
	s.exec(`INSERT INTO messages (id, conversation_id, role, content, parent_input_id, created_at, updated_at)
		VALUES ($1, $2, 'user', 'c3b', $3, $4, $4)`, c3b, conv, c2b, s.at())
	s.run(conv, c3b, "a-c3b", false, nil)
	s.exec(`UPDATE conversations SET current_input_id = $1 WHERE id = $2`, c3b, conv)

	if err := goose.DownToContext(ctx, s.sql, ".", beforeBranches); err != nil {
		t.Fatalf("rollback: %v", err)
	}
	if got, want := s.contents(oldTranscript, conv), []string{"c1", "a-c1", "c2b", "a-c2b", "c3b", "a-c3b"}; !slices.Equal(got, want) {
		t.Fatalf("after the rollback: %q, want %q", got, want)
	}
	// c2 stays as a hidden version of c2b's question; c3 (what followed it) is gone.
	var count int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM messages WHERE id = ANY ($1)`, []uuid.UUID{c2, c3}).Scan(&count); err != nil || count != 1 {
		t.Fatalf("%d of c2, c3 remain (%v), want c2 only", count, err)
	}
	var hidden bool
	if err := s.pool.QueryRow(ctx, `SELECT bool_and(superseded) FROM runs WHERE input_message_id = $1`, c2).Scan(&hidden); err != nil || !hidden {
		t.Fatalf("c2's answers are shown after the rollback (%v)", err)
	}

	// And up again: the same.
	s.migrate()
	if got, want := s.contents(newTranscript, conv), []string{"c1", "a-c1", "c2b", "a-c2b", "c3b", "a-c3b"}; !slices.Equal(got, want) {
		t.Fatalf("after migrating again: %q, want %q", got, want)
	}
}
