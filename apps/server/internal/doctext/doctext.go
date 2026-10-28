// Package doctext reads the words out of the documents a person attaches -
// Word (.docx), Excel (.xlsx) and PDF - so that a model is sent words, which
// every model takes, and content moderation checks what the model reads. No
// model reads a Word or Excel file itself, and a PDF sent whole costs every
// page as words and as a picture on every turn of a conversation.
//
// The original stays what was uploaded and is kept; the words are read again
// from it when they are needed, and kept in memory by the digest of its bytes
// (cache), so the receipt that guards a file (agent.attachmentResolver) covers
// the words too: the same bytes give the same words.
package doctext

import (
	"container/list"
	"context"
	"crypto/sha256"
	"errors"
	"sync"

	"github.com/qunqin24/openchat/apps/server/internal/llm"
)

// The media types of the documents read here, as the browser names them.
const (
	DocxMediaType = llm.DocxMediaType
	XlsxMediaType = llm.XlsxMediaType
	PDFMediaType  = llm.PDFMediaType
)

// MaxTextBytes bounds the words of one document. A file whose words are
// longer is not sent at all: cut short, it would be answered about as though
// it had been read.
const MaxTextBytes = 2 << 20

var (
	// ErrUnreadable is a file that is not the document it says it is, or is
	// damaged.
	ErrUnreadable = errors.New("doctext: the file cannot be read")
	// ErrTooLong is a document whose words are longer than MaxTextBytes.
	ErrTooLong = errors.New("doctext: the document's text is too long")
	// ErrNoReader is a PDF on a server without pdftotext.
	ErrNoReader = errors.New("doctext: this server cannot read PDF text")
)

// Text is what a document says.
type Text struct {
	// Content is the words: Markdown for Word, CSV under a heading per sheet
	// for Excel, and for a PDF its pages, each after a "[Page n]" line.
	Content string
	// Pages is a PDF's page count; 0 for the others.
	Pages int
	// Scanned is a PDF with next to no text on its pages: pictures of pages,
	// which only a model that reads PDFs can read.
	Scanned bool
}

// Supported is whether a media type is a document read here.
func Supported(mediaType string) bool {
	switch mediaType {
	case DocxMediaType, XlsxMediaType, PDFMediaType:
		return true
	}
	return false
}

// Extract reads a document's words, from the cache when the same bytes were
// read before.
func Extract(ctx context.Context, mediaType string, data []byte) (Text, error) {
	key := cacheKey{mediaType: mediaType, sum: sha256.Sum256(data)}
	if text, ok := cache.get(key); ok {
		return text, nil
	}
	var (
		text Text
		err  error
	)
	switch mediaType {
	case DocxMediaType:
		text, err = docx(data)
	case XlsxMediaType:
		text, err = xlsx(data)
	case PDFMediaType:
		text, err = pdf(ctx, data)
	default:
		return Text{}, ErrUnreadable
	}
	if err != nil {
		return Text{}, err
	}
	if len(text.Content) > MaxTextBytes {
		return Text{}, ErrTooLong
	}
	cache.put(key, text)
	return text, nil
}

// cache keeps what documents said, by their bytes, within cacheBytes of words.
var cache = &textCache{byKey: map[cacheKey]*list.Element{}, order: list.New()}

const cacheBytes = 64 << 20

type cacheKey struct {
	mediaType string
	sum       [sha256.Size]byte
}

type cacheEntry struct {
	key  cacheKey
	text Text
}

type textCache struct {
	mu    sync.Mutex
	byKey map[cacheKey]*list.Element
	order *list.List // most recently used first
	size  int
}

func (c *textCache) get(key cacheKey) (Text, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	element, ok := c.byKey[key]
	if !ok {
		return Text{}, false
	}
	c.order.MoveToFront(element)
	return element.Value.(*cacheEntry).text, true
}

func (c *textCache) put(key cacheKey, text Text) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.byKey[key]; ok {
		return
	}
	c.byKey[key] = c.order.PushFront(&cacheEntry{key: key, text: text})
	c.size += len(text.Content)
	for c.size > cacheBytes && c.order.Len() > 1 {
		oldest := c.order.Back()
		entry := oldest.Value.(*cacheEntry)
		c.order.Remove(oldest)
		delete(c.byKey, entry.key)
		c.size -= len(entry.text.Content)
	}
}
