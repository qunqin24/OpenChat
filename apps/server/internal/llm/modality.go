package llm

import (
	"bytes"
	"strings"
	"unicode/utf8"
)

// Input modalities, spelled as the model catalogue spells them. Text is the
// baseline every model has and is not listed.
const (
	ModalityImage = "image"
	ModalityPDF   = "pdf"
	// ModalityText is a text file: sent as words, which every model takes, so
	// it is never one a model has to list.
	ModalityText = "text"
)

// TextMediaType is the one media type a text file is stored and sent as,
// whatever it holds - notes, Markdown, CSV, code: the browser names text
// files inconsistently (a .ts file is "video/mp2t"), so the page sends this
// for every text file it knows by its extension.
const TextMediaType = "text/plain"

// ModalityOf is the input modality a media type is sent to a model as, or ""
// for one this package does not know how to send.
func ModalityOf(mediaType string) string {
	switch {
	case strings.HasPrefix(mediaType, "image/"):
		return ModalityImage
	case mediaType == "application/pdf":
		return ModalityPDF
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

func quoteAttr(s string) string {
	return `"` + strings.NewReplacer(`&`, "&amp;", `"`, "&quot;", "<", "&lt;").Replace(s) + `"`
}
