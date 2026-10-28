package agent

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/google/uuid"

	"github.com/qunqin24/openchat/apps/server/internal/db/sqlc"
	"github.com/qunqin24/openchat/apps/server/internal/doctext"
	"github.com/qunqin24/openchat/apps/server/internal/llm"
)

const (
	// MaxMessageAttachments is how many files one message may carry.
	MaxMessageAttachments = 10

	// MaxAttachmentBytes bounds the files in one request, and so the files of
	// one message too: a message that did not fit alone could never be sent.
	// Files go to the provider inline as base64, a third larger than they are
	// stored, and the smallest request limit among the providers is 32 MB.
	MaxAttachmentBytes = 20 << 20
)

// Blobs reads the content of stored files. Read refuses an object larger than
// limit.
type Blobs interface {
	Read(ctx context.Context, key string, limit int64) ([]byte, error)
}

// attachmentResolver turns the attachment references in a context into content
// a provider can take.
//
// A reference is resolved within the run's user: one that names someone else's
// file reads the same as one whose file is gone. The bytes are inlined, because
// a signed URL to our own storage is not something a hosted provider can
// fetch — it may be a MinIO on localhost.
//
// Anything that cannot go to the model becomes a line of text saying so. The
// model then knows a file was there, where dropping it would have it answer as
// if the user had attached nothing.
type attachmentResolver struct {
	lookup  func(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]sqlc.Attachment, error)
	read    func(ctx context.Context, key string, limit int64) ([]byte, error)
	accepts func(modality string) bool
	budget  int64
	// cards is whether a long text file goes as a card (fileCard): the
	// request offers the file tools that read the rest.
	cards bool
	// asText resolves every document, a PDF too, to its words: what the file
	// tools read.
	asText bool

	// cache keeps what a run has read, so each round of a tool loop does not
	// fetch the same files again.
	cache map[uuid.UUID][]byte
}

func (a *attachmentResolver) resolve(ctx context.Context, messages []llm.Message) ([]llm.Message, error) {
	var ids []uuid.UUID
	for _, msg := range messages {
		for _, part := range msg.Parts {
			if ref, ok := part.(llm.AttachmentPart); ok {
				if id, err := uuid.Parse(ref.Ref); err == nil {
					ids = append(ids, id)
				}
			}
		}
	}
	if len(ids) == 0 {
		return messages, nil
	}
	rows, err := a.lookup(ctx, ids)
	if err != nil {
		return nil, err
	}

	// Newest first: when the files do not all fit, the old ones give way to the
	// message being answered.
	out := make([]llm.Message, len(messages))
	copy(out, messages)
	budget := a.budget
	for i := len(out) - 1; i >= 0; i-- {
		hasFiles := false
		for _, part := range out[i].Parts {
			if _, ok := part.(llm.AttachmentPart); ok {
				hasFiles = true
			}
		}
		if !hasFiles {
			continue
		}
		parts := make([]llm.Part, len(out[i].Parts))
		copy(parts, out[i].Parts)
		for j := len(parts) - 1; j >= 0; j-- {
			ref, ok := parts[j].(llm.AttachmentPart)
			if !ok {
				continue
			}
			resolved, note := a.one(ctx, ref, rows, &budget)
			if note != "" {
				parts[j] = llm.TextPart{Text: fmt.Sprintf("\n\n[Attachment %q was not included: %s.]", ref.Filename, note)}
				continue
			}
			if resolved.MediaType == llm.TextMediaType {
				// A text file goes as words, which every model takes. sniff
				// has found them to be text.
				text, _ := llm.FileText(resolved.Data)
				if a.cards && len(resolved.Data) > llm.InlineTextBytes {
					parts[j] = llm.TextPart{Text: fileCard(ref, text)}
					budget += int64(len(resolved.Data)) // a card does not carry the file
					continue
				}
				parts[j] = llm.TextPart{Text: llm.FileBlock(ref.Filename, text)}
				continue
			}
			parts[j] = resolved
		}
		out[i].Parts = parts
	}
	return out, nil
}

// one resolves a reference, or says in note why it cannot be. A Word or Excel
// file resolves to its words (doctext), as a text file does; a PDF to itself
// for a model that reads PDFs, unless it is longer than one reads whole
// (llm.NativePDFPages), and to its words otherwise - or when asText, for the
// file tools - except a scanned one, whose words are next to none.
func (a *attachmentResolver) one(ctx context.Context, ref llm.AttachmentPart, rows map[uuid.UUID]sqlc.Attachment, budget *int64) (resolved llm.AttachmentPart, note string) {
	id, err := uuid.Parse(ref.Ref)
	row, found := rows[id]
	if err != nil || !found {
		return ref, "the file is no longer available"
	}

	modality := llm.ModalityOf(row.ContentType)
	readsPDF := modality == llm.ModalityPDF && a.accepts(modality)
	asWords := modality == llm.ModalityDocument ||
		(modality == llm.ModalityPDF && (a.asText || !readsPDF || ref.Pages > llm.NativePDFPages))
	switch {
	case modality == "":
		return ref, "the selected model does not accept this kind of file"
	case modality != llm.ModalityText && !asWords && !a.accepts(modality):
		return ref, "the selected model does not accept this kind of file"
	}
	if a.read == nil {
		return ref, "files cannot be read here"
	}
	// A document is read whole whatever the request has room for: what it
	// costs is its words.
	limit := *budget
	if asWords {
		limit = documentReadBytes
	} else if row.SizeBytes > *budget {
		return ref, "too many files to send in one request"
	}

	data, cached := a.cache[id]
	if !cached {
		data, err = a.read(ctx, row.StorageKey, limit)
		if err != nil {
			slog.WarnContext(ctx, "read attachment", "attachment_id", id, "error", err)
			return ref, "the file could not be read"
		}
	}
	// What content moderation looked at is what the model gets. The object may
	// have been written again since (an S3 presigned PUT cannot be made
	// write-once), and a file that is not the checked one is not sent.
	if ref.SHA256 != "" {
		if sum := sha256.Sum256(data); hex.EncodeToString(sum[:]) != ref.SHA256 {
			slog.WarnContext(ctx, "attachment changed after it was checked", "attachment_id", id)
			return ref, "the file changed after it was checked"
		}
	}
	if a.cache == nil {
		a.cache = map[uuid.UUID][]byte{}
	}
	a.cache[id] = data

	if asWords {
		text, err := doctext.Extract(ctx, row.ContentType, data)
		scanned := err == nil && text.Scanned
		switch {
		case (err != nil || scanned) && readsPDF && !a.asText:
			// Words it cannot have, or next to none: the pages themselves.
			asWords = false
		case scanned:
			return ref, "it is a scanned PDF: its pages are pictures, which only a model that reads PDFs can read"
		case err != nil:
			slog.WarnContext(ctx, "read a document's words", "attachment_id", id, "error", err)
			return ref, "its text could not be read"
		default:
			if int64(len(text.Content)) > *budget {
				return ref, "too many files to send in one request"
			}
			*budget -= int64(len(text.Content))
			ref.Data = []byte(text.Content)
			ref.MediaType = llm.TextMediaType
			return ref, ""
		}
		if row.SizeBytes > *budget {
			return ref, "too many files to send in one request"
		}
	}

	mediaType, ok := sniff(modality, data)
	if !ok && modality == llm.ModalityText {
		return ref, "it is not UTF-8 text"
	}
	if !ok {
		return ref, "the file is not what it claims to be"
	}
	if int64(len(data)) > *budget {
		return ref, "too many files to send in one request"
	}
	*budget -= int64(len(data))

	ref.Data = data
	ref.MediaType = mediaType
	return ref, ""
}

// documentReadBytes bounds a document read for its words: the largest that
// can be uploaded (api/files.go).
const documentReadBytes = 20 << 20

// sniff checks that content is the kind of file its modality says, and gives
// the media type to send it as. The declared type came from the browser; the
// providers reject a file whose bytes disagree with the type it is sent as.
func sniff(modality string, data []byte) (mediaType string, ok bool) {
	switch modality {
	case llm.ModalityPDF:
		return "application/pdf", bytes.HasPrefix(data, []byte("%PDF-"))
	case llm.ModalityText:
		_, ok := llm.FileText(data)
		return llm.TextMediaType, ok
	case llm.ModalityImage:
		detected := http.DetectContentType(data)
		switch detected {
		case "image/png", "image/jpeg", "image/gif", "image/webp":
			return detected, true
		}
	}
	return "", false
}

// withAttachments resolves the attachments in a request's messages for this
// run; cards is whether a long text file goes as a card (the request offers
// the file tools).
func (ex *execution) withAttachments(ctx context.Context, messages []llm.Message, cards bool) ([]llm.Message, error) {
	r := ex.r
	if ex.files == nil {
		ex.files = &attachmentResolver{
			lookup: func(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]sqlc.Attachment, error) {
				rows, err := r.q.ListAttachmentsForUser(ctx, sqlc.ListAttachmentsForUserParams{Ids: ids, UserID: &ex.run.UserID})
				if err != nil {
					return nil, err
				}
				byID := make(map[uuid.UUID]sqlc.Attachment, len(rows))
				for _, row := range rows {
					byID[row.ID] = row
				}
				return byID, nil
			},
			accepts: func(string) bool { return true },
			budget:  MaxAttachmentBytes,
		}
		if r.opts.Blobs != nil {
			ex.files.read = r.opts.Blobs.Read
		}
		if r.opts.Modalities != nil {
			modalities, err := r.opts.Modalities(ctx, ex.run.ModelID)
			if err != nil {
				ex.files = nil
				return nil, err
			}
			ex.files.accepts = func(modality string) bool {
				for _, m := range modalities {
					if m == modality {
						return true
					}
				}
				return false
			}
		}
	}
	ex.files.cards = cards
	return ex.files.resolve(ctx, messages)
}
