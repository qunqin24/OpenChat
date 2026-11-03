package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/qunqin24/openchat/apps/server/internal/agent"
	"github.com/qunqin24/openchat/apps/server/internal/auth"
	"github.com/qunqin24/openchat/apps/server/internal/codemode"
	"github.com/qunqin24/openchat/apps/server/internal/config"
	"github.com/qunqin24/openchat/apps/server/internal/db/sqlc"
)

// newFakeItems is an MCP service with an item store: reading is marked
// read-only, deleting is not, and one tool always fails.
func newFakeItems(t *testing.T, deleted *atomic.Int32) string {
	server := mcp.NewServer(&mcp.Implementation{Name: "items", Version: "1"}, nil)
	server.AddTool(&mcp.Tool{
		Name: "get_item", Description: "Get an item by id.",
		InputSchema: map[string]any{"type": "object", "properties": map[string]any{"id": map[string]any{"type": "string"}}, "required": []any{"id"}},
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, func(_ context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var in struct{ ID string }
		_ = json.Unmarshal(req.Params.Arguments, &in)
		return &mcp.CallToolResult{StructuredContent: map[string]any{"id": in.ID, "name": "item " + in.ID}}, nil
	})
	server.AddTool(&mcp.Tool{
		Name: "delete_item", Description: "Delete an item.",
		InputSchema: map[string]any{"type": "object", "properties": map[string]any{"id": map[string]any{"type": "string"}}},
	}, func(_ context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		deleted.Add(1)
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "deleted"}}}, nil
	})
	server.AddTool(&mcp.Tool{Name: "broken", InputSchema: map[string]any{"type": "object"}, Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true}},
		func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: "the shelf is empty"}}}, nil
		})
	srv := httptest.NewServer(mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, nil))
	t.Cleanup(srv.Close)
	return srv.URL
}

func TestPluginCode(t *testing.T) {
	pool := agentTestPool(t)
	ctx := context.Background()
	q := sqlc.New(pool)
	s := &Server{pool: pool, q: q, cfg: &config.Config{}}
	var deleted atomic.Int32
	address := newFakeItems(t, &deleted)
	tag := strings.ReplaceAll(uuid.NewString()[:8], "-", "")
	slug := "items-" + tag

	var user sqlc.User
	if err := pool.QueryRow(ctx, `INSERT INTO users (email, name, role) VALUES ($1, $2, 'admin') RETURNING id, email, name, role`,
		tag+"@test.invalid", "code-"+tag).Scan(&user.ID, &user.Email, &user.Name, &user.Role); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM users WHERE id = $1", user.ID) })
	var pluginID uuid.UUID
	if err := pool.QueryRow(ctx, `INSERT INTO plugins (slug, name, description, mcp_url) VALUES ($1, 'Items', 'An item store', $2) RETURNING id`,
		slug, address).Scan(&pluginID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM plugins WHERE id = $1", pluginID) })
	asUser := auth.WithUser(ctx, user)
	rec := httptest.NewRecorder()
	s.RefreshAdminPluginTools(rec, httptest.NewRequest(http.MethodPost, "/", nil).WithContext(asUser), pluginID)
	if rec.Code != http.StatusOK {
		t.Fatalf("tools: %d %s", rec.Code, rec.Body)
	}
	if err := q.InstallPlugin(ctx, sqlc.InstallPluginParams{UserID: user.ID, PluginID: pluginID}); err != nil {
		t.Fatal(err)
	}

	var conversationID, messageID, runID uuid.UUID
	if err := pool.QueryRow(ctx, `INSERT INTO conversations (user_id) VALUES ($1) RETURNING id`, user.ID).Scan(&conversationID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO messages (conversation_id, role, content) VALUES ($1, 'user', 'hi') RETURNING id`, conversationID).Scan(&messageID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO runs (conversation_id, user_id, input_message_id, model_id) VALUES ($1, $2, $3, 'm') RETURNING id`,
		conversationID, user.ID, messageID).Scan(&runID); err != nil {
		t.Fatal(err)
	}
	scope := agent.Scope{RunID: runID, UserID: user.ID, ConversationID: conversationID}
	runCtx := agent.WithToolCall(agent.WithScope(asUser, scope), "call-1")

	// What the run is offered.
	offered := s.PluginTools(runCtx)
	if len(offered) != 1 || offered[0].Definition().Name != ToolPluginCode {
		t.Fatalf("offered %d tools", len(offered))
	}
	script := func(code string) agent.ToolResult {
		t.Helper()
		raw, _ := json.Marshal(map[string]string{"code": code})
		out, err := pluginCodeTool{s: s}.Execute(runCtx, raw)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	ns := fmt.Sprintf("tools[%q]", slug)

	// Reads run at once, together, and come back as data.
	out := script(fmt.Sprintf(`const [a, b] = await Promise.all([%s.get_item({ id: "1" }), %s.get_item({ id: "2" })])
return [a.name, b.name]`, ns, ns))
	if out.IsError || !strings.Contains(out.Content, `Returned: ["item 1","item 2"]`) || !strings.Contains(out.Content, "Tool calls: 2") {
		t.Fatalf("reads: %+v", out)
	}
	details := out.Details.(PluginCodeProgress)
	if len(details.Calls) != 2 || details.Calls[0].Status != PluginCallProgressStatusDone || len(details.Plugins) != 1 || details.Plugins[0].Name != "Items" {
		t.Fatalf("details: %+v", details)
	}

	// Input that does not fit the schema is refused before it is sent; a
	// failing tool throws its own words.
	out = script(fmt.Sprintf(`const why = []
try { await %s.get_item({}) } catch (e) { why.push(e.message) }
try { await %s.broken({}) } catch (e) { why.push(e.message) }
return why`, ns, ns))
	if !strings.Contains(out.Content, "id") || !strings.Contains(out.Content, "the shelf is empty") {
		t.Fatalf("failures: %s", out.Content)
	}

	// A call that changes something waits for the person. Refused, it throws
	// and nothing is deleted.
	answer := func(decision string) {
		t.Helper()
		var id uuid.UUID
		deadline := time.Now().Add(10 * time.Second)
		for {
			err := pool.QueryRow(ctx, `SELECT id FROM plugin_confirmations WHERE run_id = $1 AND status = 'pending'`, runID).Scan(&id)
			if err == nil {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("no call waits for an answer")
			}
			time.Sleep(20 * time.Millisecond)
		}
		body := strings.NewReader(fmt.Sprintf(`{"decision":%q}`, decision))
		rec := httptest.NewRecorder()
		s.AnswerPluginConfirmation(rec, httptest.NewRequest(http.MethodPost, "/", body).WithContext(asUser), conversationID, id)
		if rec.Code != http.StatusNoContent {
			t.Fatalf("answer %s: %d %s", decision, rec.Code, rec.Body)
		}
		// Answered once: a second answer finds nothing waiting.
		rec = httptest.NewRecorder()
		s.AnswerPluginConfirmation(rec, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"decision":"allow"}`)).WithContext(asUser), conversationID, id)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("a second answer: %d", rec.Code)
		}
	}
	deleteOne := fmt.Sprintf(`try { return await %s.delete_item({ id: "1" }) } catch (e) { return "refused: " + e.message }`, ns)
	done := make(chan agent.ToolResult)
	go func() { done <- script(deleteOne) }()
	answer("deny")
	if out := <-done; !strings.Contains(out.Content, "refused: the user refused") || deleted.Load() != 0 {
		t.Fatalf("refused: %s, %d deleted", out.Content, deleted.Load())
	}

	// Allowed for the conversation: it runs, and the next one does not ask.
	go func() { done <- script(deleteOne) }()
	answer("always")
	if out := <-done; !strings.Contains(out.Content, `Returned: "deleted"`) || deleted.Load() != 1 {
		t.Fatalf("allowed: %s", out.Content)
	}
	if out := script(deleteOne); !strings.Contains(out.Content, `Returned: "deleted"`) || deleted.Load() != 2 {
		t.Fatalf("granted: %s", out.Content)
	}

	// An administrator blocking the tool reaches a script that already started.
	if _, err := pool.Exec(ctx, `UPDATE plugin_tools SET permission = 'blocked' WHERE plugin_id = $1 AND name = 'get_item'`, pluginID); err != nil {
		t.Fatal(err)
	}
	host := s.newPluginHost(runCtx, scope)
	for _, u := range []usableTool{{PluginID: pluginID, Slug: slug, Name: "get_item", PluginName: "Items"}} {
		host.known[u.code().Path()] = u
	}
	if _, err := host.Call(runCtx, codemodeCall(slug, "get_item", `{"id":"1"}`)); err == nil || !strings.Contains(err.Error(), "no longer available") {
		t.Fatalf("blocked meanwhile: %v", err)
	}
	host.close()

	// Every call was recorded, without its input.
	var calls, refused int
	if err := pool.QueryRow(ctx, `SELECT count(*), count(*) FILTER (WHERE outcome = 'denied') FROM plugin_calls WHERE user_id = $1`, user.ID).Scan(&calls, &refused); err != nil {
		t.Fatal(err)
	}
	if calls != 8 || refused != 1 {
		t.Fatalf("%d calls recorded (%d refused), want 8 (1)", calls, refused)
	}

	// The catalogue is taken once and kept; the turn note tells what came since.
	prompt := pluginCatalogPrompt(runCtx, q, offered)
	if !strings.Contains(prompt, "## Plugins") || !strings.Contains(prompt, `tools["`+slug+`"] (Items, 2 tools)`) || !strings.Contains(prompt, ".delete_item(input: { id?: string })") {
		t.Fatalf("catalogue:\n%s", prompt)
	}
	var otherID uuid.UUID
	if err := pool.QueryRow(ctx, `INSERT INTO plugins (slug, name, mcp_url) VALUES ($1, 'Other', $2) RETURNING id`, "other-"+tag, address).Scan(&otherID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM plugins WHERE id = $1", otherID) })
	rec = httptest.NewRecorder()
	s.RefreshAdminPluginTools(rec, httptest.NewRequest(http.MethodPost, "/", nil).WithContext(asUser), otherID)
	_ = q.InstallPlugin(ctx, sqlc.InstallPluginParams{UserID: user.ID, PluginID: otherID})
	if again := pluginCatalogPrompt(runCtx, q, offered); again != prompt {
		t.Fatal("the catalogue changed within the conversation")
	}
	if note, _ := s.PluginNote(runCtx); !strings.Contains(note, "other-"+tag) {
		t.Fatalf("note: %q", note)
	}

	// Listed directly, a plugin's tools are offered one by one.
	if _, err := pool.Exec(ctx, `UPDATE plugins SET direct_tools = true WHERE id IN ($1, $2)`, pluginID, otherID); err != nil {
		t.Fatal(err)
	}
	offered = s.PluginTools(runCtx)
	names := map[string]bool{}
	for _, tool := range offered {
		names[tool.Definition().Name] = true
	}
	if names[ToolPluginCode] || !names[slug+"__delete_item"] {
		t.Fatalf("direct tools: %v", names)
	}
}

func codemodeCall(namespace, name, input string) codemode.Call {
	return codemode.Call{Seq: 1, Tool: codemode.Tool{Namespace: namespace, Name: name}, Input: json.RawMessage(input)}
}
