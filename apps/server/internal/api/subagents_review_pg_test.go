package api

import (
	"context"
	"strings"
	"testing"

	"github.com/qunqin24/openchat/apps/server/internal/db/sqlc"
)

// Review findings on sub-agents, each reproduced: a stop, a deletion or a
// restart that left a gap.

// backgroundDone stages a conversation whose one background sub-agent has
// finished and reported.
func backgroundDone(t *testing.T, f *subFixture) (sqlc.Conversation, sqlc.Conversation) {
	t.Helper()
	f.play(func(ctx context.Context, r fakeReq) fakeTurn {
		if r.kind() == "sub" {
			return fakeTurn{Text: "REPORT " + r.last().Text}
		}
		if r.toolResults() == 0 && !strings.Contains(r.last().Text, "<subagent-report") {
			return fakeTurn{Calls: []fakeCall{launch("后台", "work", true)}}
		}
		return fakeTurn{Text: "ok"}
	})
	conv := f.newConversation()
	f.send(conv, "go")
	f.settled(conv)
	children := f.children(conv)
	if len(children) != 1 || len(f.runs(children[0])) != 1 {
		t.Fatalf("staging: %+v", children)
	}
	return conv, children[0]
}

// A foreground call that claimed its report and was cut off before its
// result was saved takes the same report again when it is replayed, rather
// than reading it as moved to the background and losing it.
func TestSubagentReplayedForegroundCallRereadsItsReport(t *testing.T) {
	f := newSubFixture(t)
	f.play(func(ctx context.Context, r fakeReq) fakeTurn {
		if r.kind() == "sub" {
			return fakeTurn{Text: "REPORT-ONCE"}
		}
		if r.toolResults() == 0 {
			return fakeTurn{Calls: []fakeCall{launch("前台", "work", false)}}
		}
		return fakeTurn{Text: "ok"}
	})
	conv := f.newConversation()
	f.send(conv, "go")
	f.settled(conv)
	child := f.children(conv)[0]
	run := f.runs(child)[0]
	// The replayed call, after the claim was committed.
	result, err := f.server.awaitSubagent(context.Background(), child, run.ID)
	if err != nil || !strings.Contains(result.Content, "REPORT-ONCE") {
		t.Fatalf("replayed call got %q, %v", result.Content, err)
	}
}

// A message waiting for a sub-agent of a deleted conversation does not start
// it again: nobody could stop that run.
func TestSubagentInboxOfADeletedConversationStartsNothing(t *testing.T) {
	f := newSubFixture(t)
	conv, child := backgroundDone(t, f)
	ctx := context.Background()
	if err := f.q.QueueSubagentMessage(ctx, sqlc.QueueSubagentMessageParams{ConversationID: child.ID, Content: "more"}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(ctx, `UPDATE conversations SET deleted_at = now() WHERE id = $1`, conv.ID); err != nil {
		t.Fatal(err)
	}
	f.server.flushSubagentInbox(ctx, child.ID)
	f.server.SweepSubagents(ctx)
	if runs := f.runs(child); len(runs) != 1 {
		t.Fatalf("a sub-agent of a deleted conversation ran again: %d runs", len(runs))
	}
}

// A stop the person made is not undone by a message of the main agent that
// read the sub-agent before the stop.
func TestSubagentStopIsNotUndoneByAMessage(t *testing.T) {
	f := newSubFixture(t)
	_, stale := backgroundDone(t, f)
	ctx := context.Background()
	if err := f.q.SetSubagentStopped(ctx, sqlc.SetSubagentStoppedParams{ID: stale.ID, Stopped: true}); err != nil {
		t.Fatal(err)
	}
	settings, err := loadSubagentSettings(ctx, f.q)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.server.messageSubagent(ctx, stale, "go on", settings); err == nil {
		t.Fatal("a message to a stopped sub-agent was taken")
	}
	if runs := f.runs(stale); len(runs) != 1 {
		t.Fatalf("a stopped sub-agent was continued: %d runs", len(runs))
	}
}

// A queued sub-agent is started once, however many starters took it from the
// queue - even when its first run has already ended.
func TestSubagentQueuedStartsOnce(t *testing.T) {
	f := newSubFixture(t)
	f.play(func(ctx context.Context, r fakeReq) fakeTurn { return fakeTurn{Text: "ok"} })
	conv := f.newConversation()
	f.send(conv, "go")
	f.settled(conv)
	ctx := context.Background()
	launching := f.runs(conv)[0]
	callID := "call-queued"
	child, err := f.q.CreateSubagentConversation(ctx, sqlc.CreateSubagentConversationParams{
		UserID: &f.user.ID, Title: "排着", Model: &f.model, ParentID: &conv.ID,
		LaunchedBy: &launching.ID, LaunchCallID: &callID, Background: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := appendAgentInput(ctx, f.q, child.ID, nil, "work"); err != nil {
		t.Fatal(err)
	}
	first, _, err := f.server.startWaiting(ctx, child)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(ctx, `UPDATE runs SET status = 'completed', finished_at = now() WHERE id = $1`, first.ID); err != nil {
		t.Fatal(err)
	}
	// A second starter, with the same snapshot of the queue.
	_, _, _ = f.server.startWaiting(ctx, child)
	if runs := f.runs(child); len(runs) != 1 {
		t.Fatalf("a queued sub-agent was started %d times", len(runs))
	}
}
