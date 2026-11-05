package anthropic

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/qunqin24/openchat/apps/server/internal/llm"
	"github.com/qunqin24/openchat/apps/server/internal/llm/llmtest"
)

// answer is parts as the adapter keeps them after receiving them in answer
// to messages and tools: their thinking bound to that request.
func answer(t *testing.T, tools []llm.Tool, messages []llm.Message, parts ...llm.Part) llm.Message {
	t.Helper()
	_, end, err := buildRequest(llm.Request{Model: "claude-sonnet-5-5", Messages: messages, Tools: tools}, Config{}, false)
	if err != nil {
		t.Fatal(err)
	}
	return llm.Message{Role: llm.RoleAssistant, Parts: end.bind(parts)}
}

// sentThinking is the signatures of the thinking blocks a request sends, in
// order, and the roles of its messages.
func sentThinking(t *testing.T, req llm.Request) (signatures []string, roles []string) {
	t.Helper()
	req.Model = "claude-sonnet-5-5"
	body := build(t, req, Config{})
	for _, message := range body.Messages {
		roles = append(roles, message["role"].(string))
		for _, block := range message["content"].([]any) {
			b := block.(map[string]any)
			if b["type"] == "thinking" {
				signatures = append(signatures, b["signature"].(string))
			}
		}
	}
	return signatures, roles
}

var (
	webTool  = llm.Tool{Name: "web_search", Description: "Search the web.", Parameters: json.RawMessage(`{"type":"object"}`)}
	fileTool = llm.Tool{Name: "file_read", Description: "Read a file.", Parameters: json.RawMessage(`{"type":"object"}`)}
)

func conversation(t *testing.T, system string, tools []llm.Tool) []llm.Message {
	t.Helper()
	first := []llm.Message{llm.TextMessage(llm.RoleSystem, system), llm.TextMessage(llm.RoleUser, "first question")}
	reply := answer(t, tools, first,
		llm.ReasoningPart{Text: "thinking about it", Signature: "sig-1"},
		llm.ToolCallPart{ID: "call_1", Name: "web_search", Arguments: json.RawMessage(`{"query": "x", "limit": 3}`)},
	)
	results := llm.Message{Role: llm.RoleTool, Parts: []llm.Part{llm.ToolResultPart{CallID: "call_1", Name: "web_search", Status: llm.ToolSucceeded, Content: "found"}}}
	second := append(append(first, reply), results)
	final := answer(t, tools, second,
		llm.ReasoningPart{Text: "now I know", Signature: "sig-2"},
		llm.TextPart{Text: "the answer"},
	)
	return append(second, final, llm.TextMessage(llm.RoleUser, "follow-up"))
}

func TestThinkingGoesBackWhileItsConversationHolds(t *testing.T) {
	tools := []llm.Tool{webTool}
	messages := conversation(t, "system prompt", tools)
	got, _ := sentThinking(t, llm.Request{Messages: messages, Tools: tools})
	if strings.Join(got, ",") != "sig-1,sig-2" {
		t.Fatalf("sent %v, want both blocks", got)
	}
}

func TestThinkingIsLeftOutOnceWhatCameBeforeItChanged(t *testing.T) {
	tools := []llm.Tool{webTool}
	for name, change := range map[string]func([]llm.Message) ([]llm.Message, []llm.Tool){
		// A new release's prompt, or the phone app's note.
		"system prompt": func(m []llm.Message) ([]llm.Message, []llm.Tool) {
			m[0] = llm.TextMessage(llm.RoleSystem, "a new system prompt")
			return m, tools
		},
		// Web search switched off, or the file tools added.
		"tools": func(m []llm.Message) ([]llm.Message, []llm.Tool) { return m, []llm.Tool{webTool, fileTool} },
		// A compressed conversation's summary, merged into the first turn.
		"first message": func(m []llm.Message) ([]llm.Message, []llm.Tool) {
			return append([]llm.Message{llm.TextMessage(llm.RoleUser, "<summary>earlier</summary>")}, m...), tools
		},
	} {
		t.Run(name, func(t *testing.T) {
			messages, now := change(conversation(t, "system prompt", tools))
			got, roles := sentThinking(t, llm.Request{Messages: messages, Tools: now})
			if len(got) != 0 {
				t.Fatalf("sent %v, want no thinking", got)
			}
			if strings.Join(roles, ",") != "user,assistant,user,assistant,user" {
				t.Fatalf("roles = %v", roles)
			}
		})
	}
}

func TestThinkingFromBeforeThisIsLeftOut(t *testing.T) {
	messages := []llm.Message{
		llm.TextMessage(llm.RoleUser, "hello"),
		{Role: llm.RoleAssistant, Parts: []llm.Part{llm.ReasoningPart{Signature: "sig-old"}, llm.TextPart{Text: "hi"}}},
		llm.TextMessage(llm.RoleUser, "again"),
	}
	if got, _ := sentThinking(t, llm.Request{Messages: messages}); len(got) != 0 {
		t.Fatalf("sent %v, want the unbound block left out", got)
	}
}

// A block made after the ones before it were left out followed none of them,
// so it goes back once they are left out again; one made while they were
// still sent does not, its chain being broken.
func TestThinkingAfterALeftOutRunFollowsTheChain(t *testing.T) {
	old := []llm.Message{llm.TextMessage(llm.RoleSystem, "old prompt"), llm.TextMessage(llm.RoleUser, "q1")}
	stale := answer(t, nil, old, llm.ReasoningPart{Signature: "sig-old"}, llm.TextPart{Text: "a1"})

	now := []llm.Message{llm.TextMessage(llm.RoleSystem, "new prompt"), llm.TextMessage(llm.RoleUser, "q1"), stale, llm.TextMessage(llm.RoleUser, "q2")}
	fresh := answer(t, nil, now, llm.ReasoningPart{Signature: "sig-new"}, llm.TextPart{Text: "a2"})
	got, _ := sentThinking(t, llm.Request{Messages: append(now, fresh, llm.TextMessage(llm.RoleUser, "q3"))})
	if strings.Join(got, ",") != "sig-new" {
		t.Fatalf("sent %v, want only the block made after the stale one went", got)
	}

	// Made under the old prompt after the stale block was sent: both go.
	before := append(old, stale, llm.TextMessage(llm.RoleUser, "q2"))
	chained := answer(t, nil, before, llm.ReasoningPart{Signature: "sig-chained"}, llm.TextPart{Text: "a2"})
	messages := append([]llm.Message{llm.TextMessage(llm.RoleSystem, "new prompt")}, append(before[1:], chained, llm.TextMessage(llm.RoleUser, "q3"))...)
	if got, _ := sentThinking(t, llm.Request{Messages: messages}); len(got) != 0 {
		t.Fatalf("sent %v, want neither", got)
	}
}

// Tool arguments come back from the database as values, not as the bytes the
// model streamed.
func TestThinkingSurvivesArgumentsReencoded(t *testing.T) {
	tools := []llm.Tool{webTool}
	messages := conversation(t, "system prompt", tools)
	call := messages[2].Parts[1].(llm.ToolCallPart)
	call.Arguments = json.RawMessage(`{"limit":3,"query":"x"}`)
	messages[2].Parts[1] = call
	if got, _ := sentThinking(t, llm.Request{Messages: messages, Tools: tools}); strings.Join(got, ",") != "sig-1,sig-2" {
		t.Fatalf("sent %v, want both blocks", got)
	}
}

// A reply that was only thinking leaves nothing once its block goes: the
// turns either side become one.
func TestAThinkingOnlyReplyLeftOutJoinsItsNeighbours(t *testing.T) {
	messages := []llm.Message{
		llm.TextMessage(llm.RoleUser, "hello"),
		{Role: llm.RoleAssistant, Parts: []llm.Part{llm.ReasoningPart{Signature: "sig-old"}}},
		llm.TextMessage(llm.RoleUser, "are you there?"),
	}
	body := build(t, llm.Request{Model: "claude-sonnet-5-5", Messages: messages}, Config{})
	if len(body.Messages) != 1 || len(body.Messages[0]["content"].([]any)) != 2 {
		t.Fatalf("messages = %v, want one user turn of two blocks", body.Messages)
	}
}

// Refused all the same - recorded before this, or issued to another
// organisation - the request goes again once with no thinking, and what comes
// back is bound to that.
func TestClientRetriesWithoutThinkingWhenABlockIsRefused(t *testing.T) {
	var bodies []string
	client, err := New(Config{
		Provider: "anthropic",
		BaseURL:  "https://api.example.test/v1",
		HTTPClient: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			raw, _ := io.ReadAll(r.Body)
			bodies = append(bodies, string(raw))
			if len(bodies) == 1 {
				return respond(http.StatusBadRequest, "application/json",
					`{"type":"error","error":{"type":"invalid_request_error","message":"messages.1.content.0: Invalid `+"`signature`"+` in `+"`thinking`"+` block. The block is bound to a different conversation."}}`), nil
			}
			return respond(http.StatusOK, "text/event-stream", string(fixture(t, "signature.sse"))), nil
		})},
	})
	if err != nil {
		t.Fatal(err)
	}
	history := []llm.Message{llm.TextMessage(llm.RoleUser, "hello")}
	reply := answer(t, nil, history, llm.ReasoningPart{Signature: "sig-1"}, llm.TextPart{Text: "hi"})
	resp, err := client.Stream(context.Background(), llm.Request{
		Model:    "claude-sonnet-5-5",
		Messages: append(history, reply, llm.TextMessage(llm.RoleUser, "again")),
	}, (&llmtest.Recorder{}).Emit)
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	if len(bodies) != 2 || !strings.Contains(bodies[0], "sig-1") || strings.Contains(bodies[1], `"thinking","thinking"`) {
		t.Fatalf("bodies = %q", bodies)
	}
	for _, part := range resp.Message.Parts {
		if r, ok := part.(llm.ReasoningPart); ok && r.Signature != "" && !strings.HasPrefix(r.Binding, bindingVersion) {
			t.Fatalf("reasoning %+v is not bound", r)
		}
	}
}

// The turn whose tool calls a request answers keeps its thinking even where
// what came before it changed: older models refuse a tool round whose turn
// does not start with it, and the newer ones' refusal brings the retry.
func TestAPendingToolTurnKeepsItsThinking(t *testing.T) {
	tools := []llm.Tool{webTool}
	first := []llm.Message{llm.TextMessage(llm.RoleSystem, "old prompt"), llm.TextMessage(llm.RoleUser, "q")}
	call := answer(t, tools, first,
		llm.ReasoningPart{Signature: "sig-call"},
		llm.ToolCallPart{ID: "call_1", Name: "web_search", Arguments: json.RawMessage(`{}`)},
	)
	results := llm.Message{Role: llm.RoleTool, Parts: []llm.Part{llm.ToolResultPart{CallID: "call_1", Name: "web_search", Status: llm.ToolSucceeded, Content: "found"}}}
	messages := []llm.Message{llm.TextMessage(llm.RoleSystem, "a compressed history"), llm.TextMessage(llm.RoleUser, "q"), call, results}
	if got, _ := sentThinking(t, llm.Request{Messages: messages, Tools: tools}); strings.Join(got, ",") != "sig-call" {
		t.Fatalf("sent %v, want the pending turn's block", got)
	}
}
