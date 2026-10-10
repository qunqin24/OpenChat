package llm

import (
	"context"
	"strings"
	"testing"
)

// The block DeepSeek V4.1 wrote into an answer, as it arrived.
const leakedV41 = "\n\n<｜DSML｜ calls>\n<｜DSML｜ invoke name=\"web_search\">\n<｜DSML｜ parameter name=\"query\" string=\"true\">江门日报</｜DSML｜ parameter>\n</｜DSML｜ invoke>\n</｜DSML｜ calls>\n"

func TestStripToolMarkup(t *testing.T) {
	for _, c := range []struct {
		name, in, want string
		found          bool
	}{
		{"plain text", "No markup here <b>at all</b>.", "No markup here <b>at all</b>.", false},
		{"v4.1, the whole answer", leakedV41, "", true},
		{"after words", "Let me look." + leakedV41, "Let me look.", true},
		{"v3.2", "a<｜DSML｜function_calls><｜DSML｜invoke name=\"x\"></｜DSML｜invoke></｜DSML｜function_calls>b", "ab", true},
		{"v4, ascii bars", "a<|DSML|tool_calls>…</|DSML|tool_calls>", "a", true},
		{"cut off", "Searching.<｜DSML｜ calls>\n<｜DSML｜ invoke name=", "Searching.", true},
	} {
		got, found := StripToolMarkup(c.in)
		if got != c.want || found != c.found {
			t.Errorf("%s: got %q %v, want %q %v", c.name, got, found, c.want, c.found)
		}
	}
}

// scripted streams a text part in the given pieces.
type scripted struct{ pieces []string }

func (s scripted) Stream(_ context.Context, _ Request, emit func(Event) error) (ModelResponse, error) {
	full := strings.Join(s.pieces, "")
	_ = emit(Event{Type: EventPartStart, Index: 0, PartKind: PartText})
	for _, piece := range s.pieces {
		if err := emit(Event{Type: EventPartDelta, Index: 0, PartKind: PartText, Delta: piece}); err != nil {
			return ModelResponse{}, err
		}
	}
	_ = emit(PartEndEvent(0, TextPart{Text: full}))
	resp := ModelResponse{Message: Message{Role: RoleAssistant, Parts: []Part{TextPart{Text: full}}}, FinishReason: FinishStop}
	_ = emit(ResponseEndEvent(resp))
	return resp, nil
}

func TestHideToolMarkupWhileStreaming(t *testing.T) {
	stream := func(pieces ...string) (shown string, end string, resp ModelResponse) {
		t.Helper()
		resp, err := HideToolMarkup(scripted{pieces}).Stream(context.Background(), Request{}, func(ev Event) error {
			switch ev.Type {
			case EventPartDelta:
				shown += ev.Delta
			case EventPartEnd:
				end = ev.Part.(TextPart).Text
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		return shown, end, resp
	}

	// The tag split across deltas, even inside the full-width bar's bytes.
	bar := "｜"
	shown, end, resp := stream("I'll search.\n\n<", bar[:2], bar[2:]+"DSML｜ ca", "lls>\n<｜DSML｜ invoke name=\"x\">", "</｜DSML｜ invoke></｜DSML｜ calls>")
	if strings.Contains(shown, "DSML") || strings.Contains(shown, "<") {
		t.Errorf("markup reached the reader: %q", shown)
	}
	if end != "I'll search." || !resp.LeakedToolCalls || len(resp.Message.Parts) != 1 {
		t.Errorf("finished: %q %+v", end, resp)
	}
	if !strings.HasPrefix(end, strings.TrimSpace(shown)) {
		t.Errorf("streamed %q does not lead the finished %q", shown, end)
	}

	// A `<` that turns out to be text is held only until it is plainly text,
	// and all of it reaches the reader.
	shown, end, resp = stream("a <", "b> c <", "DS")
	if shown != "a <b> c <DS" || end != "a <b> c <DS" || resp.LeakedToolCalls {
		t.Errorf("ordinary text: shown %q, end %q, leaked %v", shown, end, resp.LeakedToolCalls)
	}

	// Nothing left but markup: no text part at all.
	_, _, resp = stream(leakedV41)
	if len(resp.Message.Parts) != 0 || !resp.LeakedToolCalls {
		t.Errorf("markup-only answer: %+v", resp)
	}
}
