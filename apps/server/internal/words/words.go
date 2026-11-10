// Package words reads the person's own words out of a user message.
//
// The composer puts lines before what a person types (a deep research, picked
// skills, a picture to change) and can wrap a quoted passage around their
// request; whatever reads a message as the person's words - a title, a search
// excerpt, the list of recent conversations in a snapshot - takes those off.
// Pure string functions, shared by package api and package memory.
package words

import (
	"regexp"
	"strings"

	"github.com/qunqin24/openchat/apps/server/internal/skills"
)

// ResearchLine is how a message asks for a deep research: its first line.
const ResearchLine = "<deep-research/>"

// SplitResearch is whether a message asks for a deep research, and the rest of
// it.
func SplitResearch(content string) (bool, string) {
	if content == ResearchLine {
		return true, ""
	}
	if rest, ok := strings.CutPrefix(content, ResearchLine+"\n"); ok {
		return true, rest
	}
	return false, content
}

// imageRefLine is a picture the person picked for a message with 「拿来改」,
// at its start (chat-core/src/image-ref.ts).
var imageRefLine = regexp.MustCompile(`^<image-ref id="(img_[0-9a-f]{8})"/>\n`)

// SplitImageRefs is the pictures a message picked, and the rest of it.
func SplitImageRefs(content string) ([]string, string) {
	var handles []string
	for {
		match := imageRefLine.FindStringSubmatch(content)
		if match == nil {
			return handles, content
		}
		handles = append(handles, match[1])
		content = content[len(match[0]):]
	}
}

// Person is what a person wrote in a message, without the lines the composer
// puts before it (deep research, picked skills, pictures).
func Person(content string) string {
	_, rest := SplitResearch(content)
	_, rest = skills.SplitMentions(rest)
	_, rest = SplitImageRefs(rest)
	return rest
}

// Quotes at the start of a message, as chat-core/src/document-selection.ts
// writes them (quoteMessage) and reads them back (parseQuotes): a passage of a
// writing block or of an earlier answer, then what the person asks of it.
var (
	documentQuote = regexp.MustCompile(`^<document-selection id="[^"]*" title="[^"]*">\n[\s\S]*?\n</document-selection>\n\n?`)
	answerQuote   = regexp.MustCompile(`^<quote>\n[\s\S]*?\n</quote>\n\n?`)
)

// StripQuotes is a message without the quoted passages it begins with: the
// passage is somebody else's text (a document, a model answer), the request
// after it is the person's. A message that quotes nothing is returned as it is.
func StripQuotes(content string) string {
	for {
		if loc := documentQuote.FindStringIndex(content); loc != nil {
			content = content[loc[1]:]
			continue
		}
		if loc := answerQuote.FindStringIndex(content); loc != nil {
			content = content[loc[1]:]
			continue
		}
		return content
	}
}

// Quoted says whether a message begins, after the composer's lines, with a
// quoted passage. Only the whole of such a message tells the passage from the
// person's words after it: one that was cut short (to bound what is read) has
// to be read again whole before Typed.
func Quoted(content string) bool {
	rest := Person(content)
	return strings.HasPrefix(rest, "<document-selection ") || strings.HasPrefix(rest, "<quote>\n")
}

// Typed is what the person typed in a message: Person with the quotes off. In
// the order the composer builds a message: the research and skill lines, then
// the quotes, then the request (chat-core/src/document-selection.ts is applied
// to the text after the lines).
func Typed(content string) string {
	return StripQuotes(Person(content))
}
