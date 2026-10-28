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

// Scheduled tasks over their handlers, the scheduler and the model's tool,
// against a fake OpenAI-compatible endpoint. A database of its own: a
// scheduler pass takes every due task there is.
func TestScheduledTasks(t *testing.T) {
	pool := dbtest.Pool(t, "api_tasks")
	ctx := context.Background()
	q := sqlc.New(pool)

	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, `data: {"id":"r","model":"m","choices":[{"index":0,"delta":{"role":"assistant","content":"Done."},"finish_reason":null}]}`+"\n\n")
		_, _ = fmt.Fprint(w, `data: {"id":"r","model":"m","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`+"\n\n")
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(endpoint.Close)

	tag := uuid.NewString()
	newUser := func(name string) sqlc.User {
		var u sqlc.User
		if err := pool.QueryRow(ctx, `INSERT INTO users (email, name) VALUES ($1, $2) RETURNING id, email, name, role`,
			name+tag+"@test.invalid", name+tag).Scan(&u.ID, &u.Email, &u.Name, &u.Role); err != nil {
			t.Fatal(err)
		}
		return u
	}
	alice, mallory := newUser("alice"), newUser("mallory")
	modelID := "tasks-" + tag
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
		_, _ = pool.Exec(bg, "DELETE FROM users WHERE id = ANY($1)", []uuid.UUID{alice.ID, mallory.ID})
		_, _ = pool.Exec(bg, "DELETE FROM channels WHERE id = $1", channel.ID)
		_, _ = pool.Exec(bg, "DELETE FROM app_settings WHERE key = $1", settingScheduledTasks)
	})

	runner := agent.NewRunner(pool, agent.NewHub(), NewModelResolver(pool), nil, agent.Options{PartialInterval: 5 * time.Millisecond})
	t.Cleanup(func() { _ = runner.Shutdown(context.Background()) })
	server := &Server{pool: pool, q: q, runner: runner}

	call := func(user sqlc.User, body string, handler func(http.ResponseWriter, *http.Request)) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
		handler(rec, req.WithContext(auth.WithUser(req.Context(), user)))
		return rec
	}
	create := func(user sqlc.User, body string) *httptest.ResponseRecorder {
		return call(user, body, server.CreateTask)
	}
	decode := func(rec *httptest.ResponseRecorder) Task {
		t.Helper()
		var task Task
		if err := json.Unmarshal(rec.Body.Bytes(), &task); err != nil {
			t.Fatalf("%d %s: %v", rec.Code, rec.Body, err)
		}
		return task
	}
	daily := fmt.Sprintf(`{"title":"早报","prompt":"Summarise the news.","model":%q,"schedule":{"kind":"daily","time":"08:30","timeZone":"Asia/Shanghai"}}`, modelID)
	// Waits for the conversation's run to finish.
	settle := func(conversationID uuid.UUID) sqlc.Run {
		t.Helper()
		deadline := time.Now().Add(10 * time.Second)
		for {
			var run sqlc.Run
			err := pool.QueryRow(ctx, `SELECT id, status FROM runs WHERE conversation_id = $1 ORDER BY created_at DESC LIMIT 1`, conversationID).Scan(&run.ID, &run.Status)
			if err == nil && run.Status != sqlc.RunStatusRunning {
				return run
			}
			if time.Now().After(deadline) {
				t.Fatalf("no finished run in %s (%v)", conversationID, err)
			}
			time.Sleep(20 * time.Millisecond)
		}
	}

	var first Task
	t.Run("a task gets its own conversation and its next time", func(t *testing.T) {
		rec := create(alice, daily)
		if rec.Code != http.StatusCreated {
			t.Fatalf("create: %d %s", rec.Code, rec.Body)
		}
		first = decode(rec)
		if first.Status != TaskActive || first.NextRunAt == nil || first.Schedule.Time == nil || *first.Schedule.Time != "08:30" {
			t.Fatalf("task = %+v", first)
		}
		local := first.NextRunAt.In(mustLoad(t, "Asia/Shanghai"))
		if local.Hour() != 8 || local.Minute() != 30 || !first.NextRunAt.After(time.Now()) {
			t.Errorf("next run %v", local)
		}
		conv, err := q.GetConversation(ctx, sqlc.GetConversationParams{ID: first.ConversationId, UserID: &alice.ID})
		if err != nil || conv.Title != "早报" || conv.Model == nil || *conv.Model != modelID {
			t.Errorf("conversation = %+v, %v", conv, err)
		}
	})

	t.Run("bad schedules are refused", func(t *testing.T) {
		for _, body := range []string{
			`{"title":"x","prompt":"y","schedule":{"kind":"once","at":"2020-01-01T00:00:00Z","timeZone":"UTC"}}`,
			`{"title":"x","prompt":"y","schedule":{"kind":"weekly","time":"08:00","timeZone":"UTC"}}`,
			`{"title":"x","prompt":"y","schedule":{"kind":"daily","time":"08:00","timeZone":"Mars/Base"}}`,
		} {
			if rec := create(alice, body); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "invalid_schedule") {
				t.Errorf("%s: %d %s", body, rec.Code, rec.Body)
			}
		}
	})

	t.Run("nobody else's task is there", func(t *testing.T) {
		rec := call(mallory, `{"enabled":false}`, func(w http.ResponseWriter, r *http.Request) { server.UpdateTask(w, r, first.Id) })
		if rec.Code != http.StatusNotFound {
			t.Errorf("update: %d", rec.Code)
		}
		rec = call(mallory, ``, func(w http.ResponseWriter, r *http.Request) { server.DeleteTask(w, r, first.Id) })
		if rec.Code != http.StatusNotFound {
			t.Errorf("delete: %d", rec.Code)
		}
		var list TaskList
		rec = call(mallory, ``, server.ListTasks)
		_ = json.Unmarshal(rec.Body.Bytes(), &list)
		if len(list.Items) != 0 {
			t.Errorf("mallory sees %d tasks", len(list.Items))
		}
	})

	t.Run("pause and resume", func(t *testing.T) {
		paused := decode(call(alice, `{"enabled":false}`, func(w http.ResponseWriter, r *http.Request) { server.UpdateTask(w, r, first.Id) }))
		if paused.Status != TaskPaused || paused.NextRunAt != nil {
			t.Fatalf("paused = %+v", paused)
		}
		resumed := decode(call(alice, `{"enabled":true}`, func(w http.ResponseWriter, r *http.Request) { server.UpdateTask(w, r, first.Id) }))
		if resumed.Status != TaskActive || resumed.NextRunAt == nil {
			t.Fatalf("resumed = %+v", resumed)
		}
	})

	t.Run("the limit counts tasks still to run", func(t *testing.T) {
		for i := 1; i < maxEnabledTasks; i++ {
			if rec := create(alice, daily); rec.Code != http.StatusCreated {
				t.Fatalf("task %d: %d %s", i, rec.Code, rec.Body)
			}
		}
		if rec := create(alice, daily); rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "too_many_tasks") {
			t.Fatalf("over the limit: %d %s", rec.Code, rec.Body)
		}
		// A paused one makes room, and resuming it then has none.
		decode(call(alice, `{"enabled":false}`, func(w http.ResponseWriter, r *http.Request) { server.UpdateTask(w, r, first.Id) }))
		extra := decode(create(alice, daily))
		if rec := call(alice, `{"enabled":true}`, func(w http.ResponseWriter, r *http.Request) { server.UpdateTask(w, r, first.Id) }); rec.Code != http.StatusConflict {
			t.Fatalf("resume over the limit: %d %s", rec.Code, rec.Body)
		}
		if rec := call(alice, ``, func(w http.ResponseWriter, r *http.Request) { server.DeleteTask(w, r, extra.Id) }); rec.Code != http.StatusNoContent {
			t.Fatalf("delete: %d", rec.Code)
		}
		decode(call(alice, `{"enabled":true}`, func(w http.ResponseWriter, r *http.Request) { server.UpdateTask(w, r, first.Id) }))
	})

	t.Run("a due task runs once, and its next time moves on", func(t *testing.T) {
		now := time.Now()
		if _, err := pool.Exec(ctx, `UPDATE scheduled_tasks SET next_run_at = $2 WHERE id = $1`, first.Id, now.Add(-time.Minute)); err != nil {
			t.Fatal(err)
		}
		server.RunDueTasks(ctx, now)
		server.RunDueTasks(ctx, now) // a second pass finds nothing due
		run := settle(first.ConversationId)
		if run.Status != sqlc.RunStatusCompleted {
			t.Fatalf("run %s", run.Status)
		}
		var runs int
		_ = pool.QueryRow(ctx, `SELECT count(*) FROM runs WHERE conversation_id = $1`, first.ConversationId).Scan(&runs)
		if runs != 1 {
			t.Errorf("%d runs, want 1", runs)
		}
		task, err := q.GetTask(ctx, sqlc.GetTaskParams{ID: first.Id, UserID: alice.ID})
		if err != nil {
			t.Fatal(err)
		}
		if task.LastRunID == nil || *task.LastRunID != run.ID || task.LastError != nil || !task.NextRunAt.Time.After(now) {
			t.Errorf("task after its turn = %+v", task)
		}
		rows, _ := q.ListTranscript(ctx, first.ConversationId)
		if len(rows) < 2 || rows[0].Role != sqlc.MessageRoleUser || rows[0].Content != "Summarise the news." {
			t.Errorf("transcript = %+v", rows)
		}
	})

	t.Run("a turn missed by long is skipped", func(t *testing.T) {
		now := time.Now()
		if _, err := pool.Exec(ctx, `UPDATE scheduled_tasks SET next_run_at = $2 WHERE id = $1`, first.Id, now.Add(-2*time.Hour)); err != nil {
			t.Fatal(err)
		}
		server.RunDueTasks(ctx, now)
		task, _ := q.GetTask(ctx, sqlc.GetTaskParams{ID: first.Id, UserID: alice.ID})
		if task.LastError == nil || *task.LastError != "missed" || !task.NextRunAt.Time.After(now) {
			t.Errorf("task = %+v", task)
		}
	})

	t.Run("switched off, nothing is there and nothing runs", func(t *testing.T) {
		if err := q.SetSetting(ctx, sqlc.SetSettingParams{Key: settingScheduledTasks, Value: []byte("false")}); err != nil {
			t.Fatal(err)
		}
		if rec := call(alice, ``, server.ListTasks); rec.Code != http.StatusNotFound {
			t.Errorf("list: %d", rec.Code)
		}
		ctx := agent.WithScope(ctx, agent.Scope{UserID: alice.ID, ConversationID: first.ConversationId})
		if tools := server.TaskTools(ctx); len(tools) != 0 {
			t.Errorf("offered %d tools", len(tools))
		}
		if _, err := pool.Exec(ctx, `UPDATE scheduled_tasks SET next_run_at = now() - interval '1 minute' WHERE id = $1`, first.Id); err != nil {
			t.Fatal(err)
		}
		server.RunDueTasks(ctx, time.Now())
		var runs int
		_ = pool.QueryRow(ctx, `SELECT count(*) FROM runs WHERE conversation_id = $1`, first.ConversationId).Scan(&runs)
		if runs != 1 {
			t.Errorf("%d runs while off", runs)
		}
		if err := q.SetSetting(ctx, sqlc.SetSettingParams{Key: settingScheduledTasks, Value: []byte("true")}); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("the model's tool works in the person's time zone", func(t *testing.T) {
		if err := q.SetUserTimeZone(ctx, sqlc.SetUserTimeZoneParams{ID: mallory.ID, TimeZone: new("America/New_York")}); err != nil {
			t.Fatal(err)
		}
		conv, err := q.CreateConversation(ctx, sqlc.CreateConversationParams{UserID: &mallory.ID, Title: "chat", Model: &modelID})
		if err != nil {
			t.Fatal(err)
		}
		scoped := agent.WithScope(ctx, agent.Scope{UserID: mallory.ID, ConversationID: conv.ID})
		tools := server.TaskTools(scoped)
		if len(tools) != 1 {
			t.Fatalf("offered %d tools", len(tools))
		}
		tomorrow := time.Now().In(mustLoad(t, "America/New_York")).AddDate(0, 0, 1).Format("2006-01-02")
		args, _ := json.Marshal(map[string]any{
			"op": "create", "title": "Dentist", "prompt": "Remind the user of the dentist at 3pm.",
			"schedule": map[string]any{"kind": "once", "at": tomorrow + "T09:00"},
		})
		out, err := tools[0].Execute(scoped, args)
		if err != nil {
			t.Fatal(err)
		}
		details := out.Details.(TaskToolDetails)
		if details.Task == nil || details.Task.Schedule.At == nil {
			t.Fatalf("details = %+v", details)
		}
		at := details.Task.Schedule.At.In(mustLoad(t, "America/New_York"))
		if at.Format("2006-01-02 15:04") != tomorrow+" 09:00" || !strings.Contains(out.Content, "America/New_York") {
			t.Errorf("at %v, content %q", at, out.Content)
		}
		// Its own conversation, not this one.
		if details.Task.ConversationId == conv.ID {
			t.Error("the task went into the conversation it was made in")
		}

		listed, err := tools[0].Execute(scoped, json.RawMessage(`{"op":"list"}`))
		if err != nil || !strings.Contains(listed.Content, details.Task.Id.String()) {
			t.Errorf("list: %v %q", err, listed.Content)
		}
		paused, err := tools[0].Execute(scoped, json.RawMessage(fmt.Sprintf(`{"op":"pause","id":%q}`, details.Task.Id)))
		if err != nil || paused.Details.(TaskToolDetails).Task.Status != TaskPaused {
			t.Errorf("pause: %v %+v", err, paused)
		}
		// Alice's task is not reachable from Mallory's run.
		if _, err := tools[0].Execute(scoped, json.RawMessage(fmt.Sprintf(`{"op":"delete","id":%q}`, first.Id))); err == nil {
			t.Error("deleted someone else's task")
		}

		// Not in a temporary conversation.
		temp, err := q.CreateConversation(ctx, sqlc.CreateConversationParams{UserID: &mallory.ID, Title: "t", Temporary: true})
		if err != nil {
			t.Fatal(err)
		}
		if tools := server.TaskTools(agent.WithScope(ctx, agent.Scope{UserID: mallory.ID, ConversationID: temp.ID})); len(tools) != 0 {
			t.Error("offered in a temporary conversation")
		}
	})

	t.Run("run now", func(t *testing.T) {
		rec := call(alice, ``, func(w http.ResponseWriter, r *http.Request) { server.RunTask(w, r, first.Id) })
		if rec.Code != http.StatusAccepted {
			t.Fatalf("run: %d %s", rec.Code, rec.Body)
		}
		if run := settle(first.ConversationId); run.Status != sqlc.RunStatusCompleted {
			t.Errorf("run %s", run.Status)
		}
		if got := decode(rec); got.LastError != nil || got.LastRunAt == nil {
			t.Errorf("task = %+v", got)
		}
	})

	t.Run("the browser's time zone is kept", func(t *testing.T) {
		rec := call(alice, `{"timeZone":"Europe/Berlin"}`, server.UpdateMe)
		var me SessionUser
		_ = json.Unmarshal(rec.Body.Bytes(), &me)
		if rec.Code != http.StatusOK || me.TimeZone == nil || *me.TimeZone != "Europe/Berlin" {
			t.Errorf("%d %s", rec.Code, rec.Body)
		}
		if rec := call(alice, `{"timeZone":"Nowhere/Land"}`, server.UpdateMe); rec.Code != http.StatusBadRequest {
			t.Errorf("bad zone: %d", rec.Code)
		}
	})

	t.Run("an edit keeps what the scheduler or a pause changed meanwhile", func(t *testing.T) {
		rec := create(mallory, daily)
		if rec.Code != http.StatusCreated {
			t.Fatalf("create: %d %s", rec.Code, rec.Body)
		}
		task := decode(rec)
		// The scheduler holds the row and moves its next time on while the
		// edit, which read the task before, waits for it.
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		moved := time.Now().Add(72 * time.Hour).Truncate(time.Second)
		if _, err := tx.Exec(ctx, `UPDATE scheduled_tasks SET next_run_at = $2 WHERE id = $1`, task.Id, moved); err != nil {
			t.Fatal(err)
		}
		title := "晚报"
		done := make(chan error, 1)
		go func() {
			_, err := server.updateTask(ctx, mallory, task.Id, taskChange{title: &title}, ModerationSourceTask)
			done <- err
		}()
		time.Sleep(100 * time.Millisecond)
		if err := tx.Commit(ctx); err != nil {
			t.Fatal(err)
		}
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		row, err := q.GetTask(ctx, sqlc.GetTaskParams{ID: task.Id, UserID: mallory.ID})
		if err != nil {
			t.Fatal(err)
		}
		if row.Title != title || !row.NextRunAt.Time.Equal(moved) {
			t.Errorf("row = %q next=%v, want the title changed and the scheduler's next time kept", row.Title, row.NextRunAt.Time)
		}
	})
}

func mustLoad(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Fatal(err)
	}
	return loc
}
