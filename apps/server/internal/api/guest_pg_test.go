package api

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
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
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/qunqin/openchat/apps/server/internal/agent"
	"github.com/qunqin/openchat/apps/server/internal/auth"
	"github.com/qunqin/openchat/apps/server/internal/config"
	"github.com/qunqin/openchat/apps/server/internal/db/dbtest"
	"github.com/qunqin/openchat/apps/server/internal/db/sqlc"
	"github.com/qunqin/openchat/apps/server/internal/usage"
)

// Guest mode, over HTTP, against a scripted OpenAI-compatible endpoint: a
// request carries a conversation the server has never seen, is answered by the
// agent runner, and leaves nothing behind. The package has a database of its
// own (<name in the URL>_api_guest) because the limits count rows nobody else
// may write.

// guestHarness is a server with guest mode on, one model, and a model endpoint
// that answers by a script.
type guestHarness struct {
	t      *testing.T
	pool   *pgxpool.Pool
	q      *sqlc.Queries
	server *Server
	blobs  *memoryStore
	model  string
	http   *httptest.Server

	mu     sync.Mutex
	bodies []string
	// script answers the nth request (from 1) to the model with a body of
	// server-sent events; the default answers "ok".
	script func(n int, body string) string
	// pace holds each request to the model for this long, for a test that
	// stops a run while the model is busy.
	pace time.Duration
}

func newGuestHarness(t *testing.T) *guestHarness {
	t.Helper()
	pool := dbtest.Pool(t, "api_guest")
	ctx := context.Background()
	q := sqlc.New(pool)
	h := &guestHarness{t: t, pool: pool, q: q, blobs: &memoryStore{blobs: map[string][]byte{}}}

	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		h.mu.Lock()
		h.bodies = append(h.bodies, string(raw))
		n := len(h.bodies)
		script, pace := h.script, h.pace
		h.mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		if pace > 0 {
			select {
			case <-time.After(pace):
			case <-r.Context().Done():
				return
			}
		}
		body := openAIWire.text("ok")
		if script != nil {
			body = script(n, string(raw))
		}
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(endpoint.Close)

	tag := uuid.NewString()
	h.model = "guest-model-" + tag
	channel, err := q.CreateChannel(ctx, sqlc.CreateChannelParams{
		Name: "fake " + tag, Type: sqlc.ChannelTypeOpenaiCompatible, Enabled: true,
		BaseUrl: endpoint.URL + "/v1", ApiKey: "sk-test",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := q.CreateSelectedChannelModels(ctx, sqlc.CreateSelectedChannelModelsParams{
		ChannelID: channel.ID, ModelIds: []string{h.model}, DisplayNames: []string{""}, Sources: []string{"manual"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := q.SaveModelOverride(ctx, sqlc.SaveModelOverrideParams{ModelID: h.model, InputModalities: []string{"image"}}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM channels WHERE id = $1", channel.ID)
		_, _ = pool.Exec(context.Background(), "DELETE FROM model_overrides WHERE model_id = $1", h.model)
	})

	runner := agent.NewRunner(pool, agent.NewHub(), NewModelResolver(pool), NewToolSource(pool),
		agent.Options{SystemPrompt: NewSystemPrompt(pool), Blobs: h.blobs, Modalities: NewModalities(pool), PartialInterval: 5 * time.Millisecond})
	t.Cleanup(func() { _ = runner.Shutdown(context.Background()) })
	h.server = &Server{cfg: &config.Config{}, pool: pool, q: q, runner: runner, store: h.blobs, usage: usage.NewRecorder(q)}
	runner.AfterRun(h.server.TitleAfterRun)

	h.set(settingGuestMode, true)
	h.set(settingGuestHourlyLimit, 0)
	h.set(settingGuestDailyLimit, 0)
	_, _ = pool.Exec(ctx, "DELETE FROM guest_requests")

	h.http = httptest.NewServer(NewRouter(&config.Config{}, h.server, auth.NewMiddleware(q)))
	t.Cleanup(h.http.Close)
	return h
}

func (h *guestHarness) set(key string, value any) {
	h.t.Helper()
	raw, _ := json.Marshal(value)
	if err := h.q.SetSetting(context.Background(), sqlc.SetSettingParams{Key: key, Value: raw}); err != nil {
		h.t.Fatal(err)
	}
}

func (h *guestHarness) requests() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.bodies...)
}

// guestFrames are the frames of one guest run.
type guestFrames struct {
	status int
	// error is the JSON error of a refused request.
	error  map[string]any
	frames []map[string]any
}

// events are the run events of every batch frame, in order.
func (g guestFrames) events() []map[string]any {
	var out []map[string]any
	for _, f := range g.frames {
		if f["type"] != "batch" {
			continue
		}
		for _, e := range f["events"].([]any) {
			out = append(out, e.(map[string]any))
		}
	}
	return out
}

func (g guestFrames) find(kind string) map[string]any {
	for _, e := range g.events() {
		if e["type"] == kind {
			return e
		}
	}
	return nil
}

func (g guestFrames) end() map[string]any {
	for _, f := range g.frames {
		if f["type"] == "guest_end" {
			return f
		}
	}
	return nil
}

// run posts a request and reads the stream to its end.
func (h *guestHarness) run(body any) guestFrames {
	h.t.Helper()
	raw, _ := json.Marshal(body)
	resp, err := http.Post(h.http.URL+"/api/v1/guest/runs", "application/json", bytes.NewReader(raw))
	if err != nil {
		h.t.Fatal(err)
	}
	defer resp.Body.Close()
	out := guestFrames{status: resp.StatusCode}
	if resp.StatusCode != http.StatusOK {
		_ = json.NewDecoder(resp.Body).Decode(&out.error)
		return out
	}
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 1<<20), 64<<20)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		var frame map[string]any
		if json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &frame) == nil {
			out.frames = append(out.frames, frame)
		}
	}
	return out
}

// noGuestRows fails the test if anything of a guest is still in the database.
func (h *guestHarness) noGuestRows(conversation uuid.UUID) {
	h.t.Helper()
	ctx := context.Background()
	for _, query := range []struct{ what, sql string }{
		{"guest users", `SELECT count(*) FROM users WHERE guest`},
		{"the conversation", `SELECT count(*) FROM conversations WHERE id = '` + conversation.String() + `'`},
		{"messages", `SELECT count(*) FROM messages WHERE conversation_id = '` + conversation.String() + `'`},
	} {
		deadline := time.Now().Add(5 * time.Second)
		for {
			var n int
			if err := h.pool.QueryRow(ctx, query.sql).Scan(&n); err != nil {
				h.t.Fatal(err)
			}
			if n == 0 {
				break
			}
			if time.Now().After(deadline) {
				h.t.Fatalf("%d %s left behind", n, query.what)
			}
			time.Sleep(50 * time.Millisecond)
		}
	}
}

func userRow(id uuid.UUID, text string, at time.Time) map[string]any {
	return map[string]any{"id": id, "role": "user", "content": text, "createdAt": at.Format(time.RFC3339Nano),
		"parts": []any{map[string]any{"type": "text", "text": text}}}
}

func assistantRow(id uuid.UUID, text string, at time.Time) map[string]any {
	return map[string]any{"id": id, "role": "assistant", "content": text, "createdAt": at.Format(time.RFC3339Nano), "stopReason": "stop",
		"parts": []any{map[string]any{"type": "text", "text": text}}}
}

// openAIToolCall is a model asking for one tool.
func openAIToolCall(callID, name, args string) string {
	quoted, _ := json.Marshal(args)
	return fmt.Sprintf(`data: {"id":"r","model":"fake","choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[{"index":0,"id":%q,"type":"function","function":{"name":%q,"arguments":%s}}]},"finish_reason":null}]}`+"\n\n"+
		`data: {"id":"r","model":"fake","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`+"\n\ndata: [DONE]\n\n", callID, name, quoted)
}

func TestGuestRun(t *testing.T) {
	h := newGuestHarness(t)
	ctx := context.Background()
	h.script = func(n int, _ string) string { return openAIWire.text("Hello again, Ada.") }

	conv := uuid.New()
	u1, a1, u2 := uuid.New(), uuid.New(), uuid.New()
	attachment := uuid.New()
	yesterday := time.Now().Add(-24 * time.Hour)
	memoryID := uuid.New()
	size := len(testPNG)
	frames := h.run(map[string]any{
		"action":         "send",
		"conversationId": conv,
		"model":          h.model,
		"messages": []any{
			userRow(u1, "My name is Ada.", yesterday),
			assistantRow(a1, "Nice to meet you, Ada.", yesterday.Add(time.Second)),
		},
		"input": map[string]any{
			"id": u2, "role": "user", "content": "What is in this picture?", "createdAt": time.Now().Format(time.RFC3339Nano), "parts": []any{},
			"attachments": []any{map[string]any{
				"id": attachment, "filename": "dot.png", "contentType": "image/png", "size": size,
				"data": base64.StdEncoding.EncodeToString(testPNG),
			}},
		},
		"aboutYou":      "I am a mathematician.",
		"memoryEnabled": true,
		"memories":      []any{map[string]any{"id": memoryID, "content": "Ada likes tea."}},
	})
	if frames.status != http.StatusOK {
		t.Fatalf("status %d: %v", frames.status, frames.error)
	}

	// The events carry the ids the browser sent, and the rows the run wrote.
	started := frames.find("run_started")
	if started == nil {
		t.Fatalf("no run_started: %v", frames.events())
	}
	if got := started["message"].(map[string]any)["id"]; got != u2.String() {
		t.Errorf("run_started input id %v, want %v", got, u2)
	}
	finished := frames.find("run_finished")
	if finished == nil || finished["run"].(map[string]any)["status"] != "completed" {
		t.Fatalf("run_finished: %v", finished)
	}
	var answer map[string]any
	for _, e := range frames.events() {
		if e["type"] == "message_appended" && e["message"].(map[string]any)["role"] == "assistant" {
			answer = e["message"].(map[string]any)
		}
	}
	if answer == nil || answer["content"] != "Hello again, Ada." {
		t.Fatalf("answer: %v", answer)
	}
	// What a provider wants back is handed over, since nothing is kept here.
	if state, _ := answer["providerState"].(string); state == "" {
		t.Error("the assistant row has no providerState")
	}

	// The last frame is what the browser keeps.
	end := frames.end()
	if end == nil {
		t.Fatalf("no guest_end frame; frames %d", len(frames.frames))
	}
	if end["run"].(map[string]any)["status"] != "completed" {
		t.Errorf("end run: %v", end["run"])
	}
	snapshot, _ := end["memorySnapshot"].(map[string]any)
	if snapshot["about"] != "I am a mathematician." {
		t.Errorf("snapshot: %v", end["memorySnapshot"])
	}
	if mem := end["memories"].([]any); len(mem) != 1 || mem[0].(map[string]any)["id"] != memoryID.String() {
		t.Errorf("memories: %v", end["memories"])
	}
	if title, _ := end["title"].(string); title == "" {
		t.Errorf("a first exchange is titled; end: %v", end)
	}

	// What the model was sent: the history, the instructions, the memory, the
	// date line, and the picture.
	requests := h.requests()
	if len(requests) < 1 {
		t.Fatal("the model was never asked")
	}
	chat := requests[0]
	for _, want := range []string{"My name is Ada.", "Nice to meet you, Ada.", "I am a mathematician.", "Ada likes tea.",
		"[Date: " + yesterday.UTC().Format(time.DateOnly) + "]", base64.StdEncoding.EncodeToString(testPNG)} {
		if !strings.Contains(chat, want) {
			t.Errorf("the model's request lacks %q", want)
		}
	}

	// And nothing stays.
	h.noGuestRows(conv)
	var owners, files int
	if err := h.pool.QueryRow(ctx, `SELECT count(*) FROM usage_logs WHERE model_id = $1 AND user_id IS NOT NULL`, h.model).Scan(&owners); err != nil {
		t.Fatal(err)
	}
	if owners != 0 {
		t.Errorf("%d usage rows still name a guest", owners)
	}
	var kept int
	if err := h.pool.QueryRow(ctx, `SELECT count(*) FROM usage_logs WHERE model_id = $1 AND user_id IS NULL AND status = 'ok'`, h.model).Scan(&kept); err != nil {
		t.Fatal(err)
	}
	if kept < 1 {
		t.Error("the guest's request is not in the usage ledger")
	}
	h.blobs.mu.Lock()
	files = len(h.blobs.blobs)
	h.blobs.mu.Unlock()
	if files != 0 {
		t.Errorf("%d files left in storage", files)
	}
}

func TestGuestModeOffAndModels(t *testing.T) {
	h := newGuestHarness(t)
	get := func(path string) (int, map[string]any) {
		resp, err := http.Get(h.http.URL + "/api/v1" + path)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var body map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&body)
		return resp.StatusCode, body
	}

	if code, body := get("/guest/status"); code != 200 || body["enabled"] != true {
		t.Fatalf("status on: %d %v", code, body)
	}
	code, config := get("/guest/config")
	if code != 200 {
		t.Fatalf("config: %d %v", code, config)
	}
	ids := map[string]bool{}
	for _, m := range config["models"].([]any) {
		ids[m.(map[string]any)["id"].(string)] = true
	}
	if !ids[h.model] {
		t.Errorf("the model is not offered: %v", ids)
	}
	if features := config["features"].(map[string]any); features["avatarUploads"] != false {
		t.Errorf("avatars are for accounts: %v", features)
	}

	// A model kept for administrators is not one a guest sees, or can use.
	hidden := h.model + "-staff"
	var channelID uuid.UUID
	if err := h.pool.QueryRow(context.Background(), `SELECT channel_id FROM channel_models WHERE model_id = $1`, h.model).Scan(&channelID); err != nil {
		t.Fatal(err)
	}
	if err := h.q.CreateSelectedChannelModels(context.Background(), sqlc.CreateSelectedChannelModelsParams{
		ChannelID: channelID, ModelIds: []string{hidden}, DisplayNames: []string{""}, Sources: []string{"manual"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.q.SaveModelOverride(context.Background(), sqlc.SaveModelOverrideParams{ModelID: hidden, AdminOnly: true}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = h.pool.Exec(context.Background(), "DELETE FROM model_overrides WHERE model_id = $1", hidden)
	})
	_, config = get("/guest/config")
	for _, m := range config["models"].([]any) {
		if m.(map[string]any)["id"] == hidden {
			t.Error("a model kept for administrators is offered to a guest")
		}
	}
	conv := uuid.New()
	refused := h.run(map[string]any{
		"action": "send", "conversationId": conv, "model": hidden, "messages": []any{},
		"input": userRow(uuid.New(), "hi", time.Now()),
	})
	if refused.status != http.StatusBadRequest || refused.error["code"] != "model_unavailable" {
		t.Errorf("a staff model: %d %v", refused.status, refused.error)
	}

	// Off, everything but the status is not there.
	h.set(settingGuestMode, false)
	if code, body := get("/guest/status"); code != 200 || body["enabled"] != false {
		t.Errorf("status off: %d %v", code, body)
	}
	if code, _ := get("/guest/config"); code != http.StatusNotFound {
		t.Errorf("config while off: %d", code)
	}
	off := h.run(map[string]any{"action": "send", "conversationId": conv, "messages": []any{}, "input": userRow(uuid.New(), "hi", time.Now())})
	if off.status != http.StatusNotFound {
		t.Errorf("run while off: %d", off.status)
	}
}

func TestGuestLimits(t *testing.T) {
	h := newGuestHarness(t)
	send := func() guestFrames {
		return h.run(map[string]any{
			"action": "send", "conversationId": uuid.New(), "model": h.model, "messages": []any{},
			"input": userRow(uuid.New(), "hi", time.Now()), "titled": true,
		})
	}

	h.set(settingGuestHourlyLimit, 2)
	for i := 0; i < 2; i++ {
		if got := send(); got.status != http.StatusOK {
			t.Fatalf("request %d: %d %v", i+1, got.status, got.error)
		}
	}
	if got := send(); got.status != http.StatusTooManyRequests || got.error["code"] != "guest_ip_limit" {
		t.Fatalf("over the hourly limit: %d %v", got.status, got.error)
	}
	// Another address has its own allowance: the router believes the proxy.
	other := func() guestFrames {
		raw, _ := json.Marshal(map[string]any{
			"action": "send", "conversationId": uuid.New(), "model": h.model, "messages": []any{},
			"input": userRow(uuid.New(), "hi", time.Now()), "titled": true,
		})
		req, _ := http.NewRequest(http.MethodPost, h.http.URL+"/api/v1/guest/runs", bytes.NewReader(raw))
		req.Header.Set("X-Forwarded-For", "203.0.113.9")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		_, _ = io.Copy(io.Discard, resp.Body)
		return guestFrames{status: resp.StatusCode}
	}
	if got := other(); got.status != http.StatusOK {
		t.Fatalf("another address: %d", got.status)
	}

	// The daily limit counts everyone together.
	h.set(settingGuestHourlyLimit, 0)
	var today int
	if err := h.pool.QueryRow(context.Background(), `SELECT count(*) FROM guest_requests WHERE created_at > now() - interval '1 day'`).Scan(&today); err != nil {
		t.Fatal(err)
	}
	h.set(settingGuestDailyLimit, today)
	if got := send(); got.status != http.StatusTooManyRequests || got.error["code"] != "guest_daily_limit" {
		t.Fatalf("over the daily limit: %d %v", got.status, got.error)
	}
	// Zero is no limit.
	h.set(settingGuestDailyLimit, 0)
	if got := send(); got.status != http.StatusOK {
		t.Fatalf("unlimited: %d %v", got.status, got.error)
	}

	// Rows older than a day are purged by the cleanup pass.
	if _, err := h.pool.Exec(context.Background(), `UPDATE guest_requests SET created_at = now() - interval '2 days'`); err != nil {
		t.Fatal(err)
	}
	h.server.CleanupNow(context.Background())
	var left int
	if err := h.pool.QueryRow(context.Background(), `SELECT count(*) FROM guest_requests`).Scan(&left); err != nil {
		t.Fatal(err)
	}
	if left != 0 {
		t.Errorf("%d request rows survived the cleanup", left)
	}
}

func TestGuestAskUserAndRegenerate(t *testing.T) {
	h := newGuestHarness(t)
	h.script = func(n int, _ string) string {
		switch n {
		case 1:
			return openAIToolCall("call_q", "ask_user", questionsJSON)
		case 2:
			return openAIWire.text("Beach it is.")
		default:
			return openAIWire.text("Another try.")
		}
	}
	conv := uuid.New()
	u1 := uuid.New()
	first := h.run(map[string]any{
		"action": "send", "conversationId": conv, "model": h.model, "messages": []any{},
		"input": userRow(u1, "Plan a trip", time.Now()), "titled": true,
	})
	run1 := first.find("run_finished")["run"].(map[string]any)
	if run1["status"] != "awaiting_input" {
		t.Fatalf("the first run: %v", run1)
	}
	var asking map[string]any
	for _, e := range first.events() {
		if e["type"] == "message_appended" && e["message"].(map[string]any)["role"] == "assistant" {
			asking = e["message"].(map[string]any)
		}
	}
	if asking == nil {
		t.Fatalf("no question asked: %v", first.events())
	}
	h.noGuestRows(conv)

	// Answered from another request: the question is the last row, and the
	// run that answers joins the reply that asked.
	asking["createdAt"] = time.Now().Add(-time.Second).Format(time.RFC3339Nano)
	answers := []any{
		map[string]any{"selected": []string{"Beach"}},
		map[string]any{"selected": []string{"Food"}, "other": ""},
	}
	second := h.run(map[string]any{
		"action": "answer", "conversationId": conv, "model": h.model, "titled": true,
		"messages":  []any{userRow(u1, "Plan a trip", time.Now().Add(-time.Minute)), asking},
		"callId":    "call_q",
		"answers":   answers,
		"continues": run1["id"],
	})
	if second.status != http.StatusOK {
		t.Fatalf("answer: %d %v", second.status, second.error)
	}
	started := second.find("run_started")["run"].(map[string]any)
	if started["continues"] != run1["id"] {
		t.Errorf("the run does not continue the question's: %v", started)
	}
	if got := second.find("run_finished")["run"].(map[string]any)["status"]; got != "completed" {
		t.Errorf("answered run: %v", got)
	}
	// The answer was written as the tool's result and the model read it.
	var result map[string]any
	for _, e := range second.events() {
		if e["type"] == "message_appended" && e["message"].(map[string]any)["role"] == "tool" {
			result = e["message"].(map[string]any)
		}
	}
	if result == nil || !strings.Contains(result["parts"].([]any)[0].(map[string]any)["content"].(string), "Beach") {
		t.Errorf("the answer's row: %v", result)
	}
	if req := h.requests(); len(req) < 2 || !strings.Contains(req[1], "Beach") {
		t.Error("the model was not sent the answers")
	}
	h.noGuestRows(conv)

	// A wrong reply is refused before anything is written.
	bad := h.run(map[string]any{
		"action": "answer", "conversationId": conv, "model": h.model, "titled": true,
		"messages":  []any{userRow(u1, "Plan a trip", time.Now().Add(-time.Minute)), asking},
		"callId":    "call_q",
		"answers":   []any{map[string]any{"selected": []string{"Nowhere"}}},
		"continues": run1["id"],
	})
	if bad.status != http.StatusBadRequest || bad.error["code"] != "invalid_answers" {
		t.Errorf("a bad answer: %d %v", bad.status, bad.error)
	}
	// And so is an answer to a question that is not the last row.
	stale := h.run(map[string]any{
		"action": "answer", "conversationId": conv, "model": h.model, "titled": true,
		"messages":  []any{userRow(u1, "Plan a trip", time.Now().Add(-time.Minute)), asking, userRow(uuid.New(), "never mind", time.Now())},
		"callId":    "call_q",
		"answers":   answers,
		"continues": run1["id"],
	})
	if stale.status != http.StatusConflict || stale.error["code"] != "not_pending" {
		t.Errorf("a stale answer: %d %v", stale.status, stale.error)
	}

	// Regenerating answers the last question again.
	regen := h.run(map[string]any{
		"action": "regenerate", "conversationId": conv, "model": h.model, "titled": true,
		"messages": []any{userRow(u1, "Plan a trip", time.Now().Add(-time.Minute))},
	})
	if regen.status != http.StatusOK || regen.find("run_started")["message"].(map[string]any)["id"] != u1.String() {
		t.Errorf("regenerate: %d %v", regen.status, regen.error)
	}
	nothing := h.run(map[string]any{"action": "regenerate", "conversationId": conv, "model": h.model, "messages": []any{}})
	if nothing.status != http.StatusNotFound || nothing.error["code"] != "nothing_to_answer" {
		t.Errorf("regenerating nothing: %d %v", nothing.status, nothing.error)
	}
	h.noGuestRows(conv)
}

// An edit is a send that ends before the question it rewords: the browser has
// already deleted that question and what followed it, and the server is told
// nothing else.
func TestGuestEditIsASendBeforeTheQuestion(t *testing.T) {
	h := newGuestHarness(t)
	conv := uuid.New()
	u1, a1, edited := uuid.New(), uuid.New(), uuid.New()
	now := time.Now()
	frames := h.run(map[string]any{
		"action": "send", "conversationId": conv, "model": h.model, "titled": true,
		"messages": []any{userRow(u1, "First", now.Add(-time.Hour)), assistantRow(a1, "Reply", now.Add(-time.Hour+time.Second))},
		"input":    userRow(edited, "Second, reworded", now),
	})
	started := frames.find("run_started")
	if frames.status != http.StatusOK || started == nil {
		t.Fatalf("edit: %d %v", frames.status, frames.error)
	}
	// The model is sent the first exchange and the reworded question, only.
	body := h.requests()[0]
	if !strings.Contains(body, "Second, reworded") || !strings.Contains(body, "Reply") {
		t.Errorf("request: %s", body)
	}
	h.noGuestRows(conv)
}

func TestGuestMemoryTool(t *testing.T) {
	h := newGuestHarness(t)
	h.script = func(n int, _ string) string {
		if n == 1 {
			return openAIToolCall("call_m", "memory", `{"op":"add","content":"The user likes jazz."}`)
		}
		return openAIWire.text("Noted.")
	}
	conv := uuid.New()
	frames := h.run(map[string]any{
		"action": "send", "conversationId": conv, "model": h.model, "titled": true,
		"messages": []any{}, "input": userRow(uuid.New(), "I like jazz", time.Now()),
		"memoryEnabled": true, "memories": []any{},
	})
	end := frames.end()
	if end == nil {
		t.Fatalf("no end: %v", frames.error)
	}
	memories := end["memories"].([]any)
	events := end["memoryEvents"].([]any)
	if len(memories) != 1 || memories[0].(map[string]any)["content"] != "The user likes jazz." {
		t.Fatalf("memories: %v", memories)
	}
	if len(events) != 1 || events[0].(map[string]any)["op"] != "add" || events[0].(map[string]any)["source"] != "model" {
		t.Fatalf("events: %v", events)
	}
	snapshot := end["memorySnapshot"].(map[string]any)
	if added, _ := snapshot["added"].([]any); len(added) != 1 {
		t.Errorf("the snapshot does not know the new memory: %v", snapshot)
	}

	// A temporary conversation has no memory tool; memory off, neither.
	h.mu.Lock()
	h.bodies = nil
	h.mu.Unlock()
	h.run(map[string]any{
		"action": "send", "conversationId": uuid.New(), "model": h.model, "titled": true, "temporary": true,
		"messages": []any{}, "input": userRow(uuid.New(), "hi", time.Now()), "memoryEnabled": true,
	})
	if body := h.requests()[0]; strings.Contains(body, `"name":"memory"`) {
		t.Error("a temporary conversation was offered the memory tool")
	}
}

// A client that goes away stops the run and leaves nothing behind.
func TestGuestDisconnect(t *testing.T) {
	h := newGuestHarness(t)
	h.pace = 30 * time.Second
	conv := uuid.New()
	raw, _ := json.Marshal(map[string]any{
		"action": "send", "conversationId": conv, "model": h.model, "titled": true,
		"messages": []any{}, "input": userRow(uuid.New(), "hi", time.Now()),
	})
	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, h.http.URL+"/api/v1/guest/runs", bytes.NewReader(raw))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	// Wait for the run to have started, then hang up.
	reader := bufio.NewReader(resp.Body)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(line, "run_started") {
			break
		}
	}
	cancel()
	resp.Body.Close()
	h.noGuestRows(conv)
	var stuck int
	if err := h.pool.QueryRow(context.Background(), `SELECT count(*) FROM runs WHERE status = 'running' AND model_id = $1`, h.model).Scan(&stuck); err != nil {
		t.Fatal(err)
	}
	if stuck != 0 {
		t.Errorf("%d runs still going", stuck)
	}
}

// The cancel endpoint stops a run and the stream ends with what was made.
func TestGuestCancel(t *testing.T) {
	h := newGuestHarness(t)
	h.pace = 30 * time.Second
	conv := uuid.New()
	raw, _ := json.Marshal(map[string]any{
		"action": "send", "conversationId": conv, "model": h.model, "titled": true,
		"messages": []any{}, "input": userRow(uuid.New(), "hi", time.Now()),
	})
	resp, err := http.Post(h.http.URL+"/api/v1/guest/runs", "application/json", bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	scanner := bufio.NewScanner(resp.Body)
	var runID string
	var types []string
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		var frame map[string]any
		_ = json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &frame)
		types = append(types, fmt.Sprint(frame["type"]))
		if frame["type"] == "batch" && runID == "" {
			runID = frame["runId"].(string)
			go func() {
				stop, err := http.Post(h.http.URL+"/api/v1/guest/runs/"+runID+"/cancel", "application/json", nil)
				if err == nil {
					stop.Body.Close()
				}
			}()
		}
		if frame["type"] == "guest_end" {
			if frame["run"].(map[string]any)["status"] != "cancelled" {
				t.Errorf("end run: %v", frame["run"])
			}
		}
	}
	if len(types) == 0 || types[len(types)-1] != "guest_end" {
		t.Errorf("frames: %v", types)
	}
	h.noGuestRows(conv)
}

// A guest is never an account: not in the admin's list, not a candidate for
// first administrator, not someone who can sign in.
func TestGuestIsNotAnAccount(t *testing.T) {
	pool := dbtest.Pool(t, "api_guest")
	ctx := context.Background()
	q := sqlc.New(pool)
	tag := uuid.NewString()
	guest, err := q.CreateGuestUser(ctx, sqlc.CreateGuestUserParams{Email: "guest-" + tag + "@guest.invalid", Name: "guest-" + tag})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM users WHERE id = $1", guest.ID) })

	var before, after int64
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM users WHERE NOT guest`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if n, err := q.CountUsers(ctx); err != nil || n != before {
		t.Errorf("CountUsers %d (%v), want %d without the guest", n, err, before)
	}
	filtered, err := q.CountUsersFiltered(ctx, sqlc.CountUsersFilteredParams{})
	if err != nil || filtered != before {
		t.Errorf("CountUsersFiltered %d (%v), want %d", filtered, err, before)
	}
	list, err := q.ListUsers(ctx, sqlc.ListUsersParams{RowLimit: 1000})
	if err != nil {
		t.Fatal(err)
	}
	for _, u := range list {
		if u.ID == guest.ID {
			t.Error("the guest is in the admin's user list")
		}
	}
	if _, err := q.GetUserByLogin(ctx, guest.Email); err == nil {
		t.Error("a guest can be found by its email to sign in")
	}
	if _, err := q.GetUserByLogin(ctx, guest.Name); err == nil {
		t.Error("a guest can be found by its name to sign in")
	}
	window, err := q.GetDashboardWindow(ctx, sqlc.GetDashboardWindowParams{Bucket: "day", Buckets: 7})
	if err != nil {
		t.Fatal(err)
	}
	if window.TotalUsers != before-countDisabled(t, pool) {
		t.Errorf("the dashboard counts %d users", window.TotalUsers)
	}
	_ = after
}

func countDisabled(t *testing.T, pool *pgxpool.Pool) int64 {
	t.Helper()
	var n int64
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM users WHERE disabled_at IS NOT NULL AND NOT guest`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// A process that died mid-run leaves a guest; the cleanup pass takes it, files
// and all, and the next boot does not resume its run.
func TestGuestSweep(t *testing.T) {
	h := newGuestHarness(t)
	ctx := context.Background()
	tag := uuid.NewString()
	guest, err := h.q.CreateGuestUser(ctx, sqlc.CreateGuestUserParams{Email: "guest-" + tag + "@guest.invalid", Name: "guest-" + tag})
	if err != nil {
		t.Fatal(err)
	}
	conv, err := h.q.CreateGuestConversation(ctx, sqlc.CreateGuestConversationParams{ID: uuid.New(), UserID: &guest.ID, Title: "x"})
	if err != nil {
		t.Fatal(err)
	}
	input, err := h.q.AppendEntry(ctx, sqlc.AppendEntryParams{ConversationID: conv.ID, Role: sqlc.MessageRoleUser, Content: "hi",
		Parts: agent.EncodeParts(nil), Status: sqlc.MessageStatusComplete})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.q.CreateGuestAttachment(ctx, sqlc.CreateGuestAttachmentParams{ID: uuid.New(), MessageID: &input.ID, UserID: &guest.ID,
		StorageKey: "uploads/guest/test/" + tag, Filename: "a.png", ContentType: "image/png", SizeBytes: 3}); err != nil {
		t.Fatal(err)
	}
	h.blobs.put("uploads/guest/test/"+tag, []byte("abc"))
	run, err := h.q.CreateRun(ctx, sqlc.CreateRunParams{ConversationID: conv.ID, UserID: guest.ID, InputMessageID: input.ID, ModelID: h.model})
	if err != nil {
		t.Fatal(err)
	}

	running, err := h.q.ListRunningRuns(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range running {
		if r.ID == run.ID {
			t.Error("a guest's run would be resumed at boot")
		}
	}

	// Young: left alone.
	h.server.CleanupNow(ctx)
	if guestExists(t, h.pool, guest.ID) != true {
		t.Fatal("a guest minutes old was swept")
	}
	if _, err := h.pool.Exec(ctx, `UPDATE users SET created_at = now() - interval '3 hours' WHERE id = $1`, guest.ID); err != nil {
		t.Fatal(err)
	}
	h.server.CleanupNow(ctx)
	if guestExists(t, h.pool, guest.ID) {
		t.Error("a guest three hours old survived the sweep")
	}
	h.blobs.mu.Lock()
	_, left := h.blobs.blobs["uploads/guest/test/"+tag]
	h.blobs.mu.Unlock()
	if left {
		t.Error("the guest's file survived the sweep")
	}
}

func guestExists(t *testing.T, pool *pgxpool.Pool, id uuid.UUID) bool {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM users WHERE id = $1`, id).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n > 0
}

func TestGuestSettings(t *testing.T) {
	h := newGuestHarness(t)
	admin := sqlc.User{ID: uuid.New(), Email: "admin@test.invalid", Role: sqlc.UserRoleAdmin}
	patch := func(body string) (int, SystemSettings) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPatch, "/", strings.NewReader(body))
		h.server.UpdateSystemSettings(rec, req.WithContext(auth.WithUser(req.Context(), admin)))
		var out SystemSettings
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		return rec.Code, out
	}
	h.set(settingGuestMode, false)
	code, out := patch(`{"guestMode":true,"guestHourlyLimit":30,"guestDailyLimit":500}`)
	if code != 200 || !out.GuestMode || out.GuestHourlyLimit != 30 || out.GuestDailyLimit != 500 {
		t.Fatalf("update: %d %+v", code, out)
	}
	// Out of range changes nothing, not even what was valid beside it.
	code, _ = patch(`{"guestMode":false,"guestHourlyLimit":-1}`)
	if code != http.StatusBadRequest {
		t.Fatalf("a negative limit: %d", code)
	}
	code, _ = patch(`{"guestDailyLimit":99999999999}`)
	if code != http.StatusBadRequest {
		t.Fatalf("a huge limit: %d", code)
	}
	if !h.server.guestEnabled(context.Background()) {
		t.Error("a refused update changed the switch")
	}
	code, out = patch(`{}`)
	if code != 200 || out.GuestHourlyLimit != 30 {
		t.Errorf("read back: %d %+v", code, out)
	}
}
