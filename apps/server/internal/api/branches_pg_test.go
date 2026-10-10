package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/qunqin/openchat/apps/server/internal/db/sqlc"
)

// Conversation branching end to end, over the HTTP handlers and a scripted
// model that answers "re: <the last thing it was asked>" and keeps every request
// it was sent, so what a model is shown after an edit can be read back.
// Needs the same scratch database as the other agent tests.

// branching is a conversation whose model echoes what it was last asked.
type branching struct {
	*askHarness
}

func newBranching(t *testing.T) *branching {
	t.Helper()
	var b branching
	b.askHarness = newAskHarness(t, openAIWire, func(n int) string {
		history := b.history(n)
		last := history[len(history)-1]
		// A question that says "ask" makes the model ask one back.
		if strings.HasPrefix(last, "user:") && strings.Contains(last, "ask") {
			return openAIWire.ask(fmt.Sprintf("call_%d", n), questionsJSON)
		}
		return openAIWire.text("re: " + strings.TrimPrefix(last, "user:"))
	})
	return &b
}

var dateStamp = regexp.MustCompile(`\[Date: [0-9-]+\]`)

// wording is a request message's text, without the date stamp the first
// message of a day carries.
func wording(content json.RawMessage) string {
	var text string
	if json.Unmarshal(content, &text) != nil {
		var parts []struct {
			Text string `json:"text"`
		}
		_ = json.Unmarshal(content, &parts)
		for _, p := range parts {
			text += p.Text
		}
	}
	return dateStamp.ReplaceAllString(text, "")
}

// history is the nth request's conversation as role:text, without the system
// prompt.
func (b *branching) history(n int) []string {
	b.t.Helper()
	var req struct {
		Messages []struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal([]byte(b.body(n)), &req); err != nil {
		b.t.Fatalf("request body: %v", err)
	}
	var out []string
	for _, m := range req.Messages {
		if m.Role != "system" {
			out = append(out, m.Role+":"+wording(m.Content))
		}
	}
	return out
}

// lastHistory is what the model was sent most recently.
func (b *branching) lastHistory() []string {
	b.mu.Lock()
	n := len(b.bodies)
	b.mu.Unlock()
	return b.history(n)
}

// path is the branch shown, as role:text.
func (b *branching) path() []string {
	b.t.Helper()
	var out []string
	for _, m := range b.transcript() {
		out = append(out, string(m.Role)+":"+m.Content)
	}
	return out
}

func (b *branching) edit(id uuid.UUID, content string) (CreateMessageResponse, sqlc.RunStatus) {
	b.t.Helper()
	rec := httptest.NewRecorder()
	b.server.EditMessage(rec, b.request(http.MethodPost, fmt.Sprintf(`{"content":%q,"model":%q}`, content, b.model)), b.conv.ID, id)
	var out CreateMessageResponse
	b.accepted(rec, &out)
	return out, b.settle(out.Run.Id)
}

// switchTo shows another version of a question and returns the page it answers.
func (b *branching) switchTo(id uuid.UUID) MessagePage {
	b.t.Helper()
	rec := httptest.NewRecorder()
	b.server.SwitchVersion(rec, b.request(http.MethodPost, ""), b.conv.ID, id, SwitchVersionParams{})
	if rec.Code != http.StatusOK {
		b.t.Fatalf("switch: %d %s", rec.Code, rec.Body)
	}
	var page MessagePage
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		b.t.Fatal(err)
	}
	return page
}

func (b *branching) refused(rec *httptest.ResponseRecorder, status int, code string) {
	b.t.Helper()
	if rec.Code != status || !strings.Contains(rec.Body.String(), code) {
		b.t.Fatalf("got %d %s, want %d %s", rec.Code, rec.Body, status, code)
	}
}

// question finds a user message by its wording on the branch shown.
func (b *branching) question(content string) Message {
	b.t.Helper()
	for _, m := range b.transcript() {
		if m.Role == RoleUser && m.Content == content {
			return m
		}
	}
	b.t.Fatalf("no question %q on the branch shown: %v", content, b.path())
	return Message{}
}

func ids(list *[]uuid.UUID) []uuid.UUID {
	if list == nil {
		return nil
	}
	return *list
}

func same(t *testing.T, what string, got, want []string) {
	t.Helper()
	equal(t, what, got, want)
}

// Editing any question — not only the last — starts a branch beside it: the
// model is asked with what came before and nothing from the branch left, the
// turns that followed are off the transcript, and switching back brings them
// and their context back.
func TestEditingAnEarlierQuestionStartsABranch(t *testing.T) {
	b := newBranching(t)
	sent := map[string]CreateMessageResponse{}
	for _, q := range []string{"q1", "q2", "q3", "q4"} {
		sent[q], _ = b.send(q)
	}
	same(t, "four turns", b.path(), []string{
		"user:q1", "assistant:re: q1", "user:q2", "assistant:re: q2",
		"user:q3", "assistant:re: q3", "user:q4", "assistant:re: q4"})

	// Edit the second question.
	q2b, status := b.edit(sent["q2"].Message.Id, "q2b")
	if status != sqlc.RunStatusCompleted {
		t.Fatalf("edit's run ended %s", status)
	}
	same(t, "the model is asked with the path only", b.lastHistory(), []string{"user:q1", "assistant:re: q1", "user:q2b"})
	same(t, "after editing", b.path(), []string{"user:q1", "assistant:re: q1", "user:q2b", "assistant:re: q2b"})
	if q2b.Message.EditOf == nil || *q2b.Message.EditOf != sent["q2"].Message.Id {
		t.Fatalf("editOf: %v", q2b.Message.EditOf)
	}
	versions := []uuid.UUID{sent["q2"].Message.Id, q2b.Message.Id}
	if got := ids(q2b.Message.Branches); !equalIDs(got, versions) {
		t.Fatalf("the edit's branches: %v, want %v", got, versions)
	}
	if shown := b.question("q2b"); !equalIDs(ids(shown.Branches), versions) {
		t.Fatalf("branches in the transcript: %v", ids(shown.Branches))
	}
	if first := b.question("q1"); first.Branches != nil {
		t.Fatalf("a question with one version has branches %v", ids(first.Branches))
	}

	// Back to the original: its later turns are there again, and the next
	// message is answered from them.
	page := b.switchTo(sent["q2"].Message.Id)
	var restored []string
	for _, m := range page.Items {
		restored = append(restored, string(m.Role)+":"+m.Content)
	}
	want := []string{
		"user:q1", "assistant:re: q1", "user:q2", "assistant:re: q2",
		"user:q3", "assistant:re: q3", "user:q4", "assistant:re: q4"}
	same(t, "switch's page", restored, want)
	same(t, "after switching back", b.path(), want)
	b.send("q5")
	same(t, "context after switching back", b.lastHistory(),
		[]string{"user:q1", "assistant:re: q1", "user:q2", "assistant:re: q2", "user:q3", "assistant:re: q3", "user:q4", "assistant:re: q4", "user:q5"})

	// And to the edit again: only its own branch.
	b.switchTo(q2b.Message.Id)
	same(t, "the edit's branch", b.path(), []string{"user:q1", "assistant:re: q1", "user:q2b", "assistant:re: q2b"})
	b.send("q3b")
	same(t, "context on the edit's branch", b.lastHistory(),
		[]string{"user:q1", "assistant:re: q1", "user:q2b", "assistant:re: q2b", "user:q3b"})
}

// Branches nest: an edit inside an edit, switched in either direction, and the
// reader comes back to the end of the branch they were last on.
func TestNestedBranchesRestoreTheLastBranchShown(t *testing.T) {
	b := newBranching(t)
	q1, _ := b.send("q1")
	q2, _ := b.send("q2")
	b.send("q3")

	q2b, _ := b.edit(q2.Message.Id, "q2b")
	b.send("q3b")
	q3b := b.question("q3b")
	q3c, _ := b.edit(q3b.Id, "q3c")
	same(t, "nested edit's context", b.lastHistory(),
		[]string{"user:q1", "assistant:re: q1", "user:q2b", "assistant:re: q2b", "user:q3c"})
	b.send("q4c")

	// q3b/q3c are versions of one question under q2b.
	if got, want := ids(b.question("q3c").Branches), []uuid.UUID{q3b.Id, q3c.Message.Id}; !equalIDs(got, want) {
		t.Fatalf("q3c's branches %v, want %v", got, want)
	}
	// q2 and q2b are versions of one question under q1; q1 is on its own.
	if got, want := ids(b.question("q2b").Branches), []uuid.UUID{q2.Message.Id, q2b.Message.Id}; !equalIDs(got, want) {
		t.Fatalf("q2b's branches %v, want %v", got, want)
	}
	_ = q1

	// Look at q3b, then leave for the original second question.
	b.switchTo(q3b.Id)
	same(t, "q3b's branch", b.path(), []string{"user:q1", "assistant:re: q1", "user:q2b", "assistant:re: q2b", "user:q3b", "assistant:re: q3b"})
	b.switchTo(q2.Message.Id)
	same(t, "the original branch", b.path(), []string{
		"user:q1", "assistant:re: q1", "user:q2", "assistant:re: q2", "user:q3", "assistant:re: q3"})

	// Coming back lands on q3b — the branch last shown — not on q3c, the
	// newest one.
	b.switchTo(q2b.Message.Id)
	same(t, "back under the edit", b.path(), []string{"user:q1", "assistant:re: q1", "user:q2b", "assistant:re: q2b", "user:q3b", "assistant:re: q3b"})
	b.switchTo(q3c.Message.Id)
	same(t, "q3c's branch keeps its later turn", b.path(), []string{
		"user:q1", "assistant:re: q1", "user:q2b", "assistant:re: q2b", "user:q3c", "assistant:re: q3c", "user:q4c", "assistant:re: q4c"})

	// Editing the first question branches at the root.
	root, _ := b.edit(q1.Message.Id, "q1b")
	same(t, "a root's edit", b.path(), []string{"user:q1b", "assistant:re: q1b"})
	if got, want := ids(root.Message.Branches), []uuid.UUID{q1.Message.Id, root.Message.Id}; !equalIDs(got, want) {
		t.Fatalf("root's branches %v, want %v", got, want)
	}
	b.switchTo(q1.Message.Id)
	same(t, "the first question's own branch", b.path(), []string{
		"user:q1", "assistant:re: q1", "user:q2b", "assistant:re: q2b", "user:q3c", "assistant:re: q3c", "user:q4c", "assistant:re: q4c"})
}

// Regenerating works on the last turn of the branch shown, and the answer
// chosen is the one the branch keeps.
func TestRegenerateOnABranch(t *testing.T) {
	b := newBranching(t)
	q1, _ := b.send("q1")
	q2, _ := b.send("q2")
	q2b, _ := b.edit(q2.Message.Id, "q2b")

	rec := httptest.NewRecorder()
	b.server.RegenerateAnswer(rec, b.request(http.MethodPost, fmt.Sprintf(`{"model":%q}`, b.model)), b.conv.ID)
	var again CreateMessageResponse
	b.accepted(rec, &again)
	b.settle(again.Run.Id)
	if again.Message.Id != q2b.Message.Id {
		t.Fatalf("regenerated %s, want the edit %s", again.Message.Id, q2b.Message.Id)
	}
	if v := ids(again.Message.Versions); len(v) != 2 || v[0] != q2b.Run.Id || v[1] != again.Run.Id {
		t.Fatalf("answer versions: %v", v)
	}
	same(t, "regenerate's context", b.lastHistory(), []string{"user:q1", "assistant:re: q1", "user:q2b"})
	same(t, "one answer shown", b.path(), []string{"user:q1", "assistant:re: q1", "user:q2b", "assistant:re: q2b"})

	// The first answer back, then away and back: the choice is the branch's.
	rec = httptest.NewRecorder()
	b.server.ChooseAnswer(rec, b.request(http.MethodPost, ""), b.conv.ID, q2b.Run.Id)
	if rec.Code != http.StatusOK {
		t.Fatalf("choose: %d %s", rec.Code, rec.Body)
	}
	b.switchTo(q2.Message.Id)
	b.switchTo(q2b.Message.Id)
	if got := b.question("q2b").Versions; got == nil || len(*got) != 2 {
		t.Fatalf("answer versions after switching: %v", got)
	}

	// An earlier turn's answer cannot be swapped, on a branch either.
	q1Runs, err := b.q.ListAnswers(context.Background(), []uuid.UUID{q1.Message.Id})
	if err != nil || len(q1Runs) != 1 {
		t.Fatalf("q1's answers: %v %v", q1Runs, err)
	}
	rec = httptest.NewRecorder()
	b.server.ChooseAnswer(rec, b.request(http.MethodPost, ""), b.conv.ID, q1Runs[0].ID)
	b.refused(rec, http.StatusConflict, "not_latest")

	// Editing a question does not touch the answers of the one it replaces.
	b.switchTo(q2.Message.Id)
	if got := b.question("q2").Versions; got != nil {
		t.Fatalf("the original question gained answer versions: %v", ids(got))
	}
}

// What cannot be edited or switched to is refused with its own reason.
func TestBranchRefusals(t *testing.T) {
	b := newBranching(t)
	q1, _ := b.send("q1")
	q2, _ := b.send("q2")
	q2b, _ := b.edit(q2.Message.Id, "q2b")
	b.send("q3b")
	q3 := b.question("q3b").Id

	post := func(content string) *http.Request {
		return b.request(http.MethodPost, fmt.Sprintf(`{"content":%q}`, content))
	}

	// q2 is on another branch now.
	rec := httptest.NewRecorder()
	b.server.EditMessage(rec, post("again"), b.conv.ID, q2.Message.Id)
	b.refused(rec, http.StatusConflict, "not_on_path")
	// An answer is not a question.
	var answer Message
	for _, m := range b.transcript() {
		if m.Role == RoleAssistant {
			answer = m
		}
	}
	rec = httptest.NewRecorder()
	b.server.EditMessage(rec, post("again"), b.conv.ID, answer.Id)
	b.refused(rec, http.StatusNotFound, "message_not_found")
	rec = httptest.NewRecorder()
	b.server.EditMessage(rec, post("again"), b.conv.ID, uuid.New())
	b.refused(rec, http.StatusNotFound, "message_not_found")
	rec = httptest.NewRecorder()
	b.server.EditMessage(rec, post("  "), b.conv.ID, q1.Message.Id)
	b.refused(rec, http.StatusBadRequest, "empty_content")

	// Switching: to a version of a question on the path, yes; to a question
	// deep inside another branch, no.
	b.switchTo(q2.Message.Id)
	rec = httptest.NewRecorder()
	b.server.SwitchVersion(rec, b.request(http.MethodPost, ""), b.conv.ID, q3, SwitchVersionParams{})
	b.refused(rec, http.StatusConflict, "not_on_path")
	rec = httptest.NewRecorder()
	b.server.SwitchVersion(rec, b.request(http.MethodPost, ""), b.conv.ID, uuid.New(), SwitchVersionParams{})
	b.refused(rec, http.StatusNotFound, "message_not_found")
	// Switching to the version already shown changes nothing.
	b.switchTo(q2.Message.Id)
	same(t, "unchanged", b.path(), []string{"user:q1", "assistant:re: q1", "user:q2", "assistant:re: q2"})

	// Nothing moves while a run is going.
	if _, err := b.q.CreateRun(context.Background(), sqlc.CreateRunParams{
		ConversationID: b.conv.ID, UserID: b.user.ID, InputMessageID: q2.Message.Id, ModelID: "m",
	}); err != nil {
		t.Fatal(err)
	}
	rec = httptest.NewRecorder()
	b.server.SwitchVersion(rec, b.request(http.MethodPost, ""), b.conv.ID, q2b.Message.Id, SwitchVersionParams{})
	b.refused(rec, http.StatusConflict, "conversation_busy")
	rec = httptest.NewRecorder()
	b.server.EditMessage(rec, post("again"), b.conv.ID, q1.Message.Id)
	b.refused(rec, http.StatusConflict, "conversation_busy")
	same(t, "nothing was written", b.path(), []string{"user:q1", "assistant:re: q1", "user:q2", "assistant:re: q2"})
}

func equalIDs(a, b []uuid.UUID) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// A question the model asked belongs to its branch: it can be answered while
// that branch is shown, not from another, and the answer carries on there.
func TestAskUserOnABranch(t *testing.T) {
	b := newBranching(t)
	b.send("q1")
	q2, _ := b.send("q2")
	asked, status := b.edit(q2.Message.Id, "ask q2b")
	if status != sqlc.RunStatusAwaitingInput {
		t.Fatalf("the edit's run ended %s, want awaiting_input", status)
	}
	call := fmt.Sprintf("call_%d", 3)
	equal(t, "asking on the branch", shape(t, b.transcript()), []string{"user:text", "assistant:text", "user:text", "assistant:call"})

	// On the other branch the question is not there to answer.
	b.switchTo(q2.Message.Id)
	if rec := b.answer(call, answersJSON); rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "not_pending") {
		t.Fatalf("answering from the other branch: %d %s", rec.Code, rec.Body)
	}
	same(t, "nothing written", b.path(), []string{"user:q1", "assistant:re: q1", "user:q2", "assistant:re: q2"})

	// Back on it, the question is still waiting; the answer goes on from it.
	b.switchTo(asked.Message.Id)
	out, status := b.answered(call, answersJSON)
	if status != sqlc.RunStatusCompleted {
		t.Fatalf("the continuation ended %s", status)
	}
	if out.Run.Continues == nil {
		t.Fatal("the answer did not continue the run that asked")
	}
	equal(t, "continued on the branch", shape(t, b.transcript()), []string{"user:text", "assistant:text", "user:text", "assistant:call", "tool:result", "assistant:text"})
	history := b.lastHistory()
	if len(history) != 5 || history[0] != "user:q1" || history[1] != "assistant:re: q1" || history[2] != "user:ask q2b" {
		t.Fatalf("continuation's context: %q", history)
	}
}

// A conversation with branches can be deleted for good: the leaf pointer and
// the parent links do not hold it back.
func TestDeletingABranchedConversation(t *testing.T) {
	b := newBranching(t)
	q1, _ := b.send("q1")
	b.send("q2")
	b.edit(q1.Message.Id, "q1b")
	ctx := context.Background()
	if _, err := b.pool.Exec(ctx, `DELETE FROM conversations WHERE id = $1`, b.conv.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	var left int
	if err := b.pool.QueryRow(ctx, `SELECT count(*) FROM messages WHERE conversation_id = $1`, b.conv.ID).Scan(&left); err != nil || left != 0 {
		t.Fatalf("%d messages left (%v)", left, err)
	}
}
