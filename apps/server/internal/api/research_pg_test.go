package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/qunqin24/openchat/apps/server/internal/agent"
	"github.com/qunqin24/openchat/apps/server/internal/db/dbtest"
	"github.com/qunqin24/openchat/apps/server/internal/db/sqlc"
)

// Deep research end to end (research.go): a message that asks for one drafts
// a plan, the person starts, revises or cancels it, and starting counts
// against the month. Its own database, since it sets site-wide settings.

const planJSON = `{"title":"Compare X and Y","steps":["Read X's documentation","Read Y's documentation","Write the report"]}`

func newResearchHarness(t *testing.T, monthly int, reply func(n int) string) *askHarness {
	t.Helper()
	pool := dbtest.Pool(t, "api_research")
	ctx := context.Background()
	q := sqlc.New(pool)
	h := &askHarness{t: t, pool: pool, q: q, wire: openAIWire, reply: reply}

	set := func(key string, value any) {
		raw, _ := json.Marshal(value)
		if err := q.SetSetting(ctx, sqlc.SetSettingParams{Key: key, Value: raw}); err != nil {
			t.Fatal(err)
		}
	}
	config := defaultWebSearch()
	config.Provider = string(Free)
	set(settingWebSearch, config)
	set(settingDeepResearch, true)
	set(settingResearchMonthly, monthly)

	endpoint := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		h.mu.Lock()
		h.bodies = append(h.bodies, string(raw))
		n := len(h.bodies)
		h.mu.Unlock()
		rw.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(rw, h.reply(n))
	}))
	t.Cleanup(endpoint.Close)

	tag := uuid.NewString()
	if err := pool.QueryRow(ctx, `INSERT INTO users (email, name) VALUES ($1, $2) RETURNING id, email, name, role`,
		tag+"@test.invalid", "research-"+tag).Scan(&h.user.ID, &h.user.Email, &h.user.Name, &h.user.Role); err != nil {
		t.Fatal(err)
	}
	// The first account of a database is an administrator, whom the allowance
	// exempts; the tests are about a member.
	if _, err := pool.Exec(ctx, `UPDATE users SET role = 'user' WHERE id = $1`, h.user.ID); err != nil {
		t.Fatal(err)
	}
	h.user.Role = sqlc.UserRoleUser
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM users WHERE id = $1", h.user.ID) })
	var err error
	if h.conv, err = q.CreateConversation(ctx, sqlc.CreateConversationParams{UserID: &h.user.ID, Title: "research"}); err != nil {
		t.Fatal(err)
	}
	h.model = "fake-model-" + tag
	channel, err := q.CreateChannel(ctx, sqlc.CreateChannelParams{
		Name: "fake " + tag, Type: sqlc.ChannelTypeOpenaiCompatible, Enabled: true, BaseUrl: endpoint.URL + "/v1", ApiKey: "sk-test",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := q.CreateSelectedChannelModels(ctx, sqlc.CreateSelectedChannelModelsParams{
		ChannelID: channel.ID, ModelIds: []string{h.model}, DisplayNames: []string{""}, Sources: []string{"manual"},
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM channels WHERE id = $1", channel.ID) })

	runner := agent.NewRunner(pool, agent.NewHub(), NewModelResolver(pool), NewToolSource(pool),
		agent.Options{SystemPrompt: NewSystemPrompt(pool), PartialInterval: 5 * time.Millisecond})
	t.Cleanup(func() { _ = runner.Shutdown(context.Background()) })
	h.server = &Server{pool: pool, q: q, runner: runner}
	return h
}

func (h *askHarness) decide(callID, body string) *httptest.ResponseRecorder {
	h.t.Helper()
	rec := httptest.NewRecorder()
	h.server.AnswerResearchPlan(rec, h.request(http.MethodPost, body), h.conv.ID, callID)
	return rec
}

func (h *askHarness) researchStarts() int {
	h.t.Helper()
	var n int
	if err := h.pool.QueryRow(context.Background(), `SELECT count(*) FROM research_starts WHERE user_id = $1`, h.user.ID).Scan(&n); err != nil {
		h.t.Fatal(err)
	}
	return n
}

// offered is the names of the tools a request carried.
func offered(t *testing.T, body string) []string {
	t.Helper()
	var req struct {
		Tools []struct {
			Function struct {
				Name string `json:"name"`
			} `json:"function"`
		} `json:"tools"`
	}
	if err := json.Unmarshal([]byte(body), &req); err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, tool := range req.Tools {
		names = append(names, tool.Function.Name)
	}
	return names
}

// The plan is put first; starting it runs the research as part of the same
// reply, counts once, and the month's last research is refused before a plan.
func TestResearchPlanStartsTheResearch(t *testing.T) {
	h := newResearchHarness(t, 1, func(n int) string {
		if n == 1 {
			return openAICall("call_plan_1", "research_plan", planJSON)
		}
		return openAIWire.text("report written")
	})
	ctx := context.Background()
	if _, err := h.q.SetConversationWebSearch(ctx, sqlc.SetConversationWebSearchParams{ID: h.conv.ID, UserID: &h.user.ID, WebSearch: false}); err != nil {
		t.Fatal(err)
	}

	sent, status := h.send("<deep-research/>\nCompare X and Y")
	if status != sqlc.RunStatusAwaitingInput {
		t.Fatalf("plan run ended %s, want awaiting_input", status)
	}
	plan, _ := h.q.GetRun(ctx, sent.Run.Id)
	if !plan.WebSearch || plan.Research {
		t.Fatalf("plan run: web %v research %v, want web on and not the research", plan.WebSearch, plan.Research)
	}
	if conv, _ := h.q.GetConversation(ctx, sqlc.GetConversationParams{ID: h.conv.ID, UserID: &h.user.ID}); conv.WebSearch {
		t.Fatal("the conversation's web switch was turned on")
	}
	first := h.body(1)
	if !strings.Contains(first, "Deep research: the plan") || !contains(offered(t, first), "research_plan") {
		t.Fatal("the plan run was not given the plan's prompt and tool")
	}

	var out AnswerResearchPlanResponse
	h.accepted(h.decide("call_plan_1", `{"decision":"start","model":"`+h.model+`"}`), &out)
	if out.Run == nil {
		t.Fatal("starting returned no run")
	}
	if status := h.settle(out.Run.Id); status != sqlc.RunStatusCompleted {
		t.Fatalf("research run ended %s", status)
	}
	research, _ := h.q.GetRun(ctx, out.Run.Id)
	if !research.Research || research.Continues == nil || *research.Continues != plan.ID || !research.WebSearch {
		t.Fatalf("research run: research %v continues %v web %v", research.Research, research.Continues, research.WebSearch)
	}
	second := h.body(2)
	if !strings.Contains(second, "## Deep research\\n") || contains(offered(t, second), "research_plan") {
		t.Fatal("the research run was not given the research prompt, or was offered the plan again")
	}
	if n := h.researchStarts(); n != 1 {
		t.Fatalf("%d researches counted, want 1", n)
	}
	// Answered: not pending any more.
	if rec := h.decide("call_plan_1", `{"decision":"start"}`); rec.Code != http.StatusConflict {
		t.Fatalf("second start: %d %s", rec.Code, rec.Body)
	}

	// The month's one research is used: the next is refused before a plan.
	rec := httptest.NewRecorder()
	h.server.CreateMessage(rec, h.request(http.MethodPost, fmt.Sprintf(`{"content":%q,"model":%q}`, "<deep-research/>\nAnother", h.model)), h.conv.ID)
	if rec.Code != http.StatusTooManyRequests || !strings.Contains(rec.Body.String(), "research_limit") {
		t.Fatalf("over the limit: %d %s", rec.Code, rec.Body)
	}
}

// Revising runs the plan again with the person's words; cancelling writes the
// result and starts nothing; neither counts.
func TestResearchPlanReviseAndCancel(t *testing.T) {
	h := newResearchHarness(t, 10, func(n int) string {
		return openAICall(fmt.Sprintf("call_plan_%d", n), "research_plan", planJSON)
	})

	if _, status := h.send("<deep-research/>\nCompare X and Y"); status != sqlc.RunStatusAwaitingInput {
		t.Fatalf("plan run ended %s", status)
	}
	if rec := h.decide("call_plan_1", `{"decision":"revise"}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("revise without feedback: %d", rec.Code)
	}
	var revised AnswerResearchPlanResponse
	h.accepted(h.decide("call_plan_1", `{"decision":"revise","feedback":"Add Z as well","model":"`+h.model+`"}`), &revised)
	if status := h.settle(revised.Run.Id); status != sqlc.RunStatusAwaitingInput {
		t.Fatalf("revised plan run ended %s", status)
	}
	run, _ := h.q.GetRun(context.Background(), revised.Run.Id)
	if run.Research {
		t.Fatal("a revision started the research")
	}
	if body := h.body(2); !strings.Contains(body, "Add Z as well") || !contains(offered(t, body), "research_plan") {
		t.Fatal("the revision did not reach the model with the plan tool")
	}

	var cancelled AnswerResearchPlanResponse
	h.accepted(h.decide("call_plan_2", `{"decision":"cancel"}`), &cancelled)
	if cancelled.Run != nil {
		t.Fatal("cancelling started a run")
	}
	if rec := h.decide("call_plan_2", `{"decision":"start"}`); rec.Code != http.StatusConflict {
		t.Fatalf("start after cancel: %d", rec.Code)
	}
	if n := h.researchStarts(); n != 0 {
		t.Fatalf("%d researches counted, want 0", n)
	}
	// The next request reads the cancellation as the call's result.
	h.send("thanks")
	if body := h.body(3); !strings.Contains(body, "cancelled this research") {
		t.Fatal("the cancellation was not the plan's result")
	}
}

// Off, a message that asks for a deep research is refused.
func TestResearchOff(t *testing.T) {
	h := newResearchHarness(t, 10, func(int) string { return openAIWire.text("hi") })
	raw, _ := json.Marshal(false)
	if err := h.q.SetSetting(context.Background(), sqlc.SetSettingParams{Key: settingDeepResearch, Value: raw}); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	h.server.CreateMessage(rec, h.request(http.MethodPost, fmt.Sprintf(`{"content":%q,"model":%q}`, "<deep-research/>\nX", h.model)), h.conv.ID)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("research off: %d %s", rec.Code, rec.Body)
	}
}

func TestPersonWords(t *testing.T) {
	for in, want := range map[string]string{
		"<deep-research/>\nCompare X":                        "Compare X",
		"<deep-research/>\n<skill name=\"pdf\"/>\nRead this": "Read this",
		"<skill name=\"pdf\"/>\nRead this":                   "Read this",
		"Just words <deep-research/>":                        "Just words <deep-research/>",
	} {
		if got := personWords(in); got != want {
			t.Errorf("personWords(%q) = %q, want %q", in, got, want)
		}
	}
	if asks, _ := splitResearch("<deep-research/>"); !asks {
		t.Error("the line alone was not read as asking")
	}
}
