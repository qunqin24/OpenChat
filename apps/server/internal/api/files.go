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

	"github.com/qunqin/openchat/apps/server/internal/auth"
	"github.com/qunqin/openchat/apps/server/internal/db/sqlc"
	"github.com/qunqin/openchat/apps/server/internal/httpx"
	"github.com/qunqin/openchat/apps/server/internal/llm"
)

const uploadTTL = 15 * time.Minute

// uploadable is what may be uploaded: the files a model can be sent, with the
// largest of each. The images stop at the 5 MB the strictest provider takes; a
// larger one would be accepted here and refused by the model.
var uploadable = map[string]int64{
	"image/png":       5 << 20,
	"image/jpeg":      5 << 20,
	"image/webp":      5 << 20,
	"image/gif":       5 << 20,
	"application/pdf": 10 << 20,
}

// blobStore is the part of the blob store the handlers use.
type blobStore interface {
	PresignPut(ctx context.Context, key, contentType string, ttl time.Duration) (string, error)
	PresignGet(ctx context.Context, key string, ttl time.Duration) (string, error)
	ObjectURL(key string) string
	Size(ctx context.Context, key string) (int64, error)
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
		httpx.Fail(w, r, httpx.NewError(http.StatusUnsupportedMediaType, "unsupported_file_type",
			"only images (PNG, JPEG, WebP, GIF) and PDFs can be attached"))
		return
	case body.Size <= 0:
		httpx.Fail(w, r, httpx.NewError(http.StatusBadRequest, "invalid_size", "size must be positive"))
		return
	case body.Size > limit:
		httpx.Fail(w, r, httpx.NewError(http.StatusRequestEntityTooLarge, "file_too_large",
			fmt.Sprintf("a %s file can be at most %d MiB", llm.ModalityOf(contentType), limit>>20)))
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
