package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestFaviconCache(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		switch r.URL.Path {
		case "/example.com.ico":
			w.Header().Set("Content-Type", "image/x-icon")
			_, _ = w.Write([]byte("ICO"))
		case "/svg.example.ico":
			w.Header().Set("Content-Type", "image/svg+xml")
			_, _ = w.Write([]byte("<svg/>"))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	cache := &faviconCache{entries: map[string]faviconEntry{}, client: srv.Client(), source: srv.URL + "/"}
	ctx := context.Background()

	for range 2 {
		entry, err := cache.get(ctx, "example.com")
		if err != nil || string(entry.body) != "ICO" || !strings.HasPrefix(entry.contentType, "image/") {
			t.Fatalf("entry %+v, err %v", entry, err)
		}
	}
	if hits.Load() != 1 {
		t.Errorf("fetched %d times, want once and then the cache", hits.Load())
	}
	if entry, err := cache.get(ctx, "missing.example"); err != nil || entry.body != nil {
		t.Errorf("missing icon: %+v %v, want a cached miss", entry, err)
	}
	if entry, err := cache.get(ctx, "svg.example"); err != nil || entry.body != nil {
		t.Errorf("svg icon: %+v %v, want it refused", entry, err)
	}
	for _, bad := range []string{"localhost", "a/b.com", "127.0.0.1:80", "x..y"} {
		if faviconDomain.MatchString(bad) {
			t.Errorf("%q passed the domain check", bad)
		}
	}
}
