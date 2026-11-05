package plugins

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"path"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/qunqin24/openchat/apps/server/internal/db/sqlc"
	"github.com/qunqin24/openchat/apps/server/internal/skills"
)

// The skills that come with the built-in plugins, one directory a skill
// under the plugin's key (builtin_skills/<key>/<skill>/SKILL.md).
//
//go:embed builtin_skills
var builtinSkillFiles embed.FS

// settingBuiltinSkills remembers, by "<key>/<skill>", the version of each
// skill last put into a built-in plugin, so a later release updates what an
// administrator left as it was, leaves what they edited, and does not bring
// back what they deleted.
const settingBuiltinSkills = "builtin_plugin_skills"

// builtinSkills are a built-in plugin's skills as they ship, parsed.
func builtinSkills(key string) ([]skills.Parsed, error) {
	dirs, err := fs.ReadDir(builtinSkillFiles, path.Join("builtin_skills", key))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []skills.Parsed
	for _, dir := range dirs {
		root := path.Join("builtin_skills", key, dir.Name())
		var files []skills.File
		err := fs.WalkDir(builtinSkillFiles, root, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			data, err := builtinSkillFiles.ReadFile(p)
			if err != nil {
				return err
			}
			rel := p[len(root)+1:]
			files = append(files, skills.File{Path: rel, Content: string(data)})
			return nil
		})
		if err != nil {
			return nil, err
		}
		parsed, err := skills.Parse(files)
		if err != nil {
			return nil, fmt.Errorf("built-in skill %s/%s: %w", key, dir.Name(), err)
		}
		if parsed.Name != dir.Name() {
			return nil, fmt.Errorf("built-in skill %s/%s is named %q", key, dir.Name(), parsed.Name)
		}
		out = append(out, parsed)
	}
	return out, nil
}

// EnsureBuiltinSkills gives each built-in plugin the skills it ships with:
// a skill it never had is added, one still as it was last shipped is brought
// up to date, and one an administrator edited or deleted is left alone.
// Written by OpenChat, so not checked by moderation, as an administrator's are not.
func EnsureBuiltinSkills(ctx context.Context, pool *pgxpool.Pool) error {
	q := sqlc.New(pool)
	shipped := map[string]string{}
	raw, err := q.GetSetting(ctx, settingBuiltinSkills)
	switch {
	case err == nil:
		if err := json.Unmarshal(raw, &shipped); err != nil {
			return err
		}
	case !errors.Is(err, pgx.ErrNoRows):
		return err
	}
	changed := false
	for _, b := range Builtins {
		list, err := builtinSkills(b.Key)
		if err != nil {
			return err
		}
		if len(list) == 0 {
			continue
		}
		key := b.Key
		pluginID, err := q.GetBuiltinPluginID(ctx, &key)
		if errors.Is(err, pgx.ErrNoRows) {
			continue // its short name was taken (EnsureBuiltins)
		}
		if err != nil {
			return err
		}
		store := skills.NewStore(pool).ForPlugin(pluginID)
		have, err := store.List(ctx, nil)
		if err != nil {
			return err
		}
		for _, parsed := range list {
			mark := b.Key + "/" + parsed.Name
			last, seen := shipped[mark]
			if last == parsed.Hash {
				continue
			}
			var current *skills.Skill
			for i := range have {
				if have[i].Name == parsed.Name {
					current = &have[i]
				}
			}
			var id *uuid.UUID
			switch {
			case current == nil && seen:
				continue // deleted by an administrator
			case current == nil:
			default:
				_, files, err := store.Get(ctx, nil, current.ID)
				if err != nil {
					return err
				}
				now, err := skills.Parse(files)
				if err != nil || now.Hash != last {
					continue // edited by an administrator, or theirs to begin with
				}
				id = &current.ID
			}
			hash := parsed.Hash
			if _, err := store.Save(ctx, nil, id, parsed, &hash, nil); err != nil {
				return fmt.Errorf("built-in skill %s: %w", mark, err)
			}
			slog.InfoContext(ctx, "built-in plugin skill put in", "plugin", b.Slug, "skill", parsed.Name, "updated", id != nil)
			shipped[mark] = parsed.Hash
			changed = true
		}
	}
	if !changed {
		return nil
	}
	value, _ := json.Marshal(shipped)
	return q.SetSetting(ctx, sqlc.SetSettingParams{Key: settingBuiltinSkills, Value: value})
}
