package api

import (
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/qunqin/openchat/apps/server/internal/db/sqlc"
	"github.com/qunqin/openchat/apps/server/internal/httpx"
)

const (
	maxUploadBytes = 25 << 20 // 25 MiB
	uploadTTL      = 15 * time.Minute
)

// CreateUpload records an attachment row and hands back a presigned PUT URL.
// The attachment is orphaned until a CreateMessage call claims it by id.
func (s *Server) CreateUpload(w http.ResponseWriter, r *http.Request) {
	var body CreateUploadRequest
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.Fail(w, r, err)
		return
	}

	filename := strings.TrimSpace(body.Filename)
	switch {
	case filename == "":
		httpx.Fail(w, r, httpx.NewError(http.StatusBadRequest, "invalid_filename", "filename must not be empty"))
		return
	case body.Size <= 0:
		httpx.Fail(w, r, httpx.NewError(http.StatusBadRequest, "invalid_size", "size must be positive"))
		return
	case body.Size > maxUploadBytes:
		httpx.Fail(w, r, httpx.NewError(http.StatusRequestEntityTooLarge, "file_too_large",
			fmt.Sprintf("file exceeds the %d MiB limit", maxUploadBytes>>20)))
		return
	}

	contentType := body.ContentType
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	id := uuid.New()
	key := fmt.Sprintf("uploads/%s/%s%s", time.Now().UTC().Format("2006/01/02"), id, filepath.Ext(filename))

	uploadURL, err := s.store.PresignPut(r.Context(), key, contentType, uploadTTL)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}

	row, err := s.q.CreateAttachment(r.Context(), sqlc.CreateAttachmentParams{
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
