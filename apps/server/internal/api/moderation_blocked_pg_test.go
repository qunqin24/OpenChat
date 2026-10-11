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
// writes summaries on request, with moderation on.
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
	rec := h.post(content)
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

	// The next send appends after it, and the model is shown the conversation
	// without it.
	b.send("second question")
	same(t, "shown", b.shown(), []string{
		"user:first question", "assistant:re: first question", "user:BLOCKME please [blocked]",
		"user:second question", "assistant:re: second question",
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
	// Nor anything to regenerate or compress.
	b.refused(func() *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		b.server.RegenerateAnswer(rec, b.request(http.MethodPost, fmt.Sprintf(`{"model":%q}`, b.model)), b.conv.ID)
		return rec
	}(), http.StatusNotFound, "nothing_to_answer")
	b.refused(b.compact(), http.StatusConflict, "nothing_to_compact")

	// The first real message after it starts the conversation as usual.
	b.send("a real question")
	same(t, "shown", b.shown(), []string{"user:BLOCKME first [blocked]", "user:a real question", "assistant:re: a real question"})
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

// A summary never covers, ends at or keeps a refused message, when a person
// compresses and when a run does.
func TestBlockedMessageAndCompaction(t *testing.T) {
	t.Run("manual", func(t *testing.T) {
		b := newBlocking(t)
		b.send("q1")
		answer := b.transcript()[1].Id
		blocked := b.blockedWith("BLOCKME at the end")

		summary := b.compacted()
		if summary.ThroughMessageId != answer || summary.KeptMessageId != nil {
			t.Fatalf("summary ends at %v keeping %v, want the answer %v and nothing", summary.ThroughMessageId, summary.KeptMessageId, answer)
		}
		if summary.ThroughMessageId == blocked {
			t.Fatal("the summary ends at a refused message")
		}
		for n := 1; n <= b.requests(); n++ {
			if strings.Contains(b.body(n), "BLOCKME") {
				t.Errorf("request %d, the summariser's, was shown the refused message", n)
			}
		}

		// Only a refused message since: nothing new to cover.
		b.refused(b.compact(), http.StatusConflict, "nothing_to_compact")

		// The next question is answered from the summary and itself.
		b.send("q2")
		history := b.lastHistory()
		if len(history) != 2 || !isSummary(history[0]) || history[1] != "user:q2" {
			t.Fatalf("the request after compressing: %q", history)
		}
		if got := b.state(); got == nil || got.ThroughMessageId != answer {
			t.Errorf("summary after the next send: %+v", got)
		}
	})

	t.Run("a refused message between the last answer and a question without one", func(t *testing.T) {
		b := newBlocking(t)
		b.send("q1")
		b.blockedWith("BLOCKME in between")
		// A question the model has not answered yet (it failed, say): the summary
		// ends at it and keeps it, and the refused message before it is not read.
		question := b.appendUser("q2")
		summary := b.compacted()
		if summary.ThroughMessageId != question.ID || summary.KeptMessageId == nil || *summary.KeptMessageId != question.ID {
			t.Fatalf("summary = %+v, want it to end at and keep %s", summary, question.ID)
		}
		for n := 1; n <= b.requests(); n++ {
			if strings.Contains(b.body(n), "BLOCKME") {
				t.Errorf("request %d was shown the refused message", n)
			}
		}
	})

	t.Run("on its own", func(t *testing.T) {
		long := strings.Repeat("lorem ipsum ", 120)
		b := newBlocking(t)
		b.compressesOnItsOwn(1200)
		blockedBy(t, b.askHarness)
		b.send("short question")
		// Refused messages as long as a window: none of it is the model's to read.
		b.blockedWith("BLOCKME " + long)
		b.blockedWith("BLOCKME " + long)
		b.send("another short question")
		if b.state() != nil {
			t.Fatal("refused messages filled the window")
		}
		b.send(long)
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

// Regenerating answers the last question that was asked, not the refused
// message after it, and removes that message with the answer.
func TestRegenerateAfterABlockedMessage(t *testing.T) {
	b := newBlocking(t)
	q1, _ := b.send("q1")
	b.blockedWith("BLOCKME")
	b.regenerate()

	same(t, "shown", b.shown(), []string{"user:q1", "assistant:re: q1"})
	same(t, "context", b.lastHistory(), []string{"user:q1"})
	if got := b.transcript()[0].Id; got != q1.Message.Id {
		t.Errorf("the question was replaced: %v", got)
	}
	if n := b.count(`SELECT count(*) FROM messages WHERE conversation_id = $1 AND status = 'blocked'`, b.conv.ID); n != 0 {
		t.Errorf("%d refused messages survived a regeneration", n)
	}

	// A refused message is the end of the transcript only until the next send,
	// and a question sent after it is the last.
	b.blockedWith("BLOCKME again")
	b.send("q2")
	b.regenerate()
	same(t, "shown", b.shown(), []string{
		"user:q1", "assistant:re: q1", "user:BLOCKME again [blocked]", "user:q2", "assistant:re: q2",
	})
	same(t, "context", b.lastHistory(), []string{"user:q1", "assistant:re: q1", "user:q2"})
}

// An edit is an edit whatever it edits: of a refused message, the new words go
// at the end, are checked, and take the refused one and what follows it.
func TestEditingABlockedMessage(t *testing.T) {
	b := newBlocking(t)
	if rec := b.patchModeration(`{"strikes":0}`); rec.Code != http.StatusOK { // no sanction in the way
		t.Fatal(rec.Body)
	}
	b.send("q1")
	blocked := b.blockedWith("BLOCKME")
	b.send("q2")

	// Refused again: nothing changes.
	rec := httptest.NewRecorder()
	b.server.EditMessage(rec, b.request(http.MethodPost, fmt.Sprintf(`{"content":"BLOCKME still","model":%q}`, b.model)), b.conv.ID, blocked)
	refused(t, rec)
	same(t, "shown", b.shown(), []string{
		"user:q1", "assistant:re: q1", "user:BLOCKME [blocked]", "user:q2", "assistant:re: q2",
	})

	// Reworded to something that passes.
	edited, status := b.edit(blocked, "q1 reworded")
	if status != sqlc.RunStatusCompleted {
		t.Fatalf("run ended %s", status)
	}
	same(t, "shown", b.shown(), []string{"user:q1", "assistant:re: q1", "user:q1 reworded", "assistant:re: q1 reworded"})
	same(t, "context", b.lastHistory(), []string{"user:q1", "assistant:re: q1", "user:q1 reworded"})
	if edited.Message.Status == MessageStatusBlocked {
		t.Error("the new wording is a refused message")
	}
	if n := b.count(`SELECT count(*) FROM messages WHERE id = $1`, blocked); n != 0 {
		t.Error("the refused message survived being edited")
	}

	// A refused edit of an ordinary question keeps nothing: the words go back
	// to the editor.
	rec = httptest.NewRecorder()
	b.server.EditMessage(rec, b.request(http.MethodPost, fmt.Sprintf(`{"content":"BLOCKME edit","model":%q}`, b.model)), b.conv.ID, edited.Message.Id)
	refused(t, rec)
	if n := b.count(`SELECT count(*) FROM messages WHERE conversation_id = $1 AND status = 'blocked'`, b.conv.ID); n != 0 {
		t.Errorf("a refused edit left %d refused messages", n)
	}
}

// A refused message after the model's question neither hides the question nor
// answers it.
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
			h.blockedWith("BLOCKME instead of answering")

			// Still waiting for its answer: compression says so, and the answer is taken.
			rec := httptest.NewRecorder()
			h.server.CompactConversation(rec, h.request(http.MethodPost, ""), h.conv.ID)
			if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "question_pending") {
				t.Fatalf("compress while a question waits: %d %s", rec.Code, rec.Body)
			}
			if _, status := h.answered("call_ask_1", answersJSON); status != sqlc.RunStatusCompleted {
				t.Fatalf("the answer's run ended %s", status)
			}

			// The model was sent its question and the answer, not the refused words.
			last := h.body(2)
			if strings.Contains(last, "BLOCKME") {
				t.Errorf("the request after the answer carried the refused message")
			}
			equal(t, "results", w.results(t, last), []string{"call_ask_1"})
			shown := h.transcript()
			if len(shown) != 5 || shown[2].Status != MessageStatusBlocked || shown[3].Role != RoleTool {
				t.Errorf("transcript = %v", shape(t, shown))
			}
		})
	}
}

// The message that earns the sanction is kept too, and answered with the
// sanction; so are those before it.
func TestBlockedMessagesAreKeptWhenOneEarnsTheSanction(t *testing.T) {
	h, _ := newModerationHarness(t, "")
	for i := 1; i <= 2; i++ {
		h.blockedWith(fmt.Sprintf("BLOCKME %d", i))
	}
	if rec := h.post("BLOCKME 3"); rec.Code != http.StatusUnauthorized {
		t.Fatalf("the sanctioning message: %d %s", rec.Code, rec.Body)
	}
	if n := h.count(`SELECT count(*) FROM messages WHERE conversation_id = $1 AND status = 'blocked'`, h.conv.ID); n != 3 {
		t.Errorf("%d refused messages kept, want 3", n)
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
