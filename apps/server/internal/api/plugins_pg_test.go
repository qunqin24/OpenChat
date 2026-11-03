package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/qunqin24/openchat/apps/server/internal/auth"
	"github.com/qunqin24/openchat/apps/server/internal/db/sqlc"
)

// A person sees the plugins that are on and meant for them, installs and
// uninstalls them; one for administrators only, or switched off, is not
// there; one that needs their own account is refused until accounts connect.
func TestPluginsInstall(t *testing.T) {
	pool := agentTestPool(t)
	ctx := context.Background()
	q := sqlc.New(pool)
	s := &Server{pool: pool, q: q}
	tag := strings.ReplaceAll(uuid.NewString()[:8], "-", "")

	var user sqlc.User
	if err := pool.QueryRow(ctx, `INSERT INTO users (email, name, role) VALUES ($1, $2, 'user') RETURNING id, email, name, role`,
		tag+"@test.invalid", "plugins-"+tag).Scan(&user.ID, &user.Email, &user.Name, &user.Role); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM users WHERE id = $1", user.ID) })
	add := func(slug, audience, auth string, enabled bool) uuid.UUID {
		var id uuid.UUID
		if err := pool.QueryRow(ctx, `INSERT INTO plugins (slug, name, audience, auth_type, enabled) VALUES ($1, $1, $2, $3, $4) RETURNING id`,
			slug+"-"+tag, audience, auth, enabled).Scan(&id); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM plugins WHERE id = $1", id) })
		return id
	}
	open := add("open", "all", "none", true)
	staff := add("staff", "admins", "none", true)
	off := add("off", "all", "none", false)
	account := add("account", "all", "oauth", true)
	if _, err := pool.Exec(ctx, `INSERT INTO plugin_tools (plugin_id, name, read_only, permission) VALUES ($1, 'read', true, 'allow'), ($1, 'write', false, 'confirm'), ($1, 'gone', false, 'blocked')`, open); err != nil {
		t.Fatal(err)
	}

	call := func(method string, id uuid.UUID) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		r := httptest.NewRequest(method, "/", nil).WithContext(auth.WithUser(ctx, user))
		switch method {
		case http.MethodGet:
			s.ListPlugins(rec, r)
		case http.MethodPost:
			s.InstallPlugin(rec, r, id)
		default:
			s.UninstallPlugin(rec, r, id)
		}
		return rec
	}
	listed := func() map[uuid.UUID]Plugin {
		var out PluginList
		if err := json.Unmarshal(call(http.MethodGet, uuid.Nil).Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		m := map[uuid.UUID]Plugin{}
		for _, p := range out.Items {
			m[p.Id] = p
		}
		return m
	}

	seen := listed()
	if _, ok := seen[staff]; ok {
		t.Fatal("a plugin for administrators was listed to a member")
	}
	if _, ok := seen[off]; ok {
		t.Fatal("a plugin switched off was listed")
	}
	if p := seen[open]; p.ReadTools != 1 || p.WriteTools != 1 || p.Installed {
		t.Fatalf("open plugin listed as %+v", p)
	}
	if rec := call(http.MethodPost, open); rec.Code != http.StatusOK || !listed()[open].Installed {
		t.Fatalf("install: %d %s", rec.Code, rec.Body)
	}
	if rec := call(http.MethodPost, staff); rec.Code != http.StatusNotFound {
		t.Fatalf("installing an administrators' plugin: %d", rec.Code)
	}
	if rec := call(http.MethodPost, account); rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "plugin_needs_account") {
		t.Fatalf("installing a plugin that needs an account: %d %s", rec.Code, rec.Body)
	}
	if rec := call(http.MethodDelete, open); rec.Code != http.StatusNoContent || listed()[open].Installed {
		t.Fatalf("uninstall: %d", rec.Code)
	}
}
