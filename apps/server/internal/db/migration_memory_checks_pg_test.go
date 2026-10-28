package db_test

import (
	"context"
	"slices"
	"testing"

	"github.com/google/uuid"
	"github.com/pressly/goose/v3"

	"github.com/qunqin24/openchat/apps/server/internal/memory"
)

// The migration after the moderated-memories one (20261025000006) against what
// that one left: it marked every memory not written by a person as checked,
// including a memory the model wrote that a person then rewrote (before
// 20261025000005 an edit did not change `source`). Those marks are unearned and
// must go, so that the words in them are moderated before they reach a model.

const (
	beforeMemoryChecks   = 20261025000004 // what a database had before the backfill
	moderatedMemories    = 20261025000005 // the backfill
	unearnedMemoryChecks = 20261025000006
)

func TestUnearnedMemoryChecksAreCleared(t *testing.T) {
	ctx := context.Background()
	s := newScratchAt(t, beforeMemoryChecks)

	memoryRow := func(content, source string) uuid.UUID {
		id := uuid.New()
		s.exec(`INSERT INTO user_memories (id, user_id, content, source) VALUES ($1, $2, $3, $4::memory_source)`,
			id, s.user, content, source)
		return id
	}
	// The model wrote it and it is the model's still; the model wrote it and a
	// person has since typed over it (source stays 'model'); the extractor's; a
	// person's own.
	untouched := memoryRow("The model's own note", "model")
	typedOver := memoryRow("A person typed this over the model's note", "model")
	s.exec(`UPDATE user_memories SET updated_at = now() + interval '1 minute' WHERE id = $1`, typedOver)
	auto := memoryRow("Learned on its own", "auto")
	mine := memoryRow("Written by the person", "user")

	s.migrateTo(moderatedMemories)
	marked := func(id uuid.UUID) bool {
		var n int
		if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM user_memories WHERE id = $1 AND checked_hash IS NOT NULL`, id).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n == 1
	}
	// What shipped: the person's words are vouched for.
	if !marked(typedOver) || !marked(untouched) || !marked(auto) || marked(mine) {
		t.Fatalf("after the backfill: typed over %v, untouched %v, auto %v, mine %v", marked(typedOver), marked(untouched), marked(auto), marked(mine))
	}
	// And what a person's own memory earns by being checked, and a refusal.
	s.exec(`UPDATE user_memories SET checked_hash = encode(sha256(convert_to(content, 'UTF8')), 'hex') WHERE id = $1`, mine)
	refused := memoryRow("Refused at a snapshot", "auto")
	s.exec(`UPDATE user_memories SET blocked_hash = 'h' WHERE id = $1`, refused)

	s.migrateTo(unearnedMemoryChecks)
	if marked(typedOver) || marked(untouched) || marked(auto) || marked(refused) {
		t.Errorf("a mark of the backfill is left: typed over %v, untouched %v, auto %v", marked(typedOver), marked(untouched), marked(auto))
	}
	if !marked(mine) {
		t.Error("the mark of a person's own memory was cleared")
	}
	var blocked *string
	if err := s.pool.QueryRow(ctx, `SELECT blocked_hash FROM user_memories WHERE id = $1`, refused).Scan(&blocked); err != nil || blocked == nil || *blocked != "h" {
		t.Errorf("a refusal was lost: %v %v", blocked, err)
	}

	// Taking a snapshot now has the words checked, the person's rewrite among
	// them.
	var asked []string
	vet := func(_ context.Context, pieces []memory.Piece) ([]memory.Verdict, error) {
		out := make([]memory.Verdict, len(pieces))
		for i, piece := range pieces {
			asked = append(asked, piece.Text)
			out[i] = memory.Verdict{Checked: true}
		}
		return out, nil
	}
	notes := memory.NewStore(s.pool)
	if _, err := notes.EnsureSnapshot(ctx, s.user, s.conversation(), vet); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(asked, "A person typed this over the model's note") {
		t.Errorf("the rewritten memory was not moderated: asked %q", asked)
	}
	if slices.Contains(asked, "Written by the person") {
		t.Error("a memory that kept its mark was asked about again")
	}

	// A memory the model writes from now on is recorded as checked as it is
	// written, so a later snapshot does not ask about it. (What was asked
	// about above was recorded as checked, too.)
	asked = nil
	if _, err := notes.Add(ctx, s.user, "Written by the model afterwards", memory.By{Source: memory.SourceModel}); err != nil {
		t.Fatal(err)
	}
	if _, err := notes.EnsureSnapshot(ctx, s.user, s.conversation(), vet); err != nil {
		t.Fatal(err)
	}
	if len(asked) != 0 {
		t.Errorf("a snapshot asked about %q", asked)
	}

	// And back: nothing to restore, nothing broken.
	if err := goose.DownToContext(ctx, s.sql, ".", moderatedMemories); err != nil {
		t.Fatalf("roll back: %v", err)
	}
	if err := goose.UpToContext(ctx, s.sql, ".", unearnedMemoryChecks); err != nil {
		t.Fatalf("migrate again: %v", err)
	}
}
