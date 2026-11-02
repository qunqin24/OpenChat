package api

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/qunqin24/openchat/apps/server/internal/db/sqlc"
	"github.com/qunqin24/openchat/apps/server/internal/skills"
	"github.com/qunqin24/openchat/apps/server/internal/tools"
)

// Skills end to end: the handlers, the snapshot, the system prompt and the
// skill tool, with a scripted model (memHarness). Site skills are shared state
// of the package's database, so each test names its own and removes them.

func skillTag() string { return strings.ReplaceAll(uuid.NewString()[:8], "-", "") }

func skillBody(name, description, body string, extra map[string]string) string {
	files := []SkillFile{{Path: "SKILL.md", Content: "---\nname: " + name + "\ndescription: " + description + "\n---\n" + body}}
	for path, content := range extra {
		files = append(files, SkillFile{Path: path, Content: content})
	}
	raw, _ := json.Marshal(SkillRequest{Files: files})
	return string(raw)
}

func resetSkillSettings(t *testing.T, h *memHarness) {
	t.Helper()
	clear := func() {
		_, _ = h.pool.Exec(context.Background(), "DELETE FROM app_settings WHERE key = ANY($1)",
			[]string{skills.SettingEnabled, skills.SettingPersonal, skills.SettingBuiltinOff})
	}
	clear()
	t.Cleanup(clear)
}

func (h *memHarness) createSkill(user sqlc.User, site bool, body string) SkillDetail {
	h.t.Helper()
	rec := httptest.NewRecorder()
	if site {
		h.server.CreateSiteSkill(rec, h.as(user, http.MethodPost, body))
	} else {
		h.server.CreateMySkill(rec, h.as(user, http.MethodPost, body))
	}
	if rec.Code != http.StatusCreated {
		h.t.Fatalf("create skill: %d %s", rec.Code, rec.Body)
	}
	var out SkillDetail
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		h.t.Fatal(err)
	}
	if site {
		id := out.Skill.Id
		h.t.Cleanup(func() { _, _ = h.pool.Exec(context.Background(), "DELETE FROM skills WHERE id = $1", id) })
	}
	return out
}

func TestSkillsListReadAndPinnedVersion(t *testing.T) {
	tag := skillTag()
	personal, site := "house-style-"+tag, "site-guide-"+tag
	h := newMemHarness(t, func(n int) string {
		switch n {
		case 1:
			return openAICall("c1", tools.ToolSkill, fmt.Sprintf(`{"op":"view","name":%q}`, personal))
		case 2:
			return openAICall("c2", tools.ToolSkill, fmt.Sprintf(`{"op":"read","name":%q,"path":"references/tone.md"}`, personal))
		}
		return openAIWire.text("done")
	})
	resetSkillSettings(t, h)
	admin := newUser(t, h.pool, "admin")

	h.createSkill(admin, true, skillBody(site, "Site wide guide.", "Site body text", nil))
	mine := h.createSkill(h.user, false, skillBody(personal, "Write in the house style.", "Open references/tone.md first. BODY-V1",
		map[string]string{"references/tone.md": "TONE-REFERENCE-TEXT"}))
	if mine.Skill.Source != SkillSourcePersonal || len(mine.Files) != 2 || mine.Files[0].Path != "SKILL.md" {
		t.Fatalf("created = %+v", mine)
	}

	conv := h.conversation(false)
	if status := h.send(conv.ID, "write me a post"); status != sqlc.RunStatusCompleted {
		t.Fatalf("run: %s", status)
	}
	first := h.system(1)
	mustContain(t, "prompt", first, "## Skills", "- "+site+": Site wide guide.", "- "+personal+": Write in the house style.")
	mustNotContain(t, "prompt", first, "BODY-V1")
	if !offers(h.toolNames(1), tools.ToolSkill) {
		t.Fatalf("the skill tool is not offered: %v", h.toolNames(1))
	}
	mustContain(t, "view result", h.body(2), "BODY-V1", "references/tone.md")
	mustNotContain(t, "view result", h.body(2), "name: "+personal)
	mustContain(t, "read result", h.body(3), "TONE-REFERENCE-TEXT")

	// A new version: the conversation under way keeps the one it started with.
	rec := httptest.NewRecorder()
	h.server.UpdateMySkill(rec, h.request(http.MethodPut, skillBody(personal, "Changed description.", "BODY-V2", nil)), mine.Skill.Id)
	if rec.Code != http.StatusOK {
		t.Fatalf("update: %d %s", rec.Code, rec.Body)
	}
	turn := h.requests() + 1
	h.reply = func(n int) string {
		if n == turn {
			return openAICall("c3", tools.ToolSkill, fmt.Sprintf(`{"op":"view","name":%q}`, personal))
		}
		return openAIWire.text("done")
	}
	h.send(conv.ID, "again")
	if h.system(turn) != first {
		t.Fatal("the system prompt changed after the skill was edited")
	}
	mustContain(t, "pinned view", h.body(turn+1), "BODY-V1")

	next := h.conversation(false)
	h.send(next.ID, "hi")
	mustContain(t, "new conversation", h.system(h.requests()), "- "+personal+": Changed description.")
}

func TestSkillsAreTheOwnersOnly(t *testing.T) {
	h := newMemHarness(t, nil)
	resetSkillSettings(t, h)
	other := newUser(t, h.pool, "user")
	admin := newUser(t, h.pool, "admin")
	mine := h.createSkill(h.user, false, skillBody("private-"+skillTag(), "Mine.", "secret", nil))

	for name, call := range map[string]func(*httptest.ResponseRecorder){
		"other user": func(rec *httptest.ResponseRecorder) {
			h.server.GetMySkill(rec, h.as(other, http.MethodGet, ""), mine.Skill.Id)
		},
		"admin as own": func(rec *httptest.ResponseRecorder) {
			h.server.GetMySkill(rec, h.as(admin, http.MethodGet, ""), mine.Skill.Id)
		},
		"site route": func(rec *httptest.ResponseRecorder) {
			h.server.GetSiteSkill(rec, h.as(admin, http.MethodGet, ""), mine.Skill.Id)
		},
		"delete": func(rec *httptest.ResponseRecorder) {
			h.server.DeleteMySkill(rec, h.as(other, http.MethodDelete, ""), mine.Skill.Id)
		},
	} {
		rec := httptest.NewRecorder()
		call(rec)
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s: %d %s", name, rec.Code, rec.Body)
		}
	}

	// Someone else's skill is not in their conversations either.
	snap, err := h.notes.EnsureSnapshot(context.Background(), other.ID, h.conversationOf(other).ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range snap.Skills {
		if s.Name == mine.Skill.Name {
			t.Fatal("another person's skill is in the snapshot")
		}
	}
}

func (h *memHarness) conversationOf(user sqlc.User) sqlc.Conversation {
	h.t.Helper()
	conv, err := h.q.CreateConversation(context.Background(), sqlc.CreateConversationParams{UserID: &user.ID, Title: defaultTitle})
	if err != nil {
		h.t.Fatal(err)
	}
	return conv
}

func TestSkillsPersonalShadowsSiteAndSwitches(t *testing.T) {
	h := newMemHarness(t, nil)
	resetSkillSettings(t, h)
	admin := newUser(t, h.pool, "admin")
	name := "shared-" + skillTag()
	h.createSkill(admin, true, skillBody(name, "The site's.", "site", nil))
	h.createSkill(h.user, false, skillBody(name, "Mine.", "mine", nil))

	snap, err := h.notes.EnsureSnapshot(context.Background(), h.user.ID, h.conversation(false).ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	found := 0
	for _, s := range snap.Skills {
		if s.Name == name {
			found++
			if s.Description != "Mine." {
				t.Fatalf("the site's skill was kept over the person's: %+v", s)
			}
		}
	}
	if found != 1 {
		t.Fatalf("%d entries named %s", found, name)
	}

	// Personal skills off: writing one is refused, and new conversations leave theirs out.
	if _, err := h.pool.Exec(context.Background(), "INSERT INTO app_settings (key, value) VALUES ($1, 'false')", skills.SettingPersonal); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	h.server.CreateMySkill(rec, h.request(http.MethodPost, skillBody("another-"+skillTag(), "x", "x", nil)))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("create while off: %d %s", rec.Code, rec.Body)
	}
	snap, err = h.notes.EnsureSnapshot(context.Background(), h.user.ID, h.conversation(false).ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range snap.Skills {
		if s.Name == name && s.Description != "The site's." {
			t.Fatalf("a personal skill went in while they are off: %+v", s)
		}
	}
}

func TestSkillsRefusals(t *testing.T) {
	h := newMemHarness(t, nil)
	resetSkillSettings(t, h)
	name := "dup-" + skillTag()
	h.createSkill(h.user, false, skillBody(name, "First.", "x", nil))

	for what, c := range map[string]struct {
		body string
		code int
		err  string
	}{
		"taken name": {skillBody(name, "Again.", "x", nil), http.StatusConflict, "skill_name_taken"},
		"bad name":   {skillBody("Bad Name", "x", "x", nil), http.StatusBadRequest, "skill_name"},
		"no entry":   {`{"files":[{"path":"README.md","content":"x"}]}`, http.StatusBadRequest, "skill_missing_entry"},
	} {
		rec := httptest.NewRecorder()
		h.server.CreateMySkill(rec, h.request(http.MethodPost, c.body))
		if rec.Code != c.code || !strings.Contains(rec.Body.String(), c.err) {
			t.Errorf("%s: %d %s", what, rec.Code, rec.Body)
		}
	}
}

func TestSkillsImportArchive(t *testing.T) {
	h := newMemHarness(t, nil)
	resetSkillSettings(t, h)
	tag := skillTag()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for path, content := range map[string]string{
		"pack/one/SKILL.md":        "---\nname: one-" + tag + "\ndescription: One.\n---\nbody",
		"pack/two/SKILL.md":        "---\nname: two-" + tag + "\ndescription: Two.\n---\nbody",
		"pack/two/assets/a.bin":    "\x00\x01",
		"pack/bad/SKILL.md":        "no frontmatter",
		"pack/one/references/r.md": "ref",
	} {
		f, _ := w.Create(path)
		_, _ = f.Write([]byte(content))
	}
	_ = w.Close()

	rec := httptest.NewRecorder()
	r := h.request(http.MethodPost, "")
	r.Body = io.NopCloser(bytes.NewReader(buf.Bytes()))
	h.server.ImportMySkills(rec, r, ImportMySkillsParams{Filename: "pack.zip"})
	if rec.Code != http.StatusOK {
		t.Fatalf("import: %d %s", rec.Code, rec.Body)
	}
	var out SkillImportResult
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Created) != 2 || len(out.Failed) != 1 || out.Failed[0].Code != "skill_frontmatter" {
		t.Fatalf("result = %+v", out)
	}
	if len(out.Skipped) != 1 || out.Skipped[0] != "pack/two/assets/a.bin" {
		t.Fatalf("skipped = %v", out.Skipped)
	}
}

// A person's skill is moderated like their custom instructions: refused when
// saved with moderation on, and, saved while it was off, checked when a
// conversation would take it - and left out if it fails.
func TestSkillsAreModerated(t *testing.T) {
	h, f := newModerationMemoryHarness(t, `,"strikes":0`)
	clear := func() { _, _ = h.pool.Exec(context.Background(), "DELETE FROM skills WHERE owner_id = $1", h.user.ID) }
	clear()
	t.Cleanup(clear)
	create := func(body string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		h.server.CreateMySkill(rec, h.request(http.MethodPost, body))
		return rec
	}

	refused(t, create(skillBody("bad-"+skillTag(), "Fine description.", "Do BLOCKME things", nil)))
	if h.count(`SELECT count(*) FROM skills WHERE owner_id = $1`, h.user.ID) != 0 {
		t.Fatal("a refused skill was saved")
	}
	if h.eventsOf("skill") != 1 {
		t.Errorf("%d skill events", h.eventsOf("skill"))
	}

	// Saved while moderation is off: unchecked, then left out of the snapshot.
	if rec := h.patchModeration(`{"mode":"off"}`); rec.Code != http.StatusOK {
		t.Fatal(rec.Body)
	}
	name := "later-" + skillTag()
	if rec := create(skillBody(name, "Fine description.", "Do BLOCKME things", nil)); rec.Code != http.StatusCreated {
		t.Fatalf("create while off: %d %s", rec.Code, rec.Body)
	}
	if rec := h.patchModeration(`{"mode":"openai"}`); rec.Code != http.StatusOK {
		t.Fatal(rec.Body)
	}
	calls := f.calls()
	conv := h.newConv()
	if rec := h.sendIn(conv.ID, "hello"); rec.Code != http.StatusAccepted {
		t.Fatalf("send: %d %s", rec.Code, rec.Body)
	}
	if f.calls() <= calls+1 {
		t.Errorf("the snapshot did not ask about the skill: %d calls", f.calls()-calls)
	}
	snap, _ := h.snapshotOf(conv.ID)
	for _, s := range snap.Skills {
		if s.Name == name {
			t.Fatalf("a skill that fails the check is in the snapshot: %+v", snap.Skills)
		}
	}
	if h.count(`SELECT count(*) FROM skill_versions v JOIN skills s ON s.id = v.skill_id WHERE s.owner_id = $1 AND v.blocked = v.content_hash`, h.user.ID) != 1 {
		t.Error("the refusal was not recorded on the version")
	}
}

// Built-in skills are listed with the site's, can be switched off, and give way
// to a site or personal skill of the same name.
func TestSkillsBuiltinListedAndSwitched(t *testing.T) {
	h := newMemHarness(t, nil)
	resetSkillSettings(t, h)
	admin := newUser(t, h.pool, "admin")
	names := func() map[string]memorySkill {
		snap, err := h.notes.EnsureSnapshot(context.Background(), h.user.ID, h.conversation(false).ID, nil)
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]memorySkill{}
		for _, s := range snap.Skills {
			out[s.Name] = memorySkill{builtin: s.Builtin, personal: s.Personal, description: s.Description}
		}
		return out
	}
	if got := names()["skill-authoring"]; !got.builtin {
		t.Fatalf("the built-in skill is not listed: %+v", names())
	}

	rec := httptest.NewRecorder()
	h.server.SetBuiltinSkillEnabled(rec, h.as(admin, http.MethodPatch, `{"enabled":false}`), "skill-authoring")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("switch off: %d %s", rec.Code, rec.Body)
	}
	if _, there := names()["skill-authoring"]; there {
		t.Fatal("a built-in skill switched off is still listed")
	}
	rec = httptest.NewRecorder()
	h.server.SetBuiltinSkillEnabled(rec, h.as(admin, http.MethodPatch, `{"enabled":true}`), "skill-authoring")

	h.createSkill(h.user, false, skillBody("skill-authoring", "Mine instead.", "x", nil))
	if got := names()["skill-authoring"]; got.builtin || !got.personal || got.description != "Mine instead." {
		t.Fatalf("a personal skill did not take the built-in one's place: %+v", got)
	}

	rec = httptest.NewRecorder()
	h.server.SetBuiltinSkillEnabled(rec, h.as(admin, http.MethodPatch, `{"enabled":false}`), "no-such-skill")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown built-in: %d", rec.Code)
	}
}

type memorySkill struct {
	builtin, personal bool
	description       string
}

func TestSkillsVersionsExportAndPicker(t *testing.T) {
	h := newMemHarness(t, nil)
	resetSkillSettings(t, h)
	name := "versioned-" + skillTag()
	mine := h.createSkill(h.user, false, skillBody(name, "First.", "ONE", nil))
	rec := httptest.NewRecorder()
	h.server.UpdateMySkill(rec, h.request(http.MethodPut, skillBody(name, "Second.", "TWO", nil)), mine.Skill.Id)
	if rec.Code != http.StatusOK {
		t.Fatalf("update: %d %s", rec.Code, rec.Body)
	}

	rec = httptest.NewRecorder()
	h.server.ListMySkillVersions(rec, h.request(http.MethodGet, ""), mine.Skill.Id)
	var versions SkillVersionList
	_ = json.Unmarshal(rec.Body.Bytes(), &versions)
	if len(versions.Items) != 2 || !versions.Items[0].Current || versions.Items[1].Description != "First." {
		t.Fatalf("versions = %+v", versions)
	}
	rec = httptest.NewRecorder()
	h.server.GetMySkillVersion(rec, h.request(http.MethodGet, ""), mine.Skill.Id, versions.Items[1].Id)
	mustContain(t, "old version", rec.Body.String(), "ONE")
	other := newUser(t, h.pool, "user")
	rec = httptest.NewRecorder()
	h.server.GetMySkillVersion(rec, h.as(other, http.MethodGet, ""), mine.Skill.Id, versions.Items[1].Id)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("another person read a version: %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	h.server.ExportMySkill(rec, h.request(http.MethodGet, ""), mine.Skill.Id)
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "application/zip" {
		t.Fatalf("export: %d %s", rec.Code, rec.Header())
	}
	unpacked, err := skills.Unpack(name+".zip", rec.Body.Bytes())
	if err != nil || len(unpacked.Skills) != 1 {
		t.Fatalf("export does not import back: %v %+v", err, unpacked)
	}

	// The picker: before a conversation starts, what it would list; after,
	// what its snapshot listed, whatever was added since.
	conv := h.conversation(false)
	available := func() []string {
		rec := httptest.NewRecorder()
		id := conv.ID
		h.server.ListAvailableSkills(rec, h.request(http.MethodGet, ""), ListAvailableSkillsParams{ConversationId: &id})
		var out AvailableSkillList
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		var names []string
		for _, s := range out.Items {
			if s.Source == AvailableSkillSourcePersonal {
				names = append(names, s.Name)
			}
		}
		return names
	}
	sameList(t, "before the snapshot", available(), []string{name})
	if _, err := h.notes.EnsureSnapshot(context.Background(), h.user.ID, conv.ID, nil); err != nil {
		t.Fatal(err)
	}
	later := "later-" + skillTag()
	h.createSkill(h.user, false, skillBody(later, "Added after.", "x", nil))
	sameList(t, "after the snapshot", available(), []string{name})
}
