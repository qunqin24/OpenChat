package skills

import (
	"archive/zip"
	"bytes"
	"io"
	"path"
	"sort"
	"strings"
	"unicode/utf8"
)

// MaxArchiveBytes bounds an uploaded archive, and MaxUnpackedBytes what is read
// out of it: a skill is a few files of text, and an archive that inflates past
// this is not one.
const (
	MaxArchiveBytes  = 8 << 20
	MaxUnpackedBytes = 16 << 20
)

// Unpacked is what an upload held: the files of each skill in it, and the files
// that were left out because they are not text (kept once there is a sandbox).
type Unpacked struct {
	Skills  [][]File
	Skipped []string
}

// Unpack reads an upload: a single SKILL.md (any .md file is taken as one), or
// a ZIP archive (.zip, or a .skill, which is a ZIP) holding one skill with
// SKILL.md at its root or several skill folders that each have one - the
// layouts Open WebUI and Claude export.
func Unpack(filename string, data []byte) (Unpacked, error) {
	if strings.HasSuffix(strings.ToLower(filename), ".md") {
		return Unpacked{Skills: [][]File{{{Path: Entry, Content: string(data)}}}}, nil
	}
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return Unpacked{}, invalid("skill_archive", "the file is neither a SKILL.md nor a ZIP archive")
	}

	type entry struct{ path, content string }
	var (
		entries []entry
		skipped []string
		read    int64
	)
	for _, f := range reader.File {
		name := strings.ReplaceAll(f.Name, "\\", "/")
		if f.FileInfo().IsDir() || hidden(name) {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return Unpacked{}, invalid("skill_archive", "%s cannot be read from the archive", name)
		}
		// Read to the remaining allowance plus one byte, to tell a file that
		// fits from one that is cut off.
		body, err := io.ReadAll(io.LimitReader(rc, MaxUnpackedBytes-read+1))
		rc.Close()
		if err != nil {
			return Unpacked{}, invalid("skill_archive", "%s cannot be read from the archive", name)
		}
		read += int64(len(body))
		if read > MaxUnpackedBytes {
			return Unpacked{}, invalid("skill_archive", "the archive unpacks to more than %d MB", MaxUnpackedBytes>>20)
		}
		if !utf8.Valid(body) || bytes.IndexByte(body, 0) >= 0 {
			skipped = append(skipped, name)
			continue
		}
		entries = append(entries, entry{path: name, content: string(body)})
	}

	// Each SKILL.md marks a skill's root; a file belongs to the deepest root
	// above it. Files under no root are left out.
	var roots []string
	for _, e := range entries {
		if path.Base(e.path) == Entry {
			roots = append(roots, path.Dir(e.path))
		}
	}
	if len(roots) == 0 {
		return Unpacked{}, invalid("skill_missing_entry", "the archive has no %s", Entry)
	}
	sort.Slice(roots, func(i, j int) bool { return len(roots[i]) > len(roots[j]) })
	groups := map[string][]File{}
	for _, e := range entries {
		for _, root := range roots {
			rel, ok := under(e.path, root)
			if ok {
				groups[root] = append(groups[root], File{Path: rel, Content: e.content})
				break
			}
		}
	}
	sort.Strings(roots)
	out := Unpacked{Skipped: skipped}
	for _, root := range roots {
		out.Skills = append(out.Skills, groups[root])
	}
	return out, nil
}

// under is p relative to root, if p is inside it.
func under(p, root string) (string, bool) {
	if root == "." {
		return p, true
	}
	if strings.HasPrefix(p, root+"/") {
		return p[len(root)+1:], true
	}
	return "", false
}

// hidden is a file an archiver adds that is not part of a skill: macOS's
// resource forks and folder settings, and dot-files.
func hidden(name string) bool {
	for _, part := range strings.Split(name, "/") {
		if strings.HasPrefix(part, ".") || part == "__MACOSX" {
			return true
		}
	}
	return false
}
