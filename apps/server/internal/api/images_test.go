package api

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"testing"
)

func pngBytes(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	img.Set(1, 1, color.RGBA{R: 255, A: 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestImageCacheServesOnlyRealPictures(t *testing.T) {
	picture := pngBytes(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/photo.png":
			// The declared type is wrong; the bytes decide.
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write(picture)
		case "/drawing.svg":
			w.Header().Set("Content-Type", "image/svg+xml")
			_, _ = w.Write([]byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`))
		case "/page.html":
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write([]byte("<html><body>not a picture</body></html>"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	cache := &imageCache{entries: map[string]imageEntry{}, client: srv.Client()}

	got := cache.get(context.Background(), srv.URL+"/photo.png")
	if got.contentType != "image/png" || !bytes.Equal(got.body, picture) {
		t.Errorf("photo: %q, %d bytes", got.contentType, len(got.body))
	}
	for _, path := range []string{"/drawing.svg", "/page.html", "/missing.png"} {
		if got := cache.get(context.Background(), srv.URL+path); got.body != nil {
			t.Errorf("%s was served as %q", path, got.contentType)
		}
	}
}

func TestImageCacheStaysWithinItsBudget(t *testing.T) {
	cache := &imageCache{entries: map[string]imageEntry{}}
	big := make([]byte, imageCacheBytes/3+1)
	for _, name := range []string{"a", "b", "c", "d"} {
		cache.put(name, imageEntry{body: big, contentType: "image/png"})
	}
	if cache.size > imageCacheBytes {
		t.Errorf("cache holds %d bytes, budget %d", cache.size, imageCacheBytes)
	}
	if _, ok := cache.entries["a"]; ok {
		t.Error("the oldest picture was kept over the newest")
	}
	if _, ok := cache.entries["d"]; !ok {
		t.Error("the newest picture was evicted")
	}
}
