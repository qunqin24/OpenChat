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

	"github.com/qunqin24/openchat/apps/server/internal/agent"
	"github.com/qunqin24/openchat/apps/server/internal/auth"
	"github.com/qunqin24/openchat/apps/server/internal/db/dbtest"
	"github.com/qunqin24/openchat/apps/server/internal/db/sqlc"
)

// Writing blocks: the model writes and edits with the tool, the person saves
// versions of their own, neither overwrites the other unseen, and a document
// goes with the answer that made it.
func TestDocuments(t *testing.T) {
	pool := dbtest.Pool(t, "api_documents")
	ctx := context.Background()
	q := sqlc.New(pool)

	tag := uuid.NewString()
	newUser := func(name string) sqlc.User {
		var u sqlc.User
		if err := pool.QueryRow(ctx, `INSERT INTO users (email, name) VALUES ($1, $2) RETURNING id, email, name, role`,
			name+tag+"@test.invalid", name+tag).Scan(&u.ID, &u.Email, &u.Name, &u.Role); err != nil {
			t.Fatal(err)
		}
		return u
	}
	alice, mallory := newUser("alice"), newUser("mallory")
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM users WHERE id = ANY($1)", []uuid.UUID{alice.ID, mallory.ID})
	})
	server := &Server{pool: pool, q: q}

	// A conversation with one question and the run answering it.
	runIn := func(user sqlc.User) (sqlc.Conversation, sqlc.Run) {
		t.Helper()
		conv, err := q.CreateConversation(ctx, sqlc.CreateConversationParams{UserID: &user.ID, Title: "c"})
		if err != nil {
			t.Fatal(err)
		}
		input, err := appendInput(ctx, q, conv.ID, "write me an email", inputParts("write me an email", nil))
		if err != nil {
			t.Fatal(err)
		}
		run, err := q.CreateRun(ctx, sqlc.CreateRunParams{ConversationID: conv.ID, UserID: user.ID, InputMessageID: input.ID, ModelID: "m"})
		if err != nil {
			t.Fatal(err)
		}
		return conv, run
	}
	conv, run := runIn(alice)
	tool := DocumentTool{s: server}
	call := func(r sqlc.Run, args string) (agent.ToolResult, error) {
		scoped := agent.WithScope(ctx, agent.Scope{RunID: r.ID, UserID: r.UserID, ConversationID: r.ConversationID})
		return tool.Execute(scoped, json.RawMessage(args))
	}
	get := func(user sqlc.User, id uuid.UUID) (*httptest.ResponseRecorder, Document) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		server.GetDocument(rec, req.WithContext(auth.WithUser(req.Context(), user)), id)
		var doc Document
		_ = json.Unmarshal(rec.Body.Bytes(), &doc)
		return rec, doc
	}
	save := func(user sqlc.User, id uuid.UUID, body string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPatch, "/", strings.NewReader(body))
		server.UpdateDocument(rec, req.WithContext(auth.WithUser(req.Context(), user)), id)
		return rec
	}

	out, err := call(run, `{"op":"create","title":"Leave request","content":"Dear Sam,\n\nI would like to take Friday off.\n\nThanks,\nAlice"}`)
	if err != nil {
		t.Fatal(err)
	}
	id := out.Details.(DocumentToolDetails).DocumentID

	t.Run("the model's document is the person's to read", func(t *testing.T) {
		rec, doc := get(alice, id)
		if rec.Code != http.StatusOK || len(doc.Versions) != 1 || doc.Versions[0].Source != "model" || doc.Versions[0].Title != "Leave request" {
			t.Fatalf("%d %s", rec.Code, rec.Body)
		}
		if rec, _ := get(mallory, id); rec.Code != http.StatusNotFound {
			t.Errorf("mallory: %d", rec.Code)
		}
	})

	t.Run("the person saves a version, and a stale save is refused", func(t *testing.T) {
		rec := save(alice, id, `{"title":"Leave request","content":"Dear Sam,\n\nI would like to take Friday off, for a family event.\n\nThanks,\nAlice","baseVersion":1}`)
		var doc Document
		_ = json.Unmarshal(rec.Body.Bytes(), &doc)
		if rec.Code != http.StatusOK || len(doc.Versions) != 2 || doc.Versions[1].Source != "user" {
			t.Fatalf("save: %d %s", rec.Code, rec.Body)
		}
		if rec := save(alice, id, `{"title":"x","content":"y","baseVersion":1}`); rec.Code != http.StatusConflict {
			t.Errorf("stale save: %d", rec.Code)
		}
		if rec := save(mallory, id, `{"title":"x","content":"y","baseVersion":2}`); rec.Code != http.StatusNotFound {
			t.Errorf("mallory's save: %d", rec.Code)
		}
	})

	t.Run("the model edits a passage of the current text, and is told of the person's edit", func(t *testing.T) {
		read, err := call(run, fmt.Sprintf(`{"op":"read","id":%q}`, id))
		if err != nil || !strings.Contains(read.Content, "family event") || !strings.Contains(read.Content, "edited it") {
			t.Fatalf("read: %v %q", err, read.Content)
		}
		if _, err := call(run, fmt.Sprintf(`{"op":"update","id":%q,"edits":[{"find":"not in the text","replace":"x"}]}`, id)); err == nil {
			t.Error("a missing passage was accepted")
		}
		if _, err := call(run, fmt.Sprintf(`{"op":"update","id":%q,"edits":[{"find":"a","replace":"x"}]}`, id)); err == nil {
			t.Error("an ambiguous passage was accepted")
		}
		out, err := call(run, fmt.Sprintf(`{"op":"update","id":%q,"edits":[{"find":"Dear Sam,","replace":"Hi Sam,"}]}`, id))
		if err != nil || out.Details.(DocumentToolDetails).Version != 3 {
			t.Fatalf("update: %v %+v", err, out)
		}
		_, doc := get(alice, id)
		last := doc.Versions[len(doc.Versions)-1]
		if !strings.HasPrefix(last.Content, "Hi Sam,") || !strings.Contains(last.Content, "family event") {
			t.Errorf("v3 = %q", last.Content)
		}
	})

	t.Run("the person's edits are told to the next run, and a rewrite never undoes them unseen", func(t *testing.T) {
		latestOf := func() Document {
			_, doc := get(alice, id)
			return doc
		}
		base := latestOf().Versions
		saved := strings.Replace(base[len(base)-1].Content, "Thanks,", "Thank you so much,", 1)
		body, _ := json.Marshal(map[string]any{"title": "Leave request", "content": saved, "baseVersion": len(base)})
		if rec := save(alice, id, string(body)); rec.Code != http.StatusOK {
			t.Fatalf("save: %d %s", rec.Code, rec.Body)
		}

		// A run that was told nothing: its rewrite is refused, with the change.
		scoped := agent.WithScope(ctx, agent.Scope{RunID: run.ID, UserID: alice.ID, ConversationID: conv.ID, State: &agent.RunState{}})
		rewrite := fmt.Sprintf(`{"op":"update","id":%q,"content":"Hi Sam,\n\nFriday off, please.\n\nThank you so much,\nAlice"}`, id)
		_, err := tool.Execute(scoped, json.RawMessage(rewrite))
		if err == nil || !strings.Contains(err.Error(), "+ Thank you so much,") || !strings.Contains(err.Error(), "- Thanks,") {
			t.Fatalf("an unseen edit was overwritten: %v", err)
		}
		// Shown it, the run may write over it.
		if _, err := tool.Execute(scoped, json.RawMessage(rewrite)); err != nil {
			t.Fatalf("the rewrite after being shown: %v", err)
		}

		// The next edit of the person's is in the next run's note.
		now := latestOf().Versions
		body, _ = json.Marshal(map[string]any{"title": "Leave request", "content": now[len(now)-1].Content + "\n\nP.S. Back on Monday.", "baseVersion": len(now)})
		if rec := save(alice, id, string(body)); rec.Code != http.StatusOK {
			t.Fatalf("save: %d %s", rec.Code, rec.Body)
		}
		next := agent.WithScope(ctx, agent.Scope{RunID: run.ID, UserID: alice.ID, ConversationID: conv.ID, State: &agent.RunState{}})
		note, err := server.DocumentNote(next)
		if err != nil || !strings.Contains(note, "<document-edits>") || !strings.Contains(note, "+ P.S. Back on Monday.") ||
			!strings.Contains(note, fmt.Sprintf("from your version %d", len(now))) {
			t.Fatalf("note: %v %q", err, note)
		}
		if _, err := tool.Execute(next, json.RawMessage(rewrite)); err != nil {
			t.Errorf("a rewrite after the note: %v", err)
		}
		// Nothing of the person's since: no note.
		if note, err := server.DocumentNote(next); err != nil || note != "" {
			t.Errorf("a note with nothing to tell: %v %q", err, note)
		}
	})

	t.Run("the conversation lists its documents for its owner only", func(t *testing.T) {
		list := func(user sqlc.User) *httptest.ResponseRecorder {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			server.ListConversationDocuments(rec, req.WithContext(auth.WithUser(req.Context(), user)), conv.ID)
			return rec
		}
		rec := list(alice)
		var body struct{ Items []DocumentSummary }
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		if rec.Code != http.StatusOK || len(body.Items) != 1 || body.Items[0].Id != id || body.Items[0].Version < 3 {
			t.Fatalf("alice: %d %s", rec.Code, rec.Body)
		}
		if rec := list(mallory); rec.Code != http.StatusNotFound {
			t.Errorf("mallory: %d", rec.Code)
		}
	})

	t.Run("a document of another conversation is not there", func(t *testing.T) {
		_, other := runIn(alice)
		if _, err := call(other, fmt.Sprintf(`{"op":"read","id":%q}`, id)); err == nil {
			t.Error("read across conversations")
		}
		list, err := call(other, `{"op":"list"}`)
		if err != nil || !strings.Contains(list.Content, "no documents") {
			t.Errorf("list: %v %q", err, list.Content)
		}
	})

	t.Run("it goes with the answer that made it", func(t *testing.T) {
		if _, err := pool.Exec(ctx, `DELETE FROM runs WHERE id = $1`, run.ID); err != nil {
			t.Fatal(err)
		}
		if rec, _ := get(alice, id); rec.Code != http.StatusNotFound {
			t.Errorf("after the run went: %d", rec.Code)
		}
		_ = conv
	})
}

// A title the person wrote is their words too: refused by moderation, it
// reaches the model neither in the turn note nor in op=list.
func TestDocumentTitleRefusedStaysFromTheModel(t *testing.T) {
	pool := dbtest.Pool(t, "api_documents_title")
	ctx := context.Background()
	q := sqlc.New(pool)
	mod := newFakeModeration(t)
	reset := func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM app_settings WHERE key = ANY($1)`, moderationKeys)
	}
	reset()
	t.Cleanup(reset)

	var alice sqlc.User
	tag := uuid.NewString()
	if err := pool.QueryRow(ctx, `INSERT INTO users (email, name) VALUES ($1, $2) RETURNING id, email, name, role`,
		"t"+tag+"@test.invalid", "t"+tag).Scan(&alice.ID, &alice.Email, &alice.Name, &alice.Role); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM users WHERE id = $1", alice.ID) })
	server := &Server{pool: pool, q: q}
	conv, err := q.CreateConversation(ctx, sqlc.CreateConversationParams{UserID: &alice.ID, Title: "c"})
	if err != nil {
		t.Fatal(err)
	}
	input, err := appendInput(ctx, q, conv.ID, "write", inputParts("write", nil))
	if err != nil {
		t.Fatal(err)
	}
	run, err := q.CreateRun(ctx, sqlc.CreateRunParams{ConversationID: conv.ID, UserID: alice.ID, InputMessageID: input.ID, ModelID: "m"})
	if err != nil {
		t.Fatal(err)
	}
	scoped := agent.WithScope(ctx, agent.Scope{RunID: run.ID, UserID: alice.ID, ConversationID: conv.ID, State: &agent.RunState{}})
	tool := DocumentTool{s: server}
	out, err := tool.Execute(scoped, json.RawMessage(`{"op":"create","title":"Leave","content":"Dear Sam,\n\nFriday off."}`))
	if err != nil {
		t.Fatal(err)
	}
	id := out.Details.(DocumentToolDetails).DocumentID

	// The person renames it to something moderation refuses, with moderation on.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPatch, "/", strings.NewReader(`{"title":"BLOCKME secret","content":"Dear Sam,\n\nFriday off.","baseVersion":1}`))
	server.UpdateDocument(rec, req.WithContext(auth.WithUser(req.Context(), alice)), id)
	if rec.Code != http.StatusOK {
		t.Fatalf("save: %d %s", rec.Code, rec.Body)
	}
	for key, value := range map[string]any{
		settingModerationMode:          "openai",
		settingModerationOpenAIKey:     "sk-mod-secret-1234",
		settingModerationOpenAIBaseURL: mod.srv.URL + "/v1",
		settingModerationCategories:    []string{"violence/graphic"},
	} {
		raw, _ := json.Marshal(value)
		if err := q.SetSetting(ctx, sqlc.SetSettingParams{Key: key, Value: raw}); err != nil {
			t.Fatal(err)
		}
	}

	next := agent.WithScope(ctx, agent.Scope{RunID: run.ID, UserID: alice.ID, ConversationID: conv.ID, State: &agent.RunState{}})
	note, err := server.DocumentNote(next)
	if err != nil || strings.Contains(note, "BLOCKME") || !strings.Contains(note, "refused") {
		t.Errorf("note: %v %q", err, note)
	}
	list, err := tool.Execute(next, json.RawMessage(`{"op":"list"}`))
	if err != nil || strings.Contains(list.Content, "BLOCKME") || !strings.Contains(list.Content, id.String()) {
		t.Errorf("list: %v %q", err, list.Content)
	}
}
