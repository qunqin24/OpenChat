package webui

import (
	"bytes"
	"compress/gzip"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

const (
	indexHTML = "<!doctype html><title>OpenChat</title><div id=root></div>"
	appJS     = "console.log('app');"
)

// A build as Vite and precompress would leave it. The .br and .gz bodies are
// labelled rather than real: the handler only picks bytes, so tests can tell
// them apart by content.
func testFS() fs.FS {
	return fstest.MapFS{
		"index.html":              {Data: []byte(indexHTML)},
		"index.html.br":           {Data: []byte("BR:index")},
		"index.html.gz":           {Data: []byte("GZ:index")},
		"favicon.svg":             {Data: []byte("<svg/>")},
		"model-icons/openai.svg":  {Data: []byte("<svg/>")},
		"assets/app-abc123.js":    {Data: []byte(appJS)},
		"assets/app-abc123.js.br": {Data: []byte("BR:app")},
		"assets/app-abc123.js.gz": {Data: []byte("GZ:app")},
		"assets/_chunk-9f.js":     {Data: []byte("export{}")},
		"assets/font-xyz.woff2":   {Data: []byte("wOF2")},
		"assets/only-gz-1.css":    {Data: []byte("a{}")},
		"assets/only-gz-1.css.gz": {Data: []byte("GZ:css")},
		"assets/data-2.json":      {Data: []byte("{}")},
		"assets/data-2.json.br":   {Data: []byte("BR:json")},
	}
}

func do(t *testing.T, h http.Handler, method, target string, headers ...string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, nil)
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func body(rec *httptest.ResponseRecorder) string { return rec.Body.String() }

func TestNegotiation(t *testing.T) {
	h := New(testFS())
	cases := []struct {
		name, accept, wantEnc, wantBody string
	}{
		{"br preferred over gzip", "gzip, deflate, br", "br", "BR:app"},
		{"br only", "br", "br", "BR:app"},
		{"gzip only", "gzip", "gzip", "GZ:app"},
		{"no header", "", "", appJS},
		{"unrelated codings", "deflate, zstd", "", appJS},
		{"br refused by q=0", "br;q=0, gzip", "gzip", "GZ:app"},
		{"wildcard", "*", "br", "BR:app"},
		{"wildcard with br refused", "*, br;q=0", "gzip", "GZ:app"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var rec *httptest.ResponseRecorder
			if tc.accept == "" {
				rec = do(t, h, "GET", "/assets/app-abc123.js")
			} else {
				rec = do(t, h, "GET", "/assets/app-abc123.js", "Accept-Encoding", tc.accept)
			}
			if rec.Code != 200 {
				t.Fatalf("status %d", rec.Code)
			}
			if got := rec.Header().Get("Content-Encoding"); got != tc.wantEnc {
				t.Errorf("Content-Encoding = %q, want %q", got, tc.wantEnc)
			}
			if got := body(rec); got != tc.wantBody {
				t.Errorf("body = %q, want %q", got, tc.wantBody)
			}
			// The type comes from the original extension, not .br or .gz.
			if got := rec.Header().Get("Content-Type"); got != "text/javascript; charset=utf-8" {
				t.Errorf("Content-Type = %q", got)
			}
			if got := rec.Header().Get("Vary"); got != "Accept-Encoding" {
				t.Errorf("Vary = %q", got)
			}
			if rec.Header().Get("ETag") == "" {
				t.Error("no ETag")
			}
		})
	}
}

func TestNegotiationFallsThroughMissingVariants(t *testing.T) {
	h := New(testFS())
	// Only a .gz exists: a br-capable client still gets it.
	rec := do(t, h, "GET", "/assets/only-gz-1.css", "Accept-Encoding", "br, gzip")
	if rec.Header().Get("Content-Encoding") != "gzip" || body(rec) != "GZ:css" {
		t.Errorf("got %q %q", rec.Header().Get("Content-Encoding"), body(rec))
	}
	// Only a .br exists: a gzip-only client gets the original.
	rec = do(t, h, "GET", "/assets/data-2.json", "Accept-Encoding", "gzip")
	if rec.Header().Get("Content-Encoding") != "" || body(rec) != "{}" {
		t.Errorf("got %q %q", rec.Header().Get("Content-Encoding"), body(rec))
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q", got)
	}
}

func TestIncompressibleFileHasNoEncodingOrVary(t *testing.T) {
	rec := do(t, New(testFS()), "GET", "/assets/font-xyz.woff2", "Accept-Encoding", "br, gzip")
	if rec.Code != 200 || body(rec) != "wOF2" {
		t.Fatalf("status %d body %q", rec.Code, body(rec))
	}
	if rec.Header().Get("Content-Encoding") != "" || rec.Header().Get("Vary") != "" {
		t.Errorf("unexpected encoding headers: %v", rec.Header())
	}
	if got := rec.Header().Get("Content-Type"); got != "font/woff2" {
		t.Errorf("Content-Type = %q", got)
	}
}

func TestContentLengthMatchesTheEncodedBody(t *testing.T) {
	rec := do(t, New(testFS()), "GET", "/assets/app-abc123.js", "Accept-Encoding", "br")
	if got := rec.Header().Get("Content-Length"); got != "6" {
		t.Errorf("Content-Length = %q, want 6", got)
	}
}

func TestCacheControlPerPathClass(t *testing.T) {
	h := New(testFS())
	cases := []struct{ target, want string }{
		{"/assets/app-abc123.js", "public, max-age=31536000, immutable"},
		{"/assets/_chunk-9f.js", "public, max-age=31536000, immutable"}, // embedded despite the leading _
		{"/assets/font-xyz.woff2", "public, max-age=31536000, immutable"},
		{"/", "no-cache"},
		{"/index.html", "no-cache"},
		{"/chat/123", "no-cache"}, // the SPA fallback
		{"/favicon.svg", "public, max-age=3600"},
		{"/model-icons/openai.svg", "public, max-age=3600"},
	}
	for _, tc := range cases {
		t.Run(tc.target, func(t *testing.T) {
			rec := do(t, h, "GET", tc.target)
			if rec.Code != 200 {
				t.Fatalf("status %d", rec.Code)
			}
			if got := rec.Header().Get("Cache-Control"); got != tc.want {
				t.Errorf("Cache-Control = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestIndexAtRootAndHTMLHeaders(t *testing.T) {
	rec := do(t, New(testFS()), "GET", "/", "Accept-Encoding", "br")
	if rec.Code != 200 || body(rec) != "BR:index" || rec.Header().Get("Content-Encoding") != "br" {
		t.Fatalf("status %d enc %q body %q", rec.Code, rec.Header().Get("Content-Encoding"), body(rec))
	}
	h := rec.Header()
	if got := h.Get("Content-Type"); got != "text/html; charset=utf-8" {
		t.Errorf("Content-Type = %q", got)
	}
	if got := h.Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q", got)
	}
	if h.Get("Referrer-Policy") == "" {
		t.Error("no Referrer-Policy on HTML")
	}
}

func TestMissingAssetIs404NotIndex(t *testing.T) {
	h := New(testFS())
	for _, target := range []string{"/assets/missing.js", "/assets/missing", "/assets", "/assets/", "/nope.css", "/some/dir/file.png"} {
		t.Run(target, func(t *testing.T) {
			rec := do(t, h, "GET", target)
			if rec.Code != 404 {
				t.Errorf("status %d, want 404", rec.Code)
			}
			if strings.Contains(body(rec), "<div id=root>") {
				t.Error("served index.html for a file request")
			}
			if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
				t.Errorf("X-Content-Type-Options = %q on a 404", got)
			}
		})
	}
}

func TestSPAFallback(t *testing.T) {
	h := New(testFS())
	for _, target := range []string{"/chat/123", "/admin/users", "/login", "/model-icons", "/chat/123?x=1"} {
		t.Run(target, func(t *testing.T) {
			rec := do(t, h, "GET", target)
			if rec.Code != 200 || body(rec) != indexHTML {
				t.Fatalf("status %d body %q", rec.Code, body(rec))
			}
			if rec.Header().Get("Cache-Control") != "no-cache" {
				t.Errorf("Cache-Control = %q", rec.Header().Get("Cache-Control"))
			}
		})
	}
	// The fallback negotiates like the real file.
	rec := do(t, h, "GET", "/chat/123", "Accept-Encoding", "gzip")
	if rec.Header().Get("Content-Encoding") != "gzip" || body(rec) != "GZ:index" {
		t.Errorf("got %q %q", rec.Header().Get("Content-Encoding"), body(rec))
	}
}

func TestHEAD(t *testing.T) {
	h := New(testFS())
	for _, target := range []string{"/assets/app-abc123.js", "/", "/chat/123"} {
		t.Run(target, func(t *testing.T) {
			rec := do(t, h, "HEAD", target, "Accept-Encoding", "br")
			if rec.Code != 200 {
				t.Fatalf("status %d", rec.Code)
			}
			if rec.Body.Len() != 0 {
				t.Errorf("HEAD sent a body of %d bytes", rec.Body.Len())
			}
			if rec.Header().Get("Content-Encoding") != "br" || rec.Header().Get("Content-Length") == "" {
				t.Errorf("headers: %v", rec.Header())
			}
		})
	}
	if rec := do(t, h, "HEAD", "/assets/missing.js"); rec.Code != 404 {
		t.Errorf("HEAD missing asset: status %d", rec.Code)
	}
}

func TestOtherMethodsAreRefused(t *testing.T) {
	rec := do(t, New(testFS()), "POST", "/chat/123")
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status %d", rec.Code)
	}
	if got := rec.Header().Get("Allow"); got != "GET, HEAD" {
		t.Errorf("Allow = %q", got)
	}
}

func TestAPIPathsAreNeverSwallowed(t *testing.T) {
	h := New(testFS())
	for _, method := range []string{"GET", "HEAD", "POST", "PUT", "DELETE"} {
		for _, target := range []string{"/api", "/api/", "/api/v1/nope", "/api/v1/conversations/x/typo", "/api/anything.js", "/api/../api/v1/x", "//api/v1/x"} {
			rec := do(t, h, method, target)
			if rec.Code != 404 {
				t.Errorf("%s %s: status %d, want 404", method, target, rec.Code)
			}
			if strings.Contains(body(rec), "<div id=root>") {
				t.Errorf("%s %s served index.html", method, target)
			}
		}
	}
}

func TestPathTraversalCannotEscapeTheFS(t *testing.T) {
	h := New(testFS())
	for _, target := range []string{"/../go.mod", "/assets/../../go.mod", "/%2e%2e/go.mod"} {
		rec := do(t, h, "GET", target)
		if rec.Code == 200 && !strings.Contains(body(rec), "<div id=root>") {
			t.Errorf("%s: status %d body %q", target, rec.Code, body(rec))
		}
	}
}

func TestEncodedSiblingsAreNotFiles(t *testing.T) {
	h := New(testFS())
	for _, target := range []string{"/assets/app-abc123.js.br", "/assets/app-abc123.js.gz", "/index.html.br"} {
		if rec := do(t, h, "GET", target); rec.Code != 404 {
			t.Errorf("%s: status %d, want 404", target, rec.Code)
		}
	}
}

func TestConditionalRequests(t *testing.T) {
	h := New(testFS())
	first := do(t, h, "GET", "/assets/app-abc123.js", "Accept-Encoding", "br")
	etag := first.Header().Get("ETag")
	if etag == "" {
		t.Fatal("no ETag")
	}

	rec := do(t, h, "GET", "/assets/app-abc123.js", "Accept-Encoding", "br", "If-None-Match", etag)
	if rec.Code != http.StatusNotModified || rec.Body.Len() != 0 {
		t.Errorf("status %d, body %d bytes", rec.Code, rec.Body.Len())
	}

	// Each encoding is different bytes, so it has its own validator.
	gz := do(t, h, "GET", "/assets/app-abc123.js", "Accept-Encoding", "gzip")
	id := do(t, h, "GET", "/assets/app-abc123.js")
	if gz.Header().Get("ETag") == etag || id.Header().Get("ETag") == etag || gz.Header().Get("ETag") == id.Header().Get("ETag") {
		t.Errorf("ETags collide: br %s gzip %s identity %s", etag, gz.Header().Get("ETag"), id.Header().Get("ETag"))
	}
	if rec := do(t, h, "GET", "/assets/app-abc123.js", "Accept-Encoding", "gzip", "If-None-Match", etag); rec.Code != 200 {
		t.Errorf("a br validator matched the gzip body: status %d", rec.Code)
	}
}

func TestRangeRequestUsesEncodedBytes(t *testing.T) {
	rec := do(t, New(testFS()), "GET", "/assets/app-abc123.js", "Accept-Encoding", "br", "Range", "bytes=0-1")
	if rec.Code != http.StatusPartialContent || body(rec) != "BR" {
		t.Errorf("status %d body %q", rec.Code, body(rec))
	}
	if got := rec.Header().Get("Content-Length"); got != "2" {
		t.Errorf("Content-Length = %q, want 2", got)
	}
}

func TestRealGzipBytesSurvive(t *testing.T) {
	// A realistic round trip: what is stored is what is sent, byte for byte.
	var buf bytes.Buffer
	zw, _ := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	_, _ = zw.Write([]byte(strings.Repeat("hello ", 200)))
	_ = zw.Close()
	h := New(fstest.MapFS{
		"index.html":       {Data: []byte(indexHTML)},
		"assets/a-1.js":    {Data: []byte(strings.Repeat("hello ", 200))},
		"assets/a-1.js.gz": {Data: buf.Bytes()},
	})
	rec := do(t, h, "GET", "/assets/a-1.js", "Accept-Encoding", "gzip")
	zr, err := gzip.NewReader(rec.Body)
	if err != nil {
		t.Fatal(err)
	}
	out, _ := io.ReadAll(zr)
	if string(out) != strings.Repeat("hello ", 200) {
		t.Error("gzip body does not round-trip")
	}
}

func TestPlaceholderWhenNoBuild(t *testing.T) {
	// What a fresh checkout embeds: a directory with only .gitkeep.
	h := New(fstest.MapFS{".gitkeep": {Data: nil}})

	rec := do(t, h, "GET", "/")
	if rec.Code != 200 {
		t.Fatalf("status %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Errorf("Content-Type = %q", ct)
	}
	if !strings.Contains(body(rec), "make build") || !strings.Contains(body(rec), "/api/v1") {
		t.Errorf("note does not say what to do: %q", body(rec))
	}

	for _, target := range []string{"/chat/123", "/assets/x.js", "/favicon.svg", "/.gitkeep", "/api/v1/nope"} {
		if rec := do(t, h, "GET", target); rec.Code != 404 {
			t.Errorf("%s: status %d, want 404", target, rec.Code)
		}
	}
	if rec := do(t, h, "HEAD", "/"); rec.Code != 200 || rec.Body.Len() != 0 {
		t.Errorf("HEAD /: status %d, %d bytes", rec.Code, rec.Body.Len())
	}
	if rec := do(t, h, "POST", "/"); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST /: status %d", rec.Code)
	}
}

func TestEmbeddedHandlerBuilds(t *testing.T) {
	// Whatever dist/ holds in this checkout (placeholder or a real build), the
	// embedded handler must come up and answer "/" without error.
	rec := do(t, Handler(), "GET", "/")
	if rec.Code != 200 {
		t.Errorf("status %d", rec.Code)
	}
}
