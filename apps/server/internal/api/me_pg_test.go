package api

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/qunqin/openchat/apps/server/internal/agent"
	"github.com/qunqin/openchat/apps/server/internal/auth"
	"github.com/qunqin/openchat/apps/server/internal/config"
	"github.com/qunqin/openchat/apps/server/internal/db/sqlc"
	"github.com/qunqin/openchat/apps/server/internal/storage"
)

type mePG struct {
	t     *testing.T
	pool  *pgxpool.Pool
	q     *sqlc.Queries
	web   *httptest.Server
	local *storage.Local
}

func newMePG(t *testing.T) *mePG {
	t.Helper()
	pool := agentTestPool(t)
	local, err := storage.NewLocal(t.TempDir(), []byte("test-secret"), "")
	if err != nil {
		t.Fatal(err)
	}
	runner := agent.NewRunner(pool, agent.NewHub(), NewModelResolver(pool), nil, agent.Options{})
	t.Cleanup(func() { _ = runner.Shutdown(context.Background()) })
	cfg := &config.Config{CORSOrigins: []string{"http://localhost"}}
	q := sqlc.New(pool)
	web := httptest.NewServer(NewRouter(cfg, NewServer(cfg, pool, local, runner), auth.NewMiddleware(q)))
	t.Cleanup(web.Close)
	return &mePG{t: t, pool: pool, q: q, web: web, local: local}
}

// allowAvatars sets whether pictures may be uploaded, and puts it back after.
func (m *mePG) allowAvatars(on bool) {
	m.t.Helper()
	set := func(on bool) error {
		value, _ := json.Marshal(on)
		return m.q.SetSetting(context.Background(), sqlc.SetSettingParams{Key: settingAvatarUploads, Value: value})
	}
	if err := set(on); err != nil {
		m.t.Fatal(err)
	}
	m.t.Cleanup(func() { _ = set(false) })
}

// signedIn makes a user and a session cookie for it.
func (m *mePG) signedIn(role string) (sqlc.User, *http.Cookie) {
	m.t.Helper()
	user := newUser(m.t, m.pool, role)
	token, hash, err := auth.NewToken()
	if err != nil {
		m.t.Fatal(err)
	}
	if _, err := m.q.CreateSession(context.Background(), sqlc.CreateSessionParams{UserID: user.ID, TokenHash: hash,
		ExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(time.Hour), Valid: true}}); err != nil {
		m.t.Fatal(err)
	}
	return user, &http.Cookie{Name: auth.CookieName, Value: token}
}

func (m *mePG) do(cookie *http.Cookie, method, target, contentType string, body []byte) (*http.Response, []byte) {
	m.t.Helper()
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, m.web.URL+target, reader)
	if err != nil {
		m.t.Fatal(err)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if cookie != nil {
		req.AddCookie(cookie)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		m.t.Fatal(err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	return res, raw
}

// conversation makes one of the user's with a question, and an answer when
// asked for, written the way the agent layer writes them.
func (m *mePG) conversation(user sqlc.User, title, question string, temporary bool) sqlc.Conversation {
	m.t.Helper()
	ctx := context.Background()
	conv, err := m.q.CreateConversation(ctx, sqlc.CreateConversationParams{UserID: &user.ID, Title: title, Temporary: temporary})
	if err != nil {
		m.t.Fatal(err)
	}
	if _, err := appendInput(ctx, m.q, conv.ID, nil, question, nil, nil); err != nil {
		m.t.Fatal(err)
	}
	return conv
}

// ---- export and delete-all ---------------------------------------------------

func TestExportAndDeleteAllAreTheCallersOwn(t *testing.T) {
	m := newMePG(t)
	alice, aliceCookie := m.signedIn("user")
	bob, bobCookie := m.signedIn("user")

	// Alice: more conversations than one read of the export takes, so the
	// batching is crossed; a temporary one and a deleted one, which are not
	// history. Bob has one with a text of his own.
	const kept = exportBatch*2 + 7
	want := map[uuid.UUID]string{}
	for i := 0; i < kept; i++ {
		c := m.conversation(alice, fmt.Sprintf("alice %d", i), fmt.Sprintf("alice question %d", i), false)
		want[c.ID] = fmt.Sprintf("alice question %d", i)
	}
	temp := m.conversation(alice, "alice temporary", "alice temporary question", true)
	gone := m.conversation(alice, "alice deleted", "alice deleted question", false)
	if _, err := m.q.SoftDeleteConversation(context.Background(), sqlc.SoftDeleteConversationParams{ID: gone.ID, UserID: &alice.ID}); err != nil {
		t.Fatal(err)
	}
	bobs := m.conversation(bob, "bob", "bob's secret question", false)

	// Signed in, or not at all.
	if res, _ := m.do(nil, http.MethodGet, "/api/v1/me/export", "", nil); res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("export without a session: %d", res.StatusCode)
	}
	if res, _ := m.do(nil, http.MethodDelete, "/api/v1/me/conversations", "", nil); res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("delete-all without a session: %d", res.StatusCode)
	}

	export := func(cookie *http.Cookie) (ConversationExport, *http.Response, string) {
		t.Helper()
		res, raw := m.do(cookie, http.MethodGet, "/api/v1/me/export", "", nil)
		if res.StatusCode != http.StatusOK {
			t.Fatalf("export: %d %s", res.StatusCode, raw)
		}
		var out ConversationExport
		if err := json.Unmarshal(raw, &out); err != nil {
			t.Fatalf("the export is not JSON: %v\n%.300s", err, raw)
		}
		return out, res, string(raw)
	}

	out, res, raw := export(aliceCookie)
	if !strings.HasPrefix(res.Header.Get("Content-Disposition"), "attachment;") || res.Header.Get("Content-Type") != "application/json" {
		t.Errorf("headers: %v", res.Header)
	}
	if out.Version != 1 || len(out.Conversations) != kept {
		t.Fatalf("version %d, %d conversations; want 1 and %d", out.Version, len(out.Conversations), kept)
	}
	seen := map[uuid.UUID]bool{}
	for i, c := range out.Conversations {
		if seen[c.Id] {
			t.Fatalf("conversation %s twice", c.Id)
		}
		seen[c.Id] = true
		question, ours := want[c.Id]
		if !ours {
			t.Fatalf("conversation %q does not belong in alice's export", c.Title)
		}
		if len(c.Messages) != 1 || c.Messages[0].Role != "user" || c.Messages[0].Content != question {
			t.Fatalf("messages of %q: %+v", c.Title, c.Messages)
		}
		if i > 0 && c.UpdatedAt.After(out.Conversations[i-1].UpdatedAt) {
			t.Fatalf("not most recent first at %d", i)
		}
	}
	for _, other := range []string{"bob's secret", "alice temporary", "alice deleted"} {
		if strings.Contains(raw, other) {
			t.Errorf("alice's export holds %q", other)
		}
	}

	// Bob gets his own, and nothing of Alice's.
	bobOut, _, bobRaw := export(bobCookie)
	if len(bobOut.Conversations) != 1 || bobOut.Conversations[0].Id != bobs.ID || strings.Contains(bobRaw, "alice") {
		t.Fatalf("bob's export: %s", bobRaw)
	}

	// Delete all: Alice's, the temporary one included, and only hers.
	res, body := m.do(aliceCookie, http.MethodDelete, "/api/v1/me/conversations", "", nil)
	var deleted DeletedConversations
	if err := json.Unmarshal(body, &deleted); err != nil || res.StatusCode != http.StatusOK || deleted.Deleted != kept+1 {
		t.Fatalf("delete-all: %d %s (want %d)", res.StatusCode, body, kept+1)
	}
	for id := range want {
		if _, err := m.q.GetConversation(context.Background(), sqlc.GetConversationParams{ID: id, UserID: &alice.ID}); err == nil {
			t.Fatalf("conversation %s survived", id)
		}
	}
	if _, err := m.q.GetConversation(context.Background(), sqlc.GetConversationParams{ID: temp.ID, UserID: &alice.ID}); err == nil {
		t.Fatal("the temporary conversation survived")
	}
	if _, err := m.q.GetConversation(context.Background(), sqlc.GetConversationParams{ID: bobs.ID, UserID: &bob.ID}); err != nil {
		t.Fatalf("bob's conversation went with alice's: %v", err)
	}
	// Soft: the rows are still there.
	var rows int
	_ = m.pool.QueryRow(context.Background(), "SELECT count(*) FROM conversations WHERE user_id = $1 AND deleted_at IS NOT NULL", alice.ID).Scan(&rows)
	if rows != kept+2 {
		t.Errorf("%d rows marked deleted, want %d", rows, kept+2)
	}

	// Nothing left to export (an empty list, not null) and nothing left to delete.
	after, _, afterRaw := export(aliceCookie)
	if len(after.Conversations) != 0 || !strings.Contains(afterRaw, `"conversations":[]`) {
		t.Fatalf("export after deleting: %s", afterRaw)
	}
	_, body = m.do(aliceCookie, http.MethodDelete, "/api/v1/me/conversations", "", nil)
	if err := json.Unmarshal(body, &deleted); err != nil || deleted.Deleted != 0 {
		t.Fatalf("a second delete-all: %s", body)
	}
	if again, _, _ := export(bobCookie); len(again.Conversations) != 1 {
		t.Fatal("bob lost his conversation")
	}
}

// The export is the branch shown, as text: answers, not tool traces or the
// versions set aside.
func TestExportFollowsTheBranchShown(t *testing.T) {
	b := newBranching(t)
	q1, _ := b.send("q1")
	b.send("q2")
	b.edit(q1.Message.Id, "q1b")

	rec := httptest.NewRecorder()
	b.server.ExportMyConversations(rec, b.request(http.MethodGet, ""))
	var out ConversationExport
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("%d %v\n%s", rec.Code, err, rec.Body)
	}
	var mine *ExportedConversation
	for i := range out.Conversations {
		if out.Conversations[i].Id == b.conv.ID {
			mine = &out.Conversations[i]
		}
	}
	if mine == nil {
		t.Fatal("the conversation is not in its owner's export")
	}
	var got []string
	for _, m := range mine.Messages {
		got = append(got, string(m.Role)+":"+m.Content)
	}
	same(t, "exported branch", got, []string{"user:q1b", "assistant:re: q1b"})
	if mine.Messages[1].Model == nil || *mine.Messages[1].Model == "" {
		t.Errorf("the answer carries no model: %+v", mine.Messages[1])
	}
}

// ---- avatars ---------------------------------------------------------------

// pictureOf is a w x h image of three vertical bands: red, blue, red.
func pictureOf(w, h int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			c := color.NRGBA{R: 255, A: 255}
			if x >= w/3 && x < 2*w/3 {
				c = color.NRGBA{B: 255, A: 255}
			}
			img.SetNRGBA(x, y, c)
		}
	}
	return img
}

func encodePNG(t *testing.T, img image.Image) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestAvatarUploadAndRemoval(t *testing.T) {
	m := newMePG(t)
	alice, aliceCookie := m.signedIn("user")
	bob, bobCookie := m.signedIn("user")
	admin, adminCookie := m.signedIn("admin")

	put := func(cookie *http.Cookie, contentType string, body []byte) (*http.Response, []byte) {
		return m.do(cookie, http.MethodPut, "/api/v1/me/avatar", contentType, body)
	}
	refused := func(res *http.Response, raw []byte, status int, code string) {
		t.Helper()
		if res.StatusCode != status || !strings.Contains(string(raw), code) {
			t.Fatalf("got %d %s, want %d %s", res.StatusCode, raw, status, code)
		}
	}
	sessionOf := func(cookie *http.Cookie) SessionUser {
		t.Helper()
		_, raw := m.do(cookie, http.MethodGet, "/api/v1/auth/session", "", nil)
		var u SessionUser
		if err := json.Unmarshal(raw, &u); err != nil {
			t.Fatal(err)
		}
		return u
	}

	// Off, as it is by default: refused before anything is read.
	m.allowAvatars(false)
	res, raw := put(aliceCookie, "image/png", encodePNG(t, pictureOf(8, 8)))
	refused(res, raw, http.StatusForbidden, "avatar_uploads_off")
	m.allowAvatars(true)

	// Validation: who may, what type, how big, what it really is.
	res, raw = put(nil, "image/png", encodePNG(t, pictureOf(8, 8)))
	refused(res, raw, http.StatusUnauthorized, "")
	res, raw = put(aliceCookie, "text/plain", []byte("hello"))
	refused(res, raw, http.StatusUnsupportedMediaType, "unsupported_file_type")
	res, raw = put(aliceCookie, "image/gif", []byte("GIF89a"))
	refused(res, raw, http.StatusUnsupportedMediaType, "unsupported_file_type")
	res, raw = put(aliceCookie, "image/png", nil)
	refused(res, raw, http.StatusUnsupportedMediaType, "unsupported_file_type")
	var asJPEG bytes.Buffer
	_ = jpeg.Encode(&asJPEG, pictureOf(40, 40), nil)
	res, raw = put(aliceCookie, "image/png", asJPEG.Bytes()) // says PNG, is a JPEG
	refused(res, raw, http.StatusUnsupportedMediaType, "unsupported_file_type")
	res, raw = put(aliceCookie, "image/png", append(encodePNG(t, pictureOf(8, 8))[:40], bytes.Repeat([]byte{7}, 64)...))
	refused(res, raw, http.StatusBadRequest, "invalid_image") // a PNG header with nothing behind it
	res, raw = put(aliceCookie, "image/png", encodePNG(t, pictureOf(4097, 1)))
	refused(res, raw, http.StatusBadRequest, "invalid_image") // too many pixels to decode
	huge := bytes.Repeat([]byte{0}, avatarMaxBytes+1)
	copy(huge, encodePNG(t, pictureOf(8, 8)))
	res, raw = put(aliceCookie, "image/png", huge)
	refused(res, raw, http.StatusRequestEntityTooLarge, "file_too_large")
	if u := sessionOf(aliceCookie); u.AvatarUrl != nil {
		t.Fatalf("a refused upload left a picture: %v", *u.AvatarUrl)
	}

	// A good one, landscape: cut to the square at its centre.
	res, raw = put(aliceCookie, "image/png", encodePNG(t, pictureOf(300, 200)))
	var user SessionUser
	if err := json.Unmarshal(raw, &user); err != nil || res.StatusCode != http.StatusOK || user.AvatarUrl == nil || user.Id != alice.ID {
		t.Fatalf("upload: %d %s", res.StatusCode, raw)
	}
	first := *user.AvatarUrl
	if again := sessionOf(aliceCookie); again.AvatarUrl == nil || *again.AvatarUrl != first {
		t.Fatalf("the session issues another URL for the same picture:\n%v\n%s", again.AvatarUrl, first)
	}
	if other := sessionOf(bobCookie); other.AvatarUrl != nil {
		t.Fatal("bob has alice's picture")
	}

	read := func(avatarURL string) (*http.Response, []byte) {
		t.Helper()
		return m.do(nil, http.MethodGet, avatarURL, "", nil) // no session: the signature is the authorization
	}
	res, raw = read(first)
	if res.StatusCode != http.StatusOK || res.Header.Get("Content-Type") != "image/png" || res.Header.Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("reading the picture: %d %v", res.StatusCode, res.Header)
	}
	stored, err := png.Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if b := stored.Bounds(); b.Dx() != 200 || b.Dy() != 200 {
		t.Fatalf("stored %v, want 200x200 (the centre square of 300x200)", b)
	}
	// 300 wide in thirds, 200 high: the square takes x in [50, 250), and the
	// blue band is [100, 200), so it is the middle half of the stored picture.
	centre := color.NRGBAModel.Convert(stored.At(100, 100)).(color.NRGBA)
	corner := color.NRGBAModel.Convert(stored.At(5, 100)).(color.NRGBA)
	if centre.B < 250 || centre.R > 5 || corner.R < 250 || corner.B > 5 {
		t.Errorf("the crop is not centred: centre %v, left edge %v", centre, corner)
	}

	// A large photo is scaled down to 256.
	res, raw = put(aliceCookie, "image/png", encodePNG(t, pictureOf(900, 700)))
	if res.StatusCode != http.StatusOK {
		t.Fatalf("a large picture: %d %s", res.StatusCode, raw)
	}
	_ = json.Unmarshal(raw, &user)
	second := *user.AvatarUrl
	if second == first {
		t.Fatal("a replaced picture keeps its address")
	}
	_, raw = read(second)
	if scaled, err := png.Decode(bytes.NewReader(raw)); err != nil || scaled.Bounds().Dx() != 256 {
		t.Fatalf("scaled picture: %v %v", scaled, err)
	}
	// The first one is gone from the store and is no longer served.
	row, _ := m.q.GetUser(context.Background(), alice.ID)
	if row.AvatarKey == nil || !m.local.Has(*row.AvatarKey) {
		t.Fatalf("the new picture is not stored: %v", row.AvatarKey)
	}
	if res, _ = read(first); res.StatusCode != http.StatusNotFound {
		t.Errorf("the replaced picture is still served: %d", res.StatusCode)
	}
	firstURL, _ := url.Parse(first)
	if oldKey := strings.TrimPrefix(firstURL.Path, storage.ContentPath); m.local.Has(oldKey) {
		t.Errorf("the replaced picture's file %s is still stored", oldKey)
	}

	// The signed URL reads, and only reads: a tampered one or a write is refused.
	parsed, _ := url.Parse(second)
	if res, _ = m.do(nil, http.MethodPut, second, "image/png", encodePNG(t, pictureOf(8, 8))); res.StatusCode != http.StatusNotFound && res.StatusCode != http.StatusForbidden {
		t.Errorf("a PUT to a picture's URL: %d", res.StatusCode)
	}
	q := parsed.Query()
	q.Set("signature", "AAAA")
	if res, _ = read(parsed.Path + "?" + q.Encode()); res.StatusCode != http.StatusForbidden {
		t.Errorf("a bad signature: %d", res.StatusCode)
	}

	// Where the picture is shown: the admin's list of accounts and the usage
	// ledger's user column.
	ctx := context.Background()
	if err := m.q.InsertUsageLog(ctx, sqlc.InsertUsageLogParams{
		CreatedAt: pgtype.Timestamptz{Time: time.Now(), Valid: true}, Kind: "chat", Status: "ok",
		UserID: &alice.ID, ChannelName: "c", ModelID: "avatar-model-" + alice.ID.String(), ServedModel: "",
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = m.pool.Exec(context.Background(), "DELETE FROM usage_logs WHERE user_id = $1", alice.ID)
	})
	_, raw = m.do(adminCookie, http.MethodGet, "/api/v1/admin/users?search="+url.QueryEscape(alice.Name), "", nil)
	var users UserPage
	if err := json.Unmarshal(raw, &users); err != nil || len(users.Items) != 1 || users.Items[0].AvatarUrl == nil {
		t.Fatalf("admin user list: %s", raw)
	}
	_, raw = m.do(adminCookie, http.MethodGet, "/api/v1/admin/usage-logs?userId="+alice.ID.String(), "", nil)
	var logs UsageLogPage
	if err := json.Unmarshal(raw, &logs); err != nil || len(logs.Items) != 1 || logs.Items[0].UserAvatarUrl == nil {
		t.Fatalf("usage logs: %s", raw)
	}
	_ = admin

	// Removing it: back to the initial, the file gone and no longer served.
	if res, raw = m.do(nil, http.MethodDelete, "/api/v1/me/avatar", "", nil); res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("remove without a session: %d %s", res.StatusCode, raw)
	}
	res, raw = m.do(aliceCookie, http.MethodDelete, "/api/v1/me/avatar", "", nil)
	user = SessionUser{}
	if err := json.Unmarshal(raw, &user); err != nil || res.StatusCode != http.StatusOK || user.AvatarUrl != nil {
		t.Fatalf("remove: %d %s", res.StatusCode, raw)
	}
	if m.local.Has(*row.AvatarKey) {
		t.Error("the removed picture is still stored")
	}
	if res, _ = read(second); res.StatusCode != http.StatusNotFound {
		t.Errorf("the removed picture is still served: %d", res.StatusCode)
	}
	// And again: nothing to remove is not an error.
	if res, _ = m.do(aliceCookie, http.MethodDelete, "/api/v1/me/avatar", "", nil); res.StatusCode != http.StatusOK {
		t.Errorf("removing no picture: %d", res.StatusCode)
	}

	// Pictures are the caller's own: bob's upload never touches alice's row.
	if res, raw = put(bobCookie, "image/png", encodePNG(t, pictureOf(64, 64))); res.StatusCode != http.StatusOK {
		t.Fatalf("bob's upload: %d %s", res.StatusCode, raw)
	}
	if got := sessionOf(aliceCookie); got.AvatarUrl != nil {
		t.Error("bob's upload became alice's picture")
	}
	if n := countRows(t, m.pool, "SELECT count(*) FROM users WHERE avatar_key IS NOT NULL AND id = ANY($1)", []uuid.UUID{alice.ID, bob.ID}); n != 1 {
		t.Errorf("%d of the two have a picture, want 1", n)
	}
}

// A minimal lossless WebP (1x1), for the one format Go cannot write.
const tinyWebP = "UklGRhoAAABXRUJQVlA4TA0AAAAvAAAAEAcQERGIiP4HAA=="

func TestAvatarAcceptsWebP(t *testing.T) {
	m := newMePG(t)
	m.allowAvatars(true)
	_, cookie := m.signedIn("user")
	raw, err := base64.StdEncoding.DecodeString(tinyWebP)
	if err != nil {
		t.Fatal(err)
	}
	res, body := m.do(cookie, http.MethodPut, "/api/v1/me/avatar", "image/webp", raw)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("webp: %d %s", res.StatusCode, body)
	}
}

func countRows(t *testing.T, pool *pgxpool.Pool, query string, args ...any) (n int) {
	t.Helper()
	if err := pool.QueryRow(context.Background(), query, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}
