// Package webui serves the built web app from inside the server binary, so a
// self-hosted deployment is one process on one origin.
//
// The build is copied into ./dist by scripts/embed-web.mjs (`make build`), and
// embedded from there because go:embed cannot reach outside the module. Vite
// precompresses its output to <file>.br and <file>.gz; this package only picks
// among those bytes, it never compresses anything itself. That is deliberate:
// brotli at maximum quality is too slow to run per request, and the point of
// serving from here is a fast first load over a slow, high-latency link.
//
// A checkout that has never run a web build embeds nothing but a .gitkeep.
// The handler then serves the API's neighbours only: "/" answers with a short
// plain-text note and everything else is a 404. `go build` and `go test` work
// either way.
package webui

import (
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"
)

// all: so hashed files whose names start with "_" or "." are embedded too;
// without it go:embed silently skips them.
//
//go:embed all:dist
var embedded embed.FS

// Cache policies. See classify.
const (
	cacheImmutable  = "public, max-age=31536000, immutable"
	cacheRevalidate = "no-cache"
	cacheModest     = "public, max-age=3600"
)

// Handler serves the embedded build. It is meant to be the router's catch-all.
func Handler() http.Handler {
	sub, err := fs.Sub(embedded, "dist")
	if err != nil {
		// "dist" is embedded above; Sub only fails on an invalid path.
		panic(err)
	}
	return New(sub)
}

// BuildID names the embedded front end: the first hex of the SHA-256 of its
// index.html, which lists every hashed asset by name, so it changes exactly
// when the front end does - not on a restart, nor on a server-only change. ""
// when nothing is embedded (placeholder mode; development, where Vite serves
// the page and reloads it itself). Sent with every response (the router's
// X-OpenChat-Build), so a page left open across an upgrade can tell it is out
// of date.
func BuildID() string {
	buildID.once.Do(func() {
		index, err := embedded.ReadFile("dist/index.html")
		if err != nil {
			return
		}
		sum := sha256.Sum256(index)
		buildID.id = hex.EncodeToString(sum[:6])
	})
	return buildID.id
}

var buildID struct {
	once sync.Once
	id   string
}

// New serves the build in fsys, whose root holds index.html.
//
// If fsys has no index.html the handler runs in placeholder mode (see the
// package comment). The check is made once, here: an embedded FS never
// changes.
func New(fsys fs.FS) http.Handler {
	h := &handler{fsys: fsys}
	if info, err := fs.Stat(fsys, "index.html"); err == nil && !info.IsDir() {
		h.hasIndex = true
	}
	return h
}

type handler struct {
	fsys     fs.FS
	hasIndex bool
	// ETags by file name. The content of an embedded file never changes, so
	// each is hashed once, on first use.
	etags sync.Map // string -> string
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Every response from here, including errors: never let a browser
	// reinterpret a body as another type.
	w.Header().Set("X-Content-Type-Options", "nosniff")

	urlPath := path.Clean("/" + r.URL.Path)

	// The API owns /api. A request that reaches this handler under it matched
	// no route, and must say so rather than be answered with the app shell.
	if urlPath == "/api" || strings.HasPrefix(urlPath, "/api/") {
		http.NotFound(w, r)
		return
	}

	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if !h.hasIndex {
		h.placeholder(w, r, urlPath)
		return
	}

	name := strings.TrimPrefix(urlPath, "/")
	if name == "" {
		name = "index.html"
	}

	if h.isFile(name) {
		h.serve(w, r, name)
		return
	}

	// Single-page-app fallback: a deep link such as /chat/123 is a client-side
	// route, so it gets the shell. Anything that looks like a file request does
	// not. A stale tab asking for /assets/old-hash.js must get a real 404;
	// answered with HTML it would be parsed as a script, and the failure it
	// causes is far harder to read than a 404 in the network panel.
	if path.Ext(name) != "" || name == "assets" || strings.HasPrefix(name, "assets/") {
		http.NotFound(w, r)
		return
	}
	h.serve(w, r, "index.html")
}

// placeholder answers when the binary was built without the web app.
func (h *handler) placeholder(w http.ResponseWriter, r *http.Request, urlPath string) {
	if urlPath != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", cacheRevalidate)
	if r.Method == http.MethodHead {
		return
	}
	_, _ = io.WriteString(w, "OpenChat API server.\n\n"+
		"This binary was built without the web app, so there is no UI to serve here.\n"+
		"Run `make build` to embed it, or `make web` for the Vite dev server.\n"+
		"The API is under /api/v1.\n")
}

// isFile reports whether name is a regular file a client may ask for by name.
// The .br and .gz siblings of another file are not: they are that file's
// encoded bytes, reached through Accept-Encoding, and handing them out as
// files would give them the wrong Content-Type.
func (h *handler) isFile(name string) bool {
	if !fs.ValidPath(name) {
		return false
	}
	if ext := path.Ext(name); ext == ".br" || ext == ".gz" {
		if h.regular(strings.TrimSuffix(name, ext)) {
			return false
		}
	}
	return h.regular(name)
}

func (h *handler) regular(name string) bool {
	info, err := fs.Stat(h.fsys, name)
	return err == nil && info.Mode().IsRegular()
}

// serve sends name, in the best encoding the client accepts.
func (h *handler) serve(w http.ResponseWriter, r *http.Request, name string) {
	encoding, variant := h.negotiate(r, name)

	f, err := h.fsys.Open(variant)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	// embed.FS and fstest.MapFS files seek. Anything that does not is read
	// whole; ServeContent needs to seek for ranges and conditional requests.
	body, ok := f.(io.ReadSeeker)
	if !ok {
		data, err := io.ReadAll(f)
		if err != nil {
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}
		body = bytes.NewReader(data)
	}

	hd := w.Header()
	ctype := contentType(name)
	hd.Set("Content-Type", ctype)
	hd.Set("Cache-Control", classify(name))
	hd.Set("ETag", h.etag(variant, body))
	if h.hasVariants(name) {
		hd.Add("Vary", "Accept-Encoding")
	}
	if encoding != "" {
		hd.Set("Content-Encoding", encoding)
		// ServeContent leaves Content-Length off whenever Content-Encoding is
		// set (it cannot tell middleware compression from ours), which would
		// make every response chunked. We know the length of the bytes we are
		// sending; a Range request has its own, which ServeContent sets.
		if r.Header.Get("Range") == "" {
			hd.Set("Content-Length", strconv.FormatInt(info.Size(), 10))
		}
	}
	if strings.HasPrefix(ctype, "text/html") {
		hd.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		// No Content-Security-Policy on purpose. The app inlines a theme
		// script and a splash style in index.html, loads model icons and
		// favicons from this origin and from third-party hosts, and renders
		// model-authored content; a policy tight enough to mean something
		// would have to be written against all of that and tested in a
		// browser, and a loose one is decoration. Add it deliberately, with
		// frame-ancestors, when that work is done.
	}

	// A zero modtime: embedded files have none, so no Last-Modified is sent
	// and the ETag alone drives revalidation.
	http.ServeContent(w, r, name, time.Time{}, body)
}

// negotiate picks the encoding to send for name and the file that holds those
// bytes. Brotli wins over gzip when both are acceptable.
func (h *handler) negotiate(r *http.Request, name string) (encoding, variant string) {
	accept := r.Header.Values("Accept-Encoding")
	if len(accept) > 0 {
		if acceptsEncoding(accept, "br") && h.regular(name+".br") {
			return "br", name + ".br"
		}
		if acceptsEncoding(accept, "gzip") && h.regular(name+".gz") {
			return "gzip", name + ".gz"
		}
	}
	return "", name
}

func (h *handler) hasVariants(name string) bool {
	return h.regular(name+".br") || h.regular(name+".gz")
}

// etag is a strong validator over the bytes actually sent, so each encoding
// of a file has its own. Computed once per file.
func (h *handler) etag(variant string, body io.ReadSeeker) string {
	if v, ok := h.etags.Load(variant); ok {
		return v.(string)
	}
	sum := sha256.New()
	_, _ = io.Copy(sum, body)
	_, _ = body.Seek(0, io.SeekStart)
	tag := `"` + hex.EncodeToString(sum.Sum(nil)[:12]) + `"`
	h.etags.Store(variant, tag)
	return tag
}

// classify returns the Cache-Control for the file at name.
//
//   - assets/: Vite names these by content hash, so a URL never changes
//     meaning and can be kept for a year without asking again. This is what
//     makes a repeat visit nearly free.
//   - index.html: names those hashes, so it is the one file that must be
//     revalidated every time, or a deploy would never be noticed.
//   - everything else (favicons, model icons, self-hosted fonts at the root):
//     stable names but not hashed, so a short lifetime.
func classify(name string) string {
	switch {
	case name == "index.html":
		return cacheRevalidate
	case strings.HasPrefix(name, "assets/"):
		return cacheImmutable
	default:
		return cacheModest
	}
}

// types holds what the build emits that the platform's mime table may lack:
// distroless has no /etc/mime.types, and Go's built-in table omits fonts.
var types = map[string]string{
	".html":        "text/html; charset=utf-8",
	".js":          "text/javascript; charset=utf-8",
	".mjs":         "text/javascript; charset=utf-8",
	".css":         "text/css; charset=utf-8",
	".json":        "application/json",
	".map":         "application/json",
	".webmanifest": "application/manifest+json",
	".svg":         "image/svg+xml",
	".txt":         "text/plain; charset=utf-8",
	".xml":         "application/xml",
	".ico":         "image/x-icon",
	".png":         "image/png",
	".jpg":         "image/jpeg",
	".jpeg":        "image/jpeg",
	".gif":         "image/gif",
	".webp":        "image/webp",
	".avif":        "image/avif",
	".woff":        "font/woff",
	".woff2":       "font/woff2",
	".ttf":         "font/ttf",
	".otf":         "font/otf",
	".wasm":        "application/wasm",
}

// contentType comes from the original file's extension, never from the
// .br/.gz one: the encoding is described by Content-Encoding.
func contentType(name string) string {
	ext := strings.ToLower(path.Ext(name))
	if t, ok := types[ext]; ok {
		return t
	}
	if t := mime.TypeByExtension(ext); t != "" {
		return t
	}
	return "application/octet-stream"
}

// acceptsEncoding reports whether an Accept-Encoding header allows coding,
// honouring q-values (so "br;q=0" refuses it) and the "*" wildcard.
func acceptsEncoding(header []string, coding string) bool {
	allowed, wildcard := false, false
	seen := false
	for _, line := range header {
		for _, part := range strings.Split(line, ",") {
			token, params, _ := strings.Cut(strings.TrimSpace(part), ";")
			token = strings.ToLower(strings.TrimSpace(token))
			if token == "" {
				continue
			}
			q := 1.0
			for _, p := range strings.Split(params, ";") {
				k, v, ok := strings.Cut(strings.TrimSpace(p), "=")
				if ok && strings.EqualFold(strings.TrimSpace(k), "q") {
					if parsed, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil {
						q = parsed
					} else {
						q = 0
					}
				}
			}
			switch token {
			case coding:
				allowed, seen = q > 0, true
			case "*":
				wildcard = q > 0
			}
		}
	}
	if seen {
		return allowed
	}
	return wildcard
}
