package skills

import (
	"embed"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"
)

// SettingBuiltinOff is the app_settings key listing the built-in skills an
// administrator switched off (a JSON array of names). A built-in skill is on
// unless listed.
const SettingBuiltinOff = "builtin_skills_off"

// Built-in skills ship in the binary (builtin/<name>/), so they change with an
// upgrade and nowhere else: nobody edits them, an administrator only switches
// them off. They are listed with the site's skills; a site skill or a person's
// own of the same name takes its place.
//
//go:embed builtin
var builtinFS embed.FS

// BuiltinSkill is a built-in skill, parsed.
type BuiltinSkill struct {
	Name        string
	Description string
	Files       []File
}

var builtins = loadBuiltins()

// loadBuiltins reads every built-in skill. One that does not parse is a bug of
// this repository, caught by the package's tests; it panics at start.
func loadBuiltins() []BuiltinSkill {
	dirs, err := fs.ReadDir(builtinFS, "builtin")
	if err != nil {
		panic(err)
	}
	var out []BuiltinSkill
	for _, dir := range dirs {
		if !dir.IsDir() {
			continue
		}
		root := path.Join("builtin", dir.Name())
		var files []File
		err := fs.WalkDir(builtinFS, root, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			data, err := builtinFS.ReadFile(p)
			if err != nil {
				return err
			}
			files = append(files, File{Path: strings.TrimPrefix(p, root+"/"), Content: string(data)})
			return nil
		})
		if err != nil {
			panic(err)
		}
		parsed, err := Parse(files)
		if err != nil {
			panic(fmt.Sprintf("built-in skill %s: %v", dir.Name(), err))
		}
		if parsed.Name != dir.Name() {
			panic(fmt.Sprintf("built-in skill %s is named %s in its SKILL.md", dir.Name(), parsed.Name))
		}
		out = append(out, BuiltinSkill{Name: parsed.Name, Description: parsed.Description, Files: parsed.Files})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Builtins are the built-in skills, by name.
func Builtins() []BuiltinSkill { return builtins }

// Builtin is the built-in skill of this name.
func Builtin(name string) (BuiltinSkill, bool) {
	for _, b := range builtins {
		if b.Name == name {
			return b, true
		}
	}
	return BuiltinSkill{}, false
}
