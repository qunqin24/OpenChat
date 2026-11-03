package api

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/qunqin24/openchat/apps/server/internal/auth"
	"github.com/qunqin24/openchat/apps/server/internal/config"
	"github.com/qunqin24/openchat/apps/server/internal/db/sqlc"
	"github.com/qunqin24/openchat/apps/server/internal/secrets"
)

// fakeOAuthService is an MCP service behind its own OAuth server, as GitHub's
// is, but registering clients on the fly: metadata, registration, an
// authorization that checks PKCE, a code, tokens, a refresh.
type fakeOAuthService struct {
	srv       *httptest.Server
	mu        sync.Mutex
	challenge string
	grants    []string
}

func newFakeOAuthService(t *testing.T) *fakeOAuthService {
	f := &fakeOAuthService{}
	server := mcp.NewServer(&mcp.Implementation{Name: "svc", Version: "1"}, nil)
	server.AddTool(&mcp.Tool{Name: "whoami", InputSchema: map[string]any{"type": "object"}, Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true}},
		func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) { return &mcp.CallToolResult{}, nil })
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, nil)
	mux := http.NewServeMux()
	f.srv = httptest.NewServer(mux)
	base := func() string { return f.srv.URL }
	mux.HandleFunc("/mcp", func(w http.ResponseWriter, r *http.Request) {
		if a := r.Header.Get("Authorization"); a != "Bearer tok-1" && a != "Bearer tok-2" {
			w.Header().Set("WWW-Authenticate", fmt.Sprintf(`Bearer resource_metadata="%s/.well-known/oauth-protected-resource/mcp"`, base()))
			http.Error(w, "no", http.StatusUnauthorized)
			return
		}
		handler.ServeHTTP(w, r)
	})
	mux.HandleFunc("/.well-known/oauth-protected-resource/mcp", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"resource": base() + "/mcp", "authorization_servers": []string{base() + "/oauth"}})
	})
	mux.HandleFunc("/.well-known/oauth-authorization-server/oauth", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer": base() + "/oauth", "authorization_endpoint": base() + "/oauth/authorize", "token_endpoint": base() + "/oauth/token",
			"registration_endpoint": base() + "/oauth/register", "code_challenge_methods_supported": []string{"S256"},
		})
	})
	mux.HandleFunc("/oauth/register", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{"client_id": "dyn-1", "client_secret": "dyn-secret"})
	})
	mux.HandleFunc("/oauth/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		f.mu.Lock()
		defer f.mu.Unlock()
		f.grants = append(f.grants, r.Form.Get("grant_type"))
		switch r.Form.Get("grant_type") {
		case "authorization_code":
			sum := sha256.Sum256([]byte(r.Form.Get("code_verifier")))
			if r.Form.Get("code") != "c1" || base64.RawURLEncoding.EncodeToString(sum[:]) != f.challenge || r.Form.Get("resource") != base()+"/mcp" {
				http.Error(w, `{"error":"invalid_grant"}`, http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"access_token":"tok-1","refresh_token":"ref-1","token_type":"bearer","expires_in":3600}`))
		case "refresh_token":
			if r.Form.Get("refresh_token") != "ref-1" {
				http.Error(w, `{"error":"invalid_grant"}`, http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"access_token":"tok-2","token_type":"bearer","expires_in":3600}`))
		}
	})
	t.Cleanup(f.srv.Close)
	return f
}

func TestPluginOAuthConnect(t *testing.T) {
	pool := agentTestPool(t)
	ctx := context.Background()
	q := sqlc.New(pool)
	raw := make([]byte, 32)
	_, _ = rand.Read(raw)
	box, _ := secrets.New(base64.StdEncoding.EncodeToString(raw))
	s := &Server{pool: pool, q: q, cfg: &config.Config{}, secrets: box}
	svc := newFakeOAuthService(t)
	tag := strings.ReplaceAll(uuid.NewString()[:8], "-", "")

	var admin sqlc.User
	if err := pool.QueryRow(ctx, `INSERT INTO users (email, name, role) VALUES ($1, $2, 'admin') RETURNING id, email, name, role`,
		tag+"@test.invalid", "oauth-"+tag).Scan(&admin.ID, &admin.Email, &admin.Name, &admin.Role); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM users WHERE id = $1", admin.ID) })
	var pluginID uuid.UUID
	if err := pool.QueryRow(ctx, `INSERT INTO plugins (slug, name, mcp_url, auth_type) VALUES ($1, 'Svc', $2, 'oauth') RETURNING id`,
		"svc-"+tag, svc.srv.URL+"/mcp").Scan(&pluginID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM plugins WHERE id = $1", pluginID) })
	req := func(method, target string) *http.Request {
		r := httptest.NewRequest(method, target, nil).WithContext(auth.WithUser(ctx, admin))
		r.Header.Set("Origin", "https://chat.test")
		return r
	}

	// Connect: registered on the fly, PKCE, the resource named.
	rec := httptest.NewRecorder()
	s.ConnectPlugin(rec, req(http.MethodPost, "/"), pluginID)
	if rec.Code != http.StatusOK {
		t.Fatalf("connect: %d %s", rec.Code, rec.Body)
	}
	var out struct{ AuthorizeUrl string }
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	authorize, _ := url.Parse(out.AuthorizeUrl)
	query := authorize.Query()
	if query.Get("client_id") != "dyn-1" || query.Get("code_challenge_method") != "S256" ||
		query.Get("redirect_uri") != "https://chat.test/api/v1/plugins/oauth/callback" || query.Get("resource") != svc.srv.URL+"/mcp" {
		t.Fatalf("authorize URL: %s", out.AuthorizeUrl)
	}
	svc.challenge = query.Get("code_challenge")

	// A state that is not the one given goes nowhere.
	rec = httptest.NewRecorder()
	bad := "nope"
	s.PluginOAuthCallback(rec, req(http.MethodGet, "/"), PluginOAuthCallbackParams{State: &bad, Code: ptr("c1")})
	if loc := rec.Header().Get("Location"); !strings.Contains(loc, "connect_error=state") {
		t.Fatalf("a wrong state: %s", loc)
	}

	// The real one installs the plugin with sealed tokens.
	state := query.Get("state")
	rec = httptest.NewRecorder()
	s.PluginOAuthCallback(rec, req(http.MethodGet, "/"), PluginOAuthCallbackParams{State: &state, Code: ptr("c1")})
	if rec.Code != http.StatusFound || !strings.Contains(rec.Header().Get("Location"), "connected=svc-"+tag) {
		t.Fatalf("callback: %d %s", rec.Code, rec.Header().Get("Location"))
	}
	var access, refresh []byte
	if err := pool.QueryRow(ctx, `SELECT access_token, refresh_token FROM plugin_installs WHERE user_id = $1 AND plugin_id = $2`, admin.ID, pluginID).
		Scan(&access, &refresh); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(access, []byte("tok-1")) || bytes.Contains(refresh, []byte("ref-1")) {
		t.Fatal("tokens are stored in the clear")
	}
	// Taken once.
	rec = httptest.NewRecorder()
	s.PluginOAuthCallback(rec, req(http.MethodGet, "/"), PluginOAuthCallbackParams{State: &state, Code: ptr("c1")})
	if !strings.Contains(rec.Header().Get("Location"), "connect_error=state") {
		t.Fatal("a state was taken twice")
	}

	// The administrator's connection lists the tools.
	rec = httptest.NewRecorder()
	s.RefreshAdminPluginTools(rec, req(http.MethodPost, "/"), pluginID)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "whoami") {
		t.Fatalf("listing with the connection: %d %s", rec.Code, rec.Body)
	}

	// Saving the plugin again with the client id left empty keeps the one
	// registered on the fly.
	rec = httptest.NewRecorder()
	body := fmt.Sprintf(`{"slug":"svc-%s","name":"Svc","mcpUrl":%q,"authType":"oauth","oauthScopes":"read"}`, tag, svc.srv.URL+"/mcp")
	s.UpdateAdminPlugin(rec, httptest.NewRequest(http.MethodPut, "/", strings.NewReader(body)).WithContext(auth.WithUser(ctx, admin)), pluginID)
	if p, _ := q.GetPlugin(ctx, pluginID); rec.Code != http.StatusOK || p.OauthClientID != "dyn-1" || p.OauthScopes != "read" {
		t.Fatalf("saving again: %d, client %q scopes %q", rec.Code, p.OauthClientID, p.OauthScopes)
	}

	// A token about to run out is refreshed.
	if _, err := pool.Exec(ctx, `UPDATE plugin_installs SET token_expires_at = now() WHERE user_id = $1 AND plugin_id = $2`, admin.ID, pluginID); err != nil {
		t.Fatal(err)
	}
	p, _ := q.GetPlugin(ctx, pluginID)
	token, err := s.pluginToken(ctx, admin.ID, p, "https://chat.test"+pluginCallbackPath)
	if err != nil || token != "tok-2" {
		t.Fatalf("refresh: %q %v (grants %v)", token, err, svc.grants)
	}
}

