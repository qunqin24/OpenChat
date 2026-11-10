package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/qunqin24/openchat/apps/server/internal/agent"
	"github.com/qunqin24/openchat/apps/server/internal/db/sqlc"
	"github.com/qunqin24/openchat/apps/server/internal/llm"
	"github.com/qunqin24/openchat/apps/server/internal/memory"
	"github.com/qunqin24/openchat/apps/server/internal/tools"
)

// The list of recent conversations (docs/design/chat-history-plan.md) through
// the handlers and the system prompt. What the store takes into the list is
// tested in package memory; here: the switches, the prompt, titles, and the
// removal on delete.

func newHistoryHarness(t *testing.T) *memHarness {
	t.Helper()
	h := newMemHarness(t, nil)
	clear := func() {
		_, _ = h.pool.Exec(context.Background(), "DELETE FROM app_settings WHERE key = $1", memory.SettingChatHistory)
	}
	clear()
	t.Cleanup(clear)
	return h
}

func (h *memHarness) snapshotOf(conv uuid.UUID) memory.Snapshot {
	h.t.Helper()
	var raw []byte
	if err := h.pool.QueryRow(context.Background(), `SELECT memory_snapshot FROM conversations WHERE id = $1`, conv).Scan(&raw); err != nil {
		h.t.Fatal(err)
	}
	var snap memory.Snapshot
	if raw != nil {
		if err := json.Unmarshal(raw, &snap); err != nil {
			h.t.Fatal(err)
		}
	}
	return snap
}

func (h *memHarness) deleteConv(id uuid.UUID) {
	h.t.Helper()
	rec := httptest.NewRecorder()
	h.server.DeleteConversation(rec, h.request(http.MethodDelete, ""), id)
	if rec.Code != http.StatusNoContent {
		h.t.Fatalf("delete: %d %s", rec.Code, rec.Body)
	}
}

func (h *memHarness) personalization() Personalization {
	h.t.Helper()
	rec := httptest.NewRecorder()
	h.server.GetPersonalization(rec, h.request(http.MethodGet, ""))
	var out Personalization
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &out) != nil {
		h.t.Fatalf("personalization: %d %s", rec.Code, rec.Body)
	}
	return out
}

// A new conversation starts with what was said in the earlier ones; the
// person's and the administrator's switches each take it away, and a switch
// flipped during a conversation takes effect at the next request without
// touching the snapshot.
func TestChatHistorySwitchesAndPrompt(t *testing.T) {
	h := newHistoryHarness(t)
	if p := h.personalization(); !p.ChatHistoryEnabled || !p.ChatHistoryAllowed {
		t.Fatalf("defaults: %+v", p)
	}
	earlier := h.conversation(false)
	h.send(earlier.ID, "MARKER-ALPHA how do I pickle plums")

	on := h.conversation(false)
	h.send(on.ID, "hello there")
	mustContain(t, "prompt", h.system(2), "## Recent conversations", "<recent_conversations>", "MARKER-ALPHA how do I pickle plums", "[c1] ")
	// The tool is offered to this conversation, so the prompt names it.
	mustContain(t, "prompt", h.system(2), "chat_history tool", "## Earlier conversations")
	if !offers(h.toolNames(2), tools.ToolChatHistory) {
		t.Errorf("the tool is not offered: %v", h.toolNames(2))
	}
	if got := h.snapshotOf(on.ID).Recent; len(got) == 0 || got[0].ID != earlier.ID {
		t.Fatalf("snapshot: %+v", got)
	}
	// The conversation itself and the current question are not in its own list.
	mustNotContain(t, "prompt", h.system(2), "hello there")

	// The person's switch, mid-conversation: rendered no more, snapshot unchanged.
	h.personalize(`{"chatHistoryEnabled":false}`)
	if p := h.personalization(); p.ChatHistoryEnabled {
		t.Fatalf("not saved: %+v", p)
	}
	h.send(on.ID, "and again")
	mustNotContain(t, "prompt after switching off", h.system(3), "MARKER-ALPHA", "Recent conversations", "chat_history")
	if offers(h.toolNames(3), tools.ToolChatHistory) {
		t.Error("the tool is still offered with the person's switch off")
	}
	if len(h.snapshotOf(on.ID).Recent) == 0 {
		t.Error("the snapshot lost its list when the switch was turned off")
	}
	// A new conversation with the switch off gets no list at all.
	off := h.conversation(false)
	h.send(off.ID, "hi")
	if len(h.snapshotOf(off.ID).Recent) != 0 {
		t.Error("a list was taken with the switch off")
	}
	mustNotContain(t, "prompt", h.system(4), "MARKER-ALPHA")

	// Back on: the older conversation's list is rendered again, from the same bytes.
	h.personalize(`{"chatHistoryEnabled":true}`)
	h.send(on.ID, "third")
	mustContain(t, "prompt", h.system(5), "MARKER-ALPHA")
	if h.system(2) != h.system(5) {
		t.Errorf("the prompt of one snapshot changed:\n%s\n---\n%s", h.system(2), h.system(5))
	}

	// The administrator's switch beats the person's.
	admin := newUser(t, h.pool, "admin")
	patch := func(body string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		h.server.UpdateSystemSettings(rec, h.as(admin, http.MethodPatch, body))
		if rec.Code != http.StatusOK {
			t.Fatalf("system settings: %d %s", rec.Code, rec.Body)
		}
		return rec
	}
	if rec := patch(`{"chatHistory":false}`); !strings.Contains(rec.Body.String(), `"chatHistory":false`) {
		t.Errorf("system settings: %s", rec.Body)
	}
	if p := h.personalization(); p.ChatHistoryAllowed || !p.ChatHistoryEnabled {
		t.Errorf("personalization: %+v", p)
	}
	h.send(on.ID, "fourth")
	mustNotContain(t, "prompt with the administrator's switch off", h.system(6), "MARKER-ALPHA")
	globally := h.conversation(false)
	h.send(globally.ID, "hi")
	if len(h.snapshotOf(globally.ID).Recent) != 0 {
		t.Error("a list was taken with the administrator's switch off")
	}
	if rec := patch(`{"chatHistory":true}`); !strings.Contains(rec.Body.String(), `"chatHistory":true`) {
		t.Errorf("system settings: %s", rec.Body)
	}
	if p := h.personalization(); !p.ChatHistoryAllowed {
		t.Errorf("personalization: %+v", p)
	}
}

func TestChatHistoryTemporaryConversations(t *testing.T) {
	h := newHistoryHarness(t)
	temp := h.conversation(true)
	h.send(temp.ID, "TEMP-WORDS secret")
	normal := h.conversation(false)
	h.send(normal.ID, "hi")
	mustNotContain(t, "prompt", h.system(2), "TEMP-WORDS")
	// And it has no list of its own.
	other := h.conversation(false)
	h.send(other.ID, "NORMAL-WORDS")
	again := h.conversation(true)
	h.send(again.ID, "hi")
	mustNotContain(t, "temporary prompt", h.system(4), "NORMAL-WORDS", "Recent conversations")
}

// The prompt names the tool exactly when the run is offered it.
func TestPersonalPromptMentionsTheHistoryToolWhenOffered(t *testing.T) {
	h := newHistoryHarness(t)
	earlier := h.conversation(false)
	h.send(earlier.ID, "MARKER-BETA")
	conv := h.conversation(false)
	if _, err := h.notes.EnsureSnapshot(context.Background(), h.user.ID, conv.ID, nil); err != nil {
		t.Fatal(err)
	}
	ctx := agent.WithScope(context.Background(), agent.Scope{UserID: h.user.ID, ConversationID: conv.ID})
	without := personalPrompt(ctx, h.notes, nil)
	with := personalPrompt(ctx, h.notes, []agent.Tool{fakeNamedTool{tools.ToolChatHistory}})
	mustContain(t, "prompt", without, "MARKER-BETA")
	mustNotContain(t, "prompt without the tool", without, "chat_history")
	mustContain(t, "prompt with the tool", with, "MARKER-BETA", "chat_history tool")
	if with == without {
		t.Error("offering the tool changed nothing")
	}
}

type fakeNamedTool struct{ name string }

func (f fakeNamedTool) Definition() llm.Tool { return llm.Tool{Name: f.name} }
func (fakeNamedTool) Replayable() bool       { return true }
func (fakeNamedTool) Execute(context.Context, json.RawMessage) (agent.ToolResult, error) {
	return agent.ToolResult{}, nil
}

// Deleting a conversation takes it out of the lists of the person's other
// conversations, in the same request; the other entries and their refs stay.
func TestDeleteRemovesTheConversationFromOtherSnapshots(t *testing.T) {
	h := newHistoryHarness(t)
	var chats []sqlc.Conversation
	for _, words := range []string{"WORDS-ONE", "WORDS-TWO", "WORDS-THREE"} {
		c := h.conversation(false)
		h.send(c.ID, words)
		chats = append(chats, c)
	}
	holderA, holderB := h.conversation(false), h.conversation(false)
	h.send(holderA.ID, "from holder a")
	h.send(holderB.ID, "from holder b")

	before := h.snapshotOf(holderB.ID)
	// holderB listed holderA, the three, and nothing of itself.
	if len(before.Recent) != 4 {
		t.Fatalf("holder b lists %d: %+v", len(before.Recent), before.Recent)
	}
	refOf := map[uuid.UUID]string{}
	for _, c := range before.Recent {
		refOf[c.ID] = c.Ref
	}
	// holder a was started first and numbers what it lists by itself.
	refsA := map[uuid.UUID]string{}
	for _, c := range h.snapshotOf(holderA.ID).Recent {
		refsA[c.ID] = c.Ref
	}

	// Somebody else's snapshot that lists the same id is left alone.
	stranger := newUser(t, h.pool, "user")
	var strangerConv uuid.UUID
	raw, _ := json.Marshal(memory.Snapshot{Recent: before.Recent})
	if err := h.pool.QueryRow(context.Background(),
		`INSERT INTO conversations (user_id, title, memory_snapshot) VALUES ($1, 'x', $2::jsonb) RETURNING id`, stranger.ID, raw).Scan(&strangerConv); err != nil {
		t.Fatal(err)
	}

	h.deleteConv(chats[1].ID)
	for holder, refs := range map[uuid.UUID]map[uuid.UUID]string{holderA.ID: refsA, holderB.ID: refOf} {
		snap := h.snapshotOf(holder)
		for _, c := range snap.Recent {
			if c.ID == chats[1].ID {
				t.Errorf("the deleted conversation is still in %s", holder)
			}
			if c.Ref != refs[c.ID] {
				t.Errorf("%s was renumbered: %s -> %s", c.ID, refs[c.ID], c.Ref)
			}
		}
	}
	if got := len(h.snapshotOf(holderB.ID).Recent); got != 3 {
		t.Errorf("holder b lists %d, want 3", got)
	}
	if got := len(h.snapshotOf(strangerConv).Recent); got != 4 {
		t.Errorf("another person's snapshot was changed: %d", got)
	}
	// The next request no longer shows the words, and still the others.
	h.send(holderB.ID, "next")
	last := h.requests()
	mustNotContain(t, "prompt after the delete", h.system(last), "WORDS-TWO")
	mustContain(t, "prompt after the delete", h.system(last), "WORDS-ONE", "WORDS-THREE")
	// A ref resolves only to a conversation still on the list.
	if _, err := h.notes.ResolveRecent(context.Background(), h.user.ID, holderB.ID, refOf[chats[1].ID]); err != memory.ErrUnknownRef {
		t.Errorf("a removed ref resolves: %v", err)
	}
	if id, err := h.notes.ResolveRecent(context.Background(), h.user.ID, holderB.ID, refOf[chats[0].ID]); err != nil || id != chats[0].ID {
		t.Errorf("a kept ref: %v %v", id, err)
	}

	// Deleting everything (DELETE /me/conversations) clears the lists as well.
	rec := httptest.NewRecorder()
	h.server.DeleteAllMyConversations(rec, h.request(http.MethodDelete, ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("delete all: %d %s", rec.Code, rec.Body)
	}
	for _, id := range []uuid.UUID{holderA.ID, holderB.ID, chats[0].ID} {
		if n := len(h.snapshotOf(id).Recent); n != 0 {
			t.Errorf("%s still lists %d", id, n)
		}
	}
	if n := len(h.snapshotOf(strangerConv).Recent); n != 4 {
		t.Errorf("another person's snapshot after delete all: %d", n)
	}
}

// A title the server wrote is recorded as checked when it is written; one the
// person typed is not, and is moderated when a list would carry it.
func TestGeneratedTitlesAreRecordedAsChecked(t *testing.T) {
	h := newHistoryHarness(t)
	conv := h.conversation(false)
	conv2, err := h.q.SetGeneratedTitle(context.Background(), sqlc.SetGeneratedTitleParams{
		ID: conv.ID, Title: "A generated title", TitleHash: memory.Hash("A generated title"), DefaultTitle: defaultTitle,
	})
	if err != nil {
		t.Fatal(err)
	}
	if conv2.TitleCheckedHash == nil || *conv2.TitleCheckedHash != memory.Hash("A generated title") {
		t.Fatalf("hash %v", conv2.TitleCheckedHash)
	}
	if title, ok, err := h.notes.TitleChecked(context.Background(), h.user.ID, conv.ID); err != nil || !ok || title != "A generated title" {
		t.Errorf("TitleChecked: %q %v %v", title, ok, err)
	}
	// Renaming it makes it unchecked.
	if _, err := h.q.UpdateConversationTitle(context.Background(), sqlc.UpdateConversationTitleParams{ID: conv.ID, UserID: &h.user.ID, Title: "Mine now"}); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := h.notes.TitleChecked(context.Background(), h.user.ID, conv.ID); ok {
		t.Error("a renamed title still counts as checked")
	}

	// End to end: the title the server writes after a run is checked.
	h.server.usage = h.server.runner.Recorder()
	h.server.endUsers = NewEndUsers(h.pool)
	named := h.conversation(false)
	h.send(named.ID, "name this conversation")
	run, err := h.q.GetRun(context.Background(), h.lastRun)
	if err != nil {
		t.Fatal(err)
	}
	h.server.titleConversation(run)
	title, ok, err := h.notes.TitleChecked(context.Background(), h.user.ID, named.ID)
	if err != nil || !ok || title == "" || title == defaultTitle {
		t.Errorf("the title written after a run is not recorded as checked: %q %v %v", title, ok, err)
	}
}

// A title the person typed is asked about together with the other texts of the
// snapshot, once; a refused one is left off its entry.
func TestRenamedTitlesAreModeratedAtSnapshotTime(t *testing.T) {
	h, f := newModerationMemoryHarness(t, "")
	_, _ = h.pool.Exec(context.Background(), "DELETE FROM app_settings WHERE key = $1", memory.SettingChatHistory)
	t.Cleanup(func() {
		_, _ = h.pool.Exec(context.Background(), "DELETE FROM app_settings WHERE key = $1", memory.SettingChatHistory)
	})

	fine, bad := h.newConv(), h.newConv()
	h.sendIn(fine.ID, "about plums")
	h.sendIn(bad.ID, "about pears")
	for id, title := range map[uuid.UUID]string{fine.ID: "Plum jam", bad.ID: "Pear BLOCKME"} {
		if _, err := h.q.UpdateConversationTitle(context.Background(), sqlc.UpdateConversationTitleParams{ID: id, UserID: &h.user.ID, Title: title}); err != nil {
			t.Fatal(err)
		}
	}

	// One call for the message, one for the snapshot (both titles in it).
	calls := f.calls()
	next := h.newConv()
	if rec := h.sendIn(next.ID, "hello"); rec.Code != http.StatusAccepted {
		t.Fatalf("send: %d %s", rec.Code, rec.Body)
	}
	titleOf := map[uuid.UUID]string{}
	for _, c := range func() []memory.RecentChat { s, _ := h.snapshotOf(next.ID); return s.Recent }() {
		titleOf[c.ID] = c.Title
	}
	if len(titleOf) != 2 || titleOf[fine.ID] != "Plum jam" || titleOf[bad.ID] != "" {
		t.Errorf("titles in the list: %v", titleOf)
	}
	if got := f.calls() - calls; got < 2 || got > 4 {
		t.Errorf("%d moderation calls for the send", got)
	}
	if h.modelSaw("Pear BLOCKME") != 0 {
		t.Error("a refused title reached the model")
	}
	if h.modelSaw("Plum jam") == 0 {
		t.Error("a title that passed is not in the prompt")
	}
	if h.eventsOf("snapshot") == 0 {
		t.Error("the refused title was not logged")
	}
	// Recorded: the next conversation asks about neither.
	calls = f.calls()
	if rec := h.sendIn(h.newConv().ID, "hello again"); rec.Code != http.StatusAccepted {
		t.Fatal(rec.Body)
	}
	if got := f.calls() - calls; got != 1 {
		t.Errorf("the next conversation made %d moderation calls, want 1 (its message)", got)
	}
}

// Deleting a conversation writes to the person's other conversations, the ones
// that list it. Conversations that list each other, deleted at the same time,
// would each hold their own row and wait for the others': the deletions queue
// behind the person's lock instead (LockRecentChats).
func TestConcurrentDeletesOfConversationsThatListEachOther(t *testing.T) {
	h := newHistoryHarness(t)
	var chats []sqlc.Conversation
	for i := 0; i < 8; i++ {
		c := h.conversation(false)
		h.send(c.ID, fmt.Sprintf("WORDS-%d", i))
		chats = append(chats, c)
	}
	// Each lists all the others, as conversations do once the older ones are
	// written in again.
	for _, c := range chats {
		var recent []memory.RecentChat
		for i, other := range chats {
			if other.ID != c.ID {
				recent = append(recent, memory.RecentChat{Ref: memory.RecentRef(i + 1), ID: other.ID, Messages: []string{"x"}})
			}
		}
		raw, _ := json.Marshal(memory.Snapshot{Recent: recent})
		if _, err := h.pool.Exec(context.Background(), `UPDATE conversations SET memory_snapshot = $2::jsonb WHERE id = $1`, c.ID, raw); err != nil {
			t.Fatal(err)
		}
	}

	codes := make([]int, len(chats))
	var wg sync.WaitGroup
	for i, c := range chats {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rec := httptest.NewRecorder()
			h.server.DeleteConversation(rec, h.request(http.MethodDelete, ""), c.ID)
			codes[i] = rec.Code
		}()
	}
	wg.Wait()
	for i, code := range codes {
		if code != http.StatusNoContent {
			t.Errorf("delete %d: %d", i, code)
		}
	}
}
