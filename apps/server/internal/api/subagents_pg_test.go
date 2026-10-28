package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/qunqin24/openchat/apps/server/internal/agent"
	"github.com/qunqin24/openchat/apps/server/internal/auth"
	"github.com/qunqin24/openchat/apps/server/internal/db/dbtest"
	"github.com/qunqin24/openchat/apps/server/internal/db/sqlc"
)

// Sub-agents end to end: a real runner and server against a scripted fake
// OpenAI-compatible endpoint. The script answers each request by what it is -
// the main agent's, a sub-agent's, a title - so a test reads as the
// conversation it stages. The settings are global, so these tests share a
// database of their own and do not run in parallel.

// fakeReq is a chat request as the fake endpoint read it.
type fakeReq struct {
	System   string
	Messages []fakeMsg
	Tools    []string
}

type fakeMsg struct {
	Role  string
	Text  string
	Calls []string
}

// kind is whose request it is: "title", "sub" or "main".
func (r fakeReq) kind() string {
	switch {
	case strings.Contains(r.System+r.all(), "Write a short title"):
		return "title"
	case strings.HasPrefix(r.System, "You are a sub-agent"):
		return "sub"
	}
	return "main"
}

// all is every message's text.
func (r fakeReq) all() string {
	var b strings.Builder
	for _, m := range r.Messages {
		b.WriteString(m.Text)
		b.WriteString("\n")
	}
	return b.String()
}

// first is the first user message's text: which task a sub-agent has.
func (r fakeReq) first() string {
	for _, m := range r.Messages {
		if m.Role == "user" {
			return m.Text
		}
	}
	return ""
}

// last is the last message.
func (r fakeReq) last() fakeMsg {
	if len(r.Messages) == 0 {
		return fakeMsg{}
	}
	return r.Messages[len(r.Messages)-1]
}

// toolResults is how many tool results the request carries.
func (r fakeReq) toolResults() int {
	n := 0
	for _, m := range r.Messages {
		if m.Role == "tool" {
			n++
		}
	}
	return n
}

type fakeTurn struct {
	Text  string
	Calls []fakeCall
}

type fakeCall struct {
	Name string
	Args map[string]any
}

type subFixture struct {
	t      *testing.T
	pool   *pgxpool.Pool
	q      *sqlc.Queries
	server *Server
	runner *agent.Runner
	user   sqlc.User
	model  string

	script atomic.Pointer[func(context.Context, fakeReq) fakeTurn]
	mu     sync.Mutex
	seen   []fakeReq
}

func newSubFixture(t *testing.T) *subFixture {
	t.Helper()
	pool := dbtest.Pool(t, "api_subagents")
	q := sqlc.New(pool)
	ctx := context.Background()
	f := &subFixture{t: t, pool: pool, q: q}

	endpoint := httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(endpoint.Close)

	tag := uuid.NewString()
	if err := pool.QueryRow(ctx, `INSERT INTO users (email, name) VALUES ($1, $2) RETURNING id, email, name, role`,
		tag+"@test.invalid", "sub-"+tag).Scan(&f.user.ID, &f.user.Email, &f.user.Name, &f.user.Role); err != nil {
		t.Fatal(err)
	}
	f.model = "fake-sub-" + tag
	channel, err := q.CreateChannel(ctx, sqlc.CreateChannelParams{
		Name: "fake " + tag, Type: sqlc.ChannelTypeOpenaiCompatible, Enabled: true,
		BaseUrl: endpoint.URL + "/v1", ApiKey: "sk-test",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := q.CreateSelectedChannelModels(ctx, sqlc.CreateSelectedChannelModelsParams{
		ChannelID: channel.ID, ModelIds: []string{f.model}, DisplayNames: []string{""}, Sources: []string{"manual"},
	}); err != nil {
		t.Fatal(err)
	}

	f.runner = agent.NewRunner(pool, agent.NewHub(), NewModelResolver(pool), NewToolSource(pool),
		agent.Options{SystemPrompt: NewSystemPrompt(pool), PartialInterval: 5 * time.Millisecond})
	f.server = &Server{pool: pool, q: q, runner: f.runner, usage: f.runner.Recorder()}
	f.runner.AddTools(f.server.ServerTools)
	f.runner.SetTurnNote(f.server.TurnNote)
	f.runner.SetSubagents(f.server.SubagentHooks())
	f.runner.AfterRun(f.server.AfterRun)
	t.Cleanup(func() {
		_ = f.runner.Shutdown(context.Background())
		_, _ = pool.Exec(context.Background(), "DELETE FROM users WHERE id = $1", f.user.ID)
		_, _ = pool.Exec(context.Background(), "DELETE FROM channels WHERE id = $1", channel.ID)
	})

	f.setting(settingSubagents, true)
	f.setting(settingSubagentModel, "")
	f.setting(settingSubagentBackground, true)
	f.setting(settingSubagentMaxPerRun, defaultSubagentMaxPerRun)
	f.setting(settingSubagentMaxPerConversation, defaultSubagentMaxPerConversation)
	f.setting(settingSubagentMaxPerUser, defaultSubagentMaxPerUser)
	f.setting(settingMaxRuns, defaultMaxRuns)
	return f
}

func (f *subFixture) setting(key string, value any) {
	f.t.Helper()
	raw, _ := json.Marshal(value)
	if err := f.q.SetSetting(context.Background(), sqlc.SetSettingParams{Key: key, Value: raw}); err != nil {
		f.t.Fatal(err)
	}
}

// play sets what the fake model answers. Titles are answered here.
func (f *subFixture) play(script func(ctx context.Context, r fakeReq) fakeTurn) {
	f.script.Store(&script)
}

func (f *subFixture) requests(kind string) []fakeReq {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []fakeReq
	for _, r := range f.seen {
		if kind == "" || r.kind() == kind {
			out = append(out, r)
		}
	}
	return out
}

func (f *subFixture) serve(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Messages []struct {
			Role      string          `json:"role"`
			Content   json.RawMessage `json:"content"`
			ToolCalls []struct {
				Function struct {
					Name string `json:"name"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"messages"`
		Tools []struct {
			Function struct {
				Name string `json:"name"`
			} `json:"function"`
		} `json:"tools"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	var req fakeReq
	for _, m := range body.Messages {
		msg := fakeMsg{Role: m.Role, Text: contentText(m.Content)}
		for _, c := range m.ToolCalls {
			msg.Calls = append(msg.Calls, c.Function.Name)
		}
		if m.Role == "system" {
			req.System = msg.Text
			continue
		}
		req.Messages = append(req.Messages, msg)
	}
	for _, tool := range body.Tools {
		req.Tools = append(req.Tools, tool.Function.Name)
	}
	f.mu.Lock()
	f.seen = append(f.seen, req)
	f.mu.Unlock()

	turn := fakeTurn{Text: "A title"}
	if req.kind() != "title" {
		if script := f.script.Load(); script != nil {
			turn = (*script)(r.Context(), req)
		} else {
			turn = fakeTurn{Text: "ok"}
		}
	}
	if r.Context().Err() != nil {
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	frame := func(v any) {
		raw, _ := json.Marshal(v)
		_, _ = fmt.Fprintf(w, "data: %s\n\n", raw)
		w.(http.Flusher).Flush()
	}
	chunk := func(delta map[string]any, finish any) map[string]any {
		return map[string]any{"id": "x", "model": "fake", "choices": []any{map[string]any{"index": 0, "delta": delta, "finish_reason": finish}}}
	}
	if len(turn.Calls) > 0 {
		for i, call := range turn.Calls {
			args, _ := json.Marshal(call.Args)
			frame(chunk(map[string]any{"role": "assistant", "tool_calls": []any{map[string]any{
				"index": i, "id": fmt.Sprintf("call_%s", uuid.NewString()[:8]), "type": "function",
				"function": map[string]any{"name": call.Name, "arguments": string(args)},
			}}}, nil))
		}
		frame(chunk(map[string]any{}, "tool_calls"))
	} else {
		frame(chunk(map[string]any{"role": "assistant", "content": turn.Text}, nil))
		frame(chunk(map[string]any{}, "stop"))
	}
	_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
}

// contentText is a message's text, whether the content is a string or parts.
func contentText(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var parts []struct {
		Text string `json:"text"`
	}
	_ = json.Unmarshal(raw, &parts)
	var b strings.Builder
	for _, p := range parts {
		b.WriteString(p.Text)
	}
	return b.String()
}

func (f *subFixture) newConversation() sqlc.Conversation {
	f.t.Helper()
	conv, err := f.q.CreateConversation(context.Background(), sqlc.CreateConversationParams{UserID: &f.user.ID, Title: "New chat", Model: &f.model})
	if err != nil {
		f.t.Fatal(err)
	}
	return conv
}

func (f *subFixture) call(method string, body string, handle func(w http.ResponseWriter, r *http.Request)) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(method, "/", strings.NewReader(body))
	handle(rec, req.WithContext(auth.WithUser(req.Context(), f.user)))
	return rec
}

func (f *subFixture) send(conv sqlc.Conversation, text string) {
	f.t.Helper()
	body, _ := json.Marshal(map[string]any{"content": text, "model": f.model})
	rec := f.call(http.MethodPost, string(body), func(w http.ResponseWriter, r *http.Request) { f.server.CreateMessage(w, r, conv.ID) })
	if rec.Code != http.StatusAccepted {
		f.t.Fatalf("send: %d %s", rec.Code, rec.Body)
	}
}

func (f *subFixture) waitFor(what string, cond func() bool) {
	f.t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			f.t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// settled waits until nothing runs in the conversation or its sub-agents and
// no report waits for it.
func (f *subFixture) settled(conv sqlc.Conversation) {
	f.t.Helper()
	f.waitFor("the conversation to settle", func() bool {
		var n int
		_ = f.pool.QueryRow(context.Background(), `
			SELECT (SELECT count(*) FROM runs r JOIN conversations c ON c.id = r.conversation_id
			        WHERE (c.id = $1 OR c.parent_id = $1) AND r.status = 'running')
			     + (SELECT count(*) FROM subagent_reports WHERE parent_id = $1 AND delivered_message_id IS NULL AND NOT via_tool)`,
			conv.ID).Scan(&n)
		return n == 0
	})
}

func (f *subFixture) runs(conv sqlc.Conversation) []sqlc.Run {
	f.t.Helper()
	rows, err := f.pool.Query(context.Background(), `SELECT id, status, input_message_id FROM runs WHERE conversation_id = $1 ORDER BY created_at, id`, conv.ID)
	if err != nil {
		f.t.Fatal(err)
	}
	defer rows.Close()
	var out []sqlc.Run
	for rows.Next() {
		var r sqlc.Run
		if err := rows.Scan(&r.ID, &r.Status, &r.InputMessageID); err != nil {
			f.t.Fatal(err)
		}
		out = append(out, r)
	}
	return out
}

func (f *subFixture) children(conv sqlc.Conversation) []sqlc.Conversation {
	f.t.Helper()
	list, err := f.q.ListSubagents(context.Background(), sqlc.ListSubagentsParams{ParentID: &conv.ID, UserID: &f.user.ID})
	if err != nil {
		f.t.Fatal(err)
	}
	return list
}

func (f *subFixture) reports(conv sqlc.Conversation) []sqlc.SubagentReport {
	f.t.Helper()
	rows, err := f.pool.Query(context.Background(), `SELECT run_id, conversation_id, state, report, via_tool, delivered_message_id
		FROM subagent_reports WHERE parent_id = $1 ORDER BY created_at, run_id`, conv.ID)
	if err != nil {
		f.t.Fatal(err)
	}
	defer rows.Close()
	var out []sqlc.SubagentReport
	for rows.Next() {
		var r sqlc.SubagentReport
		if err := rows.Scan(&r.RunID, &r.ConversationID, &r.State, &r.Report, &r.ViaTool, &r.DeliveredMessageID); err != nil {
			f.t.Fatal(err)
		}
		out = append(out, r)
	}
	return out
}

// reportRows are the conversation's rows carrying reports.
func (f *subFixture) reportRows(conv sqlc.Conversation) []sqlc.Message {
	f.t.Helper()
	all, err := f.q.ListMessages(context.Background(), sqlc.ListMessagesParams{ConversationID: conv.ID, RowLimit: 500})
	if err != nil {
		f.t.Fatal(err)
	}
	var out []sqlc.Message
	for i := len(all) - 1; i >= 0; i-- {
		if all[i].Origin != nil && *all[i].Origin == "subagent" {
			out = append(out, all[i])
		}
	}
	return out
}

func launch(name, task string, background bool) fakeCall {
	return fakeCall{Name: ToolSubagent, Args: map[string]any{"name": name, "task": task, "background": background}}
}

// waitUntil blocks a fake response until cond holds or the request ends.
func waitUntil(ctx context.Context, cond func() bool) {
	for !cond() && ctx.Err() == nil {
		time.Sleep(10 * time.Millisecond)
	}
}

// A foreground sub-agent: the call waits, and the report is its result -
// never also delivered as a row. The sub-agent sees only its task, not the
// conversation; its requests are logged as kind subagent on the conversation.
func TestSubagentForeground(t *testing.T) {
	f := newSubFixture(t)
	conv := f.newConversation()
	f.play(func(ctx context.Context, r fakeReq) fakeTurn {
		switch r.kind() {
		case "sub":
			if strings.Contains(r.all(), "SECRET-QUESTION") {
				return fakeTurn{Text: "I saw the conversation"}
			}
			return fakeTurn{Text: "REPORT-FG: prices are 3, 4 and 5."}
		}
		if r.toolResults() == 0 {
			return fakeTurn{Calls: []fakeCall{launch("价格对比", "Compare prices", false)}}
		}
		return fakeTurn{Text: "The answer."}
	})
	f.send(conv, "SECRET-QUESTION about prices")
	f.settled(conv)

	mains := f.requests("main")
	if len(mains) != 2 || !strings.Contains(mains[1].all(), "REPORT-FG") || !strings.Contains(mains[1].all(), "<subagent-report") {
		t.Fatalf("the report should be the call's result: %+v", mains)
	}
	subs := f.requests("sub")
	if len(subs) != 1 || strings.Contains(subs[0].all(), "SECRET-QUESTION") || !strings.HasSuffix(subs[0].first(), "Compare prices") {
		t.Fatalf("the sub-agent should see its task alone: %+v", subs)
	}
	for _, tool := range subs[0].Tools {
		if strings.HasPrefix(tool, "subagent") || tool == "ask_user" || tool == "memory" || tool == ToolScheduledTasks {
			t.Fatalf("a sub-agent was offered %s", tool)
		}
	}
	reports := f.reports(conv)
	if len(reports) != 1 || !reports[0].ViaTool || reports[0].DeliveredMessageID != nil || reports[0].State != "completed" {
		t.Fatalf("reports = %+v", reports)
	}
	if rows := f.reportRows(conv); len(rows) != 0 {
		t.Fatalf("a foreground report was also delivered as a row: %+v", rows)
	}
	if runs := f.runs(conv); len(runs) != 1 {
		t.Fatalf("one run expected, got %d", len(runs))
	}
	var subagentLogs int
	if err := f.pool.QueryRow(context.Background(), `SELECT count(*) FROM usage_logs WHERE conversation_id = $1 AND kind = 'subagent'`, conv.ID).Scan(&subagentLogs); err != nil {
		t.Fatal(err)
	}
	if subagentLogs != 1 {
		t.Fatalf("subagent usage rows = %d", subagentLogs)
	}
	// The sub-agent is not a conversation the person can reach.
	child := f.children(conv)[0]
	if rec := f.call(http.MethodGet, "", func(w http.ResponseWriter, r *http.Request) {
		f.server.ListMessages(w, r, child.ID, ListMessagesParams{})
	}); rec.Code != http.StatusNotFound {
		t.Fatalf("listing a sub-agent's messages as a conversation: %d", rec.Code)
	}
	if rec := f.call(http.MethodPost, `{"content":"hi"}`, func(w http.ResponseWriter, r *http.Request) {
		f.server.CreateMessage(w, r, child.ID)
	}); rec.Code != http.StatusNotFound {
		t.Fatalf("sending to a sub-agent: %d", rec.Code)
	}
	list, err := f.q.ListConversations(context.Background(), sqlc.ListConversationsParams{UserID: &f.user.ID, RowLimit: 100})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range list {
		if c.Kind != "chat" {
			t.Fatalf("a sub-agent is listed: %+v", c)
		}
	}
	// The read-only view.
	rec := f.call(http.MethodGet, "", func(w http.ResponseWriter, r *http.Request) { f.server.GetSubagent(w, r, conv.ID, child.ID) })
	var detail SubagentDetail
	_ = json.Unmarshal(rec.Body.Bytes(), &detail)
	if rec.Code != http.StatusOK || detail.Subagent.Name != "价格对比" || detail.Subagent.State != SubagentStateCompleted ||
		len(detail.Messages) != 2 || detail.Messages[0].Origin == nil || *detail.Messages[0].Origin != MessageOriginAgent {
		t.Fatalf("detail = %d %+v", rec.Code, detail)
	}
}

// A background sub-agent whose report arrives while the conversation is idle
// starts a run of its own, with the report as its input.
func TestSubagentBackgroundDeliveredWhenIdle(t *testing.T) {
	f := newSubFixture(t)
	conv := f.newConversation()
	release := make(chan struct{})
	f.play(func(ctx context.Context, r fakeReq) fakeTurn {
		if r.kind() == "sub" {
			select {
			case <-release:
			case <-ctx.Done():
			}
			return fakeTurn{Text: "REPORT-BG done."}
		}
		if strings.Contains(r.last().Text, "<subagent-report") {
			return fakeTurn{Text: "Here is what it found."}
		}
		if r.toolResults() == 0 {
			return fakeTurn{Calls: []fakeCall{launch("资料调研", "Research", true)}}
		}
		return fakeTurn{Text: "I launched a researcher."}
	})
	f.send(conv, "research this")
	f.waitFor("the first run to end", func() bool {
		runs := f.runs(conv)
		return len(runs) == 1 && runs[0].Status == sqlc.RunStatusCompleted
	})
	if rows := f.reportRows(conv); len(rows) != 0 {
		t.Fatal("a report before the sub-agent finished")
	}
	close(release)
	f.settled(conv)

	runs := f.runs(conv)
	rows := f.reportRows(conv)
	if len(runs) != 2 || len(rows) != 1 || runs[1].InputMessageID != rows[0].ID || rows[0].RunID != nil {
		t.Fatalf("runs %+v, report rows %+v", runs, rows)
	}
	if !strings.Contains(rows[0].Content, `name="资料调研"`) || !strings.Contains(rows[0].Content, "REPORT-BG") ||
		!strings.Contains(rows[0].Content, "not written by the user") {
		t.Fatalf("report row = %q", rows[0].Content)
	}
	reports := f.reports(conv)
	if len(reports) != 1 || reports[0].DeliveredMessageID == nil || *reports[0].DeliveredMessageID != rows[0].ID {
		t.Fatalf("reports = %+v", reports)
	}
	// The run started for it answered it.
	mains := f.requests("main")
	if last := mains[len(mains)-1]; !strings.Contains(last.last().Text, "REPORT-BG") {
		t.Fatalf("the delivered run's request = %+v", last.last())
	}
	// The report row is no question: regenerate answers the person's.
	if last, err := f.q.GetLastInput(context.Background(), conv.ID); err != nil || last.Origin != nil {
		t.Fatalf("last input = %+v, %v", last, err)
	}
}

// A report that arrives while a run of the conversation is going is written
// into that run before its next request, and no other run is started.
func TestSubagentDeliveredWhileRunning(t *testing.T) {
	f := newSubFixture(t)
	conv := f.newConversation()
	var child atomic.Pointer[uuid.UUID]
	f.play(func(ctx context.Context, r fakeReq) fakeTurn {
		if r.kind() == "sub" {
			return fakeTurn{Text: "REPORT-LIVE"}
		}
		switch {
		case strings.Contains(r.all(), "<subagent-report"):
			return fakeTurn{Text: "Folded the report in."}
		case r.toolResults() == 0:
			return fakeTurn{Calls: []fakeCall{launch("快速查", "Look it up", true)}}
		default:
			// Still working when the report lands: wait for it, then take
			// another step, before whose request it is delivered.
			waitUntil(ctx, func() bool { return len(f.reports(conv)) == 1 })
			list := f.children(conv)
			id := list[0].ID
			child.Store(&id)
			return fakeTurn{Calls: []fakeCall{{Name: ToolSubagentStop, Args: map[string]any{"to": id.String()}}}}
		}
	})
	f.send(conv, "go")
	f.settled(conv)

	runs := f.runs(conv)
	rows := f.reportRows(conv)
	if len(runs) != 1 || len(rows) != 1 || rows[0].RunID == nil || *rows[0].RunID != runs[0].ID {
		t.Fatalf("runs %+v, report rows %+v", runs, rows)
	}
	mains := f.requests("main")
	if !strings.Contains(mains[len(mains)-1].all(), "REPORT-LIVE") {
		t.Fatal("the run's next request did not carry the report")
	}
}

// The end-of-turn race: the report lands after the run's last request was
// made, while it is still going. The run ending must pick it up and start the
// next run (the sub-agent's own ending saw the conversation busy).
func TestSubagentReportAtTheEndOfATurn(t *testing.T) {
	f := newSubFixture(t)
	conv := f.newConversation()
	f.play(func(ctx context.Context, r fakeReq) fakeTurn {
		if r.kind() == "sub" {
			return fakeTurn{Text: "REPORT-RACE"}
		}
		switch {
		case strings.Contains(r.last().Text, "<subagent-report"):
			return fakeTurn{Text: "Got it."}
		case r.toolResults() == 0:
			return fakeTurn{Calls: []fakeCall{launch("赛跑", "Race", true)}}
		default:
			// The final answer, held until the report is in: it was made
			// before the report, and the run ends after it.
			waitUntil(ctx, func() bool { return len(f.reports(conv)) == 1 })
			time.Sleep(100 * time.Millisecond) // the sub-agent's ending looks while this run is going
			return fakeTurn{Text: "Launched."}
		}
	})
	f.send(conv, "go")
	f.waitFor("the report to be delivered", func() bool { return len(f.reportRows(conv)) == 1 })
	f.settled(conv)
	runs := f.runs(conv)
	rows := f.reportRows(conv)
	if len(runs) != 2 || runs[1].InputMessageID != rows[0].ID {
		t.Fatalf("runs %+v, report rows %+v", runs, rows)
	}
}

// A delivered report whose row is deleted - the answer that received it
// regenerated - is pending again and reaches the new answer.
func TestSubagentReportRedeliveredAfterRegenerate(t *testing.T) {
	f := newSubFixture(t)
	conv := f.newConversation()
	release := make(chan struct{})
	f.play(func(ctx context.Context, r fakeReq) fakeTurn {
		if r.kind() == "sub" {
			select {
			case <-release:
			case <-ctx.Done():
			}
			return fakeTurn{Text: "REPORT-AGAIN"}
		}
		if strings.Contains(r.first(), "first question") && !strings.Contains(r.all(), "second question") {
			if r.toolResults() == 0 {
				return fakeTurn{Calls: []fakeCall{launch("后台", "Background work", true)}}
			}
			return fakeTurn{Text: "Launched."}
		}
		// The second question's answer: a step, before which the report is
		// delivered, then an answer.
		if !strings.Contains(r.all(), "<subagent-report") {
			waitUntil(ctx, func() bool { return len(f.reports(conv)) == 1 })
			return fakeTurn{Calls: []fakeCall{{Name: ToolSubagentStop, Args: map[string]any{"to": "后台"}}}}
		}
		return fakeTurn{Text: "Answer with the report."}
	})
	f.send(conv, "first question")
	f.waitFor("the first answer", func() bool { return len(f.runs(conv)) == 1 && f.runs(conv)[0].Status == sqlc.RunStatusCompleted })
	f.send(conv, "second question")
	f.waitFor("the second run to be waiting", func() bool { return len(f.runs(conv)) == 2 })
	close(release)
	f.settled(conv)
	rows := f.reportRows(conv)
	runs := f.runs(conv)
	if len(rows) != 1 || len(runs) != 2 || rows[0].RunID == nil || *rows[0].RunID != runs[1].ID {
		t.Fatalf("before: rows %+v runs %+v", rows, runs)
	}
	first := rows[0].ID

	rec := f.call(http.MethodPost, `{}`, func(w http.ResponseWriter, r *http.Request) { f.server.RegenerateAnswer(w, r, conv.ID) })
	if rec.Code != http.StatusAccepted {
		t.Fatalf("regenerate: %d %s", rec.Code, rec.Body)
	}
	f.settled(conv)
	rows = f.reportRows(conv)
	runs = f.runs(conv)
	if len(rows) != 1 || rows[0].ID == first || len(runs) != 2 || rows[0].RunID == nil || *rows[0].RunID != runs[1].ID {
		t.Fatalf("after: rows %+v runs %+v", rows, runs)
	}
	reports := f.reports(conv)
	if len(reports) != 1 || reports[0].DeliveredMessageID == nil || *reports[0].DeliveredMessageID != rows[0].ID {
		t.Fatalf("reports = %+v", reports)
	}
	// The sub-agent launched by the first answer survived the second's regeneration.
	if len(f.children(conv)) != 1 {
		t.Fatal("the sub-agent went with an answer that did not launch it")
	}
}

// A message to a finished sub-agent continues it - a new run, so a second
// report; one to a working sub-agent is read before its next request.
func TestSubagentMessageContinuesAndSteers(t *testing.T) {
	f := newSubFixture(t)
	conv := f.newConversation()
	var subRequests atomic.Int32
	f.play(func(ctx context.Context, r fakeReq) fakeTurn {
		if r.kind() == "sub" {
			n := subRequests.Add(1)
			if n == 1 {
				// Working: a tool round, during which the main agent writes.
				waitUntil(ctx, func() bool {
					var queued int
					_ = f.pool.QueryRow(ctx, `SELECT count(*) FROM subagent_inbox`).Scan(&queued)
					return queued > 0
				})
				return fakeTurn{Calls: []fakeCall{{Name: "web_fetch", Args: map[string]any{"url": "http://127.0.0.1:9/"}}}}
			}
			if strings.Contains(r.last().Text, "focus on Europe") {
				return fakeTurn{Text: "REPORT-1 Europe only"}
			}
			if strings.Contains(r.last().Text, "now Asia") {
				return fakeTurn{Text: "REPORT-2 Asia"}
			}
			return fakeTurn{Text: "REPORT-? " + r.last().Text}
		}
		last := r.last().Text
		switch {
		case strings.Contains(last, "REPORT-2"):
			return fakeTurn{Text: "Asia too."}
		case strings.Contains(last, "REPORT-1"):
			return fakeTurn{Calls: []fakeCall{{Name: ToolSubagentMessage, Args: map[string]any{"to": "调研", "message": "now Asia"}}}}
		case r.toolResults() == 0:
			return fakeTurn{Calls: []fakeCall{launch("调研", "Research markets", true)}}
		case r.toolResults() == 1:
			return fakeTurn{Calls: []fakeCall{{Name: ToolSubagentMessage, Args: map[string]any{"to": "调研", "message": "focus on Europe"}}}}
		default:
			return fakeTurn{Text: "OK."}
		}
	})
	f.send(conv, "research markets")
	f.waitFor("two reports", func() bool { return len(f.reports(conv)) == 2 })
	f.settled(conv)
	reports := f.reports(conv)
	if !strings.Contains(reports[0].Report, "REPORT-1") || !strings.Contains(reports[1].Report, "REPORT-2") || reports[0].RunID == reports[1].RunID {
		t.Fatalf("reports = %+v", reports)
	}
	child := f.children(conv)[0]
	if runs := f.runs(child); len(runs) != 2 {
		t.Fatalf("the sub-agent should have run twice, got %d", len(runs))
	}
	// The steering message went in as the main agent's row, not the person's.
	msgs, _ := f.q.ListMessages(context.Background(), sqlc.ListMessagesParams{ConversationID: child.ID, RowLimit: 100})
	steered := false
	for _, m := range msgs {
		if m.Content == "focus on Europe" && m.Origin != nil && *m.Origin == "agent" && m.RunID != nil {
			steered = true
		}
	}
	if !steered {
		t.Fatal("the steering message was not written into the working run")
	}
}

// The person stops a sub-agent: it reports what it has, the main agent's
// messages no longer wake it, and "继续" does.
func TestSubagentStopAndResume(t *testing.T) {
	f := newSubFixture(t)
	conv := f.newConversation()
	var subRuns atomic.Int32
	f.play(func(ctx context.Context, r fakeReq) fakeTurn {
		if r.kind() == "sub" {
			if strings.Contains(r.last().Text, resumeText) {
				return fakeTurn{Text: "REPORT-RESUMED"}
			}
			subRuns.Add(1)
			<-ctx.Done() // works until stopped
			return fakeTurn{}
		}
		last := r.last().Text
		switch {
		case strings.Contains(last, "<subagent-report"):
			if strings.Contains(last, `state="cancelled"`) {
				return fakeTurn{Calls: []fakeCall{{Name: ToolSubagentMessage, Args: map[string]any{"to": "长任务", "message": "keep going"}}}}
			}
			return fakeTurn{Text: "Done."}
		case r.toolResults() == 0:
			return fakeTurn{Calls: []fakeCall{launch("长任务", "Long work", true)}}
		default:
			return fakeTurn{Text: "OK."}
		}
	})
	f.send(conv, "go")
	f.waitFor("the sub-agent to work", func() bool { return subRuns.Load() == 1 })
	child := f.children(conv)[0]
	if rec := f.call(http.MethodPost, "", func(w http.ResponseWriter, r *http.Request) { f.server.StopSubagent(w, r, conv.ID, child.ID) }); rec.Code != http.StatusAccepted {
		t.Fatalf("stop: %d", rec.Code)
	}
	f.settled(conv)
	reports := f.reports(conv)
	if len(reports) != 1 || reports[0].State != "cancelled" {
		t.Fatalf("reports = %+v", reports)
	}
	// The main agent tried to wake it and was told no.
	var refused int
	_ = f.pool.QueryRow(context.Background(), `SELECT count(*) FROM messages WHERE conversation_id = $1 AND role = 'tool' AND parts::text LIKE '%user stopped%'`, conv.ID).Scan(&refused)
	if refused != 1 || len(f.runs(child)) != 1 {
		t.Fatalf("a stopped sub-agent was woken by the main agent (refused %d, runs %d)", refused, len(f.runs(child)))
	}
	if rec := f.call(http.MethodPost, "", func(w http.ResponseWriter, r *http.Request) { f.server.ResumeSubagent(w, r, conv.ID, child.ID) }); rec.Code != http.StatusAccepted {
		t.Fatalf("resume: %d %s", rec.Code, rec.Body)
	}
	f.waitFor("the second report", func() bool { return len(f.reports(conv)) == 2 })
	f.settled(conv)
	reports = f.reports(conv)
	if !strings.Contains(reports[1].Report, "REPORT-RESUMED") {
		t.Fatalf("reports = %+v", reports)
	}
}

// The limits: launches per run, and sub-agents working per conversation (a
// background launch past it is refused); the person's own run limit does not
// count sub-agents.
func TestSubagentLimits(t *testing.T) {
	f := newSubFixture(t)
	f.setting(settingSubagentMaxPerRun, 2)
	f.setting(settingMaxRuns, 1)
	release := make(chan struct{})
	t.Cleanup(func() {
		select {
		case <-release:
		default:
			close(release)
		}
	})
	f.play(func(ctx context.Context, r fakeReq) fakeTurn {
		if r.kind() == "sub" {
			if strings.Contains(r.first(), "slow") {
				select {
				case <-release:
				case <-ctx.Done():
				}
			}
			return fakeTurn{Text: "report of " + r.first()}
		}
		switch {
		case strings.Contains(r.first(), "other conversation"):
			return fakeTurn{Text: "fine"}
		case strings.Contains(r.last().Text, "<subagent-report"):
			return fakeTurn{Text: "noted"}
		case r.toolResults() > 0:
			return fakeTurn{Text: "stopped launching"}
		case strings.Contains(r.first(), "three"):
			return fakeTurn{Calls: []fakeCall{launch("一", "one", true), launch("二", "two", true), launch("三", "three", true)}}
		default:
			return fakeTurn{Calls: []fakeCall{launch("慢", "slow one", true), launch("也慢", "slow two", true)}}
		}
	})
	results := func(conv sqlc.Conversation) string {
		var out []string
		rows, _ := f.pool.Query(context.Background(), `SELECT parts::text FROM messages WHERE conversation_id = $1 AND role = 'tool' ORDER BY created_at`, conv.ID)
		for rows.Next() {
			var c string
			_ = rows.Scan(&c)
			out = append(out, c)
		}
		rows.Close()
		return strings.Join(out, "\n")
	}

	// Three launches in one reply, two allowed.
	conv := f.newConversation()
	f.send(conv, "launch three")
	f.settled(conv)
	if got := results(conv); len(f.children(conv)) != 2 || !strings.Contains(got, "already launched 2") {
		t.Fatalf("children %d, results:\n%s", len(f.children(conv)), got)
	}

	// One working at a time for the conversation: the second is refused.
	f.setting(settingSubagentMaxPerConversation, 1)
	busy := f.newConversation()
	f.send(busy, "launch two")
	f.waitFor("the launches to be answered", func() bool {
		runs := f.runs(busy)
		return len(runs) == 1 && runs[0].Status == sqlc.RunStatusCompleted
	})
	if got := results(busy); len(f.children(busy)) != 1 || !strings.Contains(got, "Too many sub-agents are working") {
		t.Fatalf("children %d, results:\n%s", len(f.children(busy)), got)
	}
	// The person can still send elsewhere: sub-agents do not hold their slot.
	other := f.newConversation()
	f.send(other, "other conversation")
	f.settled(other)
	close(release)
	f.settled(busy)
}

// A foreground sub-agent that has to wait for a slot waits, then runs.
func TestSubagentForegroundWaitsForASlot(t *testing.T) {
	f := newSubFixture(t)
	f.setting(settingSubagentMaxPerConversation, 1)
	conv := f.newConversation()
	release := make(chan struct{})
	f.play(func(ctx context.Context, r fakeReq) fakeTurn {
		if r.kind() == "sub" {
			if strings.Contains(r.first(), "slow") {
				select {
				case <-release:
				case <-ctx.Done():
				}
			}
			return fakeTurn{Text: "report of " + r.first()}
		}
		if strings.Contains(r.last().Text, "<subagent-report") && r.toolResults() == 0 {
			return fakeTurn{Text: "noted"}
		}
		switch r.toolResults() {
		case 0:
			return fakeTurn{Calls: []fakeCall{launch("慢", "slow one", true)}}
		case 1:
			go func() { time.Sleep(300 * time.Millisecond); close(release) }()
			return fakeTurn{Calls: []fakeCall{launch("等", "waiting one", false)}}
		}
		return fakeTurn{Text: "done"}
	})
	f.send(conv, "go")
	f.settled(conv)
	mains := f.requests("main")
	found := false
	for _, r := range mains {
		if strings.Contains(r.all(), `name="等"`) && strings.Contains(r.all(), "waiting one\n</subagent-report>") {
			found = true
		}
	}
	if !found {
		for _, r := range mains {
			t.Logf("main request: %s", r.all())
		}
		t.Fatal("the foreground sub-agent's report never came back")
	}
}

// A foreground sub-agent with no slot is there from its launch, queued: listed
// as such, with no run, and running once a slot frees - in launch order, the
// same row throughout. (Three launched by one reply run their calls at once,
// so which takes the slot first is a race; the other two queue.)
func TestSubagentQueuedIsVisibleAndStartsWhenASlotFrees(t *testing.T) {
	f := newSubFixture(t)
	f.setting(settingSubagentMaxPerConversation, 1)
	conv := f.newConversation()
	release := make(chan struct{})
	t.Cleanup(func() {
		select {
		case <-release:
		default:
			close(release)
		}
	})
	f.play(func(ctx context.Context, r fakeReq) fakeTurn {
		if r.kind() == "sub" {
			select {
			case <-release:
			case <-ctx.Done():
			}
			return fakeTurn{Text: "report of " + r.first()}
		}
		if r.toolResults() == 0 {
			return fakeTurn{Calls: []fakeCall{launch("甲", "one", false), launch("乙", "two", false), launch("丙", "three", false)}}
		}
		return fakeTurn{Text: "done"}
	})
	f.send(conv, "go")
	list := func() []Subagent {
		out, err := f.server.subagentList(context.Background(), conv.ID, f.user.ID)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	var queued []Subagent
	f.waitFor("one running and two queued", func() bool {
		queued = queued[:0]
		running := 0
		all := list()
		for _, a := range all {
			switch a.State {
			case SubagentStateQueued:
				queued = append(queued, a)
			case SubagentStateRunning:
				running++
			}
		}
		return len(all) == 3 && running == 1 && len(queued) == 2
	})
	for _, a := range queued {
		if runs := f.runs(sqlc.Conversation{ID: a.Id}); len(runs) != 0 {
			t.Fatalf("a queued sub-agent has runs: %+v", runs)
		}
	}
	// Its task is there to read already.
	rec := f.call(http.MethodGet, "", func(w http.ResponseWriter, r *http.Request) { f.server.GetSubagent(w, r, conv.ID, queued[0].Id) })
	var detail SubagentDetail
	_ = json.Unmarshal(rec.Body.Bytes(), &detail)
	if rec.Code != http.StatusOK || detail.Subagent.State != SubagentStateQueued || len(detail.Messages) != 1 || detail.Run != nil {
		t.Fatalf("queued detail = %d %+v", rec.Code, detail)
	}
	close(release)
	f.settled(conv)
	for _, a := range list() {
		if a.State != SubagentStateCompleted {
			t.Fatalf("%s ended %s", a.Name, a.State)
		}
	}
	// The same sub-agents, started in the order they were launched.
	startOf := func(id uuid.UUID) time.Time {
		var at time.Time
		if err := f.pool.QueryRow(context.Background(), `SELECT created_at FROM runs WHERE conversation_id = $1`, id).Scan(&at); err != nil {
			t.Fatal(err)
		}
		return at
	}
	first, second := queued[0], queued[1] // listed oldest first
	if !startOf(first.Id).Before(startOf(second.Id)) {
		t.Fatal("queued sub-agents started out of their launch order")
	}
}

// A queued sub-agent moved to the background: the call that launched it
// returns at once, so the reply can end; it starts by itself when a slot
// frees, and its report is delivered like any background one's.
func TestSubagentQueuedMovedToTheBackground(t *testing.T) {
	f := newSubFixture(t)
	f.setting(settingSubagentMaxPerConversation, 1)
	conv := f.newConversation()
	release := make(chan struct{})
	t.Cleanup(func() {
		select {
		case <-release:
		default:
			close(release)
		}
	})
	f.play(func(ctx context.Context, r fakeReq) fakeTurn {
		if r.kind() == "sub" {
			if strings.Contains(r.first(), "hold") {
				select {
				case <-release:
				case <-ctx.Done():
				}
			}
			return fakeTurn{Text: "REPORT of " + r.first()}
		}
		switch {
		case strings.Contains(r.last().Text, "<subagent-report"):
			return fakeTurn{Text: "got the report"}
		case r.toolResults() == 0:
			// The holder first, so it takes the slot; then the one that queues.
			return fakeTurn{Calls: []fakeCall{launch("占位", "hold the slot", true)}}
		case r.toolResults() == 1:
			return fakeTurn{Calls: []fakeCall{launch("排着", "queued work", false)}}
		}
		return fakeTurn{Text: "answered"}
	})
	f.send(conv, "go")
	var queued Subagent
	f.waitFor("a queued sub-agent", func() bool {
		list, _ := f.server.subagentList(context.Background(), conv.ID, f.user.ID)
		for _, a := range list {
			if a.State == SubagentStateQueued {
				queued = a
				return true
			}
		}
		return false
	})
	if rec := f.call(http.MethodPost, "", func(w http.ResponseWriter, r *http.Request) { f.server.BackgroundSubagent(w, r, conv.ID, queued.Id) }); rec.Code != http.StatusAccepted {
		t.Fatalf("background: %d", rec.Code)
	}
	// The reply ends while the holder still has the slot.
	f.waitFor("the reply to end", func() bool {
		runs := f.runs(conv)
		return len(runs) == 1 && runs[0].Status == sqlc.RunStatusCompleted
	})
	if runs := f.runs(sqlc.Conversation{ID: queued.Id}); len(runs) != 0 {
		t.Fatal("the moved sub-agent started without a slot")
	}
	var said int
	_ = f.pool.QueryRow(context.Background(), `SELECT count(*) FROM messages WHERE conversation_id = $1 AND role = 'tool' AND parts::text LIKE '%while it was queued%'`, conv.ID).Scan(&said)
	if said != 1 {
		t.Fatal("the call did not say it was moved to the background while queued")
	}
	// The sweep must not take it for an orphan.
	_, _ = f.pool.Exec(context.Background(), `UPDATE conversations SET created_at = created_at - interval '1 hour' WHERE id = $1`, queued.Id)
	f.server.SweepSubagents(context.Background())
	if _, err := f.q.GetSubagentByID(context.Background(), queued.Id); err != nil {
		t.Fatal("the sweep removed a sub-agent queued in the background")
	}
	// The slot frees: it starts by itself, and its report is delivered.
	close(release)
	f.settled(conv)
	reports := f.reports(conv)
	var mine *sqlc.SubagentReport
	for i := range reports {
		if reports[i].ConversationID == queued.Id {
			mine = &reports[i]
		}
	}
	if mine == nil || mine.ViaTool || mine.DeliveredMessageID == nil || !strings.Contains(mine.Report, "queued work") {
		t.Fatalf("the moved sub-agent's report: %+v", mine)
	}
}

// A queued sub-agent goes when its wait ends without a start: the run that
// launched it stopped, or the person cancelled it.
func TestSubagentQueuedIsCancelledWithItsParentOrByThePerson(t *testing.T) {
	f := newSubFixture(t)
	f.setting(settingSubagentMaxPerConversation, 1)
	f.play(func(ctx context.Context, r fakeReq) fakeTurn {
		if r.kind() == "sub" {
			<-ctx.Done()
			return fakeTurn{}
		}
		if r.toolResults() == 0 {
			return fakeTurn{Calls: []fakeCall{launch("占位", "hold the slot", false), launch("等着", "wait", false)}}
		}
		return fakeTurn{Text: "over"}
	})
	// Two launched at once with one slot: one runs (until stopped), one queues.
	var holder Subagent
	queuedIn := func(conv sqlc.Conversation) Subagent {
		var found Subagent
		f.waitFor("a queued sub-agent", func() bool {
			list, _ := f.server.subagentList(context.Background(), conv.ID, f.user.ID)
			for _, a := range list {
				switch a.State {
				case SubagentStateQueued:
					found = a
				case SubagentStateRunning:
					holder = a
				}
			}
			return found.Id != uuid.Nil && holder.Id != uuid.Nil
		})
		return found
	}
	goneFrom := func(id uuid.UUID) {
		f.waitFor("the queued sub-agent to go", func() bool {
			_, err := f.q.GetSubagentByID(context.Background(), id)
			return err != nil
		})
	}

	// The person stops the reply.
	conv := f.newConversation()
	f.send(conv, "go")
	queued := queuedIn(conv)
	main := f.runs(conv)[0]
	if rec := f.call(http.MethodPost, "", func(w http.ResponseWriter, r *http.Request) { f.server.CancelRun(w, r, conv.ID, main.ID) }); rec.Code != http.StatusAccepted {
		t.Fatalf("cancel: %d", rec.Code)
	}
	goneFrom(queued.Id)

	// The person cancels the queued sub-agent: the call says so, the reply goes on.
	other := f.newConversation()
	f.send(other, "go")
	queued = queuedIn(other)
	if rec := f.call(http.MethodPost, "", func(w http.ResponseWriter, r *http.Request) { f.server.StopSubagent(w, r, other.ID, queued.Id) }); rec.Code != http.StatusAccepted {
		t.Fatalf("stop: %d", rec.Code)
	}
	goneFrom(queued.Id)
	// The other one is still being waited for; stopped, the reply ends.
	if rec := f.call(http.MethodPost, "", func(w http.ResponseWriter, r *http.Request) { f.server.StopSubagent(w, r, other.ID, holder.Id) }); rec.Code != http.StatusAccepted {
		t.Fatalf("stop: %d", rec.Code)
	}
	f.waitFor("the reply to finish", func() bool {
		runs := f.runs(other)
		return len(runs) == 1 && runs[0].Status == sqlc.RunStatusCompleted
	})
	var cancelled int
	_ = f.pool.QueryRow(context.Background(), `SELECT count(*) FROM messages WHERE conversation_id = $1 AND role = 'tool' AND parts::text LIKE '%cancelled sub-agent%'`, other.ID).Scan(&cancelled)
	if cancelled != 1 {
		t.Fatal("the call did not say the person cancelled it")
	}

	// Deleting the conversation stops the reply, and with it the wait.
	third := f.newConversation()
	f.send(third, "go")
	queued = queuedIn(third)
	if rec := f.call(http.MethodDelete, "", func(w http.ResponseWriter, r *http.Request) { f.server.DeleteConversation(w, r, third.ID) }); rec.Code != http.StatusNoContent {
		t.Fatalf("delete: %d", rec.Code)
	}
	goneFrom(queued.Id)
}

// Sub-agents go with the answer that launched them: an edit or a regeneration
// deletes them and stops their runs; deleting the conversation stops them too.
func TestSubagentCascades(t *testing.T) {
	f := newSubFixture(t)
	f.play(func(ctx context.Context, r fakeReq) fakeTurn {
		if r.kind() == "sub" {
			<-ctx.Done()
			return fakeTurn{}
		}
		if r.toolResults() == 0 {
			return fakeTurn{Calls: []fakeCall{launch("一直跑", "Run forever", true)}}
		}
		return fakeTurn{Text: "launched"}
	})
	childRunning := func(child sqlc.Conversation) bool {
		_, err := f.q.GetActiveRun(context.Background(), child.ID)
		return err == nil
	}
	launched := func(conv sqlc.Conversation) sqlc.Conversation {
		f.waitFor("the sub-agent to start", func() bool {
			list := f.children(conv)
			return len(list) == 1 && childRunning(list[0]) && f.runs(conv)[len(f.runs(conv))-1].Status != sqlc.RunStatusRunning
		})
		return f.children(conv)[0]
	}
	gone := func(child sqlc.Conversation) {
		f.waitFor("the sub-agent's run to stop", func() bool {
			var n int
			_ = f.pool.QueryRow(context.Background(), `SELECT count(*) FROM runs WHERE conversation_id = $1 AND status = 'running'`, child.ID).Scan(&n)
			return n == 0
		})
	}

	// Regenerate.
	conv := f.newConversation()
	f.send(conv, "q")
	child := launched(conv)
	if rec := f.call(http.MethodPost, `{}`, func(w http.ResponseWriter, r *http.Request) { f.server.RegenerateAnswer(w, r, conv.ID) }); rec.Code != http.StatusAccepted {
		t.Fatalf("regenerate: %d %s", rec.Code, rec.Body)
	}
	gone(child)
	if _, err := f.q.GetSubagentByID(context.Background(), child.ID); err == nil {
		t.Fatal("the regenerated answer's sub-agent survived")
	}
	newChild := launched(conv)

	// Edit.
	input, err := f.q.GetLastInput(context.Background(), conv.ID)
	if err != nil {
		t.Fatal(err)
	}
	if rec := f.call(http.MethodPost, `{"content":"q2"}`, func(w http.ResponseWriter, r *http.Request) {
		f.server.EditMessage(w, r, conv.ID, input.ID)
	}); rec.Code != http.StatusAccepted {
		t.Fatalf("edit: %d %s", rec.Code, rec.Body)
	}
	gone(newChild)
	if _, err := f.q.GetSubagentByID(context.Background(), newChild.ID); err == nil {
		t.Fatal("the edited answer's sub-agent survived")
	}
	last := launched(conv)

	// Delete the conversation.
	if rec := f.call(http.MethodDelete, "", func(w http.ResponseWriter, r *http.Request) { f.server.DeleteConversation(w, r, conv.ID) }); rec.Code != http.StatusNoContent {
		t.Fatalf("delete: %d %s", rec.Code, rec.Body)
	}
	gone(last)
}
