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

// Regenerating keeps the answer it replaces as a version: hidden from the
// transcript and from the next request's history, and switchable back to while
// it is the last turn. Editing the last question starts a branch beside it.
func TestRegenerateKeepsVersions(t *testing.T) {
	pool := agentTestPool(t)
	ctx := context.Background()
	q := sqlc.New(pool)

	// Answers "answer N" to the Nth request, and keeps every request body.
	var (
		mu     sync.Mutex
		bodies []string
	)
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, string(raw))
		n := len(bodies)
		mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprintf(w, "data: {\"id\":\"r\",\"model\":\"fake\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"answer %d\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n", n)
	}))
	t.Cleanup(endpoint.Close)

	tag := uuid.NewString()
	var user sqlc.User
	if err := pool.QueryRow(ctx, `INSERT INTO users (email, name) VALUES ($1, $2) RETURNING id, email, name, role`,
		tag+"@test.invalid", "regen-"+tag).Scan(&user.ID, &user.Email, &user.Name, &user.Role); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM users WHERE id = $1", user.ID) })
	conv, err := q.CreateConversation(ctx, sqlc.CreateConversationParams{UserID: &user.ID, Title: "regen"})
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
		func(context.Context) []agent.Tool { return nil },
		agent.Options{SystemPrompt: NewSystemPrompt(pool), PartialInterval: 5 * time.Millisecond})
	t.Cleanup(func() { _ = runner.Shutdown(context.Background()) })
	server := &Server{pool: pool, q: q, runner: runner}
	withUser := func(r *http.Request) *http.Request { return r.WithContext(auth.WithUser(r.Context(), user)) }
	post := func(body string) *http.Request {
		return withUser(httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body)))
	}

	finish := func(runID uuid.UUID) {
		t.Helper()
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) {
			run, err := q.GetRun(ctx, runID)
			if err != nil {
				t.Fatal(err)
			}
			if run.Status != sqlc.RunStatusRunning {
				if run.Status != sqlc.RunStatusCompleted {
					t.Fatalf("run %s", run.Status)
				}
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
		t.Fatal("run did not finish")
	}
	started := func(rec *httptest.ResponseRecorder) CreateMessageResponse {
		t.Helper()
		if rec.Code != http.StatusAccepted {
			t.Fatalf("start run: %d %s", rec.Code, rec.Body)
		}
		var out CreateMessageResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		finish(out.Run.Id)
		return out
	}
	transcript := func() []Message {
		t.Helper()
		rec := httptest.NewRecorder()
		server.ListMessages(rec, withUser(httptest.NewRequest(http.MethodGet, "/", nil)), conv.ID, ListMessagesParams{})
		var page MessagePage
		if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
			t.Fatal(err)
		}
		return page.Items
	}
	answers := func(items []Message) []string {
		var out []string
		for _, m := range items {
			if m.Role == RoleAssistant {
				out = append(out, m.Content)
			}
		}
		return out
	}

	// Nothing to answer yet.
	rec := httptest.NewRecorder()
	server.RegenerateAnswer(rec, post(`{}`), conv.ID)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("regenerate in an empty conversation: %d %s", rec.Code, rec.Body)
	}

	rec = httptest.NewRecorder()
	server.CreateMessage(rec, post(fmt.Sprintf(`{"content":"hello","model":%q}`, modelID)), conv.ID)
	first := started(rec)

	rec = httptest.NewRecorder()
	server.RegenerateAnswer(rec, post(`{"requestId":"again-1"}`), conv.ID)
	second := started(rec)
	if second.Message.Id != first.Message.Id {
		t.Fatalf("regenerated a different input: %s vs %s", second.Message.Id, first.Message.Id)
	}
	if second.Message.Versions == nil || len(*second.Message.Versions) != 2 {
		t.Fatalf("response versions: %v", second.Message.Versions)
	}

	// A retry of the same regenerate is the same run.
	rec = httptest.NewRecorder()
	server.RegenerateAnswer(rec, post(`{"requestId":"again-1"}`), conv.ID)
	var retried CreateMessageResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &retried)
	if retried.Run.Id != second.Run.Id {
		t.Fatalf("retried regenerate started run %s, want %s", retried.Run.Id, second.Run.Id)
	}

	// The second request was asked without the first answer in its history.
	mu.Lock()
	if len(bodies) != 2 || strings.Contains(bodies[1], "answer 1") || !strings.Contains(bodies[1], "hello") {
		t.Fatalf("regenerate's request: %d requests, last %s", len(bodies), bodies[len(bodies)-1])
	}
	mu.Unlock()

	items := transcript()
	if got := answers(items); len(got) != 1 || got[0] != "answer 2" {
		t.Fatalf("after regenerate, answers %v", got)
	}
	if v := items[0].Versions; v == nil || (*v)[0] != first.Run.Id || (*v)[1] != second.Run.Id {
		t.Fatalf("versions %v", v)
	}

	// Back to the first version.
	rec = httptest.NewRecorder()
	server.ChooseAnswer(rec, post(``), conv.ID, first.Run.Id)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "answer 1") {
		t.Fatalf("choose: %d %s", rec.Code, rec.Body)
	}
	if got := answers(transcript()); len(got) != 1 || got[0] != "answer 1" {
		t.Fatalf("after choosing the first, answers %v", got)
	}

	// The next message builds on the version shown.
	rec = httptest.NewRecorder()
	server.CreateMessage(rec, post(`{"content":"and then?"}`), conv.ID)
	third := started(rec)
	mu.Lock()
	last := bodies[len(bodies)-1]
	mu.Unlock()
	if !strings.Contains(last, "answer 1") || strings.Contains(last, "answer 2") {
		t.Fatalf("next request's history: %s", last)
	}

	// The earlier turn is settled now.
	rec = httptest.NewRecorder()
	server.ChooseAnswer(rec, post(``), conv.ID, second.Run.Id)
	if rec.Code != http.StatusConflict {
		t.Fatalf("choosing on an earlier turn: %d %s", rec.Code, rec.Body)
	}
	// Editing the last question: a new message beside it, a version of the question.
	rec = httptest.NewRecorder()
	server.EditMessage(rec, post(`{"content":"and so?"}`), conv.ID, third.Message.Id)
	edited := started(rec)
	if edited.Message.EditOf == nil || *edited.Message.EditOf != third.Message.Id {
		t.Fatalf("edit's editOf: %v", edited.Message.EditOf)
	}
	// The question has two versions now; the new one has had one answer.
	if b := edited.Message.Branches; b == nil || len(*b) != 2 || (*b)[0] != third.Message.Id || (*b)[1] != edited.Message.Id {
		t.Fatalf("edit's branches: %v", b)
	}
	if v := edited.Message.Versions; v != nil {
		t.Fatalf("edit's answer versions: %v", v)
	}
	mu.Lock()
	last = bodies[len(bodies)-1]
	mu.Unlock()
	if !strings.Contains(last, "and so?") || strings.Contains(last, "and then?") || !strings.Contains(last, "answer 1") {
		t.Fatalf("edit's request: %s", last)
	}
	items = transcript()
	var questions []string
	for _, m := range items {
		if m.Role == RoleUser {
			questions = append(questions, m.Content)
		}
	}
	if strings.Join(questions, "|") != "hello|and so?" {
		t.Fatalf("after editing, questions %v", questions)
	}
	if got := answers(items); len(got) != 2 || got[1] != fmt.Sprintf("answer %d", len(bodies)) {
		t.Fatalf("after editing, answers %v", got)
	}

	// Back to the question as first asked, with the answer it had.
	rec = httptest.NewRecorder()
	server.SwitchVersion(rec, post(``), conv.ID, third.Message.Id, SwitchVersionParams{})
	var chosen MessagePage
	_ = json.Unmarshal(rec.Body.Bytes(), &chosen)
	if rec.Code != http.StatusOK || len(chosen.Items) < 4 || chosen.Items[2].Content != "and then?" {
		t.Fatalf("switching to the unedited version: %d %s", rec.Code, rec.Body)
	}
	questions = nil
	for _, m := range transcript() {
		if m.Role == RoleUser {
			questions = append(questions, m.Content)
		}
	}
	if strings.Join(questions, "|") != "hello|and then?" {
		t.Fatalf("after switching back, questions %v", questions)
	}

	// An earlier question can be edited too: the turns after it are off the
	// branch shown, and the new wording is answered without them.
	rec = httptest.NewRecorder()
	server.EditMessage(rec, post(`{"content":"howdy"}`), conv.ID, first.Message.Id)
	reworded := started(rec)
	mu.Lock()
	last = bodies[len(bodies)-1]
	mu.Unlock()
	if strings.Contains(last, "and then?") || strings.Contains(last, "answer 1") || !strings.Contains(last, "howdy") {
		t.Fatalf("earlier edit's request: %s", last)
	}
	questions = nil
	for _, m := range transcript() {
		if m.Role == RoleUser {
			questions = append(questions, m.Content)
		}
	}
	if strings.Join(questions, "|") != "howdy" {
		t.Fatalf("after editing the first question, questions %v", questions)
	}
	if reworded.Message.EditOf == nil || *reworded.Message.EditOf != first.Message.Id {
		t.Fatalf("earlier edit's editOf: %v", reworded.Message.EditOf)
	}

	// And another conversation's run is not found here.
	rec = httptest.NewRecorder()
	server.ChooseAnswer(rec, post(``), conv.ID, uuid.New())
	if rec.Code != http.StatusNotFound {
		t.Fatalf("choosing an unknown run: %d %s", rec.Code, rec.Body)
	}
}
