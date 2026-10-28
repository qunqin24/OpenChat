package agent

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/qunqin24/openchat/apps/server/internal/llm"
)

// longMarkdown is a Markdown file well past the inline limit: a heading every
// fifty lines, one inside a code fence that is not one.
func longMarkdown() string {
	var b strings.Builder
	for i := 1; i <= 1200; i++ {
		switch {
		case i%50 == 1:
			fmt.Fprintf(&b, "## Section %d\n", i/50+1)
		case i == 120:
			b.WriteString("```\n")
		case i == 121:
			b.WriteString("# not a heading\n")
		case i == 122:
			b.WriteString("```\n")
		case i == 777:
			b.WriteString("The revenue grew by 12 percent.\n")
		default:
			fmt.Fprintf(&b, "Line %d of the report, padded with some words to look like prose.\n", i)
		}
	}
	return b.String()
}

func TestALongTextFileGoesAsACardWhenTheToolsAreOffered(t *testing.T) {
	s := newStore()
	text := longMarkdown()
	long := s.add("report.md", llm.TextMediaType, []byte(text))
	short := s.add("notes.txt", llm.TextMediaType, []byte("a short note\n"))
	if len(text) <= llm.InlineTextBytes {
		t.Fatalf("the file is only %d bytes", len(text))
	}

	r := s.resolver(MaxAttachmentBytes)
	r.cards = true
	out, err := r.resolve(context.Background(), []llm.Message{userWith("summarise", long, short)})
	if err != nil {
		t.Fatal(err)
	}
	card := out[0].Parts[1].(llm.TextPart).Text
	for _, want := range []string{
		`<file name="report.md" id="` + long.Ref + `" lines="1200"`, `partial="true"`, "file_read", "[Lines 1-40 of 1200]",
		"     1\t## Section 1", "51: ## Section 2", "1151: ## Section 24",
	} {
		if !strings.Contains(card, want) {
			t.Errorf("card lacks %q:\n%s", want, card)
		}
	}
	if strings.Contains(card, "not a heading") || strings.Contains(card, "Line 41 ") || len(card) > llm.FileCardBytes {
		t.Errorf("card shows too much (%d bytes):\n%s", len(card), card)
	}
	if got := out[0].Parts[2].(llm.TextPart).Text; !strings.Contains(got, "a short note") || strings.Contains(got, "partial") {
		t.Errorf("a short file is sent whole: %q", got)
	}

	// Without the tools, the file goes whole.
	r = s.resolver(MaxAttachmentBytes)
	out, _ = r.resolve(context.Background(), []llm.Message{userWith("summarise", long)})
	if whole := out[0].Parts[1].(llm.TextPart).Text; !strings.Contains(whole, "The revenue grew") || strings.Contains(whole, "partial") {
		t.Error("without the tools the file is not sent whole")
	}
}

func TestReadingAndSearchingAFile(t *testing.T) {
	file := llm.AttachmentPart{Ref: "f1", Filename: "report.md"}
	lines := splitLines(longMarkdown())

	result, err := readLines(file, lines, 770, 780)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Content, "   777\tThe revenue grew") || !strings.Contains(result.Content, "lines 770-780 of 1200") ||
		strings.Contains(result.Content, "   781\t") {
		t.Errorf("read:\n%s", result.Content)
	}
	if d := result.Details.(FileReadDetails); d.StartLine != 770 || d.EndLine != 780 || d.TotalLines != 1200 {
		t.Errorf("details %+v", d)
	}
	// A whole read stops at the call's share and says where to go on.
	result, _ = readLines(file, lines, 0, 0)
	if d := result.Details.(FileReadDetails); d.EndLine >= 1200 || !strings.Contains(result.Content, fmt.Sprintf("start_line=%d", d.EndLine+1)) {
		t.Errorf("a long read: %+v\n%s", d, result.Content[len(result.Content)-200:])
	}
	if _, err := readLines(file, lines, 1300, 0); err == nil {
		t.Error("a start past the end is read")
	}

	var b strings.Builder
	writeHits(&b, lines, []lineSpan{{776, 776}, {778, 778}, {900, 900}})
	got := b.String()
	for _, want := range []string{"775-Line 775", "777:The revenue grew", "779:Line 779", "781-Line 781", "--\n899-", "901:## Section 19"} {
		if !strings.Contains(got, want) {
			t.Errorf("hits lack %q:\n%s", want, got)
		}
	}
	if strings.Count(got, "778-") != 1 {
		t.Errorf("a line beside two matches is written twice:\n%s", got)
	}
}

// A PDF's text breaks lines where the page did: in the middle of a Chinese
// phrase, of an English one, and of a hyphenated word, whose hyphen goes.
func TestSearchReadsAcrossLineBreaks(t *testing.T) {
	lines := splitLines("二〇二五年，全年营收增长达到百\n分之三十八，其中企业订阅贡献了超过一半。\n" +
		"Revenue growth was driven by large-\nscale deployments across financial\ninstitutions, with ＡＰＩ calls up １２％.\n\nThe end.")
	index := indexLines(lines)
	for query, want := range map[string]lineSpan{
		"百分之三十八":                  {0, 1},
		"largescale":              {2, 3},
		"large-scale deployments": {2, 3},
		"financial institutions":  {3, 4},
		"API calls up 12%":        {4, 4},
		"REVENUE  GROWTH":         {2, 2},
		"the end":                 {6, 6},
	} {
		spans := index.find(fold(query))
		if len(spans) != 1 || spans[0] != want {
			t.Errorf("%q: %v, want %v", query, spans, want)
		}
	}
	if spans := index.find(fold("五十八")); len(spans) != 0 {
		t.Errorf("found what is not there: %v", spans)
	}
	var b strings.Builder
	writeHits(&b, lines, index.find(fold("百分之三十八")))
	if got := b.String(); !strings.HasPrefix(got, "1:二〇二五年") || !strings.Contains(got, "2:分之三十八") {
		t.Errorf("a match over two lines:\n%s", got)
	}
}

func TestPickFileByIDOrName(t *testing.T) {
	files := []llm.AttachmentPart{{Ref: "a", Filename: "Report.md"}, {Ref: "b", Filename: "data.csv"}, {Ref: "c", Filename: "data.csv"}}
	if file, err := pickFile(files, "report.md"); err != nil || file.Ref != "a" {
		t.Errorf("by name: %+v %v", file, err)
	}
	if file, err := pickFile(files, "c"); err != nil || file.Ref != "c" {
		t.Errorf("by id: %+v %v", file, err)
	}
	if _, err := pickFile(files, "data.csv"); err == nil || !strings.Contains(err.Error(), "id b") {
		t.Errorf("two of a name: %v", err)
	}
	if _, err := pickFile(files, "nope.txt"); err == nil {
		t.Error("an unknown file was picked")
	}
}
