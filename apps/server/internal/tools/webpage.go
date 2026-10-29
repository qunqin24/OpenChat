package tools

import (
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	readability "codeberg.org/readeck/go-readability/v2"
	htmltomarkdown "github.com/JohannesKaufmann/html-to-markdown/v2/converter"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/base"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/commonmark"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/strikethrough"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/table"
	"github.com/go-shiori/dom"
	"golang.org/x/net/html"
)

// ---------------------------------------------------------------- HTML to Markdown

// A page is read the way a browser's reader view reads it: its main content
// (readability, a port of Firefox's), as Markdown, so the model keeps what the
// page's structure says - headings, lists, tables, links - and is not handed
// its menus, footers and sidebars. Where no main content can be told apart (a
// list of links, a product page, a page of short blocks) the whole body goes,
// without what holds no reading text.

// skipped elements hold no reading text; they go before anything else is
// done with a page. Not <form>: some sites wrap the whole page in one.
var skipped = map[string]bool{
	"script": true, "style": true, "noscript": true, "template": true, "svg": true,
	"canvas": true, "iframe": true, "nav": true, "footer": true, "select": true, "textarea": true,
}

const (
	// articleEnough is the length past which the main content is taken as the
	// page's even when the page has much more text besides: what readability
	// leaves out of a page with a real article in it is chrome.
	articleEnough = 500
)

var (
	// Pictures are listed apart (htmlImages); inline, each is a long address
	// standing in for a few words of alt text.
	markdownImage = regexp.MustCompile(`!\[[^\]]*\]\([^)]*\)`)
	// A link with no words left in it, once its picture is gone.
	markdownEmptyLink = regexp.MustCompile(`\[\s*\]\([^)]*\)`)
	// A link: its words, its address, and the title a page may give it.
	markdownLink = regexp.MustCompile(`\[((?:\\.|[^\[\]\\])*)\]\(([^)\s]+)(?: "[^"]*")?\)`)
	// A footnote marker's words, "[12]" or "[note 3]", escaped.
	footnoteMarker = regexp.MustCompile(`^\\\[[\w ]{1,12}\\\]$`)
	blankLines     = regexp.MustCompile(`\n{3,}`)
)

// pageMarkdown is an HTML page's title, the day it says it was published
// (empty when it does not), and its text as Markdown.
func pageMarkdown(doc *html.Node, base *url.URL) (title, published, text string) {
	title = findTitle(doc)

	stripped := withoutSkipped(doc)
	whole := wholePage(stripped, base)
	article, err := readability.FromDocument(stripped, base)
	if err == nil && article.Node != nil {
		if when, err := article.PublishedTime(); err == nil && !when.IsZero() {
			published = when.Format("2006-01-02")
		}
		main := toMarkdown(article.Node, base)
		if n := utf8.RuneCountInString(main); n >= articleEnough || 2*n >= utf8.RuneCountInString(whole) {
			text = main
		}
		if title == "" {
			title = article.Title()
		}
	}
	if text == "" {
		text = whole
	}
	return title, published, text
}

// withoutSkipped is a copy of the page without the elements that hold no
// reading text. The <head> stays: readability reads the title, the date and
// the language from it.
func withoutSkipped(doc *html.Node) *html.Node {
	clone := dom.Clone(doc, true)
	var strip func(n *html.Node)
	strip = func(n *html.Node) {
		for c := n.FirstChild; c != nil; {
			next := c.NextSibling
			if c.Type == html.ElementNode && skipped[c.Data] {
				n.RemoveChild(c)
			} else {
				strip(c)
			}
			c = next
		}
	}
	strip(clone)
	return clone
}

// wholePage is the page's body as Markdown.
func wholePage(doc *html.Node, base *url.URL) string {
	body := findElement(doc, "body")
	if body == nil {
		return ""
	}
	return toMarkdown(body, base)
}

func findElement(n *html.Node, tag string) *html.Node {
	if n.Type == html.ElementNode && n.Data == tag {
		return n
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if found := findElement(c, tag); found != nil {
			return found
		}
	}
	return nil
}

// toMarkdown writes a part of a page as Markdown, its links made absolute.
func toMarkdown(n *html.Node, pageURL *url.URL) string {
	conv := htmltomarkdown.NewConverter(htmltomarkdown.WithPlugins(
		base.NewBasePlugin(),
		commonmark.NewCommonmarkPlugin(),
		table.NewTablePlugin(),
		strikethrough.NewStrikethroughPlugin(),
	))
	out, err := conv.ConvertNode(n, htmltomarkdown.WithDomain(pageURL.String()))
	if err != nil {
		return ""
	}
	text := markdownImage.ReplaceAllString(string(out), "")
	text = markdownEmptyLink.ReplaceAllString(text, "")
	text = markdownLink.ReplaceAllStringFunc(text, func(link string) string {
		parts := markdownLink.FindStringSubmatch(link)
		words, address := parts[1], parts[2]
		// A footnote marker is noise without its note; a link within the page
		// leads nowhere a reader of this text can go.
		if footnoteMarker.MatchString(words) {
			return ""
		}
		if target, err := url.Parse(address); err == nil && target.Fragment != "" {
			target.Fragment = ""
			here := *pageURL
			here.Fragment = ""
			if target.String() == here.String() {
				return words
			}
		}
		// Its title repeats its words, more often than not.
		return "[" + words + "](" + readableAddress(address) + ")"
	})
	return tidyText(text)
}

// readableAddress is an address with its escaped letters shown as letters -
// "/wiki/草莓" rather than "/wiki/%E8%8D%89%E8%8E%93", a third of the length.
// Only escapes of bytes past ASCII are undone, so nothing that would end the
// link or change what the address means (a space, a bracket, a "?") comes out
// of one; fetched, the letters are escaped again as they were.
func readableAddress(address string) string {
	if !strings.Contains(address, "%") {
		return address
	}
	var b strings.Builder
	for i := 0; i < len(address); i++ {
		if address[i] == '%' && i+2 < len(address) {
			if v, ok := unhex(address[i+1], address[i+2]); ok && v >= 0x80 {
				b.WriteByte(v)
				i += 2
				continue
			}
		}
		b.WriteByte(address[i])
	}
	if out := b.String(); utf8.ValidString(out) {
		return out
	}
	return address
}

func unhex(hi, lo byte) (byte, bool) {
	digit := func(c byte) (byte, bool) {
		switch {
		case '0' <= c && c <= '9':
			return c - '0', true
		case 'a' <= c && c <= 'f':
			return c - 'a' + 10, true
		case 'A' <= c && c <= 'F':
			return c - 'A' + 10, true
		}
		return 0, false
	}
	h, ok1 := digit(hi)
	l, ok2 := digit(lo)
	return h<<4 | l, ok1 && ok2
}

// tidyText drops the spaces that end lines and the blank lines past one.
func tidyText(text string) string {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRightFunc(line, unicode.IsSpace)
	}
	text = blankLines.ReplaceAllString(strings.Join(lines, "\n"), "\n\n")
	return strings.TrimSpace(text)
}

// ---------------------------------------------------------------- reading in parts

// A page longer than one read (fetchMaxChars) is read in parts: each read
// starts where the model says (offset, in characters) and says where the next
// starts. Or the model asks for the passages that mention something (find)
// and reads on from the one it wants. Both count characters, not bytes, so an
// offset the model was given is one it can pass back.

const (
	// windowSnapBack is how far a read's start may move back to begin at the
	// start of a line, rather than mid-sentence.
	windowSnapBack = 300
	// windowSnapEnd is how far before its limit a read may end, to end at a
	// line's end.
	windowSnapEnd = 2000

	// findPassages is how many passages one find shows; findBefore and
	// findAfter, how much of the page each shows around its match. Matches
	// close together share a passage, up to findPassageMax; all of them
	// together are at most a read's worth (fetchMaxChars). Past either bound
	// the find says where to find on from, as a read says where to read on:
	// a common word must not bring a whole long page in at once.
	findPassages   = 8
	findBefore     = 300
	findAfter      = 600
	findPassageMax = 2000
)

// window is the part of text a read starting at offset shows: [start, end).
func window(text []rune, offset int) (start, end int) {
	start = min(max(offset, 0), len(text))
	for back := start; back > 0 && start-back < windowSnapBack; back-- {
		if text[back-1] == '\n' {
			start = back
			break
		}
	}
	end = min(start+fetchMaxChars, len(text))
	if end < len(text) {
		for cut := end; cut > end-windowSnapEnd && cut > start; cut-- {
			if text[cut-1] == '\n' {
				end = cut
				break
			}
		}
	}
	return start, end
}

// passage is a part of a page around what a find matched.
type passage struct {
	start, end int
}

// findIn is where query occurs in text from offset on, ignoring case: the
// passages around the matches shown (see findPassages), how many matches
// there are in all, and where the first match not shown is - -1 when every
// one is.
func findIn(text []rune, query string, offset int) (passages []passage, matches, next int) {
	next = -1
	needle := []rune(strings.ToLower(query))
	if len(needle) == 0 {
		return nil, 0, -1
	}
	lower := make([]rune, len(text))
	for i, r := range text {
		lower[i] = unicode.ToLower(r)
	}
	total := 0
	for i := max(offset, 0); i+len(needle) <= len(lower); i++ {
		if !runesAt(lower, i, needle) {
			continue
		}
		matches++
		if next >= 0 {
			// Past what is shown: counted only.
			i += len(needle) - 1
			continue
		}
		start, end := lineStart(text, max(i-findBefore, 0), i), lineEnd(text, min(i+len(needle)+findAfter, len(text)))
		shown := false
		if n := len(passages); n > 0 && start <= passages[n-1].end {
			last := &passages[n-1]
			switch {
			case i+len(needle) <= last.end:
				shown = true // inside the passage already
			case end-last.start <= findPassageMax && total+end-last.end <= fetchMaxChars:
				total += end - last.end
				last.end = end
				shown = true
			default:
				// Too long to grow: a passage of its own, from where that one ends.
				start = last.end
			}
		}
		if !shown && len(passages) < findPassages && total+end-start <= fetchMaxChars {
			passages = append(passages, passage{start, end})
			total += end - start
			shown = true
		}
		if !shown {
			next = i
		}
		i += len(needle) - 1
	}
	return passages, matches, next
}

func runesAt(haystack []rune, at int, needle []rune) bool {
	for j, r := range needle {
		if haystack[at+j] != r {
			return false
		}
	}
	return true
}

// lineStart moves from back to the start of its line, if one begins before
// the match at limit.
func lineStart(text []rune, from, limit int) int {
	for i := from; i < limit; i++ {
		if i == 0 || text[i-1] == '\n' {
			return i
		}
	}
	return from
}

// lineEnd moves to the end of the line to, if it ends soon after.
func lineEnd(text []rune, to int) int {
	for i := to; i < len(text) && i < to+200; i++ {
		if text[i] == '\n' {
			return i
		}
	}
	return to
}

// ---------------------------------------------------------------- recently read

// Reading on from an offset, or finding in a page just read, should not fetch
// it again: the page may have changed in between, and the offsets with it. A
// page read is kept a few minutes, a few at a time. Pages are public and read
// with no cookies or credentials, so what one conversation read is what any
// other would have.

const (
	pageCacheTTL     = 10 * time.Minute
	pageCacheEntries = 32
)

var recentPages = &pageCache{byURL: map[string]cachedPage{}}

type pageCache struct {
	mu    sync.Mutex
	byURL map[string]cachedPage
}

type cachedPage struct {
	page page
	at   time.Time
}

func (c *pageCache) get(address string) (page, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.byURL[address]
	if !ok || time.Since(entry.at) > pageCacheTTL {
		delete(c.byURL, address)
		return page{}, false
	}
	return entry.page, true
}

func (c *pageCache) put(address string, p page) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.byURL) >= pageCacheEntries {
		// The oldest goes.
		var oldest string
		for key, entry := range c.byURL {
			if oldest == "" || entry.at.Before(c.byURL[oldest].at) {
				oldest = key
			}
		}
		delete(c.byURL, oldest)
	}
	c.byURL[address] = cachedPage{page: p, at: time.Now()}
}
