// Package llmtest holds the rules every ModelClient has to obey, written down
// once so that a second adapter cannot quietly invent its own event order.
//
// Phase A's acceptance bar is about parsing and terminal states, so the
// assertions here are the bar: blocks are opened before they are written to and
// closed after, exactly one response ends, and nothing at this layer ever
// claims to end a Run.
package llmtest

import (
	"strings"
	"testing"

	"github.com/qunqin/openchat/apps/server/internal/llm"
)

// Recorder captures the events a provider emits. It is not safe for concurrent
// use: a ModelClient emits from one goroutine, and one that does not is already
// breaking its contract.
type Recorder struct {
	Events []llm.Event

	// EmitErr, when set, is returned from Emit at the call named by FailAt.
	// This is how a test stands in for the run layer failing a required write.
	EmitErr error
	FailAt  int

	calls int
}

func (r *Recorder) Emit(ev llm.Event) error {
	r.calls++
	if r.EmitErr != nil && r.calls > r.FailAt {
		return r.EmitErr
	}
	r.Events = append(r.Events, ev)
	return nil
}

// Types returns the event types in order, which is what most failure messages
// actually want to print.
func (r *Recorder) Types() []llm.EventType {
	out := make([]llm.EventType, 0, len(r.Events))
	for _, ev := range r.Events {
		out = append(out, ev.Type)
	}
	return out
}

// OfType returns every event of one type.
func (r *Recorder) OfType(t llm.EventType) []llm.Event {
	var out []llm.Event
	for _, ev := range r.Events {
		if ev.Type == t {
			out = append(out, ev)
		}
	}
	return out
}

// Deltas concatenates the delta text of one kind of block, which is what a
// browser would have rendered.
func (r *Recorder) Deltas(kind llm.PartKind) string {
	var b strings.Builder
	for _, ev := range r.Events {
		if ev.Type == llm.EventPartDelta && ev.PartKind == kind {
			b.WriteString(ev.Delta)
		}
	}
	return b.String()
}

// CheckProviderInvariants fails the test if the recorded stream breaks the
// ModelClient contract.
func (r *Recorder) CheckProviderInvariants(t *testing.T) {
	t.Helper()

	started := map[int]llm.PartKind{}
	ended := map[int]bool{}
	responseEnds := 0

	for i, ev := range r.Events {
		switch ev.Type {
		case llm.EventPartStart:
			if _, dup := started[ev.Index]; dup {
				t.Errorf("event %d: block %d started twice", i, ev.Index)
			}
			started[ev.Index] = ev.PartKind
		case llm.EventPartDelta:
			kind, ok := started[ev.Index]
			if !ok {
				t.Errorf("event %d: delta for block %d that never started", i, ev.Index)
			} else if kind != ev.PartKind {
				t.Errorf("event %d: block %d started as %s, delta says %s", i, ev.Index, kind, ev.PartKind)
			}
			if ended[ev.Index] {
				t.Errorf("event %d: delta for block %d after it ended", i, ev.Index)
			}
		case llm.EventPartEnd:
			if _, ok := started[ev.Index]; !ok {
				t.Errorf("event %d: block %d ended without starting", i, ev.Index)
			}
			if ended[ev.Index] {
				t.Errorf("event %d: block %d ended twice", i, ev.Index)
			}
			if ev.Part == nil {
				t.Errorf("event %d: block %d ended without a part", i, ev.Index)
			}
			ended[ev.Index] = true
		case llm.EventResponseEnd:
			responseEnds++
			if ev.Response == nil {
				t.Errorf("event %d: response_end without a response", i)
			}
		case llm.EventToolCallsRequested, llm.EventRunEnd:
			t.Errorf("event %d: %s belongs to the run layer, a provider must not emit it", i, ev.Type)
		}
	}

	if responseEnds > 1 {
		t.Errorf("response_end emitted %d times, want at most one: %v", responseEnds, r.Types())
	}
	if responseEnds == 1 {
		// A turn that ended has no open blocks left; anything still open would
		// show in the UI as text that never stops arriving.
		for index := range started {
			if !ended[index] {
				t.Errorf("block %d was still open when the response ended", index)
			}
		}
	}
}
