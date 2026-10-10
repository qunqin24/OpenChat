package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/qunqin/openchat/apps/server/internal/agent"
	"github.com/qunqin/openchat/apps/server/internal/auth"
	"github.com/qunqin/openchat/apps/server/internal/db/sqlc"
)

// Compressing a conversation's context over the HTTP handlers and a scripted
// model that echoes what it was last asked, and answers a summarising request
// with "SUMMARY #<n>" (n being the number of the request). Needs the same
// scratch database as the other agent tests (TEST_DATABASE_URL).

// compacting is a conversation whose model also writes summaries.
type compacting struct {
	*redoing
}

func newCompacting(t *testing.T) *compacting {
	t.Helper()
	b := &compacting{redoing: &redoing{}}
	b.redoing.askHarness = newAskHarness(t, openAIWire, func(n int) string {
		if strings.Contains(b.body(n), "conversation-context compressor") {
			return openAIWire.text(fmt.Sprintf("SUMMARY #%d", n))
		}
		history := b.history(n)
		return openAIWire.text("re: " + strings.TrimPrefix(history[len(history)-1], "user:"))
	})
	return b
}

// compressesOnItsOwn gives the conversation a runner that compresses requests
// that near the window, with the model's window set to limit tokens.
func (b *compacting) compressesOnItsOwn(limit int64) {
	b.t.Helper()
	ctx := context.Background()
	if _, err := b.q.SaveModelOverride(ctx, sqlc.SaveModelOverrideParams{ModelID: b.model, ContextLimit: &limit}); err != nil {
		b.t.Fatal(err)
	}
	b.t.Cleanup(func() {
		_, _ = b.pool.Exec(context.Background(), "DELETE FROM model_overrides WHERE model_id = $1", b.model)
	})
	runner := agent.NewRunner(b.pool, agent.NewHub(), NewModelResolver(b.pool),
		func(context.Context) []agent.Tool { return nil },
		agent.Options{SystemPrompt: NewSystemPrompt(b.pool), PartialInterval: 5 * time.Millisecond, Compression: NewCompression(b.pool)})
	b.t.Cleanup(func() { _ = runner.Shutdown(context.Background()) })
	b.server.runner = runner
}

func (b *compacting) compact() *httptest.ResponseRecorder {
	b.t.Helper()
	rec := httptest.NewRecorder()
	b.server.CompactConversation(rec, b.request(http.MethodPost, ""), b.conv.ID)
	return rec
}

func (b *compacting) undo() *httptest.ResponseRecorder {
	b.t.Helper()
	rec := httptest.NewRecorder()
	b.server.UndoCompaction(rec, b.request(http.MethodDelete, ""), b.conv.ID)
	return rec
}

// state is the conversation's summary as the API reports it, nil for none.
func (b *compacting) state() *ContextSummary {
	b.t.Helper()
	rec := httptest.NewRecorder()
	b.server.GetCompaction(rec, b.request(http.MethodGet, ""), b.conv.ID)
	if rec.Code != http.StatusOK {
		b.t.Fatalf("GET compaction: %d %s", rec.Code, rec.Body)
	}
	var out CompactionState
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		b.t.Fatal(err)
	}
	return out.Summary
}

func (b *compacting) compacted() *ContextSummary {
	b.t.Helper()
	rec := b.compact()
	if rec.Code != http.StatusOK {
		b.t.Fatalf("compact: %d %s", rec.Code, rec.Body)
	}
	var out CompactionState
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || out.Summary == nil {
		b.t.Fatalf("compact: %s (%v)", rec.Body, err)
	}
	return out.Summary
}

// holds says whether a request message starts as the summary does.
func isSummary(message string) bool {
	return strings.HasPrefix(message, "user:<compressed_history_summary>")
}

// A person's compression summarises the whole conversation with the
// conversation's own model, goes in the ledger as its own kind, and is what the
// model is sent in place of the rows - which are all still in the transcript.
func TestCompactionOnRequest(t *testing.T) {
	b := newCompacting(t)
	b.send("q1")
	b.send("q2")
	transcript := b.path()
	last := b.transcript()[3].Id

	summary := b.compacted()
	if !strings.HasPrefix(summary.Content, "SUMMARY #") || summary.ThroughMessageId != last || summary.KeptMessageId != nil || summary.Model != b.model {
		t.Fatalf("summary = %+v, want it to end at the last answer %s and keep nothing", summary, last)
	}
	if got := b.state(); got == nil || got.Content != summary.Content {
		t.Fatalf("GET after compacting: %+v", got)
	}

	// The summariser was shown the history, and with the conversation's own model.
	read := b.body(3) // q1, q2, then the summary
	for _, want := range []string{"chat_history", "q1", "re: q2", b.model} {
		if !strings.Contains(read, want) {
			t.Errorf("the summariser's request lacks %q", want)
		}
	}

	// In the ledger, as the person's, under the conversation, with no run.
	var kind string
	var owner, conversation *uuid.UUID
	var run *uuid.UUID
	if err := b.pool.QueryRow(context.Background(),
		`SELECT kind, user_id, conversation_id, run_id FROM usage_logs WHERE conversation_id = $1 AND kind = 'compression'`,
		b.conv.ID).Scan(&kind, &owner, &conversation, &run); err != nil {
		t.Fatalf("ledger: %v", err)
	}
	if owner == nil || *owner != b.user.ID || run != nil {
		t.Errorf("ledger row: user %v, run %v", owner, run)
	}

	// The transcript is untouched; the model is sent the summary, then the new question.
	same(t, "transcript", b.path(), transcript)
	b.send("q3")
	history := b.lastHistory()
	if len(history) != 2 || !isSummary(history[0]) || !strings.Contains(history[0], summary.Content) || history[1] != "user:q3" {
		t.Fatalf("the request after compressing: %q", history)
	}

	// Nothing new since: nothing to do.
	b2 := b.compact()
	if b2.Code != http.StatusOK { // q3 and its answer are new
		t.Fatalf("compacting what q3 added: %d %s", b2.Code, b2.Body)
	}
	if again := b.compact(); again.Code != http.StatusConflict || !strings.Contains(again.Body.String(), "nothing_to_compact") {
		t.Fatalf("compacting twice: %d %s", again.Code, again.Body)
	}
}

// Taking the compression back deletes only the summary: the model is sent the
// whole conversation again.
func TestUndoCompaction(t *testing.T) {
	b := newCompacting(t)
	b.send("q1")
	b.send("q2")
	b.compacted()
	rows := b.count(`SELECT count(*) FROM messages WHERE conversation_id = $1`, b.conv.ID)

	sub := b.server.runner.Hub().Subscribe(b.conv.ID)
	defer sub.Close()
	if rec := b.undo(); rec.Code != http.StatusNoContent {
		t.Fatalf("undo: %d %s", rec.Code, rec.Body)
	}
	if b.state() != nil {
		t.Fatal("the summary is still there")
	}
	if n := b.count(`SELECT count(*) FROM messages WHERE conversation_id = $1`, b.conv.ID); n != rows {
		t.Fatalf("%d rows, want the %d it had", n, rows)
	}
	b.send("q3")
	same(t, "the whole conversation again", b.lastHistory(),
		[]string{"user:q1", "assistant:re: q1", "user:q2", "assistant:re: q2", "user:q3"})

	// Idempotent; and only a change is announced.
	if rec := b.undo(); rec.Code != http.StatusNoContent {
		t.Fatalf("undo again: %d", rec.Code)
	}
	var changes []bool
	batches, _ := sub.Drain()
	for _, batch := range batches {
		for _, e := range batch.Events {
			if c, ok := e.(agent.CompactionChanged); ok {
				if batch.RunID != uuid.Nil || batch.Version != 0 {
					t.Errorf("the announcement belongs to a run: %v/%d", batch.RunID, batch.Version)
				}
				changes = append(changes, c.Summary != nil)
			}
		}
	}
	if len(changes) != 1 || changes[0] {
		t.Errorf("announced changes = %v, want one removal", changes)
	}
}

// Making a summary is announced to the conversation's other followers.
func TestCompactionIsAnnounced(t *testing.T) {
	b := newCompacting(t)
	b.send("q1")
	sub := b.server.runner.Hub().Subscribe(b.conv.ID)
	defer sub.Close()
	made := b.compacted()
	batches, _ := sub.Drain()
	for _, batch := range batches {
		for _, e := range batch.Events {
			if c, ok := e.(agent.CompactionChanged); ok && c.Summary != nil && c.Summary.Content == made.Content {
				return
			}
		}
	}
	t.Fatalf("no compaction_changed with the summary in %v", batches)
}

// Neither is possible under a run: it adds to the conversation, and compresses
// it itself. Nor is a compression of a conversation with nothing to compress.
func TestCompactionRefusals(t *testing.T) {
	b := newCompacting(t)

	if rec := b.compact(); rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "nothing_to_compact") {
		t.Fatalf("an empty conversation: %d %s", rec.Code, rec.Body)
	}
	q1, _ := b.send("q1")
	b.compacted()

	// A run is going.
	run, err := b.q.CreateRun(context.Background(), sqlc.CreateRunParams{
		ConversationID: b.conv.ID, UserID: b.user.ID, InputMessageID: q1.Message.Id, ModelID: b.model,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = b.pool.Exec(context.Background(), `UPDATE runs SET status = 'cancelled', finished_at = now() WHERE id = $1`, run.ID)
	})
	if rec := b.compact(); rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "conversation_busy") {
		t.Fatalf("compacting under a run: %d %s", rec.Code, rec.Body)
	}
	if rec := b.undo(); rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "conversation_busy") {
		t.Fatalf("undoing under a run: %d %s", rec.Code, rec.Body)
	}
	if b.state() == nil {
		t.Fatal("a refused undo deleted the summary")
	}
}

// Someone else's conversation does not exist, for reading, compressing or
// taking back.
func TestCompactionBelongsToTheConversationsOwner(t *testing.T) {
	b := newCompacting(t)
	b.send("q1")
	b.compacted()

	var stranger sqlc.User
	tag := uuid.NewString()
	if err := b.pool.QueryRow(context.Background(), `INSERT INTO users (email, name) VALUES ($1, $2) RETURNING id, email, name, role`,
		tag+"@test.invalid", "stranger-"+tag).Scan(&stranger.ID, &stranger.Email, &stranger.Name, &stranger.Role); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = b.pool.Exec(context.Background(), "DELETE FROM users WHERE id = $1", stranger.ID) })
	as := func(r *http.Request) *http.Request { return r.WithContext(auth.WithUser(r.Context(), stranger)) }

	rec := httptest.NewRecorder()
	b.server.GetCompaction(rec, as(b.request(http.MethodGet, "")), b.conv.ID)
	if rec.Code != http.StatusNotFound {
		t.Errorf("GET: %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	b.server.CompactConversation(rec, as(b.request(http.MethodPost, "")), b.conv.ID)
	if rec.Code != http.StatusNotFound {
		t.Errorf("POST: %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	b.server.UndoCompaction(rec, as(b.request(http.MethodDelete, "")), b.conv.ID)
	if rec.Code != http.StatusNotFound {
		t.Errorf("DELETE: %d", rec.Code)
	}
	if b.state() == nil {
		t.Fatal("a stranger deleted the summary")
	}
}

// A summary is tied to the rows it covers, in the database: editing a question
// the summary covers deletes the summary with it; editing one after it leaves
// it. Regenerating an answer the summary covers takes it too.
func TestSummaryAgainstEditsAndRegeneration(t *testing.T) {
	t.Run("an edit inside the range deletes the summary", func(t *testing.T) {
		b := newCompacting(t)
		q := map[string]CreateMessageResponse{}
		for _, text := range []string{"q1", "q2", "q3"} {
			q[text], _ = b.send(text)
		}
		b.compacted() // through the last answer
		b.edit(q["q2"].Message.Id, "q2b")
		if b.state() != nil {
			t.Fatal("the summary survived an edit of a question it covers")
		}
		// The model was sent the rows before the edit, not a summary of rows that are gone.
		same(t, "context", b.lastHistory(), []string{"user:q1", "assistant:re: q1", "user:q2b"})
		if n := b.count(`SELECT count(*) FROM context_summaries WHERE conversation_id = $1`, b.conv.ID); n != 0 {
			t.Fatalf("%d summary rows", n)
		}
	})

	t.Run("an edit of the question a summary keeps deletes it too", func(t *testing.T) {
		// A summary made in the middle of a run keeps the question being answered.
		b := newCompacting(t)
		q1, _ := b.send("q1")
		rows := b.transcript()
		if _, err := b.q.UpsertContextSummary(context.Background(), sqlc.UpsertContextSummaryParams{
			ConversationID: b.conv.ID, ThroughMessageID: rows[1].Id, KeptMessageID: &rows[0].Id, Content: "S", ModelID: b.model,
		}); err != nil {
			t.Fatal(err)
		}
		b.edit(q1.Message.Id, "q1b")
		if b.state() != nil {
			t.Fatal("the summary survived the edit of the question it kept")
		}
	})

	t.Run("an edit after the range leaves the summary", func(t *testing.T) {
		b := newCompacting(t)
		b.send("q1")
		b.send("q2")
		summary := b.compacted() // through a2
		q3, _ := b.send("q3")
		q4, _ := b.send("q4")

		b.edit(q4.Message.Id, "q4b")
		if got := b.state(); got == nil || got.Content != summary.Content {
			t.Fatalf("an edit after the range changed the summary: %+v", got)
		}
		history := b.lastHistory()
		if len(history) != 4 || !isSummary(history[0]) || history[1] != "user:q3" || history[2] != "assistant:re: q3" || history[3] != "user:q4b" {
			t.Fatalf("request after the edit: %q", history)
		}

		// The first question after the range: the new wording replaces it and what followed.
		b.edit(q3.Message.Id, "q3b")
		if got := b.state(); got == nil || got.Content != summary.Content {
			t.Fatalf("editing the first question past the range changed the summary: %+v", got)
		}
		history = b.lastHistory()
		if len(history) != 2 || !isSummary(history[0]) || history[1] != "user:q3b" {
			t.Fatalf("request after the second edit: %q", history)
		}
	})

	t.Run("regenerating an answer the summary covers deletes it", func(t *testing.T) {
		b := newCompacting(t)
		b.send("q1")
		b.send("q2")
		b.compacted() // through a2, the answer
		b.regenerate()
		if b.state() != nil {
			t.Fatal("the summary of an answer that was deleted survived")
		}
		same(t, "context", b.lastHistory(), []string{"user:q1", "assistant:re: q1", "user:q2"})
	})

	t.Run("regenerating leaves a summary that ends at the question", func(t *testing.T) {
		// The answer failed because the conversation was too long; compressing
		// and answering again must keep the compression.
		b := newCompacting(t)
		b.send("q1")
		ctx := context.Background()
		q2 := b.appendUser("q2")
		stop := agent.StopError
		code := agent.CodeContextTooLong
		if _, err := b.q.AppendEntry(ctx, sqlc.AppendEntryParams{
			ConversationID: b.conv.ID, Role: sqlc.MessageRoleAssistant, Parts: agent.EncodeParts(nil),
			Status: sqlc.MessageStatusFailed, StopReason: &stop, ErrorCode: &code,
		}); err != nil {
			t.Fatal(err)
		}
		summary := b.compacted()
		if summary.ThroughMessageId != q2.ID || summary.KeptMessageId == nil || *summary.KeptMessageId != q2.ID {
			t.Fatalf("summary %+v: should end at the unanswered question and keep it", summary)
		}
		b.regenerate()
		if got := b.state(); got == nil || got.Content != summary.Content {
			t.Fatalf("answering again lost the summary: %+v", got)
		}
		history := b.lastHistory()
		if len(history) != 2 || !isSummary(history[0]) || history[1] != "user:q2" {
			t.Fatalf("the answer was asked with %q, want the summary and the question", history)
		}
		same(t, "the transcript", b.path(), []string{"user:q1", "assistant:re: q1", "user:q2", "assistant:re: q2"})
	})
}

func (b *compacting) appendUser(text string) sqlc.Message {
	b.t.Helper()
	row, err := b.q.AppendEntry(context.Background(), sqlc.AppendEntryParams{
		ConversationID: b.conv.ID, Role: sqlc.MessageRoleUser, Content: text,
		Parts:  agent.EncodeParts(inputParts(text, nil)),
		Status: sqlc.MessageStatusComplete,
	})
	if err != nil {
		b.t.Fatal(err)
	}
	return row
}

// A run compresses by itself when the model's window is near, if the
// administrator has not switched that off - and the settings page reports and
// changes the switch.
func TestAutomaticCompression(t *testing.T) {
	long := strings.Repeat("lorem ipsum ", 120) // ~1440 characters: ~360 tokens

	t.Run("on by default", func(t *testing.T) {
		b := newCompacting(t)
		b.compressesOnItsOwn(1200) // half of it is 600 tokens
		b.send(long)
		b.send(long)
		b.send("last question")

		if got := b.state(); got == nil {
			t.Fatal("a conversation over half of its window was not compressed")
		}
		history := b.lastHistory()
		if !isSummary(history[0]) || history[len(history)-1] != "user:last question" {
			t.Fatalf("the request after compressing: %q", history)
		}
		// The summary kept the question being answered and the transcript is whole.
		got := b.state()
		if got.KeptMessageId == nil {
			t.Error("a run's compression keeps its question")
		}
		if n := len(b.transcript()); n != 6 {
			t.Errorf("%d rows shown, want all 6", n)
		}
	})

	t.Run("switched off", func(t *testing.T) {
		b := newCompacting(t)
		b.compressesOnItsOwn(1200)
		raw, _ := json.Marshal(false)
		if err := b.q.SetSetting(context.Background(), sqlc.SetSettingParams{Key: settingCompressContext, Value: raw}); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			_, _ = b.pool.Exec(context.Background(), `DELETE FROM app_settings WHERE key = $1`, settingCompressContext)
		})
		b.send(long)
		b.send(long)
		b.send("last question")
		if b.state() != nil {
			t.Fatal("compressed with the switch off")
		}
		if got := len(b.lastHistory()); got != 5 {
			t.Fatalf("the request had %d messages, want the whole conversation", got)
		}
		// People can still ask.
		if rec := b.compact(); rec.Code != http.StatusOK {
			t.Fatalf("manual compression with the switch off: %d %s", rec.Code, rec.Body)
		}
	})
}

func TestCompressContextSetting(t *testing.T) {
	b := newCompacting(t)
	t.Cleanup(func() {
		_, _ = b.pool.Exec(context.Background(), `DELETE FROM app_settings WHERE key = $1`, settingCompressContext)
	})
	_, _ = b.pool.Exec(context.Background(), `DELETE FROM app_settings WHERE key = $1`, settingCompressContext)

	settings, err := b.server.systemSettings(context.Background())
	if err != nil || !settings.CompressContext {
		t.Fatalf("default = %v (%v), want on", settings.CompressContext, err)
	}
	off := false
	body, _ := json.Marshal(UpdateSystemSettingsRequest{CompressContext: &off})
	rec := httptest.NewRecorder()
	b.server.UpdateSystemSettings(rec, b.request(http.MethodPatch, string(body)))
	if rec.Code != http.StatusOK {
		t.Fatalf("update: %d %s", rec.Code, rec.Body)
	}
	var out SystemSettings
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || out.CompressContext {
		t.Fatalf("after switching off: %+v (%v)", out.CompressContext, err)
	}
	if on := NewCompression(b.pool).Auto(context.Background()); on {
		t.Fatal("the runs' policy ignores the switch")
	}
}
