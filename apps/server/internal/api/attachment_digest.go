package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/google/uuid"

	"github.com/qunqin24/openchat/apps/server/internal/db/sqlc"
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

// fileDigests are the digests of the files a check read, by attachment id.
type fileDigests struct {
	mu  sync.Mutex
	sum map[uuid.UUID]string
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
// (attachmentImages) and the words of its text files (attachmentTexts) - and
// the digests of the files it read, filled in as it reads them.
func (s *Server) checkedFiles(content string, files []sqlc.Attachment) (moderationInput, *fileDigests) {
	digests := &fileDigests{}
	in := moderationInput{text: content, images: s.checkedImages(files, digests)}
	if texts := s.attachmentTexts(files, digests); texts != nil {
		in.files = texts
	}
	return in, digests
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
// it read in digests. Nil when the message has none.
func (s *Server) attachmentTexts(files []sqlc.Attachment, digests *fileDigests) func(ctx context.Context) (string, error) {
	var texts []sqlc.Attachment
	for _, file := range files {
		if llm.ModalityOf(file.ContentType) == llm.ModalityText {
			texts = append(texts, file)
		}
	}
	if len(texts) == 0 {
		return nil
	}
	return func(ctx context.Context) (string, error) {
		var b strings.Builder
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
				parts[i] = ref
			}
		}
	}
	return parts
}
