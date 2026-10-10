package agent

import (
	"encoding/json"
	"testing"

	"github.com/qunqin/openchat/apps/server/internal/db/sqlc"
	"github.com/qunqin/openchat/apps/server/internal/llm"
)

func row(role sqlc.MessageRole, stop string, parts ...llm.Part) sqlc.Message {
	m := sqlc.Message{Role: role, Parts: EncodeParts(parts), Content: PlainText(parts)}
	if stop != "" {
		m.StopReason = &stop
	}
	return m
}

func call(id, name string) llm.ToolCallPart {
	return llm.ToolCallPart{ID: id, Name: name, Arguments: json.RawMessage(`{}`), RawArguments: `{}`}
}

func result(id, name, content string) llm.ToolResultPart {
	return llm.ToolResultPart{CallID: id, Name: name, Content: content, Status: llm.ToolSucceeded}
}

// shape is a context reduced to what the rules are about: who speaks, and
// which call each tool message answers.
func shape(msgs []llm.Message) []string {
	var out []string
	for _, m := range msgs {
		s := string(m.Role)
		for _, p := range m.Parts {
			switch p := p.(type) {
			case llm.ToolCallPart:
				s += " call:" + p.ID
			case llm.ToolResultPart:
				s += " result:" + p.CallID + ":" + string(p.Status)
			case llm.TextPart:
				s += " " + p.Text
			}
		}
		out = append(out, s)
	}
	return out
}

func assertShape(t *testing.T, got []llm.Message, want ...string) {
	t.Helper()
	have := shape(got)
	if len(have) != len(want) {
		t.Fatalf("got %d messages %q, want %q", len(have), have, want)
	}
	for i := range want {
		if have[i] != want[i] {
			t.Fatalf("message %d: got %q, want %q\nall: %q", i, have[i], want[i], have)
		}
	}
}

func TestBuildContextOrdersResultsByCall(t *testing.T) {
	// Results are committed in the order the tools finished.
	got, err := BuildContext([]sqlc.Message{
		row(sqlc.MessageRoleUser, "", llm.TextPart{Text: "q"}),
		row(sqlc.MessageRoleAssistant, StopToolCalls, call("a", "x"), call("b", "x")),
		row(sqlc.MessageRoleTool, "", result("b", "x", "B")),
		row(sqlc.MessageRoleTool, "", result("a", "x", "A")),
		row(sqlc.MessageRoleAssistant, StopStop, llm.TextPart{Text: "done"}),
	})
	if err != nil {
		t.Fatal(err)
	}
	assertShape(t, got,
		"user q",
		"assistant call:a call:b",
		"tool result:a:succeeded",
		"tool result:b:succeeded",
		"assistant done")
}

func TestBuildContextSynthesizesMissingResults(t *testing.T) {
	got, err := BuildContext([]sqlc.Message{
		row(sqlc.MessageRoleUser, "", llm.TextPart{Text: "q"}),
		row(sqlc.MessageRoleAssistant, StopToolCalls, call("a", "x"), call("b", "x")),
		row(sqlc.MessageRoleTool, "", result("a", "x", "A")),
	})
	if err != nil {
		t.Fatal(err)
	}
	assertShape(t, got,
		"user q",
		"assistant call:a call:b",
		"tool result:a:succeeded",
		"tool result:b:interrupted")
}

func TestBuildContextDropsOrphanResults(t *testing.T) {
	// The history window started between a call and its result.
	got, err := BuildContext([]sqlc.Message{
		row(sqlc.MessageRoleTool, "", result("a", "x", "A")),
		row(sqlc.MessageRoleAssistant, StopStop, llm.TextPart{Text: "answer"}),
		row(sqlc.MessageRoleUser, "", llm.TextPart{Text: "next"}),
	})
	if err != nil {
		t.Fatal(err)
	}
	assertShape(t, got, "assistant answer", "user next")
}

func TestBuildContextSkipsFailedAndCancelledResponses(t *testing.T) {
	got, err := BuildContext([]sqlc.Message{
		row(sqlc.MessageRoleUser, "", llm.TextPart{Text: "q"}),
		row(sqlc.MessageRoleAssistant, StopError, llm.TextPart{Text: "half"}),
		row(sqlc.MessageRoleAssistant, StopCancelled, llm.TextPart{Text: "stopped"}, call("a", "x")),
		row(sqlc.MessageRoleTool, "", result("a", "x", "A")),
		row(sqlc.MessageRoleUser, "", llm.TextPart{Text: "again"}),
	})
	if err != nil {
		t.Fatal(err)
	}
	// The cancelled response's result has no call left to answer.
	assertShape(t, got, "user q", "user again")
}

func TestBuildContextKeepsResultsAdjacentToTheirCall(t *testing.T) {
	got, err := BuildContext([]sqlc.Message{
		row(sqlc.MessageRoleAssistant, StopToolCalls, call("a", "x")),
		row(sqlc.MessageRoleUser, "", llm.TextPart{Text: "meanwhile"}),
		row(sqlc.MessageRoleTool, "", result("a", "x", "A")),
	})
	if err != nil {
		t.Fatal(err)
	}
	assertShape(t, got, "assistant call:a", "tool result:a:succeeded", "user meanwhile")
}

func TestPartsRoundTrip(t *testing.T) {
	ms := int64(12)
	in := []llm.Part{
		llm.TextPart{Text: "hi"},
		llm.ReasoningPart{Text: "hmm", Signature: "sig", Redacted: []byte{1, 2}},
		llm.ToolCallPart{ID: "c", Name: "web_search", Arguments: json.RawMessage(`{"query":"x"}`), RawArguments: `{"query":"x"}`},
		llm.ToolResultPart{CallID: "c", Name: "web_search", Content: "r", Status: llm.ToolSucceeded,
			Details: json.RawMessage(`{"results":[]}`), DurationMS: &ms},
	}
	out, err := DecodeParts(EncodeParts(in))
	if err != nil {
		t.Fatal(err)
	}
	if string(EncodeParts(out)) != string(EncodeParts(in)) {
		t.Fatalf("round trip changed the parts:\n%s\n%s", EncodeParts(in), EncodeParts(out))
	}
	if _, err := DecodeParts([]byte(`[{"type":"hologram"}]`)); err == nil {
		t.Fatal("an unknown part type decoded")
	}
}
