package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/qunqin24/openchat/apps/server/internal/agent"
	"github.com/qunqin24/openchat/apps/server/internal/auth"
	"github.com/qunqin24/openchat/apps/server/internal/browser"
	"github.com/qunqin24/openchat/apps/server/internal/config"
	"github.com/qunqin24/openchat/apps/server/internal/db/sqlc"
)

// testChromium is a headless Chromium to drive: BROWSER_TEST_EXEC, or one
// Playwright installed. The test skips without one.
func testChromium(t *testing.T) string {
	if path := os.Getenv("BROWSER_TEST_EXEC"); path != "" {
		return path
	}
	home, _ := os.UserHomeDir()
	for _, pattern := range []string{
		filepath.Join(home, "Library/Caches/ms-playwright/chromium_headless_shell-*/chrome-headless-shell-*/chrome-headless-shell"),
		filepath.Join(home, ".cache/ms-playwright/chromium_headless_shell-*/chrome-headless-shell-*/chrome-headless-shell"),
	} {
		if found, _ := filepath.Glob(pattern); len(found) > 0 {
			return found[len(found)-1]
		}
	}
	t.Skip("no headless Chromium (set BROWSER_TEST_EXEC)")
	return ""
}

const browserTestPage = `<!doctype html><title>Board</title>
<form action="/search" role="search"><input name="q" aria-label="Search"><button>Search</button></form>
<form action="/post" method="post"><textarea name="body" aria-label="Message"></textarea><button>Send</button></form>
<form action="/login" method="post"><input name="user" aria-label="User"><input type="password" aria-label="Password"><button>Sign in</button></form>`

func TestBrowserTool(t *testing.T) {
	exec := testChromium(t)
	pool := agentTestPool(t)
	ctx := context.Background()
	q := sqlc.New(pool)
	s := &Server{pool: pool, q: q, cfg: &config.Config{}}
	// No proxy: the test site is on loopback, which the proxy refuses
	// (internal/browser tests that).
	s.browser.pool = browser.NewPool()
	t.Cleanup(s.browser.pool.Close)
	s.browser.pool.Configure(browser.Config{Endpoint: "exec:" + exec})

	var posted atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(browserTestPage)) })
	mux.HandleFunc("/search", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `<title>Found</title><h1>Results for %s</h1>`, r.URL.Query().Get("q"))
	})
	mux.HandleFunc("/post", func(w http.ResponseWriter, r *http.Request) {
		posted.Add(1)
		_, _ = w.Write([]byte(`<title>Posted</title><h1>Posted</h1>`))
	})
	site := httptest.NewServer(mux)
	t.Cleanup(site.Close)

	tag := strings.ReplaceAll(uuid.NewString()[:8], "-", "")
	var user sqlc.User
	if err := pool.QueryRow(ctx, `INSERT INTO users (email, name, role) VALUES ($1, $2, 'admin') RETURNING id, email, name, role`,
		tag+"@test.invalid", "browser-"+tag).Scan(&user.ID, &user.Email, &user.Name, &user.Role); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM users WHERE id = $1", user.ID) })
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
	asUser := auth.WithUser(ctx, user)
	scope := agent.Scope{RunID: runID, UserID: user.ID, ConversationID: conversationID, State: &agent.RunState{}}
	runCtx := agent.WithToolCall(agent.WithScope(asUser, scope), "call-1")

	// Offered only once the person opened the browser in the conversation.
	if offered := s.BrowserTools(runCtx); len(offered) != 0 {
		t.Fatal("offered with the browser closed")
	}
	if _, err := q.SetConversationBrowser(ctx, sqlc.SetConversationBrowserParams{ID: conversationID, UserID: &user.ID, Browser: true}); err != nil {
		t.Fatal(err)
	}
	// A run keeps what it started with: this one still has no browser.
	if offered := s.BrowserTools(runCtx); len(offered) != 0 {
		t.Fatal("the browser appeared mid-run")
	}
	nextRun := agent.WithScope(asUser, agent.Scope{RunID: runID, UserID: user.ID, ConversationID: conversationID, State: &agent.RunState{}})
	if offered := s.BrowserTools(nextRun); len(offered) != 1 {
		t.Fatal("not offered with the browser open")
	}
	child := conversationID
	if offered := s.BrowserTools(agent.WithScope(asUser, agent.Scope{RunID: runID, UserID: user.ID, ConversationID: conversationID, Parent: &child})); len(offered) != 0 {
		t.Fatal("offered to a sub-agent")
	}

	tool := browserTool{s: s}
	act := func(ctx context.Context, args map[string]any) (agent.ToolResult, error) {
		raw, _ := json.Marshal(args)
		return tool.Execute(ctx, raw)
	}
	page, err := act(runCtx, map[string]any{"action": "open", "url": site.URL})
	if err != nil || !strings.Contains(page.Content, "Page: Board") {
		t.Fatalf("open: %v\n%s", err, page.Content)
	}
	refOf := func(content, line string) string {
		t.Helper()
		for _, l := range strings.Split(content, "\n") {
			if strings.Contains(l, line) {
				if i := strings.Index(l, "[ref="); i >= 0 {
					return strings.TrimSuffix(l[i+5:], "]")
				}
			}
		}
		t.Fatalf("no %q in\n%s", line, content)
		return ""
	}

	// A search submits without asking.
	page, err = act(runCtx, map[string]any{"action": "type", "ref": refOf(page.Content, `textbox "Search"`), "text": "shoes", "submit": true})
	if err != nil || !strings.Contains(page.Content, "Results for shoes") {
		t.Fatalf("search: %v\n%s", err, page.Content)
	}
	if page, err = act(runCtx, map[string]any{"action": "back"}); err != nil {
		t.Fatal(err)
	}

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
				t.Error("no action waits for an answer")
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
		rec := httptest.NewRecorder()
		s.AnswerPluginConfirmation(rec, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(fmt.Sprintf(`{"decision":%q}`, decision))).WithContext(asUser), conversationID, id)
		if rec.Code != http.StatusNoContent {
			t.Errorf("answer %s: %d %s", decision, rec.Code, rec.Body)
		}
	}

	// Posting waits for the person. Refused, nothing is sent.
	send := refOf(page.Content, `button "Send"`)
	go answer("deny")
	if _, err := act(runCtx, map[string]any{"action": "click", "ref": send}); !errors.Is(err, errBrowserDenied) || posted.Load() != 0 {
		t.Fatalf("refused: %v, posted %d", err, posted.Load())
	}
	// "Always" sends it, and the site needs no asking after.
	go answer("always")
	if page, err = act(runCtx, map[string]any{"action": "click", "ref": send}); err != nil || posted.Load() != 1 {
		t.Fatalf("always: %v, posted %d", err, posted.Load())
	}
	if page, err = act(runCtx, map[string]any{"action": "back"}); err != nil {
		t.Fatal(err)
	}
	if _, err = act(runCtx, map[string]any{"action": "click", "ref": refOf(page.Content, `button "Send"`)}); err != nil || posted.Load() != 2 {
		t.Fatalf("granted: %v, posted %d", err, posted.Load())
	}
	var outcomes []string
	rows, err := pool.Query(ctx, `SELECT outcome FROM browser_actions WHERE conversation_id = $1 ORDER BY id`, conversationID)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var outcome string
		_ = rows.Scan(&outcome)
		outcomes = append(outcomes, outcome)
	}
	if strings.Join(outcomes, ",") != "denied,allowed,granted" {
		t.Fatalf("recorded %v", outcomes)
	}

	// No password is typed.
	if page, err = act(runCtx, map[string]any{"action": "back"}); err != nil {
		t.Fatal(err)
	}
	if _, err := act(runCtx, map[string]any{"action": "type", "ref": refOf(page.Content, `textbox "Password"`), "text": "x"}); err == nil || !strings.Contains(err.Error(), "password") {
		t.Fatalf("password: %v", err)
	}
	// Nobody to ask: refused at once, on a site not allowed.
	if _, err := pool.Exec(ctx, `DELETE FROM browser_site_grants WHERE conversation_id = $1`, conversationID); err != nil {
		t.Fatal(err)
	}
	unattended := agent.WithToolCall(agent.WithScope(asUser, agent.Scope{RunID: runID, UserID: user.ID, ConversationID: conversationID, Unattended: true}), "call-2")
	if _, err := act(unattended, map[string]any{"action": "click", "ref": refOf(page.Content, `button "Sign in"`)}); err == nil || !strings.Contains(err.Error(), "nobody is here") {
		t.Fatalf("unattended: %v", err)
	}
	// A stale ref says so.
	if _, err := act(runCtx, map[string]any{"action": "click", "ref": "e999"}); err == nil || !strings.Contains(err.Error(), "latest result") {
		t.Fatalf("stale: %v", err)
	}
	// Read gives the text; a screenshot is kept as a tool image.
	if out, err := act(runCtx, map[string]any{"action": "read"}); err != nil || !strings.Contains(out.Content, "URL: "+site.URL) {
		t.Fatalf("read: %v\n%s", err, out.Content)
	}
	if out, err := act(runCtx, map[string]any{"action": "screenshot"}); err != nil || len(out.Images) != 1 {
		t.Fatalf("screenshot: %v %+v", err, out)
	}
}
