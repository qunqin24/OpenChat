package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/qunqin/openchat/apps/server/internal/auth"
	"github.com/qunqin/openchat/apps/server/internal/db/sqlc"
	"github.com/qunqin/openchat/apps/server/internal/httpx"
)

// The caller's own data, as a whole: take it out, or delete it. Both act on
// the account of the session and have no parameter that could name another.

// exportBatch is how many conversations are read at a time.
const exportBatch = 50

// ExportMyConversations writes the caller's kept conversations as one JSON
// document, as it reads them.
//
// A conversation at a time is all that is ever in memory: its row, the text of
// it, and the names of the files its questions carried. The
// document is the contract's ConversationExport, assembled by hand around the
// conversations so that none of them waits on the others.
func (s *Server) ExportMyConversations(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user := auth.MustUser(ctx)
	now := time.Now().UTC()

	// The first batch is read before the response begins, so that a database
	// that is down is a 500 and not a download that stops short.
	next, err := s.exportPage(r, user.ID, pgtype.Timestamptz{}, nil)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}

	h := w.Header()
	h.Set("Content-Type", "application/json")
	h.Set("Content-Disposition", `attachment; filename="openchat-export-`+now.Format("2006-01-02")+`.json"`)
	h.Set("Cache-Control", "no-store")
	stamp, _ := json.Marshal(now)
	_, _ = w.Write([]byte(`{"version":1,"exportedAt":` + string(stamp) + `,"conversations":[`))

	flusher, _ := w.(http.Flusher)
	first := true
	for len(next.rows) > 0 {
		for _, conv := range next.rows {
			exported, err := s.exportConversation(r, conv)
			if err != nil {
				s.abortExport(r, err)
			}
			body, err := json.Marshal(exported)
			if err != nil {
				s.abortExport(r, err)
			}
			if !first {
				_, _ = w.Write([]byte{','})
			}
			first = false
			if _, err := w.Write(body); err != nil {
				return // the reader went away
			}
			if flusher != nil {
				flusher.Flush()
			}
		}
		if !next.cursorAt.Valid {
			break
		}
		if next, err = s.exportPage(r, user.ID, next.cursorAt, next.cursorID); err != nil {
			s.abortExport(r, err)
		}
	}
	_, _ = w.Write([]byte(`]}`))
}

// abortExport ends a download that failed once it had begun. The status line
// is gone by then, so the connection is cut: a reader sees a transfer that
// failed instead of a document that looks whole and is not.
func (s *Server) abortExport(r *http.Request, err error) {
	slog.ErrorContext(r.Context(), "export conversations", "error", err)
	panic(http.ErrAbortHandler)
}

// exportedFile is the contract's inline attachment object.
type exportedFile = struct {
	ContentType string `json:"contentType"`
	Filename    string `json:"filename"`
	Size        int64  `json:"size"`
}

// exportRows is a batch of conversations and where the one after it starts.
type exportRows struct {
	rows     []sqlc.Conversation
	cursorAt pgtype.Timestamptz // invalid when this was the last batch
	cursorID *uuid.UUID
}

// exportPage reads the batch of the caller's conversations after a cursor. The
// owner is a parameter of the query, as it is wherever conversations are read.
func (s *Server) exportPage(r *http.Request, userID uuid.UUID, at pgtype.Timestamptz, id *uuid.UUID) (exportRows, error) {
	rows, err := s.q.ListConversations(r.Context(), sqlc.ListConversationsParams{
		UserID:          &userID,
		CursorUpdatedAt: at,
		CursorID:        id,
		RowLimit:        exportBatch + 1,
	})
	if err != nil {
		return exportRows{}, err
	}
	out := exportRows{rows: rows}
	if len(rows) > exportBatch {
		out.rows = rows[:exportBatch]
		last := out.rows[exportBatch-1]
		out.cursorAt, out.cursorID = last.UpdatedAt, &last.ID
	}
	return out, nil
}

// exportConversation is one conversation's row with its text.
func (s *Server) exportConversation(r *http.Request, conv sqlc.Conversation) (ExportedConversation, error) {
	ctx := r.Context()
	rows, err := s.q.ListTranscript(ctx, conv.ID)
	if err != nil {
		return ExportedConversation{}, err
	}

	// The files a question carried.
	ids := make([]uuid.UUID, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ID)
	}
	files := map[uuid.UUID][]sqlc.Attachment{}
	if len(ids) > 0 {
		found, err := s.q.ListAttachmentsByMessages(ctx, ids)
		if err != nil {
			return ExportedConversation{}, err
		}
		for _, f := range found {
			if f.MessageID != nil {
				files[*f.MessageID] = append(files[*f.MessageID], f)
			}
		}
	}

	out := ExportedConversation{
		Id:        conv.ID,
		Title:     conv.Title,
		Model:     conv.Model,
		CreatedAt: conv.CreatedAt.Time,
		UpdatedAt: conv.UpdatedAt.Time,
		Messages:  []ExportedMessage{},
	}
	for _, row := range rows {
		// An assistant row without text only made tool calls (or failed before
		// saying anything): there is nothing in it to take out.
		if row.Role == sqlc.MessageRoleAssistant && strings.TrimSpace(row.Content) == "" {
			continue
		}
		msg := ExportedMessage{
			Id:        row.ID,
			Role:      ExportedMessageRole(row.Role),
			Content:   row.Content,
			CreatedAt: row.CreatedAt.Time,
		}
		if row.Role == sqlc.MessageRoleAssistant {
			msg.Model = row.Model
		}
		held := files[row.ID]
		if row.Role == sqlc.MessageRoleUser && len(held) > 0 {
			names := make([]exportedFile, 0, len(held))
			for _, f := range held {
				names = append(names, exportedFile{ContentType: f.ContentType, Filename: f.Filename, Size: f.SizeBytes})
			}
			msg.Attachments = &names
		}
		out.Messages = append(out.Messages, msg)
	}
	return out, nil
}

// DeleteAllMyConversations deletes every conversation of the caller's, the way
// DeleteConversation deletes one: the rows are marked, the runs they have going
// are stopped, and nothing is read from them again. All the conversations are
// locked first, so no send is starting a run in one of them.
func (s *Server) DeleteAllMyConversations(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user := auth.MustUser(ctx)

	var (
		deleted   int64
		cancelled []uuid.UUID
	)
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		ids, err := q.LockUserConversations(ctx, &user.ID)
		if err != nil {
			return err
		}
		if deleted, err = q.SoftDeleteAllConversations(ctx, &user.ID); err != nil {
			return err
		}
		cancelled, err = q.CancelConversationRuns(ctx, ids)
		return err
	})
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	s.cancelRuns(cancelled)
	httpx.JSON(w, http.StatusOK, DeletedConversations{Deleted: deleted})
}
