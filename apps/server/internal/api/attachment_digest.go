package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/google/uuid"

	"github.com/qunqin24/openchat/apps/server/internal/db/sqlc"
	"github.com/qunqin24/openchat/apps/server/internal/doctext"
	"github.com/qunqin24/openchat/apps/server/internal/llm"
	"github.com/qunqin24/openchat/apps/server/internal/storage"
)

// What was checked is what is sent.
//
// A person's upload is two steps: the bytes go to a URL the server signed, and
// a message claims the upload by id. Content moderation reads the images of a
// message between the two, and the model reads them when the run starts. The
// object can be written again in that time - the local driver refuses a second
// PUT (PutFileContent), but an S3 presigned PUT cannot refuse anything, and
// stays valid after the message has claimed the file - so the picture the
// model gets could be another than the one that passed.
//
// So the check leaves a receipt on the message: the SHA-256 of the exact bytes
// it handed to the moderation service, written into the message's attachment
// reference (llm.AttachmentPart.SHA256). The context builder compares the bytes
// it reads against it and sends the file only when they are the same. The
// digest is taken from the bytes the check used, not from a second read, so
// there is no gap between "checked" and "recorded", and it does not matter
// when the claim happens.

// fileDigests are the digests of the files a check read, by attachment id,
// and what was read out of its documents (readDocuments).
type fileDigests struct {
	mu    sync.Mutex
	sum   map[uuid.UUID]string
	texts map[uuid.UUID]doctext.Text
}

func (d *fileDigests) setText(id uuid.UUID, text doctext.Text) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.texts == nil {
		d.texts = map[uuid.UUID]doctext.Text{}
	}
	d.texts[id] = text
}

func (d *fileDigests) text(id uuid.UUID) (doctext.Text, bool) {
	if d == nil {
		return doctext.Text{}, false
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	text, ok := d.texts[id]
	return text, ok
}

// readDocuments reads the words out of a message's Word, Excel and PDF files
// (doctext), once, when it is sent: they are what the model is sent of a Word
// or Excel file, and of a PDF too long for it to read whole, so they are what
// content moderation checks, and their length is recorded on the message
// (inputPartsChecked). The receipt is the digest of the original bytes they
// were read from: the same bytes give the same words, so the resolver reading
// them again later reads what was checked.
//
// A PDF this server cannot read the words of still goes, whole, to a model
// that reads PDFs (readsPDF), as before; to any other it is refused, as is a
// document that is damaged or whose words are too long to send.
func (s *Server) readDocuments(ctx context.Context, files []sqlc.Attachment, readsPDF bool) (*fileDigests, error) {
	digests := &fileDigests{}
	for _, file := range files {
		if !doctext.Supported(file.ContentType) {
			continue
		}
		data, err := s.store.Read(ctx, file.StorageKey, uploadable[file.ContentType])
		if err != nil {
			slog.WarnContext(ctx, "read a document", "attachment_id", file.ID, "error", err)
			return nil, errAttachmentUnavailable
		}
		digests.note(file.ID, data)
		text, err := doctext.Extract(ctx, file.ContentType, data)
		switch {
		case err == nil:
			digests.setText(file.ID, text)
		case errors.Is(err, doctext.ErrTooLong):
			return nil, errAttachmentTextTooLong
		case ctx.Err() != nil:
			return nil, ctx.Err()
		case file.ContentType == llm.PDFMediaType && readsPDF:
			slog.InfoContext(ctx, "a PDF's words could not be read; it goes whole", "attachment_id", file.ID, "error", err)
		case errors.Is(err, doctext.ErrNoReader):
			return nil, errAttachmentUnsupported
		default:
			slog.InfoContext(ctx, "a document could not be read", "attachment_id", file.ID, "error", err)
			return nil, errAttachmentUnreadable
		}
	}
	return digests, nil
}

// changedDigest is recorded for a file the check read two different ways: it
// matches no bytes, so the file is not sent.
const changedDigest = "changed-while-checked"

func (d *fileDigests) note(id uuid.UUID, data []byte) {
	sum := sha256.Sum256(data)
	hexed := hex.EncodeToString(sum[:])
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.sum == nil {
		d.sum = map[uuid.UUID]string{}
	}
	if old, seen := d.sum[id]; seen && old != hexed {
		hexed = changedDigest
	}
	d.sum[id] = hexed
}

func (d *fileDigests) of(id uuid.UUID) string {
	if d == nil {
		return ""
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.sum[id]
}

// checkedFiles is what a check of a message reads - its words, its pictures
// (attachmentImages) and the words of its text files and documents
// (attachmentTexts) - with digests, which hold what readDocuments read and
// are filled in with the files the check reads as it reads them.
func (s *Server) checkedFiles(content string, files []sqlc.Attachment, digests *fileDigests) moderationInput {
	in := moderationInput{text: content, images: s.checkedImages(files, digests)}
	if texts := s.attachmentTexts(files, digests); texts != nil {
		in.files = texts
	}
	return in
}

// checkedImages is attachmentImages that notes what it read in digests. Nil
// when the message has no image.
func (s *Server) checkedImages(files []sqlc.Attachment, digests *fileDigests) func(ctx context.Context) ([]moderationImage, error) {
	inner := s.attachmentImages(files)
	if inner == nil {
		return nil
	}
	var images []sqlc.Attachment
	for _, file := range files {
		if llm.ModalityOf(file.ContentType) == llm.ModalityImage {
			images = append(images, file)
		}
	}
	return func(ctx context.Context) ([]moderationImage, error) {
		read, err := inner(ctx)
		if err != nil {
			return nil, err
		}
		// In the order the loader read them.
		for i, image := range read {
			if i < len(images) {
				digests.note(images[i].ID, image.data)
			}
		}
		return read, nil
	}
}

// attachmentTexts loads the text files among a message's attachments for a
// check, as the words the model will be shown (llm.FileBlock), and notes what
// it read in digests; the words of its documents come from readDocuments.
// Nil when the message has neither.
func (s *Server) attachmentTexts(files []sqlc.Attachment, digests *fileDigests) func(ctx context.Context) (string, error) {
	var texts, documents []sqlc.Attachment
	for _, file := range files {
		if llm.ModalityOf(file.ContentType) == llm.ModalityText {
			texts = append(texts, file)
		} else if _, ok := digests.text(file.ID); ok {
			documents = append(documents, file)
		}
	}
	if len(texts) == 0 && len(documents) == 0 {
		return nil
	}
	return func(ctx context.Context) (string, error) {
		var b strings.Builder
		for _, file := range documents {
			text, _ := digests.text(file.ID)
			b.WriteString(llm.FileBlock(file.Filename, text.Content))
		}
		for _, file := range texts {
			data, err := s.store.Read(ctx, file.StorageKey, maxTextFileBytes+1)
			if errors.Is(err, storage.ErrTooLarge) {
				return "", fmt.Errorf("a text file is larger than a check reads: %w", errTooLargeToCheck)
			}
			if err != nil {
				return "", fmt.Errorf("read an attachment: %w", err)
			}
			digests.note(file.ID, data)
			// Bytes that are not text are not sent (the model is told the file
			// was left out), and are checked as what they would read as.
			text, ok := llm.FileText(data)
			if !ok {
				text = strings.ToValidUTF8(string(data), "")
			}
			b.WriteString(llm.FileBlock(file.Filename, text))
		}
		return b.String(), nil
	}
}

// inputPartsChecked is inputParts with the digests of the files a check read.
func inputPartsChecked(content string, files []sqlc.Attachment, digests *fileDigests) []llm.Part {
	parts := inputParts(content, files)
	for i, part := range parts {
		if ref, ok := part.(llm.AttachmentPart); ok {
			if id, err := uuid.Parse(ref.Ref); err == nil {
				ref.SHA256 = digests.of(id)
				if text, ok := digests.text(id); ok {
					ref.TextSize, ref.Pages = int64(len(text.Content)), text.Pages
				}
				parts[i] = ref
			}
		}
	}
	return parts
}
