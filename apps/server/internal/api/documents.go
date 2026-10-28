package api

import (
	"context"
	"errors"
	"net/http"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/qunqin24/openchat/apps/server/internal/auth"
	"github.com/qunqin24/openchat/apps/server/internal/db/sqlc"
	"github.com/qunqin24/openchat/apps/server/internal/httpx"
	"github.com/qunqin24/openchat/apps/server/internal/memory"
)

// Writing blocks: a text a model writes or revises for the person - an
// article, an email, a post - kept apart from the transcript so it can be
// changed in place: by the person in the side panel (UpdateDocument), by the
// model with targeted edits (document_tool.go). Every change is a version.
//
// A model's text is vouched for, as its memories are. The person's text
// reaches a model only when the model reads the document, so that is where it
// is checked (documentText): a strike like a message sent, since it is being
// sent; saving it checks nothing, or every pause in typing would.

const (
	maxDocumentTitle   = 200
	maxDocumentContent = 100_000
)

var (
	errVersionConflict = httpx.NewError(http.StatusConflict, "version_conflict",
		"the document has a newer version; reload it before saving")
	errDocumentTooLong = httpx.NewFieldError("content", "document_too_long", "a document is at most 100000 characters")
)

// documentHash is what a version's `checked` records: its title and text.
func documentHash(title, content string) string {
	return memory.Hash(title + "\n" + content)
}

func toDocument(doc sqlc.Document, versions []sqlc.DocumentVersion) Document {
	out := Document{Id: doc.ID, ConversationId: doc.ConversationID, CreatedAt: doc.CreatedAt.Time, Versions: make([]DocumentVersion, 0, len(versions))}
	for _, v := range versions {
		out.Versions = append(out.Versions, DocumentVersion{
			Version: int(v.Version), Title: v.Title, Content: v.Content,
			Source: DocumentVersionSource(v.Source), CreatedAt: v.CreatedAt.Time,
		})
	}
	return out
}

func (s *Server) documentWithVersions(ctx context.Context, doc sqlc.Document) (Document, error) {
	versions, err := s.q.ListDocumentVersions(ctx, doc.ID)
	if err != nil {
		return Document{}, err
	}
	return toDocument(doc, versions), nil
}

// ListConversationDocuments is a conversation's writing blocks, the most
// recently changed first, to reopen one without scrolling back to it.
func (s *Server) ListConversationDocuments(w http.ResponseWriter, r *http.Request, conversationId uuid.UUID) {
	ctx := r.Context()
	user := auth.MustUser(ctx)
	if _, err := s.q.GetConversation(ctx, sqlc.GetConversationParams{ID: conversationId, UserID: &user.ID}); err != nil {
		httpx.Fail(w, r, dbErr(err))
		return
	}
	docs, err := s.q.ListConversationDocuments(ctx, sqlc.ListConversationDocumentsParams{ConversationID: conversationId, UserID: user.ID})
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	items := make([]DocumentSummary, 0, len(docs))
	for _, d := range docs {
		items = append(items, DocumentSummary{Id: d.ID, Title: d.Title, Version: int(d.Version), UpdatedAt: d.UpdatedAt.Time})
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].UpdatedAt.After(items[j].UpdatedAt) })
	httpx.JSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) GetDocument(w http.ResponseWriter, r *http.Request, documentId uuid.UUID) {
	user := auth.MustUser(r.Context())
	doc, err := s.q.GetDocument(r.Context(), sqlc.GetDocumentParams{ID: documentId, UserID: user.ID})
	if err != nil {
		httpx.Fail(w, r, dbErr(err))
		return
	}
	out, err := s.documentWithVersions(r.Context(), doc)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

func (s *Server) UpdateDocument(w http.ResponseWriter, r *http.Request, documentId uuid.UUID) {
	ctx := r.Context()
	var body UpdateDocumentRequest
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	title := strings.TrimSpace(body.Title)
	if title == "" || utf8.RuneCountInString(title) > maxDocumentTitle {
		httpx.Fail(w, r, httpx.NewFieldError("title", "invalid_title", "title must be 1 to 200 characters"))
		return
	}
	if utf8.RuneCountInString(body.Content) > maxDocumentContent {
		httpx.Fail(w, r, errDocumentTooLong)
		return
	}
	user := auth.MustUser(ctx)

	var doc sqlc.Document
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		var err error
		if doc, err = q.LockDocument(ctx, sqlc.LockDocumentParams{ID: documentId, UserID: user.ID}); err != nil {
			return dbErr(err)
		}
		latest, err := q.LatestDocumentVersion(ctx, doc.ID)
		if err != nil {
			return err
		}
		if int(latest.Version) != body.BaseVersion {
			return errVersionConflict
		}
		if latest.Title == title && latest.Content == body.Content {
			return nil // nothing changed: no version
		}
		_, err = q.InsertDocumentVersion(ctx, sqlc.InsertDocumentVersionParams{
			DocumentID: doc.ID, Title: title, Content: body.Content, Source: "user",
		})
		return err
	})
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	out, err := s.documentWithVersions(ctx, doc)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

// documentText is a document's latest version for a model to read: the
// person's text checked by content moderation first, if it has not been as it
// is now. A refusal is the 422 content_blocked of a message, and a strike.
func (s *Server) documentText(ctx context.Context, user sqlc.User, doc sqlc.Document) (sqlc.DocumentVersion, error) {
	latest, err := s.q.LatestDocumentVersion(ctx, doc.ID)
	if err != nil {
		return latest, err
	}
	hash := documentHash(latest.Title, latest.Content)
	if latest.Checked != nil && *latest.Checked == hash {
		return latest, nil
	}
	who := s.accountSubject(ctx, user, doc.ConversationID, ModerationSourceDocument)
	checked, err := s.checkText(ctx, who, latest.Title+"\n\n"+latest.Content)
	if err != nil {
		return latest, err
	}
	if checked {
		if err := s.q.SetDocumentVersionChecked(ctx, sqlc.SetDocumentVersionCheckedParams{
			DocumentID: doc.ID, Version: latest.Version, Checked: &hash,
		}); err != nil && !errors.Is(err, context.Canceled) {
			return latest, err
		}
	}
	return latest, nil
}
