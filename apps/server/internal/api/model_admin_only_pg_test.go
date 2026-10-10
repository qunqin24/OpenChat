package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/qunqin/openchat/apps/server/internal/auth"
	"github.com/qunqin/openchat/apps/server/internal/db/sqlc"
)

// A model kept for administrators is out of everyone else's picker and refused
// to them by id, while administrators and the server's own tasks keep it.
func TestAdminOnlyModel(t *testing.T) {
	pool := agentTestPool(t)
	ctx := context.Background()
	q := sqlc.New(pool)
	server := &Server{pool: pool, q: q}

	tag := uuid.NewString()
	var member, admin sqlc.User
	for _, u := range []struct {
		into *sqlc.User
		role sqlc.UserRole
	}{{&member, sqlc.UserRoleUser}, {&admin, sqlc.UserRoleAdmin}} {
		if err := pool.QueryRow(ctx, `INSERT INTO users (email, name, role) VALUES ($1, $2, $3) RETURNING id, email, name, role`,
			string(u.role)+tag+"@test.invalid", string(u.role)+tag, u.role).Scan(&u.into.ID, &u.into.Email, &u.into.Name, &u.into.Role); err != nil {
			t.Fatal(err)
		}
	}
	modelID := "kept-" + tag
	channel, err := q.CreateChannel(ctx, sqlc.CreateChannelParams{
		Name: "kept " + tag, Type: sqlc.ChannelTypeOpenaiCompatible, Enabled: true,
		BaseUrl: "http://127.0.0.1:1/v1", ApiKey: "sk-test",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := q.CreateSelectedChannelModels(ctx, sqlc.CreateSelectedChannelModelsParams{
		ChannelID: channel.ID, ModelIds: []string{modelID}, DisplayNames: []string{""}, Sources: []string{"manual"},
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM channels WHERE id = $1", channel.ID)
		_, _ = pool.Exec(context.Background(), "DELETE FROM model_overrides WHERE model_id = $1", modelID)
		_, _ = pool.Exec(context.Background(), "DELETE FROM users WHERE id = ANY($1)", []uuid.UUID{member.ID, admin.ID})
	})

	// Switching it on alone is a decision worth a row, and reads back.
	rec := httptest.NewRecorder()
	server.UpdateModelSettings(rec, httptest.NewRequest(http.MethodPut, "/", strings.NewReader(`{"adminOnly":true}`)), modelID)
	var saved ModelOverride
	if err := json.Unmarshal(rec.Body.Bytes(), &saved); err != nil || rec.Code != http.StatusOK || saved.AdminOnly == nil || !*saved.AdminOnly {
		t.Fatalf("save: %d %s", rec.Code, rec.Body)
	}

	listed := func(as sqlc.User) bool {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		server.ListChatModels(rec, req.WithContext(auth.WithUser(req.Context(), as)))
		var models []ChatModel
		if err := json.Unmarshal(rec.Body.Bytes(), &models); err != nil {
			t.Fatal(err)
		}
		return slices.ContainsFunc(models, func(m ChatModel) bool { return m.Id == modelID })
	}
	if listed(member) {
		t.Error("a member's picker lists the administrators' model")
	}
	if !listed(admin) {
		t.Error("an administrator's picker does not list it")
	}

	memberCtx, adminCtx := auth.WithUser(ctx, member), auth.WithUser(ctx, admin)
	if _, _, err := server.pickChannel(memberCtx, modelID, askerFrom(memberCtx)); !errors.Is(err, errModelUnavailable) {
		t.Errorf("a member asking for it by id: %v, want unavailable", err)
	}
	if _, _, err := server.pickChannel(adminCtx, modelID, askerFrom(adminCtx)); err != nil {
		t.Errorf("an administrator asking for it: %v", err)
	}
	// A title for a member's conversation is the server's task, not the member's.
	if got, _, err := server.taskModel(memberCtx, modelID, nil, false); err != nil || got != modelID {
		t.Errorf("the task model for a member's conversation: %q %v", got, err)
	}

	// Switching it off again, with nothing else decided, leaves no row.
	rec = httptest.NewRecorder()
	server.UpdateModelSettings(rec, httptest.NewRequest(http.MethodPut, "/", strings.NewReader(`{"adminOnly":false}`)), modelID)
	if rec.Code != http.StatusOK {
		t.Fatalf("unset: %d %s", rec.Code, rec.Body)
	}
	if !listed(member) {
		t.Error("the model did not come back for members")
	}
}
