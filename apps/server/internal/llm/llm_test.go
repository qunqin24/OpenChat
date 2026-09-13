package llm_test

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/qunqin/openchat/apps/server/internal/llm"
)

func TestMessageTextLeavesReasoningOut(t *testing.T) {
	// Reasoning is not the answer. A Text() that included it would put the
	// model's private deliberation in every place the answer is shown, and in
	// every context replayed to a provider that rejects it.
	message := llm.Message{Role: llm.RoleAssistant, Parts: []llm.Part{
		llm.ReasoningPart{Text: "the user probably means photography"},
		llm.TextPart{Text: "An aperture "},
		llm.ToolCallPart{ID: "call_1", Name: "search"},
		llm.TextPart{Text: "is an opening."},
	}}

	if got, want := message.Text(), "An aperture is an opening."; got != want {
		t.Errorf("Text() = %q, want %q", got, want)
	}
	if calls := message.ToolCalls(); len(calls) != 1 || calls[0].ID != "call_1" {
		t.Errorf("ToolCalls() = %+v", calls)
	}
}

func TestToolCallIsOnlyExecutableWhenItsArgumentsAreComplete(t *testing.T) {
	tests := []struct {
		name string
		call llm.ToolCallPart
		want bool
	}{
		{
			name: "complete arguments",
			call: llm.ToolCallPart{Arguments: json.RawMessage(`{"q":"x"}`), RawArguments: `{"q":"x"}`},
			want: true,
		},
		{
			name: "a fragment that never finished",
			call: llm.ToolCallPart{RawArguments: `{"q":`},
		},
		{
			name: "no arguments at all",
			call: llm.ToolCallPart{},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.call.Parsed(); got != tc.want {
				t.Errorf("Parsed() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestTheFourEndingsStayDistinct(t *testing.T) {
	// The design draft's requirement, written down where a future edit that
	// aliases two of them will trip over it: a finished block, a finished
	// response, a tool request that implies another turn, and a finished Run
	// are four different facts.
	endings := []llm.EventType{
		llm.EventPartEnd,
		llm.EventResponseEnd,
		llm.EventToolCallsRequested,
		llm.EventRunEnd,
	}

	seen := map[llm.EventType]bool{}
	for _, ending := range endings {
		if seen[ending] {
			t.Errorf("%q is used for two different endings", ending)
		}
		seen[ending] = true
	}
	for _, ending := range endings {
		if want := ending == llm.EventRunEnd; ending.TerminatesRun() != want {
			t.Errorf("%q.TerminatesRun() = %v, want %v — only the Run's own end is terminal", ending, !want, want)
		}
	}
}

func TestPartEndCarriesTheToolIdentityFromThePart(t *testing.T) {
	// part_start may go out before the provider has sent an id, so part_end is
	// the event a consumer can key on.
	call := llm.ToolCallPart{ID: "call_late", Name: "search_notes", RawArguments: "{}"}
	ev := llm.PartEndEvent(3, call)

	if ev.Index != 3 || ev.PartKind != llm.PartToolCall {
		t.Errorf("event = %+v", ev)
	}
	if ev.ToolCallID != "call_late" || ev.ToolName != "search_notes" {
		t.Errorf("tool identity = %q / %q", ev.ToolCallID, ev.ToolName)
	}
}

func TestRunStatusTerminality(t *testing.T) {
	tests := map[llm.RunStatus]bool{
		llm.RunPending:     false,
		llm.RunRunning:     false,
		llm.RunCompleted:   true,
		llm.RunFailed:      true,
		llm.RunCancelled:   true,
		llm.RunInterrupted: true,
	}

	for status, want := range tests {
		if got := status.Terminal(); got != want {
			t.Errorf("%q.Terminal() = %v, want %v", status, got, want)
		}
	}
}

func TestErrorKeepsItsCauseInspectable(t *testing.T) {
	cause := errors.New("unexpected end of JSON input")
	err := llm.Errorf(llm.ErrorProtocol, cause, "frame %d is not valid JSON", 4)

	if !llm.IsKind(err, llm.ErrorProtocol) {
		t.Error("the kind was lost")
	}
	if llm.IsKind(err, llm.ErrorTransport) {
		t.Error("a protocol error matched transport")
	}
	if !errors.Is(err, cause) {
		t.Error("the cause is no longer reachable, so nothing above can tell what broke")
	}
}
