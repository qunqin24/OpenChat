package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/qunqin24/openchat/apps/server/internal/db/dbtest"
	"github.com/qunqin24/openchat/apps/server/internal/db/sqlc"
)

// The list of recent conversations against a real database. Every test makes
// its own user, so none depends on what the tables already hold.

type recentFixture struct {
	t     *testing.T
	pool  *pgxpool.Pool
	store *Store
	user  uuid.UUID
	clock time.Time
}

func newRecentFixture(t *testing.T) *recentFixture {
	t.Helper()
	pool := dbtest.Pool(t, "memory")
	f := &recentFixture{t: t, pool: pool, store: NewStore(pool), clock: time.Now().Add(-48 * time.Hour)}
	f.user = f.newUser()
	// The administrator's switch is shared state of the database.
	_, _ = pool.Exec(context.Background(), "DELETE FROM app_settings WHERE key = $1", SettingChatHistory)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM app_settings WHERE key = $1", SettingChatHistory)
	})
	return f
}

func (f *recentFixture) exec(sql string, args ...any) {
	f.t.Helper()
	if _, err := f.pool.Exec(context.Background(), sql, args...); err != nil {
		f.t.Fatalf("%s: %v", sql, err)
	}
}

func (f *recentFixture) newUser() uuid.UUID {
	f.t.Helper()
	var id uuid.UUID
	tag := uuid.NewString()
	if err := f.pool.QueryRow(context.Background(),
		`INSERT INTO users (email, name, role) VALUES ($1, $2, 'user') RETURNING id`, tag+"@test.invalid", "recent-"+tag).Scan(&id); err != nil {
		f.t.Fatal(err)
	}
	f.t.Cleanup(func() { _, _ = f.pool.Exec(context.Background(), "DELETE FROM users WHERE id = $1", id) })
	return id
}

// conv makes a conversation of the fixture's user.
func (f *recentFixture) conv(title string) uuid.UUID {
	f.t.Helper()
	var id uuid.UUID
	if err := f.pool.QueryRow(context.Background(),
		`INSERT INTO conversations (user_id, title) VALUES ($1, $2) RETURNING id`, f.user, title).Scan(&id); err != nil {
		f.t.Fatal(err)
	}
	return id
}

// say adds a message, each a minute after the one before, in the order the
// test writes them across conversations too.
func (f *recentFixture) say(conv uuid.UUID, role, content string) {
	f.sayWith(conv, role, content, "complete", nil)
}

func (f *recentFixture) sayWith(conv uuid.UUID, role, content, status string, origin *string) {
	f.t.Helper()
	f.clock = f.clock.Add(time.Minute)
	f.exec(`INSERT INTO messages (conversation_id, role, content, status, origin, created_at)
	        VALUES ($1, $2::message_role, $3, $4::message_status, $5, $6)`, conv, role, content, status, origin, f.clock)
}

func (f *recentFixture) snapshot(conv uuid.UUID, vet Vet) Snapshot {
	f.t.Helper()
	snap, err := f.store.EnsureSnapshot(context.Background(), f.user, conv, vet)
	if err != nil {
		f.t.Fatal(err)
	}
	return snap
}

func ids(list []RecentChat) []uuid.UUID {
	out := make([]uuid.UUID, len(list))
	for i, c := range list {
		out[i] = c.ID
	}
	return out
}

func (f *recentFixture) listed(snap Snapshot, conv uuid.UUID) *RecentChat {
	for i := range snap.Recent {
		if snap.Recent[i].ID == conv {
			return &snap.Recent[i]
		}
	}
	return nil
}

func TestRecentListsWhatThePersonSaidNewestFirst(t *testing.T) {
	f := newRecentFixture(t)
	older, newer, current := f.conv("Older chat"), f.conv("Newer chat"), f.conv("New chat")
	f.say(older, "user", "first thing")
	f.say(older, "assistant", "ASSISTANT ANSWER")
	f.say(newer, "user", "second thing\nwith  two lines")
	f.say(newer, "assistant", "ASSISTANT ANSWER")

	snap := f.snapshot(current, nil)
	if len(snap.Recent) != 2 {
		t.Fatalf("listed %+v", snap.Recent)
	}
	if snap.Recent[0].ID != newer || snap.Recent[1].ID != older {
		t.Errorf("not newest first: %v", ids(snap.Recent))
	}
	if snap.Recent[0].Ref != "c1" || snap.Recent[1].Ref != "c2" {
		t.Errorf("refs %s %s", snap.Recent[0].Ref, snap.Recent[1].Ref)
	}
	if got := snap.Recent[0].Messages; len(got) != 1 || got[0] != "second thing with two lines" {
		t.Errorf("messages %q", got)
	}
	if strings.Contains(fmt.Sprint(snap.Recent), "ASSISTANT") {
		t.Error("an answer of the model is in the list")
	}
	// Taken once: a conversation started later does not change it.
	later := f.conv("Later")
	f.say(later, "user", "after the snapshot")
	if again := f.snapshot(current, nil); len(again.Recent) != 2 {
		t.Errorf("the snapshot changed: %+v", again.Recent)
	}
}

func TestRecentLeavesOutWhatIsNotTheirHistory(t *testing.T) {
	f := newRecentFixture(t)
	keep := f.conv("Kept")
	f.say(keep, "user", "listed")

	temp := f.conv("Temporary one")
	f.exec(`UPDATE conversations SET temporary = true WHERE id = $1`, temp)
	f.say(temp, "user", "temporary words")

	gone := f.conv("Deleted one")
	f.say(gone, "user", "deleted words")
	f.exec(`UPDATE conversations SET deleted_at = now() WHERE id = $1`, gone)

	parent := f.conv("Parent")
	f.say(parent, "user", "parent words")
	var sub uuid.UUID
	if err := f.pool.QueryRow(context.Background(),
		`INSERT INTO conversations (user_id, title, kind, parent_id) VALUES ($1, 'sub', 'subagent', $2) RETURNING id`, f.user, parent).Scan(&sub); err != nil {
		t.Fatal(err)
	}
	f.say(sub, "user", "subagent words")

	task := f.conv("A task")
	f.say(task, "user", "TASK INSTRUCTION")
	f.exec(`INSERT INTO scheduled_tasks (user_id, conversation_id, title, prompt, kind, run_at, time_zone, enabled)
	        VALUES ($1, $2, 'task', 'p', 'once', now() + interval '1 day', 'UTC', true)`, f.user, task)

	onlyBlocked := f.conv("Refused")
	f.sayWith(onlyBlocked, "user", "refused words", "blocked", nil)
	onlyReport := f.conv("Only a report")
	agent, subagent := "agent", "subagent"
	f.sayWith(onlyReport, "user", "report words", "complete", &subagent)
	f.sayWith(onlyReport, "user", "agent words", "complete", &agent)
	noWords := f.conv("Nothing typed")
	f.say(noWords, "user", "<skill name=\"pdf\"/>\n")
	onlyAnswers := f.conv("Only answers")
	f.say(onlyAnswers, "assistant", "answer words")

	mixed := f.conv("Mixed")
	f.say(mixed, "user", "visible one")
	f.sayWith(mixed, "user", "refused inside", "blocked", nil)
	f.sayWith(mixed, "user", "report inside", "complete", &subagent)

	other := f.newUser()
	var strangers uuid.UUID
	f.exec(`INSERT INTO conversations (user_id, title) VALUES ($1, 'stranger')`, other)
	_ = strangers
	f.exec(`INSERT INTO messages (conversation_id, role, content, status)
	        SELECT id, 'user', 'stranger words', 'complete' FROM conversations WHERE user_id = $1`, other)

	current := f.conv("New chat")
	f.say(current, "user", "the current conversation itself")

	snap := f.snapshot(current, nil)
	text := fmt.Sprintf("%+v", snap.Recent)
	for _, bad := range []string{"temporary words", "deleted words", "subagent words", "TASK INSTRUCTION", "refused words", "report words", "agent words",
		"answer words", "stranger", "refused inside", "report inside", "the current conversation"} {
		if strings.Contains(text, bad) {
			t.Errorf("%q is in the list: %s", bad, text)
		}
	}
	want := map[uuid.UUID]string{keep: "listed", parent: "parent words", mixed: "visible one"}
	if len(snap.Recent) != len(want) {
		t.Fatalf("listed %d, want %d: %s", len(snap.Recent), len(want), text)
	}
	for id, words := range want {
		if c := f.listed(snap, id); c == nil || len(c.Messages) != 1 || c.Messages[0] != words {
			t.Errorf("%s: %+v", words, c)
		}
	}
}

func TestRecentStripsTheComposerAndQuotes(t *testing.T) {
	f := newRecentFixture(t)
	c := f.conv("Edits")
	f.say(c, "user", "<deep-research/>\n<skill name=\"pdf\"/>\n<image-ref id=\"img_0123abcd\"/>\n<quote>\nTHE MODEL'S ANSWER\n</quote>\n\nwhy is that so?")
	f.say(c, "user", "<document-selection id=\"d1\" title=\"Doc\">\nTHE DOCUMENT\n</document-selection>\n\nmake it shorter")
	f.say(c, "user", "<skill name=\"pdf\"/>\n")
	snap := f.snapshot(f.conv("New chat"), nil)
	got := f.listed(snap, c)
	if got == nil || strings.Join(got.Messages, "|") != "why is that so?|make it shorter" {
		t.Fatalf("%+v", got)
	}
}

func TestRecentKeepsTheFirstAndLastFiveAndShortens(t *testing.T) {
	f := newRecentFixture(t)
	c := f.conv("Long one")
	for i := 1; i <= 10; i++ {
		f.say(c, "user", fmt.Sprintf("message %d %s", i, strings.Repeat("z", 300)))
	}
	snap := f.snapshot(f.conv("New chat"), nil)
	got := f.listed(snap, c)
	if got == nil || len(got.Messages) != 6 {
		t.Fatalf("%+v", got)
	}
	for i, n := range []int{1, 6, 7, 8, 9, 10} {
		if !strings.HasPrefix(got.Messages[i], fmt.Sprintf("message %d ", n)) {
			t.Errorf("entry %d is %q", i, got.Messages[i])
		}
		if len([]rune(got.Messages[i])) != recentMessageRunes || !strings.HasSuffix(got.Messages[i], "…") {
			t.Errorf("entry %d is not cut: %d runes", i, len([]rune(got.Messages[i])))
		}
	}
	if !got.At.Equal(f.clock) {
		t.Errorf("at %v, last message %v", got.At, f.clock)
	}
}

func TestRecentCapAndBudgetOnAFullAccount(t *testing.T) {
	f := newRecentFixture(t)
	var all []uuid.UUID
	for i := 0; i < MaxRecentChats+15; i++ {
		c := f.conv(fmt.Sprint("chat ", i))
		f.say(c, "user", fmt.Sprintf("topic %d %s", i, strings.Repeat("w", 100)))
		all = append(all, c)
	}
	snap := f.snapshot(f.conv("New chat"), nil)
	if len(snap.Recent) > MaxRecentChats || len(snap.Recent) < 20 {
		t.Fatalf("listed %d", len(snap.Recent))
	}
	if snap.Recent[0].ID != all[len(all)-1] {
		t.Error("the newest conversation is not first")
	}
	total := 0
	for _, c := range snap.Recent {
		total += c.cost()
	}
	if total > recentBudgetRunes {
		t.Errorf("budget exceeded: %d", total)
	}
}

func TestRecentSwitchesAndTemporaryConversations(t *testing.T) {
	f := newRecentFixture(t)
	c := f.conv("A chat")
	f.say(c, "user", "words")
	ctx := context.Background()

	on := func() bool {
		v, err := f.store.ChatHistoryOn(ctx, f.user)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	if !on() {
		t.Fatal("chat history is not on by default")
	}
	temp := f.conv("Temp")
	f.exec(`UPDATE conversations SET temporary = true WHERE id = $1`, temp)
	if ok, _ := f.store.ChatHistoryAvailable(ctx, f.user, temp); ok {
		t.Error("available in a temporary conversation")
	}
	if ok, _ := f.store.ChatHistoryAvailable(ctx, f.user, c); !ok {
		t.Error("not available in a normal conversation")
	}
	if snap := f.snapshot(temp, nil); len(snap.Recent) != 0 {
		t.Errorf("a temporary conversation got a list: %+v", snap.Recent)
	}

	// The person's switch.
	f.exec(`UPDATE users SET chat_history = false WHERE id = $1`, f.user)
	if on() {
		t.Error("on with the person's switch off")
	}
	if ok, _ := f.store.ChatHistoryAvailable(ctx, f.user, c); ok {
		t.Error("available with the person's switch off")
	}
	off := f.conv("Started while off")
	if snap := f.snapshot(off, nil); len(snap.Recent) != 0 {
		t.Errorf("listed with the person's switch off: %+v", snap.Recent)
	}
	f.exec(`UPDATE users SET chat_history = true WHERE id = $1`, f.user)

	// The administrator's.
	f.exec(`INSERT INTO app_settings (key, value) VALUES ($1, 'false'::jsonb) ON CONFLICT (key) DO UPDATE SET value = 'false'::jsonb`, SettingChatHistory)
	if on() {
		t.Error("on with the administrator's switch off")
	}
	if g, _ := f.store.ChatHistoryGlobalOn(ctx); g {
		t.Error("global switch reads on")
	}
	off2 := f.conv("Started while globally off")
	if snap := f.snapshot(off2, nil); len(snap.Recent) != 0 {
		t.Errorf("listed with the administrator's switch off: %+v", snap.Recent)
	}
	f.exec(`DELETE FROM app_settings WHERE key = $1`, SettingChatHistory)
	if !on() {
		t.Error("not on again")
	}
	// The snapshot of a conversation does not change when the switch flips: only
	// what is rendered does (Prompt's Recent option).
	started := f.conv("Started on")
	snap := f.snapshot(started, nil)
	if len(snap.Recent) == 0 {
		t.Fatal("no list while on")
	}
	f.exec(`UPDATE users SET chat_history = false WHERE id = $1`, f.user)
	if again := f.snapshot(started, nil); len(again.Recent) != len(snap.Recent) {
		t.Error("the stored list changed with the switch")
	}
}

func TestRecentTitleRules(t *testing.T) {
	f := newRecentFixture(t)
	def := f.conv(DefaultTitle)
	generated := f.conv("Generated title")
	f.exec(`UPDATE conversations SET title_checked_hash = $2 WHERE id = $1`, generated, Hash("Generated title"))
	renamed := f.conv("Renamed by the person")
	refusedBefore := f.conv("Refused earlier")
	f.exec(`UPDATE conversations SET title_blocked_hash = $2 WHERE id = $1`, refusedBefore, Hash("Refused earlier"))
	staleCheck := f.conv("Edited after the check")
	f.exec(`UPDATE conversations SET title_checked_hash = $2 WHERE id = $1`, staleCheck, Hash("What it was"))
	for _, c := range []uuid.UUID{def, generated, renamed, refusedBefore, staleCheck} {
		f.say(c, "user", "words of "+c.String())
	}

	// Without a vet nothing unchecked is shown; what was recorded is respected.
	snap := f.snapshot(f.conv("New chat"), nil)
	titles := map[uuid.UUID]string{}
	for _, c := range snap.Recent {
		titles[c.ID] = c.Title
	}
	if len(titles) != 5 {
		t.Fatalf("listed %d", len(titles))
	}
	if titles[def] != "" || titles[generated] != "Generated title" || titles[renamed] != "" || titles[refusedBefore] != "" || titles[staleCheck] != "" {
		t.Errorf("without a vet: %v", titles)
	}

	// With one, the unchecked ones are asked about, in one call; a refusal is
	// recorded and the entry stays without its title.
	var asked [][]Piece
	vet := func(_ context.Context, pieces []Piece) ([]Verdict, error) {
		asked = append(asked, pieces)
		out := make([]Verdict, len(pieces))
		for i, p := range pieces {
			out[i] = Verdict{Checked: true, Blocked: strings.Contains(p.Text, "Edited")}
		}
		return out, nil
	}
	snap = f.snapshot(f.conv("New chat 2"), vet)
	if len(asked) != 1 || len(asked[0]) != 2 {
		t.Fatalf("asked %+v", asked)
	}
	for _, p := range asked[0] {
		if p.Kind != PieceTitle || (p.Text != "Renamed by the person" && p.Text != "Edited after the check") {
			t.Errorf("asked about %+v", p)
		}
	}
	titles = map[uuid.UUID]string{}
	for _, c := range snap.Recent {
		titles[c.ID] = c.Title
	}
	if len(titles) != 5 || titles[renamed] != "Renamed by the person" || titles[generated] != "Generated title" ||
		titles[staleCheck] != "" || titles[refusedBefore] != "" || titles[def] != "" {
		t.Errorf("with a vet: %v", titles)
	}
	if _, ok, _ := f.store.TitleChecked(context.Background(), f.user, renamed); !ok {
		t.Error("the passed title is not recorded as checked")
	}
	if _, ok, _ := f.store.TitleChecked(context.Background(), f.user, staleCheck); ok {
		t.Error("the refused title counts as checked")
	}
	if _, ok, _ := f.store.TitleChecked(context.Background(), f.user, def); ok {
		t.Error("the default title counts as checked")
	}
	if title, ok, _ := f.store.TitleChecked(context.Background(), f.user, generated); !ok || title != "Generated title" {
		t.Errorf("generated: %q %v", title, ok)
	}
	// A third snapshot asks about none of them again.
	asked = nil
	f.snapshot(f.conv("New chat 3"), vet)
	if len(asked) != 0 {
		t.Errorf("asked again: %+v", asked)
	}

	// A person's title that is renamed again is unchecked again.
	f.exec(`UPDATE conversations SET title = 'Renamed twice' WHERE id = $1`, renamed)
	f.snapshot(f.conv("New chat 4"), vet)
	if len(asked) != 1 || asked[0][0].Text != "Renamed twice" {
		t.Errorf("asked %+v", asked)
	}
	// Someone else's conversation is not found.
	if _, _, err := f.store.TitleChecked(context.Background(), f.newUser(), renamed); err == nil {
		t.Error("another person's title was read")
	}
}

func TestRecentUnverifiedVetKeepsTitlesButRecordsNothing(t *testing.T) {
	// Moderation off, or the person exempt: the title goes in, but is not recorded
	// as checked, so it is asked about when moderation is on.
	f := newRecentFixture(t)
	c := f.conv("Typed by the person")
	f.say(c, "user", "words")
	vet := func(_ context.Context, pieces []Piece) ([]Verdict, error) { return make([]Verdict, len(pieces)), nil }
	snap := f.snapshot(f.conv("New chat"), vet)
	if got := f.listed(snap, c); got == nil || got.Title != "Typed by the person" {
		t.Errorf("%+v", got)
	}
	if _, ok, _ := f.store.TitleChecked(context.Background(), f.user, c); ok {
		t.Error("recorded as checked though nothing looked at it")
	}
}

func TestResolveRecentRefs(t *testing.T) {
	f := newRecentFixture(t)
	a, b := f.conv("A"), f.conv("B")
	f.say(a, "user", "one")
	f.say(b, "user", "two")
	current := f.conv("New chat")
	snap := f.snapshot(current, nil)
	ctx := context.Background()
	for _, c := range snap.Recent {
		got, err := f.store.ResolveRecent(ctx, f.user, current, strings.ToUpper(c.Ref))
		if err != nil || got != c.ID {
			t.Errorf("%s: %v %v", c.Ref, got, err)
		}
	}
	if _, err := f.store.ResolveRecent(ctx, f.user, current, "c9"); err != ErrUnknownRef {
		t.Errorf("c9: %v", err)
	}
	if _, err := f.store.ResolveRecent(ctx, f.user, current, "m1"); err != ErrUnknownRef {
		t.Errorf("m1: %v", err)
	}
	if _, err := f.store.ResolveRecent(ctx, f.newUser(), current, "c1"); err != ErrUnknownRef {
		t.Errorf("another person's conversation: %v", err)
	}
}

func TestRemoveFromRecentChatsKeepsTheOthersAndTheirRefs(t *testing.T) {
	f := newRecentFixture(t)
	var chats []uuid.UUID
	for i := 0; i < 6; i++ {
		c := f.conv(fmt.Sprint("chat ", i))
		f.say(c, "user", fmt.Sprint("words ", i))
		chats = append(chats, c)
	}
	one, two := f.conv("holder one"), f.conv("holder two")
	snap := f.snapshot(one, nil)
	f.snapshot(two, nil)
	if len(snap.Recent) < 6 {
		t.Fatalf("setup: %d", len(snap.Recent))
	}

	// A stranger's snapshot that happens to list the same ids is not touched.
	stranger := f.newUser()
	var strangerConv uuid.UUID
	raw, _ := json.Marshal(Snapshot{Recent: snap.Recent})
	if err := f.pool.QueryRow(context.Background(),
		`INSERT INTO conversations (user_id, title, memory_snapshot) VALUES ($1, 's', $2::jsonb) RETURNING id`, stranger, raw).Scan(&strangerConv); err != nil {
		t.Fatal(err)
	}

	gone := chats[1]
	if err := f.store.q.RemoveFromRecentChats(context.Background(), removeParams(f.user, gone)); err != nil {
		t.Fatal(err)
	}
	for _, holder := range []uuid.UUID{one, two} {
		after := f.snapshot(holder, nil)
		if len(after.Recent) != len(snap.Recent)-1 || f.listed(after, gone) != nil {
			t.Fatalf("holder %s: %+v", holder, after.Recent)
		}
		for _, before := range snap.Recent {
			if before.ID == gone {
				continue
			}
			kept := f.listed(after, before.ID)
			if kept == nil || kept.Ref != before.Ref || kept.Title != before.Title || strings.Join(kept.Messages, "|") != strings.Join(before.Messages, "|") {
				t.Errorf("entry %s changed: %+v -> %+v", before.Ref, before, kept)
			}
		}
	}
	var n int
	if err := f.pool.QueryRow(context.Background(), `SELECT jsonb_array_length(memory_snapshot->'recent') FROM conversations WHERE id = $1`, strangerConv).Scan(&n); err != nil || n != len(snap.Recent) {
		t.Errorf("another person's snapshot: %d %v", n, err)
	}
	// Removing the rest leaves a snapshot that still decodes and renders nothing.
	all := ids(snap.Recent)
	if err := f.store.q.RemoveFromRecentChats(context.Background(), removeParams(f.user, all...)); err != nil {
		t.Fatal(err)
	}
	empty := f.snapshot(one, nil)
	if len(empty.Recent) != 0 || empty.Prompt(PromptOptions{Recent: true}) != "" {
		t.Errorf("%+v", empty.Recent)
	}
}

func removeParams(user uuid.UUID, gone ...uuid.UUID) sqlc.RemoveFromRecentChatsParams {
	return sqlc.RemoveFromRecentChatsParams{UserID: &user, Ids: gone}
}

// A list is read, moderation is asked, and only then is the snapshot saved. A
// conversation deleted in between was not in any saved list for the deletion
// to remove it from, so the save leaves it out itself.
func TestSaveSnapshotDropsWhatWasDeletedSinceTheListWasRead(t *testing.T) {
	f := newRecentFixture(t)
	ctx := context.Background()
	kept, gone, holder := f.conv("kept"), f.conv("gone"), f.conv("holder")
	f.say(kept, "user", "still here")
	f.say(gone, "user", "about to go")

	cands, err := f.store.recentCandidates(ctx, f.user, holder)
	if err != nil {
		t.Fatal(err)
	}
	list := recentList(cands)
	if len(list) != 2 || list[0].ID != gone || list[1].ID != kept {
		t.Fatalf("read %+v", list)
	}
	f.exec(`UPDATE conversations SET deleted_at = now() WHERE id = $1`, gone)

	raw, err := f.store.saveSnapshot(ctx, f.user, holder, Snapshot{Recent: list})
	if err != nil {
		t.Fatal(err)
	}
	snap, err := decodeSnapshot(raw)
	if err != nil {
		t.Fatal(err)
	}
	// The one that is left keeps the number it was given.
	if len(snap.Recent) != 1 || snap.Recent[0].ID != kept || snap.Recent[0].Ref != "c2" {
		t.Errorf("saved %+v", snap.Recent)
	}
}

// A message that quotes a long passage is read whole before the person's words
// are taken from it: cut short, it would lose the closing tag and the request
// after it, and the passage (someone else's text) would be listed as theirs.
func TestRecentReadsALongQuotedMessageWhole(t *testing.T) {
	f := newRecentFixture(t)
	conv, holder := f.conv("Translate"), f.conv("holder")
	passage := strings.Repeat("PASSAGE-TEXT 虚构的个人信息。", 1500) // well past the 20000 the query reads
	f.say(conv, "user", "<quote>\n"+passage+"\n</quote>\n\n请把上面这段翻译成英文")
	f.say(conv, "assistant", "Here it is.")

	snap := f.snapshot(holder, nil)
	listed := f.listed(snap, conv)
	if listed == nil {
		t.Fatalf("not listed: %+v", snap.Recent)
	}
	if len(listed.Messages) != 1 || listed.Messages[0] != "请把上面这段翻译成英文" {
		t.Errorf("listed %q", listed.Messages)
	}
}
