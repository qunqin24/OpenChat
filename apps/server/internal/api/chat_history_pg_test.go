package api

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/qunqin24/openchat/apps/server/internal/agent"
	"github.com/qunqin24/openchat/apps/server/internal/db/sqlc"
	"github.com/qunqin24/openchat/apps/server/internal/memory"
	"github.com/qunqin24/openchat/apps/server/internal/tools"
)

// The chat_history tool (docs/design/chat-history-plan.md, section 4) against a
// real database: whose conversations and which of their messages it can reach,
// and when it is offered. Ranking and merging are tested in package tools.

type historyFixture struct {
	*memHarness
	current sqlc.Conversation
	state   *agent.RunState
	clock   time.Time
}

func newHistoryFixture(t *testing.T) *historyFixture {
	t.Helper()
	h := newHistoryHarness(t)
	return &historyFixture{memHarness: h, current: h.conversation(false), state: &agent.RunState{}, clock: time.Now().Add(-72 * time.Hour)}
}

func (f *historyFixture) exec(sql string, args ...any) {
	f.t.Helper()
	if _, err := f.pool.Exec(context.Background(), sql, args...); err != nil {
		f.t.Fatalf("%s: %v", sql, err)
	}
}

// chat makes a conversation of a user (kind 'chat') and returns its id.
func (f *historyFixture) chat(user uuid.UUID, title string) uuid.UUID {
	f.t.Helper()
	var id uuid.UUID
	if err := f.pool.QueryRow(context.Background(),
		`INSERT INTO conversations (user_id, title) VALUES ($1, $2) RETURNING id`, user, title).Scan(&id); err != nil {
		f.t.Fatal(err)
	}
	return id
}

func (f *historyFixture) say(conv uuid.UUID, role, content string) {
	f.sayAt(conv, role, content, f.clock.Add(time.Minute), "complete", nil)
}

func (f *historyFixture) sayAt(conv uuid.UUID, role, content string, at time.Time, status string, origin *string) {
	f.t.Helper()
	if at.After(f.clock) {
		f.clock = at
	}
	f.exec(`INSERT INTO messages (conversation_id, role, content, status, origin, created_at)
	        VALUES ($1, $2::message_role, $3, $4::message_status, $5, $6)`, conv, role, content, status, origin, at)
}

func (f *historyFixture) tool() tools.ChatHistory {
	return tools.ChatHistory{Store: f.notes, Q: f.q}
}

// call runs the tool as the fixture's user from the current conversation; the
// run's state is shared between calls, as it is in a run.
func (f *historyFixture) call(args string) (agent.ToolResult, error) {
	return f.callAs(f.user.ID, f.current.ID, args)
}

func (f *historyFixture) callAs(user, conv uuid.UUID, args string) (agent.ToolResult, error) {
	f.t.Helper()
	ctx := agent.WithScope(context.Background(), agent.Scope{RunID: uuid.New(), UserID: user, ConversationID: conv, State: f.state})
	return f.tool().Execute(ctx, json.RawMessage(args))
}

func (f *historyFixture) mustCall(args string) (agent.ToolResult, tools.ChatHistoryDetails) {
	f.t.Helper()
	res, err := f.call(args)
	if err != nil {
		f.t.Fatalf("%s: %v", args, err)
	}
	raw, _ := json.Marshal(res.Details)
	var d tools.ChatHistoryDetails
	if err := json.Unmarshal(raw, &d); err != nil {
		f.t.Fatal(err)
	}
	return res, d
}

func hitIDs(d tools.ChatHistoryDetails) []uuid.UUID {
	out := make([]uuid.UUID, len(d.Hits))
	for i, h := range d.Hits {
		out[i] = h.ConversationID
	}
	return out
}

func hasID(ids []uuid.UUID, id uuid.UUID) bool {
	for _, x := range ids {
		if x == id {
			return true
		}
	}
	return false
}

func TestChatHistorySearchReachesOnlyTheUsersOwnSearchableMessages(t *testing.T) {
	f := newHistoryFixture(t)
	word := "quokka" + strings.ReplaceAll(uuid.NewString()[:8], "-", "")

	mine := f.chat(f.user.ID, "My plan")
	f.say(mine, "user", "we should plan the "+word+" trip")
	f.say(mine, "assistant", "ok, the "+word+" itinerary: day one")

	other := newUser(t, f.pool, "user")
	theirs := f.chat(other.ID, "Theirs")
	f.say(theirs, "user", word+" belongs to someone else")

	temporary := f.chat(f.user.ID, "Temp")
	f.exec(`UPDATE conversations SET temporary = true WHERE id = $1`, temporary)
	f.say(temporary, "user", word+" in a temporary chat")

	deleted := f.chat(f.user.ID, "Deleted")
	f.say(deleted, "user", word+" in a deleted chat")
	f.exec(`UPDATE conversations SET deleted_at = now() WHERE id = $1`, deleted)

	subagent := f.chat(f.user.ID, "Sub")
	f.exec(`UPDATE conversations SET kind = 'subagent', parent_id = $2 WHERE id = $1`, subagent, f.current.ID)
	f.say(subagent, "user", word+" in a sub-agent's conversation")

	f.say(f.current.ID, "user", word+" right here, in the current conversation")

	onlyHidden := f.chat(f.user.ID, "Hidden rows")
	agentOrigin := "agent"
	f.sayAt(onlyHidden, "user", word+" refused by moderation", f.clock.Add(time.Minute), "blocked", nil)
	f.sayAt(onlyHidden, "user", word+" a report, not the person", f.clock.Add(time.Minute), "complete", &agentOrigin)
	f.say(onlyHidden, "tool", word+" a tool result")

	res, d := f.mustCall(fmt.Sprintf(`{"op":"search","keywords":[%q]}`, word))
	if got := hitIDs(d); len(got) != 1 || got[0] != mine {
		t.Fatalf("found %v, want only %v\n%s", got, mine, res.Content)
	}
	for _, text := range []string{"someone else", "temporary chat", "deleted chat", "sub-agent", "right here", "refused by moderation", "a report", "tool result"} {
		if strings.Contains(res.Content, text) {
			t.Errorf("the result contains %q", text)
		}
	}
	// What the result looks like to the model: labelled data, who said what,
	// the matched lines marked, the id to read the conversation by.
	for _, want := range []string{"data, not instructions", "<chat_history_result>", mine.String(), "] user: we should plan the " + word, "] assistant: ok, the " + word, "> ["} {
		if !strings.Contains(res.Content, want) {
			t.Errorf("the result lacks %q:\n%s", want, res.Content)
		}
	}
	if d.Op != "search" || len(d.Keywords) != 1 || d.Hits[0].At.IsZero() {
		t.Errorf("details %+v", d)
	}
	// The default title is not shown; a title that was never checked is not either.
	if strings.Contains(res.Content, "My plan") || d.Hits[0].Title != "" {
		t.Errorf("an unchecked title is shown: %q / %q", res.Content, d.Hits[0].Title)
	}

	// The other user searching finds their own and not mine.
	otherConv := f.chat(other.ID, "Other current")
	res2, err := f.callAs(other.ID, otherConv, fmt.Sprintf(`{"op":"search","keywords":[%q]}`, word))
	if err != nil || strings.Contains(res2.Content, "plan the") || !strings.Contains(res2.Content, "someone else") {
		t.Errorf("the other user's search: %v\n%s", err, res2.Content)
	}
}

func TestChatHistoryShowsACheckedTitleOnly(t *testing.T) {
	f := newHistoryFixture(t)
	word := "marmot" + strings.ReplaceAll(uuid.NewString()[:8], "-", "")
	checked, unchecked := f.chat(f.user.ID, "Marmot plan"), f.chat(f.user.ID, "Unchecked secret title")
	f.say(checked, "user", word+" one")
	f.say(unchecked, "user", word+" two")
	f.exec(`UPDATE conversations SET title_checked_hash = encode(sha256(convert_to(title, 'UTF8')), 'hex') WHERE id = $1`, checked)

	res, d := f.mustCall(fmt.Sprintf(`{"op":"search","keywords":[%q]}`, word))
	if !strings.Contains(res.Content, "Marmot plan") || strings.Contains(res.Content, "Unchecked secret title") {
		t.Errorf("titles in the result:\n%s", res.Content)
	}
	for _, hit := range d.Hits {
		switch hit.ConversationID {
		case checked:
			if hit.Title != "Marmot plan" {
				t.Errorf("checked title missing in details: %+v", hit)
			}
		case unchecked:
			if hit.Title != "" {
				t.Errorf("unchecked title in details: %+v", hit)
			}
		}
	}
	res, d = f.mustCall(fmt.Sprintf(`{"op":"read","conversation":%q}`, unchecked))
	if strings.Contains(res.Content, "Unchecked secret title") || d.Title != "" {
		t.Error("a read shows an unchecked title")
	}
}

func TestChatHistorySearchDateRange(t *testing.T) {
	f := newHistoryFixture(t)
	word := "ibis" + strings.ReplaceAll(uuid.NewString()[:8], "-", "")
	day := func(y, m, d, hour int) time.Time { return time.Date(y, time.Month(m), d, hour, 0, 0, 0, time.Local) }
	early, mid, late := f.chat(f.user.ID, "a"), f.chat(f.user.ID, "b"), f.chat(f.user.ID, "c")
	f.sayAt(early, "user", word+" early", day(2025, 3, 5, 12), "complete", nil)
	f.sayAt(mid, "user", word+" mid start", day(2025, 4, 1, 0), "complete", nil)
	f.sayAt(late, "user", word+" late", day(2025, 4, 30, 23), "complete", nil)

	_, d := f.mustCall(fmt.Sprintf(`{"op":"search","keywords":[%q],"from":"2025-04-01","to":"2025-04-30"}`, word))
	ids := hitIDs(d)
	if len(ids) != 2 || !hasID(ids, mid) || !hasID(ids, late) {
		t.Errorf("both ends of the period should be included: %v", ids)
	}
	if d.From != "2025-04-01" || d.To != "2025-04-30" {
		t.Errorf("details %+v", d)
	}
	_, d = f.mustCall(fmt.Sprintf(`{"op":"search","keywords":[%q],"to":"2025-03-31","include_seen":true}`, word))
	if ids := hitIDs(d); len(ids) != 1 || ids[0] != early {
		t.Errorf("to only: %v", ids)
	}
	if res, d := f.mustCall(fmt.Sprintf(`{"op":"search","keywords":[%q],"from":"2030-01-01"}`, word)); len(d.Hits) != 0 || !strings.Contains(res.Content, "No messages") {
		t.Errorf("an empty period: %s", res.Content)
	}
	for _, bad := range []string{`"from":"yesterday"`, `"to":"2025-4-1"`, `"from":"2025-05-01","to":"2025-04-01"`} {
		if _, err := f.call(fmt.Sprintf(`{"op":"search","keywords":[%q],%s}`, word, bad)); err == nil {
			t.Errorf("accepted %s", bad)
		}
	}
}

func TestChatHistorySeenConversationsAreSkippedUnlessAsked(t *testing.T) {
	f := newHistoryFixture(t)
	word := "heron" + strings.ReplaceAll(uuid.NewString()[:8], "-", "")
	a, b := f.chat(f.user.ID, "a"), f.chat(f.user.ID, "b")
	f.say(a, "user", word+" first")
	f.say(b, "user", word+" second")

	_, d := f.mustCall(fmt.Sprintf(`{"op":"search","keywords":[%q]}`, word))
	if len(d.Hits) != 2 {
		t.Fatalf("first search: %v", hitIDs(d))
	}
	res, d := f.mustCall(fmt.Sprintf(`{"op":"search","keywords":[%q]}`, word))
	if len(d.Hits) != 0 || !strings.Contains(res.Content, "include_seen") {
		t.Errorf("seen conversations came back, or the model is not told why there are none:\n%s", res.Content)
	}
	if _, d = f.mustCall(fmt.Sprintf(`{"op":"search","keywords":[%q],"include_seen":true}`, word)); len(d.Hits) != 2 {
		t.Errorf("include_seen: %v", hitIDs(d))
	}
	// A conversation that is new to the run is found; the run state is the run's.
	c := f.chat(f.user.ID, "c")
	f.say(c, "user", word+" third")
	if _, d = f.mustCall(fmt.Sprintf(`{"op":"search","keywords":[%q]}`, word)); len(d.Hits) != 1 || d.Hits[0].ConversationID != c {
		t.Errorf("only the new conversation: %v", hitIDs(d))
	}
	f.state = &agent.RunState{}
	if _, d = f.mustCall(fmt.Sprintf(`{"op":"search","keywords":[%q]}`, word)); len(d.Hits) != 3 {
		t.Errorf("a new run starts with nothing seen: %v", hitIDs(d))
	}
}

func TestChatHistorySearchGivesContextAndMergesNeighbours(t *testing.T) {
	f := newHistoryFixture(t)
	word := "lynx" + strings.ReplaceAll(uuid.NewString()[:8], "-", "")
	conv := f.chat(f.user.ID, "long")
	for i := 1; i <= 14; i++ {
		role := "user"
		if i%2 == 0 {
			role = "assistant"
		}
		text := fmt.Sprintf("filler line %02d", i)
		if i == 5 || i == 7 {
			text = fmt.Sprintf("%s mention %02d %s", word, i, strings.Repeat("x", 700))
		}
		f.say(conv, role, text)
	}
	res, d := f.mustCall(fmt.Sprintf(`{"op":"search","keywords":[%q]}`, word))
	if len(d.Hits) != 1 {
		t.Fatalf("hits %v", hitIDs(d))
	}
	// Two matches two places apart are one stretch: lines 3 to 9, each once.
	for i := 3; i <= 9; i++ {
		want := fmt.Sprintf("filler line %02d", i)
		if i == 5 || i == 7 {
			want = fmt.Sprintf("mention %02d", i)
		}
		if n := strings.Count(res.Content, want); n != 1 {
			t.Errorf("%q appears %d times", want, n)
		}
	}
	for _, out := range []string{"filler line 02", "filler line 10"} {
		if strings.Contains(res.Content, out) {
			t.Errorf("%q is outside the context", out)
		}
	}
	if strings.Count(res.Content, "Conversation "+conv.String()) != 1 {
		t.Error("the merged stretch is not one block")
	}
	// Each message is cut to 600 characters.
	for _, line := range strings.Split(res.Content, "\n") {
		if strings.Contains(line, "mention") && len([]rune(line)) > 600+60 {
			t.Errorf("a message line is %d characters", len([]rune(line)))
		}
	}
}

func TestChatHistoryReadByRefAndIdWithOffset(t *testing.T) {
	f := newHistoryFixture(t)
	target := f.chat(f.user.ID, "Target")
	for i := 1; i <= 30; i++ {
		role := "user"
		if i%2 == 0 {
			role = "assistant"
		}
		f.say(target, role, fmt.Sprintf("message %02d %s", i, strings.Repeat("y", 1990)))
	}
	f.say(target, "user", "<skill name=\"x\"/>\n")
	// The snapshot of the current conversation lists it as c1.
	if _, err := f.notes.EnsureSnapshot(context.Background(), f.user.ID, f.current.ID, nil); err != nil {
		t.Fatal(err)
	}
	snap := f.snapshotOf(f.current.ID)
	if len(snap.Recent) == 0 || snap.Recent[0].ID != target {
		t.Fatalf("snapshot %+v", snap.Recent)
	}
	ref := snap.Recent[0].Ref

	res, d := f.mustCall(fmt.Sprintf(`{"op":"read","conversation":%q}`, ref))
	if d.Op != "read" || d.ConversationID == nil || *d.ConversationID != target {
		t.Errorf("details %+v", d)
	}
	if !strings.Contains(res.Content, "message 01 ") || !strings.Contains(res.Content, "#1 [") {
		t.Errorf("the first message is missing:\n%.300s", res.Content)
	}
	if size := len([]rune(res.Content)); size > 12000+1500 {
		t.Errorf("a read returned %d characters", size)
	}
	if !strings.Contains(res.Content, "more. To continue, call op=read again with offset=") {
		t.Fatalf("no pointer to the next part:\n...%s", res.Content[len(res.Content)-200:])
	}
	// Follow the pointer, by id this time, to the end; every message once, in order.
	seen := map[int]int{}
	collect := func(content string) {
		for i := 1; i <= 30; i++ {
			seen[i] += strings.Count(content, fmt.Sprintf("message %02d ", i))
		}
	}
	collect(res.Content)
	offset := nextOffset(t, res.Content)
	for pages := 0; pages < 10 && offset >= 0; pages++ {
		res, _ = f.mustCall(fmt.Sprintf(`{"op":"read","conversation":%q,"offset":%d}`, target, offset))
		collect(res.Content)
		offset = nextOffset(t, res.Content)
		if offset < 0 && !strings.Contains(res.Content, "End of the conversation") {
			t.Error("the last part does not say it is the end")
		}
	}
	for i := 1; i <= 30; i++ {
		if seen[i] != 1 {
			t.Errorf("message %d was returned %d times", i, seen[i])
		}
	}
	// The message that is only the composer's line is not shown but still counts
	// toward the offset.
	if strings.Contains(res.Content, "<skill") {
		t.Error("the composer's line is shown")
	}
	if _, err := f.call(fmt.Sprintf(`{"op":"read","conversation":%q,"offset":500}`, target)); err == nil {
		t.Error("an offset past the end was accepted")
	}
	if _, err := f.call(fmt.Sprintf(`{"op":"read","conversation":%q,"offset":-1}`, target)); err == nil {
		t.Error("a negative offset was accepted")
	}
}

func nextOffset(t *testing.T, content string) int {
	t.Helper()
	const marker = "call op=read again with offset="
	i := strings.Index(content, marker)
	if i < 0 {
		return -1
	}
	var n int
	if _, err := fmt.Sscanf(content[i+len(marker):], "%d", &n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestChatHistoryReadRefusesWhatIsNotTheUsersToRead(t *testing.T) {
	f := newHistoryFixture(t)
	other := newUser(t, f.pool, "user")
	theirs := f.chat(other.ID, "Theirs")
	f.say(theirs, "user", "private words")
	temporary := f.chat(f.user.ID, "Temp")
	f.exec(`UPDATE conversations SET temporary = true WHERE id = $1`, temporary)
	f.say(temporary, "user", "temp words")
	deleted := f.chat(f.user.ID, "Del")
	f.say(deleted, "user", "deleted words")
	f.exec(`UPDATE conversations SET deleted_at = now() WHERE id = $1`, deleted)
	sub := f.chat(f.user.ID, "Sub")
	f.exec(`UPDATE conversations SET kind = 'subagent', parent_id = $2 WHERE id = $1`, sub, f.current.ID)
	f.say(sub, "user", "sub words")
	f.say(f.current.ID, "user", "current words")

	for name, id := range map[string]uuid.UUID{"someone else's": theirs, "temporary": temporary, "deleted": deleted, "sub-agent": sub, "the current": f.current.ID, "unknown": uuid.New()} {
		res, err := f.call(fmt.Sprintf(`{"op":"read","conversation":%q}`, id))
		var te *agent.ToolError
		if err == nil || !errorsAs(err, &te) {
			t.Errorf("%s conversation: err %v, content %q", name, err, res.Content)
		}
		if strings.Contains(res.Content, "words") {
			t.Errorf("%s conversation was read", name)
		}
	}
	// A reference the snapshot does not hold, and nonsense.
	for _, bad := range []string{"c9", "C42", "not-an-id", ""} {
		if _, err := f.call(fmt.Sprintf(`{"op":"read","conversation":%q}`, bad)); err == nil {
			t.Errorf("read %q", bad)
		}
	}
	// The other user's snapshot refs mean nothing here, and mine are not theirs.
	otherCurrent := f.chat(other.ID, "x")
	if _, err := f.callAs(other.ID, otherCurrent, `{"op":"read","conversation":"c1"}`); err == nil {
		t.Error("a ref with no snapshot resolved")
	}
	if _, err := f.callAs(other.ID, otherCurrent, fmt.Sprintf(`{"op":"read","conversation":%q}`, uuid.New())); err == nil {
		t.Error("an unknown id resolved")
	}
	// Bad operations and missing keywords are errors the model can read.
	for _, bad := range []string{`{"op":"delete"}`, `{}`, `{"op":"search"}`, `{"op":"search","keywords":[]}`, `{"op":"search","keywords":["a","b","c","d","e","f","g"]}`} {
		if _, err := f.call(bad); err == nil {
			t.Errorf("accepted %s", bad)
		}
	}
}

// The tool is offered where earlier conversations may be used, and nowhere else.
func TestChatHistoryIsOfferedOnlyWhereItMayBeUsed(t *testing.T) {
	f := newHistoryFixture(t)
	source := NewToolSource(f.pool)
	offered := func(scope agent.Scope) bool {
		t.Helper()
		scope.RunID, scope.UserID = uuid.New(), f.user.ID
		scope.State = &agent.RunState{}
		for _, tool := range source(agent.WithScope(context.Background(), scope)) {
			if tool.Definition().Name == tools.ToolChatHistory {
				return true
			}
		}
		return false
	}
	if !offered(agent.Scope{ConversationID: f.current.ID}) {
		t.Fatal("not offered to an ordinary conversation")
	}
	if !offered(agent.Scope{ConversationID: f.current.ID, Unattended: true}) {
		t.Error("a scheduled task's run does not have it")
	}
	if parent := f.current.ID; offered(agent.Scope{ConversationID: f.chat(f.user.ID, "sub"), Parent: &parent}) {
		t.Error("offered to a sub-agent")
	}
	if offered(agent.Scope{ConversationID: f.current.ID, Research: true}) {
		t.Error("offered to a deep research run")
	}
	if offered(agent.Scope{ConversationID: f.conversation(true).ID}) {
		t.Error("offered in a temporary conversation")
	}

	f.exec(`UPDATE users SET chat_history = false WHERE id = $1`, f.user.ID)
	if offered(agent.Scope{ConversationID: f.current.ID}) {
		t.Error("offered with the user's switch off")
	}
	// The call itself refuses too, in case the switch was turned off mid-run.
	if _, err := f.call(`{"op":"search","keywords":["x"]}`); err == nil {
		t.Error("a call went through with the user's switch off")
	}
	f.exec(`UPDATE users SET chat_history = true WHERE id = $1`, f.user.ID)
	if !offered(agent.Scope{ConversationID: f.current.ID}) {
		t.Fatal("not offered again once the switch is back on")
	}
	f.exec(`INSERT INTO app_settings (key, value) VALUES ($1, 'false'::jsonb)
	        ON CONFLICT (key) DO UPDATE SET value = 'false'::jsonb`, memory.SettingChatHistory)
	if offered(agent.Scope{ConversationID: f.current.ID}) {
		t.Error("offered with the administrator's switch off")
	}
	if _, err := f.call(`{"op":"search","keywords":["x"]}`); err == nil {
		t.Error("a call went through with the administrator's switch off")
	}
	// A sub-agent's call is refused even if it got hold of the tool.
	parent := f.current.ID
	ctx := agent.WithScope(context.Background(), agent.Scope{RunID: uuid.New(), UserID: f.user.ID, ConversationID: f.chat(f.user.ID, "s2"), Parent: &parent})
	if _, err := f.tool().Execute(ctx, json.RawMessage(`{"op":"search","keywords":["x"]}`)); err == nil {
		t.Error("a sub-agent's call went through")
	}
}

// The prompt promises the tool's section only when the tool is offered.
func TestChatHistoryPromptSection(t *testing.T) {
	f := newHistoryFixture(t)
	prompt := NewSystemPrompt(f.pool)
	ctx := agent.WithScope(context.Background(), agent.Scope{RunID: uuid.New(), UserID: f.user.ID, ConversationID: f.current.ID, State: &agent.RunState{}})
	with := prompt(ctx, time.Now(), NewToolSource(f.pool)(ctx))
	mustContain(t, "prompt with the tool", with, "## Earlier conversations", "chat_history", "most recent occurrence", "never guess")
	f.exec(`UPDATE users SET chat_history = false WHERE id = $1`, f.user.ID)
	without := prompt(ctx, time.Now(), NewToolSource(f.pool)(ctx))
	mustNotContain(t, "prompt without the tool", without, "## Earlier conversations", "chat_history")
}

// A message longer than a whole read is read in parts, each read saying where
// the next starts inside it; every character comes once, and what lies at its
// very end is reached.
func TestChatHistoryReadsALongMessageInParts(t *testing.T) {
	f := newHistoryFixture(t)
	target := f.chat(f.user.ID, "Plan")
	answer := strings.Repeat("第一步，准备材料。", 3500) + "应急口令是 ZEBRA-7741。"
	f.say(target, "user", "给我一个很长的计划")
	f.say(target, "assistant", answer)
	f.say(target, "user", "谢谢，收到")

	var shown, pieces strings.Builder
	offset, char := 0, 0
	for reads := 0; ; reads++ {
		if reads > 10 {
			t.Fatal("never reached the end")
		}
		res, _ := f.mustCall(fmt.Sprintf(`{"op":"read","conversation":%q,"offset":%d,"char":%d}`, target, offset, char))
		if size := len([]rune(res.Content)); size > 12000+1500 {
			t.Errorf("a read returned %d characters", size)
		}
		shown.WriteString(res.Content)
		pieces.WriteString(pieceOf(res.Content, 2))
		if strings.Contains(res.Content, "End of the conversation") {
			break
		}
		offset, char = nextOffset(t, res.Content), nextChar(res.Content)
	}
	all := shown.String()
	if strings.Count(all, "ZEBRA-7741") != 1 {
		t.Errorf("the end of the long answer was read %d times", strings.Count(all, "ZEBRA-7741"))
	}
	// Put back together, the parts are the answer, to the character.
	if got := pieces.String(); got != answer {
		t.Errorf("the parts make %d characters, the answer has %d", len([]rune(got)), len([]rune(answer)))
	}
	if strings.Count(all, "谢谢，收到") != 1 || strings.Count(all, "给我一个很长的计划") != 1 {
		t.Error("the messages around it were not read once each")
	}
	if _, err := f.call(fmt.Sprintf(`{"op":"read","conversation":%q,"offset":1,"char":999999}`, target)); err == nil {
		t.Error("a char past the end of the message was accepted")
	}
}

// What a person asked about a long quoted passage is their words in a search
// and a read, not the passage (which was cut short where the queries stop).
func TestChatHistoryShowsTheWordsAfterALongQuote(t *testing.T) {
	f := newHistoryFixture(t)
	target := f.chat(f.user.ID, "Quote")
	passage := strings.Repeat("QUOTED-PASSAGE ", 2000)
	f.say(target, "user", "<quote>\n"+passage+"\n</quote>\n\n真正的请求：把它翻译成中文")
	f.say(target, "assistant", "译文如下，关键词 KESTREL")

	res, _ := f.mustCall(`{"op":"search","keywords":["KESTREL"]}`)
	if !strings.Contains(res.Content, "真正的请求：把它翻译成中文") || strings.Contains(res.Content, "QUOTED-PASSAGE") {
		t.Errorf("search context:\n%.600s", res.Content)
	}
	res, _ = f.mustCall(fmt.Sprintf(`{"op":"read","conversation":%q}`, target))
	if !strings.Contains(res.Content, "真正的请求：把它翻译成中文") || strings.Contains(res.Content, "QUOTED-PASSAGE") {
		t.Errorf("read:\n%.600s", res.Content)
	}
}

func nextChar(content string) int {
	const marker = " and char="
	i := strings.Index(content, marker)
	if i < 0 {
		return 0
	}
	var n int
	_, _ = fmt.Sscanf(content[i+len(marker):], "%d", &n)
	return n
}

// pieceOf is the text a read shows of message #n: after its line's "who: ", up
// to the next message's line or the note that ends the read.
func pieceOf(content string, n int) string {
	start := strings.Index(content, fmt.Sprintf("\n#%d [", n))
	if start < 0 {
		return ""
	}
	rest := content[start+1:]
	rest = rest[strings.Index(rest, ": ")+2:]
	end := len(rest)
	for _, stop := range []string{fmt.Sprintf("\n#%d [", n+1), "\n\n["} {
		if i := strings.Index(rest, stop); i >= 0 && i < end {
			end = i
		}
	}
	return rest[:end]
}
