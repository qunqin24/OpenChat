package api

import (
	"bufio"
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
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/qunqin24/openchat/apps/server/internal/agent"
	"github.com/qunqin24/openchat/apps/server/internal/auth"
	"github.com/qunqin24/openchat/apps/server/internal/db/dbtest"
	"github.com/qunqin24/openchat/apps/server/internal/db/sqlc"
	"github.com/qunqin24/openchat/apps/server/internal/tools"
)

// The whole path, over HTTP, against a fake OpenAI-compatible endpoint: a
// message is admitted, a run calls a tool and answers in the background, and
// the events stream reports it. Needs a scratch database:
//
//	TEST_DATABASE_URL=postgres://... go test ./internal/api/
//
// The package runs in a database of its own (<name in the URL>_api). It is
// reused between runs, so a test must not assume the tables are empty.
func agentTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	return dbtest.Pool(t, "api")
}

// fakeOpenAI streams a web_fetch call on the first request and an answer on
// every later one.
func fakeOpenAI(t *testing.T) (*httptest.Server, *atomic.Int32) {
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}
		n := requests.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		// Paced, so the answer streams across several partial commits instead
		// of arriving whole inside one throttle window.
		frame := func(v string) {
			_, _ = fmt.Fprintf(w, "data: %s\n\n", v)
			w.(http.Flusher).Flush()
			time.Sleep(20 * time.Millisecond)
		}
		if n == 1 {
			frame(`{"id":"r1","model":"fake-1","choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"web_fetch","arguments":""}}]},"finish_reason":null}]}`)
			frame(`{"id":"r1","model":"fake-1","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"url\":\"http://127.0.0.1:9/\"}"}}]},"finish_reason":null}]}`)
			frame(`{"id":"r1","model":"fake-1","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`)
		} else {
			frame(`{"id":"r2","model":"fake-1","choices":[{"index":0,"delta":{"role":"assistant","content":"That address "},"finish_reason":null}]}`)
			frame(`{"id":"r2","model":"fake-1","choices":[{"index":0,"delta":{"content":"is private."},"finish_reason":null}]}`)
			frame(`{"id":"r2","model":"fake-1","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":4,"total_tokens":14}}`)
		}
		frame("[DONE]")
	}))
	t.Cleanup(srv.Close)
	return srv, &requests
}

func TestAgentEndToEnd(t *testing.T) {
	pool := agentTestPool(t)
	ctx := context.Background()
	q := sqlc.New(pool)
	endpoint, requests := fakeOpenAI(t)

	tag := uuid.NewString()
	var user sqlc.User
	if err := pool.QueryRow(ctx, `INSERT INTO users (email, name) VALUES ($1, $2) RETURNING id, email, name, role`,
		tag+"@test.invalid", "e2e-"+tag).Scan(&user.ID, &user.Email, &user.Name, &user.Role); err != nil {
		t.Fatal(err)
	}
	conv, err := q.CreateConversation(ctx, sqlc.CreateConversationParams{UserID: &user.ID, Title: "e2e"})
	if err != nil {
		t.Fatal(err)
	}
	modelID := "fake-model-" + tag
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
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM channels WHERE id = $1", channel.ID) })

	runner := agent.NewRunner(pool, agent.NewHub(), NewModelResolver(pool),
		func(context.Context) []agent.Tool { return []agent.Tool{tools.WebFetch{}} },
		agent.Options{SystemPrompt: NewSystemPrompt(pool), PartialInterval: 5 * time.Millisecond})
	t.Cleanup(func() { _ = runner.Shutdown(context.Background()) })
	server := &Server{pool: pool, q: q, runner: runner}

	withUser := func(r *http.Request) *http.Request { return r.WithContext(auth.WithUser(r.Context(), user)) }

	// Subscribe first, the way a page does.
	events := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		server.StreamConversationEvents(w, withUser(r), conv.ID)
	}))
	t.Cleanup(events.Close)
	streamCtx, cancelStream := context.WithCancel(ctx)
	t.Cleanup(cancelStream)
	req, _ := http.NewRequestWithContext(streamCtx, http.MethodGet, events.URL, nil)
	resp, err := events.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	frames := make(chan map[string]any, 256)
	go func() {
		defer close(frames)
		defer resp.Body.Close()
		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 1<<20), 1<<20)
		for scanner.Scan() {
			line := scanner.Text()
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			var frame map[string]any
			if json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &frame) == nil {
				frames <- frame
			}
		}
	}()
	first := <-frames
	if first["type"] != "snapshot" || first["run"] != nil {
		t.Fatalf("first frame: %v", first)
	}

	// Send the message.
	body := fmt.Sprintf(`{"content":"read http://127.0.0.1:9/","model":%q,"requestId":"r-1"}`, modelID)
	rec := httptest.NewRecorder()
	server.CreateMessage(rec, withUser(httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))), conv.ID)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("create message: %d %s", rec.Code, rec.Body)
	}
	var created CreateMessageResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}

	// A retry of the same request is the same run, not a second answer.
	rec = httptest.NewRecorder()
	server.CreateMessage(rec, withUser(httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))), conv.ID)
	var retried CreateMessageResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &retried)
	if rec.Code != http.StatusAccepted || retried.Run.Id != created.Run.Id {
		t.Fatalf("retried request: %d, run %s vs %s", rec.Code, retried.Run.Id, created.Run.Id)
	}

	// Follow the stream to the end of the run.
	var seen []string
	deadline := time.After(10 * time.Second)
	finished := false
	for !finished {
		select {
		case frame, ok := <-frames:
			if !ok {
				t.Fatalf("stream closed; saw %v", seen)
			}
			if frame["type"] != "batch" {
				continue
			}
			for _, e := range frame["events"].([]any) {
				event := e.(map[string]any)
				seen = append(seen, event["type"].(string))
				if event["type"] == "run_finished" {
					run := event["run"].(map[string]any)
					if run["status"] != "completed" {
						t.Fatalf("run finished %v (%v)", run["status"], run["errorCode"])
					}
					finished = true
				}
			}
		case <-deadline:
			t.Fatalf("no run_finished; saw %v", seen)
		}
	}
	for _, want := range []string{"run_started", "message_appended", "tool_started", "tool_finished", "part_delta", "run_finished"} {
		if !contains(seen, want) {
			t.Errorf("no %s event; saw %v", want, seen)
		}
	}
	if requests.Load() != 2 {
		t.Errorf("model was asked %d times", requests.Load())
	}

	// The transcript, as the page loads it after the run.
	rec = httptest.NewRecorder()
	server.ListMessages(rec, withUser(httptest.NewRequest(http.MethodGet, "/", nil)), conv.ID, ListMessagesParams{})
	var page MessagePage
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatalf("%v: %s", err, rec.Body)
	}
	var roles []string
	for _, m := range page.Items {
		roles = append(roles, string(m.Role))
	}
	if strings.Join(roles, ",") != "user,assistant,tool,assistant" {
		t.Fatalf("transcript roles %v", roles)
	}
	toolMsg, _ := json.Marshal(page.Items[2].Parts[0])
	if !strings.Contains(string(toolMsg), `"status":"failed"`) || !strings.Contains(string(toolMsg), "public") {
		t.Errorf("the SSRF guard did not answer the fetch: %s", toolMsg)
	}
	if page.Items[3].Content != "That address is private." {
		t.Errorf("answer %q", page.Items[3].Content)
	}
	if page.Items[3].Usage == nil || page.Items[3].Usage.OutputTokens != 4 {
		t.Errorf("usage %+v", page.Items[3].Usage)
	}

	// The conversation remembers its model.
	stored, _ := q.GetConversation(ctx, sqlc.GetConversationParams{ID: conv.ID, UserID: &user.ID})
	if stored.Model == nil || *stored.Model != modelID {
		t.Errorf("conversation model %v", stored.Model)
	}
}

func contains(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}
