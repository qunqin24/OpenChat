package api

import (
	"testing"

	"github.com/qunqin24/openchat/apps/server/internal/agent"
)

// A snapshot holds every commit up to its version and the publications after
// it that its live state was taken after; anything later reaches the client.
func TestInSnapshot(t *testing.T) {
	step := 2
	snapshot := SnapshotEvent{Run: &Run{Version: 5}, Step: &step}
	for _, c := range []struct {
		version int64
		step    int
		in      bool
	}{
		{4, 0, true},
		{4, 7, true},
		{5, 0, true},
		{5, 2, true},
		{5, 3, false},
		{6, 0, false},
	} {
		if got := inSnapshot(agent.Batch{Version: c.version, Step: c.step}, snapshot); got != c.in {
			t.Errorf("(%d, %d): in snapshot %v, want %v", c.version, c.step, got, c.in)
		}
	}
	if inSnapshot(agent.Batch{Version: 5, Step: 1}, SnapshotEvent{Run: &Run{Version: 5}}) {
		t.Error("a snapshot without a step holds a publication after its commit")
	}
}
