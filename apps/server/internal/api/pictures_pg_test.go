package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"

	"github.com/qunqin24/openchat/apps/server/internal/auth"
	"github.com/qunqin24/openchat/apps/server/internal/db/dbtest"
	"github.com/qunqin24/openchat/apps/server/internal/db/sqlc"
	"github.com/qunqin24/openchat/apps/server/internal/imagesearch"
	"github.com/qunqin24/openchat/apps/server/internal/storage"
)

// pictureSite is a site with a picture on it, which the image fetcher is
// pointed at: the guarded client dials public addresses only.
func pictureSite(t *testing.T) *httptest.Server {
	t.Helper()
	picture := pngBytes(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/photo.png" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(picture)
	}))
	t.Cleanup(srv.Close)
	saved := images.client
	images.client = srv.Client()
	t.Cleanup(func() { images.client = saved })
	return srv
}

// stubSearch answers every search from its table, counting the searches.
type stubSearch struct {
	found map[string]*imagesearch.Image
	fail  bool
	asked atomic.Int32
}

func (s *stubSearch) Search(_ context.Context, query string) (*imagesearch.Image, error) {
	s.asked.Add(1)
	if s.fail {
		return nil, imagesearch.ErrUnavailable
	}
	return s.found[query], nil
}

// A description finds its picture once: kept in the store, the same picture
// for the same words after, served only by its id.
func TestPicturesFoundByDescription(t *testing.T) {
	pool := dbtest.Pool(t, "api_images")
	ctx := context.Background()
	q := sqlc.New(pool)
	site := pictureSite(t)
	tag := uuid.NewString()
	words := "laptop keyboard " + tag
	search := &stubSearch{found: map[string]*imagesearch.Image{
		words: {Provider: "test", ID: tag, URL: site.URL + "/missing.png", Fallback: site.URL + "/photo.png",
			Width: 1024, Height: 768, Title: "MacBook keyboard", Creator: "alcomm", License: "CC BY 2.0",
			LicenseURL: "https://creativecommons.org/licenses/by/2.0/", Landing: "javascript:alert(1)", Source: "flickr"},
	}}
	local, err := storage.NewLocal(t.TempDir(), []byte("test-secret"), "")
	if err != nil {
		t.Fatal(err)
	}
	server := &Server{pool: pool, q: q, store: local, imageSearch: search}
	user := sqlc.User{ID: uuid.New()}

	find := func(words string) (*httptest.ResponseRecorder, FoundImage) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		server.SearchImage(rec, req.WithContext(auth.WithUser(req.Context(), user)), SearchImageParams{Q: words})
		var found FoundImage
		_ = json.Unmarshal(rec.Body.Bytes(), &found)
		return rec, found
	}

	rec, first := find("  Laptop   Keyboard " + tag)
	if rec.Code != http.StatusOK || first.Title != "MacBook keyboard" || first.License != "CC BY 2.0" || first.LandingUrl != nil {
		t.Fatalf("first: %d %s", rec.Code, rec.Body)
	}
	if rec, again := find(words); rec.Code != http.StatusOK || again.Id != first.Id || search.asked.Load() != 1 {
		t.Fatalf("again: %d %v, %d searches", rec.Code, again.Id, search.asked.Load())
	}

	// Served from the store, by the id a search found, and nothing else.
	rec = httptest.NewRecorder()
	server.GetPicture(rec, httptest.NewRequest(http.MethodGet, "/", nil), first.Id)
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "image/png" || rec.Body.Len() != len(pngBytes(t)) {
		t.Fatalf("serve: %d %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	rec = httptest.NewRecorder()
	server.GetPicture(rec, httptest.NewRequest(http.MethodGet, "/", nil), uuid.New())
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown id: %d", rec.Code)
	}

	// Nothing found is kept too, for a day; the service is not asked again.
	if rec, _ := find("nothing " + tag); rec.Code != http.StatusNotFound {
		t.Fatalf("nothing: %d", rec.Code)
	}
	if rec, _ := find("nothing " + tag); rec.Code != http.StatusNotFound || search.asked.Load() != 2 {
		t.Fatalf("nothing again: %d, %d searches", rec.Code, search.asked.Load())
	}

	// A service that cannot be asked keeps nothing: the words are tried again.
	search.fail = true
	for range 2 {
		if rec, _ := find("later " + tag); rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("unavailable: %d %s", rec.Code, rec.Body)
		}
	}
	if search.asked.Load() != 4 {
		t.Errorf("%d searches, want 4", search.asked.Load())
	}

	// With image search off, a description finds nothing at all.
	off := &Server{pool: pool, q: q, store: local}
	_, _ = pool.Exec(ctx, `DELETE FROM app_settings WHERE key = $1`, settingImageSearch)
	rec = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	off.SearchImage(rec, req.WithContext(auth.WithUser(req.Context(), user)), SearchImageParams{Q: words})
	if rec.Code != http.StatusNotFound {
		t.Errorf("image search off: %d %s", rec.Code, rec.Body)
	}
	var kept int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM image_searches WHERE query = $1`, "later "+tag).Scan(&kept); err != nil || kept != 0 {
		t.Errorf("unavailable search kept: %d %v", kept, err)
	}
}
