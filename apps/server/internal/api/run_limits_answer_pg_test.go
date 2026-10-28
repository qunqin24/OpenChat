package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/qunqin24/openchat/apps/server/internal/agent"
	"github.com/qunqin24/openchat/apps/server/internal/db/sqlc"
	"github.com/qunqin24/openchat/apps/server/internal/llm"
)

// Answering a question starts a run, so it takes a slot like a send does. The
// slot here is held by a run row parked in another conversation, which no
// runner executes.
func TestRunLimitAlsoRefusesAnAnswer(t *testing.T) {
	h := newAskHarness(t, openAIWire, askThenText(openAIWire))
	ctx := context.Background()
	raw, _ := json.Marshal(1)
	if err := h.q.SetSetting(ctx, sqlc.SetSettingParams{Key: settingMaxRuns, Value: raw}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = h.pool.Exec(context.Background(), `DELETE FROM app_settings WHERE key = $1`, settingMaxRuns)
	})

	if _, status := h.send("plan my trip"); status != sqlc.RunStatusAwaitingInput {
		t.Fatalf("run ended %s, want awaiting_input", status)
	}
	before := shape(t, h.transcript())

	// Another conversation of the same person has a run going.
	other, err := h.q.CreateConversation(ctx, sqlc.CreateConversationParams{UserID: &h.user.ID, Title: "busy"})
	if err != nil {
		t.Fatal(err)
	}
	input := h.appendTo(other.ID, "something long")
	parked, err := h.q.CreateRun(ctx, sqlc.CreateRunParams{ConversationID: other.ID, UserID: h.user.ID, InputMessageID: input.ID, ModelID: h.model})
	if err != nil {
		t.Fatal(err)
	}

	rec := h.answer("call_ask_1", answersJSON)
	if rec.Code != http.StatusTooManyRequests || !strings.Contains(rec.Body.String(), "too_many_runs") {
		t.Fatalf("answer at the limit: %d %s", rec.Code, rec.Body)
	}
	equal(t, "transcript after the refusal", shape(t, h.transcript()), before)

	// The other run ends; the same answer now goes through.
	if _, err := h.pool.Exec(ctx, `UPDATE runs SET status = 'cancelled', finished_at = now() WHERE id = $1`, parked.ID); err != nil {
		t.Fatal(err)
	}
	if _, status := h.answered("call_ask_1", answersJSON); status != sqlc.RunStatusCompleted {
		t.Fatalf("the answer after a slot freed ended %s", status)
	}
}

func (h *askHarness) appendTo(conversation uuid.UUID, text string) sqlc.Message {
	h.t.Helper()
	msg, err := h.q.AppendEntry(context.Background(), sqlc.AppendEntryParams{
		ConversationID: conversation, Role: sqlc.MessageRoleUser, Content: text,
		Parts: agent.EncodeParts([]llm.Part{llm.TextPart{Text: text}}), Status: sqlc.MessageStatusComplete,
	})
	if err != nil {
		h.t.Fatal(err)
	}
	return msg
}
