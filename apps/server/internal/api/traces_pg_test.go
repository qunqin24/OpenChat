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

	"github.com/qunqin24/openchat/apps/server/internal/agent"
	"github.com/qunqin24/openchat/apps/server/internal/auth"
	"github.com/qunqin24/openchat/apps/server/internal/db/dbtest"
	"github.com/qunqin24/openchat/apps/server/internal/db/sqlc"
)

// The 轨迹 view: an administrator's requests are recorded as sent and read
// back from their own conversation; a member's are not recorded, and nobody's
// conversation but the caller's can be read.
func TestConversationTrace(t *testing.T) {
	pool := dbtest.Pool(t, "api_traces")
	ctx := context.Background()
	q := sqlc.New(pool)

	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, `data: {"id":"r","model":"served-m","choices":[{"index":0,"delta":{"role":"assistant","content":"Hello there."},"finish_reason":null}]}`+"\n\n")
		_, _ = fmt.Fprint(w, `data: {"id":"r","model":"served-m","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":12,"completion_tokens":3}}`+"\n\n")
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(endpoint.Close)

	tag := uuid.NewString()
	newUser := func(name string, role sqlc.UserRole) sqlc.User {
		var u sqlc.User
		if err := pool.QueryRow(ctx, `INSERT INTO users (email, name, role) VALUES ($1, $2, $3) RETURNING id, email, name, role`,
			name+tag+"@test.invalid", name+tag, role).Scan(&u.ID, &u.Email, &u.Name, &u.Role); err != nil {
			t.Fatal(err)
		}
		return u
	}
	admin, member := newUser("admin", sqlc.UserRoleAdmin), newUser("member", sqlc.UserRoleUser)
	modelID := "trace-" + tag
	channel, err := q.CreateChannel(ctx, sqlc.CreateChannelParams{
		Name: "fake " + tag, Type: sqlc.ChannelTypeOpenaiCompatible, Enabled: true,
		BaseUrl: endpoint.URL + "/v1", ApiKey: "sk-test",
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
		bg := context.Background()
		_, _ = pool.Exec(bg, "DELETE FROM users WHERE id = ANY($1)", []uuid.UUID{admin.ID, member.ID})
		_, _ = pool.Exec(bg, "DELETE FROM channels WHERE id = $1", channel.ID)
		_, _ = pool.Exec(bg, "DELETE FROM app_settings WHERE key = $1", settingRequestTraces)
	})
	setTraces := func(on bool) {
		t.Helper()
		if err := q.SetSetting(ctx, sqlc.SetSettingParams{Key: settingRequestTraces, Value: []byte(fmt.Sprint(on))}); err != nil {
			t.Fatal(err)
		}
	}

	runner := agent.NewRunner(pool, agent.NewHub(), NewModelResolver(pool), nil, agent.Options{
		PartialInterval: 5 * time.Millisecond,
		SystemPrompt:    func(context.Context, time.Time, []agent.Tool) string { return "You are a test." },
		Tracer:          NewTracer(pool),
	})
	t.Cleanup(func() { _ = runner.Shutdown(context.Background()) })
	server := &Server{pool: pool, q: q, runner: runner}

	// chat sends one message in a new conversation of user's and waits for its answer.
	chat := func(user sqlc.User) sqlc.Conversation {
		t.Helper()
		conv, err := q.CreateConversation(ctx, sqlc.CreateConversationParams{UserID: &user.ID, Title: "c", Model: &modelID})
		if err != nil {
			t.Fatal(err)
		}
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"content":"hi"}`))
		server.CreateMessage(rec, req.WithContext(auth.WithUser(req.Context(), user)), conv.ID)
		if rec.Code != http.StatusAccepted {
			t.Fatalf("send: %d %s", rec.Code, rec.Body)
		}
		deadline := time.Now().Add(10 * time.Second)
		for {
			var status sqlc.RunStatus
			_ = pool.QueryRow(ctx, `SELECT status FROM runs WHERE conversation_id = $1`, conv.ID).Scan(&status)
			if status != "" && status != sqlc.RunStatusRunning {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("the run did not finish")
			}
			time.Sleep(20 * time.Millisecond)
		}
		return conv
	}
	trace := func(user sqlc.User, conv uuid.UUID) (*httptest.ResponseRecorder, ConversationTrace) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		server.GetConversationTrace(rec, req.WithContext(auth.WithUser(req.Context(), user)), conv)
		var out ConversationTrace
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		return rec, out
	}

	// Off by default: nothing is recorded, and there is no view.
	_, _ = pool.Exec(ctx, "DELETE FROM app_settings WHERE key = $1", settingRequestTraces)
	quiet := chat(admin)
	var recorded int
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM request_traces WHERE conversation_id = $1`, quiet.ID).Scan(&recorded)
	if recorded != 0 {
		t.Fatalf("%d traces recorded while off", recorded)
	}
	if rec, _ := trace(admin, quiet.ID); rec.Code != http.StatusNotFound {
		t.Fatalf("view while off: %d", rec.Code)
	}

	setTraces(true)
	adminConv := chat(admin)
	memberConv := chat(member)

	t.Run("an administrator's request is recorded as sent", func(t *testing.T) {
		rec, out := trace(admin, adminConv.ID)
		if rec.Code != http.StatusOK || len(out.Requests) != 1 {
			t.Fatalf("%d %s", rec.Code, rec.Body)
		}
		got := out.Requests[0]
		if got.Kind != "chat" || got.Status != "ok" || got.ModelId != modelID || got.ServedModel != "served-m" || got.OutputTokens != 3 {
			t.Errorf("figures = %+v", got)
		}
		raw, _ := json.Marshal(got.Request)
		if !strings.Contains(string(raw), "You are a test.") || !strings.Contains(string(raw), "hi") {
			t.Errorf("request = %s", raw)
		}
		resp, _ := json.Marshal(got.Response)
		if !strings.Contains(string(resp), "Hello there.") {
			t.Errorf("response = %s", resp)
		}
	})

	t.Run("a member's requests are not recorded", func(t *testing.T) {
		var n int
		_ = pool.QueryRow(ctx, `SELECT count(*) FROM request_traces WHERE conversation_id = $1`, memberConv.ID).Scan(&n)
		if n != 0 {
			t.Errorf("%d traces of a member", n)
		}
	})

	t.Run("nobody else's conversation", func(t *testing.T) {
		if rec, _ := trace(admin, memberConv.ID); rec.Code != http.StatusNotFound {
			t.Errorf("admin reading a member's: %d", rec.Code)
		}
	})
}
