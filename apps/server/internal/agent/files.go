package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"

	"github.com/google/uuid"

	"github.com/qunqin24/openchat/apps/server/internal/db/sqlc"
	"github.com/qunqin24/openchat/apps/server/internal/llm"
)

// A text file longer than llm.InlineTextBytes is not put before the model
// whole: it costs its full length on every later turn of the conversation,
// whatever the turn is about. The model is shown a card instead - the file's
// name, its length, its beginning and, for Markdown, its headings
// (fileCard) - and reads what a question needs with two tools: file_read, a
// range of lines, and file_search, the lines holding a phrase. The way a
// coding agent reads a repository, and Open WebUI's native mode reads files.
//
// The tools are offered only to a request whose conversation holds such a
// file and whose model calls tools; the attachment resolver makes cards only
// then, and sends the file whole otherwise. What they read is what the card
// stands for: the files of the person's own messages in this conversation,
// never a refused one's, through the same resolver - so the same receipt -
// as a file sent whole. The file's words were checked by content moderation
// when the message was sent, so a read is not checked again.

const (
	// ToolFileRead and ToolFileSearch are the file tools' names.
	ToolFileRead   = "file_read"
	ToolFileSearch = "file_search"

	// fileReadLines and fileReadBytes bound one read: a file read through
	// in ranges this large takes a few calls, not dozens.
	fileReadLines = 2000
	fileReadBytes = 48 << 10
	// fileLineChars cuts a line longer than this where a tool shows lines
	// around something (a card's beginning, a search's context); a search shows
	// this much of a long line around a match, and file_read reads a long line
	// in parts by offset.
	fileLineChars = 2000

	// fileSearchHits is how many matching lines a search shows; fileContext
	// how many lines around each.
	fileSearchHits = 30
	fileContext    = 2
	fileQueryChars = 200

	// cardPreviewLines and cardPreviewBytes bound the beginning a card shows,
	// cardHeadings and cardHeadingBytes its headings.
	cardPreviewLines  = 40
	cardPreviewBytes  = 4 << 10
	cardHeadings      = 80
	cardHeadingBytes  = 3 << 10
	cardHeadingLength = 120
)

// fileTools are the tools that read the long text files of a conversation.
func (r *Runner) fileTools() []Tool {
	return []Tool{fileTool{r: r}, fileTool{r: r, search: true}}
}

// hasLongFiles reports whether the conversation holds a file whose words are
// too long to send whole: a text file, or a document by the length of the
// words read out of it when it was sent (AttachmentPart.TextSize).
func (r *Runner) hasLongFiles(ctx context.Context, conversationID uuid.UUID) (bool, error) {
	files, err := r.conversationFiles(ctx, conversationID)
	if err != nil {
		return false, err
	}
	for _, file := range files {
		size := file.TextSize
		if file.MediaType == llm.TextMediaType {
			size = file.Size
		}
		if size > llm.InlineTextBytes {
			return true, nil
		}
	}
	return false, nil
}

type fileTool struct {
	r      *Runner
	search bool
}

// FileReadDetails is what the interface is shown of a read.
type FileReadDetails struct {
	File       string `json:"file"`
	Filename   string `json:"filename"`
	StartLine  int    `json:"startLine"`
	EndLine    int    `json:"endLine"`
	TotalLines int    `json:"totalLines"`
}

// FileSearchDetails is what the interface is shown of a search.
type FileSearchDetails struct {
	Query    string `json:"query"`
	Filename string `json:"filename,omitempty"`
	Matches  int    `json:"matches"`
	Files    int    `json:"files"`
}

func (t fileTool) Definition() llm.Tool {
	if t.search {
		return llm.Tool{
			Name: ToolFileSearch,
			Description: "Find the lines of the long text files the user attached (shown as <file ... partial=\"true\"> " +
				"cards) that contain a phrase: case-insensitive, matched literally, not as a pattern. Searches one file " +
				"(file: the card's id, or its name) or, without file, all of them. Returns up to 30 matching lines " +
				"with their line numbers and two lines around each. Use it to find where something is, then " +
				"file_read around the lines found. Try a shorter phrase or another wording - in the file's own " +
				"language - when nothing matches.",
			Parameters: json.RawMessage(`{
  "type": "object",
  "properties": {
    "query": {"type": "string", "description": "The phrase to find."},
    "file": {"type": "string", "description": "The card's id, or the file's name. All files when absent."}
  },
  "required": ["query"]
}`),
		}
	}
	return llm.Tool{
		Name: ToolFileRead,
		Description: "Read a range of lines of a long text file the user attached, shown in the conversation as a " +
			"<file ... partial=\"true\"> card with only its beginning. Lines come back numbered; at most 2,000 lines " +
			"or about 48 KB per call, and the result says where to continue. A line longer than one call reads " +
			"(minified JSON or HTML, say) comes back in parts: continue it with the same start_line and the offset " +
			"the result gives. Read what the question needs - found with file_search, or by the card's headings - " +
			"or, to work with the whole file, read it through in consecutive ranges.",
		Parameters: json.RawMessage(`{
  "type": "object",
  "properties": {
    "file": {"type": "string", "description": "The card's id, or the file's name."},
    "start_line": {"type": "integer", "minimum": 1, "description": "The first line to read, from 1. 1 when absent."},
    "end_line": {"type": "integer", "minimum": 1, "description": "The last line to read. As far as one call goes when absent."},
    "offset": {"type": "integer", "minimum": 0, "description": "How many characters of start_line to skip, to read on in a line too long for one call. 0 when absent."}
  },
  "required": ["file"]
}`),
	}
}

// Replayable is true: the tools only read.
func (fileTool) Replayable() bool { return true }

type fileToolArgs struct {
	File      string `json:"file"`
	Query     string `json:"query"`
	StartLine int    `json:"start_line"`
	EndLine   int    `json:"end_line"`
	Offset    int    `json:"offset"`
}

func (t fileTool) Execute(ctx context.Context, raw json.RawMessage) (ToolResult, error) {
	scope, ok := ScopeFrom(ctx)
	if !ok {
		return ToolResult{}, errors.New("file tool: no run in context")
	}
	var args fileToolArgs
	if err := DecodeArgs(raw, &args); err != nil {
		return ToolResult{}, err
	}
	files, err := t.r.conversationFiles(ctx, scope.ConversationID)
	if err != nil {
		return ToolResult{}, err
	}
	if len(files) == 0 {
		return ToolResult{}, Errorf("this conversation has no text files")
	}
	if t.search {
		return t.find(ctx, scope, files, args)
	}
	file, err := pickFile(files, args.File)
	if err != nil {
		return ToolResult{}, err
	}
	lines, err := t.r.fileLines(ctx, scope, file)
	if err != nil {
		return ToolResult{}, err
	}
	return readLines(file, lines, args.StartLine, args.EndLine, args.Offset)
}

// conversationFiles is the files of the person's messages in a conversation
// that are read as words - text files and documents - each once, oldest first.
func (r *Runner) conversationFiles(ctx context.Context, conversationID uuid.UUID) ([]llm.AttachmentPart, error) {
	rows, err := r.q.ListConversationTextFileMessages(ctx, conversationID)
	if err != nil {
		return nil, err
	}
	var files []llm.AttachmentPart
	seen := map[string]bool{}
	for _, row := range rows {
		parts, err := DecodeParts(row.Parts)
		if err != nil {
			continue
		}
		for _, part := range parts {
			ref, ok := part.(llm.AttachmentPart)
			if !ok || seen[ref.Ref] {
				continue
			}
			if modality := llm.ModalityOf(ref.MediaType); modality == llm.ModalityText || modality == llm.ModalityDocument || modality == llm.ModalityPDF {
				seen[ref.Ref] = true
				files = append(files, ref)
			}
		}
	}
	return files, nil
}

// pickFile is the file a call names, by the id on its card or by its name.
func pickFile(files []llm.AttachmentPart, name string) (llm.AttachmentPart, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return llm.AttachmentPart{}, Errorf("name the file: the id on its card, or its name")
	}
	var named []llm.AttachmentPart
	for _, file := range files {
		if file.Ref == name {
			return file, nil
		}
		if strings.EqualFold(file.Filename, name) {
			named = append(named, file)
		}
	}
	switch len(named) {
	case 1:
		return named[0], nil
	case 0:
		return llm.AttachmentPart{}, Errorf("no text file %q in this conversation; its files are: %s", name, fileList(files))
	}
	return llm.AttachmentPart{}, Errorf("several files are named %q; give the id instead: %s", name, fileList(named))
}

func fileList(files []llm.AttachmentPart) string {
	names := make([]string, len(files))
	for i, file := range files {
		names[i] = fmt.Sprintf("%s (id %s)", file.Filename, file.Ref)
	}
	return strings.Join(names, ", ")
}

// fileKey is where a run keeps a file's lines once read.
type fileKey struct{ ref string }

// fileLines is a file's lines, read once per run, through the resolver and so
// with its receipt checked.
func (r *Runner) fileLines(ctx context.Context, scope Scope, file llm.AttachmentPart) ([]string, error) {
	if kept, ok := scope.State.Load(fileKey{file.Ref}); ok {
		return kept.([]string), nil
	}
	resolver := &attachmentResolver{
		lookup: func(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]sqlc.Attachment, error) {
			rows, err := r.q.ListAttachmentsForUser(ctx, sqlc.ListAttachmentsForUserParams{Ids: ids, UserID: &scope.UserID})
			if err != nil {
				return nil, err
			}
			byID := make(map[uuid.UUID]sqlc.Attachment, len(rows))
			for _, row := range rows {
				byID[row.ID] = row
			}
			return byID, nil
		},
		accepts: func(string) bool { return false },
		budget:  MaxAttachmentBytes,
		asText:  true,
	}
	if r.opts.Blobs != nil {
		resolver.read = r.opts.Blobs.Read
	}
	id, err := uuid.Parse(file.Ref)
	if err != nil {
		return nil, Errorf("the file %s is no longer available", file.Filename)
	}
	rows, err := resolver.lookup(ctx, []uuid.UUID{id})
	if err != nil {
		return nil, err
	}
	resolved, note := resolver.one(ctx, file, rows, &resolver.budget)
	if note != "" {
		return nil, Errorf("%s cannot be read: %s", file.Filename, note)
	}
	text, _ := llm.FileText(resolved.Data)
	lines := splitLines(text)
	scope.State.Store(fileKey{file.Ref}, lines)
	return lines, nil
}

// splitLines is text's lines, without their endings; a final line ending
// starts no line of its own.
func splitLines(text string) []string {
	lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	for i, line := range lines {
		lines[i] = strings.TrimSuffix(line, "\r")
	}
	return lines
}

// numbered writes a line as the tools show it: its number, a tab, its text,
// cut where it is very long.
func numbered(b *strings.Builder, number int, line string) {
	fmt.Fprintf(b, "%6d\t%s\n", number, cutLine(line))
}

// cutLine is a line as shown beside others: at most fileLineChars characters.
func cutLine(line string) string {
	if runes := []rune(line); len(runes) > fileLineChars {
		return string(runes[:fileLineChars]) + " [line cut: file_read reads on with offset]"
	}
	return line
}

// readLines is file_read: lines start to end, as many as one call reads. A
// line that does not fit in one call on its own comes back in parts, from
// offset characters in: the part that fits, and where to go on from.
func readLines(file llm.AttachmentPart, lines []string, start, end, offset int) (ToolResult, error) {
	total := len(lines)
	if start <= 0 {
		start = 1
	}
	if start > total {
		return ToolResult{}, Errorf("%s has %d lines; start_line %d is past its end", file.Filename, total, start)
	}
	if end <= 0 || end > total {
		end = total
	}
	if end < start {
		return ToolResult{}, Errorf("end_line %d comes before start_line %d", end, start)
	}
	first := lines[start-1]
	firstChars := utf8.RuneCountInString(first)
	if offset < 0 || (offset > 0 && offset >= firstChars) {
		return ToolResult{}, Errorf("line %d has %d characters; offset %d is past its end", start, firstChars, offset)
	}
	var b strings.Builder
	last := start - 1
	cutAt := 0 // where the last line shown was cut, in characters; 0 when it was shown to its end
	for n := start; n <= end && n-start < fileReadLines; n++ {
		line, from := lines[n-1], 0
		if n == start {
			line, from = string([]rune(first)[offset:]), offset
		}
		if b.Len()+len(line) > fileReadBytes {
			if b.Len() > 0 {
				break
			}
			part := fitBytes(line, fileReadBytes)
			cutAt = from + utf8.RuneCountInString(part)
			line = part
		}
		fmt.Fprintf(&b, "%6d\t%s\n", n, line)
		last = n
		if cutAt > 0 {
			break
		}
	}
	fmt.Fprintf(&b, "[%s, lines %d-%d of %d.", file.Filename, start, last, total)
	if offset > 0 {
		fmt.Fprintf(&b, " Line %d from character %d.", start, offset)
	}
	if cutAt > 0 {
		fmt.Fprintf(&b, " Line %d is longer than one call reads: shown up to character %d of %d; continue with "+
			"start_line=%d, offset=%d.", last, cutAt, utf8.RuneCountInString(lines[last-1]), last, cutAt)
	} else if last < end {
		fmt.Fprintf(&b, " One call reads at most %d lines or %d KB: continue with start_line=%d.", fileReadLines, fileReadBytes>>10, last+1)
	} else if last < total {
		fmt.Fprintf(&b, " The file goes on to line %d.", total)
	}
	b.WriteString("]")
	return ToolResult{Content: b.String(), Details: FileReadDetails{
		File: file.Ref, Filename: file.Filename, StartLine: start, EndLine: last, TotalLines: total,
	}}, nil
}

// fitBytes is the longest beginning of text, whole characters, of at most limit bytes.
func fitBytes(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	cut := limit
	for cut > 0 && !utf8.RuneStart(text[cut]) {
		cut--
	}
	return text[:cut]
}

func (t fileTool) find(ctx context.Context, scope Scope, files []llm.AttachmentPart, args fileToolArgs) (ToolResult, error) {
	query := strings.TrimSpace(args.Query)
	if query == "" {
		return ToolResult{}, Errorf("give a phrase to find (query)")
	}
	if len([]rune(query)) > fileQueryChars {
		return ToolResult{}, Errorf("the phrase is too long: at most %d characters", fileQueryChars)
	}
	needle := fold(query)
	if needle == "" {
		return ToolResult{}, Errorf("the phrase has nothing to find: give some letters or digits")
	}
	details := FileSearchDetails{Query: query}
	if args.File != "" {
		file, err := pickFile(files, args.File)
		if err != nil {
			return ToolResult{}, err
		}
		files, details.Filename = []llm.AttachmentPart{file}, file.Filename
	}
	var b strings.Builder
	shown := 0
	for _, file := range files {
		lines, err := t.r.fileLines(ctx, scope, file)
		if err != nil {
			// One unreadable file does not spoil a search of all of them.
			if len(files) == 1 {
				return ToolResult{}, err
			}
			fmt.Fprintf(&b, "%s (id %s): %v\n\n", file.Filename, file.Ref, err)
			continue
		}
		index := fileIndexOf(scope, file, lines)
		hits := index.find(needle)
		details.Files++
		details.Matches += len(hits)
		if len(hits) == 0 {
			continue
		}
		room := fileSearchHits - shown
		if room <= 0 {
			fmt.Fprintf(&b, "%s (id %s): %d more matches, not shown\n\n", file.Filename, file.Ref, len(hits))
			continue
		}
		fmt.Fprintf(&b, "%s (id %s): %d matches", file.Filename, file.Ref, len(hits))
		if len(hits) > room {
			fmt.Fprintf(&b, ", the first %d shown", room)
			hits = hits[:room]
		}
		b.WriteString("\n")
		shown += len(hits)
		writeHits(&b, lines, index, hits)
		b.WriteString("\n")
	}
	if details.Matches == 0 {
		return ToolResult{Content: fmt.Sprintf("Nothing matches %q. Try a shorter phrase, another wording, or the "+
			"file's own language.", query), Details: details}, nil
	}
	b.WriteString("[Lines holding a match are marked with \":\" - a match can run over two lines - and the lines " +
		"around them with \"-\". Read more with file_read.]")
	return ToolResult{Content: b.String(), Details: details}, nil
}

// A search compares words as a reader would, not character for character: a
// PDF's text breaks its lines where the page did, in the middle of a Chinese
// phrase ("百\n分之三十八") or of an English one, and drops the hyphen of a
// word broken at a line's end ("large-\nscale" reads "largescale"). So the
// file and the phrase are both folded (fold) - compatibility forms made plain
// (full-width "１２％" is "12%"), lower case, and without white space or
// hyphens - and the phrase is found in the file's folded text as one string,
// whose matches are then traced back to the lines they lie on.

// fileIndex is a file's folded text, with where each of its lines starts in it.
type fileIndex struct {
	folded string
	starts []int
}

// fileIndexKey is where a run keeps a file's index once made.
type fileIndexKey struct{ ref string }

func fileIndexOf(scope Scope, file llm.AttachmentPart, lines []string) *fileIndex {
	if kept, ok := scope.State.Load(fileIndexKey{file.Ref}); ok {
		return kept.(*fileIndex)
	}
	index := indexLines(lines)
	scope.State.Store(fileIndexKey{file.Ref}, index)
	return index
}

func indexLines(lines []string) *fileIndex {
	var b strings.Builder
	starts := make([]int, len(lines))
	for i, line := range lines {
		starts[i] = b.Len()
		b.WriteString(fold(line))
	}
	return &fileIndex{folded: b.String(), starts: starts}
}

// fold is text as a search compares it.
func fold(text string) string {
	text = strings.ToLower(norm.NFKC.String(text))
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) || unicode.Is(unicode.Hyphen, r) || r == '-' || r == '\u2010' || r == '\u2011' || r == '\u00ad' {
			return -1
		}
		return r
	}, text)
}

// lineSpan is the lines a match lies on: first to last, from 0; at is where it
// starts in the folded text.
type lineSpan struct{ first, last, at int }

// find is where needle (folded) occurs, each match once, in order.
func (x *fileIndex) find(needle string) []lineSpan {
	var spans []lineSpan
	for from := 0; from < len(x.folded); {
		at := strings.Index(x.folded[from:], needle)
		if at < 0 {
			break
		}
		start := from + at
		end := start + len(needle)
		spans = append(spans, lineSpan{x.lineAt(start), x.lineAt(end - 1), start})
		from = end
	}
	return spans
}

// lineAt is the line the folded text's byte at offset belongs to: the last
// line starting at or before it (a blank line starts where the next does).
func (x *fileIndex) lineAt(offset int) int {
	return sort.Search(len(x.starts), func(i int) bool { return x.starts[i] > offset }) - 1
}

// writeHits writes the lines of each match with the lines around them, grep's
// way: "12:" a line holding a match, "11-" a line beside one, "--" between
// groups. A very long line a match starts on is shown around the match, not
// from its beginning, with the offset file_read reads it from.
func writeHits(b *strings.Builder, lines []string, index *fileIndex, hits []lineSpan) {
	match := map[int]bool{}
	startsAt := map[int]int{} // a line's first match, in its folded text
	for _, hit := range hits {
		for i := hit.first; i <= hit.last; i++ {
			match[i] = true
		}
		if _, ok := startsAt[hit.first]; !ok {
			startsAt[hit.first] = hit.at - index.starts[hit.first]
		}
	}
	next := 0 // the first line not yet written
	for _, hit := range hits {
		from := max(hit.first-fileContext, next)
		if from > next && next > 0 {
			b.WriteString("--\n")
		}
		to := min(hit.last+fileContext, len(lines)-1)
		for i := from; i <= to; i++ {
			mark := "-"
			if match[i] {
				mark = ":"
			}
			line := cutLine(lines[i])
			if at, ok := startsAt[i]; ok {
				line = around(lines[i], at)
			}
			b.WriteString(strconv.Itoa(i+1) + mark + line + "\n")
		}
		next = max(next, to+1)
	}
}

// around is a matching line as a search shows it: whole when it is short;
// otherwise fileLineChars characters about the match, which starts at folded
// (bytes into the line's folded text), and where they start.
func around(line string, folded int) string {
	runes := []rune(line)
	if len(runes) <= fileLineChars {
		return line
	}
	// The character the match starts at: the first whose folded text reaches past folded.
	pos, seen := 0, 0
	for pos < len(runes) {
		seen += len(fold(string(runes[pos])))
		if seen > folded {
			break
		}
		pos++
	}
	from := max(0, min(pos-fileLineChars/4, len(runes)-fileLineChars))
	to := from + fileLineChars
	var b strings.Builder
	fmt.Fprintf(&b, "[characters %d-%d of %d; file_read offset=%d reads from here] ", from, to, len(runes), from)
	if from > 0 {
		b.WriteString("…")
	}
	b.WriteString(string(runes[from:to]))
	if to < len(runes) {
		b.WriteString("…")
	}
	return b.String()
}

// fileCard is what the model is shown of a long text file: how to read it,
// its first lines, and, for Markdown, its headings with their line numbers.
func fileCard(file llm.AttachmentPart, text string) string {
	lines := splitLines(text)
	var b strings.Builder
	b.WriteString("This file is too long to include whole: only its beginning is shown. Read any part of it with " +
		"file_read and find passages with file_search before answering about what is not shown here.")
	switch llm.ModalityOf(file.MediaType) {
	case llm.ModalityPDF:
		fmt.Fprintf(&b, " It is the text of a %d-page PDF: each page starts with a line like [Page 3]; tables, "+
			"charts and pictures are not in it.", file.Pages)
	case llm.ModalityDocument:
		b.WriteString(" It is the text read out of the document: pictures are not in it.")
	}
	b.WriteString("\n\n")
	var preview strings.Builder
	shown := 0
	for shown < len(lines) && shown < cardPreviewLines {
		if preview.Len()+len(lines[shown]) > cardPreviewBytes && shown > 0 {
			break
		}
		numbered(&preview, shown+1, lines[shown])
		shown++
	}
	fmt.Fprintf(&b, "[Lines 1-%d of %d]\n%s", shown, len(lines), preview.String())
	if headings := markdownHeadings(file.Filename, lines); headings != "" {
		b.WriteString("\n[Headings, with their line numbers]\n" + headings)
	}
	return llm.FileCard(file.Filename, file.Ref, len(lines), len(text), b.String())
}

// markdownHeadings lists a Markdown file's headings, outside code fences, as
// "line: heading", within the card's share; "" for any other file.
func markdownHeadings(filename string, lines []string) string {
	switch strings.ToLower(path.Ext(filename)) {
	case ".md", ".markdown", ".mdx":
	default:
		return ""
	}
	var b strings.Builder
	fenced, listed, more := false, 0, 0
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			fenced = !fenced
			continue
		}
		level := len(trimmed) - len(strings.TrimLeft(trimmed, "#"))
		if fenced || level == 0 || level > 6 || (len(trimmed) > level && trimmed[level] != ' ') {
			continue
		}
		if runes := []rune(trimmed); len(runes) > cardHeadingLength {
			trimmed = string(runes[:cardHeadingLength]) + "…"
		}
		entry := strconv.Itoa(i+1) + ": " + trimmed + "\n"
		if listed >= cardHeadings || b.Len()+len(entry) > cardHeadingBytes {
			more++
			continue
		}
		b.WriteString(entry)
		listed++
	}
	if more > 0 {
		fmt.Fprintf(&b, "(%d more headings not listed; find them with file_search)\n", more)
	}
	return b.String()
}
