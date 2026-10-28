package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
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
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/qunqin24/openchat/apps/server/internal/agent"
	"github.com/qunqin24/openchat/apps/server/internal/auth"
	"github.com/qunqin24/openchat/apps/server/internal/config"
	"github.com/qunqin24/openchat/apps/server/internal/db/sqlc"
	"github.com/qunqin24/openchat/apps/server/internal/storage"
)

// The picture that passed moderation is the picture the model reads.
//
// Content moderation reads an upload's image between the PUT that wrote it and
// the run that sends it. These tests write another image into that gap - from
// inside the moderation read itself, the latest point at which the first is
// "checked" - and look at what the model is sent. The local driver refuses the
// second write; a bucket (a map here: it takes any PUT) takes it, and the
// digest the check left on the message makes the run drop the file instead.

// swapBackend is a blob store, whichever driver, that can do something right
// after the first read of an object.
type swapBackend interface {
	blobStore
	// afterFirstRead is called once the first Read has returned.
	onFirstRead(func(key string))
}

type swapLocal struct {
	*storage.Local
	once sync.Once
	hook func(key string)
}

func (s *swapLocal) onFirstRead(f func(string)) { s.hook = f }

func (s *swapLocal) Read(ctx context.Context, key string, limit int64) ([]byte, error) {
	data, err := s.Local.Read(ctx, key, limit)
	if err == nil && s.hook != nil {
		s.once.Do(func() { s.hook(key) })
	}
	return data, err
}

type swapBucket struct {
	*memoryStore
	once sync.Once
	hook func(key string)
}

func (s *swapBucket) onFirstRead(f func(string)) { s.hook = f }

func (s *swapBucket) Read(ctx context.Context, key string, limit int64) ([]byte, error) {
	data, err := s.memoryStore.Read(ctx, key, limit)
	if err == nil && s.hook != nil {
		s.once.Do(func() { s.hook(key) })
	}
	return data, err
}

type swapHarness struct {
	t       *testing.T
	pool    *pgxpool.Pool
	q       *sqlc.Queries
	server  *Server
	user    sqlc.User
	conv    sqlc.Conversation
	model   string
	web     *httptest.Server
	backend swapBackend
	mod     *fakeModeration

	mu     sync.Mutex
	bodies []string
}

// newSwapHarness is an account with a model that takes images, moderation on
// against a fake service that blocks images containing "BADIMG", and the
// storage `make` returns.
func newSwapHarness(t *testing.T, mk func(t *testing.T) (swapBackend, *storage.Local)) *swapHarness {
	t.Helper()
	pool := agentTestPool(t)
	ctx := context.Background()
	q := sqlc.New(pool)
	h := &swapHarness{t: t, pool: pool, q: q, mod: newFakeModeration(t)}

	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		h.mu.Lock()
		h.bodies = append(h.bodies, string(raw))
		h.mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, `data: {"id":"r","model":"m","choices":[{"index":0,"delta":{"role":"assistant","content":"I see it."},"finish_reason":null}]}`+"\n\n")
		_, _ = fmt.Fprint(w, `data: {"id":"r","model":"m","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`+"\n\n")
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(endpoint.Close)

	tag := uuid.NewString()
	if err := pool.QueryRow(ctx, `INSERT INTO users (email, name) VALUES ($1, $2) RETURNING id, email, name, role`,
		"swap"+tag+"@test.invalid", "swap"+tag).Scan(&h.user.ID, &h.user.Email, &h.user.Name, &h.user.Role); err != nil {
		t.Fatal(err)
	}
	var err error
	if h.conv, err = q.CreateConversation(ctx, sqlc.CreateConversationParams{UserID: &h.user.ID, Title: "swap"}); err != nil {
		t.Fatal(err)
	}
	h.model = "swap-vision-" + tag
	channel, err := q.CreateChannel(ctx, sqlc.CreateChannelParams{
		Name: "fake " + tag, Type: sqlc.ChannelTypeOpenaiCompatible, Enabled: true,
		BaseUrl: endpoint.URL + "/v1", ApiKey: "sk-test",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := q.CreateSelectedChannelModels(ctx, sqlc.CreateSelectedChannelModelsParams{
		ChannelID: channel.ID, ModelIds: []string{h.model}, DisplayNames: []string{""}, Sources: []string{"manual"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := q.SaveModelOverride(ctx, sqlc.SaveModelOverrideParams{ModelID: h.model, InputModalities: []string{"image"}}); err != nil {
		t.Fatal(err)
	}
	clear := func() {
		bg := context.Background()
		_, _ = pool.Exec(bg, `DELETE FROM app_settings WHERE key = ANY($1)`, moderationKeys)
		_, _ = pool.Exec(bg, `DELETE FROM moderation_events WHERE user_id = $1`, h.user.ID)
	}
	clear()
	t.Cleanup(func() {
		clear()
		_, _ = pool.Exec(context.Background(), "DELETE FROM channels WHERE id = $1", channel.ID)
		_, _ = pool.Exec(context.Background(), "DELETE FROM model_overrides WHERE model_id = $1", h.model)
		_, _ = pool.Exec(context.Background(), "DELETE FROM users WHERE id = $1", h.user.ID)
	})
	for key, value := range map[string]any{
		settingModerationMode:          "openai",
		settingModerationOpenAIKey:     "sk-mod-secret-1234",
		settingModerationOpenAIBaseURL: h.mod.srv.URL + "/v1",
		settingModerationCategories:    []string{"sexual"},
	} {
		raw, _ := json.Marshal(value)
		if err := q.SetSetting(ctx, sqlc.SetSettingParams{Key: key, Value: raw}); err != nil {
			t.Fatal(err)
		}
	}

	backend, local := mk(t)
	h.backend = backend
	runner := agent.NewRunner(pool, agent.NewHub(), NewModelResolver(pool), nil,
		agent.Options{Blobs: backend, Modalities: NewModalities(pool), PartialInterval: 5 * time.Millisecond})
	t.Cleanup(func() { _ = runner.Shutdown(context.Background()) })
	cfg := &config.Config{CORSOrigins: []string{"http://localhost"}}
	if local != nil {
		h.server = NewServer(cfg, pool, local, runner)
	} else {
		h.server = NewServer(cfg, pool, backend, runner)
	}
	h.server.store = backend
	h.web = httptest.NewServer(NewRouter(cfg, h.server, auth.NewMiddleware(q)))
	t.Cleanup(h.web.Close)
	return h
}

func (h *swapHarness) call(handler func(http.ResponseWriter, *http.Request), body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	handler(rec, req.WithContext(auth.WithUser(req.Context(), h.user)))
	return rec
}

// issue asks for an upload of a PNG and returns its id and upload URL.
func (h *swapHarness) issue(size int) Upload {
	h.t.Helper()
	rec := h.call(h.server.CreateUpload, fmt.Sprintf(`{"filename":"a.png","contentType":"image/png","size":%d}`, size))
	if rec.Code != http.StatusCreated {
		h.t.Fatalf("issue: %d %s", rec.Code, rec.Body)
	}
	var up Upload
	if err := json.Unmarshal(rec.Body.Bytes(), &up); err != nil {
		h.t.Fatal(err)
	}
	return up
}

func (h *swapHarness) keyOf(id uuid.UUID) string {
	h.t.Helper()
	var key string
	if err := h.pool.QueryRow(context.Background(), `SELECT storage_key FROM attachments WHERE id = $1`, id).Scan(&key); err != nil {
		h.t.Fatal(err)
	}
	return key
}

// put writes through the signed local URL, as a browser does.
func (h *swapHarness) put(url string, data []byte) (int, string) {
	h.t.Helper()
	req, err := http.NewRequest(http.MethodPut, h.web.URL+url, bytes.NewReader(data))
	if err != nil {
		h.t.Fatal(err)
	}
	req.Header.Set("Content-Type", "image/png")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		h.t.Fatal(err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	return res.StatusCode, string(raw)
}

// send posts the message and waits for the run; it returns the model's request.
func (h *swapHarness) send(attachment uuid.UUID) (modelRequest string, status sqlc.RunStatus) {
	h.t.Helper()
	rec := h.call(func(w http.ResponseWriter, r *http.Request) { h.server.CreateMessage(w, r, h.conv.ID) },
		fmt.Sprintf(`{"content":"what is this?","model":%q,"attachmentIds":["%s"]}`, h.model, attachment))
	if rec.Code != http.StatusAccepted {
		h.t.Fatalf("send: %d %s", rec.Code, rec.Body)
	}
	var created CreateMessageResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		h.t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		run, err := h.q.GetRun(context.Background(), created.Run.Id)
		if err != nil {
			h.t.Fatal(err)
		}
		if run.Status != sqlc.RunStatusRunning {
			status = run.Status
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.bodies) > 0 {
		modelRequest = h.bodies[len(h.bodies)-1]
	}
	return modelRequest, status
}

func dataURL(data []byte) string {
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(data)
}

// the image that is swapped in: a PNG the moderation service would flag.
var swappedPNG = append(append([]byte{}, testPNG...), []byte("BADIMG")...)

// Local driver: the second PUT is refused, so the bytes read by the check are
// the bytes the model gets.
func TestUploadedImageCannotBeSwappedLocal(t *testing.T) {
	h := newSwapHarness(t, func(t *testing.T) (swapBackend, *storage.Local) {
		local, err := storage.NewLocal(t.TempDir(), []byte("test-secret"), "")
		if err != nil {
			t.Fatal(err)
		}
		return &swapLocal{Local: local}, local
	})
	up := h.issue(len(testPNG))
	if status, body := h.put(up.UploadUrl, testPNG); status != http.StatusOK {
		t.Fatalf("PUT: %d %s", status, body)
	}

	// The reviewer's reproduction: image B is written through the same signed
	// URL while the check is reading image A.
	var swapStatus int
	var swapBody string
	h.backend.onFirstRead(func(string) { swapStatus, swapBody = h.put(up.UploadUrl, swappedPNG) })

	request, status := h.send(up.Id)
	if swapStatus != http.StatusConflict || !strings.Contains(swapBody, "already_uploaded") {
		t.Errorf("the second PUT got %d %s, want 409 already_uploaded", swapStatus, swapBody)
	}
	if status != sqlc.RunStatusCompleted {
		t.Fatalf("run %s", status)
	}
	if !strings.Contains(request, dataURL(testPNG)) || strings.Contains(request, dataURL(swappedPNG)) {
		t.Errorf("the model was not sent the image that was checked: %s", request)
	}
	if got := h.mod.images[len(h.mod.images)-1]; len(got) != 1 || !bytes.Equal(got[0], testPNG) {
		t.Errorf("the service was sent %q", got)
	}

	// Writing twice is refused whether or not anything has read the file.
	again := h.issue(len(testPNG))
	if status, _ := h.put(again.UploadUrl, testPNG); status != http.StatusOK {
		t.Fatalf("PUT: %d", status)
	}
	if status, body := h.put(again.UploadUrl, testPNG); status != http.StatusConflict || !strings.Contains(body, "already_uploaded") {
		t.Errorf("a repeated PUT of the same bytes: %d %s", status, body)
	}
	data, err := h.backend.Read(context.Background(), h.keyOf(again.Id), 1<<20)
	if err != nil || !bytes.Equal(data, testPNG) {
		t.Errorf("stored %q, %v", data, err)
	}
}

// A bucket takes any PUT, claimed or not. The check records the digest of what
// it looked at, and the run sends a file with that digest or none.
func TestUploadedImageSwappedInABucketIsNotSent(t *testing.T) {
	h := newSwapHarness(t, func(t *testing.T) (swapBackend, *storage.Local) {
		return &swapBucket{memoryStore: &memoryStore{blobs: map[string][]byte{}}}, nil
	})
	bucket := h.backend.(*swapBucket)

	up := h.issue(len(testPNG))
	key := h.keyOf(up.Id)
	bucket.put(key, testPNG)
	// B replaces A right after the check has read it (and, were it later, after
	// the claim: the presigned URL is good for fifteen minutes).
	h.backend.onFirstRead(func(key string) { bucket.put(key, swappedPNG) })

	request, status := h.send(up.Id)
	if status != sqlc.RunStatusCompleted {
		t.Fatalf("run %s", status)
	}
	if strings.Contains(request, dataURL(swappedPNG)) || strings.Contains(request, dataURL(testPNG)) || strings.Contains(request, "BADIMG") {
		t.Errorf("an image went to the model although it is not the one that was checked: %s", request)
	}
	if !strings.Contains(request, "changed after it was checked") {
		t.Errorf("the model was not told the file was left out: %s", request)
	}

	// The digest is on the message, as the hash of what the service was sent.
	var parts []byte
	if err := h.pool.QueryRow(context.Background(),
		`SELECT parts FROM messages WHERE conversation_id = $1 AND role = 'user'`, h.conv.ID).Scan(&parts); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(testPNG)
	if !strings.Contains(string(parts), hex.EncodeToString(sum[:])) {
		t.Errorf("the message does not carry the digest of the checked image: %s", parts)
	}

	// Untouched, the same flow sends the file.
	fine := h.issue(len(testPNG))
	bucket.put(h.keyOf(fine.Id), testPNG)
	h.backend.onFirstRead(nil)
	request, status = h.send(fine.Id)
	if status != sqlc.RunStatusCompleted || !strings.Contains(request, dataURL(testPNG)) {
		t.Errorf("an unchanged image: %s, %s", status, request)
	}
}
