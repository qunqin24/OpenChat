package agent

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/qunqin/openchat/apps/server/internal/db/sqlc"
	"github.com/qunqin/openchat/apps/server/internal/llm"
	"github.com/qunqin/openchat/apps/server/internal/providers/fake"
)

type ledgerRow struct {
	kind, status, model, served string
	code, message               *string
	input, output, reasoning    int
	cacheRead                   int
	cost                        *float64
	firstToken                  *int
	duration                    int
	user                        *uuid.UUID
	conversation                *uuid.UUID
}

// ledger reads the rows a run wrote, oldest first.
func (f *fixture) ledger(runID uuid.UUID) []ledgerRow {
	f.t.Helper()
	rows, err := f.pool.Query(context.Background(), `
		SELECT kind, status, model_id, served_model, error_code, error_message,
		       input_tokens, output_tokens, reasoning_tokens, cache_read_tokens,
		       cost::double precision, first_token_ms, duration_ms, user_id, conversation_id
		FROM usage_logs WHERE run_id = $1 ORDER BY created_at, id`, runID)
	if err != nil {
		f.t.Fatal(err)
	}
	defer rows.Close()
	var out []ledgerRow
	for rows.Next() {
		var r ledgerRow
		if err := rows.Scan(&r.kind, &r.status, &r.model, &r.served, &r.code, &r.message,
			&r.input, &r.output, &r.reasoning, &r.cacheRead, &r.cost, &r.firstToken, &r.duration, &r.user, &r.conversation); err != nil {
			f.t.Fatal(err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		f.t.Fatal(err)
	}
	return out
}

// A request that answers is one `ok` row carrying its tokens, the model the
// provider served, who asked, and how long the first token took.
func TestRunRecordsItsRequest(t *testing.T) {
	f := newFixture(t, nil, script(t, fake.Script{Model: "served-m", FinishReason: "stop",
		Usage: &fake.Usage{InputTokens: 120, OutputTokens: 40, ReasoningTokens: 10, CacheReadTokens: 300},
		Steps: []fake.Step{{Kind: fake.StepText, Chunks: []string{"Hel", "lo"}}}}))
	run := f.admit("hi")
	f.runner.Start(run.ID)
	f.waitFor(run.ID, sqlc.RunStatusCompleted)

	rows := f.ledger(run.ID)
	if len(rows) != 1 {
		t.Fatalf("%d rows, want 1: %+v", len(rows), rows)
	}
	r := rows[0]
	if r.kind != "chat" || r.status != "ok" || r.model != "m" || r.served != "served-m" {
		t.Errorf("row = %+v", r)
	}
	if r.input != 120 || r.output != 40 || r.reasoning != 10 || r.cacheRead != 300 {
		t.Errorf("tokens = %d/%d/%d/%d", r.input, r.output, r.reasoning, r.cacheRead)
	}
	if r.user == nil || *r.user != f.user || r.conversation == nil || *r.conversation != f.conv {
		t.Errorf("owner = %v / %v", r.user, r.conversation)
	}
	if r.firstToken == nil || *r.firstToken > r.duration {
		t.Errorf("first token %v, duration %d", r.firstToken, r.duration)
	}
	// The model is in no catalogue, so there is no price and no cost: an
	// unpriced request is not a free one.
	if r.cost != nil {
		t.Errorf("cost %v for an unpriced model", *r.cost)
	}
}

// Every attempt is a row: the failed one that was retried, then the one that
// answered. A tool round makes two requests, so two rows.
func TestRunRecordsEveryAttempt(t *testing.T) {
	f := newFixture(t, []Tool{&echoTool{name: "echo", replay: true}},
		func() (llm.ModelClient, error) {
			return nil, &llm.Error{Kind: llm.ErrorTransport, Message: "connection reset"}
		},
		toolCall(t, "c1", "echo", `{}`),
		answer(t, "done"))
	run := f.admit("hi")
	f.runner.Start(run.ID)
	f.waitFor(run.ID, sqlc.RunStatusCompleted)

	rows := f.ledger(run.ID)
	if len(rows) != 3 {
		t.Fatalf("%d rows, want 3: %+v", len(rows), rows)
	}
	if rows[0].status != "error" || rows[0].code == nil || *rows[0].code != CodeModelError {
		t.Errorf("first attempt = %+v", rows[0])
	}
	if rows[0].message == nil || *rows[0].message == "" {
		t.Error("error row has no message")
	}
	if rows[1].status != "ok" || rows[2].status != "ok" {
		t.Errorf("later attempts = %s, %s", rows[1].status, rows[2].status)
	}
}

func TestRunRecordsAuthFailure(t *testing.T) {
	f := newFixture(t, nil, func() (llm.ModelClient, error) {
		return nil, &llm.Error{Kind: llm.ErrorAuth, Code: "invalid_api_key", Message: "bad key sk-abcdef1234567890"}
	})
	run := f.admit("hi")
	f.runner.Start(run.ID)
	f.waitFor(run.ID, sqlc.RunStatusFailed)

	rows := f.ledger(run.ID)
	if len(rows) != 1 {
		t.Fatalf("%d rows, want 1", len(rows))
	}
	r := rows[0]
	if r.status != "error" || r.code == nil || *r.code != CodeModelAuth {
		t.Fatalf("row = %+v", r)
	}
	if r.message == nil || *r.message == "" || strings.Contains(*r.message, "sk-abcdef") {
		t.Errorf("message %v", r.message)
	}
}

// Stopping a reply is an ordinary ending, not a failure.
func TestRunRecordsCancelAsOK(t *testing.T) {
	f := newFixture(t, nil, hang(t))
	run := f.admit("hi")
	f.runner.Start(run.ID)
	for f.models.calls.Load() == 0 {
		time.Sleep(5 * time.Millisecond)
	}
	if _, err := f.q.RequestRunCancel(context.Background(), sqlc.RequestRunCancelParams{ID: run.ID, ConversationID: f.conv}); err != nil {
		t.Fatal(err)
	}
	f.runner.Cancel(run.ID)
	f.waitFor(run.ID, sqlc.RunStatusCancelled)

	rows := f.ledger(run.ID)
	if len(rows) != 1 || rows[0].status != "ok" || rows[0].firstToken != nil {
		t.Fatalf("rows = %+v", rows)
	}
}
