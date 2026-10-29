package llm

import (
	"bytes"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Input modalities, spelled as the model catalogue spells them. Text is the
// baseline every model has and is not listed.
const (
	ModalityImage = "image"
	ModalityPDF   = "pdf"
	// ModalityDocument is a Word or Excel file: sent as the words read out of
	// it (doctext), which every model takes, like a text file.
	ModalityDocument = "document"
	// ModalityText is a text file: sent as words, which every model takes, so
	// it is never one a model has to list.
	ModalityText = "text"
)

// TextMediaType is the one media type a text file is stored and sent as,
// whatever it holds - notes, Markdown, CSV, code: the browser names text
// files inconsistently (a .ts file is "video/mp2t"), so the page sends this
// for every text file it knows by its extension.
const TextMediaType = "text/plain"

// The media types of the Word and Excel files read as words, and of a PDF.
const (
	DocxMediaType = "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
	XlsxMediaType = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
	PDFMediaType  = "application/pdf"
	// ConversationMediaType marks an attachment part that is another of the
	// user's conversations, not a file (agent/references.go): read as words,
	// like a document, and never stored.
	ConversationMediaType = "application/x-openchat-conversation"
)

// NativePDFPages is the longest PDF sent whole to a model that reads PDFs:
// every page costs its words and a picture, on every turn. A longer one goes
// as its words, which a model reads in parts (agent/files.go).
const NativePDFPages = 20

// ModalityOf is the input modality a media type is sent to a model as, or ""
// for one this package does not know how to send.
func ModalityOf(mediaType string) string {
	switch {
	case strings.HasPrefix(mediaType, "image/"):
		return ModalityImage
	case mediaType == PDFMediaType:
		return ModalityPDF
	case mediaType == DocxMediaType || mediaType == XlsxMediaType || mediaType == ConversationMediaType:
		return ModalityDocument
	case mediaType == TextMediaType:
		return ModalityText
	}
	return ""
}

// FileText is a text file's content as words, or false for bytes that are not
// text: not UTF-8, or with a NUL in them (what a binary file has). A leading
// byte-order mark is dropped.
func FileText(data []byte) (string, bool) {
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
	if !utf8.Valid(data) || bytes.IndexByte(data, 0) >= 0 {
		return "", false
	}
	return string(data), true
}

// FileBlock is how a text file is put before a model: its name and its
// content, marked off from the words around them.
func FileBlock(filename, text string) string {
	return "\n\n<file name=" + quoteAttr(filename) + ">\n" + strings.TrimRight(text, "\n") + "\n</file>"
}

// InlineTextBytes is the longest text file put before a model whole. A longer
// one goes as a card (FileCard) when the model can read files with tools
// (agent/files.go): sent whole, it would be paid for again on every later turn
// of the conversation.
const InlineTextBytes = 16 << 10

// FileCardBytes bounds what a card says of its file: its beginning and its
// headings, with the words around them.
const FileCardBytes = 8 << 10

// FileCard is how a text file too long to send whole is put before a model:
// its name, the id the file tools take, its length, and body - what of it is
// shown - marked off as FileBlock marks a whole one, and flagged partial.
func FileCard(filename, id string, lines, size int, body string) string {
	return "\n\n<file name=" + quoteAttr(filename) + " id=" + quoteAttr(id) + " lines=\"" + strconv.Itoa(lines) +
		"\" size=\"" + strconv.Itoa((size+1023)/1024) + " KB\" partial=\"true\">\n" + strings.TrimRight(body, "\n") + "\n</file>"
}

func quoteAttr(s string) string {
	return `"` + strings.NewReplacer(`&`, "&amp;", `"`, "&quot;", "<", "&lt;").Replace(s) + `"`
}
