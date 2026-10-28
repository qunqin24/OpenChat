package doctext

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"io"
	"regexp"
	"strconv"
	"strings"
)

// A .docx is a zip of XML parts. The words are in word/document.xml: a body of
// paragraphs (w:p), made of runs (w:r) of text (w:t), and tables (w:tbl) of
// rows (w:tr) of cells (w:tc). A paragraph's style says whether it is a
// heading - by the style's outline level or its name, "heading 2", in
// word/styles.xml - and a numbering property makes it a list item. That much
// is kept, as Markdown; pictures, headers and footers, comments and deleted
// revisions are left out.

// docxPartBytes bounds one XML part as unzipped: a zip that unpacks to more is
// not a document someone wrote.
const docxPartBytes = 64 << 20

// node is an XML element, as much of it as the reading needs.
type node struct {
	name     string // local name, without the namespace
	attrs    map[string]string
	children []*node
	text     string // the character data of a w:t
}

func (n *node) attr(name string) string { return n.attrs[name] }

func (n *node) child(name string) *node {
	for _, child := range n.children {
		if child.name == name {
			return child
		}
	}
	return nil
}

func docx(data []byte) (Text, error) {
	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return Text{}, ErrUnreadable
	}
	document, err := zipPart(archive, "word/document.xml")
	if err != nil {
		return Text{}, ErrUnreadable
	}
	levels := map[string]int{}
	if styles, err := zipPart(archive, "word/styles.xml"); err == nil {
		levels = headingStyles(styles)
	}
	body := document.child("body")
	if body == nil {
		return Text{}, ErrUnreadable
	}
	var b strings.Builder
	writeBlocks(&b, body.children, levels)
	return Text{Content: strings.TrimSpace(collapseBlankLines(b.String())) + "\n"}, nil
}

// zipPart parses one XML part of the archive.
func zipPart(archive *zip.Reader, name string) (*node, error) {
	for _, file := range archive.File {
		if file.Name != name {
			continue
		}
		reader, err := file.Open()
		if err != nil {
			return nil, err
		}
		defer reader.Close()
		return parseXML(io.LimitReader(reader, docxPartBytes))
	}
	return nil, io.ErrUnexpectedEOF
}

// parseXML builds the element tree, keeping the text of w:t (and w:delText,
// which the walk skips) only: everything else between tags is layout.
func parseXML(r io.Reader) (*node, error) {
	decoder := xml.NewDecoder(r)
	root := &node{name: "#root"}
	stack := []*node{root}
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		switch t := token.(type) {
		case xml.StartElement:
			element := &node{name: t.Name.Local}
			if len(t.Attr) > 0 {
				element.attrs = make(map[string]string, len(t.Attr))
				for _, a := range t.Attr {
					element.attrs[a.Name.Local] = a.Value
				}
			}
			parent := stack[len(stack)-1]
			parent.children = append(parent.children, element)
			stack = append(stack, element)
		case xml.EndElement:
			if len(stack) > 1 {
				stack = stack[:len(stack)-1]
			}
		case xml.CharData:
			if top := stack[len(stack)-1]; top.name == "t" || top.name == "delText" {
				top.text += string(t)
			}
		}
	}
	if len(root.children) == 0 {
		return nil, io.ErrUnexpectedEOF
	}
	return root.children[0], nil
}

var headingName = regexp.MustCompile(`(?i)^heading\s*([1-9])$`)

// headingStyles is the heading level of each paragraph style that is one, by
// its outline level or else its name ("heading 2", "Title").
func headingStyles(styles *node) map[string]int {
	levels := map[string]int{}
	for _, style := range styles.children {
		if style.name != "style" || style.attr("type") != "paragraph" {
			continue
		}
		id := style.attr("styleId")
		if pPr := style.child("pPr"); pPr != nil {
			if outline := pPr.child("outlineLvl"); outline != nil {
				if level, err := strconv.Atoi(outline.attr("val")); err == nil && level >= 0 && level < 6 {
					levels[id] = level + 1
					continue
				}
			}
		}
		if name := style.child("name"); name != nil {
			value := strings.TrimSpace(name.attr("val"))
			if match := headingName.FindStringSubmatch(value); match != nil {
				level, _ := strconv.Atoi(match[1])
				levels[id] = min(level, 6)
			} else if strings.EqualFold(value, "title") {
				levels[id] = 1
			}
		}
	}
	return levels
}

// writeBlocks writes a body's paragraphs and tables, in order.
func writeBlocks(b *strings.Builder, blocks []*node, levels map[string]int) {
	for _, block := range blocks {
		switch block.name {
		case "p":
			writeParagraph(b, block, levels)
		case "tbl":
			writeTable(b, block)
		case "sdt":
			// A content control: its content is part of the body.
			if content := block.child("sdtContent"); content != nil {
				writeBlocks(b, content.children, levels)
			}
		}
	}
}

func writeParagraph(b *strings.Builder, p *node, levels map[string]int) {
	text := strings.TrimSpace(inlineText(p))
	if text == "" {
		b.WriteString("\n")
		return
	}
	if pPr := p.child("pPr"); pPr != nil {
		if style := pPr.child("pStyle"); style != nil {
			if level := levels[style.attr("val")]; level > 0 {
				b.WriteString("\n" + strings.Repeat("#", level) + " " + strings.ReplaceAll(text, "\n", " ") + "\n\n")
				return
			}
		}
		if numbering := pPr.child("numPr"); numbering != nil {
			depth := 0
			if ilvl := numbering.child("ilvl"); ilvl != nil {
				depth, _ = strconv.Atoi(ilvl.attr("val"))
			}
			b.WriteString(strings.Repeat("  ", min(max(depth, 0), 8)) + "- " + text + "\n")
			return
		}
	}
	b.WriteString(text + "\n\n")
}

// inlineText is the words of a paragraph or cell: its runs' text, tabs and
// line breaks, through hyperlinks, insertions and fields, but not deletions,
// field codes or drawings.
func inlineText(n *node) string {
	var b strings.Builder
	var walk func(*node)
	walk = func(n *node) {
		for _, child := range n.children {
			switch child.name {
			case "t":
				b.WriteString(child.text)
			case "tab":
				b.WriteString("\t")
			case "br", "cr":
				b.WriteString("\n")
			case "del", "delText", "instrText", "drawing", "pict", "pPr", "rPr", "footnoteReference", "commentReference":
			case "p":
				// A paragraph inside a cell or a text box: its own line.
				if b.Len() > 0 {
					b.WriteString("\n")
				}
				walk(child)
			default:
				walk(child)
			}
		}
	}
	walk(n)
	return b.String()
}

// writeTable writes a table as Markdown, its first row as the header; a cell's
// lines are joined, and a pipe in it escaped.
func writeTable(b *strings.Builder, table *node) {
	var rows [][]string
	width := 0
	for _, row := range table.children {
		if row.name != "tr" {
			continue
		}
		var cells []string
		for _, cell := range row.children {
			if cell.name != "tc" {
				continue
			}
			text := strings.Join(strings.Fields(inlineText(cell)), " ")
			cells = append(cells, strings.ReplaceAll(text, "|", `\|`))
		}
		rows = append(rows, cells)
		width = max(width, len(cells))
	}
	if len(rows) == 0 || width == 0 {
		return
	}
	b.WriteString("\n")
	for i, cells := range rows {
		for len(cells) < width {
			cells = append(cells, "")
		}
		b.WriteString("| " + strings.Join(cells, " | ") + " |\n")
		if i == 0 {
			b.WriteString("|" + strings.Repeat(" --- |", width) + "\n")
		}
	}
	b.WriteString("\n")
}

var blankLines = regexp.MustCompile(`\n{3,}`)

func collapseBlankLines(s string) string { return blankLines.ReplaceAllString(s, "\n\n") }
