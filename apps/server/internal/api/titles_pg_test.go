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

	"github.com/google/uuid"

	"github.com/qunqin/openchat/apps/server/internal/agent"
	"github.com/qunqin/openchat/apps/server/internal/auth"
	"github.com/qunqin/openchat/apps/server/internal/db/sqlc"
)

// A conversation's first answer gets it a title from the task model, in the
// background; a title the user chose is left alone.
func TestTitleAfterFirstAnswer(t *testing.T) {
	pool := agentTestPool(t)
	ctx := context.Background()
	q := sqlc.New(pool)

	var (
		mu         sync.Mutex
		titleAsked []string // the model each title request named
	)
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body struct {
			Model string `json:"model"`
		}
		_ = json.Unmarshal(raw, &body)
		text := "An answer about futures."
		if strings.Contains(string(raw), "Write a short title") {
			mu.Lock()
			titleAsked = append(titleAsked, body.Model)
			mu.Unlock()
			text = `  “期货入门”。 `
		}
		chunk, _ := json.Marshal(map[string]any{"id": "r", "model": body.Model, "choices": []any{
			map[string]any{"index": 0, "delta": map[string]any{"role": "assistant", "content": text}, "finish_reason": "stop"},
		}})
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", chunk)
	}))
	t.Cleanup(endpoint.Close)

	tag := uuid.NewString()
	var user sqlc.User
	if err := pool.QueryRow(ctx, `INSERT INTO users (email, name) VALUES ($1, $2) RETURNING id, email, name, role`,
		tag+"@test.invalid", "title-"+tag).Scan(&user.ID, &user.Email, &user.Name, &user.Role); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM users WHERE id = $1", user.ID) })
	chatModel, taskModel := "chat-model-"+tag, "task-model-"+tag
	channel, err := q.CreateChannel(ctx, sqlc.CreateChannelParams{
		Name: "fake " + tag, Type: sqlc.ChannelTypeOpenaiCompatible, Enabled: true,
		BaseUrl: endpoint.URL + "/v1", ApiKey: "sk-test",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := q.CreateSelectedChannelModels(ctx, sqlc.CreateSelectedChannelModelsParams{
		ChannelID: channel.ID, ModelIds: []string{chatModel, taskModel}, DisplayNames: []string{"", ""}, Sources: []string{"manual", "manual"},
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM channels WHERE id = $1", channel.ID)
		_, _ = pool.Exec(context.Background(), "DELETE FROM app_settings WHERE key = $1", settingTaskModel)
	})

	runner := agent.NewRunner(pool, agent.NewHub(), NewModelResolver(pool),
		func(context.Context) []agent.Tool { return nil },
		agent.Options{SystemPrompt: NewSystemPrompt(pool), PartialInterval: 5 * time.Millisecond})
	t.Cleanup(func() { _ = runner.Shutdown(context.Background()) })
	server := &Server{pool: pool, q: q, runner: runner}
	runner.AfterRun(server.TitleAfterRun)
	withUser := func(r *http.Request) *http.Request { return r.WithContext(auth.WithUser(r.Context(), user)) }
	post := func(body string) *http.Request {
		return withUser(httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body)))
	}

	// The task model is set, and is the one asked for the title.
	rec := httptest.NewRecorder()
	server.UpdateSystemSettings(rec, post(fmt.Sprintf(`{"taskModel":%q}`, taskModel)))
	if rec.Code != http.StatusOK {
		t.Fatalf("set task model: %d %s", rec.Code, rec.Body)
	}
	rec = httptest.NewRecorder()
	server.UpdateSystemSettings(rec, post(`{"taskModel":"no-such-model"}`))
	if rec.Code == http.StatusOK {
		t.Fatal("an unavailable task model was accepted")
	}

	titleOf := func(id uuid.UUID, want string) string {
		t.Helper()
		deadline := time.Now().Add(10 * time.Second)
		for {
			conv, err := q.GetConversation(ctx, sqlc.GetConversationParams{ID: id, UserID: &user.ID})
			if err != nil {
				t.Fatal(err)
			}
			if conv.Title == want || time.Now().After(deadline) {
				return conv.Title
			}
			time.Sleep(25 * time.Millisecond)
		}
	}

	conv, err := q.CreateConversation(ctx, sqlc.CreateConversationParams{UserID: &user.ID, Title: defaultTitle})
	if err != nil {
		t.Fatal(err)
	}
	rec = httptest.NewRecorder()
	server.CreateMessage(rec, post(fmt.Sprintf(`{"content":"用大白话解释一下：期货","model":%q}`, chatModel)), conv.ID)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("send: %d %s", rec.Code, rec.Body)
	}
	if got := titleOf(conv.ID, "期货入门"); got != "期货入门" {
		t.Fatalf("title %q", got)
	}
	mu.Lock()
	if len(titleAsked) != 1 || titleAsked[0] != taskModel {
		t.Fatalf("title requests named %v, want [%s]", titleAsked, taskModel)
	}
	mu.Unlock()

	// A conversation the user named keeps its name.
	named, err := q.CreateConversation(ctx, sqlc.CreateConversationParams{UserID: &user.ID, Title: "My own name"})
	if err != nil {
		t.Fatal(err)
	}
	rec = httptest.NewRecorder()
	server.CreateMessage(rec, post(fmt.Sprintf(`{"content":"hi","model":%q}`, chatModel)), named.ID)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("send: %d %s", rec.Code, rec.Body)
	}
	time.Sleep(500 * time.Millisecond)
	if got := titleOf(named.ID, "My own name"); got != "My own name" {
		t.Fatalf("a named conversation was retitled to %q", got)
	}
	mu.Lock()
	if len(titleAsked) != 1 {
		t.Fatalf("a named conversation asked for a title: %v", titleAsked)
	}
	mu.Unlock()

	// On request, any conversation is (re)named — the user's own name too.
	rec = httptest.NewRecorder()
	server.GenerateConversationTitle(rec, post(``), named.ID)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "期货入门") {
		t.Fatalf("title on request: %d %s", rec.Code, rec.Body)
	}
	mu.Lock()
	if len(titleAsked) != 2 || titleAsked[1] != taskModel {
		t.Fatalf("title on request asked %v", titleAsked)
	}
	mu.Unlock()
	empty, err := q.CreateConversation(ctx, sqlc.CreateConversationParams{UserID: &user.ID, Title: defaultTitle})
	if err != nil {
		t.Fatal(err)
	}
	rec = httptest.NewRecorder()
	server.GenerateConversationTitle(rec, post(``), empty.ID)
	if rec.Code != http.StatusConflict {
		t.Fatalf("titling an empty conversation: %d %s", rec.Code, rec.Body)
	}
}
