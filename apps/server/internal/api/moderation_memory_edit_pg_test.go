package api

import (
	"context"
	"net/http"
	"testing"

	"github.com/qunqin24/openchat/apps/server/internal/memory"
)

// A person rewriting a memory the model wrote makes it the person's words: its
// `source` stays the model's, so it is the hash - of the text that was looked at
// - that says whether the text is checked, never the source. An edit made
// without a check leaves no mark, and the next snapshot moderates it; an edit
// that was checked leaves the mark of its own text.
func TestUserEditOfModelMemoryIsCheckedAgain(t *testing.T) {
	ctx := context.Background()
	h, f := newModerationMemoryHarness(t, `,"strikes":0`)
	change, err := memory.NewStore(h.pool).Add(ctx, h.user.ID, "Model note", memory.By{Source: memory.SourceModel})
	if err != nil {
		t.Fatal(err)
	}
	id := change.Memory.ID
	if hashes := h.memoryHashes()["Model note"]; hashes[0] == nil || *hashes[0] != memory.Hash("Model note") {
		t.Fatalf("a memory the model wrote is not recorded as checked: %v", hashes)
	}

	// Edited while moderation is off: nobody looked at the person's words.
	if rec := h.patchModeration(`{"mode":"off"}`); rec.Code != http.StatusOK {
		t.Fatal(rec.Body)
	}
	if rec := h.rewriteMemory(id, "Rewritten, BLOCKME"); rec.Code != http.StatusOK {
		t.Fatalf("rewrite: %d %s", rec.Code, rec.Body)
	}
	if hashes := h.memoryHashes()["Rewritten, BLOCKME"]; hashes[0] != nil {
		t.Errorf("a person's rewrite of the model's memory kept the model's mark: %v", hashes)
	}
	if h.count(`SELECT count(*) FROM user_memories WHERE id = $1 AND source = 'model'`, id) != 1 {
		t.Fatal("the rewrite changed the memory's source; the test means to show it does not matter")
	}

	// Moderation back on: the conversation's snapshot looks at it, and leaves it out.
	if rec := h.patchModeration(`{"mode":"openai"}`); rec.Code != http.StatusOK {
		t.Fatal(rec.Body)
	}
	calls := f.calls()
	conv := h.newConv()
	if rec := h.sendIn(conv.ID, "hello"); rec.Code != http.StatusAccepted {
		t.Fatalf("send: %d %s", rec.Code, rec.Body)
	}
	if f.calls() <= calls+1 {
		t.Errorf("the snapshot did not ask about the rewritten memory: %d calls", f.calls()-calls)
	}
	if snap, _ := h.snapshotOf(conv.ID); len(snap.Memories) != 0 {
		t.Errorf("a rewrite that fails the check is in the snapshot: %+v", snap)
	}
	if h.eventsOf("snapshot") != 1 {
		t.Errorf("%d snapshot events", h.eventsOf("snapshot"))
	}

	// Edited with moderation on: checked as it is saved, and marked for its own text.
	if rec := h.rewriteMemory(id, "Rewritten again, and fine"); rec.Code != http.StatusOK {
		t.Fatalf("rewrite: %d %s", rec.Code, rec.Body)
	}
	if hashes := h.memoryHashes()["Rewritten again, and fine"]; hashes[0] == nil || *hashes[0] != memory.Hash("Rewritten again, and fine") || hashes[1] != nil {
		t.Errorf("hashes after a checked rewrite: %v", hashes)
	}
	// A rewrite the check refuses changes nothing.
	refused(t, h.rewriteMemory(id, "Now BLOCKME"))
	if _, there := h.memoryHashes()["Rewritten again, and fine"]; !there {
		t.Error("a refused rewrite changed the memory")
	}
}
