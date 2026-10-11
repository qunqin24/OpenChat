package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"sync"

	"github.com/google/uuid"

	"github.com/qunqin/openchat/apps/server/internal/db/sqlc"
	"github.com/qunqin/openchat/apps/server/internal/llm"
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

// checkedImages is attachmentImages that remembers what it read: the loader
// the check calls, and the digests it fills in as it does. Nil when the message
// has no image.
func (s *Server) checkedImages(files []sqlc.Attachment) (func(ctx context.Context) ([]moderationImage, error), *fileDigests) {
	inner := s.attachmentImages(files)
	if inner == nil {
		return nil, nil
	}
	var images []sqlc.Attachment
	for _, file := range files {
		if llm.ModalityOf(file.ContentType) == llm.ModalityImage {
			images = append(images, file)
		}
	}
	digests := &fileDigests{}
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
	}, digests
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
