package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/qunqin/openchat/apps/server/internal/agent"
	"github.com/qunqin/openchat/apps/server/internal/db/sqlc"
	"github.com/qunqin/openchat/apps/server/internal/storage"
)

// Editing a question and regenerating an answer end to end, over the HTTP
// handlers and a scripted model that answers "re: <the last thing it was
// asked>" and keeps every request it was sent, so what a model is shown after a
// redo can be read back. What is replaced is gone: the tests look at the
// transcript, at the rows, and at what the model is sent. Needs the same
// scratch database as the other agent tests.

// redoing is a conversation whose model echoes what it was last asked.
type redoing struct {
	*askHarness
}

func newRedoing(t *testing.T) *redoing {
	t.Helper()
	var b redoing
	b.askHarness = newAskHarness(t, openAIWire, func(n int) string {
		history := b.history(n)
		last := history[len(history)-1]
		// A question that says "ask" makes the model ask one back.
		if strings.HasPrefix(last, "user:") && strings.Contains(last, "ask") {
			return openAIWire.ask(fmt.Sprintf("call_%d", n), questionsJSON)
		}
		return openAIWire.text("re: " + strings.TrimPrefix(last, "user:"))
	})
	return &b
}

var dateStamp = regexp.MustCompile(`\[Date: [0-9-]+\]`)

// wording is a request message's text, without the date stamp the first
// message of a day carries.
func wording(content json.RawMessage) string {
	var text string
	if json.Unmarshal(content, &text) != nil {
		var parts []struct {
			Text string `json:"text"`
		}
		_ = json.Unmarshal(content, &parts)
		for _, p := range parts {
			text += p.Text
		}
	}
	return dateStamp.ReplaceAllString(text, "")
}

// history is the nth request's conversation as role:text, without the system
// prompt.
func (b *redoing) history(n int) []string {
	b.t.Helper()
	var req struct {
		Messages []struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal([]byte(b.body(n)), &req); err != nil {
		b.t.Fatalf("request body: %v", err)
	}
	var out []string
	for _, m := range req.Messages {
		if m.Role != "system" {
			out = append(out, m.Role+":"+wording(m.Content))
		}
	}
	return out
}

// lastHistory is what the model was sent most recently.
func (b *redoing) lastHistory() []string {
	b.mu.Lock()
	n := len(b.bodies)
	b.mu.Unlock()
	return b.history(n)
}

// path is the transcript, as role:text.
func (b *redoing) path() []string {
	b.t.Helper()
	var out []string
	for _, m := range b.transcript() {
		out = append(out, string(m.Role)+":"+m.Content)
	}
	return out
}

func (b *redoing) edit(id uuid.UUID, content string) (CreateMessageResponse, sqlc.RunStatus) {
	b.t.Helper()
	rec := httptest.NewRecorder()
	b.server.EditMessage(rec, b.request(http.MethodPost, fmt.Sprintf(`{"content":%q,"model":%q}`, content, b.model)), b.conv.ID, id)
	var out CreateMessageResponse
	b.accepted(rec, &out)
	return out, b.settle(out.Run.Id)
}

func (b *redoing) regenerate() (CreateMessageResponse, sqlc.RunStatus) {
	b.t.Helper()
	rec := httptest.NewRecorder()
	b.server.RegenerateAnswer(rec, b.request(http.MethodPost, fmt.Sprintf(`{"model":%q}`, b.model)), b.conv.ID)
	var out CreateMessageResponse
	b.accepted(rec, &out)
	return out, b.settle(out.Run.Id)
}

func (b *redoing) refused(rec *httptest.ResponseRecorder, status int, code string) {
	b.t.Helper()
	if rec.Code != status || !strings.Contains(rec.Body.String(), code) {
		b.t.Fatalf("got %d %s, want %d %s", rec.Code, rec.Body, status, code)
	}
}

// count is how many rows a query counts, for "is it really gone".
func (b *redoing) count(query string, args ...any) int {
	b.t.Helper()
	var n int
	if err := b.pool.QueryRow(context.Background(), query, args...).Scan(&n); err != nil {
		b.t.Fatalf("%s: %v", query, err)
	}
	return n
}

func (b *redoing) exists(id uuid.UUID) bool {
	b.t.Helper()
	_, err := b.q.GetMessage(context.Background(), id)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		b.t.Fatal(err)
	}
	return err == nil
}

func same(t *testing.T, what string, got, want []string) {
	t.Helper()
	equal(t, what, got, want)
}

// Editing any question - not only the last - deletes it and everything after
// it, and the new wording goes on the end: the model is asked with what came
// before and nothing of what was removed, and the conversation goes on from
// there.
func TestEditingAQuestionDeletesItAndEverythingAfter(t *testing.T) {
	b := newRedoing(t)
	sent := map[string]CreateMessageResponse{}
	for _, q := range []string{"q1", "q2", "q3", "q4"} {
		sent[q], _ = b.send(q)
	}
	same(t, "four turns", b.path(), []string{
		"user:q1", "assistant:re: q1", "user:q2", "assistant:re: q2",
		"user:q3", "assistant:re: q3", "user:q4", "assistant:re: q4"})
	oldRuns := []uuid.UUID{sent["q2"].Run.Id, sent["q3"].Run.Id, sent["q4"].Run.Id}

	_, status := b.edit(sent["q2"].Message.Id, "q2b")
	if status != sqlc.RunStatusCompleted {
		t.Fatalf("edit's run ended %s", status)
	}
	same(t, "the model is asked with what came before", b.lastHistory(), []string{"user:q1", "assistant:re: q1", "user:q2b"})
	same(t, "after editing", b.path(), []string{"user:q1", "assistant:re: q1", "user:q2b", "assistant:re: q2b"})

	// Gone for good: the questions, their runs, and every row they wrote.
	for _, q := range []string{"q2", "q3", "q4"} {
		if b.exists(sent[q].Message.Id) {
			t.Errorf("%s is still in the database", q)
		}
	}
	if n := b.count(`SELECT count(*) FROM runs WHERE id = ANY ($1)`, oldRuns); n != 0 {
		t.Errorf("%d of the deleted questions' runs remain", n)
	}
	if n := b.count(`SELECT count(*) FROM messages WHERE conversation_id = $1`, b.conv.ID); n != 4 {
		t.Errorf("%d rows in the conversation, want 4", n)
	}
	if n := b.count(`SELECT count(*) FROM runs WHERE conversation_id = $1`, b.conv.ID); n != 2 {
		t.Errorf("%d runs in the conversation, want 2", n)
	}

	// The conversation goes on from the new question.
	b.send("q3b")
	same(t, "context after the edit", b.lastHistory(),
		[]string{"user:q1", "assistant:re: q1", "user:q2b", "assistant:re: q2b", "user:q3b"})

	// Editing the first question empties the conversation but for itself.
	b.edit(sent["q1"].Message.Id, "q1b")
	same(t, "a root's edit", b.path(), []string{"user:q1b", "assistant:re: q1b"})
	if n := b.count(`SELECT count(*) FROM messages WHERE conversation_id = $1`, b.conv.ID); n != 2 {
		t.Errorf("%d rows left after editing the first question, want 2", n)
	}
}

// Editing the last question replaces just it and its answer.
func TestEditingTheLastQuestion(t *testing.T) {
	b := newRedoing(t)
	b.send("q1")
	q2, _ := b.send("q2")
	b.edit(q2.Message.Id, "q2b")
	same(t, "after editing", b.path(), []string{"user:q1", "assistant:re: q1", "user:q2b", "assistant:re: q2b"})
	same(t, "context", b.lastHistory(), []string{"user:q1", "assistant:re: q1", "user:q2b"})
	if b.exists(q2.Message.Id) {
		t.Error("the replaced question is still there")
	}
}

// Regenerating deletes the old answer: nothing is kept to go back to, and the
// model is asked again without it.
func TestRegenerateDeletesTheOldAnswer(t *testing.T) {
	b := newRedoing(t)
	b.send("q1")
	q2, _ := b.send("q2")
	oldAnswer := b.transcript()[3].Id

	again, status := b.regenerate()
	if status != sqlc.RunStatusCompleted {
		t.Fatalf("regenerate's run ended %s", status)
	}
	if again.Message.Id != q2.Message.Id {
		t.Fatalf("regenerated %s, want %s", again.Message.Id, q2.Message.Id)
	}
	same(t, "regenerate's context", b.lastHistory(), []string{"user:q1", "assistant:re: q1", "user:q2"})
	same(t, "one answer", b.path(), []string{"user:q1", "assistant:re: q1", "user:q2", "assistant:re: q2"})
	if b.exists(oldAnswer) {
		t.Error("the old answer is still in the database")
	}
	if n := b.count(`SELECT count(*) FROM runs WHERE id = $1`, q2.Run.Id); n != 0 {
		t.Error("the old answer's run is still in the database")
	}
	if n := b.count(`SELECT count(*) FROM runs WHERE input_message_id = $1`, q2.Message.Id); n != 1 {
		t.Errorf("%d runs answer the question, want 1", n)
	}
	if n := b.count(`SELECT count(*) FROM messages WHERE conversation_id = $1`, b.conv.ID); n != 4 {
		t.Errorf("%d rows in the conversation, want 4", n)
	}
	// The message that came before is untouched, and the next one builds on the new answer.
	b.send("q3")
	same(t, "next context", b.lastHistory(), []string{"user:q1", "assistant:re: q1", "user:q2", "assistant:re: q2", "user:q3"})

	// A retry of the same regenerate finds its run instead of deleting again.
	rec := httptest.NewRecorder()
	b.server.RegenerateAnswer(rec, b.request(http.MethodPost, fmt.Sprintf(`{"model":%q,"requestId":"r-1"}`, b.model)), b.conv.ID)
	var first CreateMessageResponse
	b.accepted(rec, &first)
	b.settle(first.Run.Id)
	rec = httptest.NewRecorder()
	b.server.RegenerateAnswer(rec, b.request(http.MethodPost, fmt.Sprintf(`{"model":%q,"requestId":"r-1"}`, b.model)), b.conv.ID)
	var retried CreateMessageResponse
	b.accepted(rec, &retried)
	if retried.Run.Id != first.Run.Id {
		t.Fatalf("retried regenerate started run %s, want %s", retried.Run.Id, first.Run.Id)
	}
}

// What cannot be redone is refused with its own reason, and nothing is deleted
// by a refusal.
func TestRedoRefusals(t *testing.T) {
	b := newRedoing(t)

	// Nothing to answer in an empty conversation.
	rec := httptest.NewRecorder()
	b.server.RegenerateAnswer(rec, b.request(http.MethodPost, `{}`), b.conv.ID)
	b.refused(rec, http.StatusNotFound, "nothing_to_answer")

	q1, _ := b.send("q1")
	q2, _ := b.send("q2")
	post := func(content string) *http.Request {
		return b.request(http.MethodPost, fmt.Sprintf(`{"content":%q}`, content))
	}
	// An answer is not a question.
	answer := b.transcript()[1]
	rec = httptest.NewRecorder()
	b.server.EditMessage(rec, post("again"), b.conv.ID, answer.Id)
	b.refused(rec, http.StatusNotFound, "message_not_found")
	rec = httptest.NewRecorder()
	b.server.EditMessage(rec, post("again"), b.conv.ID, uuid.New())
	b.refused(rec, http.StatusNotFound, "message_not_found")
	rec = httptest.NewRecorder()
	b.server.EditMessage(rec, post("  "), b.conv.ID, q1.Message.Id)
	b.refused(rec, http.StatusBadRequest, "empty_content")

	// Nothing is redone while a run is going: it would be deleted from under
	// itself.
	if _, err := b.q.CreateRun(context.Background(), sqlc.CreateRunParams{
		ConversationID: b.conv.ID, UserID: b.user.ID, InputMessageID: q2.Message.Id, ModelID: "m",
	}); err != nil {
		t.Fatal(err)
	}
	rec = httptest.NewRecorder()
	b.server.EditMessage(rec, post("again"), b.conv.ID, q1.Message.Id)
	b.refused(rec, http.StatusConflict, "conversation_busy")
	rec = httptest.NewRecorder()
	b.server.RegenerateAnswer(rec, b.request(http.MethodPost, `{}`), b.conv.ID)
	b.refused(rec, http.StatusConflict, "conversation_busy")
	same(t, "nothing was deleted", b.path(), []string{"user:q1", "assistant:re: q1", "user:q2", "assistant:re: q2"})
	if !b.exists(q1.Message.Id) {
		t.Error("a refused edit deleted the question")
	}
}

// Another tab holding the conversation is told, in the batch that starts the
// run, that rows were deleted - for an edit and for a regeneration, and not for
// a plain message.
func TestRedoIsAnnounced(t *testing.T) {
	b := newRedoing(t)
	q1, _ := b.send("q1")
	b.send("q2")

	sub := b.server.runner.Hub().Subscribe(b.conv.ID)
	defer sub.Close()
	var froms []uuid.UUID
	announced := func() (changed, started int) {
		batches, resync := sub.Drain()
		if resync {
			t.Fatal("the subscriber fell behind")
		}
		for _, batch := range batches {
			var hasChange, hasStart bool
			for _, event := range batch.Events {
				switch e := event.(type) {
				case agent.TranscriptChanged:
					hasChange = true
					froms = append(froms, e.From)
				case agent.RunStarted:
					hasStart = true
				}
			}
			if hasChange {
				changed++
				if !hasStart || batch.RunID == uuid.Nil {
					t.Errorf("a deletion was announced apart from the run that follows it: %+v", batch)
				}
			}
			if hasStart {
				started++
			}
		}
		return changed, started
	}

	b.regenerate()
	if changed, started := announced(); changed != 1 || started != 1 {
		t.Fatalf("after a regeneration: %d deletions announced, %d runs started; want 1 and 1", changed, started)
	}
	b.edit(q1.Message.Id, "q1b")
	if changed, started := announced(); changed != 1 || started != 1 {
		t.Fatalf("after an edit: %d deletions announced, %d runs started; want 1 and 1", changed, started)
	}
	// An edit names the question it deleted from; a regeneration has no such row.
	if len(froms) != 2 || froms[0] != uuid.Nil || froms[1] != q1.Message.Id {
		t.Fatalf("the announcements name %v, want none and the edited question %s", froms, q1.Message.Id)
	}
	b.send("q2b")
	if changed, started := announced(); changed != 0 || started != 1 {
		t.Fatalf("after a message: %d deletions announced, %d runs started; want 0 and 1", changed, started)
	}

	// On the wire it is the contract's transcript_changed.
	raw, err := json.Marshal(toBatchEvent(agent.Batch{Events: []agent.Event{agent.TranscriptChanged{}}}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"type":"transcript_changed"`) || strings.Contains(string(raw), `"from"`) {
		t.Fatalf("the event on the wire: %s", raw)
	}
	from := uuid.New()
	raw, _ = json.Marshal(toBatchEvent(agent.Batch{Events: []agent.Event{agent.TranscriptChanged{From: from}}}))
	if !strings.Contains(string(raw), `"from":"`+from.String()+`"`) {
		t.Fatalf("the event with a question named on the wire: %s", raw)
	}
}

// An edit keeps the files its question carried, on the new question; the files
// of the questions deleted with it are removed, rows and stored objects.
func TestEditMovesFilesAndRemovesTheRest(t *testing.T) {
	b := newRedoing(t)
	local, err := storage.NewLocal(filepath.Join(t.TempDir(), "data"), []byte("secret"), "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = local.Close() })
	b.server.store = local

	q1, _ := b.send("q1")
	q2, _ := b.send("q2")
	q3, _ := b.send("q3")

	// q2 carries a file (referenced in its parts, as sending writes it), q3 another.
	attach := func(message uuid.UUID, name string) (uuid.UUID, string) {
		t.Helper()
		id := uuid.New()
		key := fmt.Sprintf("uploads/2026/10/10/%s.png", id)
		if _, err := local.Put(key, strings.NewReader("data"), 100); err != nil {
			t.Fatal(err)
		}
		if _, err := b.pool.Exec(context.Background(), `INSERT INTO attachments (id, message_id, user_id, storage_key, filename, content_type, size_bytes)
			VALUES ($1, $2, $3, $4, $5, 'image/png', 4)`, id, message, b.user.ID, key, name); err != nil {
			t.Fatal(err)
		}
		if _, err := b.pool.Exec(context.Background(), `UPDATE messages SET parts = parts || jsonb_build_array(jsonb_build_object('type', 'attachment', 'ref', $2::text, 'filename', $3::text, 'mediaType', 'image/png')) WHERE id = $1`,
			message, id.String(), name); err != nil {
			t.Fatal(err)
		}
		return id, key
	}
	keptID, keptKey := attach(q2.Message.Id, "kept.png")
	_, goneKey := attach(q3.Message.Id, "gone.png")
	beforeID, beforeKey := attach(q1.Message.Id, "before.png")

	edited, _ := b.edit(q2.Message.Id, "q2b")

	if !local.Has(keptKey) {
		t.Error("the file of the edited question was removed")
	}
	if local.Has(goneKey) {
		t.Error("the file of a question deleted with it is still stored")
	}
	if !local.Has(beforeKey) {
		t.Error("the file of an earlier question was removed")
	}
	var owner uuid.UUID
	if err := b.pool.QueryRow(context.Background(), `SELECT message_id FROM attachments WHERE id = $1`, keptID).Scan(&owner); err != nil || owner != edited.Message.Id {
		t.Fatalf("the file belongs to %v (%v), want the new question %s", owner, err, edited.Message.Id)
	}
	if n := b.count(`SELECT count(*) FROM attachments WHERE storage_key = $1`, goneKey); n != 0 {
		t.Error("the deleted question's attachment row remains")
	}
	if n := b.count(`SELECT count(*) FROM attachments WHERE id = $1 AND message_id = $2`, beforeID, q1.Message.Id); n != 1 {
		t.Error("the earlier question lost its file")
	}
	// The new question shows the file.
	items := b.transcript()
	if files := items[2].Attachments; files == nil || len(*files) != 1 || (*files)[0].Filename != "kept.png" {
		t.Fatalf("the new question's files: %+v", files)
	}
}

// A question the model asked belongs to its answer: editing the question
// before it takes it away, and the new answer's own question can be answered.
func TestAskUserAfterAnEdit(t *testing.T) {
	b := newRedoing(t)
	b.send("q1")
	q2, _ := b.send("ask q2")
	asked, status := b.edit(q2.Message.Id, "ask q2b")
	if status != sqlc.RunStatusAwaitingInput {
		t.Fatalf("the edit's run ended %s, want awaiting_input", status)
	}
	call := fmt.Sprintf("call_%d", 3)
	equal(t, "asking after the edit", shape(t, b.transcript()), []string{"user:text", "assistant:text", "user:text", "assistant:call"})

	// The old question was deleted with its answer.
	if rec := b.answer("call_2", answersJSON); rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "not_pending") {
		t.Fatalf("answering the deleted question: %d %s", rec.Code, rec.Body)
	}
	out, status := b.answered(call, answersJSON)
	if status != sqlc.RunStatusCompleted {
		t.Fatalf("the continuation ended %s", status)
	}
	if out.Run.Continues == nil || out.Message.Id != asked.Message.Id {
		t.Fatal("the answer did not continue the run that asked")
	}
	equal(t, "continued", shape(t, b.transcript()), []string{"user:text", "assistant:text", "user:text", "assistant:call", "tool:result", "assistant:text"})
}

// A conversation can be deleted for good after any of this.
func TestDeletingAfterARedo(t *testing.T) {
	b := newRedoing(t)
	q1, _ := b.send("q1")
	b.send("q2")
	b.edit(q1.Message.Id, "q1b")
	b.regenerate()
	if _, err := b.pool.Exec(context.Background(), `DELETE FROM conversations WHERE id = $1`, b.conv.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if n := b.count(`SELECT count(*) FROM messages WHERE conversation_id = $1`, b.conv.ID); n != 0 {
		t.Fatalf("%d messages left", n)
	}
}
