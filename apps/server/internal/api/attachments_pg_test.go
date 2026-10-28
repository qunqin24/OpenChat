package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/qunqin24/openchat/apps/server/internal/agent"
	"github.com/qunqin24/openchat/apps/server/internal/auth"
	"github.com/qunqin24/openchat/apps/server/internal/db/sqlc"
)

// memoryStore is a blob store that keeps bytes in a map. It stands in for S3 in
// both directions: the API signs and measures through it, the runner reads
// through it.
type memoryStore struct {
	mu    sync.Mutex
	blobs map[string][]byte
}

func (m *memoryStore) PresignPut(_ context.Context, key, _ string, _ time.Duration) (string, error) {
	return "http://blobs.test/put/" + key, nil
}

func (m *memoryStore) PresignGet(_ context.Context, key string, _ time.Duration) (string, error) {
	return "http://blobs.test/get/" + key, nil
}

func (m *memoryStore) PresignDownload(_ context.Context, key, _ string, _ time.Duration) (string, error) {
	return "http://blobs.test/download/" + key, nil
}

func (m *memoryStore) ObjectURL(key string) string { return "http://blobs.test/" + key }

func (m *memoryStore) Size(_ context.Context, key string) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	data, ok := m.blobs[key]
	if !ok {
		return 0, fmt.Errorf("no object %q", key)
	}
	return int64(len(data)), nil
}

func (m *memoryStore) Read(_ context.Context, key string, limit int64) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	data, ok := m.blobs[key]
	if !ok || int64(len(data)) > limit {
		return nil, fmt.Errorf("cannot read %q", key)
	}
	return data, nil
}

func (m *memoryStore) PutObject(_ context.Context, key, _ string, data []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.blobs == nil {
		m.blobs = map[string][]byte{}
	}
	m.blobs[key] = data
	return nil
}

func (m *memoryStore) Delete(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.blobs, key)
	return nil
}

func (m *memoryStore) put(key string, data []byte) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.blobs[key] = data
}

var (
	testPNG = []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x06\x00\x00\x00\x1f\x15\xc4\x89")
	testPDF = []byte("%PDF-1.4\n%%EOF\n")
)

// Attachments, over the handlers, against a fake OpenAI-compatible endpoint:
// what a model that takes images is sent, and what is refused before anything
// is written. Needs the same scratch database as TestAgentEndToEnd.
func TestAttachments(t *testing.T) {
	pool := agentTestPool(t)
	ctx := context.Background()
	q := sqlc.New(pool)

	// The endpoint records every request body it is sent.
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
	newUser := func(name string) sqlc.User {
		var u sqlc.User
		if err := pool.QueryRow(ctx, `INSERT INTO users (email, name) VALUES ($1, $2) RETURNING id, email, name, role`,
			name+tag+"@test.invalid", name+tag).Scan(&u.ID, &u.Email, &u.Name, &u.Role); err != nil {
			t.Fatal(err)
		}
		return u
	}
	alice, mallory := newUser("alice"), newUser("mallory")
	conv, err := q.CreateConversation(ctx, sqlc.CreateConversationParams{UserID: &alice.ID, Title: "files"})
	if err != nil {
		t.Fatal(err)
	}
	malloryConv, err := q.CreateConversation(ctx, sqlc.CreateConversationParams{UserID: &mallory.ID, Title: "hers"})
	if err != nil {
		t.Fatal(err)
	}

	// A model whose settings say it takes images and nothing else.
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
	if _, err := q.SaveModelOverride(ctx, sqlc.SaveModelOverrideParams{ModelID: modelID, InputModalities: []string{"image"}}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM channels WHERE id = $1", channel.ID)
		_, _ = pool.Exec(context.Background(), "DELETE FROM model_overrides WHERE model_id = $1", modelID)
		_, _ = pool.Exec(context.Background(), "DELETE FROM users WHERE id = ANY($1)", []uuid.UUID{alice.ID, mallory.ID})
	})

	blobs := &memoryStore{blobs: map[string][]byte{}}
	runner := agent.NewRunner(pool, agent.NewHub(), NewModelResolver(pool), nil,
		agent.Options{Blobs: blobs, Modalities: NewModalities(pool), PartialInterval: 5 * time.Millisecond})
	t.Cleanup(func() { _ = runner.Shutdown(context.Background()) })
	server := &Server{pool: pool, q: q, runner: runner, store: blobs}

	call := func(user sqlc.User, handler func(http.ResponseWriter, *http.Request), body string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
		handler(rec, req.WithContext(auth.WithUser(req.Context(), user)))
		return rec
	}
	upload := func(user sqlc.User, name, contentType string, data []byte) uuid.UUID {
		t.Helper()
		rec := call(user, server.CreateUpload, fmt.Sprintf(`{"filename":%q,"contentType":%q,"size":%d}`, name, contentType, len(data)))
		if rec.Code != http.StatusCreated {
			t.Fatalf("upload %s: %d %s", name, rec.Code, rec.Body)
		}
		var up Upload
		if err := json.Unmarshal(rec.Body.Bytes(), &up); err != nil {
			t.Fatal(err)
		}
		var key string
		if err := pool.QueryRow(ctx, `SELECT storage_key FROM attachments WHERE id = $1`, up.Id).Scan(&key); err != nil {
			t.Fatal(err)
		}
		blobs.put(key, data)
		return up.Id
	}
	sendTo := func(user sqlc.User, in sqlc.Conversation, ids ...uuid.UUID) *httptest.ResponseRecorder {
		list, _ := json.Marshal(ids)
		return call(user, func(w http.ResponseWriter, r *http.Request) { server.CreateMessage(w, r, in.ID) },
			fmt.Sprintf(`{"content":"what is this?","model":%q,"attachmentIds":%s}`, modelID, list))
	}
	send := func(user sqlc.User, ids ...uuid.UUID) *httptest.ResponseRecorder { return sendTo(user, conv, ids...) }
	assertRefused := func(rec *httptest.ResponseRecorder, code string) {
		t.Helper()
		if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), `"`+code+`"`) {
			t.Fatalf("got %d %s, want 400 %s", rec.Code, rec.Body, code)
		}
		var n int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM messages WHERE conversation_id = $1`, conv.ID).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 0 {
			t.Fatalf("a refused message left %d rows behind", n)
		}
	}

	t.Run("uploads are limited to what a model can be sent", func(t *testing.T) {
		for _, tc := range []struct {
			name, contentType string
			size              int
			status            int
			code              string
		}{
			{"a.svg", "image/svg+xml", 10, http.StatusUnsupportedMediaType, "unsupported_file_type"},
			{"a.bin", "", 10, http.StatusUnsupportedMediaType, "unsupported_file_type"},
			{"big.png", "image/png", 6 << 20, http.StatusRequestEntityTooLarge, "file_too_large"},
			{"big.pdf", "application/pdf", 11 << 20, http.StatusRequestEntityTooLarge, "file_too_large"},
			{"ok.pdf", "application/pdf; charset=binary", 10 << 20, http.StatusCreated, ""},
		} {
			rec := call(alice, server.CreateUpload, fmt.Sprintf(`{"filename":%q,"contentType":%q,"size":%d}`, tc.name, tc.contentType, tc.size))
			if rec.Code != tc.status || !strings.Contains(rec.Body.String(), tc.code) {
				t.Errorf("%s: got %d %s, want %d %s", tc.name, rec.Code, rec.Body, tc.status, tc.code)
			}
		}
	})

	t.Run("a model that takes no PDFs refuses one", func(t *testing.T) {
		assertRefused(send(alice, upload(alice, "r.pdf", "application/pdf", testPDF)), "attachment_unsupported")
	})

	t.Run("nobody can attach another person's upload", func(t *testing.T) {
		rec := sendTo(mallory, malloryConv, upload(alice, "a.png", "image/png", testPNG))
		if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "attachment_unavailable") {
			t.Fatalf("got %d %s", rec.Code, rec.Body)
		}
	})

	t.Run("a file that never arrived is refused", func(t *testing.T) {
		rec := call(alice, server.CreateUpload, `{"filename":"a.png","contentType":"image/png","size":10}`)
		var up Upload
		_ = json.Unmarshal(rec.Body.Bytes(), &up)
		assertRefused(send(alice, up.Id), "attachment_unavailable")
	})

	t.Run("a file bigger than announced is refused", func(t *testing.T) {
		rec := call(alice, server.CreateUpload, `{"filename":"a.png","contentType":"image/png","size":10}`)
		var up Upload
		_ = json.Unmarshal(rec.Body.Bytes(), &up)
		var key string
		_ = pool.QueryRow(ctx, `SELECT storage_key FROM attachments WHERE id = $1`, up.Id).Scan(&key)
		blobs.put(key, make([]byte, 5<<20+1))
		assertRefused(send(alice, up.Id), "attachments_too_large")
	})

	t.Run("a message carries at most ten files", func(t *testing.T) {
		var ids []uuid.UUID
		for range agent.MaxMessageAttachments + 1 {
			ids = append(ids, upload(alice, "a.png", "image/png", testPNG))
		}
		assertRefused(send(alice, ids...), "too_many_attachments")
	})

	t.Run("an image reaches the model as inline data", func(t *testing.T) {
		id := upload(alice, "square.png", "image/png", testPNG)
		rec := send(alice, id)
		if rec.Code != http.StatusAccepted {
			t.Fatalf("got %d %s", rec.Code, rec.Body)
		}
		var created CreateMessageResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
			t.Fatal(err)
		}
		if created.Message.Attachments == nil || len(*created.Message.Attachments) != 1 ||
			(*created.Message.Attachments)[0].Filename != "square.png" || !strings.HasPrefix((*created.Message.Attachments)[0].Url, "http://blobs.test/get/") {
			t.Fatalf("the response lacks the signed attachment: %+v", created.Message.Attachments)
		}
		for _, part := range created.Message.Parts {
			if raw, _ := json.Marshal(part); strings.Contains(string(raw), "attachment") {
				t.Errorf("a storage reference leaked into the message parts: %s", raw)
			}
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
		defer bodiesMu.Unlock()
		if len(bodies) != 1 {
			t.Fatalf("the model was asked %d times", len(bodies))
		}
		want := "data:image/png;base64," + base64.StdEncoding.EncodeToString(testPNG)
		if !strings.Contains(bodies[0], want) {
			t.Errorf("the request lacks the image as a data URL: %s", bodies[0])
		}
		if strings.Contains(bodies[0], "blobs.test") {
			t.Errorf("a storage URL went to the provider: %s", bodies[0])
		}

		// It is the claimant's now: the upload cannot be sent a second time.
		rec = send(alice, id)
		if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "attachment_unavailable") {
			t.Errorf("re-sending a claimed upload: %d %s", rec.Code, rec.Body)
		}

		// The transcript carries it back with a fresh signed URL.
		page := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		server.ListMessages(page, req.WithContext(auth.WithUser(req.Context(), alice)), conv.ID, ListMessagesParams{})
		var msgs MessagePage
		_ = json.Unmarshal(page.Body.Bytes(), &msgs)
		if len(msgs.Items) == 0 || msgs.Items[0].Attachments == nil || (*msgs.Items[0].Attachments)[0].ContentType != "image/png" {
			t.Errorf("listed messages: %s", page.Body)
		}
	})
}
