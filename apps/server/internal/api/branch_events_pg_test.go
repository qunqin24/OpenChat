package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/qunqin/openchat/apps/server/internal/agent"
)

// Another tab showing the conversation is told when the transcript it holds
// stopped being the one shown: on a switch of version and on an edit, not when
// a switch changes nothing.
func TestBranchChangeIsAnnounced(t *testing.T) {
	b := newBranching(t)
	q1, _ := b.send("q1")
	b.send("q2")

	sub := b.server.runner.Hub().Subscribe(b.conv.ID)
	defer sub.Close()
	drain := func() []agent.Batch {
		batches, resync := sub.Drain()
		if resync {
			t.Fatal("the subscriber fell behind")
		}
		return batches
	}
	branchEvents := func(batches []agent.Batch) (alone, withRun int) {
		for _, batch := range batches {
			for _, event := range batch.Events {
				if _, ok := event.(agent.BranchChanged); !ok {
					continue
				}
				started := false
				for _, other := range batch.Events {
					_, isStart := other.(agent.RunStarted)
					started = started || isStart
				}
				if started {
					withRun++
				} else {
					if batch.RunID != uuid.Nil || batch.Version != 0 {
						t.Errorf("a switch is announced under run %s v%d, want none", batch.RunID, batch.Version)
					}
					alone++
				}
			}
		}
		return alone, withRun
	}

	// An edit moves the leaf onto the new wording and starts a run: one batch
	// says both.
	edited, _ := b.edit(q1.Message.Id, "q1b")
	if alone, withRun := branchEvents(drain()); alone != 0 || withRun != 1 {
		t.Fatalf("after an edit: %d alone, %d with the run; want 0 and 1", alone, withRun)
	}

	// Switching back is announced on its own.
	b.switchTo(q1.Message.Id)
	batches := drain()
	if alone, withRun := branchEvents(batches); alone != 1 || withRun != 0 || len(batches) != 1 {
		t.Fatalf("after a switch: %d alone, %d with a run, %d batches; want 1, 0 and 1", alone, withRun, len(batches))
	}

	// Showing what is already shown moves nothing, so says nothing.
	b.switchTo(q1.Message.Id)
	if batches := drain(); len(batches) != 0 {
		t.Fatalf("a switch to the version already shown was announced: %+v", batches)
	}

	// A refused switch (a version that is not on the path) says nothing either.
	rec := httptest.NewRecorder()
	b.server.SwitchVersion(rec, b.request(http.MethodPost, ""), b.conv.ID, uuid.New(), SwitchVersionParams{})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("an unknown message: %d", rec.Code)
	}
	if batches := drain(); len(batches) != 0 {
		t.Fatalf("a refused switch was announced: %+v", batches)
	}
	_ = edited

	// On the wire it is the contract's branch_changed.
	mapped := toBatchEvent(agent.Batch{Events: []agent.Event{agent.BranchChanged{}}})
	raw, err := json.Marshal(mapped)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"type":"branch_changed"`) {
		t.Fatalf("the event on the wire: %s", raw)
	}
}
