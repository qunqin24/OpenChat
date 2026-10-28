package agent

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/qunqin24/openchat/apps/server/internal/db/sqlc"
	"github.com/qunqin24/openchat/apps/server/internal/llm"
)

// Compressing a conversation's context: when a run does it, what the request
// built afterwards holds, and what a summary covers. Needs the same scratch
// database as the other agent tests (TEST_DATABASE_URL).

// compressing is the options of a runner that compresses at half of a window
// of the given size, and has a system prompt.
func compressing(window int64) func(*Options) {
	return func(o *Options) {
		o.SystemPrompt = func(context.Context, time.Time, []Tool) string { return "SYS" }
		o.Compression = &Compression{
			Auto:    func(context.Context) bool { return true },
			Profile: func(context.Context, string) (ModelProfile, error) { return ModelProfile{ContextWindow: window}, nil },
		}
	}
}

// bigArgs is a tool call's arguments of about n tokens.
func bigArgs(n int) string { return `{"q":"` + strings.Repeat("x", n*4) + `"}` }

// row appends a row of the conversation as the run layer writes them.
func (f *fixture) row(role sqlc.MessageRole, text string, stop ...string) sqlc.Message {
	f.t.Helper()
	params := sqlc.AppendEntryParams{
		ConversationID: f.conv,
		Role:           role,
		Content:        text,
		Parts:          EncodeParts([]llm.Part{llm.TextPart{Text: text}}),
		Status:         sqlc.MessageStatusComplete,
	}
	if len(stop) > 0 {
		params.StopReason = &stop[0]
		if stop[0] == StopError {
			params.Status = sqlc.MessageStatusFailed
		}
	}
	msg, err := f.q.AppendEntry(context.Background(), params)
	if err != nil {
		f.t.Fatal(err)
	}
	return msg
}

func (f *fixture) summary() *Summary {
	f.t.Helper()
	s, err := LoadSummary(context.Background(), f.q, f.conv)
	if err != nil {
		f.t.Fatal(err)
	}
	return s
}

func (f *fixture) setSummary(through sqlc.Message, kept *sqlc.Message, content string) {
	f.t.Helper()
	params := sqlc.UpsertContextSummaryParams{ConversationID: f.conv, ThroughMessageID: through.ID, Content: content, ModelID: "m"}
	if kept != nil {
		params.KeptMessageID = &kept.ID
	}
	if _, err := f.q.UpsertContextSummary(context.Background(), params); err != nil {
		f.t.Fatal(err)
	}
}

// roles is a request's messages as role:first-words, for reading it at a glance.
func roles(messages []llm.Message) []string {
	var out []string
	for _, m := range messages {
		text := dateStamp0.ReplaceAllString(m.Text(), "")
		if calls := m.ToolCalls(); len(calls) > 0 {
			text += "call:" + calls[0].Name
		}
		for _, part := range m.Parts {
			if r, ok := part.(llm.ToolResultPart); ok {
				text += "result:" + r.Name
			}
		}
		if len(text) > 24 {
			text = text[:24]
		}
		out = append(out, string(m.Role)+":"+text)
	}
	return out
}

func equalStrings(t *testing.T, what string, got, want []string) {
	t.Helper()
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("%s:\n got  %q\n want %q", what, got, want)
	}
}

func (f *fixture) compactParams() CompactParams {
	return CompactParams{
		ConversationID: f.conv, UserID: f.user, ModelID: "m", Client: f.models,
		Profile: ModelProfile{ContextWindow: 100_000},
	}
}

// What a request is built from once there is a summary: the summary, the
// question kept from inside the range, and only the rows after the range.
func TestContextRowsAfterASummary(t *testing.T) {
	f := newFixture(t, nil)
	q1 := f.row(sqlc.MessageRoleUser, "q1")
	f.row(sqlc.MessageRoleAssistant, "a1", StopStop)
	q2 := f.row(sqlc.MessageRoleUser, "q2")
	a2 := f.row(sqlc.MessageRoleAssistant, "a2", StopStop)
	f.row(sqlc.MessageRoleUser, "q3")

	// Without one, every row.
	summary, rows, err := LoadContextRows(context.Background(), f.q, f.conv, 100)
	if err != nil || summary != nil || len(rows) != 5 {
		t.Fatalf("no summary: %v, %d rows, %v", summary, len(rows), err)
	}

	// The summary covers up to a2 and keeps q2.
	f.setSummary(a2, &q2, "the story so far")
	summary, rows, err = LoadContextRows(context.Background(), f.q, f.conv, 100)
	if err != nil {
		t.Fatal(err)
	}
	if summary == nil || summary.Content != "the story so far" || summary.ThroughID != a2.ID || summary.KeptID == nil || *summary.KeptID != q2.ID {
		t.Fatalf("summary = %+v", summary)
	}
	if len(rows) != 2 || rows[0].ID != q2.ID || rows[1].Content != "q3" {
		t.Fatalf("rows = %d: the kept question then the rows after the range", len(rows))
	}
	messages, err := BuildRequestContext(summary, rows)
	if err != nil {
		t.Fatal(err)
	}
	equalStrings(t, "request", roles(messages), []string{
		"user:<compressed_history_summ", "user:q2", "user:q3"})
	if !strings.Contains(messages[0].Text(), "the story so far") {
		t.Fatalf("summary message: %q", messages[0].Text())
	}

	// Without a kept question: the summary then what follows.
	f.setSummary(a2, nil, "all of it")
	summary, rows, _ = LoadContextRows(context.Background(), f.q, f.conv, 100)
	messages, _ = BuildRequestContext(summary, rows)
	equalStrings(t, "no kept question", roles(messages), []string{"user:<compressed_history_summ", "user:q3"})

	// The history cap keeps the newest rows after the range.
	_, rows, _ = LoadContextRows(context.Background(), f.q, f.conv, 1)
	if len(rows) != 1 || rows[0].Content != "q3" {
		t.Fatalf("capped rows = %d", len(rows))
	}
	_ = q1
}

// A long tool loop crosses half of the window, and the next request is the
// system prompt, the summary, the question being answered and nothing else:
// the tool calls and results the loop had made went into the summary.
func TestRunCompressesALongToolLoop(t *testing.T) {
	echo := &echoTool{name: "echo", replay: true}
	f := newFixtureWith(t, compressing(3000), []Tool{echo},
		toolCall(t, "c1", "echo", bigArgs(600)),
		answer(t, "- the tool was called once"), // the summary
		answer(t, "all done"))
	run := f.admit("question")
	f.runner.Start(run.ID)
	f.waitFor(run.ID, sqlc.RunStatusCompleted)

	if n := len(f.models.reqs); n != 3 {
		t.Fatalf("%d requests, want 3 (call, summary, answer)", n)
	}

	// The summariser: its own system prompt, and the history it was handed.
	summariser := f.models.reqs[1]
	if summariser.Messages[0].Role != llm.RoleSystem || summariser.Messages[0].Text() != systemPromptSummary {
		t.Fatalf("summariser system prompt: %q", summariser.Messages[0].Text())
	}
	read := summariser.Messages[1].Text()
	for _, want := range []string{"<chat_history>", "[Called tool echo with", `<tool name="echo">`, "<latest_user_message>\nquestion\n"} {
		if !strings.Contains(read, want) {
			t.Errorf("the summariser was not shown %q:\n%.400s", want, read)
		}
	}
	if strings.Contains(read, "<user>question</user>") {
		t.Error("the question being answered is kept, so it is not part of the history")
	}

	// The answer's request: [system, summary, kept question].
	equalStrings(t, "request after compression", roles(f.models.reqs[2].Messages),
		[]string{"system:SYS", "user:<compressed_history_summ", "user:question"})
	if !strings.Contains(f.models.reqs[2].Messages[1].Text(), "the tool was called once") {
		t.Error("the summary the model wrote is what it is sent")
	}

	// The summary: through the last tool result, keeping the question.
	s := f.summary()
	if s == nil {
		t.Fatal("no summary was written")
	}
	rows, _ := f.q.ListRecentMessages(context.Background(), sqlc.ListRecentMessagesParams{ConversationID: f.conv, RowLimit: 10})
	if s.ThroughID != rows[2].ID || s.KeptID == nil || *s.KeptID != rows[0].ID || s.Content != "- the tool was called once" {
		t.Fatalf("summary %+v over rows %v", s, []string{rows[0].Content, string(rows[1].Role), string(rows[2].Role)})
	}
	// The transcript itself is untouched.
	f.assertTranscript("user question", "assistant:tool_calls call:echo", "tool result:succeeded", "assistant:stop all done")

	// Told as it happened, and live state cleared.
	var started, finished int
	for _, b := range f.events() {
		for _, e := range b.Events {
			switch e := e.(type) {
			case CompactionStarted:
				started++
			case CompactionFinished:
				finished++
				if e.Summary == nil || e.Summary.Content != s.Content {
					t.Errorf("finished event = %+v", e.Summary)
				}
			}
		}
	}
	if started != 1 || finished != 1 {
		t.Errorf("events: %d started, %d finished", started, finished)
	}
	if got, _ := f.q.GetRun(context.Background(), run.ID); len(got.Live) > 0 && strings.Contains(string(got.Live), "compacting") {
		t.Errorf("live state still says compacting: %s", got.Live)
	}

	// In the ledger as its own kind, under the run.
	kinds := map[string]int{}
	for _, r := range f.ledger(run.ID) {
		kinds[r.kind]++
	}
	if kinds["chat"] != 2 || kinds["compression"] != 1 {
		t.Errorf("ledger kinds = %v", kinds)
	}
}

// A summariser that fails costs nothing but itself: the request goes out as it
// was, no summary is written, and the next request tries again.
func TestRunSendsUncompressedWhenTheSummaryFails(t *testing.T) {
	echo := &echoTool{name: "echo", replay: true}
	failing := func() (llm.ModelClient, error) { return nil, errors.New("summariser is down") }
	f := newFixtureWith(t, compressing(3000), []Tool{echo},
		toolCall(t, "c1", "echo", bigArgs(600)),
		failing, // before the second request
		toolCall(t, "c2", "echo", bigArgs(600)),
		failing, // and again before the third, the request having grown since
		answer(t, "done anyway"))
	run := f.admit("question")
	f.runner.Start(run.ID)
	f.waitFor(run.ID, sqlc.RunStatusCompleted)

	if f.summary() != nil {
		t.Fatal("a failed compression must write nothing")
	}
	if n := len(f.models.reqs); n != 5 {
		t.Fatalf("%d requests, want 5 (call, failed summary, call, failed summary, answer)", n)
	}
	// The requests after a failure carry the whole history.
	equalStrings(t, "uncompressed request", roles(f.models.reqs[2].Messages),
		[]string{"system:SYS", "user:question", "assistant:call:echo", "tool:result:echo"})
	equalStrings(t, "and the one after", roles(f.models.reqs[4].Messages),
		[]string{"system:SYS", "user:question", "assistant:call:echo", "tool:result:echo", "assistant:call:echo", "tool:result:echo"})

	// Each attempt was told to the page and ended without a summary.
	var finished []bool
	for _, b := range f.events() {
		for _, e := range b.Events {
			if e, ok := e.(CompactionFinished); ok {
				finished = append(finished, e.Summary != nil)
			}
		}
	}
	if len(finished) != 2 || finished[0] || finished[1] {
		t.Errorf("finished events = %v", finished)
	}
	// The failed requests are in the ledger, as errors.
	var failed int
	for _, r := range f.ledger(run.ID) {
		if r.kind == "compression" && r.status == "error" {
			failed++
		}
	}
	if failed != 2 {
		t.Errorf("%d failed compression rows in the ledger", failed)
	}
}

// Without the administrator's switch nothing is compressed on its own, however
// large the request: today's behaviour.
func TestRunDoesNotCompressWhenSwitchedOff(t *testing.T) {
	echo := &echoTool{name: "echo", replay: true}
	f := newFixtureWith(t, func(o *Options) {
		compressing(3000)(o)
		o.Compression.Auto = func(context.Context) bool { return false }
	}, []Tool{echo},
		toolCall(t, "c1", "echo", bigArgs(600)),
		answer(t, "done"))
	run := f.admit("question")
	f.runner.Start(run.ID)
	f.waitFor(run.ID, sqlc.RunStatusCompleted)
	if f.summary() != nil || len(f.models.reqs) != 2 {
		t.Fatalf("a summary was written (%v) or requests were added (%d)", f.summary(), len(f.models.reqs))
	}
}

// Right after a compression the bar is higher: a request between half and 65%
// of the window is not compressed again while it has a summary - and is when it
// has none.
func TestSummarisedConversationsCompressAtSixtyFivePercent(t *testing.T) {
	// 1900 characters is about 475 tokens, 600 with the safety factor and the
	// rest: over half of 1000, under 65% of it.
	text := strings.Repeat("w", 1900)

	t.Run("with a summary", func(t *testing.T) {
		f := newFixtureWith(t, compressing(1000), nil, answer(t, "ok"))
		old := f.row(sqlc.MessageRoleUser, "an earlier question")
		f.setSummary(old, nil, "S")
		run := f.admit(text)
		f.runner.Start(run.ID)
		f.waitFor(run.ID, sqlc.RunStatusCompleted)
		if len(f.models.reqs) != 1 {
			t.Fatalf("%d requests: a summarised conversation under 65%% was compressed", len(f.models.reqs))
		}
	})
	t.Run("without one", func(t *testing.T) {
		f := newFixtureWith(t, compressing(1000), nil, answer(t, "- earlier stuff"), answer(t, "ok"))
		f.row(sqlc.MessageRoleUser, "an earlier question")
		f.row(sqlc.MessageRoleAssistant, "an earlier answer", StopStop)
		run := f.admit(text)
		f.runner.Start(run.ID)
		f.waitFor(run.ID, sqlc.RunStatusCompleted)
		if len(f.models.reqs) != 2 || f.summary() == nil {
			t.Fatalf("%d requests, summary %v: over half of the window should compress", len(f.models.reqs), f.summary())
		}
	})
}

// A question too large to summarise away does not make every request pay for a
// summary: after one compression the run does not try again until it has grown.
func TestRunDoesNotCompressAgainUntilItHasGrown(t *testing.T) {
	echo := &echoTool{name: "echo", replay: true}
	f := newFixtureWith(t, compressing(1000), []Tool{echo},
		answer(t, "- summary"), // before the first request
		toolCall(t, "c1", "echo", `{}`),
		toolCall(t, "c2", "echo", `{}`),
		answer(t, "done"))
	f.row(sqlc.MessageRoleUser, "earlier")
	f.row(sqlc.MessageRoleAssistant, "earlier answer", StopStop)
	// The question alone is over the threshold and cannot be shrunk.
	run := f.admit(strings.Repeat("q", 3000))
	f.runner.Start(run.ID)
	f.waitFor(run.ID, sqlc.RunStatusCompleted)

	// Compressed once, before the first request; not again before the others.
	if n := len(f.models.reqs); n != 4 {
		t.Fatalf("%d requests, want 4: compress, call, call, answer", n)
	}
	if f.models.reqs[0].Messages[0].Text() != systemPromptSummary {
		t.Fatal("the first request should be the summary")
	}
}

// Compressing again folds the old summary and what came after into one new
// summary, which replaces it; the question the old one kept becomes history.
func TestIncrementalCompressionReplacesTheSummary(t *testing.T) {
	f := newFixture(t, nil, answer(t, "SUMMARY ONE"), answer(t, "SUMMARY TWO"))
	ctx := context.Background()
	f.row(sqlc.MessageRoleUser, "first question")
	f.row(sqlc.MessageRoleAssistant, "first answer", StopStop)
	q2 := f.row(sqlc.MessageRoleUser, "second question")
	a2 := f.row(sqlc.MessageRoleAssistant, "second answer", StopStop)

	first, err := f.runner.Compactor().Compact(ctx, f.compactParams())
	if err != nil {
		t.Fatal(err)
	}
	if first.Content != "SUMMARY ONE" || first.ThroughID != a2.ID || first.KeptID == nil || *first.KeptID != q2.ID {
		t.Fatalf("first summary %+v", first)
	}
	if body := f.models.reqs[0].Messages[1].Text(); !strings.Contains(body, "first question</user>") ||
		strings.Contains(body, "second question</user>") || !strings.Contains(body, "<assistant>second answer</assistant>") {
		t.Fatalf("first request:\n%s", body)
	}

	q3 := f.row(sqlc.MessageRoleUser, "third question")
	a3 := f.row(sqlc.MessageRoleAssistant, "third answer", StopStop)
	second, err := f.runner.Compactor().Compact(ctx, f.compactParams())
	if err != nil {
		t.Fatal(err)
	}
	if second.Content != "SUMMARY TWO" || second.ThroughID != a3.ID || second.KeptID == nil || *second.KeptID != q3.ID {
		t.Fatalf("second summary %+v", second)
	}
	body := f.models.reqs[1].Messages[1].Text()
	if !strings.HasPrefix(body, "Existing summary:\nSUMMARY ONE\n\n") {
		t.Fatalf("the old summary should lead:\n%.300s", body)
	}
	// Not what the old summary already says; the question it kept (never
	// written into it) and what came after.
	for _, want := range []string{"second question</user>", "<assistant>third answer</assistant>", "<latest_user_message>\nthird question"} {
		if !strings.Contains(body, want) {
			t.Errorf("second request lacks %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "first question") {
		t.Error("the rows the old summary covers are not read again")
	}
	if n := f.count(`SELECT count(*) FROM context_summaries WHERE conversation_id = $1`, f.conv); n != 1 {
		t.Fatalf("%d summaries, want exactly one", n)
	}
	// Both were counted.
	var kinds int
	_ = f.pool.QueryRow(ctx, `SELECT count(*) FROM usage_logs WHERE conversation_id = $1 AND kind = 'compression'`, f.conv).Scan(&kinds)
	if kinds != 2 {
		t.Errorf("%d compression rows in the ledger", kinds)
	}
}

// With nothing after the summary there is nothing to compress.
func TestCompactNothingNew(t *testing.T) {
	f := newFixture(t, nil, answer(t, "S"))
	ctx := context.Background()
	f.row(sqlc.MessageRoleUser, "q")
	f.row(sqlc.MessageRoleAssistant, "a", StopStop)
	if _, err := f.runner.Compactor().Compact(ctx, f.compactParams()); err != nil {
		t.Fatal(err)
	}
	if _, err := f.runner.Compactor().Compact(ctx, f.compactParams()); !errors.Is(err, ErrNothingToCompact) {
		t.Fatalf("second compaction = %v, want ErrNothingToCompact", err)
	}
	// Nor a conversation with no rows at all.
	empty := newFixture(t, nil)
	if _, err := empty.runner.Compactor().Compact(ctx, empty.compactParams()); !errors.Is(err, ErrNothingToCompact) {
		t.Fatalf("empty conversation = %v", err)
	}
}

// Two compressions of one conversation do not run at once.
func TestCompactionIsExclusivePerConversation(t *testing.T) {
	f := newFixture(t, nil, answer(t, "S"))
	f.row(sqlc.MessageRoleUser, "q")
	f.row(sqlc.MessageRoleAssistant, "a", StopStop)

	release, err := f.runner.Compactor().lock(context.Background(), f.conv)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.runner.Compactor().Compact(context.Background(), f.compactParams()); !errors.Is(err, ErrCompactionBusy) {
		t.Fatalf("while another holds it: %v", err)
	}
	release()
	if _, err := f.runner.Compactor().Compact(context.Background(), f.compactParams()); err != nil {
		t.Fatalf("after it is released: %v", err)
	}
}

// A summary is never written over rows an edit deleted while it was being made.
func TestSummaryOfADeletedRowIsRefused(t *testing.T) {
	f := newFixture(t, nil)
	gone := f.row(sqlc.MessageRoleUser, "q")
	if _, err := f.pool.Exec(context.Background(), `DELETE FROM messages WHERE id = $1`, gone.ID); err != nil {
		t.Fatal(err)
	}
	_, err := f.q.UpsertContextSummary(context.Background(), sqlc.UpsertContextSummaryParams{
		ConversationID: f.conv, ThroughMessageID: gone.ID, Content: "x", ModelID: "m"})
	if err == nil {
		t.Fatal("a summary through a deleted row was accepted")
	}
}

var dateStamp0 = regexp.MustCompile(`\[Date: [0-9-]+\]`)

func (f *fixture) count(query string, args ...any) int {
	f.t.Helper()
	var n int
	if err := f.pool.QueryRow(context.Background(), query, args...).Scan(&n); err != nil {
		f.t.Fatalf("%s: %v", query, err)
	}
	return n
}

// A failed compression is not paid for again before every round: only once
// the request has grown by a tenth of the window.
func TestRunDoesNotRetryAFailedCompressionUntilItGrows(t *testing.T) {
	echo := &echoTool{name: "echo", replay: true}
	failing := func() (llm.ModelClient, error) { return nil, errors.New("summariser is down") }
	f := newFixtureWith(t, compressing(3000), []Tool{echo},
		toolCall(t, "c1", "echo", bigArgs(600)),
		failing, // before the second request
		toolCall(t, "c2", "echo", bigArgs(10)),
		// no attempt before the third: it has barely grown
		answer(t, "done anyway"))
	run := f.admit("question")
	f.runner.Start(run.ID)
	f.waitFor(run.ID, sqlc.RunStatusCompleted)
	if n := len(f.models.reqs); n != 4 {
		t.Fatalf("%d requests, want 4 (call, failed summary, call, answer)", n)
	}
}
