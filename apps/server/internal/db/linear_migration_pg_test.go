package db_test

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/pressly/goose/v3"

	"github.com/qunqin24/openchat/apps/server/internal/db/sqlc"
)

// The linear-conversations migration (20261020000001) against data in the
// shape the branches migration left it: it keeps in every conversation exactly
// what the reader saw (conversation_transcript) and deletes the rest, with the
// files of what it deletes let go of for the cleanup loop. Needs a server that
// lets the test make a database of its own (see migration_pg_test.go).

const linear = 20261020000001

// child writes a user message into the tree: under parent, a version of the
// question `editOf` names.
func (s *scratch) child(conv uuid.UUID, content string, parent, editOf *uuid.UUID) uuid.UUID {
	id := uuid.New()
	ts := s.at()
	s.exec(`INSERT INTO messages (id, conversation_id, role, content, parent_input_id, edit_of, shown_at, created_at, updated_at)
		VALUES ($1, $2, 'user', $3, $4, $5, $6, $6, $6)`, id, conv, content, parent, editOf, ts)
	return id
}

func (s *scratch) leaf(conv, input uuid.UUID) {
	s.exec(`UPDATE conversations SET current_input_id = $1 WHERE id = $2`, input, conv)
}

func (s *scratch) attach(message uuid.UUID, key string) uuid.UUID {
	id := uuid.New()
	s.exec(`INSERT INTO attachments (id, message_id, user_id, storage_key, filename, content_type, size_bytes)
		VALUES ($1, $2, $3, $4, $4, 'image/png', 4)`, id, message, s.user, key)
	return id
}

func (s *scratch) migrateTo(version int64) {
	s.t.Helper()
	if err := goose.UpToContext(context.Background(), s.sql, ".", version); err != nil {
		s.t.Fatalf("migrate to %d: %v", version, err)
	}
}

// everything is the conversation's contents, oldest first: all its rows.
func (s *scratch) everything(conv uuid.UUID) []string {
	s.t.Helper()
	rows, err := s.pool.Query(context.Background(),
		`SELECT content FROM messages WHERE conversation_id = $1 ORDER BY created_at, id`, conv)
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

func (s *scratch) count(query string, args ...any) int {
	s.t.Helper()
	var n int
	if err := s.pool.QueryRow(context.Background(), query, args...).Scan(&n); err != nil {
		s.t.Fatalf("%s: %v", query, err)
	}
	return n
}

func TestLinearMigrationPrunesToTheCurrentPath(t *testing.T) {
	s := newScratch(t)
	s.migrateTo(branches)

	// A linear conversation, as plain as they come: nothing may change.
	plain := s.conversation()
	p1 := s.child(plain, "p1", nil, nil)
	plainRuns := []uuid.UUID{s.run(plain, p1, "a-p1", false, nil)}
	p2 := s.child(plain, "p2", &p1, nil)
	plainRuns = append(plainRuns, s.run(plain, p2, "a-p2", false, nil))
	s.leaf(plain, p2)
	plainFile := s.attach(p1, "uploads/plain.png")

	// A branched one: q2 was edited (q2b, its sibling, is shown) after a third
	// turn had been asked under q2; under q2b the reader went on and
	// regenerated an answer, and q3b has a file. Files hang on q2 (the first
	// version of the question, as edits leave them), on q3 (off the path) and
	// on q1 (on it).
	branched := s.conversation()
	q1 := s.child(branched, "q1", nil, nil)
	s.run(branched, q1, "a-q1", false, nil)
	q2 := s.child(branched, "q2", &q1, nil)
	s.run(branched, q2, "a-q2", true, nil)
	q3 := s.child(branched, "q3", &q2, nil)
	s.run(branched, q3, "a-q3", false, nil)
	q2b := s.child(branched, "q2b", &q1, &q2)
	s.run(branched, q2b, "a-q2b-old", true, nil)
	s.run(branched, q2b, "a-q2b", false, nil)
	q3b := s.child(branched, "q3b", &q2b, nil)
	s.run(branched, q3b, "a-q3b", false, nil)
	s.leaf(branched, q3b)
	fileQ1 := s.attach(q1, "uploads/q1.png")
	fileQ2 := s.attach(q2, "uploads/q2.png")
	fileQ3 := s.attach(q3, "uploads/q3.png")
	fileQ3b := s.attach(q3b, "uploads/q3b.png")

	// An answer that asked and went on, then was regenerated.
	asked := s.conversation()
	k1 := s.child(asked, "k1", nil, nil)
	head := s.run(asked, k1, "a-k1-ask", true, nil)
	old := s.run(asked, k1, "a-k1-went-on", true, &head)
	head2 := s.run(asked, k1, "a-k1-ask-again", false, nil)
	kept := s.run(asked, k1, "a-k1-went-on-again", false, &head2)
	s.leaf(asked, k1)

	// Rows from before runs existed, and a conversation with nothing in it.
	legacy := s.conversation()
	l1 := s.child(legacy, "l1", nil, nil)
	s.exec(`INSERT INTO messages (conversation_id, role, content, created_at, updated_at) VALUES ($1, 'assistant', 'a-l1', $2, $2)`, legacy, s.at())
	l2 := s.child(legacy, "l2", &l1, nil)
	s.exec(`INSERT INTO messages (conversation_id, role, content, created_at, updated_at) VALUES ($1, 'assistant', 'a-l2', $2, $2)`, legacy, s.at())
	s.leaf(legacy, l2)
	empty := s.conversation()

	// What the reader sees today, per conversation.
	convs := []uuid.UUID{plain, branched, asked, legacy, empty}
	shown := map[uuid.UUID][]string{}
	for _, c := range convs {
		shown[c] = s.contents(newTranscript, c)
	}
	if want := []string{"q1", "a-q1", "q2b", "a-q2b", "q3b", "a-q3b"}; !slices.Equal(shown[branched], want) {
		t.Fatalf("setup: the branched conversation shows %q, want %q", shown[branched], want)
	}
	if want := []string{"k1", "a-k1-ask-again", "a-k1-went-on-again"}; !slices.Equal(shown[asked], want) {
		t.Fatalf("setup: the asked conversation shows %q, want %q", shown[asked], want)
	}
	if len(s.everything(branched)) <= len(shown[branched]) {
		t.Fatal("setup: nothing is off the path")
	}

	s.migrateTo(linear)

	// Every conversation holds exactly what it showed.
	for _, c := range convs {
		if got := s.everything(c); !slices.Equal(got, shown[c]) && (len(got) > 0 || len(shown[c]) > 0) {
			t.Errorf("conversation %v holds %q after the migration, want %q", c, got, shown[c])
		}
	}

	// The linear conversation is untouched: its runs and file too.
	if n := s.count(`SELECT count(*) FROM runs WHERE id = ANY ($1)`, plainRuns); n != len(plainRuns) {
		t.Errorf("%d of the linear conversation's %d runs remain", n, len(plainRuns))
	}
	if n := s.count(`SELECT count(*) FROM attachments WHERE id = $1 AND message_id = $2`, plainFile, p1); n != 1 {
		t.Error("the linear conversation's file moved or went")
	}

	// Runs: the regenerated answers and the deleted questions' go; the shown
	// ones stay, a continuation with the run it continues.
	if n := s.count(`SELECT count(*) FROM runs WHERE conversation_id = $1`, branched); n != 3 {
		t.Errorf("the branched conversation has %d runs, want 3 (q1, q2b and q3b)", n)
	}
	if n := s.count(`SELECT count(*) FROM runs WHERE id = ANY ($1)`, []uuid.UUID{head, old}); n != 0 {
		t.Errorf("%d of the regenerated answer's runs remain", n)
	}
	if n := s.count(`SELECT count(*) FROM runs WHERE id = ANY ($1) AND input_message_id = $2`, []uuid.UUID{head2, kept}, k1); n != 2 {
		t.Errorf("%d of the kept answer's two runs remain", n)
	}
	if n := s.count(`SELECT count(*) FROM messages WHERE id = ANY ($1)`, []uuid.UUID{q2, q3}); n != 0 {
		t.Errorf("%d of the questions off the path remain", n)
	}

	// Files: the first version's go to the version shown; the ones on the
	// path stay; the ones of what was deleted are let go of - claimed by no
	// message, dated so that the next cleanup pass finds them.
	owner := func(id uuid.UUID) *uuid.UUID {
		var m *uuid.UUID
		if err := s.pool.QueryRow(context.Background(), `SELECT message_id FROM attachments WHERE id = $1`, id).Scan(&m); err != nil {
			t.Fatal(err)
		}
		return m
	}
	is := func(what string, got *uuid.UUID, want uuid.UUID) {
		t.Helper()
		if got == nil || *got != want {
			t.Errorf("%s belongs to %v, want %v", what, got, want)
		}
	}
	is("q1's file", owner(fileQ1), q1)
	is("q2's file (the first version of the question shown as q2b)", owner(fileQ2), q2b)
	is("q3b's file", owner(fileQ3b), q3b)
	if owner(fileQ3) != nil {
		t.Error("the file of a question deleted off the path is still claimed")
	}

	// The cleanup loop's own statement finds it, and only it, whatever the
	// unsent-upload window is.
	keys, err := sqlc.New(s.pool).DeleteStaleUploads(context.Background(), sqlc.DeleteStaleUploadsParams{
		Before: pgtype.Timestamptz{Time: time.Now().Add(-24 * time.Hour), Valid: true}, RowLimit: 100,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(keys, []string{"uploads/q3.png"}) {
		t.Errorf("the cleanup would delete %q, want only the file of the deleted question", keys)
	}

	// The tree is gone from the schema.
	for _, c := range []struct{ table, column string }{
		{"messages", "parent_input_id"}, {"messages", "shown_at"}, {"messages", "edit_of"},
		{"conversations", "current_input_id"}, {"runs", "superseded"},
	} {
		if n := s.count(`SELECT count(*) FROM information_schema.columns WHERE table_name = $1 AND column_name = $2`, c.table, c.column); n != 0 {
			t.Errorf("%s.%s still exists", c.table, c.column)
		}
	}
	if n := s.count(`SELECT count(*) FROM pg_proc WHERE proname IN ('conversation_path', 'conversation_transcript')`); n != 0 {
		t.Error("the tree's functions still exist")
	}
}

// Rolling back gives each conversation a line again: a chain of questions with
// the last one as the leaf, and the same transcript.
func TestLinearMigrationRollback(t *testing.T) {
	s := newScratch(t)
	s.migrateTo(branches)
	conv := s.conversation()
	a := s.child(conv, "a", nil, nil)
	s.run(conv, a, "a-a", false, nil)
	b := s.child(conv, "b", &a, nil)
	s.run(conv, b, "a-b", false, nil)
	s.leaf(conv, b)
	s.migrateTo(linear)

	if err := goose.DownToContext(context.Background(), s.sql, ".", branches); err != nil {
		t.Fatalf("rollback: %v", err)
	}
	if got, want := s.contents(newTranscript, conv), []string{"a", "a-a", "b", "a-b"}; !slices.Equal(got, want) {
		t.Fatalf("after the rollback the transcript is %q, want %q", got, want)
	}
	var parent *uuid.UUID
	if err := s.pool.QueryRow(context.Background(), `SELECT parent_input_id FROM messages WHERE id = $1`, b).Scan(&parent); err != nil || parent == nil || *parent != a {
		t.Fatalf("b's parent is %v (%v), want a", parent, err)
	}

	// And up again.
	s.migrateTo(linear)
	if got, want := s.everything(conv), []string{"a", "a-a", "b", "a-b"}; !slices.Equal(got, want) {
		t.Fatalf("after migrating again: %q, want %q", got, want)
	}
}
