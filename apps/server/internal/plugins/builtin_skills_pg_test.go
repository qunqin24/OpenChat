package plugins

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/qunqin24/openchat/apps/server/internal/db/dbtest"
	"github.com/qunqin24/openchat/apps/server/internal/db/sqlc"
	"github.com/qunqin24/openchat/apps/server/internal/skills"
)

// Every built-in plugin's skills parse, under their directory's name.
func TestBuiltinSkillsParse(t *testing.T) {
	for _, b := range Builtins {
		list, err := builtinSkills(b.Key)
		if err != nil {
			t.Fatal(err)
		}
		if len(list) == 0 {
			t.Errorf("%s has no skills", b.Key)
		}
	}
}

// Skills are put in once, updated while untouched, and an administrator's
// edit or deletion stands.
func TestEnsureBuiltinSkills(t *testing.T) {
	pool := dbtest.Pool(t, "plugins_builtin_skills")
	ctx := context.Background()
	q := sqlc.New(pool)
	if _, err := pool.Exec(ctx, `DELETE FROM app_settings WHERE key = $1`, settingBuiltinSkills); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM plugins WHERE builtin IS NOT NULL`); err != nil {
		t.Fatal(err)
	}
	if err := EnsureBuiltins(ctx, q); err != nil {
		t.Fatal(err)
	}
	if err := EnsureBuiltinSkills(ctx, pool); err != nil {
		t.Fatal(err)
	}
	key := "arxiv"
	id, err := q.GetBuiltinPluginID(ctx, &key)
	if err != nil {
		t.Fatal(err)
	}
	store := skills.NewStore(pool).ForPlugin(id)
	list, err := store.List(ctx, nil)
	if err != nil || len(list) != 1 || list[0].Name != "arxiv-research" || !list[0].Enabled || !list[0].Checked {
		t.Fatalf("put in: %+v %v", list, err)
	}
	skill := list[0]

	// A later release ships a new version: the untouched skill takes it.
	forget := func(hash string) {
		t.Helper()
		shipped := map[string]string{}
		raw, _ := q.GetSetting(ctx, settingBuiltinSkills)
		_ = json.Unmarshal(raw, &shipped)
		// Pretend what was shipped before was this version, now in the row.
		_, files, _ := store.Get(ctx, nil, skill.ID)
		files[0].Content += "\n" + hash
		parsed, _ := skills.Parse(files)
		if _, err := store.Save(ctx, nil, &skill.ID, parsed, &parsed.Hash, nil); err != nil {
			t.Fatal(err)
		}
		shipped["arxiv/arxiv-research"] = parsed.Hash
		value, _ := json.Marshal(shipped)
		if err := q.SetSetting(ctx, sqlc.SetSettingParams{Key: settingBuiltinSkills, Value: value}); err != nil {
			t.Fatal(err)
		}
	}
	current := func() string {
		_, files, err := store.Get(ctx, nil, skill.ID)
		if err != nil {
			t.Fatal(err)
		}
		parsed, _ := skills.Parse(files)
		return parsed.Hash
	}
	shippedNow, _ := builtinSkills("arxiv")
	forget("older")
	if err := EnsureBuiltinSkills(ctx, pool); err != nil {
		t.Fatal(err)
	}
	if current() != shippedNow[0].Hash {
		t.Fatal("an untouched skill was not updated")
	}

	// Edited by an administrator: a new release leaves it.
	forget("older again")
	_, files, _ := store.Get(ctx, nil, skill.ID)
	files[0].Content += "\nadministrator's note"
	edited, _ := skills.Parse(files)
	if _, err := store.Save(ctx, nil, &skill.ID, edited, &edited.Hash, nil); err != nil {
		t.Fatal(err)
	}
	if err := EnsureBuiltinSkills(ctx, pool); err != nil {
		t.Fatal(err)
	}
	if current() != edited.Hash {
		t.Fatal("an administrator's edit was overwritten")
	}

	// Deleted by an administrator: not put back.
	if err := store.Delete(ctx, nil, skill.ID); err != nil {
		t.Fatal(err)
	}
	if err := EnsureBuiltinSkills(ctx, pool); err != nil {
		t.Fatal(err)
	}
	if list, _ := store.List(ctx, nil); len(list) != 0 {
		t.Fatalf("a deleted skill came back: %+v", list)
	}
}
