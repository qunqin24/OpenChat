package api

import (
	"errors"
	"io/fs"
	"mime"
	"net/http"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/qunqin/openchat/apps/server/internal/db/sqlc"
	"github.com/qunqin/openchat/apps/server/internal/httpx"
	"github.com/qunqin/openchat/apps/server/internal/llm"
	"github.com/qunqin/openchat/apps/server/internal/storage"
)

// The two handlers below are the local storage driver's stand-in for the S3
// endpoint a presigned URL would otherwise point at. They are not in the
// contract for the same reason S3's API is not: they are not something a client
// calls, only what a URL it was handed leads to. No session is required — the
// signature in the URL is the authorization, as it is for S3 — and the browser
// still sends only what the signed URL says.

// signedKey checks the request's signature for method and returns the object
// key it is for, with the attachment that owns the key. A key under avatars/
// belongs to an account instead, and is answered as the picture it is: the
// attachment returned for it names its type and nothing else, and only for
// reading.
func (s *Server) signedKey(r *http.Request, local *storage.Local, method string) (string, sqlc.Attachment, error) {
	key := strings.TrimPrefix(r.URL.Path, storage.ContentPath)
	query := r.URL.Query()
	if err := local.Verify(method, key, query.Get("expires"), query.Get("signature")); err != nil {
		if errors.Is(err, storage.ErrExpired) {
			return "", sqlc.Attachment{}, httpx.NewError(http.StatusForbidden, "url_expired", "this link has expired")
		}
		return "", sqlc.Attachment{}, httpx.NewError(http.StatusForbidden, "invalid_signature", "this link is not valid")
	}
	if strings.HasPrefix(key, avatarPrefix) {
		// Pictures are written by the server, never through a signed URL.
		if method != http.MethodGet {
			return "", sqlc.Attachment{}, httpx.ErrNotFound
		}
		if _, err := s.avatarOwner(r.Context(), key); err != nil {
			return "", sqlc.Attachment{}, err
		}
		return key, sqlc.Attachment{StorageKey: key, Filename: "avatar.png", ContentType: "image/png"}, nil
	}
	row, err := s.q.GetAttachmentByStorageKey(r.Context(), key)
	if err != nil {
		return "", sqlc.Attachment{}, dbErr(err)
	}
	return key, row, nil
}

// PutFileContent receives the bytes of an upload CreateUpload issued a URL for.
// It holds the file to what the upload announced: the content type it was
// issued for, and the largest size that type may be.
func (s *Server) PutFileContent(w http.ResponseWriter, r *http.Request) {
	local := s.local()
	if local == nil {
		httpx.Fail(w, r, httpx.ErrNotFound)
		return
	}
	key, row, err := s.signedKey(r, local, http.MethodPut)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}

	// An upload that a message has claimed is part of that transcript.
	if row.MessageID != nil {
		httpx.Fail(w, r, httpx.NewError(http.StatusConflict, "already_attached", "this file is already attached to a message"))
		return
	}
	contentType, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	limit, allowed := uploadable[contentType]
	if !allowed || contentType != row.ContentType {
		httpx.Fail(w, r, httpx.NewError(http.StatusUnsupportedMediaType, "unsupported_file_type",
			"the file must be uploaded with the content type it was announced with"))
		return
	}
	tooLarge := httpx.NewError(http.StatusRequestEntityTooLarge, "file_too_large", "the file is larger than allowed")
	if r.ContentLength > limit {
		httpx.Fail(w, r, tooLarge)
		return
	}

	// Without a declared length the limit still holds: the body is cut off.
	_, err = local.Put(key, http.MaxBytesReader(w, r.Body, limit), limit)
	var tooBig *http.MaxBytesError
	switch {
	case errors.Is(err, storage.ErrTooLarge), errors.As(err, &tooBig):
		httpx.Fail(w, r, tooLarge)
	case err != nil:
		httpx.Fail(w, r, err)
	default:
		w.WriteHeader(http.StatusOK)
	}
}

// GetFileContent serves a stored file to whoever holds a signed read URL.
//
// The file is the client's, so it is served as what it was announced to be and
// as nothing else, and as inert as the browser allows: no sniffing, no scripts,
// no framing. An image is only ever shown by the app in an <img>.
func (s *Server) GetFileContent(w http.ResponseWriter, r *http.Request) {
	local := s.local()
	if local == nil {
		httpx.Fail(w, r, httpx.ErrNotFound)
		return
	}
	key, row, err := s.signedKey(r, local, http.MethodGet)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	f, err := local.Open(key)
	if errors.Is(err, fs.ErrNotExist) {
		// A row with no file: an upload that never arrived.
		httpx.Fail(w, r, httpx.ErrNotFound)
		return
	}
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}

	h := w.Header()
	h.Set("Content-Type", row.ContentType)
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Disposition", mime.FormatMediaType("inline", map[string]string{"filename": safeFilename(row.Filename, row.ContentType)}))
	// The link is private to whoever was given it, and a new one is issued
	// each time the transcript is read, so only the browser's own copy is kept.
	h.Set("Cache-Control", "private, max-age=300")
	if llm.ModalityOf(row.ContentType) == "image" {
		h.Set("Content-Security-Policy", "default-src 'none'")
	} else {
		h.Set("Content-Security-Policy", "default-src 'none'; sandbox")
	}
	// Handles ranges and conditional requests, which a PDF viewer makes.
	http.ServeContent(w, r, "", info.ModTime(), f)
}

// safeFilename is the name a download offers, from the one the client gave: the
// last path element, without control characters, and never empty.
func safeFilename(name, contentType string) string {
	name = filepath.Base(strings.ReplaceAll(name, "\\", "/"))
	name = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || r == '/' || r == '"' {
			return -1
		}
		return r
	}, name)
	if name = strings.TrimSpace(name); name == "" || name == "." || name == ".." {
		if exts, _ := mime.ExtensionsByType(contentType); len(exts) > 0 {
			return "file" + exts[0]
		}
		return "file"
	}
	return name
}
