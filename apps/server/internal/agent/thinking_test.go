package agent

import (
	"testing"
	"time"

	"github.com/qunqin/openchat/apps/server/internal/llm"
)

// A partial stream with a hand-moved clock and no throttle: emit and stampAll
// are what is under test, not the commits.
func timedStream(clock *time.Time) *partialStream {
	return &partialStream{
		ex:       &execution{r: &Runner{now: func() time.Time { return *clock }}},
		thinking: map[int]*span{},
	}
}

func durationOf(t *testing.T, part llm.Part) int64 {
	t.Helper()
	p, ok := part.(llm.ReasoningPart)
	if !ok || p.DurationMS == nil {
		t.Fatalf("part %#v carries no duration", part)
	}
	return *p.DurationMS
}

func TestReasoningIsTimedFromFirstTokenToItsEnd(t *testing.T) {
	clock := time.Unix(1000, 0)
	s := timedStream(&clock)
	emit := func(ev llm.Event) {
		if err := s.emit(ev); err != nil {
			t.Fatal(err)
		}
	}

	emit(llm.Event{Type: llm.EventPartStart, Index: 0, PartKind: llm.PartReasoning})
	clock = clock.Add(2 * time.Second)
	emit(llm.Event{Type: llm.EventPartDelta, Index: 0, PartKind: llm.PartReasoning, Delta: "thinking"})
	clock = clock.Add(3 * time.Second)
	emit(llm.Event{Type: llm.EventPartEnd, Index: 0, PartKind: llm.PartReasoning, Part: llm.ReasoningPart{Text: "thinking"}})

	// The end event itself carries the time, for a page that is watching.
	ended, ok := s.pending[len(s.pending)-1].(PartEnded)
	if !ok || durationOf(t, ended.Part) != 5000 {
		t.Fatalf("PartEnded = %#v, want the 5s duration on its part", s.pending[len(s.pending)-1])
	}

	// The answer that follows is not thinking time.
	clock = clock.Add(10 * time.Second)
	emit(llm.Event{Type: llm.EventPartDelta, Index: 1, PartKind: llm.PartText, Delta: "answer"})

	parts := s.stampAll([]llm.Part{llm.ReasoningPart{Text: "thinking"}, llm.TextPart{Text: "answer"}})
	if got := durationOf(t, parts[0]); got != 5000 {
		t.Errorf("duration = %dms, want 5000", got)
	}
	if _, ok := parts[1].(llm.TextPart); !ok {
		t.Errorf("text part changed: %#v", parts[1])
	}
}

func TestReasoningWithoutAnEndStopsWhenTheNextPartStarts(t *testing.T) {
	// Some providers never close the thinking block; the answer starting is
	// the end of it.
	clock := time.Unix(1000, 0)
	s := timedStream(&clock)
	_ = s.emit(llm.Event{Type: llm.EventPartDelta, Index: 0, PartKind: llm.PartReasoning, Delta: "a"})
	clock = clock.Add(1500 * time.Millisecond)
	_ = s.emit(llm.Event{Type: llm.EventPartDelta, Index: 1, PartKind: llm.PartText, Delta: "b"})
	clock = clock.Add(time.Minute)

	parts := s.stampAll([]llm.Part{llm.ReasoningPart{Text: "a"}, llm.TextPart{Text: "b"}})
	if got := durationOf(t, parts[0]); got != 1500 {
		t.Errorf("duration = %dms, want 1500", got)
	}
}

func TestUntimedReasoningHasNoDuration(t *testing.T) {
	clock := time.Unix(1000, 0)
	s := timedStream(&clock)
	parts := s.stampAll([]llm.Part{llm.ReasoningPart{Text: "replayed, never streamed"}})
	if p := parts[0].(llm.ReasoningPart); p.DurationMS != nil {
		t.Errorf("duration = %d, want none", *p.DurationMS)
	}
}

func TestReasoningDurationSurvivesTheCodec(t *testing.T) {
	ms := int64(4200)
	data, err := llm.MarshalPart(llm.ReasoningPart{Text: "x", DurationMS: &ms})
	if err != nil {
		t.Fatal(err)
	}
	part, err := llm.UnmarshalPart(data)
	if err != nil {
		t.Fatal(err)
	}
	if got := durationOf(t, part); got != 4200 {
		t.Errorf("duration after round trip = %d, want 4200", got)
	}
}
