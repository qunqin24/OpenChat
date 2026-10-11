package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/qunqin/openchat/apps/server/internal/agent"
	"github.com/qunqin/openchat/apps/server/internal/db/sqlc"
)

// While the last row of a conversation is a refused message it does not go on:
// sending, regenerating and answering a question are refused (409
// revise_refused) and the way on is to reword the refused message, which is an
// edit of it. These run over the HTTP handlers with the fake moderation
// endpoint and the scripted model of the other moderation tests.

// rewording is a conversation with one exchange, then a refused message.
func rewording(t *testing.T) (*compacting, *fakeModeration, uuid.UUID) {
	t.Helper()
	b := newCompacting(t)
	f := blockedBy(t, b.askHarness)
	if rec := b.patchModeration(`{"strikes":0}`); rec.Code != http.StatusOK { // no sanction in the way
		t.Fatal(rec.Body)
	}
	b.send("q1")
	return b, f, b.blockedWith("BLOCKME")
}

// A send past a refused message is refused before anything is checked,
// written, counted or sent.
func TestSendIsRefusedWhileTheLastMessageIsRefused(t *testing.T) {
	b, f, _ := rewording(t)
	rows := b.count(`SELECT count(*) FROM messages WHERE conversation_id = $1`, b.conv.ID)
	runs := b.count(`SELECT count(*) FROM runs WHERE conversation_id = $1`, b.conv.ID)
	events := b.count(`SELECT count(*) FROM moderation_events WHERE user_id = $1`, b.user.ID)
	checked, asked := f.calls(), b.requests()

	// Clean words and refusable words alike: the answer does not depend on them.
	for _, content := range []string{"a perfectly fine question", "BLOCKME again"} {
		b.refused(b.trySend(content), http.StatusConflict, "revise_refused")
	}

	if got := b.count(`SELECT count(*) FROM messages WHERE conversation_id = $1`, b.conv.ID); got != rows {
		t.Errorf("a refused send wrote %d rows", got-rows)
	}
	if got := b.count(`SELECT count(*) FROM runs WHERE conversation_id = $1`, b.conv.ID); got != runs {
		t.Errorf("a refused send started %d runs", got-runs)
	}
	if got := b.count(`SELECT count(*) FROM moderation_events WHERE user_id = $1`, b.user.ID); got != events {
		t.Errorf("a refused send was logged as a strike (%d events more)", got-events)
	}
	if f.calls() != checked {
		t.Error("a refused send was sent to the moderation service")
	}
	if b.requests() != asked {
		t.Error("a refused send reached the model")
	}
	same(t, "shown", b.shown(), []string{"user:q1", "assistant:re: q1", "user:BLOCKME [blocked]"})
}

// A regeneration past a refused message is refused, and deletes nothing.
func TestRegenerateIsRefusedWhileTheLastMessageIsRefused(t *testing.T) {
	b, _, _ := rewording(t)
	runs := b.count(`SELECT count(*) FROM runs WHERE conversation_id = $1`, b.conv.ID)
	b.refused(b.tryRegenerate(), http.StatusConflict, "revise_refused")
	same(t, "shown", b.shown(), []string{"user:q1", "assistant:re: q1", "user:BLOCKME [blocked]"})
	if got := b.count(`SELECT count(*) FROM runs WHERE conversation_id = $1`, b.conv.ID); got != runs {
		t.Errorf("a refused regeneration started %d runs", got-runs)
	}
}

// Rewording the refused message that passes: it and its card are gone, the new
// words are the question, and a run answers it. The other tabs are told which
// row went.
func TestRewordingARefusedMessage(t *testing.T) {
	b, _, blocked := rewording(t)
	sub := b.server.runner.Hub().Subscribe(b.conv.ID)
	defer sub.Close()

	edited, status := b.edit(blocked, "q2")
	if status != sqlc.RunStatusCompleted {
		t.Fatalf("run ended %s", status)
	}
	same(t, "shown", b.shown(), []string{"user:q1", "assistant:re: q1", "user:q2", "assistant:re: q2"})
	same(t, "context", b.lastHistory(), []string{"user:q1", "assistant:re: q1", "user:q2"})
	if edited.Message.Status == MessageStatusBlocked || edited.Message.Content != "q2" {
		t.Errorf("the new message is %+v", edited.Message)
	}
	if n := b.count(`SELECT count(*) FROM messages WHERE id = $1`, blocked); n != 0 {
		t.Error("the refused message survived being reworded")
	}

	batches, _ := sub.Drain()
	var told bool
	for _, batch := range batches {
		for _, event := range batch.Events {
			if changed, ok := event.(agent.TranscriptChanged); ok && changed.From == blocked {
				told = true
			}
		}
	}
	if !told {
		t.Errorf("the other tabs were not told which row went: %+v", batches)
	}

	// And the conversation goes on as any.
	b.send("q3")
	same(t, "shown", b.shown(), []string{
		"user:q1", "assistant:re: q1", "user:q2", "assistant:re: q2", "user:q3", "assistant:re: q3",
	})
}

// Refused again, a refused message that is the last row is replaced by the
// words that were last tried; one with rows after it (old data) is left alone.
func TestRewordingARefusedMessageThatIsRefusedAgain(t *testing.T) {
	t.Run("the last row", func(t *testing.T) {
		b, _, blocked := rewording(t)
		sub := b.server.runner.Hub().Subscribe(b.conv.ID)
		defer sub.Close()

		rec := b.tryEdit(blocked, "BLOCKME second try")
		refused(t, rec)
		again := messageIDOf(t, rec)
		same(t, "shown", b.shown(), []string{"user:q1", "assistant:re: q1", "user:BLOCKME second try [blocked]"})
		if b.count(`SELECT count(*) FROM messages WHERE id = $1 AND status = 'blocked'`, again) != 1 {
			t.Error("the id the refusal names is not the refused message")
		}
		if b.count(`SELECT count(*) FROM messages WHERE id = $1`, blocked) != 0 {
			t.Error("the old refused message was kept beside the new")
		}
		if b.count(`SELECT count(*) FROM runs WHERE conversation_id = $1 AND status = 'running'`, b.conv.ID) != 0 {
			t.Error("a refused edit started a run")
		}
		// The model was not sent either, and the conversation still stops there.
		for n := 1; n <= b.requests(); n++ {
			if strings.Contains(b.body(n), "BLOCKME") {
				t.Errorf("request %d carried the refused words", n)
			}
		}
		b.refused(b.trySend("fine"), http.StatusConflict, "revise_refused")

		batches, _ := sub.Drain()
		if len(batches) == 0 {
			t.Error("the other tabs were not told the refused message was replaced")
		}
	})

	t.Run("not the last row", func(t *testing.T) {
		b, _, blocked := rewording(t)
		// Old data: something written after the refused message.
		b.appendRow(sqlc.MessageRoleUser, sqlc.MessageStatusComplete, "written after it")
		rec := b.tryEdit(blocked, "BLOCKME second try")
		refused(t, rec)
		if strings.Contains(rec.Body.String(), "messageId") {
			t.Errorf("nothing was kept, yet the refusal names a message: %s", rec.Body)
		}
		same(t, "shown", b.shown(), []string{"user:q1", "assistant:re: q1", "user:BLOCKME [blocked]", "user:written after it"})
	})
}

// An earlier question can be reworded while a refused message ends the
// conversation: that deletes the refused message with everything after the
// question, and is another way on.
func TestEditingAnEarlierQuestionPastARefusedMessage(t *testing.T) {
	b, _, _ := rewording(t)
	first := b.transcript()[0].Id
	edited, status := b.edit(first, "q1 reworded")
	if status != sqlc.RunStatusCompleted {
		t.Fatalf("run ended %s", status)
	}
	same(t, "shown", b.shown(), []string{"user:q1 reworded", "assistant:re: q1 reworded"})
	if edited.Message.Status == MessageStatusBlocked {
		t.Error("the new wording is a refused message")
	}
}

// The files in the composer go with the reworded message: claimed by it, and
// left alone when the words are refused again.
func TestRewordingARefusedMessageTakesFiles(t *testing.T) {
	b, _, blocked := rewording(t)
	blobs := &memoryStore{blobs: map[string][]byte{}}
	b.server.store = blobs
	if _, err := b.q.SaveModelOverride(context.Background(), sqlc.SaveModelOverrideParams{ModelID: b.model, InputModalities: []string{"image"}}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = b.pool.Exec(context.Background(), "DELETE FROM model_overrides WHERE model_id = $1", b.model)
	})
	upload := func(data string) uuid.UUID {
		t.Helper()
		rec := httptest.NewRecorder()
		b.server.CreateUpload(rec, b.request(http.MethodPost, fmt.Sprintf(`{"filename":"x.png","contentType":"image/png","size":%d}`, len(data))))
		var up Upload
		if rec.Code != http.StatusCreated || json.Unmarshal(rec.Body.Bytes(), &up) != nil {
			t.Fatalf("upload: %d %s", rec.Code, rec.Body)
		}
		var key string
		if err := b.pool.QueryRow(context.Background(), `SELECT storage_key FROM attachments WHERE id = $1`, up.Id).Scan(&key); err != nil {
			t.Fatal(err)
		}
		if err := blobs.PutObject(context.Background(), key, "image/png", []byte(data)); err != nil {
			t.Fatal(err)
		}
		return up.Id
	}
	editWith := func(id uuid.UUID, content string, files ...uuid.UUID) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		list, _ := json.Marshal(files)
		b.server.EditMessage(rec, b.request(http.MethodPost,
			fmt.Sprintf(`{"content":%q,"model":%q,"attachmentIds":%s}`, content, b.model, list)), b.conv.ID, id)
		return rec
	}

	file := upload("\x89PNG fine")
	// Refused again: the file stays the composer's.
	rec := editWith(blocked, "BLOCKME with a file", file)
	refused(t, rec)
	blocked = messageIDOf(t, rec)
	if b.count(`SELECT count(*) FROM attachments WHERE id = $1 AND message_id IS NULL`, file) != 1 {
		t.Error("a refused edit claimed the file")
	}

	// Passed: the new question carries it.
	var out CreateMessageResponse
	b.accepted(editWith(blocked, "what is this?", file), &out)
	b.settle(out.Run.Id)
	if b.count(`SELECT count(*) FROM attachments WHERE id = $1 AND message_id = $2`, file, out.Message.Id) != 1 {
		t.Error("the reworded message did not claim the file")
	}
	if out.Message.Attachments == nil || len(*out.Message.Attachments) != 1 {
		t.Errorf("the response carries %v", out.Message.Attachments)
	}

	// Any other message keeps its own and takes none.
	other := upload("\x89PNG other")
	b.refused(editWith(out.Message.Id, "reworded", other), http.StatusBadRequest, "attachments_not_editable")
	if b.count(`SELECT count(*) FROM attachments WHERE id = $1 AND message_id IS NULL`, other) != 1 {
		t.Error("a refused edit claimed a file")
	}
}

// A send racing the edit that rewords the refused message leaves a consistent
// transcript: never a message written after the refused one, never a refused
// message left beside its replacement, and the reworded question once.
func TestSendRacingTheRewordingOfARefusedMessage(t *testing.T) {
	for round := 0; round < 6; round++ {
		b, _, blocked := rewording(t)
		var wg sync.WaitGroup
		var edit, send *httptest.ResponseRecorder
		wg.Add(2)
		go func() {
			defer wg.Done()
			edit = b.tryEdit(blocked, "reworded")
		}()
		go func() {
			defer wg.Done()
			send = b.trySend("racing question")
		}()
		wg.Wait()

		if edit.Code != http.StatusAccepted {
			t.Fatalf("round %d: the edit: %d %s", round, edit.Code, edit.Body)
		}
		// Sent first: refused for the refused message. Sent after the edit began:
		// the edit's run is going (busy), or it already ended and the send is
		// the next question. Nothing else is possible.
		switch {
		case send.Code == http.StatusConflict && (strings.Contains(send.Body.String(), "revise_refused") || strings.Contains(send.Body.String(), "conversation_busy")):
		case send.Code == http.StatusAccepted:
		default:
			t.Fatalf("round %d: the send: %d %s", round, send.Code, send.Body)
		}
		var run CreateMessageResponse
		_ = json.Unmarshal(edit.Body.Bytes(), &run)
		b.settle(run.Run.Id)
		if send.Code == http.StatusAccepted {
			var sent CreateMessageResponse
			_ = json.Unmarshal(send.Body.Bytes(), &sent)
			b.settle(sent.Run.Id)
		}

		if n := b.count(`SELECT count(*) FROM messages WHERE conversation_id = $1 AND status = 'blocked'`, b.conv.ID); n != 0 {
			t.Fatalf("round %d: %d refused messages left: %v", round, n, b.shown())
		}
		if n := b.count(`SELECT count(*) FROM messages WHERE conversation_id = $1 AND content = 'reworded'`, b.conv.ID); n != 1 {
			t.Fatalf("round %d: the reworded question is there %d times: %v", round, n, b.shown())
		}
		got := b.shown()
		want := []string{"user:q1", "assistant:re: q1", "user:reworded", "assistant:re: reworded"}
		if send.Code == http.StatusAccepted {
			want = append(want, "user:racing question", "assistant:re: racing question")
		}
		same(t, fmt.Sprintf("round %d shown", round), got, want)
	}
}

// Two sends that are both refused, racing, leave one refused message: the second
// does not go after the first.
func TestTwoRefusedSendsLeaveOneRefusedMessage(t *testing.T) {
	b := newBlocking(t)
	if rec := b.patchModeration(`{"strikes":0}`); rec.Code != http.StatusOK {
		t.Fatal(rec.Body)
	}
	b.send("q1")
	var wg sync.WaitGroup
	codes := make([]int, 2)
	for i := range codes {
		wg.Add(1)
		go func() {
			defer wg.Done()
			codes[i] = b.trySend(fmt.Sprintf("BLOCKME %d", i)).Code
		}()
	}
	wg.Wait()
	for _, code := range codes {
		if code != http.StatusUnprocessableEntity && code != http.StatusConflict {
			t.Fatalf("codes %v", codes)
		}
	}
	if n := b.count(`SELECT count(*) FROM messages WHERE conversation_id = $1 AND status = 'blocked'`, b.conv.ID); n != 1 {
		t.Errorf("%d refused messages after two refused sends, want 1: %v", n, b.shown())
	}
}
