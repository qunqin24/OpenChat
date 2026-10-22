package api

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/qunqin/openchat/apps/server/internal/agent"
	"github.com/qunqin/openchat/apps/server/internal/auth"
	"github.com/qunqin/openchat/apps/server/internal/db/sqlc"
	"github.com/qunqin/openchat/apps/server/internal/llm"
)

// A request that was stopped before the provider's usage frame still cost
// money. Its ledger row is an estimate, priced from the catalogue like any
// other, flagged, and counted by the cap - whichever path made the request.

type spendRow struct {
	kind, status string
	input        int
	output       int
	cost         *float64
	estimated    bool
	user         *uuid.UUID
}

// usageRows are the model's ledger rows of a kind, oldest first.
func (f *capFixture) usageRows(kind string) []spendRow {
	f.t.Helper()
	rows, err := f.pool.Query(context.Background(), `
		SELECT kind, status, input_tokens, output_tokens, cost::double precision, estimated, user_id
		FROM usage_logs WHERE model_id = $1 AND kind = $2 ORDER BY created_at, id`, f.modelID, kind)
	if err != nil {
		f.t.Fatal(err)
	}
	defer rows.Close()
	var out []spendRow
	for rows.Next() {
		var r spendRow
		if err := rows.Scan(&r.kind, &r.status, &r.input, &r.output, &r.cost, &r.estimated, &r.user); err != nil {
			f.t.Fatal(err)
		}
		out = append(out, r)
	}
	return out
}

// waitHeld waits until n requests are being held by the endpoint.
func (f *capFixture) waitHeld(n int32) {
	f.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for f.held.Load() < n {
		if time.Now().After(deadline) {
			f.t.Fatalf("%d requests held, want %d", f.held.Load(), n)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// stop presses Stop on a run, as its owner.
func (f *capFixture) stop(user sqlc.User, conv uuid.UUID, sent *httptest.ResponseRecorder) {
	f.t.Helper()
	var created CreateMessageResponse
	if err := json.Unmarshal(sent.Body.Bytes(), &created); err != nil {
		f.t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	w := httptest.NewRecorder()
	f.server.CancelRun(w, req.WithContext(auth.WithUser(req.Context(), user)), conv, created.Run.Id)
	if w.Code != http.StatusAccepted {
		f.t.Fatalf("stop: %d %s", w.Code, w.Body)
	}
}

func near(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

// The model streamed some words and was stopped: the row has the request's
// estimated input and the streamed output, priced at the catalogue's $3 / $15
// per million, and the cap counts it.
func TestStoppedRunIsPricedAndCountsTowardTheCap(t *testing.T) {
	f := newCapFixture(t, true)
	f.hold = make(chan struct{})
	defer close(f.hold)
	f.mustSetCap(`{"capacity":1,"capacityPeriod":"total"}`)

	rec, conv := f.send(f.member)
	f.waitHeld(1)
	time.Sleep(200 * time.Millisecond) // the first chunk reaches the run
	f.stop(f.member, conv.ID, rec)
	if status, _ := f.finished(rec); status != "cancelled" {
		t.Fatalf("run %s, want cancelled", status)
	}

	rows := f.usageRows("chat")
	if len(rows) != 1 {
		t.Fatalf("%d chat rows, want 1: %+v", len(rows), rows)
	}
	r := rows[0]
	if r.status != "ok" || !r.estimated || r.input <= 0 || r.output <= 0 {
		t.Fatalf("row = %+v, want an ok, estimated row with both sides", r)
	}
	if r.cost == nil || !near(*r.cost, (float64(r.input)*3+float64(r.output)*15)/1e6) {
		t.Fatalf("cost %v for %d in / %d out at $3/$15", r.cost, r.input, r.output)
	}

	// It counts: a cap below what it cost is reached.
	f.mustSetCap(fmt.Sprintf(`{"capacity":%.6f,"capacityPeriod":"total"}`, *r.cost/2))
	if _, _, err := f.server.pickChannel(context.Background(), f.modelID, asker{}); err != errModelCapacity {
		t.Errorf("pickChannel after the stopped request: %v, want the capacity error", err)
	}
	if !f.listed().Capacity.AtCapacity {
		t.Error("the operator's view does not count the stopped request")
	}
}

// Stopped before the endpoint sent a byte - the large-context attack - the
// input is still owed.
func TestRunStoppedBeforeTheFirstByteIsBilledItsInput(t *testing.T) {
	f := newCapFixture(t, true)
	f.hold = make(chan struct{})
	f.holdSilently = true
	defer close(f.hold)

	rec, conv := f.send(f.member)
	f.waitHeld(1)
	f.stop(f.member, conv.ID, rec)
	f.finished(rec)

	rows := f.usageRows("chat")
	if len(rows) != 1 || !rows[0].estimated || rows[0].input <= 0 || rows[0].output != 0 || rows[0].cost == nil || *rows[0].cost <= 0 {
		t.Fatalf("rows = %+v", rows)
	}
}

// An answer from a provider that sends no usage is not free either: it is
// estimated and priced, and counts.
func TestUnreportedUsageIsEstimatedAndPriced(t *testing.T) {
	f := newCapFixture(t, true)
	f.noUsage = true
	if status, _ := f.finished(mustSend(f)); status != "completed" {
		t.Fatal(status)
	}
	rows := f.usageRows("chat")
	if len(rows) != 1 || !rows[0].estimated || rows[0].cost == nil || *rows[0].cost <= 0 || rows[0].output <= 0 {
		t.Fatalf("rows = %+v", rows)
	}
}

// A title that is stopped is metered like a run's request.
func TestStoppedTitleIsEstimated(t *testing.T) {
	f := newCapFixture(t, true)
	f.hold = make(chan struct{})
	defer close(f.hold)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := f.server.generateTitle(ctx, f.member.ID, "", uuid.Nil, f.modelID, nil, "<user>\nwhat is a long question\n</user>")
		done <- err
	}()
	f.waitHeld(1)
	time.Sleep(100 * time.Millisecond)
	cancel()
	if err := <-done; err == nil {
		t.Fatal("a stopped title succeeded")
	}
	rows := f.usageRows("title")
	if len(rows) != 1 || !rows[0].estimated || rows[0].input <= 0 || rows[0].cost == nil {
		t.Fatalf("rows = %+v", rows)
	}
	if rows[0].user == nil || *rows[0].user != f.member.ID {
		t.Errorf("owner = %v", rows[0].user)
	}
}

// A compression that is stopped is metered too.
func TestStoppedCompressionIsEstimated(t *testing.T) {
	f := newCapFixture(t, true)
	f.hold = make(chan struct{})
	defer close(f.hold)

	serving, err := f.q.ServingChannels(context.Background(), f.modelID)
	if err != nil || len(serving) == 0 {
		t.Fatal("no channel serves the model", err)
	}
	channel := serving[0].ID
	client, err := NewModelResolver(f.pool).Client(context.Background(), &channel, f.modelID)
	if err != nil {
		t.Fatal(err)
	}
	// A conversation with something to summarise: a question and its answer,
	// then the question the compression keeps.
	conv, err := f.q.CreateConversation(context.Background(), sqlc.CreateConversationParams{UserID: &f.member.ID, Title: "t"})
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range []struct {
		role sqlc.MessageRole
		text string
	}{
		{sqlc.MessageRoleUser, strings.Repeat("something to remember ", 50)},
		{sqlc.MessageRoleAssistant, strings.Repeat("something that was said ", 50)},
		{sqlc.MessageRoleUser, "and now?"},
	} {
		if _, err := f.q.AppendEntry(context.Background(), sqlc.AppendEntryParams{
			ConversationID: conv.ID, Role: row.role, Content: row.text,
			Parts:  agent.EncodeParts([]llm.Part{llm.TextPart{Text: row.text}}),
			Status: sqlc.MessageStatusComplete,
		}); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := f.runner.Compactor().Compact(ctx, agent.CompactParams{
			ConversationID: conv.ID, UserID: f.member.ID, ChannelID: &channel, ModelID: f.modelID, Client: client,
		})
		done <- err
	}()
	f.waitHeld(1)
	time.Sleep(100 * time.Millisecond)
	cancel()
	if err := <-done; err == nil {
		t.Fatal("a stopped compression succeeded")
	}
	rows := f.usageRows("compression")
	if len(rows) != 1 || !rows[0].estimated || rows[0].input <= 0 || rows[0].cost == nil {
		t.Fatalf("rows = %+v", rows)
	}
}

// A guest's stopped run is metered with no owner, like its answered ones.
func TestGuestStoppedRunIsEstimated(t *testing.T) {
	h := newGuestHarness(t)
	h.pace = 30 * time.Second
	if _, err := h.pool.Exec(context.Background(), `DELETE FROM usage_logs WHERE model_id = $1`, h.model); err != nil {
		t.Fatal(err)
	}
	conv := uuid.New()
	raw, _ := json.Marshal(map[string]any{
		"action": "send", "conversationId": conv, "model": h.model,
		"messages": []any{}, "input": userRow(uuid.New(), strings.Repeat("a long question ", 100), time.Now()),
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, h.http.URL+"/api/v1/guest/runs", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() { // the first batch says the run has started
		if strings.HasPrefix(scanner.Text(), "data: ") {
			break
		}
	}
	// The model is being waited on; the guest closes the page.
	deadline := time.Now().Add(5 * time.Second)
	for len(h.requests()) == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	h.noGuestRows(conv)

	var n, input int
	var estimated bool
	var user *uuid.UUID
	deadline = time.Now().Add(5 * time.Second)
	for {
		err := h.pool.QueryRow(context.Background(),
			`SELECT count(*), coalesce(max(input_tokens), 0), coalesce(bool_or(estimated), false), (array_agg(user_id))[1]
			   FROM usage_logs WHERE model_id = $1`, h.model).Scan(&n, &input, &estimated, &user)
		if err != nil {
			t.Fatal(err)
		}
		if n > 0 || time.Now().After(deadline) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if n != 1 || !estimated || input < 300 || user != nil {
		t.Fatalf("%d rows, estimated %v, input %d, owner %v: want one ownerless estimated row", n, estimated, input, user)
	}
}
