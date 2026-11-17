package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/google/uuid"

	"github.com/qunqin24/openchat/apps/server/internal/auth"
	"github.com/qunqin24/openchat/apps/server/internal/db/sqlc"
)

// A link's card reads only pages this conversation's tool results list, and
// only for the conversation's owner; its picture comes through the same rule.
func TestLinkPreviewOnlyForSources(t *testing.T) {
	pool := agentTestPool(t)
	ctx := context.Background()
	q := sqlc.New(pool)
	server := &Server{pool: pool, q: q}

	picture := pngBytes(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/article", "/invented":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(`<head><meta property="og:title" content="What is the Blackwall"><meta property="og:image" content="/cover.png"></head>`))
		case "/cover.png":
			_, _ = w.Write(picture)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	// The guarded client refuses loopback, which is where the test server is.
	oldPreviews, oldClient := previews, images.client
	previews = &previewCache{entries: map[string]pagePreview{}, client: srv.Client()}
	images.client = srv.Client()
	t.Cleanup(func() { previews, images.client = oldPreviews, oldClient })

	tag := uuid.NewString()
	var owner, other sqlc.User
	for _, u := range []*sqlc.User{&owner, &other} {
		name := uuid.NewString()
		if err := pool.QueryRow(ctx, `INSERT INTO users (email, name, role) VALUES ($1, $2, 'user') RETURNING id, email, name, role`,
			name+tag+"@test.invalid", name).Scan(&u.ID, &u.Email, &u.Name, &u.Role); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM users WHERE id = ANY($1)", []uuid.UUID{owner.ID, other.ID})
	})
	var conv uuid.UUID
	if err := pool.QueryRow(ctx, `INSERT INTO conversations (user_id, title) VALUES ($1, 't') RETURNING id`, owner.ID).Scan(&conv); err != nil {
		t.Fatal(err)
	}
	article := srv.URL + "/article"
	if _, err := pool.Exec(ctx, `INSERT INTO messages (conversation_id, role, content, status, parts)
		VALUES ($1, 'tool', '', 'complete', jsonb_build_array(jsonb_build_object('type', 'text', 'text', $2::text)))`,
		conv, "Results: "+article); err != nil {
		t.Fatal(err)
	}

	preview := func(as sqlc.User, address string) (int, LinkPreview) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		server.GetLinkPreview(rec, req.WithContext(auth.WithUser(req.Context(), as)), conv, GetLinkPreviewParams{Url: address})
		var out LinkPreview
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		return rec.Code, out
	}
	code, got := preview(owner, article+"#section")
	if code != http.StatusOK || got.Title == nil || *got.Title != "What is the Blackwall" || !got.Image || got.Url != article+"#section" {
		t.Fatalf("source: %d %+v", code, got)
	}
	if code, _ := preview(owner, srv.URL+"/invented"); code != http.StatusNotFound {
		t.Errorf("an address no tool result lists: %d", code)
	}
	if code, _ := preview(other, article); code != http.StatusNotFound {
		t.Errorf("someone else's conversation: %d", code)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/?url="+url.QueryEscape(article), nil)
	server.GetLinkPreviewImage(rec, req.WithContext(auth.WithUser(req.Context(), owner)), conv, GetLinkPreviewImageParams{Url: article})
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "image/png" {
		t.Errorf("picture: %d %s", rec.Code, rec.Header().Get("Content-Type"))
	}
}
