package llm_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/qunqin24/openchat/apps/server/internal/llm"
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

func TestClipBodyNeverCutsACharacterInHalf(t *testing.T) {
	// 255 bytes of ASCII, then three-byte characters: byte 256 falls inside one.
	body := strings.Repeat("a", 255) + strings.Repeat("错", 10)
	got := llm.ClipBody("  " + body + "\n")
	if !utf8.ValidString(got) {
		t.Fatalf("clipped text is not valid UTF-8: %q", got)
	}
	if want := strings.Repeat("a", 255) + "…"; got != want {
		t.Errorf("clipped = %q, want the cut moved back to the character boundary", got)
	}
	if short := llm.ClipBody(" rate limited \n"); short != "rate limited" {
		t.Errorf("short body = %q, want it trimmed and whole", short)
	}
}
