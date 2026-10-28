package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/qunqin24/openchat/apps/server/internal/agent"
	"github.com/qunqin24/openchat/apps/server/internal/auth"
	"github.com/qunqin24/openchat/apps/server/internal/db/dbtest"
	"github.com/qunqin24/openchat/apps/server/internal/db/sqlc"
	"github.com/qunqin24/openchat/apps/server/internal/llm"
)

// The library over its handlers: which files it lists, that nobody else's
// are reachable, and what deleting one leaves in the message it was sent with.
func TestLibrary(t *testing.T) {
	pool := dbtest.Pool(t, "api_library")
	ctx := context.Background()
	q := sqlc.New(pool)

	tag := uuid.NewString()
	newUser := func(name string) sqlc.User {
		var u sqlc.User
		if err := pool.QueryRow(ctx, `INSERT INTO users (email, name) VALUES ($1, $2) RETURNING id, email, name, role`,
			name+tag+"@test.invalid", name+tag).Scan(&u.ID, &u.Email, &u.Name, &u.Role); err != nil {
			t.Fatal(err)
		}
		return u
	}
	alice, mallory := newUser("alice"), newUser("mallory")
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM users WHERE id = ANY($1)", []uuid.UUID{alice.ID, mallory.ID})
	})
	newConversation := func(user sqlc.User, title string, temporary bool) sqlc.Conversation {
		conv, err := q.CreateConversation(ctx, sqlc.CreateConversationParams{UserID: &user.ID, Title: title, Temporary: temporary})
		if err != nil {
			t.Fatal(err)
		}
		return conv
	}

	blobs := &memoryStore{blobs: map[string][]byte{}}
	runner := agent.NewRunner(pool, agent.NewHub(), NewModelResolver(pool), nil, agent.Options{Blobs: blobs})
	t.Cleanup(func() { _ = runner.Shutdown(context.Background()) })
	server := &Server{pool: pool, q: q, runner: runner, store: blobs}

	// sent is a user message in conv carrying a file of each name, oldest first.
	sent := func(user sqlc.User, conv sqlc.Conversation, names ...string) (sqlc.Message, []sqlc.Attachment) {
		t.Helper()
		var files []sqlc.Attachment
		for _, name := range names {
			contentType := "image/png"
			if strings.HasSuffix(name, ".pdf") {
				contentType = "application/pdf"
			}
			file, err := q.CreateAttachment(ctx, sqlc.CreateAttachmentParams{
				UserID: &user.ID, StorageKey: "uploads/" + uuid.NewString(), Filename: name, ContentType: contentType, SizeBytes: 4,
			})
			if err != nil {
				t.Fatal(err)
			}
			blobs.put(file.StorageKey, []byte("data"))
			files = append(files, file)
			time.Sleep(2 * time.Millisecond) // distinct created_at, for the order
		}
		input, err := appendInput(ctx, q, conv.ID, "look", inputParts("look", files))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := q.AttachToMessage(ctx, sqlc.AttachToMessageParams{MessageID: &input.ID, UserID: &user.ID, Ids: attachmentIDs(files)}); err != nil {
			t.Fatal(err)
		}
		return input, files
	}

	conv := newConversation(alice, "files", false)
	_, kept := sent(alice, conv, "red.png", "Report.pdf")
	_, temporary := sent(alice, newConversation(alice, "temp", true), "temp.png")
	deleted := newConversation(alice, "gone", false)
	_, inDeleted := sent(alice, deleted, "gone.png")
	if _, err := pool.Exec(ctx, `UPDATE conversations SET deleted_at = now() WHERE id = $1`, deleted.ID); err != nil {
		t.Fatal(err)
	}
	_, hers := sent(mallory, newConversation(mallory, "hers", false), "hers.png")

	as := func(user sqlc.User, method, target string, handler func(http.ResponseWriter, *http.Request)) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(method, target, nil)
		handler(rec, req.WithContext(auth.WithUser(req.Context(), user)))
		return rec
	}
	list := func(user sqlc.User, params ListFilesParams) LibraryFilePage {
		t.Helper()
		rec := as(user, http.MethodGet, "/", func(w http.ResponseWriter, r *http.Request) { server.ListFiles(w, r, params) })
		if rec.Code != http.StatusOK {
			t.Fatalf("list: %d %s", rec.Code, rec.Body)
		}
		var page LibraryFilePage
		if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
			t.Fatal(err)
		}
		return page
	}
	names := func(page LibraryFilePage) string {
		var out []string
		for _, item := range page.Items {
			out = append(out, item.Filename)
		}
		return strings.Join(out, ",")
	}

	t.Run("lists the files of conversations that can be opened, newest first", func(t *testing.T) {
		page := list(alice, ListFilesParams{})
		if got := names(page); got != "Report.pdf,red.png" {
			t.Fatalf("files = %q", got)
		}
		if page.Items[0].ConversationId != conv.ID || page.Items[0].ConversationTitle != "files" || page.Items[0].Url == "" {
			t.Errorf("item = %+v", page.Items[0])
		}
		image, pdf := LibraryFileTypeImage, LibraryFileTypePdf
		if got := names(list(alice, ListFilesParams{Type: &image})); got != "red.png" {
			t.Errorf("images = %q", got)
		}
		if got := names(list(alice, ListFilesParams{Type: &pdf})); got != "Report.pdf" {
			t.Errorf("pdfs = %q", got)
		}
		search := "REPORT"
		if got := names(list(alice, ListFilesParams{Search: &search})); got != "Report.pdf" {
			t.Errorf("search = %q", got)
		}
		wild := "%"
		if got := names(list(alice, ListFilesParams{Search: &wild})); got != "" {
			t.Errorf("search %% = %q, want nothing (a literal)", got)
		}
	})

	t.Run("pages", func(t *testing.T) {
		one := 1
		first := list(alice, ListFilesParams{Limit: &one})
		if names(first) != "Report.pdf" || first.NextCursor == nil {
			t.Fatalf("first page = %q, next %v", names(first), first.NextCursor)
		}
		second := list(alice, ListFilesParams{Limit: &one, Cursor: first.NextCursor})
		if names(second) != "red.png" || second.NextCursor != nil {
			t.Fatalf("second page = %q, next %v", names(second), second.NextCursor)
		}
	})

	t.Run("a bad type is refused", func(t *testing.T) {
		bad := LibraryFileType("zip")
		rec := as(alice, http.MethodGet, "/", func(w http.ResponseWriter, r *http.Request) { server.ListFiles(w, r, ListFilesParams{Type: &bad}) })
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("got %d %s", rec.Code, rec.Body)
		}
	})

	t.Run("files outside the library are not there", func(t *testing.T) {
		for _, id := range []uuid.UUID{hers[0].ID, temporary[0].ID, inDeleted[0].ID} {
			download := as(alice, http.MethodGet, "/", func(w http.ResponseWriter, r *http.Request) { server.GetFileDownload(w, r, id) })
			remove := as(alice, http.MethodDelete, "/", func(w http.ResponseWriter, r *http.Request) { server.DeleteFile(w, r, id) })
			if download.Code != http.StatusNotFound || remove.Code != http.StatusNotFound {
				t.Errorf("file %s: download %d, delete %d; want 404", id, download.Code, remove.Code)
			}
		}
		if got := names(list(mallory, ListFilesParams{})); got != "hers.png" {
			t.Errorf("mallory's files = %q", got)
		}
	})

	t.Run("a download link", func(t *testing.T) {
		rec := as(alice, http.MethodGet, "/", func(w http.ResponseWriter, r *http.Request) { server.GetFileDownload(w, r, kept[0].ID) })
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "/download/"+kept[0].StorageKey) {
			t.Fatalf("got %d %s", rec.Code, rec.Body)
		}
	})

	t.Run("deleting a file leaves its place in the message", func(t *testing.T) {
		red := kept[0]
		rec := as(alice, http.MethodDelete, "/", func(w http.ResponseWriter, r *http.Request) { server.DeleteFile(w, r, red.ID) })
		if rec.Code != http.StatusNoContent {
			t.Fatalf("delete: %d %s", rec.Code, rec.Body)
		}
		if _, err := blobs.Read(ctx, red.StorageKey, 100); err == nil {
			t.Error("the stored file is still there")
		}
		if got := names(list(alice, ListFilesParams{})); got != "Report.pdf" {
			t.Errorf("files after delete = %q", got)
		}
		again := as(alice, http.MethodDelete, "/", func(w http.ResponseWriter, r *http.Request) { server.DeleteFile(w, r, red.ID) })
		if again.Code != http.StatusNotFound {
			t.Errorf("second delete: %d", again.Code)
		}

		rows, err := q.ListTranscript(ctx, conv.ID)
		if err != nil {
			t.Fatal(err)
		}
		page, err := server.messagePage(ctx, rows)
		if err != nil {
			t.Fatal(err)
		}
		message := page.Items[0]
		if message.Attachments == nil || len(*message.Attachments) != 1 || (*message.Attachments)[0].Filename != "Report.pdf" {
			t.Errorf("attachments = %+v", message.Attachments)
		}
		if message.RemovedAttachments == nil || len(*message.RemovedAttachments) != 1 {
			t.Fatalf("removed = %+v", message.RemovedAttachments)
		}
		if removed := (*message.RemovedAttachments)[0]; removed.Id != red.ID || removed.Filename != "red.png" || removed.ContentType != "image/png" {
			t.Errorf("removed = %+v", removed)
		}

		// The message still names both: the reference to the deleted one is
		// what the model is told about (agent.attachmentResolver).
		parts, err := agent.DecodeParts(rows[0].Parts)
		if err != nil {
			t.Fatal(err)
		}
		var refs int
		for _, part := range parts {
			if _, ok := part.(llm.AttachmentPart); ok {
				refs++
			}
		}
		if refs != 2 {
			t.Errorf("the message names %d files, want both still", refs)
		}
	})
}
