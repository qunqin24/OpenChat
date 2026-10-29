package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/qunqin24/openchat/apps/server/internal/db/sqlc"
	"github.com/qunqin24/openchat/apps/server/internal/memory"
)

// Content moderation of the texts a person keeps - custom instructions and
// memories they write - which reach the model through the conversation's
// snapshot without ever being sent as a message. Same fakes as the message tests.

func (h *askHarness) newConv() sqlc.Conversation {
	h.t.Helper()
	conv, err := h.q.CreateConversation(context.Background(), sqlc.CreateConversationParams{UserID: &h.user.ID, Title: "ask"})
	if err != nil {
		h.t.Fatal(err)
	}
	return conv
}

// sendIn sends a message into a conversation and, if it was accepted, waits for
// the run to stop.
func (h *askHarness) sendIn(conv uuid.UUID, content string) *httptest.ResponseRecorder {
	h.t.Helper()
	rec := httptest.NewRecorder()
	h.server.CreateMessage(rec, h.request(http.MethodPost, fmt.Sprintf(`{"content":%q,"model":%q}`, content, h.model)), conv)
	if rec.Code == http.StatusAccepted {
		var out CreateMessageResponse
		h.accepted(rec, &out)
		h.settle(out.Run.Id)
	}
	return rec
}

func (h *askHarness) personalize(body string) *httptest.ResponseRecorder {
	h.t.Helper()
	rec := httptest.NewRecorder()
	h.server.UpdatePersonalization(rec, h.request(http.MethodPatch, body))
	return rec
}

func (h *askHarness) saveMemory(content string) *httptest.ResponseRecorder {
	h.t.Helper()
	rec := httptest.NewRecorder()
	h.server.CreateMemory(rec, h.request(http.MethodPost, fmt.Sprintf(`{"content":%q}`, content)))
	return rec
}

func (h *askHarness) rewriteMemory(id uuid.UUID, content string) *httptest.ResponseRecorder {
	h.t.Helper()
	rec := httptest.NewRecorder()
	h.server.UpdateMemory(rec, h.request(http.MethodPatch, fmt.Sprintf(`{"content":%q}`, content)), id)
	return rec
}

// modelSaw counts the requests to the model whose body has the text in it.
func (h *askHarness) modelSaw(text string) (n int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, body := range h.bodies {
		if strings.Contains(body, text) {
			n++
		}
	}
	return n
}

func (h *askHarness) instructions() (about, style string, aboutChecked, styleChecked *string) {
	h.t.Helper()
	if err := h.pool.QueryRow(context.Background(),
		`SELECT instructions_about, instructions_style, instructions_about_checked_hash, instructions_style_checked_hash FROM users WHERE id = $1`,
		h.user.ID).Scan(&about, &style, &aboutChecked, &styleChecked); err != nil {
		h.t.Fatal(err)
	}
	return
}

func (h *askHarness) memoryHashes() map[string][2]*string {
	h.t.Helper()
	rows, err := h.pool.Query(context.Background(), `SELECT content, checked_hash, blocked_hash FROM user_memories WHERE user_id = $1`, h.user.ID)
	if err != nil {
		h.t.Fatal(err)
	}
	defer rows.Close()
	out := map[string][2]*string{}
	for rows.Next() {
		var content string
		var checked, blocked *string
		if err := rows.Scan(&content, &checked, &blocked); err != nil {
			h.t.Fatal(err)
		}
		out[content] = [2]*string{checked, blocked}
	}
	return out
}

func (h *askHarness) snapshotOf(conv uuid.UUID) (memory.Snapshot, bool) {
	h.t.Helper()
	var raw []byte
	if err := h.pool.QueryRow(context.Background(), `SELECT memory_snapshot FROM conversations WHERE id = $1`, conv).Scan(&raw); err != nil {
		h.t.Fatal(err)
	}
	if raw == nil {
		return memory.Snapshot{}, false
	}
	var snap memory.Snapshot
	if err := json.Unmarshal(raw, &snap); err != nil {
		h.t.Fatal(err)
	}
	return snap, true
}

func (h *askHarness) eventsOf(source string) int {
	return h.count(`SELECT count(*) FROM moderation_events WHERE user_id = $1 AND source = $2`, h.user.ID, source)
}

func newModerationMemoryHarness(t *testing.T, tune string) (*askHarness, *fakeModeration) {
	t.Helper()
	h, f := newModerationHarness(t, tune)
	// Memories and instructions are the user's own; start from none, and leave none.
	clear := func() {
		_, _ = h.pool.Exec(context.Background(), `DELETE FROM user_memories WHERE user_id = $1`, h.user.ID)
	}
	clear()
	t.Cleanup(clear)
	return h, f
}

// The reproduction: blocked words in the custom instructions and in a memory
// are refused when saved, whichever way they are written, and nothing is saved.
func TestModerationRefusesBlockedInstructionsAndMemoriesOnSave(t *testing.T) {
	// No strike limit: this test refuses more often than the default allows.
	h, f := newModerationMemoryHarness(t, `,"strikes":0`)

	categories := refused(t, h.personalize(`{"aboutYou":"I am into BLOCKME"}`))
	if strings.Join(categories, ",") != "violence/graphic" {
		t.Errorf("categories %v", categories)
	}
	refused(t, h.personalize(`{"responseStyle":"always BLOCKME","memoryEnabled":false}`))
	about, style, _, _ := h.instructions()
	if about != "" || style != "" {
		t.Errorf("a refused save left instructions: %q %q", about, style)
	}
	// Nothing of the refused request was applied, the switch included.
	if h.count(`SELECT count(*) FROM users WHERE id = $1 AND memory_enabled`, h.user.ID) != 1 {
		t.Error("a refused save changed the memory switch")
	}

	refused(t, h.saveMemory("remember to BLOCKME"))
	if len(h.memoryHashes()) != 0 {
		t.Error("a refused memory was saved")
	}

	// A passing one is saved and recorded as checked.
	rec := h.saveMemory("likes green tea")
	if rec.Code != http.StatusCreated {
		t.Fatalf("a clean memory: %d %s", rec.Code, rec.Body)
	}
	var saved Memory
	_ = json.Unmarshal(rec.Body.Bytes(), &saved)
	if hashes := h.memoryHashes()["likes green tea"]; hashes[0] == nil || *hashes[0] != memory.Hash("likes green tea") {
		t.Errorf("a checked memory is not recorded as checked: %v", hashes)
	}
	// Rewriting it into something blocked is refused too, and leaves it as it was.
	refused(t, h.rewriteMemory(saved.Id, "likes BLOCKME"))
	if _, ok := h.memoryHashes()["likes green tea"]; !ok || len(h.memoryHashes()) != 1 {
		t.Errorf("a refused rewrite changed the memory: %v", h.memoryHashes())
	}
	if rec := h.rewriteMemory(saved.Id, "likes black tea"); rec.Code != http.StatusOK {
		t.Fatalf("a clean rewrite: %d %s", rec.Code, rec.Body)
	}
	if hashes := h.memoryHashes()["likes black tea"]; hashes[0] == nil || *hashes[0] != memory.Hash("likes black tea") {
		t.Errorf("a rewritten memory is not recorded as checked: %v", hashes)
	}

	if rec := h.personalize(`{"aboutYou":"a teacher","responseStyle":"short"}`); rec.Code != http.StatusOK {
		t.Fatalf("clean instructions: %d %s", rec.Code, rec.Body)
	}
	about, style, aboutChecked, styleChecked := h.instructions()
	if about != "a teacher" || style != "short" || aboutChecked == nil || *aboutChecked != memory.Hash("a teacher") ||
		styleChecked == nil || *styleChecked != memory.Hash("short") {
		t.Errorf("instructions %q %q checked %v %v", about, style, aboutChecked, styleChecked)
	}

	// Logged like a message, with where it came from.
	if h.eventsOf("instructions") != 2 || h.eventsOf("memory") != 2 {
		t.Errorf("events: %d instructions, %d memory", h.eventsOf("instructions"), h.eventsOf("memory"))
	}
	// Saved unchanged, a field is not asked about again.
	calls := f.calls()
	if rec := h.personalize(`{"aboutYou":"a teacher","responseStyle":"short","memoryEnabled":true}`); rec.Code != http.StatusOK {
		t.Fatal(rec.Body)
	}
	if f.calls() != calls {
		t.Error("instructions sent back unchanged were checked again")
	}
}

// Strikes are counted on a refused save as on a refused message, and the one
// that reaches the limit disables the account.
func TestModerationCountsStrikesOnRefusedSaves(t *testing.T) {
	h, _ := newModerationMemoryHarness(t, `,"strikes":2`)

	rec := h.personalize(`{"aboutYou":"BLOCKME"}`)
	refused(t, rec)
	var warned blockedBody
	_ = json.Unmarshal(rec.Body.Bytes(), &warned)
	if left, _ := warned.Details["strikesLeft"].(float64); int(left) != 1 || warned.Details["windowHours"] != float64(24) {
		t.Errorf("the warning is %v", warned.Details)
	}
	// A refused message and a refused memory add to the same count.
	if rec := h.saveMemory("BLOCKME too"); !bannedByModeration(rec) {
		t.Fatalf("the save that reaches the limit: %d %s", rec.Code, rec.Body)
	}
	if h.count(`SELECT count(*) FROM users WHERE id = $1 AND disabled_at IS NOT NULL`, h.user.ID) != 1 {
		t.Error("the account was not disabled at the limit")
	}
	if len(h.memoryHashes()) != 0 {
		t.Error("the refused memory was saved")
	}
}

// Content saved while moderation was off is checked when a conversation takes
// its snapshot - once - and a text that fails stays out of the model's request.
func TestModerationChecksWhatWasSavedWhileOffAtSnapshotTime(t *testing.T) {
	h, f := newModerationMemoryHarness(t, "")
	if rec := h.patchModeration(`{"mode":"off"}`); rec.Code != http.StatusOK {
		t.Fatal(rec.Body)
	}
	for _, rec := range []*httptest.ResponseRecorder{
		h.personalize(`{"aboutYou":"ABOUT BLOCKME","responseStyle":"terse and kind"}`),
		h.saveMemory("lives in Lisbon"),
		h.saveMemory("MEM BLOCKME"),
	} {
		if rec.Code >= 300 {
			t.Fatalf("save with moderation off: %d %s", rec.Code, rec.Body)
		}
	}
	if _, _, aboutChecked, _ := h.instructions(); aboutChecked != nil {
		t.Error("a text saved while moderation was off is recorded as checked")
	}
	if rec := h.patchModeration(`{"mode":"openai"}`); rec.Code != http.StatusOK {
		t.Fatal(rec.Body)
	}

	// The old code would hand all of it to the model: with "hello" being the
	// only thing moderation saw.
	conv := h.newConv()
	calls := f.calls()
	if rec := h.sendIn(conv.ID, "hello"); rec.Code != http.StatusAccepted {
		t.Fatalf("send: %d %s", rec.Code, rec.Body)
	}
	if h.modelSaw("BLOCKME") != 0 {
		t.Error("blocked instructions or memory reached the model")
	}
	if h.modelSaw("terse and kind") == 0 || h.modelSaw("lives in Lisbon") == 0 {
		t.Error("the texts that passed were left out as well")
	}
	snap, ok := h.snapshotOf(conv.ID)
	if !ok || snap.About != "" || snap.Style != "terse and kind" || len(snap.Memories) != 1 || snap.Memories[0].Content != "lives in Lisbon" || snap.Memories[0].Ref != "m1" {
		t.Errorf("snapshot %+v", snap)
	}
	if h.eventsOf("snapshot") == 0 {
		t.Error("the texts left out were not logged")
	}
	// What passed is recorded as checked, what failed as refused.
	hashes := h.memoryHashes()
	if c := hashes["lives in Lisbon"][0]; c == nil || *c != memory.Hash("lives in Lisbon") {
		t.Errorf("a memory that passed: %v", hashes["lives in Lisbon"])
	}
	if b := hashes["MEM BLOCKME"][1]; b == nil || *b != memory.Hash("MEM BLOCKME") || hashes["MEM BLOCKME"][0] != nil {
		t.Errorf("a memory that failed: %v", hashes["MEM BLOCKME"])
	}

	// The next conversation asks about the message alone: nothing is re-checked,
	// and what was refused stays out without being asked about again.
	events := h.eventsOf("snapshot")
	calls = f.calls()
	second := h.newConv()
	if rec := h.sendIn(second.ID, "hello again"); rec.Code != http.StatusAccepted {
		t.Fatalf("second send: %d %s", rec.Code, rec.Body)
	}
	if f.calls() != calls+1 {
		t.Errorf("the second conversation made %d moderation calls, want 1 (its message)", f.calls()-calls)
	}
	if h.eventsOf("snapshot") != events {
		t.Error("a text already refused was logged again")
	}
	if snap, _ := h.snapshotOf(second.ID); snap.About != "" || len(snap.Memories) != 1 {
		t.Errorf("second snapshot %+v", snap)
	}

	// The snapshot is not rewritten by later edits: a conversation keeps its own.
	if h.modelSaw("MEM BLOCKME") != 0 {
		t.Error("blocked memory reached the model")
	}
}

// Text that was checked is not checked again: with nothing changed, a send is
// one moderation call - the message.
func TestModerationDoesNotRecheckWhatPassed(t *testing.T) {
	h, f := newModerationMemoryHarness(t, "")
	if rec := h.personalize(`{"aboutYou":"a nurse"}`); rec.Code != http.StatusOK {
		t.Fatal(rec.Body)
	}
	if rec := h.saveMemory("has two cats"); rec.Code != http.StatusCreated {
		t.Fatal(rec.Body)
	}
	for i := range 2 {
		calls := f.calls()
		if rec := h.sendIn(h.newConv().ID, fmt.Sprintf("hello %d", i)); rec.Code != http.StatusAccepted {
			t.Fatalf("send: %d %s", rec.Code, rec.Body)
		}
		if f.calls() != calls+1 {
			t.Fatalf("send %d made %d moderation calls, want 1", i, f.calls()-calls)
		}
	}
	// A second message in a conversation does not look at the snapshot at all.
	conv := h.newConv()
	h.sendIn(conv.ID, "first")
	calls := f.calls()
	h.sendIn(conv.ID, "second")
	if f.calls() != calls+1 {
		t.Errorf("a later message made %d calls", f.calls()-calls)
	}
	if h.eventsOf("snapshot") != 0 {
		t.Error("a snapshot of checked text logged an event")
	}
}

// What the model wrote is model output and is not moderated; neither is anything
// at snapshot time that moderation never looks at (an exempt administrator).
func TestModerationLeavesModelMemoriesAndExemptAdministrators(t *testing.T) {
	h, f := newModerationMemoryHarness(t, "")
	if _, err := memory.NewStore(h.pool).Add(context.Background(), h.user.ID, "MODEL BLOCKME", memory.By{Source: memory.SourceModel}); err != nil {
		t.Fatal(err)
	}
	calls := f.calls()
	conv := h.newConv()
	if rec := h.sendIn(conv.ID, "hello"); rec.Code != http.StatusAccepted {
		t.Fatal(rec.Body)
	}
	if f.calls() != calls+1 {
		t.Errorf("a model-written memory was moderated: %d calls", f.calls()-calls)
	}
	if snap, _ := h.snapshotOf(conv.ID); len(snap.Memories) != 1 {
		t.Errorf("snapshot %+v", snap)
	}

	// An administrator the settings exempt: saved without a check, so not
	// recorded as checked (should the exemption end, it is looked at then).
	if _, err := h.pool.Exec(context.Background(), `UPDATE users SET role = 'admin' WHERE id = $1`, h.user.ID); err != nil {
		t.Fatal(err)
	}
	h.user.Role = sqlc.UserRoleAdmin
	if rec := h.patchModeration(`{"exemptAdmins":true}`); rec.Code != http.StatusOK {
		t.Fatal(rec.Body)
	}
	calls = f.calls()
	if rec := h.saveMemory("admin note BLOCKME"); rec.Code != http.StatusCreated {
		t.Fatalf("an exempt administrator's memory: %d %s", rec.Code, rec.Body)
	}
	if f.calls() != calls {
		t.Error("an exempt administrator's memory was checked")
	}
	if hashes := h.memoryHashes()["admin note BLOCKME"]; hashes[0] != nil {
		t.Errorf("a memory nobody checked is recorded as checked: %v", hashes)
	}
}

// selectiveModeration is a moderation endpoint that flags BLOCKME and fails
// (500) on anything containing FAILME, so a snapshot check can fail while the
// message check does not.
func newSelectiveModeration(t *testing.T) *fakeModeration {
	t.Helper()
	f := &fakeModeration{t: t}
	var mu sync.Mutex
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		// As the real endpoint takes it: an array of strings (one result each)
		// or of multi-modal items (one result for all of them).
		var req struct {
			Input []json.RawMessage `json:"input"`
		}
		_ = json.Unmarshal(raw, &req)
		var inputs []string
		plain := true
		for _, part := range req.Input {
			var text string
			if json.Unmarshal(part, &text) == nil {
				inputs = append(inputs, text)
				continue
			}
			var item struct {
				Text string `json:"text"`
			}
			_ = json.Unmarshal(part, &item)
			plain = false
			inputs = append(inputs, item.Text)
		}
		if !plain {
			inputs = []string{strings.Join(inputs, "")}
		}
		mu.Lock()
		f.texts = append(f.texts, []string{strings.Join(inputs, "")})
		mu.Unlock()
		results := make([]any, 0, len(inputs))
		for _, text := range inputs {
			if strings.Contains(text, "FAILME") {
				http.Error(w, "unavailable", http.StatusInternalServerError)
				return
			}
			categories := map[string]bool{}
			scores := map[string]float64{}
			for _, c := range moderationCategories {
				categories[string(c)] = c == "violence/graphic" && strings.Contains(text, "BLOCKME")
				scores[string(c)] = 0
			}
			results = append(results, map[string]any{"categories": categories, "category_scores": scores})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"results": results})
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func TestModerationSnapshotWhenTheCheckCannotBeMade(t *testing.T) {
	f := newSelectiveModeration(t)
	h := newAskHarness(t, openAIWire, askThenText(openAIWire))
	resetModeration(t, h)
	h.server.usage = h.server.runner.Recorder()
	h.server.endUsers = NewEndUsers(h.pool)
	_, _ = h.pool.Exec(context.Background(), `DELETE FROM user_memories WHERE user_id = $1`, h.user.ID)
	t.Cleanup(func() {
		_, _ = h.pool.Exec(context.Background(), `DELETE FROM user_memories WHERE user_id = $1`, h.user.ID)
	})
	configure := func(mode, extra string) {
		body := fmt.Sprintf(`{"mode":%q,"openaiApiKey":"sk-mod-secret-1234","openaiBaseUrl":%q%s}`, mode, f.srv.URL+"/v1", extra)
		if rec := h.patchModeration(body); rec.Code != http.StatusOK {
			t.Fatalf("configure: %d %s", rec.Code, rec.Body)
		}
	}
	configure("off", "")
	if rec := h.saveMemory("a memory FAILME"); rec.Code != http.StatusCreated {
		t.Fatalf("save: %d %s", rec.Code, rec.Body)
	}

	// on_error block: the run does not start, nothing is written and no snapshot
	// is taken; once the check can be made the conversation goes on.
	configure("openai", `,"onError":"block"`)
	conv := h.newConv()
	rec := h.sendIn(conv.ID, "hello")
	var body blockedBody
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if rec.Code != http.StatusServiceUnavailable || body.Code != "moderation_unavailable" {
		t.Fatalf("on_error block: %d %s", rec.Code, rec.Body)
	}
	if _, ok := h.snapshotOf(conv.ID); ok {
		t.Error("a snapshot was taken without its texts being checked")
	}
	if h.count(`SELECT count(*) FROM runs WHERE conversation_id = $1`, conv.ID) != 0 ||
		h.count(`SELECT count(*) FROM messages WHERE conversation_id = $1`, conv.ID) != 0 {
		t.Error("a held-back start left rows")
	}
	if h.modelSaw("FAILME") != 0 {
		t.Error("the text reached the model")
	}

	// on_error allow: the text goes in, but is not recorded as checked, so the
	// next conversation asks again.
	configure("openai", `,"onError":"allow"`)
	allowed := h.newConv()
	if rec := h.sendIn(allowed.ID, "hello"); rec.Code != http.StatusAccepted {
		t.Fatalf("on_error allow: %d %s", rec.Code, rec.Body)
	}
	if snap, ok := h.snapshotOf(allowed.ID); !ok || len(snap.Memories) != 1 {
		t.Errorf("snapshot %+v", snap)
	}
	if hashes := h.memoryHashes()["a memory FAILME"]; hashes[0] != nil || hashes[1] != nil {
		t.Errorf("a text nobody could check is recorded: %v", hashes)
	}
}

// Blocked text found at snapshot time is logged but is not a strike: an
// account is not disabled over a "hello" for words it wrote long ago.
func TestModerationSnapshotRefusalIsNotAStrike(t *testing.T) {
	h, _ := newModerationMemoryHarness(t, `,"strikes":1`)
	if rec := h.patchModeration(`{"mode":"off"}`); rec.Code != http.StatusOK {
		t.Fatal(rec.Body)
	}
	if rec := h.saveMemory("old BLOCKME"); rec.Code != http.StatusCreated {
		t.Fatal(rec.Body)
	}
	if rec := h.personalize(`{"aboutYou":"old BLOCKME text"}`); rec.Code != http.StatusOK {
		t.Fatal(rec.Body)
	}
	if rec := h.patchModeration(`{"mode":"openai"}`); rec.Code != http.StatusOK {
		t.Fatal(rec.Body)
	}
	if rec := h.sendIn(h.newConv().ID, "hello"); rec.Code != http.StatusAccepted {
		t.Fatalf("send: %d %s", rec.Code, rec.Body)
	}
	if h.count(`SELECT count(*) FROM users WHERE id = $1 AND disabled_at IS NOT NULL`, h.user.ID) != 0 {
		t.Error("an account was disabled for old text left out of a snapshot")
	}
	if h.eventsOf("snapshot") == 0 {
		t.Error("nothing was logged")
	}
	// The same words sent now are a strike all the same.
	if rec := h.post("BLOCKME now"); !bannedByModeration(rec) {
		t.Fatalf("a refused message at strikes=1: %d %s", rec.Code, rec.Body)
	}
}
