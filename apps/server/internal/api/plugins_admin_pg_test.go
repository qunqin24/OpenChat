package api

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/qunqin24/openchat/apps/server/internal/auth"
	"github.com/qunqin24/openchat/apps/server/internal/config"
	"github.com/qunqin24/openchat/apps/server/internal/db/sqlc"
	"github.com/qunqin24/openchat/apps/server/internal/plugins"
	"github.com/qunqin24/openchat/apps/server/internal/secrets"
)

// An administrator adds a plugin with a header for its service, lists the
// service's tools through it, and sets a tool's permission; the header is
// stored sealed and never comes back.
func TestAdminPlugins(t *testing.T) {
	pool := agentTestPool(t)
	ctx := context.Background()
	q := sqlc.New(pool)
	raw := make([]byte, 32)
	_, _ = rand.Read(raw)
	box, err := secrets.New(base64.StdEncoding.EncodeToString(raw))
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{pool: pool, q: q, cfg: &config.Config{Version: "test"}, secrets: box}
	admin := sqlc.User{ID: uuid.New(), Role: sqlc.UserRoleAdmin, Email: "admin@test.invalid"}
	tag := strings.ReplaceAll(uuid.NewString()[:8], "-", "")

	// The service: two tools, one that only reads; it wants the header.
	server := mcp.NewServer(&mcp.Implementation{Name: "svc", Version: "1"}, nil)
	noop := func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) { return &mcp.CallToolResult{}, nil }
	server.AddTool(&mcp.Tool{Name: "search", InputSchema: map[string]any{"type": "object"}, Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true}}, noop)
	server.AddTool(&mcp.Tool{Name: "create_issue", InputSchema: map[string]any{"type": "object"}}, noop)
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, nil)
	svc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Token") != "sekret" {
			http.Error(w, "no", http.StatusUnauthorized)
			return
		}
		handler.ServeHTTP(w, r)
	}))
	defer svc.Close()

	req := func(body string) *http.Request {
		return httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString(body)).WithContext(auth.WithUser(ctx, admin))
	}
	rec := httptest.NewRecorder()
	s.CreateAdminPlugin(rec, req(fmt.Sprintf(`{"slug":"svc-%s","name":"Svc","mcpUrl":%q,"authType":"header","headerName":"X-Token","headerValue":"sekret"}`, tag, svc.URL)))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rec.Code, rec.Body)
	}
	if strings.Contains(rec.Body.String(), "sekret") {
		t.Fatal("the header value came back")
	}
	var created AdminPlugin
	_ = json.Unmarshal(rec.Body.Bytes(), &created)
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM plugins WHERE id = $1", created.Id) })
	if !created.HeaderSet {
		t.Fatal("headerSet is false")
	}
	var stored []byte
	_ = pool.QueryRow(ctx, "SELECT header_secret FROM plugins WHERE id = $1", created.Id).Scan(&stored)
	if bytes.Contains(stored, []byte("sekret")) {
		t.Fatal("the header value is stored in the clear")
	}
	// Its tools were listed as it was added, without asking.
	var listed int
	_ = pool.QueryRow(ctx, "SELECT count(*) FROM plugin_tools WHERE plugin_id = $1", created.Id).Scan(&listed)
	if listed != 2 {
		t.Fatalf("listed %d tools on adding, want 2", listed)
	}

	rec = httptest.NewRecorder()
	s.RefreshAdminPluginTools(rec, req(""), created.Id)
	if rec.Code != http.StatusOK {
		t.Fatalf("refresh: %d %s", rec.Code, rec.Body)
	}
	var tools PluginToolList
	_ = json.Unmarshal(rec.Body.Bytes(), &tools)
	perms := map[string]PluginToolPermission{}
	for _, tool := range tools.Items {
		perms[tool.Name] = tool.Permission
	}
	if perms["search"] != PluginToolPermissionAllow || perms["create_issue"] != PluginToolPermissionConfirm {
		t.Fatalf("permissions after listing: %v", perms)
	}

	rec = httptest.NewRecorder()
	s.UpdateAdminPluginTool(rec, req(`{"permission":"blocked"}`), created.Id, "create_issue")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("set permission: %d %s", rec.Code, rec.Body)
	}
	// Listed again, a known tool keeps the administrator's permission.
	rec = httptest.NewRecorder()
	s.RefreshAdminPluginTools(rec, req(""), created.Id)
	_ = json.Unmarshal(rec.Body.Bytes(), &tools)
	for _, tool := range tools.Items {
		if tool.Name == "create_issue" && tool.Permission != PluginToolPermissionBlocked {
			t.Fatalf("listing again reset the permission to %s", tool.Permission)
		}
	}

	rec = httptest.NewRecorder()
	s.CreateAdminPlugin(rec, req(fmt.Sprintf(`{"slug":"svc-%s","name":"Again"}`, tag)))
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "slug_taken") {
		t.Fatalf("duplicate short name: %d %s", rec.Code, rec.Body)
	}
	rec = httptest.NewRecorder()
	s.CreateAdminPlugin(rec, req(fmt.Sprintf(`{"slug":"icon-%s","name":"Icon","iconPath":"M0 0\"/><script>"}`, tag)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("an icon path with markup: %d %s", rec.Code, rec.Body)
	}
	keyless := &Server{pool: pool, q: q, cfg: s.cfg, secrets: &secrets.Box{}}
	rec = httptest.NewRecorder()
	keyless.CreateAdminPlugin(rec, req(fmt.Sprintf(`{"slug":"nokey-%s","name":"No key","authType":"header","headerValue":"x"}`, tag)))
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "secret_key_missing") {
		t.Fatalf("header without a key: %d %s", rec.Code, rec.Body)
	}
}

// The built-in plugins are made once, switched off, and cannot be deleted.
func TestBuiltinPlugins(t *testing.T) {
	pool := agentTestPool(t)
	ctx := context.Background()
	q := sqlc.New(pool)
	s := &Server{pool: pool, q: q, cfg: &config.Config{}, secrets: &secrets.Box{}}
	for range 2 {
		if err := plugins.EnsureBuiltins(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	var (
		id      uuid.UUID
		enabled bool
		url     string
		icon    string
		count   int
	)
	if err := pool.QueryRow(ctx, `SELECT id, enabled, mcp_url, icon_path, (SELECT count(*) FROM plugins WHERE builtin = 'github') FROM plugins WHERE builtin = 'github'`).
		Scan(&id, &enabled, &url, &icon, &count); err != nil {
		t.Fatal(err)
	}
	if enabled || count != 1 || url != "https://api.githubcopilot.com/mcp/" || !strings.HasPrefix(icon, "M12") {
		t.Fatalf("github: enabled %v, %d rows, %s", enabled, count, url)
	}
	admin := sqlc.User{ID: uuid.New(), Role: sqlc.UserRoleAdmin, Email: "admin@test.invalid"}
	rec := httptest.NewRecorder()
	s.DeleteAdminPlugin(rec, httptest.NewRequest(http.MethodDelete, "/", nil).WithContext(auth.WithUser(ctx, admin)), id)
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "plugin_builtin") {
		t.Fatalf("deleting a built-in plugin: %d %s", rec.Code, rec.Body)
	}
}
