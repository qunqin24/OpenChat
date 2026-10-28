package api

import (
	"context"
	"fmt"
	"mime"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/qunqin24/openchat/apps/server/internal/auth"
	"github.com/qunqin24/openchat/apps/server/internal/db/sqlc"
	"github.com/qunqin24/openchat/apps/server/internal/httpx"
	"github.com/qunqin24/openchat/apps/server/internal/llm"
)

const uploadTTL = 15 * time.Minute

// uploadable is what may be uploaded: the files a model can be sent, with the
// largest of each. The images stop at the 5 MB the strictest provider takes; a
// larger one would be accepted here and refused by the model. A text file is
// sent as words, so its bound is a share of a model's window, not a provider's.
var uploadable = map[string]int64{
	"image/png":       5 << 20,
	"image/jpeg":      5 << 20,
	"image/webp":      5 << 20,
	"image/gif":       5 << 20,
	"application/pdf": 10 << 20,
	llm.TextMediaType: maxTextFileBytes,
}

// maxTextFileBytes bounds a text file: some 85,000 Chinese characters, or
// 60,000 tokens of English.
const maxTextFileBytes = 256 << 10

// errUnsupportedFileType is the answer for a file of a kind that cannot be attached.
var errUnsupportedFileType = httpx.NewError(http.StatusUnsupportedMediaType, "unsupported_file_type",
	"only images (PNG, JPEG, WebP, GIF), PDFs and text files can be attached")

// sizeLimit says a file's bound in the unit that reads best.
func sizeLimit(bytes int64) string {
	if bytes < 1<<20 {
		return fmt.Sprintf("%d KiB", bytes>>10)
	}
	return fmt.Sprintf("%d MiB", bytes>>20)
}

// blobStore is the part of the blob store the handlers use.
type blobStore interface {
	PresignPut(ctx context.Context, key, contentType string, ttl time.Duration) (string, error)
	PresignGet(ctx context.Context, key string, ttl time.Duration) (string, error)
	PresignDownload(ctx context.Context, key, filename string, ttl time.Duration) (string, error)
	ObjectURL(key string) string
	Size(ctx context.Context, key string) (int64, error)
	// Read returns a stored file, refusing one larger than limit (content
	// moderation reads the images of a message it checks).
	Read(ctx context.Context, key string, limit int64) ([]byte, error)
	// PutObject stores bytes the server holds itself (a picture a search found).
	PutObject(ctx context.Context, key, contentType string, data []byte) error
	// Delete removes a stored file; the cleanup passes use it once the row that
	// wanted the file is gone.
	Delete(ctx context.Context, key string) error
}

// CreateUpload records an attachment row and hands back a presigned PUT URL.
// The attachment is orphaned until a CreateMessage call claims it by id, and
// only its uploader can.
func (s *Server) CreateUpload(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())

	var body CreateUploadRequest
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.Fail(w, r, err)
		return
	}

	filename := strings.TrimSpace(body.Filename)
	contentType, _, _ := mime.ParseMediaType(body.ContentType)
	limit, allowed := uploadable[contentType]
	switch {
	case filename == "":
		httpx.Fail(w, r, httpx.NewError(http.StatusBadRequest, "invalid_filename", "filename must not be empty"))
		return
	case !allowed:
		httpx.Fail(w, r, errUnsupportedFileType)
		return
	case body.Size <= 0:
		httpx.Fail(w, r, httpx.NewError(http.StatusBadRequest, "invalid_size", "size must be positive"))
		return
	case body.Size > limit:
		httpx.Fail(w, r, httpx.NewError(http.StatusRequestEntityTooLarge, "file_too_large",
			fmt.Sprintf("a %s file can be at most %s", llm.ModalityOf(contentType), sizeLimit(limit))))
		return
	}

	id := uuid.New()
	key := fmt.Sprintf("uploads/%s/%s%s", time.Now().UTC().Format("2006/01/02"), id, keyExt(filename))

	uploadURL, err := s.store.PresignPut(r.Context(), key, contentType, uploadTTL)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}

	row, err := s.q.CreateAttachment(r.Context(), sqlc.CreateAttachmentParams{
		UserID:      &user.ID,
		StorageKey:  key,
		Filename:    filename,
		ContentType: contentType,
		SizeBytes:   body.Size,
	})
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}

	httpx.JSON(w, http.StatusCreated, Upload{
		Id:        row.ID,
		UploadUrl: uploadURL,
		Url:       s.store.ObjectURL(key),
		ExpiresAt: time.Now().UTC().Add(uploadTTL),
	})
}

// keyExt is the extension a storage key keeps from the filename: a short
// run of letters and digits, or none. The name is the client's, and what it puts
// after the last dot ends up in a path on whichever disk or bucket holds it.
func keyExt(filename string) string {
	ext := strings.ToLower(filepath.Ext(filename))
	if len(ext) < 2 || len(ext) > 11 {
		return ""
	}
	for _, c := range ext[1:] {
		if (c < 'a' || c > 'z') && (c < '0' || c > '9') {
			return ""
		}
	}
	return ext
}
