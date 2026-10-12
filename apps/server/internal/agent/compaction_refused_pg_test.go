package agent

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/qunqin/openchat/apps/server/internal/db/sqlc"
	"github.com/qunqin/openchat/apps/server/internal/llm"
)

// What a run does when the model refuses a request for its length although the
// estimate said it fitted: it compresses once, whatever the estimate, and asks
// again at once (recoverContextLength). Same scratch database as the other
// agent tests.

// tooLong is a model that refuses the request for its length.
func tooLong() (llm.ModelClient, error) {
	return nil, &llm.Error{Kind: llm.ErrorContextLength, Message: "maximum context length exceeded"}
}

// compressionEvents counts the live compression events of a run's batches, and
// the retries it scheduled.
func (f *fixture) compressionEvents() (started int, finished []bool, retries int) {
	for _, b := range f.events() {
		for _, e := range b.Events {
			switch e := e.(type) {
			case CompactionStarted:
				started++
			case CompactionFinished:
				finished = append(finished, e.Summary != nil)
			case RetryScheduled:
				retries++
			}
		}
	}
	return started, finished, retries
}

// A request the model refuses for its length, well under the compression
// threshold by the estimate: the run compresses once, whatever the estimate
// said, and asks again at once - the answer is the third request's.
func TestRunCompressesAndRetriesWhenTheModelRefusesTheLength(t *testing.T) {
	f := newFixtureWith(t, compressing(100_000), nil,
		tooLong,
		answer(t, "- they talked about tea"), // the summary
		answer(t, "all done"))
	f.row(sqlc.MessageRoleUser, "first question")
	f.row(sqlc.MessageRoleAssistant, "first answer", StopStop)
	run := f.admit("question")
	start := time.Now()
	f.runner.Start(run.ID)
	f.waitFor(run.ID, sqlc.RunStatusCompleted)
	if elapsed := time.Since(start); elapsed >= time.Second {
		t.Errorf("the retry waited %v: there is no backoff after a refusal for length", elapsed)
	}

	if n := len(f.models.reqs); n != 3 {
		t.Fatalf("%d requests, want 3 (refused, summary, answer)", n)
	}
	// The first request was the whole conversation; the summariser is the
	// second; the retry is built from the summary.
	equalStrings(t, "refused request", roles(f.models.reqs[0].Messages),
		[]string{"system:SYS", "user:first question", "assistant:first answer", "user:question"})
	if f.models.reqs[1].Messages[0].Text() != systemPromptSummary {
		t.Fatalf("second request is not the summariser's: %q", f.models.reqs[1].Messages[0].Text())
	}
	equalStrings(t, "retried request", roles(f.models.reqs[2].Messages),
		[]string{"system:SYS", "user:<compressed_history_summ", "user:question"})
	if !strings.Contains(f.models.reqs[2].Messages[1].Text(), "they talked about tea") {
		t.Error("the retry is not sent the summary the model wrote")
	}

	s := f.summary()
	if s == nil || s.Content != "- they talked about tea" || s.KeptID == nil {
		t.Fatalf("summary %+v", s)
	}
	// The refusal leaves no row of its own: the transcript is as without it.
	f.assertTranscript("user first question", "assistant:stop first answer", "user question", "assistant:stop all done")

	// Told like an automatic compression, and no backoff announced.
	started, finished, retries := f.compressionEvents()
	if started != 1 || len(finished) != 1 || !finished[0] || retries != 0 {
		t.Errorf("events: %d started, finished with a summary %v, %d retries scheduled", started, finished, retries)
	}
	if got, _ := f.q.GetRun(context.Background(), run.ID); len(got.Live) > 0 && strings.Contains(string(got.Live), "compacting") {
		t.Errorf("live state still says compacting: %s", got.Live)
	}
	// In the ledger: the refused request as an error, the compression, the answer.
	kinds := map[string]int{}
	for _, r := range f.ledger(run.ID) {
		kinds[r.kind+":"+r.status]++
	}
	if kinds["chat:error"] != 1 || kinds["chat:ok"] != 1 || kinds["compression:ok"] != 1 {
		t.Errorf("ledger = %v", kinds)
	}
}

// Once per run: the retry refused again ends the run as it always did, and no
// second compression is paid for.
func TestRunCompressesOnlyOnceForARefusedLength(t *testing.T) {
	f := newFixtureWith(t, compressing(100_000), nil,
		tooLong,
		answer(t, "- summary"),
		tooLong)
	f.row(sqlc.MessageRoleUser, "first question")
	f.row(sqlc.MessageRoleAssistant, "first answer", StopStop)
	run := f.admit("question")
	f.runner.Start(run.ID)
	failed := f.waitFor(run.ID, sqlc.RunStatusFailed)
	if failed.ErrorCode == nil || *failed.ErrorCode != CodeContextTooLong {
		t.Fatalf("error code %v, want %s", failed.ErrorCode, CodeContextTooLong)
	}
	if n := len(f.models.reqs); n != 3 {
		t.Fatalf("%d requests, want 3 (refused, summary, refused again)", n)
	}
	started, _, retries := f.compressionEvents()
	if started != 1 || retries != 0 {
		t.Errorf("%d compressions started, %d retries scheduled", started, retries)
	}
}

// A compression that writes nothing - the summariser fails - leaves the refusal
// to end the run, as without compression.
func TestRunFailsWhenTheCompressionAfterARefusedLengthFails(t *testing.T) {
	f := newFixtureWith(t, compressing(100_000), nil,
		tooLong,
		func() (llm.ModelClient, error) { return nil, errors.New("summariser is down") })
	f.row(sqlc.MessageRoleUser, "first question")
	f.row(sqlc.MessageRoleAssistant, "first answer", StopStop)
	run := f.admit("question")
	f.runner.Start(run.ID)
	failed := f.waitFor(run.ID, sqlc.RunStatusFailed)
	if failed.ErrorCode == nil || *failed.ErrorCode != CodeContextTooLong {
		t.Fatalf("error code %v, want %s", failed.ErrorCode, CodeContextTooLong)
	}
	if n := len(f.models.reqs); n != 2 {
		t.Fatalf("%d requests, want 2 (refused, failed summary)", n)
	}
	if f.summary() != nil {
		t.Error("a failed compression wrote a summary")
	}
	started, finished, _ := f.compressionEvents()
	if started != 1 || len(finished) != 1 || finished[0] {
		t.Errorf("events: %d started, finished %v: want one that wrote nothing", started, finished)
	}
}

// Nothing to fold in (the question alone is too long): no summariser is paid
// for, and the run fails with the refusal.
func TestRunFailsWhenThereIsNothingToCompressAfterARefusedLength(t *testing.T) {
	f := newFixtureWith(t, compressing(100_000), nil, tooLong)
	run := f.admit("question")
	f.runner.Start(run.ID)
	failed := f.waitFor(run.ID, sqlc.RunStatusFailed)
	if failed.ErrorCode == nil || *failed.ErrorCode != CodeContextTooLong {
		t.Fatalf("error code %v, want %s", failed.ErrorCode, CodeContextTooLong)
	}
	if n := len(f.models.reqs); n != 1 {
		t.Fatalf("%d requests, want only the refused one", n)
	}
	if f.summary() != nil {
		t.Error("a summary was written for nothing")
	}
}

// With the administrator's switch off nothing is compressed, not even to
// recover: the run fails with the refusal as before.
func TestRunDoesNotCompressForARefusedLengthWhenSwitchedOff(t *testing.T) {
	f := newFixtureWith(t, func(o *Options) {
		compressing(100_000)(o)
		o.Compression.Auto = func(context.Context) bool { return false }
	}, nil, tooLong, answer(t, "never asked"))
	f.row(sqlc.MessageRoleUser, "first question")
	f.row(sqlc.MessageRoleAssistant, "first answer", StopStop)
	run := f.admit("question")
	f.runner.Start(run.ID)
	failed := f.waitFor(run.ID, sqlc.RunStatusFailed)
	if failed.ErrorCode == nil || *failed.ErrorCode != CodeContextTooLong {
		t.Fatalf("error code %v, want %s", failed.ErrorCode, CodeContextTooLong)
	}
	if n := len(f.models.reqs); n != 1 {
		t.Fatalf("%d requests, want only the refused one", n)
	}
	if f.summary() != nil {
		t.Error("a summary was written with compression off")
	}
	if started, finished, _ := f.compressionEvents(); started != 0 || len(finished) != 0 {
		t.Errorf("compression events with the switch off: %d started, %v finished", started, finished)
	}
}
