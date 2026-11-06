package api

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/qunqin24/openchat/apps/server/internal/agent"
	"github.com/qunqin24/openchat/apps/server/internal/auth"
	"github.com/qunqin24/openchat/apps/server/internal/config"
	"github.com/qunqin24/openchat/apps/server/internal/db/sqlc"
	"github.com/qunqin24/openchat/apps/server/internal/plugins"
	"github.com/qunqin24/openchat/apps/server/internal/secrets"
)

// A plugin that takes each person's own token, as McDonald's China's does:
// tried on the service before it is kept, sealed on the install, and marked
// for a new one when the service later refuses it.
func TestPluginPersonalToken(t *testing.T) {
	pool := agentTestPool(t)
	ctx := context.Background()
	q := sqlc.New(pool)
	raw := make([]byte, 32)
	_, _ = rand.Read(raw)
	box, _ := secrets.New(base64.StdEncoding.EncodeToString(raw))
	s := &Server{pool: pool, q: q, cfg: &config.Config{}, secrets: box}

	// The service takes whichever token is current.
	var good atomic.Value
	good.Store("Bearer good-1")
	server := mcp.NewServer(&mcp.Implementation{Name: "svc", Version: "1"}, nil)
	server.AddTool(&mcp.Tool{Name: "whoami", InputSchema: map[string]any{"type": "object"}, Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true}},
		func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) { return &mcp.CallToolResult{}, nil })
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, nil)
	svc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != good.Load().(string) {
			http.Error(w, `{"code":"401"}`, http.StatusUnauthorized)
			return
		}
		handler.ServeHTTP(w, r)
	}))
	t.Cleanup(svc.Close)

	tag := strings.ReplaceAll(uuid.NewString()[:8], "-", "")
	user := func(role string) sqlc.User {
		t.Helper()
		var u sqlc.User
		if err := pool.QueryRow(ctx, `INSERT INTO users (email, name, role) VALUES ($1, $2, $3) RETURNING id, email, name, role`,
			role+tag+"@test.invalid", role+"-"+tag, role).Scan(&u.ID, &u.Email, &u.Name, &u.Role); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM users WHERE id = $1", u.ID) })
		return u
	}
	admin, member := user("admin"), user("user")
	var pluginID uuid.UUID
	if err := pool.QueryRow(ctx, `INSERT INTO plugins (slug, name, mcp_url, auth_type, token_url) VALUES ($1, 'Svc', $2, 'token', 'https://svc.test/token') RETURNING id`,
		"tok-"+tag, svc.URL).Scan(&pluginID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM plugins WHERE id = $1", pluginID) })

	setToken := func(who sqlc.User, token string) *httptest.ResponseRecorder {
		t.Helper()
		body, _ := json.Marshal(map[string]string{"token": token})
		rec := httptest.NewRecorder()
		s.SetPluginToken(rec, httptest.NewRequest(http.MethodPut, "/", bytes.NewReader(body)).WithContext(auth.WithUser(ctx, who)), pluginID)
		return rec
	}
	installed := func(who sqlc.User) (bool, bool) {
		t.Helper()
		row, err := q.GetUserPlugin(ctx, sqlc.GetUserPluginParams{ID: pluginID, UserID: who.ID, Admin: who.Role == sqlc.UserRoleAdmin})
		if err != nil {
			t.Fatal(err)
		}
		return row.Installed, row.NeedsReconnect
	}

	// Not installed without a token.
	rec := httptest.NewRecorder()
	s.InstallPlugin(rec, httptest.NewRequest(http.MethodPost, "/", nil).WithContext(auth.WithUser(ctx, member)), pluginID)
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "plugin_needs_account") {
		t.Fatalf("installing without a token: %d %s", rec.Code, rec.Body)
	}

	// One that cannot go in a header is not tried; one the service refuses is not kept.
	if rec := setToken(member, "two words"); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "invalid_token") {
		t.Fatalf("a token with a space: %d %s", rec.Code, rec.Body)
	}
	if rec := setToken(member, "wrong"); rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "plugin_token_refused") {
		t.Fatalf("a refused token: %d %s", rec.Code, rec.Body)
	}
	if on, _ := installed(member); on {
		t.Fatal("installed with a refused token")
	}

	// A member's good token installs it, sealed, but does not list the tools:
	// that is the administrator's first connection.
	if rec := setToken(member, "good-1"); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"tokenUrl":"https://svc.test/token"`) {
		t.Fatalf("a good token: %d %s", rec.Code, rec.Body)
	}
	var sealed []byte
	if err := pool.QueryRow(ctx, `SELECT access_token FROM plugin_installs WHERE user_id = $1 AND plugin_id = $2`, member.ID, pluginID).Scan(&sealed); err != nil {
		t.Fatal(err)
	}
	if len(sealed) == 0 || bytes.Contains(sealed, []byte("good-1")) {
		t.Fatal("the token is missing or stored in the clear")
	}
	if p, _ := q.GetPlugin(ctx, pluginID); p.ToolsFetchedAt.Valid {
		t.Fatal("a member's token listed the tools")
	}
	if rec := setToken(admin, "good-1"); rec.Code != http.StatusOK {
		t.Fatalf("the administrator's token: %d %s", rec.Code, rec.Body)
	}
	if tools, _ := q.ListPluginTools(ctx, pluginID); len(tools) != 1 || tools[0].Name != "whoami" {
		t.Fatalf("tools after the administrator connected: %+v", tools)
	}
	if usable, _ := q.ListUsablePluginTools(ctx, member.ID); !hasTool(usable, pluginID) {
		t.Fatal("the member's tools are not usable")
	}

	// The service stops taking it: a call says so and marks the install,
	// whose tools are then not offered.
	good.Store("Bearer good-2")
	h := s.newPluginHost(ctx, agent.Scope{UserID: member.ID})
	defer h.close()
	conn, err := h.conn(ctx, pluginID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Call(ctx, "whoami", nil); !errors.Is(err, plugins.ErrRefused) {
		t.Fatalf("a call with a refused token: %v", err)
	}
	if !h.refusedToken(ctx, pluginID) {
		t.Fatal("the refusal was not taken as the person's token")
	}
	if on, again := installed(member); !on || !again {
		t.Fatalf("installed %v, needs a new token %v", on, again)
	}
	if usable, _ := q.ListUsablePluginTools(ctx, member.ID); hasTool(usable, pluginID) {
		t.Fatal("tools offered with a refused token")
	}

	// A new one mends it.
	if rec := setToken(member, "good-2"); rec.Code != http.StatusOK {
		t.Fatalf("a new token: %d %s", rec.Code, rec.Body)
	}
	if on, again := installed(member); !on || again {
		t.Fatalf("after a new token: installed %v, needs a new token %v", on, again)
	}

	// An administrator points the plugin at another service: the token,
	// checked against the first, is not sent there - not listed, not called -
	// and the person is asked for one again.
	var seen atomic.Value
	moved := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if a := r.Header.Get("Authorization"); a != "" {
			seen.Store(a)
		}
		handler.ServeHTTP(w, r)
	}))
	t.Cleanup(moved.Close)
	if _, err := pool.Exec(ctx, `UPDATE plugins SET mcp_url = $2 WHERE id = $1`, pluginID, moved.URL); err != nil {
		t.Fatal(err)
	}
	if on, again := installed(member); !on || !again {
		t.Fatalf("after the address changed: installed %v, needs a new token %v", on, again)
	}
	if usable, _ := q.ListUsablePluginTools(ctx, member.ID); hasTool(usable, pluginID) {
		t.Fatal("tools offered with a token for another service")
	}
	h2 := s.newPluginHost(ctx, agent.Scope{UserID: member.ID})
	defer h2.close()
	if _, err := h2.conn(ctx, pluginID); !errors.Is(err, errPluginNotConnected) {
		t.Fatalf("opening the moved plugin: %v", err)
	}
	if got, _ := seen.Load().(string); got != "" {
		t.Fatalf("the new service received %q", got)
	}
}

func hasTool(rows []sqlc.ListUsablePluginToolsRow, pluginID uuid.UUID) bool {
	for _, row := range rows {
		if row.PluginID == pluginID {
			return true
		}
	}
	return false
}
