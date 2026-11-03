package skills

import (
	"archive/zip"
	"bytes"
	"errors"
	"strings"
	"testing"
)

const entry = "---\nname: pdf-tools\ndescription: Fill and merge PDFs. Use when the user mentions PDFs.\nlicense: MIT\n---\n# PDF tools\n\nRead references/forms.md first.\n"

func TestParseReadsFrontmatterAndOrdersFiles(t *testing.T) {
	p, err := Parse([]File{
		{Path: "references/forms.md", Content: "forms\r\nhere"},
		{Path: "./SKILL.md", Content: entry},
	})
	if err != nil {
		t.Fatal(err)
	}
	if p.Name != "pdf-tools" || !strings.HasPrefix(p.Description, "Fill and merge") {
		t.Fatalf("meta = %q %q", p.Name, p.Description)
	}
	if p.Files[0].Path != "SKILL.md" || p.Files[1].Path != "references/forms.md" {
		t.Fatalf("files not in path order: %+v", p.Files)
	}
	if p.Files[1].Content != "forms\nhere" {
		t.Fatalf("line endings not normalised: %q", p.Files[1].Content)
	}
	if p.Hash != Hash(p.Text) || !strings.Contains(p.Text, "=== references/forms.md ===") {
		t.Fatalf("text/hash = %q %q", p.Text, p.Hash)
	}
	if body := Body(entry); !strings.HasPrefix(body, "# PDF tools") {
		t.Fatalf("body = %q", body)
	}
}

func TestParseRefuses(t *testing.T) {
	cases := map[string]struct {
		files []File
		code  string
	}{
		"no entry":       {[]File{{Path: "README.md", Content: "x"}}, "skill_missing_entry"},
		"no frontmatter": {[]File{{Path: "SKILL.md", Content: "# hi"}}, "skill_frontmatter"},
		"bad name":       {[]File{{Path: "SKILL.md", Content: "---\nname: PDF Tools\ndescription: x\n---\n"}}, "skill_name"},
		"double hyphen":  {[]File{{Path: "SKILL.md", Content: "---\nname: pdf--tools\ndescription: x\n---\n"}}, "skill_name"},
		"no description": {[]File{{Path: "SKILL.md", Content: "---\nname: pdf\n---\n"}}, "skill_description"},
		"escaping path":  {[]File{{Path: "SKILL.md", Content: entry}, {Path: "../etc/passwd", Content: "x"}}, "skill_path"},
		"absolute path":  {[]File{{Path: "SKILL.md", Content: entry}, {Path: "/etc/passwd", Content: "x"}}, "skill_path"},
		"hidden file":    {[]File{{Path: "SKILL.md", Content: entry}, {Path: "a/.env", Content: "x"}}, "skill_path"},
		"binary":         {[]File{{Path: "SKILL.md", Content: entry}, {Path: "a.bin", Content: "a\x00b"}}, "skill_binary_file"},
		"duplicate":      {[]File{{Path: "SKILL.md", Content: entry}, {Path: "./SKILL.md", Content: entry}}, "skill_duplicate_file"},
		"too large":      {[]File{{Path: "SKILL.md", Content: entry + strings.Repeat("x", MaxFileChars)}}, "skill_file_too_large"},
	}
	for name, c := range cases {
		_, err := Parse(c.files)
		var bad *InvalidError
		if !errors.As(err, &bad) || bad.Code != c.code {
			t.Errorf("%s: err = %v, want %s", name, err, c.code)
		}
	}
}

func zipOf(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for name, content := range files {
		f, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestUnpackFindsEachSkillRoot(t *testing.T) {
	data := zipOf(t, map[string]string{
		"bundle/pdf-tools/SKILL.md":            entry,
		"bundle/pdf-tools/references/forms.md": "forms",
		"bundle/writer/SKILL.md":               "---\nname: writer\ndescription: Write.\n---\n",
		"bundle/writer/assets/logo.png":        "\x89PNG\x00\x00",
		"__MACOSX/bundle/._SKILL.md":           "junk",
		"bundle/.DS_Store":                     "junk",
	})
	out, err := Unpack("skills.zip", data)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Skills) != 2 {
		t.Fatalf("skills = %+v", out.Skills)
	}
	if len(out.Skipped) != 1 || out.Skipped[0] != "bundle/writer/assets/logo.png" {
		t.Fatalf("skipped = %v", out.Skipped)
	}
	for _, files := range out.Skills {
		if _, err := Parse(files); err != nil {
			t.Fatalf("unpacked skill does not parse: %v (%+v)", err, files)
		}
	}
}

func TestUnpackMarkdownAndRootLayout(t *testing.T) {
	out, err := Unpack("my-skill.md", []byte(entry))
	if err != nil || len(out.Skills) != 1 || out.Skills[0][0].Path != Entry {
		t.Fatalf("md: %+v %v", out, err)
	}
	out, err = Unpack("pdf.skill", zipOf(t, map[string]string{"SKILL.md": entry, "scripts/fill.py": "print(1)"}))
	if err != nil || len(out.Skills) != 1 || len(out.Skills[0]) != 2 {
		t.Fatalf("root layout: %+v %v", out, err)
	}
	if _, err := Unpack("x.zip", []byte("not a zip")); err == nil {
		t.Fatal("a non-archive was accepted")
	}
}

func TestBuiltinsParse(t *testing.T) {
	if len(Builtins()) == 0 {
		t.Fatal("no built-in skills")
	}
	for _, name := range []string{"skill-authoring", "doc-coauthoring", "internal-comms"} {
		b, ok := Builtin(name)
		if !ok || b.Description == "" || !hasFile(b.Files, Entry) {
			t.Fatalf("%s = %+v %v", name, b, ok)
		}
	}
	// The ones from anthropics/skills carry their licence.
	for _, name := range []string{"doc-coauthoring", "internal-comms"} {
		if b, _ := Builtin(name); !hasFile(b.Files, "LICENSE.txt") {
			t.Fatalf("%s has no LICENSE.txt", name)
		}
	}
}

func TestZipRoundTrips(t *testing.T) {
	files := []File{{Path: "SKILL.md", Content: entry}, {Path: "references/forms.md", Content: "forms"}}
	data, err := Zip("pdf-tools", files)
	if err != nil {
		t.Fatal(err)
	}
	out, err := Unpack("pdf-tools.zip", data)
	if err != nil || len(out.Skills) != 1 || len(out.Skills[0]) != 2 {
		t.Fatalf("unpacked = %+v %v", out, err)
	}
	if _, err := Parse(out.Skills[0]); err != nil {
		t.Fatal(err)
	}
}

func hasFile(files []File, path string) bool {
	for _, f := range files {
		if f.Path == path {
			return true
		}
	}
	return false
}

func TestSplitMentions(t *testing.T) {
	names, rest := SplitMentions("<skill name=\"doc-coauthoring\"/>\n<skill name=\"house-style\"/>\n写一篇文档")
	if strings.Join(names, ",") != "doc-coauthoring,house-style" || rest != "写一篇文档" {
		t.Fatalf("names %v, rest %q", names, rest)
	}
	if names, rest := SplitMentions("see <skill name=\"x\"/>\n"); names != nil || rest != "see <skill name=\"x\"/>\n" {
		t.Fatalf("a tag later in the text was taken: %v %q", names, rest)
	}
}
