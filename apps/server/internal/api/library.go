package api

import (
	"log/slog"
	"net/http"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/qunqin24/openchat/apps/server/internal/agent"
	"github.com/qunqin24/openchat/apps/server/internal/auth"
	"github.com/qunqin24/openchat/apps/server/internal/db/sqlc"
	"github.com/qunqin24/openchat/apps/server/internal/doctext"
	"github.com/qunqin24/openchat/apps/server/internal/httpx"
	"github.com/qunqin24/openchat/apps/server/internal/llm"
)

// The library: every file a user sent, across their conversations, to look
// through, save or delete. A file is in it while the conversation it was sent
// in can be opened; the scope is in the queries (ListLibraryFiles), so a file
// outside it reads as missing.

var errInvalidFileType = httpx.NewFieldError("type", "invalid_file_type", "type must be image, pdf or text")

// libraryTypes is the content types each filter of the library matches, as a
// LIKE pattern.
var libraryTypes = map[LibraryFileType]string{
	LibraryFileTypeImage: "image/%",
	LibraryFileTypePdf:   "application/pdf",
	LibraryFileTypeText:  llm.TextMediaType,
	// Word and Excel, both Office Open XML.
	LibraryFileTypeDocument: "application/vnd.openxmlformats-officedocument.%",
}

func (s *Server) ListFiles(w http.ResponseWriter, r *http.Request, params ListFilesParams) {
	cursorAt, cursorID, err := decodeCursor(params.Cursor)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	user := auth.MustUser(r.Context())
	limit := clampLimit(params.Limit)

	query := sqlc.ListLibraryFilesParams{
		UserID:          &user.ID,
		CursorCreatedAt: cursorAt,
		CursorID:        cursorID,
		RowLimit:        limit + 1,
	}
	if params.Type != nil {
		like, ok := libraryTypes[*params.Type]
		if !ok {
			httpx.Fail(w, r, errInvalidFileType)
			return
		}
		query.ContentTypeLike = &like
	}
	if search := trimmed(params.Search); search != "" {
		pattern := "%" + likeEscaper.Replace(search) + "%"
		query.Pattern = &pattern
	}

	rows, err := s.q.ListLibraryFiles(r.Context(), query)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	page := LibraryFilePage{Items: make([]LibraryFile, 0, limit)}
	if int32(len(rows)) > limit {
		last := rows[limit-1]
		next := encodeCursor(last.CreatedAt, last.ID)
		page.NextCursor = &next
		rows = rows[:limit]
	}
	for _, row := range rows {
		url, err := s.store.PresignGet(r.Context(), row.StorageKey, attachmentTTL)
		if err != nil {
			httpx.Fail(w, r, err)
			return
		}
		page.Items = append(page.Items, LibraryFile{
			Id:                row.ID,
			Filename:          row.Filename,
			ContentType:       row.ContentType,
			Size:              row.SizeBytes,
			Url:               url,
			ConversationId:    row.ConversationID,
			ConversationTitle: row.ConversationTitle,
			CreatedAt:         row.CreatedAt.Time,
		})
	}
	httpx.JSON(w, http.StatusOK, page)
}

// downloadTTL is how long a download link stays good: it is asked for at the
// moment of the click and followed at once.
const downloadTTL = 5 * time.Minute

func (s *Server) GetFileDownload(w http.ResponseWriter, r *http.Request, fileId FileId) {
	user := auth.MustUser(r.Context())
	row, err := s.q.GetLibraryFile(r.Context(), sqlc.GetLibraryFileParams{ID: fileId, UserID: &user.ID})
	if err != nil {
		httpx.Fail(w, r, dbErr(err))
		return
	}
	url, err := s.store.PresignDownload(r.Context(), row.StorageKey, safeFilename(row.Filename, row.ContentType), downloadTTL)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, FileDownload{Url: url, ExpiresAt: time.Now().UTC().Add(downloadTTL)})
}

// GetFileText serves the words of one of the caller's documents (doctext),
// for the file panel: what a model is sent of it.
func (s *Server) GetFileText(w http.ResponseWriter, r *http.Request, fileId FileId, params GetFileTextParams) {
	ctx := r.Context()
	user := auth.MustUser(ctx)
	// Any file of the caller's own, the library's or a temporary chat's.
	rows, err := s.q.ListAttachmentsForUser(ctx, sqlc.ListAttachmentsForUserParams{Ids: []uuid.UUID{fileId}, UserID: &user.ID})
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	if len(rows) == 0 {
		httpx.Fail(w, r, httpx.NewError(http.StatusNotFound, "not_found", "no such file"))
		return
	}
	row := rows[0]
	if !doctext.Supported(row.ContentType) {
		httpx.Fail(w, r, httpx.NewError(http.StatusUnsupportedMediaType, "not_a_document", "only Word, Excel and PDF files are read as words"))
		return
	}
	data, err := s.store.Read(ctx, row.StorageKey, uploadable[row.ContentType])
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	text, err := doctext.Extract(ctx, row.ContentType, data)
	if err != nil {
		slog.InfoContext(ctx, "read a document's words", "attachment_id", row.ID, "error", err)
		httpx.Fail(w, r, httpx.NewError(http.StatusUnprocessableEntity, "document_unreadable", "the document's words cannot be read"))
		return
	}
	content := text.Content
	if params.Preview != nil && *params.Preview {
		content = beginning(content, filePreviewBytes)
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "private, max-age=3600")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write([]byte(content))
}

// filePreviewBytes is how much of a document a library card shows the start of.
const filePreviewBytes = 2 << 10

// beginning is text cut to at most limit bytes, at a character's edge.
func beginning(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	for limit > 0 && !utf8.RuneStart(text[limit]) {
		limit--
	}
	return text[:limit]
}

// DeleteFile removes a file from the library: its row, then its bytes. The
// message it came with still names it in its parts, which is what shows it as
// deleted (removedAttachments) and tells a model it is gone.
func (s *Server) DeleteFile(w http.ResponseWriter, r *http.Request, fileId FileId) {
	user := auth.MustUser(r.Context())
	gone, err := s.q.DeleteLibraryFile(r.Context(), sqlc.DeleteLibraryFileParams{ID: fileId, UserID: &user.ID})
	if err != nil {
		httpx.Fail(w, r, dbErr(err))
		return
	}
	s.removeFiles(r.Context(), []string{gone.StorageKey})
	// A page showing the conversation fetches it again, and finds the file
	// gone from its message.
	s.runner.Hub().Publish(gone.ConversationID, agent.Batch{Events: []agent.Event{agent.TranscriptChanged{}}})
	w.WriteHeader(http.StatusNoContent)
}

// removedAttachments is the files a user message names in its parts that are
// no longer attached to it: deleted from the library since. A file moves with
// its question when the question is edited (MoveAttachments), so the rows on
// the message are the ones it still has.
func removedAttachments(row sqlc.Message, attached []Attachment) []RemovedAttachment {
	if row.Role != sqlc.MessageRoleUser {
		return nil
	}
	parts, err := agent.DecodeParts(row.Parts)
	if err != nil {
		return nil
	}
	kept := make(map[string]bool, len(attached))
	for _, a := range attached {
		kept[a.Id.String()] = true
	}
	var out []RemovedAttachment
	for _, part := range parts {
		ref, ok := part.(llm.AttachmentPart)
		if !ok || kept[ref.Ref] {
			continue
		}
		id, err := uuid.Parse(ref.Ref)
		if err != nil {
			continue
		}
		kept[ref.Ref] = true
		out = append(out, RemovedAttachment{Id: id, Filename: ref.Filename, ContentType: ref.MediaType})
	}
	return out
}

// messageWithFiles maps a row with the files it carries and the ones it was
// sent with that have since been deleted.
func messageWithFiles(row sqlc.Message, attached map[uuid.UUID][]Attachment) Message {
	m := toMessage(row, attached[row.ID])
	if removed := removedAttachments(row, attached[row.ID]); len(removed) > 0 {
		m.RemovedAttachments = &removed
	}
	return m
}
