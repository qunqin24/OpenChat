package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/qunqin/openchat/apps/server/internal/agent"
	"github.com/qunqin/openchat/apps/server/internal/db/sqlc"
)

// A message that content moderation refused is kept in the conversation as a
// row of status `blocked` - shown, never sent to a model. These tests run over
// the HTTP handlers, the fake moderation endpoint and the scripted model of the
// other moderation tests; nothing calls a real service.

// blockedBy configures the moderation of an existing harness.
func blockedBy(t *testing.T, h *askHarness) *fakeModeration {
	t.Helper()
	f := newFakeModeration(t)
	resetModeration(t, h)
	h.server.usage = h.server.runner.Recorder()
	h.server.endUsers = NewEndUsers(h.pool)
	body := fmt.Sprintf(`{"mode":"openai","openaiApiKey":"sk-mod-secret-1234","openaiBaseUrl":%q}`, f.srv.URL+"/v1")
	if rec := h.patchModeration(body); rec.Code != http.StatusOK {
		t.Fatalf("configure: %d %s", rec.Code, rec.Body)
	}
	return f
}

// blocking is a conversation whose model echoes the last thing it was asked and
// writes summaries, with moderation on.
func newBlocking(t *testing.T) *compacting {
	t.Helper()
	b := newCompacting(t)
	blockedBy(t, b.askHarness)
	return b
}

// blockedWith sends a message that is refused and returns the id the refusal
// names for the row it kept.
func (h *askHarness) blockedWith(content string) uuid.UUID {
	h.t.Helper()
	rec := h.trySend(content)
	refused(h.t, rec)
	var body struct {
		Details struct {
			MessageID uuid.UUID `json:"messageId"`
		} `json:"details"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body.Details.MessageID == uuid.Nil {
		h.t.Fatalf("the refusal names no message: %s", rec.Body)
	}
	return body.Details.MessageID
}

// shown is the transcript as role:text, with [blocked] after a refused message.
func (b *redoing) shown() []string {
	b.t.Helper()
	var out []string
	for _, m := range b.transcript() {
		line := string(m.Role) + ":" + m.Content
		if m.Status == MessageStatusBlocked {
			line += " [blocked]"
		}
		out = append(out, line)
	}
	return out
}

// tryEdit and tryRegenerate answer a request without requiring it to be taken.
func (h *askHarness) tryEdit(id uuid.UUID, content string) *httptest.ResponseRecorder {
	h.t.Helper()
	rec := httptest.NewRecorder()
	h.server.EditMessage(rec, h.request(http.MethodPost, fmt.Sprintf(`{"content":%q,"model":%q}`, content, h.model)), h.conv.ID, id)
	return rec
}

func (h *askHarness) tryRegenerate() *httptest.ResponseRecorder {
	h.t.Helper()
	rec := httptest.NewRecorder()
	h.server.RegenerateAnswer(rec, h.request(http.MethodPost, fmt.Sprintf(`{"model":%q}`, h.model)), h.conv.ID)
	return rec
}

// messageIDOf is the refused message a refusal names.
func messageIDOf(t *testing.T, rec *httptest.ResponseRecorder) uuid.UUID {
	t.Helper()
	var body struct {
		Details struct {
			MessageID uuid.UUID `json:"messageId"`
		} `json:"details"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body.Details.MessageID == uuid.Nil {
		t.Fatalf("the refusal names no message: %s", rec.Body)
	}
	return body.Details.MessageID
}

// appendRow writes a row straight into the conversation, as old data has it.
func (h *askHarness) appendRow(role sqlc.MessageRole, status sqlc.MessageStatus, text string) sqlc.Message {
	h.t.Helper()
	row, err := h.q.AppendEntry(context.Background(), sqlc.AppendEntryParams{
		ConversationID: h.conv.ID, Role: role, Content: text, Parts: agent.EncodeParts(inputParts(text, nil)), Status: status,
	})
	if err != nil {
		h.t.Fatal(err)
	}
	return row
}

func (b *redoing) requests() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.bodies)
}

// A refused send is stored once, at the end, as a blocked user message: no run,
// no file claimed, the other tabs told, and the answer carries its id.
func TestBlockedSendIsKept(t *testing.T) {
	b := newBlocking(t)
	blobs := &memoryStore{blobs: map[string][]byte{}}
	b.server.store = blobs
	if _, err := b.q.SaveModelOverride(context.Background(), sqlc.SaveModelOverrideParams{ModelID: b.model, InputModalities: []string{"image"}}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = b.pool.Exec(context.Background(), "DELETE FROM model_overrides WHERE model_id = $1", b.model)
	})

	b.send("first question")
	runs := b.count(`SELECT count(*) FROM runs WHERE conversation_id = $1`, b.conv.ID)
	asked := b.requests()

	// A file in the composer when the message is refused.
	data := []byte("\x89PNG fine")
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
	if err := blobs.PutObject(context.Background(), key, "image/png", data); err != nil {
		t.Fatal(err)
	}

	sub := b.server.runner.Hub().Subscribe(b.conv.ID)
	defer sub.Close()

	rec = httptest.NewRecorder()
	b.server.CreateMessage(rec, b.request(http.MethodPost,
		fmt.Sprintf(`{"content":"BLOCKME please","model":%q,"attachmentIds":[%q]}`, b.model, up.Id)), b.conv.ID)
	if got := refused(t, rec); strings.Join(got, ",") != "violence/graphic" {
		t.Fatalf("categories %v", got)
	}
	var body struct {
		Details struct {
			MessageID uuid.UUID `json:"messageId"`
		} `json:"details"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &body)

	if n := b.count(`SELECT count(*) FROM messages WHERE conversation_id = $1 AND status = 'blocked'`, b.conv.ID); n != 1 {
		t.Fatalf("%d blocked rows, want 1", n)
	}
	var role, content string
	var runID *uuid.UUID
	if err := b.pool.QueryRow(context.Background(),
		`SELECT role, content, run_id FROM messages WHERE id = $1 AND status = 'blocked'`, body.Details.MessageID).Scan(&role, &content, &runID); err != nil {
		t.Fatalf("the row the refusal names: %v", err)
	}
	if role != "user" || content != "BLOCKME please" || runID != nil {
		t.Errorf("row = %s %q %v", role, content, runID)
	}
	if got := b.count(`SELECT count(*) FROM runs WHERE conversation_id = $1`, b.conv.ID); got != runs {
		t.Errorf("a refused message started %d runs", got-runs)
	}
	if b.requests() != asked {
		t.Error("a refused message reached the model")
	}
	if b.count(`SELECT count(*) FROM attachments WHERE id = $1 AND message_id IS NULL`, up.Id) != 1 {
		t.Error("a refused message claimed its file")
	}
	same(t, "shown", b.shown(), []string{"user:first question", "assistant:re: first question", "user:BLOCKME please [blocked]"})

	// The conversation's other followers are told, with no row to drop.
	batches, _ := sub.Drain()
	var told bool
	for _, batch := range batches {
		for _, event := range batch.Events {
			if changed, ok := event.(agent.TranscriptChanged); ok {
				told = true
				if changed.From != uuid.Nil {
					t.Errorf("transcript_changed names a row: %v", changed.From)
				}
			}
		}
	}
	if !told {
		t.Errorf("no transcript_changed after a refused send: %+v", batches)
	}

	// The conversation does not go on past it: the way on is rewording it. The
	// model is shown the conversation without it.
	blocked := body.Details.MessageID
	b.refused(b.trySend("second question"), http.StatusConflict, "revise_refused")
	b.edit(blocked, "second question")
	same(t, "shown", b.shown(), []string{
		"user:first question", "assistant:re: first question", "user:second question", "assistant:re: second question",
	})
	same(t, "context", b.lastHistory(), []string{"user:first question", "assistant:re: first question", "user:second question"})
	for n := 1; n <= b.requests(); n++ {
		if strings.Contains(b.body(n), "BLOCKME") {
			t.Errorf("request %d carried the refused message", n)
		}
	}
}

// A refused send keeps nothing the memory snapshot or a title would be made of.
func TestBlockedFirstMessageOfAConversation(t *testing.T) {
	b := newBlocking(t)
	b.blockedWith("BLOCKME first")

	conv, err := b.q.GetConversation(context.Background(), sqlc.GetConversationParams{ID: b.conv.ID, UserID: &b.user.ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(conv.MemorySnapshot) != 0 {
		t.Error("a refused message took the memory snapshot")
	}
	if conv.Model != nil {
		t.Errorf("a refused message chose the conversation's model: %v", *conv.Model)
	}

	// A conversation of only refused messages has nothing to title.
	rec := httptest.NewRecorder()
	b.server.GenerateConversationTitle(rec, b.request(http.MethodPost, ""), b.conv.ID)
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "nothing_to_title") {
		t.Errorf("title: %d %s", rec.Code, rec.Body)
	}
	// Nor anything to regenerate.
	b.refused(b.tryRegenerate(), http.StatusConflict, "revise_refused")

	// Reworded, it starts the conversation as usual.
	b.edit(b.transcript()[0].Id, "a real question")
	same(t, "shown", b.shown(), []string{"user:a real question", "assistant:re: a real question"})
	same(t, "context", b.lastHistory(), []string{"user:a real question"})
}

// A title is made from the exchange, never the refused message beside it.
func TestBlockedMessageIsNotInATitle(t *testing.T) {
	b := newBlocking(t)
	b.send("tell me about bread")
	b.blockedWith("BLOCKME in the middle")

	rec := httptest.NewRecorder()
	b.server.GenerateConversationTitle(rec, b.request(http.MethodPost, ""), b.conv.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("title: %d %s", rec.Code, rec.Body)
	}
	title := b.body(b.requests())
	if !strings.Contains(title, "tell me about bread") || strings.Contains(title, "BLOCKME") {
		t.Errorf("the title was asked with %s", title)
	}
}

// Search and export skip the refused message.
func TestBlockedMessageIsNotSearchedOrExported(t *testing.T) {
	b := newBlocking(t)
	tag := strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
	b.send("an ordinary question " + tag)
	b.blockedWith("BLOCKME " + tag)

	search := func(term string) []Conversation {
		rec := httptest.NewRecorder()
		b.server.ListConversations(rec, b.request(http.MethodGet, ""), ListConversationsParams{Search: &term})
		var page ConversationPage
		if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil || rec.Code != http.StatusOK {
			t.Fatalf("search: %d %s", rec.Code, rec.Body)
		}
		return page.Items
	}
	if got := search("blockme " + tag); len(got) != 0 {
		t.Errorf("search found a refused message: %+v", got)
	}
	if got := search("ordinary question " + tag); len(got) != 1 {
		t.Errorf("search lost the conversation: %+v", got)
	}

	rec := httptest.NewRecorder()
	b.server.ExportMyConversations(rec, b.request(http.MethodGet, ""))
	var out ConversationExport
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("export: %d %v", rec.Code, err)
	}
	if strings.Contains(rec.Body.String(), "BLOCKME") {
		t.Errorf("the export has a refused message: %s", rec.Body)
	}
	if len(out.Conversations) != 1 || len(out.Conversations[0].Messages) != 2 {
		t.Errorf("export = %+v", out.Conversations)
	}
}

// A summary never covers, ends at or keeps a refused message.
func TestBlockedMessageAndCompaction(t *testing.T) {
	t.Run("a run compresses", func(t *testing.T) {
		long := strings.Repeat("lorem ipsum ", 120)
		b := newBlocking(t)
		b.compressesOnItsOwn(1200)
		blockedBy(t, b.askHarness)
		b.send("short question")
		// Refused messages as long as a window: none of it is the model's to read.
		first := b.blockedWith("BLOCKME " + long)
		b.edit(first, "another short question")
		second := b.blockedWith("BLOCKME " + long)
		if b.state() != nil {
			t.Fatal("refused messages filled the window")
		}
		b.edit(second, long)
		b.send("last question")

		got := b.state()
		if got == nil {
			t.Fatal("a conversation over half its window was not compressed")
		}
		blocked := b.count(`SELECT count(*) FROM messages WHERE id IN ($1, $2) AND status = 'blocked'`,
			got.ThroughMessageId, derefUUID(got.KeptMessageId))
		if blocked != 0 {
			t.Errorf("the summary %+v ends at or keeps a refused message", got)
		}
		history := b.lastHistory()
		if !isSummary(history[0]) || history[len(history)-1] != "user:last question" {
			t.Fatalf("the request after compressing: %q", history)
		}
		for n := 1; n <= b.requests(); n++ {
			if strings.Contains(b.body(n), "BLOCKME") {
				t.Errorf("request %d carried the refused message", n)
			}
		}
	})
}

func derefUUID(id *uuid.UUID) uuid.UUID {
	if id == nil {
		return uuid.Nil
	}
	return *id
}

// Regenerating answers the last question that was asked, not a refused
// message after it - and with a refused message at the end it is refused: that
// is reworded first (revise_refused_pg_test.go). A refused message in the middle
// of old data, with rows after it, goes with the answer.
func TestRegenerateAfterABlockedMessage(t *testing.T) {
	b := newBlocking(t)
	q1, _ := b.send("q1")
	b.blockedWith("BLOCKME")
	// Old data: an answer written after the refused message (the model's reply
	// to a question it had asked, which was answered past a refused message
	// before the conversation stopped there).
	b.appendRow(sqlc.MessageRoleAssistant, sqlc.MessageStatusComplete, "a stray answer")
	b.regenerate()

	same(t, "shown", b.shown(), []string{"user:q1", "assistant:re: q1"})
	same(t, "context", b.lastHistory(), []string{"user:q1"})
	if got := b.transcript()[0].Id; got != q1.Message.Id {
		t.Errorf("the question was replaced: %v", got)
	}
	if n := b.count(`SELECT count(*) FROM messages WHERE conversation_id = $1 AND status = 'blocked'`, b.conv.ID); n != 0 {
		t.Errorf("%d refused messages survived a regeneration", n)
	}
}

// An edit is an edit whatever it edits: of a refused message, the new words go
// at the end, are checked, and take the refused one's place.
func TestEditingABlockedMessage(t *testing.T) {
	b := newBlocking(t)
	if rec := b.patchModeration(`{"strikes":0}`); rec.Code != http.StatusOK { // no sanction in the way
		t.Fatal(rec.Body)
	}
	b.send("q1")
	blocked := b.blockedWith("BLOCKME")

	// Refused again: the words that were tried last are the refused message.
	rec := b.tryEdit(blocked, "BLOCKME still")
	refused(t, rec)
	again := messageIDOf(t, rec)
	if again == blocked {
		t.Fatal("the refused message was not replaced")
	}
	same(t, "shown", b.shown(), []string{"user:q1", "assistant:re: q1", "user:BLOCKME still [blocked]"})
	if n := b.count(`SELECT count(*) FROM messages WHERE id = $1`, blocked); n != 0 {
		t.Error("the old refused message survived being replaced")
	}

	// Reworded to something that passes.
	edited, status := b.edit(again, "q2")
	if status != sqlc.RunStatusCompleted {
		t.Fatalf("run ended %s", status)
	}
	same(t, "shown", b.shown(), []string{"user:q1", "assistant:re: q1", "user:q2", "assistant:re: q2"})
	same(t, "context", b.lastHistory(), []string{"user:q1", "assistant:re: q1", "user:q2"})
	if edited.Message.Status == MessageStatusBlocked {
		t.Error("the new wording is a refused message")
	}
	if n := b.count(`SELECT count(*) FROM messages WHERE id = $1`, again); n != 0 {
		t.Error("the refused message survived being edited")
	}

	// A refused edit of an ordinary question keeps nothing: the words go back
	// to the editor.
	rec = b.tryEdit(edited.Message.Id, "BLOCKME edit")
	refused(t, rec)
	if n := b.count(`SELECT count(*) FROM messages WHERE conversation_id = $1 AND status = 'blocked'`, b.conv.ID); n != 0 {
		t.Errorf("a refused edit left %d refused messages", n)
	}
	if strings.Contains(rec.Body.String(), "messageId") {
		t.Errorf("a refused edit of an ordinary question names a kept message: %s", rec.Body)
	}
}

// A refused message after the model's question neither hides the question nor
// answers it - and the conversation stops there: the question is not answered
// past it. Rewording the refused message goes on, skipping the question as any
// message written instead of an answer does.
func TestBlockedMessageAndAQuestionTheModelAsked(t *testing.T) {
	for _, w := range wires {
		t.Run(w.name, func(t *testing.T) {
			h := newAskHarness(t, w, askThenText(w))
			f := newFakeModeration(t)
			resetModeration(t, h)
			h.server.usage = h.server.runner.Recorder()
			h.server.endUsers = NewEndUsers(h.pool)
			body := fmt.Sprintf(`{"mode":"openai","openaiApiKey":"sk-mod-secret-1234","openaiBaseUrl":%q}`, f.srv.URL+"/v1")
			if rec := h.patchModeration(body); rec.Code != http.StatusOK {
				t.Fatalf("configure: %d %s", rec.Code, rec.Body)
			}

			if _, status := h.send("plan my trip"); status != sqlc.RunStatusAwaitingInput {
				t.Fatalf("run ended %s", status)
			}
			blocked := h.blockedWith("BLOCKME instead of answering")

			// It cannot be answered past the refused message, and nothing is written.
			rows := h.count(`SELECT count(*) FROM messages WHERE conversation_id = $1`, h.conv.ID)
			runs := h.count(`SELECT count(*) FROM runs WHERE conversation_id = $1`, h.conv.ID)
			if rec := h.answer("call_ask_1", answersJSON); rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "revise_refused") {
				t.Fatalf("answer past a refused message: %d %s", rec.Code, rec.Body)
			}
			if h.count(`SELECT count(*) FROM messages WHERE conversation_id = $1`, h.conv.ID) != rows ||
				h.count(`SELECT count(*) FROM runs WHERE conversation_id = $1`, h.conv.ID) != runs {
				t.Error("a refused answer wrote something")
			}

			// Reworded, the message is the person's reply instead of an answer.
			rec := h.tryEdit(blocked, "never mind")
			var out CreateMessageResponse
			h.accepted(rec, &out)
			if status := h.settle(out.Run.Id); status != sqlc.RunStatusCompleted && status != sqlc.RunStatusAwaitingInput {
				t.Fatalf("the reworded message's run ended %s", status)
			}
			for n := 1; n <= h.chatRequests(); n++ {
				if strings.Contains(h.body(n), "BLOCKME") {
					t.Errorf("request %d carried the refused message", n)
				}
			}
			for _, m := range h.transcript() {
				if m.Status == MessageStatusBlocked {
					t.Errorf("a refused message is left: %v", shape(t, h.transcript()))
				}
			}
		})
	}
}

// The message that earns the sanction is kept too, and answered with the
// sanction; so are those before it (each reworded, and refused again).
func TestBlockedMessagesAreKeptWhenOneEarnsTheSanction(t *testing.T) {
	h, _ := newModerationHarness(t, "")
	kept := h.blockedWith("BLOCKME 1")
	for i := 2; i <= 3; i++ {
		rec := h.tryEdit(kept, fmt.Sprintf("BLOCKME %d", i))
		if i == 3 {
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("the sanctioning message: %d %s", rec.Code, rec.Body)
			}
			break
		}
		refused(t, rec)
		kept = messageIDOf(t, rec)
	}
	// One at a time: each refused edit took the place of the last, and the one
	// that earned the sanction took the place of the one before it.
	if n := h.count(`SELECT count(*) FROM messages WHERE conversation_id = $1 AND status = 'blocked' AND content = 'BLOCKME 3'`, h.conv.ID); n != 1 {
		t.Errorf("%d copies of the sanctioning message kept, want 1", n)
	}
	if n := h.count(`SELECT count(*) FROM messages WHERE conversation_id = $1 AND status = 'blocked'`, h.conv.ID); n != 1 {
		t.Errorf("%d refused messages kept, want 1", n)
	}
	if n := h.count(`SELECT count(*) FROM users WHERE id = $1 AND disabled_at IS NOT NULL`, h.user.ID); n != 1 {
		t.Error("the account was not disabled")
	}
}

// While a run is writing the conversation nothing is kept: the row would land
// in the middle of the reply being made.
func TestBlockedMessageIsNotKeptWhileARunIsAnswering(t *testing.T) {
	h, _ := newModerationHarness(t, "")
	first, _ := h.send("a question")
	// A run the process is in the middle of.
	if _, err := h.pool.Exec(context.Background(),
		`INSERT INTO runs (conversation_id, user_id, input_message_id, model_id) VALUES ($1, $2, $3, $4)`,
		h.conv.ID, h.user.ID, first.Message.Id, h.model); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = h.pool.Exec(context.Background(), `DELETE FROM runs WHERE conversation_id = $1`, h.conv.ID)
	})

	rec := h.post("BLOCKME while answering")
	refused(t, rec)
	if strings.Contains(rec.Body.String(), "messageId") {
		t.Errorf("the refusal names a message that was not kept: %s", rec.Body)
	}
	if n := h.count(`SELECT count(*) FROM messages WHERE conversation_id = $1 AND status = 'blocked'`, h.conv.ID); n != 0 {
		t.Errorf("%d refused messages kept under a running run", n)
	}
}

// What the model sees is built from rows the queries return and skipped again
// by BuildContext, so a refused row handed to it by any other path is not sent.
func TestBuildContextSkipsBlockedRows(t *testing.T) {
	rows := []sqlc.Message{
		{Role: sqlc.MessageRoleUser, Content: "q", Parts: agent.EncodeParts(inputParts("q", nil)), Status: sqlc.MessageStatusComplete},
		{Role: sqlc.MessageRoleUser, Content: "refused", Parts: agent.EncodeParts(inputParts("refused", nil)), Status: sqlc.MessageStatusBlocked},
	}
	messages, err := agent.BuildContext(rows)
	if err != nil || len(messages) != 1 {
		t.Fatalf("%d messages (%v), want the question alone", len(messages), err)
	}
	if got := agent.EstimateRow(rows[1]); got != 0 {
		t.Errorf("a refused row weighs %d tokens", got)
	}
}
