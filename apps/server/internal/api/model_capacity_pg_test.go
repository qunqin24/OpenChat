package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/qunqin/openchat/apps/server/internal/agent"
	"github.com/qunqin/openchat/apps/server/internal/auth"
	"github.com/qunqin/openchat/apps/server/internal/db/sqlc"
	"github.com/qunqin/openchat/apps/server/internal/tools"
)

// Per-model spending caps against a real database and, where a run is needed,
// a fake OpenAI-compatible endpoint that reports token usage and a catalogue
// that prices the model, so the cost the cap counts is the one a real request
// would have logged.

// capFixture is a priced fake model on its own channel, a member and an
// administrator, a server whose clock the test sets, and a runner that checks
// caps.
type capFixture struct {
	t       *testing.T
	pool    *pgxpool.Pool
	q       *sqlc.Queries
	server  *Server
	runner  *agent.Runner
	tag     string
	modelID string
	member  sqlc.User
	admin   sqlc.User

	// requests is how many times the fake endpoint was asked; a request the cap
	// stopped never reaches it.
	requests atomic.Int32
	// toolFirst makes the first request a tool call, so a run has a second
	// request to be stopped before.
	toolFirst bool
	// promptTokens is the input each request reports: at $3 per million, 100000
	// of them cost $0.30.
	promptTokens int

	// hold, when set, has the endpoint take each request up - it streams a first
	// chunk of text, no usage yet - and then wait for hold to be closed or for
	// the client to go away, as a model does for as long as it is writing. The
	// requests that are waiting are counted in held.
	hold chan struct{}
	// holdSilently waits before sending anything, not even the first chunk.
	holdSilently bool
	// noUsage has the endpoint leave its usage out, as some gateways do.
	noUsage bool
	held    atomic.Int32
	// reserver holds money back for requests in flight; nil in a fixture made
	// without one.
	reserver *SpendReserver
}

func newCapFixture(t *testing.T, priced bool) *capFixture {
	t.Helper()
	return newCapFixtureWith(t, priced, false)
}

func newCapFixtureWith(t *testing.T, priced, reserve bool) *capFixture {
	t.Helper()
	pool := agentTestPool(t)
	ctx := context.Background()
	q := sqlc.New(pool)
	f := &capFixture{t: t, pool: pool, q: q, tag: strings.ReplaceAll(uuid.NewString(), "-", "")[:12], promptTokens: 100_000}
	f.modelID = "cap-model-" + f.tag

	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		n := f.requests.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		if f.hold != nil {
			if !f.holdSilently {
				_, _ = io.WriteString(w, `data: {"id":"r","model":"served","choices":[{"index":0,"delta":{"role":"assistant","content":"The first words of a long answer, which the user will not wait for."},"finish_reason":null}]}`+"\n\n")
				w.(http.Flusher).Flush()
			}
			f.held.Add(1)
			defer f.held.Add(-1)
			select {
			case <-f.hold:
			case <-r.Context().Done():
				return
			}
		}
		usage := fmt.Sprintf(`"usage":{"prompt_tokens":%d,"completion_tokens":0,"total_tokens":%d}`, f.promptTokens, f.promptTokens)
		if f.noUsage {
			usage = `"note":"no usage here"`
		}
		if f.toolFirst && n == 1 {
			_, _ = fmt.Fprintf(w, `data: {"id":"r","model":"served","choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"web_fetch","arguments":"{\"url\":\"http://127.0.0.1:9/\"}"}}]},"finish_reason":null}]}`+"\n\n")
			_, _ = fmt.Fprintf(w, `data: {"id":"r","model":"served","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}],%s}`+"\n\n", usage)
		} else {
			_, _ = fmt.Fprintf(w, `data: {"id":"r","model":"served","choices":[{"index":0,"delta":{"role":"assistant","content":"done"},"finish_reason":"stop"}],%s}`+"\n\n", usage)
		}
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(endpoint.Close)

	for role, into := range map[sqlc.UserRole]*sqlc.User{sqlc.UserRoleUser: &f.member, sqlc.UserRoleAdmin: &f.admin} {
		if err := pool.QueryRow(ctx, `INSERT INTO users (email, name, role) VALUES ($1, $2, $3) RETURNING id, email, name, role`,
			string(role)+f.tag+"@test.invalid", string(role)+f.tag, role).Scan(&into.ID, &into.Email, &into.Name, &into.Role); err != nil {
			t.Fatal(err)
		}
	}
	channel, err := q.CreateChannel(ctx, sqlc.CreateChannelParams{
		Name: "cap " + f.tag, Type: sqlc.ChannelTypeOpenaiCompatible, Enabled: true,
		BaseUrl: endpoint.URL + "/v1", ApiKey: "sk-test",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := q.CreateSelectedChannelModels(ctx, sqlc.CreateSelectedChannelModelsParams{
		ChannelID: channel.ID, ModelIds: []string{f.modelID}, DisplayNames: []string{""}, Sources: []string{"manual"},
	}); err != nil {
		t.Fatal(err)
	}
	if priced {
		// $3 in, $15 out, per million tokens.
		if _, err := pool.Exec(ctx, `INSERT INTO catalog_models (provider_id, model_id, name, tool_call, raw) VALUES ($1, $2, $2, true, $3::jsonb)`,
			"cap-"+f.tag, f.modelID, `{"cost":{"input":3,"output":15}}`); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		bg := context.Background()
		_, _ = pool.Exec(bg, "DELETE FROM conversations WHERE user_id = ANY($1)", []uuid.UUID{f.member.ID, f.admin.ID})
		_, _ = pool.Exec(bg, "DELETE FROM usage_logs WHERE model_id = $1", f.modelID)
		_, _ = pool.Exec(bg, "DELETE FROM model_overrides WHERE model_id = $1", f.modelID)
		_, _ = pool.Exec(bg, "DELETE FROM catalog_models WHERE provider_id = $1", "cap-"+f.tag)
		_, _ = pool.Exec(bg, "DELETE FROM channels WHERE id = $1", channel.ID)
		_, _ = pool.Exec(bg, "DELETE FROM users WHERE id = ANY($1)", []uuid.UUID{f.member.ID, f.admin.ID})
	})

	opts := agent.Options{SystemPrompt: NewSystemPrompt(pool), PartialInterval: 5 * time.Millisecond, Capacity: NewCapacityCheck(pool)}
	if reserve {
		f.reserver = NewSpendReserver(pool)
		opts.Reserver = f.reserver
	}
	f.runner = agent.NewRunner(pool, agent.NewHub(), NewModelResolver(pool),
		func(context.Context) []agent.Tool { return []agent.Tool{tools.WebFetch{}} }, opts)
	t.Cleanup(func() { _ = f.runner.Shutdown(context.Background()) })
	f.server = &Server{pool: pool, q: q, runner: f.runner, usage: f.runner.Recorder()}
	return f
}

// setCap saves the model's settings, as the settings panel does.
func (f *capFixture) setCap(body string) *httptest.ResponseRecorder {
	f.t.Helper()
	rec := httptest.NewRecorder()
	f.server.UpdateModelSettings(rec, httptest.NewRequest(http.MethodPut, "/", strings.NewReader(body)), f.modelID)
	return rec
}

func (f *capFixture) mustSetCap(body string) {
	f.t.Helper()
	if rec := f.setCap(body); rec.Code != http.StatusOK {
		f.t.Fatalf("save %s: %d %s", body, rec.Code, rec.Body)
	}
}

// spend writes a ledger row of the model; a nil cost is an unpriced request.
func (f *capFixture) spend(cost *string, at time.Time, kind string, user *uuid.UUID) {
	f.t.Helper()
	if _, err := f.pool.Exec(context.Background(), `
		INSERT INTO usage_logs (created_at, kind, status, user_id, model_id, input_tokens, cost)
		VALUES ($1, $2, 'ok', $3, $4, 1000000, $5::numeric)`, at, kind, user, f.modelID, cost); err != nil {
		f.t.Fatal(err)
	}
}

func money(v string) *string { return &v }

// listed is the model as the marketplace lists it, with its cap's spend.
func (f *capFixture) listed() CatalogModel {
	f.t.Helper()
	rec := httptest.NewRecorder()
	search := f.modelID
	f.server.ListCatalogModels(rec, httptest.NewRequest(http.MethodGet, "/", nil), ListCatalogModelsParams{Search: &search})
	var page CatalogModelPage
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil || rec.Code != http.StatusOK {
		f.t.Fatalf("list: %d %s", rec.Code, rec.Body)
	}
	for _, m := range page.Items {
		if m.ModelId == f.modelID {
			return m
		}
	}
	f.t.Fatalf("model not listed: %s", rec.Body)
	return CatalogModel{}
}

// picker is the model as GET /models shows it to a requester (nil is a guest).
func (f *capFixture) picker(as *sqlc.User) ChatModel {
	f.t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	if as != nil {
		req = req.WithContext(auth.WithUser(req.Context(), *as))
		f.server.ListChatModels(rec, req)
	} else {
		f.server.GetGuestConfig(rec, req)
	}
	if rec.Code != http.StatusOK {
		f.t.Fatalf("models: %d %s", rec.Code, rec.Body)
	}
	var models []ChatModel
	if as != nil {
		if err := json.Unmarshal(rec.Body.Bytes(), &models); err != nil {
			f.t.Fatal(err)
		}
	} else {
		var config GuestConfig
		if err := json.Unmarshal(rec.Body.Bytes(), &config); err != nil {
			f.t.Fatal(err)
		}
		models = config.Models
	}
	for _, m := range models {
		if m.Id == f.modelID {
			return m
		}
	}
	f.t.Fatalf("model not in the picker")
	return ChatModel{}
}

// send posts a message as user, in a conversation of its own.
func (f *capFixture) send(user sqlc.User) (rec *httptest.ResponseRecorder, conv sqlc.Conversation) {
	f.t.Helper()
	conv, err := f.q.CreateConversation(context.Background(), sqlc.CreateConversationParams{UserID: &user.ID, Title: "cap"})
	if err != nil {
		f.t.Fatal(err)
	}
	// Web access on, which is what keeps web_fetch among the tools offered.
	body := fmt.Sprintf(`{"content":"hello","model":%q,"webSearch":true}`, f.modelID)
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	rec = httptest.NewRecorder()
	f.server.CreateMessage(rec, req.WithContext(auth.WithUser(req.Context(), user)), conv.ID)
	return rec, conv
}

func errorCodeOf(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		Code string `json:"code"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	return body.Code
}

// finished waits for a run to leave `running` and returns its status and code.
func (f *capFixture) finished(rec *httptest.ResponseRecorder) (status string, code *string) {
	f.t.Helper()
	var created CreateMessageResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil || rec.Code != http.StatusAccepted {
		f.t.Fatalf("send: %d %s", rec.Code, rec.Body)
	}
	deadline := time.Now().Add(15 * time.Second)
	for {
		if err := f.pool.QueryRow(context.Background(), `SELECT status::text, error_code FROM runs WHERE id = $1`, created.Run.Id).Scan(&status, &code); err != nil {
			f.t.Fatal(err)
		}
		if status != "running" {
			return status, code
		}
		if time.Now().After(deadline) {
			f.t.Fatal("the run did not finish")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func (f *capFixture) ledgerRows() (n int) {
	_ = f.pool.QueryRow(context.Background(), `SELECT count(*) FROM usage_logs WHERE model_id = $1`, f.modelID).Scan(&n)
	return
}

// The spend counts the right window of every request of the model, whoever
// made it and whatever it was for, in the server's zone; "recalculate" starts a
// total again; saving the rest of the settings leaves its starting point alone;
// and resetting every setting takes the cap away.
func TestCapacityCountsPeriodsInTheServersZone(t *testing.T) {
	f := newCapFixture(t, true)
	shanghai := mustZone(t, "Asia/Shanghai")
	at := func(month time.Month, day, hour, min, sec int) time.Time {
		return time.Date(2026, month, day, hour, min, sec, 0, shanghai)
	}
	f.server.now = func() time.Time { return at(10, 15, 10, 0, 0) }

	// Different users, kinds and a guest's (no owner): all of it counts. The
	// edges are the second before and the instant a day and a month begin.
	f.spend(money("4"), at(9, 30, 23, 59, 59), "chat", &f.member.ID)  // last month
	f.spend(money("8"), at(10, 1, 0, 0, 0), "chat", &f.admin.ID)      // this month, first instant
	f.spend(money("1"), at(10, 14, 23, 59, 59), "title", nil)         // yesterday, last second
	f.spend(money("2"), at(10, 15, 0, 0, 0), "chat", &f.member.ID)    // today, first instant
	f.spend(money("0.5"), at(10, 15, 9, 0, 0), "title", &f.admin.ID)  // today
	f.spend(nil, at(10, 15, 9, 30, 0), "chat", &f.member.ID)          // unpriced: adds nothing
	f.spend(money("100"), at(10, 15, 11, 0, 0), "chat", &f.member.ID) // after "now": not yet, but counts when it is later

	// Exactly what the ledger has up to now: the row after "now" is the future
	// of the test's clock, and the window has no upper bound, as in production
	// where there is no future row.
	cases := []struct {
		period    string
		wantSpent float64
		wantStart time.Time
	}{
		{"daily", 102.5, at(10, 15, 0, 0, 0)},
		{"monthly", 111.5, at(10, 1, 0, 0, 0)},
		{"total", 115.5, time.Time{}},
	}
	for _, c := range cases {
		t.Run(c.period, func(t *testing.T) {
			f.mustSetCap(fmt.Sprintf(`{"capacity":1000,"capacityPeriod":%q}`, c.period))
			got := f.listed().Capacity
			if got == nil {
				t.Fatal("no capacity on the listed model")
			}
			if got.Spent != c.wantSpent || got.AtCapacity {
				t.Errorf("spent %v (at capacity %v), want %v", got.Spent, got.AtCapacity, c.wantSpent)
			}
			switch {
			case c.wantStart.IsZero() && got.WindowStart != nil:
				t.Errorf("window starts %v, want all history", got.WindowStart)
			case !c.wantStart.IsZero() && (got.WindowStart == nil || !got.WindowStart.Equal(c.wantStart)):
				t.Errorf("window starts %v, want %v", got.WindowStart, c.wantStart)
			}
		})
	}

	// The same clock a day later is a new daily window: yesterday's money is gone.
	f.mustSetCap(`{"capacity":1000,"capacityPeriod":"daily"}`)
	f.server.now = func() time.Time { return at(10, 16, 0, 0, 0) }
	if got := f.listed().Capacity; got.Spent != 0 {
		t.Errorf("the next day starts with %v spent, want 0", got.Spent)
	}
	f.server.now = func() time.Time { return at(10, 15, 10, 0, 0) }

	// A total counts from the starting point, and only the operator moves it.
	f.mustSetCap(`{"capacity":1000,"capacityPeriod":"total"}`)
	restart := at(10, 15, 10, 30, 0)
	f.server.now = func() time.Time { return restart }
	rec := httptest.NewRecorder()
	f.server.ResetModelCapacity(rec, httptest.NewRequest(http.MethodPost, "/", nil), f.modelID)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("reset: %d %s", rec.Code, rec.Body)
	}
	f.server.now = func() time.Time { return at(10, 15, 12, 0, 0) }
	if got := f.listed().Capacity; got.Spent != 100 || got.WindowStart == nil || !got.WindowStart.Equal(restart) {
		t.Fatalf("after the reset: %+v (start %v), want only the later row, 100 from %v", got, got.WindowStart, restart)
	}
	f.spend(money("7"), at(10, 15, 11, 30, 0), "chat", &f.member.ID)
	if got := f.listed().Capacity; got.Spent != 107 {
		t.Errorf("a request after the reset: spent %v, want 107", got.Spent)
	}

	// Saving the settings again keeps the starting point...
	f.mustSetCap(`{"capacity":500,"capacityPeriod":"total","capacityAdminExempt":false}`)
	if got := f.listed().Capacity; got.WindowStart == nil || !got.WindowStart.Equal(restart) {
		t.Errorf("saving moved the starting point to %v", got.WindowStart)
	}
	// ...and a cap saved as empty is no cap: the period and the exemption go
	// back to their defaults rather than lingering.
	f.mustSetCap(`{"capacity":null,"capacityPeriod":"daily","capacityAdminExempt":false,"adminOnly":true}`)
	listed := f.listed()
	if listed.Capacity != nil {
		t.Errorf("no cap, yet capacity %+v", listed.Capacity)
	}
	row, err := f.q.GetModelOverride(context.Background(), f.modelID)
	if err != nil || row.CapacityPeriod != "total" || !row.CapacityAdminExempt || row.Capacity.Valid {
		t.Errorf("the row of an uncapped model: %+v, %v", row, err)
	}

	// "Reset all settings" takes the cap, its period, its exemption and its
	// starting point with it. The spend stays in the ledger.
	f.mustSetCap(`{"capacity":500,"capacityPeriod":"monthly"}`)
	rec = httptest.NewRecorder()
	f.server.ResetModelSettings(rec, httptest.NewRequest(http.MethodDelete, "/", nil), f.modelID)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("reset all: %d", rec.Code)
	}
	if _, err := f.q.GetModelOverride(context.Background(), f.modelID); err == nil {
		t.Error("the override row survived resetting every setting")
	}
	if got := f.listed().Capacity; got != nil {
		t.Errorf("a cap survived resetting every setting: %+v", got)
	}
	if n := f.ledgerRows(); n != 8 {
		t.Errorf("the ledger has %d rows, want the 8 it had", n)
	}
}

// What is saved is what is read back, exactly; a bad cap is refused whole.
func TestCapacitySettingsRoundTrip(t *testing.T) {
	f := newCapFixture(t, true)
	rec := f.setCap(`{"capacity":12.345678,"capacityPeriod":"monthly","capacityAdminExempt":false}`)
	var saved ModelOverride
	if err := json.Unmarshal(rec.Body.Bytes(), &saved); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("save: %d %s", rec.Code, rec.Body)
	}
	if saved.Capacity == nil || *saved.Capacity != 12.345678 || saved.CapacityPeriod == nil || *saved.CapacityPeriod != Monthly ||
		saved.CapacityAdminExempt == nil || *saved.CapacityAdminExempt {
		t.Errorf("saved %+v", saved)
	}
	var stored string
	if err := f.pool.QueryRow(context.Background(), `SELECT capacity::text FROM model_overrides WHERE model_id = $1`, f.modelID).Scan(&stored); err != nil || stored != "12.345678" {
		t.Errorf("stored %q, %v", stored, err)
	}

	for _, body := range []string{`{"capacity":0}`, `{"capacity":-3}`, `{"capacity":1.2345678}`, `{"capacity":5,"capacityPeriod":"yearly"}`} {
		rec := f.setCap(body)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: %d, want 400", body, rec.Code)
		}
	}
	// A refused save changed nothing.
	if err := f.pool.QueryRow(context.Background(), `SELECT capacity::text FROM model_overrides WHERE model_id = $1`, f.modelID).Scan(&stored); err != nil || stored != "12.345678" {
		t.Errorf("after refused saves: %q, %v", stored, err)
	}
}

// The sum and the comparison are numeric: 0.7 + 0.1 reaches 0.8, which the
// same sum in floats (0.7999999999999999) would miss.
func TestCapacityIsComparedExactly(t *testing.T) {
	f := newCapFixture(t, true)
	f.mustSetCap(`{"capacity":0.8,"capacityPeriod":"total"}`)
	f.spend(money("0.7"), time.Now().Add(-time.Minute), "chat", &f.member.ID)
	if f.listed().Capacity.AtCapacity {
		t.Fatal("0.7 of 0.8 is at capacity")
	}
	f.spend(money("0.1"), time.Now().Add(-time.Minute), "chat", &f.member.ID)
	if got := f.listed().Capacity; !got.AtCapacity {
		t.Fatalf("0.7 + 0.1 of 0.8 is not at capacity: %+v", got)
	}
	if !f.picker(&f.member).AtCapacity {
		t.Error("the picker does not show it at capacity")
	}
}

// A model with no price logs no cost, so its spend is zero and no cap can
// trigger however much it is used.
func TestCapacityNeverTriggersWithoutAPrice(t *testing.T) {
	f := newCapFixture(t, false)
	f.mustSetCap(`{"capacity":0.000001,"capacityPeriod":"total"}`)
	for range 3 {
		f.spend(nil, time.Now().Add(-time.Minute), "chat", &f.member.ID)
	}
	got := f.listed().Capacity
	if got == nil || got.Spent != 0 || got.AtCapacity {
		t.Fatalf("unpriced usage counts: %+v", got)
	}
	if f.picker(&f.member).AtCapacity {
		t.Error("an unpriced model is at capacity")
	}
	if _, _, err := f.server.pickChannel(context.Background(), f.modelID, asker{}); err != nil {
		t.Errorf("an unpriced model is refused: %v", err)
	}

	// And through a real run: the endpoint reports usage, there is no price,
	// the ledger row has no cost, and a one-millionth-of-a-dollar cap does not
	// stop the second request.
	f.toolFirst = true
	status, code := f.finished(mustSend(f))
	if status != "completed" || code != nil || f.requests.Load() != 2 {
		t.Errorf("run %s (%v), %d requests; want completed with 2", status, code, f.requests.Load())
	}
}

func mustSend(f *capFixture) *httptest.ResponseRecorder {
	rec, _ := f.send(f.member)
	return rec
}

// A run that cannot start is refused before anything is written: no message,
// no run, and the conversation does not even take the model.
func TestCapacityRefusesARunBeforeWritingAnything(t *testing.T) {
	f := newCapFixture(t, true)
	f.mustSetCap(`{"capacity":0.25,"capacityPeriod":"total"}`)
	f.spend(money("0.3"), time.Now().Add(-time.Minute), "chat", &f.member.ID)

	rec, conv := f.send(f.member)
	if rec.Code != http.StatusConflict || errorCodeOf(t, rec) != "model_capacity_reached" {
		t.Fatalf("a member at the cap: %d %s", rec.Code, rec.Body)
	}
	var messages, runs int
	_ = f.pool.QueryRow(context.Background(), `SELECT count(*) FROM messages WHERE conversation_id = $1`, conv.ID).Scan(&messages)
	_ = f.pool.QueryRow(context.Background(), `SELECT count(*) FROM runs WHERE conversation_id = $1`, conv.ID).Scan(&runs)
	after, err := f.q.GetConversation(context.Background(), sqlc.GetConversationParams{ID: conv.ID, UserID: &f.member.ID})
	if err != nil {
		t.Fatal(err)
	}
	if messages != 0 || runs != 0 || after.Model != nil || f.requests.Load() != 0 {
		t.Errorf("a refused send left %d messages, %d runs, model %v, %d requests", messages, runs, after.Model, f.requests.Load())
	}

	// Regenerating and answering a question start runs through the same choice.
	if _, _, err := f.server.pickChannel(context.Background(), f.modelID, asker{}); !errors.Is(err, errModelCapacity) {
		t.Errorf("pickChannel: %v, want the capacity error", err)
	}
	// Asked to choose ("whatever is available") with only a capped model on
	// offer there is nothing to choose; with another on offer it is skipped.
	if _, _, err := f.server.pickChannel(context.Background(), "", asker{}); err != nil && !errors.Is(err, errModelCapacity) {
		t.Errorf("pickChannel with no model: %v", err)
	} else if err == nil {
		if id, _, _ := f.server.pickChannel(context.Background(), "", asker{}); id == f.modelID {
			t.Error("a conversation that never chose was given the capped model")
		}
	}
}

// Administrators are let through while the cap exempts them, and their spend
// counts all the same; with the exemption off the cap holds for them too.
func TestCapacityAdminExemption(t *testing.T) {
	f := newCapFixture(t, true)
	f.mustSetCap(`{"capacity":0.25,"capacityPeriod":"total","capacityAdminExempt":true}`)
	// The administrator's own spend is what reaches the cap.
	f.spend(money("0.3"), time.Now().Add(-time.Minute), "chat", &f.admin.ID)

	if rec, _ := f.send(f.member); rec.Code != http.StatusConflict {
		t.Errorf("a member: %d, want 409", rec.Code)
	}
	rec, _ := f.send(f.admin)
	if status, code := f.finished(rec); status != "completed" || code != nil {
		t.Errorf("an exempt administrator: run %s (%v)", status, code)
	}
	if got := f.listed().Capacity; !got.AtCapacity {
		t.Errorf("the operator's view of the cap says there is room: %+v", got)
	}
	// Their request was logged and counts too.
	if f.ledgerRows() != 2 {
		t.Errorf("%d ledger rows, want the seeded one and the administrator's request", f.ledgerRows())
	}

	f.mustSetCap(`{"capacity":0.25,"capacityPeriod":"total","capacityAdminExempt":false}`)
	if rec, _ := f.send(f.admin); rec.Code != http.StatusConflict || errorCodeOf(t, rec) != "model_capacity_reached" {
		t.Errorf("an administrator without the exemption: %d %s", rec.Code, rec.Body)
	}
}

// GET /models lists a capped model for everyone and marks it for those the
// cap applies to.
func TestChatModelsReportCapacityPerRequester(t *testing.T) {
	f := newCapFixture(t, true)
	f.mustSetCap(`{"capacity":0.25,"capacityPeriod":"total","capacityAdminExempt":true}`)

	// Under the cap: nobody is marked.
	for name, as := range map[string]*sqlc.User{"member": &f.member, "admin": &f.admin, "guest": nil} {
		if f.picker(as).AtCapacity {
			t.Errorf("%s: at capacity with nothing spent", name)
		}
	}

	f.spend(money("0.3"), time.Now().Add(-time.Minute), "chat", &f.member.ID)
	for name, c := range map[string]struct {
		as   *sqlc.User
		want bool
	}{
		"member":              {&f.member, true},
		"admin, exempt":       {&f.admin, false},
		"guest, never exempt": {nil, true},
	} {
		if got := f.picker(c.as).AtCapacity; got != c.want {
			t.Errorf("%s: atCapacity = %v, want %v", name, got, c.want)
		}
	}

	f.mustSetCap(`{"capacity":0.25,"capacityPeriod":"total","capacityAdminExempt":false}`)
	if !f.picker(&f.admin).AtCapacity {
		t.Error("an administrator without the exemption is not marked")
	}
	// Raising the cap lifts it for everyone at once.
	f.mustSetCap(`{"capacity":5,"capacityPeriod":"total"}`)
	if f.picker(&f.member).AtCapacity {
		t.Error("still at capacity after raising the cap")
	}
}

// A run stops before the request that would go past the cap: the one that
// reached it was allowed, the next never leaves, and the transcript says why.
func TestCapacityStopsARunBetweenRequests(t *testing.T) {
	cases := []struct {
		name      string
		who       func(*capFixture) sqlc.User
		exempt    bool
		wantStop  bool
		wantReqs  int32
		wantLedge int
	}{
		{"member", func(f *capFixture) sqlc.User { return f.member }, true, true, 1, 1},
		{"admin exempt", func(f *capFixture) sqlc.User { return f.admin }, true, false, 2, 2},
		{"admin not exempt", func(f *capFixture) sqlc.User { return f.admin }, false, true, 1, 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newCapFixture(t, true)
			f.toolFirst = true
			// One request costs $0.30: the first is under the cap, the second
			// would not be.
			f.mustSetCap(fmt.Sprintf(`{"capacity":0.25,"capacityPeriod":"total","capacityAdminExempt":%v}`, c.exempt))
			rec, conv := f.send(c.who(f))
			status, code := f.finished(rec)

			if c.wantStop {
				if status != "failed" || code == nil || *code != "model_capacity_reached" {
					t.Fatalf("run %s (%v), want failed with model_capacity_reached", status, code)
				}
				// The transcript ends with an assistant row carrying the code.
				var role, rowStatus string
				var rowCode *string
				if err := f.pool.QueryRow(context.Background(), `
					SELECT role::text, status::text, error_code FROM messages
					WHERE conversation_id = $1 ORDER BY created_at DESC, id DESC LIMIT 1`, conv.ID).Scan(&role, &rowStatus, &rowCode); err != nil {
					t.Fatal(err)
				}
				if role != "assistant" || rowStatus != "failed" || rowCode == nil || *rowCode != "model_capacity_reached" {
					t.Errorf("last row: %s %s %v", role, rowStatus, rowCode)
				}
			} else if status != "completed" || code != nil {
				t.Fatalf("run %s (%v), want completed", status, code)
			}
			if got := f.requests.Load(); got != c.wantReqs {
				t.Errorf("the model was asked %d times, want %d", got, c.wantReqs)
			}
			if f.ledgerRows() != c.wantLedge {
				t.Errorf("%d ledger rows, want %d", f.ledgerRows(), c.wantLedge)
			}
		})
	}
}

// A guest is never exempt: the request is refused before anything is staged.
func TestCapacityRefusesAGuest(t *testing.T) {
	h := newGuestHarness(t)
	ctx := context.Background()
	// The harness's model gets a price and a cap that its own ledger row meets.
	if _, err := h.pool.Exec(ctx, `INSERT INTO catalog_models (provider_id, model_id, name, raw) VALUES ($1, $2, $2, $3::jsonb)`,
		"cap-"+h.model, h.model, `{"cost":{"input":3,"output":15}}`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = h.pool.Exec(context.Background(), "DELETE FROM catalog_models WHERE provider_id = $1", "cap-"+h.model)
		_, _ = h.pool.Exec(context.Background(), "DELETE FROM usage_logs WHERE model_id = $1", h.model)
	})
	if _, err := h.pool.Exec(ctx, `INSERT INTO usage_logs (kind, status, model_id, cost) VALUES ('chat', 'ok', $1, 0.5)`, h.model); err != nil {
		t.Fatal(err)
	}
	// Not exempt-able, but set to exempt anyway: that is for administrators.
	if _, err := h.q.SaveModelOverride(ctx, sqlc.SaveModelOverrideParams{
		ModelID: h.model, InputModalities: []string{"image"},
		Capacity: pgtype.Numeric{Int: big.NewInt(25), Exp: -2, Valid: true},
	}); err != nil {
		t.Fatal(err)
	}

	frames := h.run(map[string]any{
		"action": "send", "conversationId": uuid.New(), "model": h.model, "messages": []any{},
		"input": userRow(uuid.New(), "hello", time.Now()),
	})
	if frames.status != http.StatusConflict || frames.error["code"] != "model_capacity_reached" {
		t.Fatalf("a guest at the cap: %d %v", frames.status, frames.error)
	}
	if got := len(h.requests()); got != 0 {
		t.Errorf("the model was asked %d times", got)
	}
	var guests int
	_ = h.pool.QueryRow(ctx, `SELECT count(*) FROM users WHERE guest`).Scan(&guests)
	if guests != 0 {
		t.Errorf("%d guest users staged for a refused request", guests)
	}
}

// A title is not made with a model at its cap: the task model falls back to
// the conversation's, and when that is at its cap too the title is skipped.
func TestCapacityTaskModelFallsBack(t *testing.T) {
	f := newCapFixture(t, true)
	ctx := context.Background()

	// A second model on a second channel is the conversation's own.
	own := "cap-own-" + f.tag
	channel, err := f.q.CreateChannel(ctx, sqlc.CreateChannelParams{
		Name: "own " + f.tag, Type: sqlc.ChannelTypeOpenaiCompatible, Enabled: true, BaseUrl: "http://127.0.0.1:1/v1", ApiKey: "sk-test",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.q.CreateSelectedChannelModels(ctx, sqlc.CreateSelectedChannelModelsParams{
		ChannelID: channel.ID, ModelIds: []string{own}, DisplayNames: []string{""}, Sources: []string{"manual"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(ctx, `INSERT INTO catalog_models (provider_id, model_id, name, tool_call, raw) VALUES ($1, $2, $2, true, $3::jsonb)`,
		"cap-"+f.tag+"-own", own, `{"cost":{"input":3,"output":15}}`); err != nil {
		t.Fatal(err)
	}
	prior, priorErr := f.q.GetSetting(ctx, settingTaskModel)
	t.Cleanup(func() {
		bg := context.Background()
		_, _ = f.pool.Exec(bg, "DELETE FROM model_overrides WHERE model_id = $1", own)
		_, _ = f.pool.Exec(bg, "DELETE FROM usage_logs WHERE model_id = $1", own)
		_, _ = f.pool.Exec(bg, "DELETE FROM catalog_models WHERE provider_id = $1", "cap-"+f.tag+"-own")
		_, _ = f.pool.Exec(bg, "DELETE FROM channels WHERE id = $1", channel.ID)
		if priorErr != nil {
			_, _ = f.pool.Exec(bg, "DELETE FROM app_settings WHERE key = $1", settingTaskModel)
		} else {
			_ = f.q.SetSetting(bg, sqlc.SetSettingParams{Key: settingTaskModel, Value: prior})
		}
	})
	raw, _ := json.Marshal(f.modelID)
	if err := f.q.SetSetting(ctx, sqlc.SetSettingParams{Key: settingTaskModel, Value: raw}); err != nil {
		t.Fatal(err)
	}
	capOwn := func(body string) {
		t.Helper()
		rec := httptest.NewRecorder()
		f.server.UpdateModelSettings(rec, httptest.NewRequest(http.MethodPut, "/", strings.NewReader(body)), own)
		if rec.Code != http.StatusOK {
			t.Fatalf("save %s: %d %s", body, rec.Code, rec.Body)
		}
	}

	// Nothing capped: the task model is the configured one.
	if got, _, err := f.server.taskModel(ctx, own, nil, false); err != nil || got != f.modelID {
		t.Fatalf("uncapped: %q %v", got, err)
	}
	// The task model reaches its cap: the conversation's own model is used.
	f.mustSetCap(`{"capacity":0.25,"capacityPeriod":"total"}`)
	f.spend(money("0.3"), time.Now().Add(-time.Minute), "chat", &f.member.ID)
	if got, _, err := f.server.taskModel(ctx, own, nil, false); err != nil || got != own {
		t.Fatalf("task model capped: %q %v, want the conversation's %q", got, err, own)
	}
	// On its pinned channel, the same.
	if got, ch, err := f.server.taskModel(ctx, own, &channel.ID, false); err != nil || got != own || ch != channel.ID {
		t.Fatalf("task model capped, pinned: %q %v %v", got, ch, err)
	}
	// An administrator's conversation, with the cap exempting administrators,
	// still gets the task model.
	if got, _, err := f.server.taskModel(ctx, own, nil, true); err != nil || got != f.modelID {
		t.Fatalf("an exempt administrator's title: %q %v", got, err)
	}

	// The conversation's own model reaches its cap too: no title.
	capOwn(`{"capacity":0.25,"capacityPeriod":"total"}`)
	if _, err := f.pool.Exec(ctx, `INSERT INTO usage_logs (kind, status, model_id, cost) VALUES ('chat', 'ok', $1, 0.3)`, own); err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.server.taskModel(ctx, own, nil, false); !errors.Is(err, errModelCapacity) {
		t.Errorf("both capped: %v, want the capacity error", err)
	}
	if _, _, err := f.server.taskModel(ctx, own, &channel.ID, false); !errors.Is(err, errModelCapacity) {
		t.Errorf("both capped, pinned: %v, want the capacity error", err)
	}
	before := f.ledgerRows()
	if _, err := f.server.generateTitle(ctx, f.member.ID, "", uuid.Nil, own, nil, "<user>hi</user>"); !errors.Is(err, errModelCapacity) {
		t.Errorf("generateTitle: %v, want the capacity error", err)
	}
	if f.ledgerRows() != before || f.requests.Load() != 0 {
		t.Error("a title that was skipped still made a request")
	}

	// Saving a capped model as the task model is allowed: it is merely used
	// when it has room.
	rec := httptest.NewRecorder()
	f.server.UpdateSystemSettings(rec, httptest.NewRequest(http.MethodPatch, "/", strings.NewReader(fmt.Sprintf(`{"taskModel":%q}`, f.modelID))).
		WithContext(auth.WithUser(ctx, f.admin)))
	if rec.Code != http.StatusOK {
		t.Errorf("saving a capped task model: %d %s", rec.Code, rec.Body)
	}
}
