package api

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// What the browser stores and sends back must be what the server signed, ids
// and all. The memory tool's result names the memory by the id the request
// staged it under (guest_ids.go), which the browser is given back as its own;
// the receipt has to be for the form the browser holds.

// memoryTurn is one answered request of a conversation, as the browser keeps it.
type memoryTurn struct {
	conv     uuid.UUID
	rows     []any // the rows, as streamed, in order
	end      map[string]any
	memories []any
}

// memoryRun sends one question of a conversation that holds `rows` and `memories`
// and keeps what comes back. The script answers by the nth model request.
func memoryRun(t *testing.T, h *guestHarness, ip string, prev *memoryTurn, conv uuid.UUID, memories []any, question string) memoryTurn {
	t.Helper()
	input := map[string]any{"id": uuid.New(), "role": "user", "content": question, "createdAt": time.Now().Format(time.RFC3339Nano), "parts": []any{}}
	body := map[string]any{
		"action": "send", "conversationId": conv, "model": h.model, "titled": true,
		"memoryEnabled": true, "memories": memories, "messages": []any{}, "input": input,
	}
	if prev != nil && prev.conv == conv {
		body["messages"] = prev.rows
		body["memorySnapshot"] = prev.end["memorySnapshot"]
		rc := map[string]any{}
		if r, ok := prev.end["receipts"].(map[string]any); ok {
			rc["memorySnapshot"] = r["memorySnapshot"]
			rc["aboutYou"], rc["responseStyle"] = r["aboutYou"], r["responseStyle"]
		}
		body["receipts"] = rc
	}
	got := h.runAs(ip, body)
	if got.status != http.StatusOK {
		t.Fatalf("%q: %d %v", question, got.status, got.error)
	}
	turn := memoryTurn{conv: conv, end: got.end()}
	if prev != nil && prev.conv == conv {
		turn.rows = append(turn.rows, prev.rows...)
	}
	turn.rows = append(turn.rows, got.find("run_started")["message"])
	for _, e := range got.events() {
		if e["type"] == "message_appended" {
			turn.rows = append(turn.rows, e["message"])
		}
	}
	turn.memories, _ = turn.end["memories"].([]any)
	return turn
}

func TestGuestMemoryEditsKeepTheConversationSendable(t *testing.T) {
	for _, op := range []struct{ name, args string }{
		{"update", `{"op":"update","id":"m1","content":"likes oolong tea"}`},
		{"delete", `{"op":"delete","id":"m1"}`},
		{"add", `{"op":"add","content":"lives in Hangzhou"}`},
	} {
		t.Run(op.name, func(t *testing.T) {
			h, f := newGuestModeration(t, noStrikes)
			ip := "198.51.100." + map[string]string{"update": "60", "delete": "61", "add": "62"}[op.name]
			// The model edits the list on the first request of a conversation, and answers otherwise.
			h.script = func(n int, body string) string {
				if n == 1 || (strings.Contains(body, "edit again") && !strings.Contains(body, `"role":"tool"`)) {
					return openAIToolCall(fmt.Sprintf("call_%d", n), "memory", op.args)
				}
				return openAIWire.text(fmt.Sprintf("Noted %d.", n))
			}
			note := map[string]any{"id": uuid.New(), "content": "likes green tea", "source": "user"}
			conv := uuid.New()
			first := memoryRun(t, h, ip, nil, conv, []any{note}, "I like tea")
			if len(first.rows) < 4 {
				t.Fatalf("the model did not use the tool: %d rows", len(first.rows))
			}

			// The premise: the tool's result names the memory by the id the browser
			// chose, which is not the one the request staged it under.
			if op.name != "add" {
				tool, _ := first.rows[2].(map[string]any)
				details := toolDetails(tool)
				if details["memoryId"] != note["id"].(uuid.UUID).String() {
					t.Fatalf("the tool row names memory %v, want the browser's %v", details["memoryId"], note["id"])
				}
			}

			// The next message of the same conversation, all of it handed back as stored:
			// the rows of the tool call, the list as it now is, the snapshot.
			before := f.calls()
			second := memoryRun(t, h, ip, &first, conv, first.memories, "and coffee?")
			if n := f.calls() - before; n != 1 {
				t.Errorf("the next message cost %d moderation calls, want 1 (the new message)", n)
			}

			// And a second conversation of the same browser, with the list the first left.
			before = f.calls()
			other := uuid.New()
			third := memoryRun(t, h, ip, nil, other, second.memories, "edit again")
			if n := f.calls() - before; n != 1 {
				// The notes carry their own receipts into a conversation that has no snapshot yet.
				t.Errorf("the second conversation's first message cost %d calls, want 1", n)
			}
			before = f.calls()
			fourth := memoryRun(t, h, ip, &third, other, third.memories, "thanks")
			if n := f.calls() - before; n != 1 {
				t.Errorf("the next message of the second conversation cost %d calls, want 1", n)
			}
			_ = fourth
		})
	}
}

// toolDetails is the details of the first tool result in a tool row.
func toolDetails(row map[string]any) map[string]any {
	parts, _ := row["parts"].([]any)
	for _, p := range parts {
		if part, _ := p.(map[string]any); part["type"] == "tool_result" {
			d, _ := part["details"].(map[string]any)
			return d
		}
	}
	return nil
}
