// Package skills keeps the instructions a model loads when a task calls for
// them (docs/design/skills-plan.md), in the Agent Skills format
// (agentskills.io/specification): a SKILL.md whose YAML frontmatter names the
// skill and says when to use it, and the text files it refers to.
//
// A skill is the site's, written by an administrator, or one person's own.
// Every save is a new version, and a conversation reads the versions its
// snapshot listed (memory.Snapshot), so editing a skill changes the next
// conversation and not the one under way - for the same reason the snapshot
// exists: the system prompt stays the same bytes from turn to turn.
//
// Only text is kept for now. Scripts are text and are kept, but nothing runs
// them until there is a sandbox; binary assets wait for it too.
package skills

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"gopkg.in/yaml.v3"

	"github.com/qunqin24/openchat/apps/server/internal/db/sqlc"
)

const (
	// SettingEnabled is the administrator's switch for the whole feature: off,
	// no conversation lists a skill and the tool is not offered.
	SettingEnabled = "skills"
	// SettingPersonal lets people write skills of their own. Off, the ones they
	// have are kept but left out of new conversations.
	SettingPersonal = "personal_skills"

	// Entry is the file every skill has at its root.
	Entry = "SKILL.md"

	MaxNameLength        = 64
	MaxDescriptionLength = 1024
	// MaxFiles, MaxFileChars and MaxTextChars bound one version. The whole text
	// stays under what content moderation reads in one check (moderationMaxRunes
	// in package api), so a person's skill can always be checked.
	MaxFiles     = 50
	MaxFileChars = 100_000
	MaxTextChars = 300_000
	MaxPathChars = 200
	// MaxPersonal and MaxSite bound how many skills an owner keeps.
	MaxPersonal = 30
	MaxSite     = 100
	// KeepVersions is how many versions of a skill are kept, the current one
	// among them.
	KeepVersions = 20
)

var (
	ErrNotFound  = errors.New("no such skill")
	ErrNameTaken = errors.New("a skill with this name already exists")
	ErrFull      = errors.New("the skill limit is reached")
)

// InvalidError is a skill that does not follow the format or the limits. Code
// is stable for the interface; Message says what to fix.
type InvalidError struct {
	Code    string
	Message string
}

func (e *InvalidError) Error() string { return e.Message }

func invalid(code, format string, args ...any) error {
	return &InvalidError{Code: code, Message: fmt.Sprintf(format, args...)}
}

// File is one file of a skill, by its path from the skill's root.
type File struct {
	Path    string
	Content string
}

// Parsed is a skill's files as they will be saved: checked, in path order, with
// what the frontmatter says.
type Parsed struct {
	Name        string
	Description string
	Files       []File
	// Text is the files as one text, as content moderation reads them, and Hash
	// its hash: what "checked" refers to.
	Text string
	Hash string
}

var nameRule = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// Parse checks a skill's files and reads its frontmatter.
func Parse(files []File) (Parsed, error) {
	if len(files) == 0 {
		return Parsed{}, invalid("skill_missing_entry", "a skill needs a %s", Entry)
	}
	if len(files) > MaxFiles {
		return Parsed{}, invalid("skill_too_many_files", "a skill has at most %d files", MaxFiles)
	}
	seen := map[string]bool{}
	out := Parsed{Files: make([]File, 0, len(files))}
	total := 0
	for _, f := range files {
		p, err := cleanPath(f.Path)
		if err != nil {
			return Parsed{}, err
		}
		if seen[p] {
			return Parsed{}, invalid("skill_duplicate_file", "%s appears twice", p)
		}
		seen[p] = true
		if !utf8.ValidString(f.Content) || strings.ContainsRune(f.Content, 0) {
			return Parsed{}, invalid("skill_binary_file", "%s is not text; only text files are kept for now", p)
		}
		content := strings.ReplaceAll(f.Content, "\r\n", "\n")
		n := utf8.RuneCountInString(content)
		if n > MaxFileChars {
			return Parsed{}, invalid("skill_file_too_large", "%s is longer than %d characters", p, MaxFileChars)
		}
		total += n
		out.Files = append(out.Files, File{Path: p, Content: content})
	}
	if total > MaxTextChars {
		return Parsed{}, invalid("skill_too_large", "a skill's files come to at most %d characters", MaxTextChars)
	}
	if !seen[Entry] {
		return Parsed{}, invalid("skill_missing_entry", "a skill needs a %s at its root", Entry)
	}
	sort.Slice(out.Files, func(i, j int) bool { return out.Files[i].Path < out.Files[j].Path })

	var entry string
	for _, f := range out.Files {
		if f.Path == Entry {
			entry = f.Content
		}
	}
	meta, _, err := Frontmatter(entry)
	if err != nil {
		return Parsed{}, err
	}
	out.Name, out.Description = meta.Name, meta.Description
	out.Text = Text(out.Files)
	out.Hash = Hash(out.Text)
	return out, nil
}

// Meta is what a SKILL.md's frontmatter says, of the fields used here.
type Meta struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
}

// Frontmatter reads the YAML between the opening and closing `---` of a
// SKILL.md, and returns the body after it.
func Frontmatter(entry string) (Meta, string, error) {
	text := strings.TrimPrefix(entry, "\uFEFF")
	if !strings.HasPrefix(text, "---\n") {
		return Meta{}, "", invalid("skill_frontmatter", "%s must start with frontmatter between --- lines, with a name and a description", Entry)
	}
	rest := text[len("---\n"):]
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return Meta{}, "", invalid("skill_frontmatter", "%s's frontmatter has no closing --- line", Entry)
	}
	head, body := rest[:end], rest[end+len("\n---"):]
	if i := strings.IndexByte(body, '\n'); i >= 0 {
		body = body[i+1:]
	} else {
		body = ""
	}
	var meta Meta
	if err := yaml.Unmarshal([]byte(head), &meta); err != nil {
		return Meta{}, "", invalid("skill_frontmatter", "%s's frontmatter is not valid YAML: %v", Entry, err)
	}
	meta.Name = strings.TrimSpace(meta.Name)
	meta.Description = strings.TrimSpace(meta.Description)
	switch {
	case meta.Name == "":
		return Meta{}, "", invalid("skill_name", "the frontmatter needs a name")
	case len(meta.Name) > MaxNameLength || !nameRule.MatchString(meta.Name):
		return Meta{}, "", invalid("skill_name", "the name %q must be lowercase letters, digits and single hyphens, at most %d characters", meta.Name, MaxNameLength)
	case meta.Description == "":
		return Meta{}, "", invalid("skill_description", "the frontmatter needs a description: what the skill does and when to use it")
	case utf8.RuneCountInString(meta.Description) > MaxDescriptionLength:
		return Meta{}, "", invalid("skill_description", "the description is at most %d characters", MaxDescriptionLength)
	}
	return meta, body, nil
}

// cleanPath is a file's path from the skill's root, refused if it would leave
// the root or is not a plain relative path.
func cleanPath(raw string) (string, error) {
	p := strings.TrimSpace(strings.ReplaceAll(raw, "\\", "/"))
	p = strings.TrimPrefix(p, "./")
	if p == "" || strings.HasPrefix(p, "/") || utf8.RuneCountInString(p) > MaxPathChars {
		return "", invalid("skill_path", "%q is not a valid file path", raw)
	}
	clean := path.Clean(p)
	if clean != p || clean == "." || strings.HasPrefix(clean, "../") || clean == ".." {
		return "", invalid("skill_path", "%q is not a valid file path", raw)
	}
	for _, part := range strings.Split(clean, "/") {
		if strings.HasPrefix(part, ".") {
			return "", invalid("skill_path", "%q: hidden files are not kept", raw)
		}
	}
	return clean, nil
}

// Text is a version's files as one text, each under its path: what content
// moderation reads, and what Hash is of.
func Text(files []File) string {
	var b strings.Builder
	for i, f := range files {
		if i > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString("=== ")
		b.WriteString(f.Path)
		b.WriteString(" ===\n")
		b.WriteString(f.Content)
	}
	return b.String()
}

// Hash is the hex SHA-256 of a text, as memory.Hash is: how a text is
// remembered as checked.
func Hash(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}

// ---------------------------------------------------------------------- store

// Skill is a skill at its current version.
type Skill struct {
	ID          uuid.UUID
	OwnerID     *uuid.UUID
	Name        string
	Description string
	Enabled     bool
	VersionID   uuid.UUID
	// Checked says the current version passed content moderation (or is the
	// site's); Blocked that a check refused it, so conversations leave it out.
	Checked   bool
	Blocked   bool
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Store reads and writes skills. owner is a person's id for their skills, nil
// for the site's; every call is scoped to it, so another owner's skill is not
// found. A store made with ForPlugin holds one plugin's skills instead
// (owner nil), which are neither the site's nor anyone's.
type Store struct {
	pool   *pgxpool.Pool
	q      *sqlc.Queries
	plugin *uuid.UUID
}

func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool, q: sqlc.New(pool)} }

// ForPlugin is the store of one plugin's skills (docs/design/plugins-plan.md §2).
func (s *Store) ForPlugin(pluginID uuid.UUID) *Store {
	return &Store{pool: s.pool, q: s.q, plugin: &pluginID}
}

func (s *Store) List(ctx context.Context, owner *uuid.UUID) ([]Skill, error) {
	rows, err := s.q.ListOwnedSkills(ctx, sqlc.ListOwnedSkillsParams{OwnerID: owner, PluginID: s.plugin})
	if err != nil {
		return nil, err
	}
	out := make([]Skill, len(rows))
	for i, r := range rows {
		out[i] = skillOf(r.ID, r.OwnerID, r.Name, r.Enabled, r.VersionID, r.Description, r.ContentHash, r.Checked, r.Blocked, r.CreatedAt.Time, r.UpdatedAt.Time)
	}
	return out, nil
}

// Get is one skill and the files of its current version.
func (s *Store) Get(ctx context.Context, owner *uuid.UUID, id uuid.UUID) (Skill, []File, error) {
	r, err := s.q.GetOwnedSkill(ctx, sqlc.GetOwnedSkillParams{ID: id, OwnerID: owner, PluginID: s.plugin})
	if errors.Is(err, pgx.ErrNoRows) {
		return Skill{}, nil, ErrNotFound
	}
	if err != nil {
		return Skill{}, nil, err
	}
	files, err := s.files(ctx, s.q, r.VersionID)
	if err != nil {
		return Skill{}, nil, err
	}
	return skillOf(r.ID, r.OwnerID, r.Name, r.Enabled, r.VersionID, r.Description, r.ContentHash, r.Checked, r.Blocked, r.CreatedAt.Time, r.UpdatedAt.Time), files, nil
}

func skillOf(id uuid.UUID, owner *uuid.UUID, name string, enabled bool, version uuid.UUID, description, hash string, checked, blocked *string, created, updated time.Time) Skill {
	return Skill{
		ID: id, OwnerID: owner, Name: name, Description: description, Enabled: enabled, VersionID: version,
		Checked:   checked != nil && *checked == hash,
		Blocked:   blocked != nil && *blocked == hash,
		CreatedAt: created, UpdatedAt: updated,
	}
}

func (s *Store) files(ctx context.Context, q *sqlc.Queries, version uuid.UUID) ([]File, error) {
	rows, err := q.ListSkillFiles(ctx, version)
	if err != nil {
		return nil, err
	}
	out := make([]File, len(rows))
	for i, r := range rows {
		out[i] = File{Path: r.Path, Content: r.Content}
	}
	return out, nil
}

// Save creates a skill (id nil) or saves a new version of one. checked is the
// hash that content moderation passed, or the site's own hash (an
// administrator's skill is not checked); nil leaves the version to be checked
// when a conversation would take it.
func (s *Store) Save(ctx context.Context, owner, id *uuid.UUID, p Parsed, checked *string, by uuid.UUID) (Skill, error) {
	var saved uuid.UUID
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		lock := uuid.Nil
		if owner != nil {
			lock = *owner
		}
		if s.plugin != nil {
			lock = *s.plugin
		}
		if err := q.LockSkillOwner(ctx, lock); err != nil {
			return err
		}
		taken, err := q.SkillNameTaken(ctx, sqlc.SkillNameTakenParams{Name: p.Name, OwnerID: owner, PluginID: s.plugin, Except: id})
		if err != nil {
			return err
		}
		if taken {
			return ErrNameTaken
		}
		if id == nil {
			count, err := q.CountOwnedSkills(ctx, sqlc.CountOwnedSkillsParams{OwnerID: owner, PluginID: s.plugin})
			if err != nil {
				return err
			}
			limit := MaxPersonal
			if owner == nil {
				limit = MaxSite
			}
			if count >= int64(limit) {
				return ErrFull
			}
			row, err := q.CreateSkill(ctx, sqlc.CreateSkillParams{OwnerID: owner, PluginID: s.plugin, Name: p.Name, Enabled: true})
			if err != nil {
				return err
			}
			saved = row.ID
		} else {
			if _, err := q.GetOwnedSkill(ctx, sqlc.GetOwnedSkillParams{ID: *id, OwnerID: owner, PluginID: s.plugin}); err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					return ErrNotFound
				}
				return err
			}
			saved = *id
		}
		version, err := q.CreateSkillVersion(ctx, sqlc.CreateSkillVersionParams{
			SkillID: saved, Description: p.Description, ContentHash: p.Hash, Checked: checked, CreatedBy: &by,
		})
		if err != nil {
			return err
		}
		paths, contents := make([]string, len(p.Files)), make([]string, len(p.Files))
		for i, f := range p.Files {
			paths[i], contents[i] = f.Path, f.Content
		}
		if err := q.InsertSkillFiles(ctx, sqlc.InsertSkillFilesParams{VersionID: version.ID, Paths: paths, Contents: contents}); err != nil {
			return err
		}
		if err := q.SetSkillVersion(ctx, sqlc.SetSkillVersionParams{ID: saved, VersionID: &version.ID, Name: p.Name}); err != nil {
			return err
		}
		return q.PruneSkillVersions(ctx, sqlc.PruneSkillVersionsParams{SkillID: saved, Keep: KeepVersions})
	})
	if err != nil {
		return Skill{}, err
	}
	skill, _, err := s.Get(ctx, owner, saved)
	return skill, err
}

func (s *Store) SetEnabled(ctx context.Context, owner *uuid.UUID, id uuid.UUID, enabled bool) error {
	_, err := s.q.SetSkillEnabled(ctx, sqlc.SetSkillEnabledParams{ID: id, OwnerID: owner, PluginID: s.plugin, Enabled: enabled})
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

func (s *Store) Delete(ctx context.Context, owner *uuid.UUID, id uuid.UUID) error {
	_, err := s.q.DeleteOwnedSkill(ctx, sqlc.DeleteOwnedSkillParams{ID: id, OwnerID: owner, PluginID: s.plugin})
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

// Version is a version a conversation of this person may read, with its files:
// one of a site skill or of their own. Gone - its skill deleted, or the version
// pruned - is ErrNotFound.
func (s *Store) Version(ctx context.Context, userID, versionID uuid.UUID) (string, []File, error) {
	row, err := s.q.GetSkillVersionFor(ctx, sqlc.GetSkillVersionForParams{VersionID: versionID, UserID: &userID})
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil, ErrNotFound
	}
	if err != nil {
		return "", nil, err
	}
	files, err := s.files(ctx, s.q, row.ID)
	return row.Name, files, err
}

// VersionText is a version's files as one text (Text), for a check made when a
// conversation would take the version into its snapshot.
func (s *Store) VersionText(ctx context.Context, versionID uuid.UUID) (string, error) {
	files, err := s.files(ctx, s.q, versionID)
	if err != nil {
		return "", err
	}
	return Text(files), nil
}

// Body is a SKILL.md without its frontmatter: what the model is given when it
// opens a skill, the name and description being in its list already.
func Body(entry string) string {
	_, body, err := Frontmatter(entry)
	if err != nil {
		return entry
	}
	return string(bytes.TrimLeft([]byte(body), "\n"))
}

// VersionInfo is one kept version of a skill.
type VersionInfo struct {
	ID          uuid.UUID
	Description string
	CreatedAt   time.Time
	Current     bool
}

// Versions are a skill's kept versions, newest first; ErrNotFound when the
// skill is not this owner's.
func (s *Store) Versions(ctx context.Context, owner *uuid.UUID, id uuid.UUID) ([]VersionInfo, error) {
	rows, err := s.q.ListSkillVersions(ctx, sqlc.ListSkillVersionsParams{SkillID: id, OwnerID: owner, PluginID: s.plugin})
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, ErrNotFound
	}
	out := make([]VersionInfo, len(rows))
	for i, r := range rows {
		out[i] = VersionInfo{ID: r.ID, Description: r.Description, CreatedAt: r.CreatedAt.Time, Current: r.Current}
	}
	return out, nil
}

// OwnedVersion is the files of one version of a skill of this owner, to look
// at or to save again as the current one.
func (s *Store) OwnedVersion(ctx context.Context, owner *uuid.UUID, id, versionID uuid.UUID) ([]File, error) {
	if _, err := s.q.GetOwnedSkillVersion(ctx, sqlc.GetOwnedSkillVersionParams{VersionID: versionID, SkillID: id, OwnerID: owner, PluginID: s.plugin}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return s.files(ctx, s.q, versionID)
}

// mentionLine is one skill a person picked for a message with / in the composer:
// a line `<skill name="…"/>` at the message's start (chat-core/src/skill-mention.ts).
var mentionLine = regexp.MustCompile(`^<skill name="([a-z0-9]+(?:-[a-z0-9]+)*)"/>\n`)

// SplitMentions is a message's picked skills, in order, and the rest of it: the
// words the person wrote, for whatever reads them as such - a title, a fallback
// title - rather than as the model's instructions.
func SplitMentions(content string) (names []string, rest string) {
	rest = content
	for {
		m := mentionLine.FindStringSubmatch(rest)
		if m == nil {
			return names, rest
		}
		names = append(names, m[1])
		rest = rest[len(m[0]):]
	}
}
