package api

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/qunqin/openchat/apps/server/internal/agent"
	"github.com/qunqin/openchat/apps/server/internal/db/dbtest"
	"github.com/qunqin/openchat/apps/server/internal/db/sqlc"
)

// A guest's compressed context: the browser sends its summary with a run, the
// server writes it over the rows of the request and builds the model's
// context from it, and the summary a run makes is handed back. Nothing of it
// stays on the server. Same scratch database as the other guest tests.

func summarising(h *guestHarness) {
	h.script = func(n int, body string) string {
		if strings.Contains(body, "conversation-context compressor") {
			return openAIWire.text("SUMMARY BY MODEL")
		}
		return openAIWire.text("ok")
	}
}

// The browser holds rows the summary replaces; it sends only the question the
// summary kept, the row it ends at, and what comes after - and the model is
// sent the summary in their place.
func TestGuestRunBuildsItsContextFromTheSummary(t *testing.T) {
	h := newGuestHarness(t)
	summarising(h)
	conv := uuid.New()
	kept, through, after, input := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	base := time.Now().Add(-time.Hour)

	frames := h.run(map[string]any{
		"action":         "send",
		"conversationId": conv,
		"model":          h.model,
		"messages": []any{
			userRow(kept, "the question being worked on", base),
			assistantRow(through, "an answer the summary covers", base.Add(time.Minute)),
			userRow(after, "a question after the summary", base.Add(2*time.Minute)),
			assistantRow(uuid.New(), "an answer after the summary", base.Add(3*time.Minute)),
		},
		"input": userRow(input, "the newest question", time.Now()),
		"summary": map[string]any{
			"content": "EARLIER STUFF", "throughMessageId": through, "keptMessageId": kept,
			"model": h.model, "inputTokens": 10, "outputTokens": 5, "createdAt": base.Format(time.RFC3339Nano),
		},
	})
	if frames.status != http.StatusOK {
		t.Fatalf("status %d: %v", frames.status, frames.error)
	}

	chat := h.requests()[0]
	for _, want := range []string{"EARLIER STUFF", "compressed_history_summary", "the question being worked on",
		"a question after the summary", "an answer after the summary", "the newest question"} {
		if !strings.Contains(chat, want) {
			t.Errorf("the model's request lacks %q", want)
		}
	}
	if strings.Contains(chat, "an answer the summary covers") {
		t.Error("a row the summary covers was sent as well")
	}
	if order := strings.Index(chat, "EARLIER STUFF"); order < 0 || order > strings.Index(chat, "the question being worked on") {
		t.Error("the summary should come before the question it kept")
	}

	// Handed back, as it stands, for the browser to keep; and gone from the server.
	end := frames.end()
	summary, _ := end["summary"].(map[string]any)
	if summary["content"] != "EARLIER STUFF" || summary["throughMessageId"] != through.String() || summary["keptMessageId"] != kept.String() {
		t.Errorf("guest_end summary = %v", end["summary"])
	}
	h.noGuestRows(conv)
	var n int
	if err := h.pool.QueryRow(context.Background(), `SELECT count(*) FROM context_summaries WHERE conversation_id = $1`, conv).Scan(&n); err != nil || n != 0 {
		t.Errorf("%d summaries left on the server (%v)", n, err)
	}
}

// A summary that does not fit the rows of the request is refused before
// anything is written.
func TestGuestRunRefusesAnInconsistentSummary(t *testing.T) {
	h := newGuestHarness(t)
	u1, a1, input := uuid.New(), uuid.New(), uuid.New()
	base := time.Now().Add(-time.Hour)
	rows := []any{userRow(u1, "q", base), assistantRow(a1, "a", base.Add(time.Second))}
	run := func(summary map[string]any) guestFrames {
		return h.run(map[string]any{"action": "send", "conversationId": uuid.New(), "model": h.model,
			"messages": rows, "input": userRow(input, "next", time.Now()), "summary": summary})
	}
	ok := func(over map[string]any) map[string]any {
		s := map[string]any{"content": "S", "throughMessageId": a1, "model": "m", "inputTokens": 0, "outputTokens": 0,
			"createdAt": base.Format(time.RFC3339Nano)}
		for k, v := range over {
			s[k] = v
		}
		return s
	}
	for name, summary := range map[string]map[string]any{
		"ends at a row that is not sent":       ok(map[string]any{"throughMessageId": uuid.New()}),
		"keeps a row that is not sent":         ok(map[string]any{"keptMessageId": uuid.New()}),
		"keeps an assistant row":               ok(map[string]any{"keptMessageId": a1}),
		"keeps a row after the one it ends at": ok(map[string]any{"throughMessageId": u1, "keptMessageId": a1}),
		"is empty":                             ok(map[string]any{"content": "  "}),
	} {
		got := run(summary)
		if got.status != http.StatusBadRequest || got.error["code"] != "invalid_summary" {
			t.Errorf("%s: %d %v", name, got.status, got.error)
		}
	}
	if got := run(ok(nil)); got.status != http.StatusOK {
		t.Fatalf("a consistent summary: %d %v", got.status, got.error)
	}
}

// A guest's run compresses itself when it nears the window, and the summary it
// makes is told in the run's events and handed back at the end - ending at a
// row the browser holds.
func TestGuestRunCompressesItself(t *testing.T) {
	pool := dbtest.Pool(t, "api_guest")
	h := newGuestHarnessWith(t, func(o *agent.Options) { o.Compression = NewCompression(pool) })
	summarising(h)
	// A model whose window is 1200 tokens: compression starts at 600.
	limit := int64(1200)
	if _, err := h.q.SaveModelOverride(context.Background(), sqlc.SaveModelOverrideParams{ModelID: h.model, ContextLimit: &limit, InputModalities: []string{"image"}}); err != nil {
		t.Fatal(err)
	}

	long := strings.Repeat("lorem ipsum ", 120) // about 360 tokens
	conv := uuid.New()
	u1, a1, input := uuid.New(), uuid.New(), uuid.New()
	base := time.Now().Add(-time.Hour)
	frames := h.run(map[string]any{
		"action":         "send",
		"conversationId": conv,
		"model":          h.model,
		"titled":         true, // no title request among the model's
		"messages":       []any{userRow(u1, long, base), assistantRow(a1, long, base.Add(time.Minute))},
		"input":          userRow(input, "the newest question", time.Now()),
	})
	if frames.status != http.StatusOK {
		t.Fatalf("status %d: %v", frames.status, frames.error)
	}

	// Told as it happened.
	if frames.find("compaction_started") == nil {
		t.Fatalf("no compaction_started in %v", frames.events())
	}
	finished := frames.find("compaction_finished")
	summary, _ := finished["summary"].(map[string]any)
	if summary["content"] != "SUMMARY BY MODEL" || summary["throughMessageId"] != input.String() || summary["keptMessageId"] != input.String() {
		t.Fatalf("compaction_finished = %v: want a summary through the new question, keeping it", finished)
	}
	// Handed back at the end as well, for the browser to keep.
	if end := frames.end(); end["summary"] == nil || end["summary"].(map[string]any)["content"] != "SUMMARY BY MODEL" {
		t.Errorf("guest_end summary = %v", end["summary"])
	}
	// The answer was asked with the summary and the question.
	requests := h.requests()
	if len(requests) != 2 {
		t.Fatalf("%d model requests, want the summary and the answer", len(requests))
	}
	answer := requests[1]
	if !strings.Contains(answer, "SUMMARY BY MODEL") || !strings.Contains(answer, "the newest question") || strings.Contains(answer, "lorem ipsum") {
		t.Errorf("the answer's request: %.300s", answer)
	}
	h.noGuestRows(conv)
}
