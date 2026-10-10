package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/qunqin/openchat/apps/server/internal/agent"
	"github.com/qunqin/openchat/apps/server/internal/auth"
	"github.com/qunqin/openchat/apps/server/internal/config"
	"github.com/qunqin/openchat/apps/server/internal/db/sqlc"
	"github.com/qunqin/openchat/apps/server/internal/memory"
	"github.com/qunqin/openchat/apps/server/internal/tools"
)

// Personal memory end to end: a scripted OpenAI-compatible model behind the
// real tool source, system prompt and handlers. Needs the scratch database of
// the other agent tests (AGENT_TEST_DATABASE_URL).

func openAICall(callID, name, args string) string {
	quoted, _ := json.Marshal(args)
	return fmt.Sprintf(`data: {"id":"r","model":"fake","choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[{"index":0,"id":%q,"type":"function","function":{"name":%q,"arguments":%s}}]},"finish_reason":null}]}`+"\n\n"+
		`data: {"id":"r","model":"fake","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`+"\n\n"+"data: [DONE]\n\n", callID, name, quoted)
}

type memHarness struct {
	t      *testing.T
	pool   *pgxpool.Pool
	q      *sqlc.Queries
	server *Server
	notes  *memory.Store
	user   sqlc.User
	model  string

	mu      sync.Mutex
	bodies  []string
	reply   func(n int) string
	lastRun uuid.UUID
}

func newUser(t *testing.T, pool *pgxpool.Pool, role string) sqlc.User {
	t.Helper()
	var user sqlc.User
	tag := uuid.NewString()
	if err := pool.QueryRow(context.Background(),
		`INSERT INTO users (email, name, role) VALUES ($1, $2, $3::user_role) RETURNING id, email, name, role`,
		tag+"@test.invalid", "mem-"+tag, role).Scan(&user.ID, &user.Email, &user.Name, &user.Role); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM users WHERE id = $1", user.ID) })
	return user
}

// newMemHarness answers every request with reply(n), n counting from 1; nil
// answers "ok".
func newMemHarness(t *testing.T, reply func(n int) string) *memHarness {
	t.Helper()
	pool := agentTestPool(t)
	ctx := context.Background()
	h := &memHarness{t: t, pool: pool, q: sqlc.New(pool), notes: memory.NewStore(pool), reply: reply}
	if h.reply == nil {
		h.reply = func(int) string { return openAIWire.text("ok") }
	}
	// The administrator's switch is shared state of the database.
	_, _ = pool.Exec(ctx, "DELETE FROM app_settings WHERE key = $1", memory.SettingEnabled)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM app_settings WHERE key = $1", memory.SettingEnabled)
	})

	endpoint := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		h.mu.Lock()
		h.bodies = append(h.bodies, string(raw))
		n := len(h.bodies)
		h.mu.Unlock()
		rw.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(rw, h.reply(n))
	}))
	t.Cleanup(endpoint.Close)

	h.user = newUser(t, pool, "user")
	tag := uuid.NewString()
	h.model = "fake-model-" + tag
	channel, err := h.q.CreateChannel(ctx, sqlc.CreateChannelParams{
		Name: "fake " + tag, Type: sqlc.ChannelTypeOpenaiCompatible, Enabled: true, BaseUrl: endpoint.URL + "/v1", ApiKey: "sk-test",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.q.CreateSelectedChannelModels(ctx, sqlc.CreateSelectedChannelModelsParams{
		ChannelID: channel.ID, ModelIds: []string{h.model}, DisplayNames: []string{""}, Sources: []string{"manual"},
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM channels WHERE id = $1", channel.ID) })

	runner := agent.NewRunner(pool, agent.NewHub(), NewModelResolver(pool), NewToolSource(pool),
		agent.Options{SystemPrompt: NewSystemPrompt(pool), PartialInterval: 5 * time.Millisecond})
	t.Cleanup(func() { _ = runner.Shutdown(context.Background()) })
	h.server = &Server{pool: pool, q: h.q, runner: runner}
	return h
}

func (h *memHarness) as(user sqlc.User, method, body string) *http.Request {
	r := httptest.NewRequest(method, "/", strings.NewReader(body))
	return r.WithContext(auth.WithUser(r.Context(), user))
}

func (h *memHarness) request(method, body string) *http.Request { return h.as(h.user, method, body) }

func (h *memHarness) conversation(temporary bool) sqlc.Conversation {
	h.t.Helper()
	conv, err := h.q.CreateConversation(context.Background(), sqlc.CreateConversationParams{
		UserID: &h.user.ID, Title: defaultTitle, Temporary: temporary,
	})
	if err != nil {
		h.t.Fatal(err)
	}
	return conv
}

// send writes a message and waits for its run to end.
func (h *memHarness) send(conv uuid.UUID, content string) sqlc.RunStatus {
	h.t.Helper()
	rec := httptest.NewRecorder()
	h.server.CreateMessage(rec, h.request(http.MethodPost, fmt.Sprintf(`{"content":%q,"model":%q}`, content, h.model)), conv)
	if rec.Code != http.StatusAccepted {
		h.t.Fatalf("send: %d %s", rec.Code, rec.Body)
	}
	var out CreateMessageResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		h.t.Fatal(err)
	}
	h.lastRun = out.Run.Id
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		run, err := h.q.GetRun(context.Background(), out.Run.Id)
		if err != nil {
			h.t.Fatal(err)
		}
		if run.Status != sqlc.RunStatusRunning {
			return run.Status
		}
		time.Sleep(10 * time.Millisecond)
	}
	h.t.Fatal("run did not stop")
	return ""
}

func (h *memHarness) body(n int) string {
	h.t.Helper()
	h.mu.Lock()
	defer h.mu.Unlock()
	if n > len(h.bodies) {
		h.t.Fatalf("only %d requests were made, wanted the %dth", len(h.bodies), n)
	}
	return h.bodies[n-1]
}

func (h *memHarness) requests() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.bodies)
}

// system is the system prompt of the nth request.
func (h *memHarness) system(n int) string {
	h.t.Helper()
	var req struct {
		Messages []struct {
			Role    string `json:"role"`
			Content any    `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal([]byte(h.body(n)), &req); err != nil {
		h.t.Fatal(err)
	}
	if len(req.Messages) == 0 || req.Messages[0].Role != "system" {
		return ""
	}
	text, _ := req.Messages[0].Content.(string)
	return text
}

// toolNames are the tools the nth request offered.
func (h *memHarness) toolNames(n int) []string {
	h.t.Helper()
	var req struct {
		Tools []struct {
			Function struct {
				Name string `json:"name"`
			} `json:"function"`
		} `json:"tools"`
	}
	if err := json.Unmarshal([]byte(h.body(n)), &req); err != nil {
		h.t.Fatal(err)
	}
	var names []string
	for _, tool := range req.Tools {
		names = append(names, tool.Function.Name)
	}
	return names
}

func offers(names []string, tool string) bool {
	for _, name := range names {
		if name == tool {
			return true
		}
	}
	return false
}

func (h *memHarness) personalize(body string) {
	h.t.Helper()
	rec := httptest.NewRecorder()
	h.server.UpdatePersonalization(rec, h.request(http.MethodPatch, body))
	if rec.Code != http.StatusOK {
		h.t.Fatalf("personalize: %d %s", rec.Code, rec.Body)
	}
}

func (h *memHarness) remember(content string) sqlc.UserMemory {
	h.t.Helper()
	change, err := h.notes.Add(context.Background(), h.user.ID, content, memory.By{Source: memory.SourceUser})
	if err != nil {
		h.t.Fatal(err)
	}
	return change.Memory
}

func (h *memHarness) contents(user uuid.UUID) []string {
	h.t.Helper()
	rows, err := h.notes.List(context.Background(), user)
	if err != nil {
		h.t.Fatal(err)
	}
	var out []string
	for _, row := range rows {
		out = append(out, row.Content)
	}
	return out
}

func sameList(t *testing.T, what string, got, want []string) {
	t.Helper()
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("%s: got %q, want %q", what, got, want)
	}
}

func mustContain(t *testing.T, what, text string, wants ...string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(text, want) {
			t.Fatalf("%s lacks %q:\n%s", what, want, text)
		}
	}
}

func mustNotContain(t *testing.T, what, text string, unwanted ...string) {
	t.Helper()
	for _, bad := range unwanted {
		if strings.Contains(text, bad) {
			t.Fatalf("%s has %q:\n%s", what, bad, text)
		}
	}
}

// A conversation takes its instructions and memories once, and every request of
// it, whatever changes meanwhile, is built from that copy: the system prompt is
// the same bytes on every turn.
func TestMemorySnapshotIsTakenOnceAndStaysStable(t *testing.T) {
	h := newMemHarness(t, nil)
	h.personalize(`{"aboutYou":"I am a Go developer","responseStyle":"Be terse"}`)
	h.remember("Lives in Shanghai")
	h.remember("Prefers metric units")

	conv := h.conversation(false)
	if conv.MemorySnapshot != nil {
		t.Fatal("a new conversation already has a snapshot")
	}
	if status := h.send(conv.ID, "hello"); status != sqlc.RunStatusCompleted {
		t.Fatalf("first run: %s", status)
	}
	first := h.system(1)
	mustContain(t, "first prompt", first, "I am a Go developer", "Be terse", "[m1] Lives in Shanghai", "[m2] Prefers metric units")
	if !offers(h.toolNames(1), tools.ToolMemory) {
		t.Fatalf("the memory tool is not offered: %v", h.toolNames(1))
	}

	// Everything changes between the turns.
	h.remember("Works at ACME")
	h.personalize(`{"aboutYou":"Changed text","responseStyle":"Be chatty"}`)
	if _, err := h.notes.Update(context.Background(), h.user.ID, h.firstMemory(), "Lives in Beijing", memory.By{Source: memory.SourceUser}); err != nil {
		t.Fatal(err)
	}

	if status := h.send(conv.ID, "again"); status != sqlc.RunStatusCompleted {
		t.Fatalf("second run: %s", status)
	}
	second := h.system(2)
	if first != second {
		t.Fatalf("the system prompt changed between turns:\n--- first\n%s\n--- second\n%s", first, second)
	}
	mustNotContain(t, "second prompt", second, "ACME", "Changed text", "Beijing")

	stored, err := h.q.GetConversation(context.Background(), sqlc.GetConversationParams{ID: conv.ID, UserID: &h.user.ID})
	if err != nil || stored.MemorySnapshot == nil {
		t.Fatalf("the snapshot was not stored: %v", err)
	}

	// The next conversation reads the list as it is now.
	next := h.conversation(false)
	h.send(next.ID, "hi")
	mustContain(t, "new conversation prompt", h.system(3), "Changed text", "Be chatty", "Lives in Beijing", "Works at ACME")
}

func (h *memHarness) firstMemory() uuid.UUID {
	h.t.Helper()
	rows, err := h.notes.List(context.Background(), h.user.ID)
	if err != nil || len(rows) == 0 {
		h.t.Fatalf("no memories: %v", err)
	}
	return rows[0].ID
}

// A conversation that began before this existed has no snapshot; it takes one at
// its next run, once.
func TestMemoryExistingConversationTakesASnapshotAtItsNextRun(t *testing.T) {
	h := newMemHarness(t, nil)
	conv := h.conversation(false)
	h.send(conv.ID, "one") // taken here, with nothing to say
	h.remember("A later fact")
	if _, err := h.pool.Exec(context.Background(), "UPDATE conversations SET memory_snapshot = NULL WHERE id = $1", conv.ID); err != nil {
		t.Fatal(err)
	}
	h.send(conv.ID, "two")
	mustContain(t, "prompt of the run that took the snapshot", h.system(2), "A later fact")
	h.remember("An even later fact")
	h.send(conv.ID, "three")
	if h.system(2) != h.system(3) {
		t.Fatal("the snapshot was taken again")
	}
}

// The model saves, rewrites and drops notes during a run; the notes it saved
// are known by ids of the conversation, and the prompt of its requests does not
// move while it does.
func TestMemoryToolAddUpdateDelete(t *testing.T) {
	steps := []string{
		openAICall("c1", "memory", `{"op":"add","content":"Uses Neovim"}`),
		openAICall("c2", "memory", `{"op":"update","id":"m2","content":"Uses Neovim every day"}`),
		openAICall("c3", "memory", `{"op":"delete","id":"m1"}`),
		openAIWire.text("done"),
	}
	h := newMemHarness(t, func(n int) string { return steps[min(n, len(steps))-1] })
	h.remember("Lives in Shanghai")

	conv := h.conversation(false)
	if status := h.send(conv.ID, "I use neovim, forget Shanghai"); status != sqlc.RunStatusCompleted {
		t.Fatalf("run: %s", status)
	}
	sameList(t, "memories", h.contents(h.user.ID), []string{"Uses Neovim every day"})
	for n := 2; n <= 4; n++ {
		if h.system(1) != h.system(n) {
			t.Fatalf("the system prompt of request %d differs from the first", n)
		}
	}
	mustContain(t, "result of the add", h.body(2), "Saved as m2")

	events, err := h.notes.Events(context.Background(), h.user.ID, 10)
	if err != nil || len(events) != 3 {
		t.Fatalf("events: %d, %v", len(events), err)
	}
	for _, e := range events {
		if e.Source != memory.SourceModel || e.ConversationID == nil || *e.ConversationID != conv.ID {
			t.Fatalf("event %+v lacks its source or conversation", e)
		}
	}
	rows, _ := h.notes.List(context.Background(), h.user.ID)
	if len(rows) != 1 || rows[0].Source != memory.SourceModel {
		t.Fatalf("the surviving memory: %+v", rows)
	}
}

func (h *memHarness) callTool(user uuid.UUID, conv uuid.UUID, args string) (agent.ToolResult, error) {
	h.t.Helper()
	ctx := agent.WithScope(context.Background(), agent.Scope{RunID: uuid.New(), UserID: user, ConversationID: conv})
	return tools.Memory{Store: h.notes}.Execute(ctx, json.RawMessage(args))
}

// What the tool refuses: ids that are not this conversation's, another user's
// notes, the cap, and malformed notes.
func TestMemoryToolRules(t *testing.T) {
	h := newMemHarness(t, nil)
	ctx := context.Background()
	mine := h.remember("Mine")
	conv := h.conversation(false)
	if _, err := h.notes.EnsureSnapshot(ctx, h.user.ID, conv.ID); err != nil {
		t.Fatal(err)
	}

	other := newUser(t, h.pool, "user")
	theirs, err := h.notes.Add(ctx, other.ID, "Theirs", memory.By{Source: memory.SourceUser})
	if err != nil {
		t.Fatal(err)
	}
	otherConv, err := h.q.CreateConversation(ctx, sqlc.CreateConversationParams{UserID: &other.ID, Title: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.notes.EnsureSnapshot(ctx, other.ID, otherConv.ID); err != nil {
		t.Fatal(err)
	}

	denied := func(what string, user, conversation uuid.UUID, args string) {
		t.Helper()
		if _, err := h.callTool(user, conversation, args); err == nil {
			t.Fatalf("%s was allowed", what)
		}
	}
	// A raw id is not a reference, and neither is a reference outside the snapshot.
	denied("a uuid as id", h.user.ID, conv.ID, fmt.Sprintf(`{"op":"delete","id":%q}`, mine.ID))
	denied("an unknown id", h.user.ID, conv.ID, `{"op":"update","id":"m9","content":"x"}`)
	// The other user's list, by every route the tool has.
	denied("the other user's uuid", h.user.ID, conv.ID, fmt.Sprintf(`{"op":"delete","id":%q}`, theirs.Memory.ID))
	denied("my conversation's ids as the other user", other.ID, conv.ID, `{"op":"delete","id":"m1"}`)
	sameList(t, "my memories", h.contents(h.user.ID), []string{"Mine"})
	sameList(t, "their memories", h.contents(other.ID), []string{"Theirs"})

	denied("an unknown op", h.user.ID, conv.ID, `{"op":"erase"}`)
	denied("an empty note", h.user.ID, conv.ID, `{"op":"add","content":"   "}`)
	denied("a long note", h.user.ID, conv.ID, fmt.Sprintf(`{"op":"add","content":%q}`, strings.Repeat("x", memory.MaxContent+1)))
	denied("a duplicate", h.user.ID, conv.ID, `{"op":"add","content":"mine"}`)

	if res, err := h.callTool(h.user.ID, conv.ID, `{"op":"update","id":"m1","content":"Mine, changed"}`); err != nil {
		t.Fatalf("update of my own: %v", err)
	} else if !strings.Contains(res.Content, "Updated m1") {
		t.Fatalf("result: %s", res.Content)
	}

	// The cap: refused with words that say what to do, and nothing is evicted.
	full := newUser(t, h.pool, "user")
	for i := 0; i < memory.MaxMemories; i++ {
		if _, err := h.notes.Add(ctx, full.ID, fmt.Sprintf("Fact number %d", i), memory.By{Source: memory.SourceUser}); err != nil {
			t.Fatal(err)
		}
	}
	fullConv, err := h.q.CreateConversation(ctx, sqlc.CreateConversationParams{UserID: &full.ID, Title: "x"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = h.callTool(full.ID, fullConv.ID, `{"op":"add","content":"One too many"}`)
	var toolErr *agent.ToolError
	if err == nil || !errorsAs(err, &toolErr) || !strings.Contains(toolErr.Message, "full") || !strings.Contains(toolErr.Message, "delete") {
		t.Fatalf("add at the cap: %v", err)
	}
	if got := h.contents(full.ID); len(got) != memory.MaxMemories || got[0] != "Fact number 0" {
		t.Fatalf("the list changed at the cap: %d", len(got))
	}
	// The page's own add is refused the same way.
	rec := httptest.NewRecorder()
	h.server.CreateMemory(rec, h.as(full, http.MethodPost, `{"content":"One too many"}`))
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "memory_full") {
		t.Fatalf("manual add at the cap: %d %s", rec.Code, rec.Body)
	}
}

func errorsAs(err error, target **agent.ToolError) bool {
	te, ok := err.(*agent.ToolError)
	if ok {
		*target = te
	}
	return ok
}

// Undo puts back what the model changed, while nobody has touched it since.
func TestMemoryUndo(t *testing.T) {
	h := newMemHarness(t, nil)
	ctx := context.Background()
	conv := h.conversation(false)
	original := h.remember("Original")
	if _, err := h.notes.EnsureSnapshot(ctx, h.user.ID, conv.ID); err != nil {
		t.Fatal(err)
	}

	details := func(args string) tools.MemoryDetails {
		t.Helper()
		res, err := h.callTool(h.user.ID, conv.ID, args)
		if err != nil {
			t.Fatal(err)
		}
		return res.Details.(tools.MemoryDetails)
	}
	undo := func(user sqlc.User, event uuid.UUID) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		h.server.UndoMemoryEvent(rec, h.as(user, http.MethodPost, ""), event)
		return rec
	}

	// An add is removed.
	added := details(`{"op":"add","content":"Added by the model"}`)
	if rec := undo(h.user, added.EventID); rec.Code != http.StatusOK {
		t.Fatalf("undo add: %d %s", rec.Code, rec.Body)
	}
	sameList(t, "after undoing the add", h.contents(h.user.ID), []string{"Original"})
	if rec := undo(h.user, added.EventID); rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "already_undone") {
		t.Fatalf("second undo: %d %s", rec.Code, rec.Body)
	}

	// An update is put back — unless the person has edited it since.
	updated := details(`{"op":"update","id":"m1","content":"Rewritten"}`)
	if rec := undo(h.user, updated.EventID); rec.Code != http.StatusOK {
		t.Fatalf("undo update: %d %s", rec.Code, rec.Body)
	}
	sameList(t, "after undoing the update", h.contents(h.user.ID), []string{"Original"})
	updated = details(`{"op":"update","id":"m1","content":"Rewritten again"}`)
	if _, err := h.notes.Update(ctx, h.user.ID, original.ID, "Edited by the person", memory.By{Source: memory.SourceUser}); err != nil {
		t.Fatal(err)
	}
	if rec := undo(h.user, updated.EventID); rec.Code != http.StatusNotFound && rec.Code != http.StatusConflict {
		t.Fatalf("undo over a person's edit: %d %s", rec.Code, rec.Body)
	}
	sameList(t, "the person's edit wins", h.contents(h.user.ID), []string{"Edited by the person"})

	// A delete is restored under the same id.
	deleted := details(`{"op":"delete","id":"m1"}`)
	if len(h.contents(h.user.ID)) != 0 {
		t.Fatal("not deleted")
	}
	// Another user can neither see nor undo it.
	stranger := newUser(t, h.pool, "admin")
	if rec := undo(stranger, deleted.EventID); rec.Code != http.StatusNotFound {
		t.Fatalf("undo by someone else: %d %s", rec.Code, rec.Body)
	}
	if rec := undo(h.user, deleted.EventID); rec.Code != http.StatusOK {
		t.Fatalf("undo delete: %d %s", rec.Code, rec.Body)
	}
	rows, _ := h.notes.List(ctx, h.user.ID)
	if len(rows) != 1 || rows[0].ID != original.ID || rows[0].Content != "Edited by the person" {
		t.Fatalf("restored: %+v", rows)
	}

	// The log lists what the model did, and marks what was undone.
	rec := httptest.NewRecorder()
	h.server.ListMemoryEvents(rec, h.request(http.MethodGet, ""))
	var list MemoryEventList
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil || len(list.Items) == 0 {
		t.Fatalf("events: %v %s", err, rec.Body)
	}
	var undone int
	for _, e := range list.Items {
		if e.UndoneAt != nil {
			undone++
		}
	}
	// The person's edit of the memory took its older entries out of the log.
	if undone != 2 {
		t.Fatalf("undone events: %d", undone)
	}
	rec = httptest.NewRecorder()
	h.server.ListMemoryEvents(rec, h.as(stranger, http.MethodGet, ""))
	if strings.Contains(rec.Body.String(), updated.EventID.String()) {
		t.Fatal("someone else's events are listed")
	}
	// A person deleting a memory takes the words out of the log too.
	if _, err := h.notes.Delete(ctx, h.user.ID, original.ID, memory.By{Source: memory.SourceUser}); err != nil {
		t.Fatal(err)
	}
	events, _ := h.notes.Events(ctx, h.user.ID, 50)
	for _, e := range events {
		if e.MemoryID == original.ID {
			t.Fatalf("the log still holds %+v about a deleted memory", e)
		}
	}
}

// Every memory route acts on the caller's own rows; another user's ids read as
// not found, and no route outside /me names memory.
func TestMemoryOwnershipAndRoutes(t *testing.T) {
	h := newMemHarness(t, nil)
	mine := h.remember("Private")
	stranger := newUser(t, h.pool, "admin")

	rec := httptest.NewRecorder()
	h.server.UpdateMemory(rec, h.as(stranger, http.MethodPatch, `{"content":"Hijacked"}`), mine.ID)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("update of someone else's: %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	h.server.DeleteMemory(rec, h.as(stranger, http.MethodDelete, ""), mine.ID)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("delete of someone else's: %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	h.server.ListMemories(rec, h.as(stranger, http.MethodGet, ""))
	mustNotContain(t, "stranger's list", rec.Body.String(), "Private")
	rec = httptest.NewRecorder()
	h.server.ClearMemories(rec, h.as(stranger, http.MethodDelete, ""))
	sameList(t, "memories after a stranger cleared theirs", h.contents(h.user.ID), []string{"Private"})

	rec = httptest.NewRecorder()
	h.server.CreateMemory(rec, h.request(http.MethodPost, `{"content":"private"}`))
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "memory_duplicate") {
		t.Fatalf("duplicate: %d %s", rec.Code, rec.Body)
	}
	rec = httptest.NewRecorder()
	h.server.CreateMemory(rec, h.request(http.MethodPost, fmt.Sprintf(`{"content":%q}`, strings.Repeat("y", memory.MaxContent+1))))
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "memory_too_long") {
		t.Fatalf("too long: %d %s", rec.Code, rec.Body)
	}
	rec = httptest.NewRecorder()
	h.server.UpdatePersonalization(rec, h.request(http.MethodPatch, fmt.Sprintf(`{"aboutYou":%q}`, strings.Repeat("z", memory.MaxInstructions+1))))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("long instructions: %d", rec.Code)
	}

	// Memory is only ever under /me, and /me is the signed-in group.
	router := NewRouter(&config.Config{}, h.server, auth.NewMiddleware(h.q)).(chi.Routes)
	err := chi.Walk(router, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		lower := strings.ToLower(route)
		if (strings.Contains(lower, "memor") || strings.Contains(lower, "personaliz")) && !strings.HasPrefix(route, "/api/v1/me/") {
			t.Errorf("%s %s exposes memory outside /me", method, route)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// Signed out, the routes answer 401 rather than anything of anyone's.
	anon := httptest.NewRecorder()
	NewRouter(&config.Config{}, h.server, auth.NewMiddleware(h.q)).ServeHTTP(anon, httptest.NewRequest(http.MethodGet, "/api/v1/me/memories", nil))
	if anon.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous list: %d", anon.Code)
	}
}

// The person's switch and the administrator's: with either off there is no
// tool and no memory in the prompt; custom instructions stay.
func TestMemorySwitches(t *testing.T) {
	h := newMemHarness(t, nil)
	h.personalize(`{"aboutYou":"Instruction stays"}`)
	h.remember("Remembered fact")

	// The person's own switch.
	h.personalize(`{"memoryEnabled":false}`)
	off := h.conversation(false)
	h.send(off.ID, "hi")
	mustContain(t, "prompt with memory off", h.system(1), "Instruction stays")
	mustNotContain(t, "prompt with memory off", h.system(1), "Remembered fact")
	if offers(h.toolNames(1), tools.ToolMemory) {
		t.Fatal("the memory tool is offered with the switch off")
	}
	stored, _ := h.q.GetConversation(context.Background(), sqlc.GetConversationParams{ID: off.ID, UserID: &h.user.ID})
	mustNotContain(t, "snapshot", string(stored.MemorySnapshot), "Remembered fact")

	h.personalize(`{"memoryEnabled":true}`)
	on := h.conversation(false)
	h.send(on.ID, "hi")
	mustContain(t, "prompt with memory on", h.system(2), "Remembered fact")
	if !offers(h.toolNames(2), tools.ToolMemory) {
		t.Fatal("the memory tool is not offered with the switch on")
	}

	// Turned off during the conversation: the notes leave the prompt, the tool
	// goes, and the instructions stay.
	h.personalize(`{"memoryEnabled":false}`)
	h.send(on.ID, "again")
	mustNotContain(t, "prompt after switching off", h.system(3), "Remembered fact")
	mustContain(t, "prompt after switching off", h.system(3), "Instruction stays")
	if offers(h.toolNames(3), tools.ToolMemory) {
		t.Fatal("the memory tool is still offered")
	}

	// The administrator's.
	h.personalize(`{"memoryEnabled":true}`)
	admin := newUser(t, h.pool, "admin")
	patch := func(body string) {
		rec := httptest.NewRecorder()
		h.server.UpdateSystemSettings(rec, h.as(admin, http.MethodPatch, body))
		if rec.Code != http.StatusOK {
			t.Fatalf("system settings: %d %s", rec.Code, rec.Body)
		}
	}
	patch(`{"memory":false}`)
	rec := httptest.NewRecorder()
	h.server.GetPersonalization(rec, h.request(http.MethodGet, ""))
	mustContain(t, "personalization", rec.Body.String(), `"memoryAllowed":false`, `"memoryEnabled":true`)
	killed := h.conversation(false)
	h.send(killed.ID, "hi")
	mustNotContain(t, "prompt with the kill switch", h.system(4), "Remembered fact")
	mustContain(t, "prompt with the kill switch", h.system(4), "Instruction stays")
	if offers(h.toolNames(4), tools.ToolMemory) {
		t.Fatal("the memory tool is offered with the kill switch on")
	}
	// The tool refuses a call that was offered before the switch was thrown.
	if _, err := h.callTool(h.user.ID, killed.ID, `{"op":"add","content":"Too late"}`); err == nil {
		t.Fatal("the tool ran with the kill switch on")
	}
	// People can still read and delete their own.
	rec = httptest.NewRecorder()
	h.server.ListMemories(rec, h.request(http.MethodGet, ""))
	mustContain(t, "list under the kill switch", rec.Body.String(), "Remembered fact")

	patch(`{"memory":true}`)
	back := h.conversation(false)
	h.send(back.ID, "hi")
	mustContain(t, "prompt after the kill switch", h.system(5), "Remembered fact")
}

// A temporary chat: no history, no search, no memories (instructions yes), no
// memory tool, no title, and gone after a day — with the usage ledger intact.
func TestTemporaryConversation(t *testing.T) {
	h := newMemHarness(t, nil)
	ctx := context.Background()
	h.personalize(`{"aboutYou":"Instruction applies"}`)
	h.remember("Remembered fact")

	rec := httptest.NewRecorder()
	h.server.CreateConversation(rec, h.request(http.MethodPost, `{"temporary":true}`))
	var created Conversation
	if rec.Code != http.StatusCreated || json.Unmarshal(rec.Body.Bytes(), &created) != nil || !created.Temporary {
		t.Fatalf("create: %d %s", rec.Code, rec.Body)
	}
	normal := h.conversation(false)

	list := func() string {
		rec := httptest.NewRecorder()
		h.server.ListConversations(rec, h.request(http.MethodGet, ""), ListConversationsParams{})
		return rec.Body.String()
	}
	mustContain(t, "history", list(), normal.ID.String())
	mustNotContain(t, "history", list(), created.Id.String())

	rec = httptest.NewRecorder()
	h.server.GetConversation(rec, h.request(http.MethodGet, ""), created.Id)
	if rec.Code != http.StatusOK {
		t.Fatalf("a temporary chat opened by its link: %d", rec.Code)
	}

	if status := h.send(created.Id, "hello"); status != sqlc.RunStatusCompleted {
		t.Fatalf("run: %s", status)
	}
	mustContain(t, "temporary prompt", h.system(1), "Instruction applies")
	mustNotContain(t, "temporary prompt", h.system(1), "Remembered fact")
	if offers(h.toolNames(1), tools.ToolMemory) {
		t.Fatal("a temporary chat is offered the memory tool")
	}
	// A call forced through anyway is refused.
	if _, err := h.callTool(h.user.ID, created.Id, `{"op":"add","content":"Sneaky"}`); err == nil {
		t.Fatal("the memory tool ran in a temporary chat")
	}
	sameList(t, "memories", h.contents(h.user.ID), []string{"Remembered fact"})

	// Never titled, by the hook or on request.
	finished, err := h.q.GetRun(ctx, h.lastRun)
	if err != nil {
		t.Fatal(err)
	}
	h.server.titleConversation(finished)
	stored, _ := h.q.GetConversation(ctx, sqlc.GetConversationParams{ID: created.Id, UserID: &h.user.ID})
	if stored.Title != defaultTitle || h.requests() != 1 {
		t.Fatalf("a temporary chat was titled: %q after %d requests", stored.Title, h.requests())
	}
	rec = httptest.NewRecorder()
	h.server.GenerateConversationTitle(rec, h.request(http.MethodPost, ""), created.Id)
	if rec.Code != http.StatusConflict {
		t.Fatalf("title on request: %d", rec.Code)
	}

	// Cleanup: expired temporary chats go with their rows; nothing else does.
	young, err := h.q.CreateConversation(ctx, sqlc.CreateConversationParams{UserID: &h.user.ID, Title: "young", Temporary: true})
	if err != nil {
		t.Fatal(err)
	}
	oldNormal := h.conversation(false)
	for _, id := range []uuid.UUID{created.Id, oldNormal.ID} {
		if _, err := h.pool.Exec(ctx, "UPDATE conversations SET created_at = now() - interval '25 hours' WHERE id = $1", id); err != nil {
			t.Fatal(err)
		}
	}
	h.server.CleanupNow(ctx)
	exists := func(id uuid.UUID) bool {
		var n int
		if err := h.pool.QueryRow(ctx, "SELECT count(*) FROM conversations WHERE id = $1", id).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n == 1
	}
	if exists(created.Id) {
		t.Fatal("the expired temporary chat survived the cleanup")
	}
	if !exists(young.ID) || !exists(oldNormal.ID) {
		t.Fatal("the cleanup deleted a conversation it should not have")
	}
	var messages, usage, orphaned int
	_ = h.pool.QueryRow(ctx, "SELECT count(*) FROM messages WHERE conversation_id = $1", created.Id).Scan(&messages)
	_ = h.pool.QueryRow(ctx, "SELECT count(*) FROM usage_logs WHERE user_id = $1", h.user.ID).Scan(&usage)
	_ = h.pool.QueryRow(ctx, "SELECT count(*) FROM usage_logs WHERE user_id = $1 AND conversation_id IS NULL", h.user.ID).Scan(&orphaned)
	if messages != 0 || usage == 0 || orphaned != usage {
		t.Fatalf("after cleanup: %d messages, %d usage rows of which %d detached", messages, usage, orphaned)
	}
}
