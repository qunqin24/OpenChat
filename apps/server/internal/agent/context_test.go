package agent

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/qunqin24/openchat/apps/server/internal/db/sqlc"
	"github.com/qunqin24/openchat/apps/server/internal/llm"
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

func TestBuildContextKeepsAToolCallsSignature(t *testing.T) {
	// Gemini 3 wants the signature back on the call it came on, from the
	// stored row all the way into the next request.
	signed := call("a", "x")
	signed.Signature = llm.SignatureGemini + "CvUBsig="
	got, err := BuildContext([]sqlc.Message{
		row(sqlc.MessageRoleUser, "", llm.TextPart{Text: "q"}),
		row(sqlc.MessageRoleAssistant, StopToolCalls, signed),
		row(sqlc.MessageRoleTool, "", result("a", "x", "A")),
	})
	if err != nil {
		t.Fatal(err)
	}
	calls := got[1].ToolCalls()
	if len(calls) != 1 || calls[0].Signature != signed.Signature {
		t.Fatalf("calls = %+v, want the signature to survive the stored row", calls)
	}
}

func TestBuildContextDatesTheFirstUserMessageOfEachDay(t *testing.T) {
	at := func(day string) pgtype.Timestamptz {
		ts, err := time.Parse(time.RFC3339, day)
		if err != nil {
			t.Fatal(err)
		}
		return pgtype.Timestamptz{Time: ts, Valid: true}
	}
	user := func(text, when string) sqlc.Message {
		m := row(sqlc.MessageRoleUser, "", llm.TextPart{Text: text})
		m.CreatedAt = at(when)
		return m
	}
	rows := []sqlc.Message{
		user("one", "2026-10-09T23:00:00Z"),
		row(sqlc.MessageRoleAssistant, StopStop, llm.TextPart{Text: "a"}),
		user("two", "2026-10-09T23:30:00Z"),
		row(sqlc.MessageRoleAssistant, StopStop, llm.TextPart{Text: "b"}),
		user("three", "2026-10-10T00:05:00Z"),
	}
	got, err := BuildContext(rows)
	if err != nil {
		t.Fatal(err)
	}
	assertShape(t, got, "user [Date: 2026-10-09] one", "assistant a", "user two", "assistant b", "user [Date: 2026-10-10] three")

	// Appending a turn leaves everything before it byte-identical, which is the
	// property a cached prefix and a replayed thinking block depend on.
	more, err := BuildContext(append(rows, row(sqlc.MessageRoleAssistant, StopStop, llm.TextPart{Text: "c"}),
		user("four", "2026-10-10T09:00:00Z")))
	if err != nil {
		t.Fatal(err)
	}
	before, _ := json.Marshal(got)
	prefix, _ := json.Marshal(more[:len(got)])
	if string(before) != string(prefix) {
		t.Errorf("history prefix changed:\n%s\n%s", before, prefix)
	}
}

// A question the user did not answer — they wrote something else — is closed
// off with a result saying so, which is not an error, so every provider gets a
// call with a result and the new message after it.
func TestBuildContextClosesASkippedQuestion(t *testing.T) {
	got, err := BuildContext([]sqlc.Message{
		row(sqlc.MessageRoleUser, "", llm.TextPart{Text: "q"}),
		row(sqlc.MessageRoleAssistant, StopToolCalls, call("a", ToolAskUser)),
		row(sqlc.MessageRoleUser, "", llm.TextPart{Text: "never mind"}),
	})
	if err != nil {
		t.Fatal(err)
	}
	assertShape(t, got,
		"user q",
		"assistant call:a",
		"tool result:a:succeeded",
		"user never mind")
	if text := got[2].Parts[0].(llm.ToolResultPart).Content; text != skippedResult {
		t.Fatalf("skipped result = %q", text)
	}
}

// An answered question is an ordinary result, and one still open at the tail
// (nothing came after it) is closed the same way.
func TestBuildContextKeepsAnAnswerAndClosesAnOpenQuestion(t *testing.T) {
	got, err := BuildContext([]sqlc.Message{
		row(sqlc.MessageRoleUser, "", llm.TextPart{Text: "q"}),
		row(sqlc.MessageRoleAssistant, StopToolCalls, call("a", ToolAskUser)),
		row(sqlc.MessageRoleTool, "", result("a", ToolAskUser, "The user chose A")),
		row(sqlc.MessageRoleAssistant, StopStop, llm.TextPart{Text: "ok"}),
		row(sqlc.MessageRoleUser, "", llm.TextPart{Text: "again"}),
		row(sqlc.MessageRoleAssistant, StopToolCalls, call("b", ToolAskUser)),
	})
	if err != nil {
		t.Fatal(err)
	}
	assertShape(t, got,
		"user q",
		"assistant call:a",
		"tool result:a:succeeded",
		"assistant ok",
		"user again",
		"assistant call:b",
		"tool result:b:succeeded")
	if text := got[2].Parts[0].(llm.ToolResultPart).Content; text != "The user chose A" {
		t.Fatalf("answer = %q", text)
	}
}

// An answer written before leaked tool-call markup was hidden as it streamed is
// sent back without it: the model would learn to write more of it.
func TestContextDropsLeakedToolMarkup(t *testing.T) {
	got, err := BuildContext([]sqlc.Message{
		row(sqlc.MessageRoleUser, "", llm.TextPart{Text: "today's paper?"}),
		row(sqlc.MessageRoleAssistant, "stop", llm.TextPart{Text: "Here is what I found.\n\n<｜DSML｜ calls>\n<｜DSML｜ invoke name=\"web_search\"></｜DSML｜ invoke>\n</｜DSML｜ calls>"}),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[1].Text() != "Here is what I found." {
		t.Fatalf("context: %+v", got)
	}
}

// A message sent an hour or more after the row before it is told the time,
// in the person's zone, and how long it has been; within the hour, only a new
// day is dated.
func TestBuildContextTellsTheTimeAfterAPause(t *testing.T) {
	at := func(when string) pgtype.Timestamptz {
		ts, err := time.Parse(time.RFC3339, when)
		if err != nil {
			t.Fatal(err)
		}
		return pgtype.Timestamptz{Time: ts, Valid: true}
	}
	stamp := func(m sqlc.Message, when string) sqlc.Message {
		m.CreatedAt = at(when)
		return m
	}
	rows := []sqlc.Message{
		stamp(row(sqlc.MessageRoleUser, "", llm.TextPart{Text: "one"}), "2026-10-28T01:00:00Z"),
		stamp(row(sqlc.MessageRoleAssistant, StopStop, llm.TextPart{Text: "a"}), "2026-10-28T01:00:30Z"),
		stamp(row(sqlc.MessageRoleUser, "", llm.TextPart{Text: "two"}), "2026-10-28T01:40:00Z"),
		stamp(row(sqlc.MessageRoleAssistant, StopStop, llm.TextPart{Text: "b"}), "2026-10-28T01:41:00Z"),
		stamp(row(sqlc.MessageRoleUser, "", llm.TextPart{Text: "three"}), "2026-10-28T04:44:00Z"),
		stamp(row(sqlc.MessageRoleAssistant, StopStop, llm.TextPart{Text: "c"}), "2026-10-28T04:45:00Z"),
		stamp(row(sqlc.MessageRoleUser, "", llm.TextPart{Text: "four"}), "2026-10-31T02:00:00Z"),
	}
	shanghai, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Skip("no time zone data")
	}
	got, err := BuildContextIn(rows, shanghai)
	if err != nil {
		t.Fatal(err)
	}
	assertShape(t, got,
		"user [Date: 2026-10-28] one", "assistant a",
		"user two", "assistant b",
		"user [Time: 2026-10-28 12:44 Asia/Shanghai, 3 hours after the previous message] three", "assistant c",
		"user [Time: 2026-10-31 10:00 Asia/Shanghai, 2 days after the previous message] four")
}

func TestPauseText(t *testing.T) {
	for pause, want := range map[time.Duration]string{
		61 * time.Minute:    "61 minutes",
		3 * time.Hour:       "3 hours",
		47 * time.Hour:      "47 hours",
		72 * time.Hour:      "3 days",
		24 * time.Hour * 90: "3 months",
	} {
		if got := pauseText(pause); got != want {
			t.Errorf("%v: %q, want %q", pause, got, want)
		}
	}
}
