package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/qunqin24/openchat/apps/server/internal/agent"
	"github.com/qunqin24/openchat/apps/server/internal/llm"
	"github.com/qunqin24/openchat/apps/server/internal/memory"
	"github.com/qunqin24/openchat/apps/server/internal/skills"
)

// ToolSkill is the skill tool's name.
const ToolSkill = "skill"

// Skill is the `skill` tool: the model opens one of the skills its
// conversation lists (docs/design/skills-plan.md) and reads the files the
// instructions refer to. The list, with each skill's name and description, is
// in the system prompt (api/agent_setup.go); this reads the rest, a skill at a
// time, as a task calls for it.
//
// Only what the conversation's snapshot lists is found, at the version listed
// there, and only the run's user's own skills or the site's: the user comes
// from the run (agent.ScopeFrom), never from the arguments. Read-only, so it
// runs alongside any other call.
type Skill struct {
	Notes  *memory.Store
	Skills *skills.Store
	// Only, for a sub-agent's run, is the skills its launch named: it opens
	// those and nothing else, from the list of the conversation it works for.
	Only []string
}

var _ agent.Tool = Skill{}

// SkillArgs are a call's arguments.
type SkillArgs struct {
	Op      string `json:"op"`
	Name    string `json:"name"`
	Path    string `json:"path"`
	Offset  int    `json:"offset"`
	Pattern string `json:"pattern"`
}

// SkillDetails is what the interface is shown of a call.
type SkillDetails struct {
	Op      string `json:"op"`
	Name    string `json:"name"`
	Path    string `json:"path,omitempty"`
	Pattern string `json:"pattern,omitempty"`
}

func (Skill) Definition() llm.Tool {
	return llm.Tool{
		Name: ToolSkill,
		Description: "Open one of the skills listed in the Skills section of your instructions and read its files. " +
			"op=view opens a skill (name): its instructions, and the list of its other files. " +
			"op=read reads one file of the skill (name, path) that the instructions refer to; a long file is " +
			"read in parts with offset. op=search finds a word or phrase (pattern) in the skill's files, for a " +
			"long skill. Read a skill before doing the task it is for, then follow it. Scripts in a skill cannot " +
			"be run here; use what they show as reference.",
		Parameters: json.RawMessage(`{
  "type": "object",
  "properties": {
    "op": {"type": "string", "enum": ["view", "read", "search"]},
    "name": {"type": "string", "description": "The skill's name, as listed."},
    "path": {"type": "string", "description": "For read: the file's path in the skill, such as references/guide.md."},
    "offset": {"type": "integer", "description": "For view and read: where to continue reading, from a previous result."},
    "pattern": {"type": "string", "description": "For search: the word or phrase to find, ignoring case."}
  },
  "required": ["op", "name"]
}`),
	}
}

// Replayable is true: it only reads.
func (Skill) Replayable() bool { return true }

func (t Skill) Execute(ctx context.Context, raw json.RawMessage) (agent.ToolResult, error) {
	scope, ok := agent.ScopeFrom(ctx)
	if !ok {
		return agent.ToolResult{}, errors.New("skill: no run in context")
	}
	var args SkillArgs
	if err := agent.DecodeArgs(raw, &args); err != nil {
		return agent.ToolResult{}, err
	}
	op := strings.ToLower(strings.TrimSpace(args.Op))
	name := strings.ToLower(strings.TrimSpace(args.Name))
	details := SkillDetails{Op: op, Name: name, Path: strings.TrimSpace(args.Path), Pattern: strings.TrimSpace(args.Pattern)}

	available, err := t.Listed(ctx, scope)
	if err != nil {
		return agent.ToolResult{}, err
	}
	var listed *memory.SnapshotSkill
	for i := range available {
		if available[i].Name == name {
			listed = &available[i]
		}
	}
	if listed == nil {
		names := make([]string, len(available))
		for i, s := range available {
			names[i] = s.Name
		}
		if len(names) == 0 {
			return agent.ToolResult{}, agent.Errorf("No skill is available in this conversation.")
		}
		return agent.ToolResult{}, agent.Errorf("There is no skill named %q here. Available: %s.", args.Name, strings.Join(names, ", "))
	}
	var files []skills.File
	if listed.Builtin {
		builtin, ok := skills.Builtin(name)
		if !ok {
			return agent.ToolResult{}, agent.Errorf("The skill %q is no longer part of this server and can no longer be read.", name)
		}
		files = builtin.Files
	} else {
		_, files, err = t.Skills.Version(ctx, scope.UserID, listed.VersionID)
		if errors.Is(err, skills.ErrNotFound) {
			return agent.ToolResult{}, agent.Errorf("The skill %q was deleted after this conversation started and can no longer be read.", name)
		}
		if err != nil {
			return agent.ToolResult{}, err
		}
	}

	var b strings.Builder
	switch op {
	case "view":
		var entry string
		for _, f := range files {
			if f.Path == skills.Entry {
				entry = f.Content
			}
		}
		fmt.Fprintf(&b, "Skill: %s\n\n", name)
		readPart(&b, []rune(skills.Body(entry)), args.Offset, fmt.Sprintf("op=view with name %q", name))
		if args.Offset <= 0 && len(files) > 1 {
			b.WriteString("\n\nOther files in this skill (read one with op=read):")
			for _, f := range files {
				if f.Path != skills.Entry {
					fmt.Fprintf(&b, "\n- %s (%d characters)", f.Path, utf8.RuneCountInString(f.Content))
				}
			}
		}
	case "read":
		path := strings.TrimPrefix(strings.TrimSpace(strings.ReplaceAll(args.Path, "\\", "/")), "./")
		var file *skills.File
		for i := range files {
			if files[i].Path == path {
				file = &files[i]
			}
		}
		if file == nil {
			paths := make([]string, len(files))
			for i, f := range files {
				paths[i] = f.Path
			}
			return agent.ToolResult{}, agent.Errorf("The skill %q has no file %q. Its files: %s.", name, args.Path, strings.Join(paths, ", "))
		}
		details.Path = file.Path
		fmt.Fprintf(&b, "Skill: %s, file: %s\n\n", name, file.Path)
		readPart(&b, []rune(file.Content), args.Offset, fmt.Sprintf("op=read with name %q and path %q", name, file.Path))
	case "search":
		pattern := strings.TrimSpace(args.Pattern)
		if pattern == "" {
			return agent.ToolResult{}, agent.Errorf("invalid arguments: search needs a pattern")
		}
		shown, total := 0, 0
		for _, f := range files {
			text := []rune(f.Content)
			passages, matches, _ := findIn(text, pattern, 0)
			total += matches
			for _, p := range passages {
				if shown == findPassages {
					break
				}
				shown++
				fmt.Fprintf(&b, "\n\n[%s, offset %d]\n%s", f.Path, p.start, strings.TrimSpace(string(text[p.start:p.end])))
			}
		}
		if total == 0 {
			return agent.ToolResult{Content: fmt.Sprintf("%q does not occur in the skill %q.", pattern, name), Details: details}, nil
		}
		head := fmt.Sprintf("%d %s of %q in the skill %q. Read on from a passage with op=read, its path and offset.",
			total, plural(total, "match", "matches"), pattern, name)
		if shown < total {
			head += fmt.Sprintf(" The first %d are shown.", shown)
		}
		return agent.ToolResult{Content: head + b.String(), Details: details}, nil
	default:
		return agent.ToolResult{}, agent.Errorf("invalid arguments: op must be view, read or search")
	}
	return agent.ToolResult{Content: b.String(), Details: details}, nil
}

// Listed is what a run may open: its conversation's skills - for a sub-agent,
// those of the conversation it works for that its launch named (Only).
func (t Skill) Listed(ctx context.Context, scope agent.Scope) ([]memory.SnapshotSkill, error) {
	conversation := scope.ConversationID
	if scope.Subagent() {
		conversation = *scope.Parent
	}
	snap, err := t.Notes.EnsureSnapshot(ctx, scope.UserID, conversation, nil)
	if err != nil {
		return nil, err
	}
	if !scope.Subagent() {
		return snap.Skills, nil
	}
	var out []memory.SnapshotSkill
	for _, skill := range snap.Skills {
		if slices.Contains(t.Only, skill.Name) {
			out = append(out, skill)
		}
	}
	return out, nil
}

// readPart writes the part of a text that a read from offset shows, as
// web_fetch does, and how to read on.
func readPart(b *strings.Builder, text []rune, offset int, again string) {
	start, end := window(text, offset)
	b.WriteString(string(text[start:end]))
	if start > 0 || end < len(text) {
		fmt.Fprintf(b, "\n\n[Characters %d–%d of %d.", start, end, len(text))
		if end < len(text) {
			fmt.Fprintf(b, " To read on, call skill with %s and offset %d.", again, end)
		}
		b.WriteString("]")
	}
}
