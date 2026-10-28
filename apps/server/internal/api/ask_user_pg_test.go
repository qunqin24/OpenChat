package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/qunqin24/openchat/apps/server/internal/agent"
	"github.com/qunqin24/openchat/apps/server/internal/auth"
	"github.com/qunqin24/openchat/apps/server/internal/db/sqlc"
	"github.com/qunqin24/openchat/apps/server/internal/tools"
)

// The ask_user tool end to end, over HTTP handlers and a scripted model that
// speaks each wire format: the model asks, the run stops awaiting input, and
// the user answers, writes something else, or redoes the turn. Needs the same
// scratch database as the other agent tests (TEST_DATABASE_URL).

const questionsJSON = `{"questions":[` +
	`{"question":"Where to?","header":"Place","options":[{"label":"Beach","description":"sand"},{"label":"City"}],"multiSelect":false},` +
	`{"question":"What to do?","header":"Plans","options":[{"label":"Museums"},{"label":"Food"},{"label":"Hikes"}],"multiSelect":true}]}`

// wire is one provider format: how the scripted model answers in it, and how
// the request it was sent is read back.
type wire struct {
	name        string
	channelType sqlc.ChannelType
	ask         func(callID, args string) string
	text        func(text string) string
	// calls and results are the tool-call ids of the history the request carried,
	// in order: those the assistant made, and those that were answered.
	calls, results func(t *testing.T, body string) []string
	// order is the history as role:kind pairs, to check results sit between a
	// call and what the user wrote next.
	order func(t *testing.T, body string) []string
}

var openAIWire = wire{
	name:        "openai",
	channelType: sqlc.ChannelTypeOpenaiCompatible,
	ask: func(callID, args string) string {
		quoted, _ := json.Marshal(args)
		return fmt.Sprintf(`data: {"id":"r","model":"fake","choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[{"index":0,"id":%q,"type":"function","function":{"name":"ask_user","arguments":%s}}]},"finish_reason":null}]}`+"\n\n"+
			`data: {"id":"r","model":"fake","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`+"\n\n"+"data: [DONE]\n\n", callID, quoted)
	},
	text: func(text string) string {
		quoted, _ := json.Marshal(text)
		return fmt.Sprintf(`data: {"id":"r","model":"fake","choices":[{"index":0,"delta":{"role":"assistant","content":%s},"finish_reason":"stop"}]}`+"\n\ndata: [DONE]\n\n", quoted)
	},
	calls: func(t *testing.T, body string) []string {
		var ids []string
		for _, m := range openAIMessages(t, body) {
			for _, c := range m.ToolCalls {
				ids = append(ids, c.ID)
			}
		}
		return ids
	},
	results: func(t *testing.T, body string) []string {
		var ids []string
		for _, m := range openAIMessages(t, body) {
			if m.Role == "tool" {
				ids = append(ids, m.ToolCallID)
			}
		}
		return ids
	},
	order: func(t *testing.T, body string) []string {
		var out []string
		for _, m := range openAIMessages(t, body) {
			switch {
			case m.Role == "system":
			case len(m.ToolCalls) > 0:
				out = append(out, "assistant:call")
			default:
				out = append(out, m.Role)
			}
		}
		return out
	},
}

type openAIMessage struct {
	Role       string `json:"role"`
	ToolCallID string `json:"tool_call_id"`
	ToolCalls  []struct {
		ID string `json:"id"`
	} `json:"tool_calls"`
}

func openAIMessages(t *testing.T, body string) []openAIMessage {
	t.Helper()
	var req struct {
		Messages []openAIMessage `json:"messages"`
	}
	if err := json.Unmarshal([]byte(body), &req); err != nil {
		t.Fatalf("request body: %v", err)
	}
	return req.Messages
}

var anthropicWire = wire{
	name:        "anthropic",
	channelType: sqlc.ChannelTypeAnthropic,
	ask: func(callID, args string) string {
		quoted, _ := json.Marshal(args)
		return fmt.Sprintf("event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"m\",\"type\":\"message\",\"role\":\"assistant\",\"model\":\"claude\",\"content\":[],\"usage\":{\"input_tokens\":5,\"output_tokens\":1}}}\n\n"+
			"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"tool_use\",\"id\":%q,\"name\":\"ask_user\",\"input\":{}}}\n\n"+
			"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"input_json_delta\",\"partial_json\":%s}}\n\n"+
			"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n"+
			"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"tool_use\"},\"usage\":{\"output_tokens\":9}}\n\n"+
			"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n", callID, quoted)
	},
	text: func(text string) string {
		quoted, _ := json.Marshal(text)
		return "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"m\",\"type\":\"message\",\"role\":\"assistant\",\"model\":\"claude\",\"content\":[],\"usage\":{\"input_tokens\":5,\"output_tokens\":1}}}\n\n" +
			"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n" +
			fmt.Sprintf("event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":%s}}\n\n", quoted) +
			"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n" +
			"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":3}}\n\n" +
			"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"
	},
	calls: func(t *testing.T, body string) []string {
		var ids []string
		for _, m := range anthropicMessages(t, body) {
			for _, b := range m.Content {
				if b.Type == "tool_use" {
					ids = append(ids, b.ID)
				}
			}
		}
		return ids
	},
	results: func(t *testing.T, body string) []string {
		var ids []string
		for _, m := range anthropicMessages(t, body) {
			for _, b := range m.Content {
				if b.Type == "tool_result" {
					ids = append(ids, b.ToolUseID)
				}
			}
		}
		return ids
	},
	order: func(t *testing.T, body string) []string {
		var out []string
		for _, m := range anthropicMessages(t, body) {
			kinds := []string{}
			for _, b := range m.Content {
				switch b.Type {
				case "tool_use":
					kinds = append(kinds, "call")
				case "tool_result":
					kinds = append(kinds, "result")
				case "text":
					kinds = append(kinds, "text")
				}
			}
			out = append(out, m.Role+":"+strings.Join(kinds, "+"))
		}
		return out
	},
}

type anthropicMessage struct {
	Role    string `json:"role"`
	Content []struct {
		Type      string `json:"type"`
		ID        string `json:"id"`
		ToolUseID string `json:"tool_use_id"`
	} `json:"content"`
}

func anthropicMessages(t *testing.T, body string) []anthropicMessage {
	t.Helper()
	var req struct {
		Messages []anthropicMessage `json:"messages"`
	}
	if err := json.Unmarshal([]byte(body), &req); err != nil {
		t.Fatalf("request body: %v", err)
	}
	return req.Messages
}

var wires = []wire{openAIWire, anthropicWire}

// askHarness is a conversation answered by a scripted model.
type askHarness struct {
	t      *testing.T
	pool   *pgxpool.Pool
	q      *sqlc.Queries
	server *Server
	user   sqlc.User
	conv   sqlc.Conversation
	model  string
	wire   wire

	mu     sync.Mutex
	bodies []string
	reply  func(n int) string
}

// newAskHarness serves the model from a fake endpoint of w's wire format; reply
// is the nth request's response (from 1), in that format.
func newAskHarness(t *testing.T, w wire, reply func(n int) string) *askHarness {
	t.Helper()
	pool := agentTestPool(t)
	ctx := context.Background()
	h := &askHarness{t: t, pool: pool, q: sqlc.New(pool), wire: w, reply: reply}

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
		tag+"@test.invalid", "ask-"+tag).Scan(&h.user.ID, &h.user.Email, &h.user.Name, &h.user.Role); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM users WHERE id = $1", h.user.ID) })
	var err error
	if h.conv, err = h.q.CreateConversation(ctx, sqlc.CreateConversationParams{UserID: &h.user.ID, Title: "ask"}); err != nil {
		t.Fatal(err)
	}
	h.model = "fake-model-" + tag
	channel, err := h.q.CreateChannel(ctx, sqlc.CreateChannelParams{
		Name: "fake " + tag, Type: w.channelType, Enabled: true, BaseUrl: endpoint.URL + "/v1", ApiKey: "sk-test",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.q.CreateSelectedChannelModels(ctx, sqlc.CreateSelectedChannelModelsParams{
		ChannelID: channel.ID, ModelIds: []string{h.model}, DisplayNames: []string{""}, Sources: []string{"manual"},
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM channels WHERE id = $1", channel.ID) })

	runner := agent.NewRunner(pool, agent.NewHub(), NewModelResolver(pool),
		func(context.Context) []agent.Tool { return []agent.Tool{tools.AskUser{}} },
		agent.Options{SystemPrompt: NewSystemPrompt(pool), PartialInterval: 5 * time.Millisecond})
	t.Cleanup(func() { _ = runner.Shutdown(context.Background()) })
	h.server = &Server{pool: pool, q: h.q, runner: runner}
	return h
}

func (h *askHarness) request(method, body string) *http.Request {
	r := httptest.NewRequest(method, "/", strings.NewReader(body))
	return r.WithContext(auth.WithUser(r.Context(), h.user))
}

func (h *askHarness) accepted(rec *httptest.ResponseRecorder, into any) {
	h.t.Helper()
	if rec.Code != http.StatusAccepted {
		h.t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	if err := json.Unmarshal(rec.Body.Bytes(), into); err != nil {
		h.t.Fatal(err)
	}
}

// send writes a message and waits for its run to stop, returning how.
func (h *askHarness) send(content string) (CreateMessageResponse, sqlc.RunStatus) {
	h.t.Helper()
	rec := httptest.NewRecorder()
	h.server.CreateMessage(rec, h.request(http.MethodPost, fmt.Sprintf(`{"content":%q,"model":%q}`, content, h.model)), h.conv.ID)
	var out CreateMessageResponse
	h.accepted(rec, &out)
	return out, h.settle(out.Run.Id)
}

func (h *askHarness) answer(callID, body string) *httptest.ResponseRecorder {
	h.t.Helper()
	rec := httptest.NewRecorder()
	h.server.AnswerQuestion(rec, h.request(http.MethodPost, body), h.conv.ID, callID)
	return rec
}

func (h *askHarness) answered(callID, body string) (AnswerQuestionResponse, sqlc.RunStatus) {
	h.t.Helper()
	var out AnswerQuestionResponse
	h.accepted(h.answer(callID, body), &out)
	return out, h.settle(out.Run.Id)
}

// settle waits for a run to leave `running` and returns where it stopped.
func (h *askHarness) settle(runID uuid.UUID) sqlc.RunStatus {
	h.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		run, err := h.q.GetRun(context.Background(), runID)
		if err != nil {
			h.t.Fatal(err)
		}
		if run.Status != sqlc.RunStatusRunning {
			return run.Status
		}
		time.Sleep(10 * time.Millisecond)
	}
	h.t.Fatal("run did not stop")
	return ""
}

func (h *askHarness) transcript() []Message {
	h.t.Helper()
	rec := httptest.NewRecorder()
	h.server.ListMessages(rec, h.request(http.MethodGet, ""), h.conv.ID, ListMessagesParams{})
	var page MessagePage
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		h.t.Fatal(err)
	}
	return page.Items
}

// shape is the transcript as role[:parts], parts being call, result or text.
func shape(t *testing.T, items []Message) []string {
	t.Helper()
	var out []string
	for _, m := range items {
		kinds := []string{}
		for _, p := range m.Parts {
			raw, _ := json.Marshal(p)
			var typed struct{ Type string }
			_ = json.Unmarshal(raw, &typed)
			switch typed.Type {
			case "tool_call":
				kinds = append(kinds, "call")
			case "tool_result":
				kinds = append(kinds, "result")
			case "text":
				kinds = append(kinds, "text")
			}
		}
		out = append(out, string(m.Role)+":"+strings.Join(kinds, "+"))
	}
	return out
}

func equal(t *testing.T, what string, got, want []string) {
	t.Helper()
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("%s: got %q, want %q", what, got, want)
	}
}

func (h *askHarness) body(n int) string {
	h.t.Helper()
	h.mu.Lock()
	defer h.mu.Unlock()
	if n > len(h.bodies) {
		h.t.Fatalf("only %d requests were made, wanted the %dth", len(h.bodies), n)
	}
	return h.bodies[n-1]
}

// askThenText asks on the first request and answers in text after.
func askThenText(w wire) func(n int) string {
	return func(n int) string {
		if n == 1 {
			return w.ask("call_ask_1", questionsJSON)
		}
		return w.text(fmt.Sprintf("noted (%d)", n))
	}
}

const answersJSON = `{"answers":[{"selected":["City"]},{"selected":["Food","Hikes"],"other":"and coffee"}]}`

// A run that asks stops awaiting input; answering writes the call's result and
// starts a run that carries the same reply on.
func TestAskUserAnswerContinuesTheReply(t *testing.T) {
	for _, w := range wires {
		t.Run(w.name, func(t *testing.T) {
			h := newAskHarness(t, w, askThenText(w))
			first, status := h.send("plan my trip")
			if status != sqlc.RunStatusAwaitingInput {
				t.Fatalf("run ended %s, want awaiting_input", status)
			}
			equal(t, "transcript", shape(t, h.transcript()), []string{"user:text", "assistant:call"})

			// Stopping a run that is not running changes nothing.
			rec := httptest.NewRecorder()
			h.server.CancelRun(rec, h.request(http.MethodPost, ""), h.conv.ID, first.Run.Id)
			if rec.Code != http.StatusAccepted || h.settle(first.Run.Id) != sqlc.RunStatusAwaitingInput {
				t.Fatalf("cancel of an awaiting run: %d, now %s", rec.Code, h.settle(first.Run.Id))
			}

			// Answers that do not fit the questions are refused, and nothing is written.
			for name, body := range map[string]string{
				"too few":        `{"answers":[{"selected":["City"]}]}`,
				"unknown option": `{"answers":[{"selected":["Moon"]},{"selected":["Food"]}]}`,
				"two for one":    `{"answers":[{"selected":["City","Beach"]},{"selected":["Food"]}]}`,
				"unanswered":     `{"answers":[{"selected":[]},{"selected":["Food"]}]}`,
			} {
				if rec := h.answer("call_ask_1", body); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "invalid_answers") {
					t.Fatalf("%s: %d %s", name, rec.Code, rec.Body)
				}
			}
			equal(t, "transcript after refusals", shape(t, h.transcript()), []string{"user:text", "assistant:call"})

			// Not the call that was asked.
			if rec := h.answer("call_other", answersJSON); rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "not_pending") {
				t.Fatalf("answer to another call: %d %s", rec.Code, rec.Body)
			}

			resp, status := h.answered("call_ask_1", answersJSON)
			if status != sqlc.RunStatusCompleted {
				t.Fatalf("continuation ended %s", status)
			}
			if resp.Run.Continues == nil || *resp.Run.Continues != first.Run.Id {
				t.Fatalf("continues = %v, want %s", resp.Run.Continues, first.Run.Id)
			}
			if resp.Message.Id != first.Message.Id {
				t.Fatalf("the continuation answers %s, not %s", resp.Message.Id, first.Message.Id)
			}

			items := h.transcript()
			equal(t, "transcript", shape(t, items), []string{"user:text", "assistant:call", "tool:result", "assistant:text"})
			raw, _ := json.Marshal(items[2].Parts[0])
			var result struct {
				CallID, Content, Status string
				Details                 struct {
					Answers []tools.Answer
				}
			}
			_ = json.Unmarshal(raw, &result)
			if result.CallID != "call_ask_1" || result.Status != "succeeded" ||
				!strings.Contains(result.Content, "Chose: City") || !strings.Contains(result.Content, "Chose: Food; Hikes") ||
				!strings.Contains(result.Content, "Wrote: and coffee") {
				t.Fatalf("result = %+v", result)
			}
			if len(result.Details.Answers) != 2 || result.Details.Answers[1].Other != "and coffee" || result.Details.Answers[0].Selected[0] != "City" {
				t.Fatalf("details = %+v", result.Details)
			}
			if *items[2].RunId != resp.Run.Id || *items[3].RunId != resp.Run.Id || *items[1].RunId != first.Run.Id {
				t.Fatal("rows belong to the wrong runs")
			}

			// The model's second request carried the call and its result, in order.
			second := h.body(2)
			equal(t, "calls", w.calls(t, second), []string{"call_ask_1"})
			equal(t, "results", w.results(t, second), []string{"call_ask_1"})
			if !strings.Contains(second, "and coffee") {
				t.Fatalf("the answers did not reach the model: %s", second)
			}

			// Answered once.
			if rec := h.answer("call_ask_1", answersJSON); rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "not_pending") {
				t.Fatalf("answering twice: %d %s", rec.Code, rec.Body)
			}
		})
	}
}

// A retried answer finds the run it started rather than a second one.
func TestAskUserAnswerIsIdempotentByRequestID(t *testing.T) {
	h := newAskHarness(t, openAIWire, askThenText(openAIWire))
	h.send("plan")
	body := `{"requestId":"ans-1","answers":[{"selected":["City"]},{"selected":["Food"]}]}`
	first, _ := h.answered("call_ask_1", body)
	var again AnswerQuestionResponse
	h.accepted(h.answer("call_ask_1", body), &again)
	if again.Run.Id != first.Run.Id || again.Result.Id != first.Result.Id || again.Message.Id != first.Message.Id {
		t.Fatalf("retry started %v, want %v", again.Run.Id, first.Run.Id)
	}
}

// Writing in the composer instead of answering: the new message is a turn of
// its own, the unanswered call is closed off in what the model is sent, and the
// question can no longer be answered.
func TestAskUserTypedMessageSkipsTheQuestion(t *testing.T) {
	for _, w := range wires {
		t.Run(w.name, func(t *testing.T) {
			h := newAskHarness(t, w, askThenText(w))
			h.send("plan my trip")
			if _, status := h.send("just pick something for me"); status != sqlc.RunStatusCompleted {
				t.Fatalf("second run ended %s", status)
			}
			equal(t, "transcript", shape(t, h.transcript()),
				[]string{"user:text", "assistant:call", "user:text", "assistant:text"})

			// A valid transcript for this wire: every call has its result, and
			// what the user wrote comes after the results.
			second := h.body(2)
			equal(t, "calls", w.calls(t, second), []string{"call_ask_1"})
			equal(t, "results", w.results(t, second), []string{"call_ask_1"})
			if !strings.Contains(second, "did not answer this question") {
				t.Fatalf("the model was not told the question was skipped: %s", second)
			}
			switch w.name {
			case "openai":
				equal(t, "order", w.order(t, second), []string{"user", "assistant:call", "tool", "user"})
			case "anthropic":
				equal(t, "order", w.order(t, second), []string{"user:text+text", "assistant:call", "user:result+text"})
			}

			if rec := h.answer("call_ask_1", answersJSON); rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "not_pending") {
				t.Fatalf("answering a skipped question: %d %s", rec.Code, rec.Body)
			}
		})
	}
}

// Regenerating, or editing the question, deletes the pending question with the
// answer it was part of: it is not a card in the transcript any more, and it
// can no longer be answered.
func TestAskUserRedoingTheTurnDeletesThePendingQuestion(t *testing.T) {
	reply := func(n int) string {
		switch n {
		case 1:
			return openAIWire.ask("call_ask_1", questionsJSON)
		case 2:
			return openAIWire.ask("call_ask_2", questionsJSON)
		}
		return openAIWire.text("plain answer")
	}

	t.Run("regenerate", func(t *testing.T) {
		h := newAskHarness(t, openAIWire, reply)
		first, _ := h.send("plan")

		rec := httptest.NewRecorder()
		h.server.RegenerateAnswer(rec, h.request(http.MethodPost, `{}`), h.conv.ID)
		var again CreateMessageResponse
		h.accepted(rec, &again)
		if h.settle(again.Run.Id) != sqlc.RunStatusAwaitingInput {
			t.Fatal("the regenerated run did not ask")
		}

		equal(t, "transcript", shape(t, h.transcript()), []string{"user:text", "assistant:call"})
		if strings.Contains(h.body(2), "call_ask_1") {
			t.Fatal("the deleted question reached the regenerated request")
		}
		if _, err := h.q.GetRun(context.Background(), first.Run.Id); !errors.Is(err, pgx.ErrNoRows) {
			t.Fatalf("the run that asked is still there (%v)", err)
		}
		// The first ask is gone: only the second can be answered.
		if rec := h.answer("call_ask_1", answersJSON); rec.Code != http.StatusConflict {
			t.Fatalf("answering the deleted question: %d %s", rec.Code, rec.Body)
		}
		if _, status := h.answered("call_ask_2", answersJSON); status != sqlc.RunStatusCompleted {
			t.Fatalf("answering the second ask: the continuation ended %s", status)
		}
	})

	t.Run("edit", func(t *testing.T) {
		h := newAskHarness(t, openAIWire, reply)
		first, _ := h.send("plan")
		rec := httptest.NewRecorder()
		h.server.EditMessage(rec, h.request(http.MethodPost, `{"content":"plan it differently"}`), h.conv.ID, first.Message.Id)
		var edited CreateMessageResponse
		h.accepted(rec, &edited)
		h.settle(edited.Run.Id)

		items := h.transcript()
		if len(items) != 2 || items[0].Content != "plan it differently" {
			t.Fatalf("transcript after the edit: %v", shape(t, items))
		}
		if rec := h.answer("call_ask_1", answersJSON); rec.Code != http.StatusConflict {
			t.Fatalf("answering the replaced question: %d %s", rec.Code, rec.Body)
		}
	})
}

// A reply that asked and went on is one answer: regenerating deletes both runs
// and everything they wrote.
func TestAskUserContinuationGoesWithTheAnswer(t *testing.T) {
	h := newAskHarness(t, openAIWire, func(n int) string {
		if n == 1 {
			return openAIWire.ask("call_ask_1", questionsJSON)
		}
		return openAIWire.text(fmt.Sprintf("answer %d", n))
	})
	first, _ := h.send("plan")
	continued, _ := h.answered("call_ask_1", answersJSON)
	equal(t, "before", shape(t, h.transcript()), []string{"user:text", "assistant:call", "tool:result", "assistant:text"})

	rec := httptest.NewRecorder()
	h.server.RegenerateAnswer(rec, h.request(http.MethodPost, `{}`), h.conv.ID)
	var again CreateMessageResponse
	h.accepted(rec, &again)
	h.settle(again.Run.Id)

	equal(t, "after regenerating", shape(t, h.transcript()), []string{"user:text", "assistant:text"})
	var runs, rows int
	if err := h.pool.QueryRow(context.Background(), `SELECT count(*) FROM runs WHERE id = ANY ($1)`,
		[]uuid.UUID{first.Run.Id, continued.Run.Id}).Scan(&runs); err != nil || runs != 0 {
		t.Fatalf("%d of the old answer's runs remain (%v)", runs, err)
	}
	if err := h.pool.QueryRow(context.Background(), `SELECT count(*) FROM messages WHERE conversation_id = $1`, h.conv.ID).Scan(&rows); err != nil || rows != 2 {
		t.Fatalf("%d rows in the conversation (%v), want the question and the new answer", rows, err)
	}
}

// The administrator's switch decides whether the tool is offered, and is on
// until it is turned off.
func TestAskUserToggle(t *testing.T) {
	pool := agentTestPool(t)
	ctx := context.Background()
	q := sqlc.New(pool)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM app_settings WHERE key = $1", settingAskUser)
	})
	_, _ = pool.Exec(ctx, "DELETE FROM app_settings WHERE key = $1", settingAskUser)

	var admin sqlc.User
	tag := uuid.NewString()
	if err := pool.QueryRow(ctx, `INSERT INTO users (email, name, role) VALUES ($1, $2, 'admin') RETURNING id, email, name, role`,
		tag+"@test.invalid", "toggle-"+tag).Scan(&admin.ID, &admin.Email, &admin.Name, &admin.Role); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM users WHERE id = $1", admin.ID) })
	server := &Server{pool: pool, q: q}

	offered := func() []agent.Tool { return NewToolSource(pool)(ctx) }
	has := func(tools []agent.Tool) bool {
		for _, tool := range tools {
			if tool.Definition().Name == agent.ToolAskUser {
				return true
			}
		}
		return false
	}
	setting := func() bool {
		rec := httptest.NewRecorder()
		server.GetSystemSettings(rec, httptest.NewRequest(http.MethodGet, "/", nil).WithContext(auth.WithUser(ctx, admin)))
		var s SystemSettings
		if err := json.Unmarshal(rec.Body.Bytes(), &s); err != nil {
			t.Fatal(err)
		}
		return s.AskUser
	}

	if !has(offered()) || !setting() {
		t.Fatal("ask_user is not on by default")
	}
	if prompt := basePrompt(offered()); !strings.Contains(prompt, "ask_user") {
		t.Fatalf("the prompt does not mention the tool when it is offered: %s", prompt)
	}

	rec := httptest.NewRecorder()
	server.UpdateSystemSettings(rec, httptest.NewRequest(http.MethodPatch, "/", strings.NewReader(`{"askUser":false}`)).WithContext(auth.WithUser(ctx, admin)))
	if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), `"askUser":true`) {
		t.Fatalf("turning it off: %d %s", rec.Code, rec.Body)
	}
	if has(offered()) || setting() {
		t.Fatal("ask_user is still offered after being turned off")
	}
	if prompt := basePrompt(offered()); strings.Contains(prompt, "ask_user") {
		t.Fatalf("the prompt still mentions the tool: %s", prompt)
	}

	rec = httptest.NewRecorder()
	server.UpdateSystemSettings(rec, httptest.NewRequest(http.MethodPatch, "/", strings.NewReader(`{"askUser":true}`)).WithContext(auth.WithUser(ctx, admin)))
	if !has(offered()) || !setting() {
		t.Fatal("ask_user is not offered after being turned back on")
	}
}

// A reply that asks and also calls another tool ends with that tool's result,
// not with the question; the question is still the one waiting, and answering
// it carries the reply on.
func TestAskUserAnswerAfterASiblingCallsResult(t *testing.T) {
	quoted, _ := json.Marshal(questionsJSON)
	both := `data: {"id":"r","model":"fake","choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[` +
		`{"index":0,"id":"call_other","type":"function","function":{"name":"no_such_tool","arguments":"{}"}},` +
		`{"index":1,"id":"call_ask_1","type":"function","function":{"name":"ask_user","arguments":` + string(quoted) + `}}]},"finish_reason":null}]}` + "\n\n" +
		`data: {"id":"r","model":"fake","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}` + "\n\n" + "data: [DONE]\n\n"
	h := newAskHarness(t, openAIWire, func(n int) string {
		if n == 1 {
			return both
		}
		return openAIWire.text("Here is the plan.")
	})
	_, status := h.send("plan my trip")
	if status != sqlc.RunStatusAwaitingInput {
		t.Fatalf("run ended %s, want awaiting_input", status)
	}
	equal(t, "transcript", shape(t, h.transcript()), []string{"user:text", "assistant:call+call", "tool:result"})

	if _, status := h.answered("call_ask_1", answersJSON); status != sqlc.RunStatusCompleted {
		t.Fatalf("continuation ended %s", status)
	}
	equal(t, "transcript", shape(t, h.transcript()), []string{"user:text", "assistant:call+call", "tool:result", "tool:result", "assistant:text"})
	if rec := h.answer("call_ask_1", answersJSON); rec.Code != http.StatusConflict {
		t.Fatalf("answering twice: %d %s", rec.Code, rec.Body)
	}
}
