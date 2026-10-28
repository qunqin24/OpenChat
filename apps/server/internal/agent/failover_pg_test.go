package agent

import (
	"context"
	"slices"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/qunqin24/openchat/apps/server/internal/db/sqlc"
	"github.com/qunqin24/openchat/apps/server/internal/llm"
)

// failingOver is the scripted model behind several channels: every request is
// the next step whichever channel it goes to, and NextChannel hands out the
// channels in the order given, as the serving order would.
type failingOver struct {
	*scripted
	channels []uuid.UUID
	mu       sync.Mutex
	asked    [][]uuid.UUID
}

func (f *failingOver) NextChannel(_ context.Context, _ string, tried []uuid.UUID) (*uuid.UUID, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.asked = append(f.asked, slices.Clone(tried))
	for _, id := range f.channels {
		if !slices.Contains(tried, id) {
			return &id, nil
		}
	}
	return nil, nil
}

// newFailoverFixture is a fixture whose model is offered by `count` channels,
// with a run admitted on the first.
func newFailoverFixture(t *testing.T, count int, steps ...func() (llm.ModelClient, error)) (*fixture, *failingOver, sqlc.Run) {
	t.Helper()
	f := newFixture(t, nil)
	models := &failingOver{scripted: &scripted{steps: steps}}
	for i := 0; i < count; i++ {
		channel, err := f.q.CreateChannel(context.Background(), sqlc.CreateChannelParams{
			Name: "failover-" + uuid.NewString(), Type: sqlc.ChannelTypeOpenai, Enabled: true, Weight: 1,
		})
		if err != nil {
			t.Fatal(err)
		}
		models.channels = append(models.channels, channel.ID)
	}
	f.runner = NewRunner(f.pool, f.runner.hub, models, func(context.Context) []Tool { return nil },
		Options{PartialInterval: 5_000_000, MaxRetries: 2})
	t.Cleanup(func() { _ = f.runner.Shutdown(context.Background()) })

	input := f.appendInput("hi")
	run, err := f.q.CreateRun(context.Background(), sqlc.CreateRunParams{
		ConversationID: f.conv, UserID: f.user, InputMessageID: input.ID, ModelID: "m",
		ChannelID: &models.channels[0], WebSearch: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	return f, models, run
}

func failWith(kind llm.ErrorKind) func() (llm.ModelClient, error) {
	return func() (llm.ModelClient, error) { return nil, &llm.Error{Kind: kind, Message: "channel failed"} }
}

// A key that stopped working on one channel is no reason to fail the answer
// when another channel offers the model: the request is made again there, at
// once, and the run stays on it.
func TestRunFailsOverToAnotherChannel(t *testing.T) {
	f, models, run := newFailoverFixture(t, 2, failWith(llm.ErrorAuth), answer(t, "from the second channel"))
	f.runner.Start(run.ID)
	finished := f.waitFor(run.ID, sqlc.RunStatusCompleted)

	f.assertTranscript("user hi", "assistant:stop from the second channel")
	if finished.ChannelID == nil || *finished.ChannelID != models.channels[1] {
		t.Fatalf("the run is pinned to %v, want the second channel %v", finished.ChannelID, models.channels[1])
	}
	if n := models.calls.Load(); n != 2 {
		t.Fatalf("%d requests, want 2", n)
	}
}

// With every channel failed, the failure is what it always was: an auth error
// ends the run, after each channel was tried once.
func TestRunFailsWhenEveryChannelFails(t *testing.T) {
	f, models, run := newFailoverFixture(t, 2, failWith(llm.ErrorAuth), failWith(llm.ErrorAuth))
	f.runner.Start(run.ID)
	finished := f.waitFor(run.ID, sqlc.RunStatusFailed)

	if finished.ErrorCode == nil || *finished.ErrorCode != CodeModelAuth {
		t.Fatalf("error code %v", finished.ErrorCode)
	}
	if n := models.calls.Load(); n != 2 {
		t.Fatalf("%d requests, want one per channel", n)
	}
}

// A context too long for the model is too long on every channel of it.
func TestRunDoesNotFailOverForTheContextLength(t *testing.T) {
	f, models, run := newFailoverFixture(t, 2, failWith(llm.ErrorContextLength))
	f.runner.Start(run.ID)
	f.waitFor(run.ID, sqlc.RunStatusFailed)
	if len(models.asked) != 0 {
		t.Fatalf("asked for another channel %d times", len(models.asked))
	}
}

// halfAnswer writes something and then fails, as a connection dropped
// mid-answer does.
type halfAnswer struct{}

func (halfAnswer) Stream(context.Context, llm.Request, func(llm.Event) error) (llm.ModelResponse, error) {
	return llm.ModelResponse{Message: llm.Message{Role: llm.RoleAssistant, Parts: []llm.Part{llm.TextPart{Text: "half"}}}},
		&llm.Error{Kind: llm.ErrorTransport, Message: "connection reset"}
}

// A request that has already written part of the answer stays on its channel
// and is retried there: another channel would start the answer over.
func TestRunDoesNotFailOverOnceTheModelHasWritten(t *testing.T) {
	f, models, run := newFailoverFixture(t, 2,
		func() (llm.ModelClient, error) { return halfAnswer{}, nil },
		answer(t, "whole"))
	f.runner.Start(run.ID)
	finished := f.waitFor(run.ID, sqlc.RunStatusCompleted)
	if len(models.asked) != 0 {
		t.Fatalf("asked for another channel %d times", len(models.asked))
	}
	if finished.ChannelID == nil || *finished.ChannelID != models.channels[0] {
		t.Fatalf("the run moved to %v", finished.ChannelID)
	}
}
