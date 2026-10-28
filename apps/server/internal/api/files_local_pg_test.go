package api

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/qunqin24/openchat/apps/server/internal/agent"
	"github.com/qunqin24/openchat/apps/server/internal/auth"
	"github.com/qunqin24/openchat/apps/server/internal/config"
	"github.com/qunqin24/openchat/apps/server/internal/db/sqlc"
	"github.com/qunqin24/openchat/apps/server/internal/storage"
)

// The local-disk driver, over the real router: the browser's flow of asking for
// an upload URL, PUTting the bytes to it, sending a message and reading the
// file back, with the model behind a fake OpenAI endpoint. Needs the same
// scratch database as TestAttachments.
func TestLocalStorageAttachments(t *testing.T) {
	pool := agentTestPool(t)
	ctx := context.Background()
	q := sqlc.New(pool)

	var (
		bodiesMu sync.Mutex
		bodies   []string
	)
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		bodiesMu.Lock()
		bodies = append(bodies, string(raw))
		bodiesMu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, `data: {"id":"r","model":"m","choices":[{"index":0,"delta":{"role":"assistant","content":"A red square."},"finish_reason":null}]}`+"\n\n")
		_, _ = fmt.Fprint(w, `data: {"id":"r","model":"m","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`+"\n\n")
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(endpoint.Close)

	tag := uuid.NewString()
	var alice sqlc.User
	if err := pool.QueryRow(ctx, `INSERT INTO users (email, name) VALUES ($1, $2) RETURNING id, email, name, role`,
		"alice"+tag+"@test.invalid", "alice"+tag).Scan(&alice.ID, &alice.Email, &alice.Name, &alice.Role); err != nil {
		t.Fatal(err)
	}
	conv, err := q.CreateConversation(ctx, sqlc.CreateConversationParams{UserID: &alice.ID, Title: "files"})
	if err != nil {
		t.Fatal(err)
	}
	modelID := "vision-" + tag
	channel, err := q.CreateChannel(ctx, sqlc.CreateChannelParams{
		Name: "fake " + tag, Type: sqlc.ChannelTypeOpenaiCompatible, Enabled: true,
		BaseUrl: endpoint.URL + "/v1", ApiKey: "sk-test",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := q.CreateSelectedChannelModels(ctx, sqlc.CreateSelectedChannelModelsParams{
		ChannelID: channel.ID, ModelIds: []string{modelID}, DisplayNames: []string{""}, Sources: []string{"manual"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := q.SaveModelOverride(ctx, sqlc.SaveModelOverrideParams{ModelID: modelID, InputModalities: []string{"image", "pdf"}}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM channels WHERE id = $1", channel.ID)
		_, _ = pool.Exec(context.Background(), "DELETE FROM model_overrides WHERE model_id = $1", modelID)
		_, _ = pool.Exec(context.Background(), "DELETE FROM users WHERE id = $1", alice.ID)
	})

	local, err := storage.NewLocal(t.TempDir(), []byte("test-secret"), "")
	if err != nil {
		t.Fatal(err)
	}
	runner := agent.NewRunner(pool, agent.NewHub(), NewModelResolver(pool), nil,
		agent.Options{Blobs: local, Modalities: NewModalities(pool), PartialInterval: 5 * time.Millisecond})
	t.Cleanup(func() { _ = runner.Shutdown(context.Background()) })
	cfg := &config.Config{CORSOrigins: []string{"http://localhost"}}
	server := NewServer(cfg, pool, local, runner)
	web := httptest.NewServer(NewRouter(cfg, server, auth.NewMiddleware(q)))
	t.Cleanup(web.Close)

	// The handlers that need a signed-in user are called directly, as in
	// TestAttachments; the signed URLs go over HTTP, as the browser sends them.
	call := func(handler func(http.ResponseWriter, *http.Request), body string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
		handler(rec, req.WithContext(auth.WithUser(req.Context(), alice)))
		return rec
	}
	issue := func(name, contentType string, size int) Upload {
		t.Helper()
		rec := call(server.CreateUpload, fmt.Sprintf(`{"filename":%q,"contentType":%q,"size":%d}`, name, contentType, size))
		if rec.Code != http.StatusCreated {
			t.Fatalf("issue %s: %d %s", name, rec.Code, rec.Body)
		}
		var up Upload
		if err := json.Unmarshal(rec.Body.Bytes(), &up); err != nil {
			t.Fatal(err)
		}
		return up
	}
	do := func(method, target, contentType string, body io.Reader) *http.Response {
		t.Helper()
		req, err := http.NewRequest(method, web.URL+target, body)
		if err != nil {
			t.Fatal(err)
		}
		if contentType != "" {
			req.Header.Set("Content-Type", contentType)
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = res.Body.Close() })
		return res
	}
	wantStatus := func(res *http.Response, status int, code string) {
		t.Helper()
		raw, _ := io.ReadAll(res.Body)
		if res.StatusCode != status || !strings.Contains(string(raw), code) {
			t.Fatalf("got %d %s, want %d %s", res.StatusCode, raw, status, code)
		}
	}
	// The signed URL for the same file with another method, expiry or signature.
	altered := func(raw string, change func(q url.Values)) string {
		u, err := url.Parse(raw)
		if err != nil {
			t.Fatal(err)
		}
		q := u.Query()
		change(q)
		u.RawQuery = q.Encode()
		return u.String()
	}

	t.Run("the upload URL is on this API and signed", func(t *testing.T) {
		up := issue("square.png", "image/png", len(testPNG))
		u, err := url.Parse(up.UploadUrl)
		if err != nil || u.Host != "" || !strings.HasPrefix(u.Path, "/api/v1/files/content/uploads/") ||
			u.Query().Get("expires") == "" || u.Query().Get("signature") == "" {
			t.Fatalf("upload URL = %q", up.UploadUrl)
		}
	})

	t.Run("an upload is held to what it announced", func(t *testing.T) {
		up := issue("square.png", "image/png", len(testPNG))

		wantStatus(do(http.MethodPut, altered(up.UploadUrl, func(q url.Values) { q.Set("signature", "AAAA") }), "image/png", bytes.NewReader(testPNG)), http.StatusForbidden, "invalid_signature")
		wantStatus(do(http.MethodPut, altered(up.UploadUrl, func(q url.Values) { q.Del("signature") }), "image/png", bytes.NewReader(testPNG)), http.StatusForbidden, "invalid_signature")
		wantStatus(do(http.MethodPut, altered(up.UploadUrl, func(q url.Values) { q.Set("expires", "9999999999") }), "image/png", bytes.NewReader(testPNG)), http.StatusForbidden, "invalid_signature")
		wantStatus(do(http.MethodPut, up.UploadUrl, "application/pdf", bytes.NewReader(testPNG)), http.StatusUnsupportedMediaType, "unsupported_file_type")
		wantStatus(do(http.MethodPut, up.UploadUrl, "", bytes.NewReader(testPNG)), http.StatusUnsupportedMediaType, "unsupported_file_type")

		// A size over the limit is refused whether or not it is announced.
		big := make([]byte, 5<<20+1)
		wantStatus(do(http.MethodPut, up.UploadUrl, "image/png", bytes.NewReader(big)), http.StatusRequestEntityTooLarge, "file_too_large")
		wantStatus(do(http.MethodPut, up.UploadUrl, "image/png", io.MultiReader(bytes.NewReader(big))), http.StatusRequestEntityTooLarge, "file_too_large")

		// An expired URL is refused, even a correct one.
		var key string
		if err := pool.QueryRow(ctx, `SELECT storage_key FROM attachments WHERE id = $1`, up.Id).Scan(&key); err != nil {
			t.Fatal(err)
		}
		stale, err := local.PresignPut(ctx, key, "image/png", -time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		wantStatus(do(http.MethodPut, stale, "image/png", bytes.NewReader(testPNG)), http.StatusForbidden, "url_expired")

		// A read URL does not write, and none of it stored anything.
		read, err := local.PresignGet(ctx, key, time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		wantStatus(do(http.MethodPut, read, "image/png", bytes.NewReader(testPNG)), http.StatusForbidden, "invalid_signature")
		if _, err := local.Size(ctx, key); err == nil {
			t.Fatal("a refused upload left a file")
		}
		wantStatus(do(http.MethodGet, read, "", nil), http.StatusNotFound, "not_found")

		// The key of a file nobody issued an upload for is not a way in.
		unknown, _ := local.PresignPut(ctx, "uploads/2026/01/01/"+uuid.NewString()+".png", "image/png", time.Hour)
		wantStatus(do(http.MethodPut, unknown, "image/png", bytes.NewReader(testPNG)), http.StatusNotFound, "not_found")

		// The right one works.
		if res := do(http.MethodPut, up.UploadUrl, "image/png", bytes.NewReader(testPNG)); res.StatusCode != http.StatusOK {
			t.Fatalf("PUT: %d", res.StatusCode)
		}
		if size, err := local.Size(ctx, key); err != nil || size != int64(len(testPNG)) {
			t.Errorf("stored size = %d, %v", size, err)
		}
	})

	t.Run("an image reaches the model as inline data and is served back safely", func(t *testing.T) {
		up := issue("square.png", "image/png", len(testPNG))
		if res := do(http.MethodPut, up.UploadUrl, "image/png", bytes.NewReader(testPNG)); res.StatusCode != http.StatusOK {
			t.Fatalf("PUT: %d", res.StatusCode)
		}

		rec := call(func(w http.ResponseWriter, r *http.Request) { server.CreateMessage(w, r, conv.ID) },
			fmt.Sprintf(`{"content":"what is this?","model":%q,"attachmentIds":["%s"]}`, modelID, up.Id))
		if rec.Code != http.StatusAccepted {
			t.Fatalf("send: %d %s", rec.Code, rec.Body)
		}
		var created CreateMessageResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
			t.Fatal(err)
		}

		deadline := time.Now().Add(10 * time.Second)
		for {
			run, err := q.GetRun(ctx, created.Run.Id)
			if err != nil {
				t.Fatal(err)
			}
			if run.Status != sqlc.RunStatusRunning {
				if run.Status != sqlc.RunStatusCompleted {
					t.Fatalf("run %s (%v)", run.Status, run.ErrorCode)
				}
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("the run did not finish")
			}
			time.Sleep(20 * time.Millisecond)
		}
		bodiesMu.Lock()
		want := "data:image/png;base64," + base64.StdEncoding.EncodeToString(testPNG)
		if len(bodies) != 1 || !strings.Contains(bodies[0], want) {
			t.Errorf("the model was not sent the image inline: %v", bodies)
		}
		if len(bodies) == 1 && strings.Contains(bodies[0], "/files/content/") {
			t.Errorf("a storage URL went to the provider: %s", bodies[0])
		}
		bodiesMu.Unlock()

		// Once a message has it, the upload is part of the transcript.
		wantStatus(do(http.MethodPut, up.UploadUrl, "image/png", bytes.NewReader([]byte("other"))), http.StatusConflict, "already_attached")

		// The transcript hands back a signed URL that serves the file.
		page := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		server.ListMessages(page, req.WithContext(auth.WithUser(req.Context(), alice)), conv.ID, ListMessagesParams{})
		var msgs MessagePage
		_ = json.Unmarshal(page.Body.Bytes(), &msgs)
		if len(msgs.Items) == 0 || msgs.Items[0].Attachments == nil {
			t.Fatalf("listed messages: %s", page.Body)
		}
		readURL := (*msgs.Items[0].Attachments)[0].Url

		res := do(http.MethodGet, readURL, "", nil)
		got, _ := io.ReadAll(res.Body)
		if res.StatusCode != http.StatusOK || !bytes.Equal(got, testPNG) {
			t.Fatalf("GET: %d %q", res.StatusCode, got)
		}
		for header, want := range map[string]string{
			"Content-Type":            "image/png",
			"X-Content-Type-Options":  "nosniff",
			"Content-Disposition":     `inline; filename=square.png`,
			"Content-Security-Policy": "default-src 'none'",
		} {
			if got := res.Header.Get(header); got != want {
				t.Errorf("%s = %q, want %q", header, got, want)
			}
		}

		// The library's download link is the same file, offered for saving.
		dl := httptest.NewRecorder()
		dlReq := httptest.NewRequest(http.MethodGet, "/", nil)
		server.GetFileDownload(dl, dlReq.WithContext(auth.WithUser(dlReq.Context(), alice)), up.Id)
		var link FileDownload
		if err := json.Unmarshal(dl.Body.Bytes(), &link); dl.Code != http.StatusOK || err != nil {
			t.Fatalf("download link: %d %s", dl.Code, dl.Body)
		}
		saved := do(http.MethodGet, link.Url, "", nil)
		if got := saved.Header.Get("Content-Disposition"); saved.StatusCode != http.StatusOK || got != "attachment; filename=square.png" {
			t.Errorf("download: %d, Content-Disposition = %q", saved.StatusCode, got)
		}

		// Ranges work, which is what a PDF viewer asks for.
		req2, _ := http.NewRequest(http.MethodGet, web.URL+readURL, nil)
		req2.Header.Set("Range", "bytes=0-3")
		part, err := http.DefaultClient.Do(req2)
		if err != nil {
			t.Fatal(err)
		}
		defer part.Body.Close()
		if head, _ := io.ReadAll(part.Body); part.StatusCode != http.StatusPartialContent || !bytes.Equal(head, testPNG[:4]) {
			t.Errorf("range: %d %q", part.StatusCode, head)
		}

		// A write URL does not read, and a changed or missing signature does not either.
		wantStatus(do(http.MethodGet, up.UploadUrl, "", nil), http.StatusForbidden, "invalid_signature")
		wantStatus(do(http.MethodGet, altered(readURL, func(q url.Values) { q.Set("signature", "AAAA") }), "", nil), http.StatusForbidden, "invalid_signature")
		wantStatus(do(http.MethodGet, altered(readURL, func(q url.Values) { q.Del("signature"); q.Del("expires") }), "", nil), http.StatusForbidden, "invalid_signature")
	})

	t.Run("a PDF is served with the sandbox and a safe name", func(t *testing.T) {
		up := issue("../../evil \"name\".pdf", "application/pdf", len(testPDF))
		if res := do(http.MethodPut, up.UploadUrl, "application/pdf", bytes.NewReader(testPDF)); res.StatusCode != http.StatusOK {
			t.Fatalf("PUT: %d", res.StatusCode)
		}
		var key string
		if err := pool.QueryRow(ctx, `SELECT storage_key FROM attachments WHERE id = $1`, up.Id).Scan(&key); err != nil {
			t.Fatal(err)
		}
		read, _ := local.PresignGet(ctx, key, time.Hour)
		res := do(http.MethodGet, read, "", nil)
		if res.StatusCode != http.StatusOK || res.Header.Get("Content-Type") != "application/pdf" ||
			res.Header.Get("Content-Security-Policy") != "default-src 'none'; sandbox" ||
			res.Header.Get("X-Content-Type-Options") != "nosniff" {
			t.Errorf("headers: %d %v", res.StatusCode, res.Header)
		}
		if disposition := res.Header.Get("Content-Disposition"); strings.Contains(disposition, "..") ||
			strings.Contains(disposition, "/") || !strings.HasPrefix(disposition, "inline;") {
			t.Errorf("Content-Disposition = %q", disposition)
		}
	})
}
