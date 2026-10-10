package api

import (
	"context"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/qunqin/openchat/apps/server/internal/db/sqlc"
	"github.com/qunqin/openchat/apps/server/internal/httpx"
	"github.com/qunqin/openchat/apps/server/internal/tools"
)

// Pictures an answer shows, fetched here rather than by the reader's browser.
//
// Two rules make this safe to point at addresses that came out of a model.
// The address must appear in one of the conversation's tool results: a page
// the model read listed it, or a search returned it — so an answer shows what
// its sources hold, never a URL the model invented or one a prompt injection
// slipped into its prose. And the fetch goes through the same guarded client
// as web_fetch, which dials public addresses only, so the proxy cannot be
// turned on the server's own network.
//
// Caching here serves the same picture to every reload and every reader from
// memory, and the browser is told it may keep it a day.

const (
	imageMaxBytes   = 8 << 20
	imageCacheBytes = 64 << 20
	imageTTL        = 24 * time.Hour
	imageMissTTL    = 10 * time.Minute
	imageTimeout    = 15 * time.Second
)

// imageTypes are the formats served. SVG is not one: it can carry script,
// which would run on this origin if the image were opened directly.
var imageTypes = map[string]bool{
	"image/jpeg": true, "image/png": true, "image/gif": true, "image/webp": true, "image/avif": true,
}

type imageEntry struct {
	body        []byte // nil: the image could not be had
	contentType string
	expires     time.Time
}

type imageCache struct {
	mu      sync.Mutex
	entries map[string]imageEntry
	order   []string // oldest first, for eviction
	size    int
	client  *http.Client
}

var images = &imageCache{entries: map[string]imageEntry{}}

func (c *imageCache) get(ctx context.Context, address string) imageEntry {
	c.mu.Lock()
	entry, ok := c.entries[address]
	c.mu.Unlock()
	if ok && time.Now().Before(entry.expires) {
		return entry
	}
	entry = c.fetch(ctx, address)
	c.put(address, entry)
	return entry
}

func (c *imageCache) put(address string, entry imageEntry) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if old, ok := c.entries[address]; ok {
		c.size -= len(old.body)
	} else {
		c.order = append(c.order, address)
	}
	c.entries[address] = entry
	c.size += len(entry.body)
	// Oldest out until it fits; a picture is cheap to fetch again.
	for c.size > imageCacheBytes && len(c.order) > 1 {
		victim := c.order[0]
		c.order = c.order[1:]
		c.size -= len(c.entries[victim].body)
		delete(c.entries, victim)
	}
}

func (c *imageCache) fetch(ctx context.Context, address string) imageEntry {
	miss := imageEntry{expires: time.Now().Add(imageMissTTL)}
	client := c.client
	if client == nil {
		client = tools.GuardedClient(imageTimeout)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return miss
	}
	req.Header.Set("Accept", "image/avif,image/webp,image/png,image/jpeg,image/*;q=0.8")
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; OpenChat image proxy)")
	// Sites that refuse hotlinking usually let their own pages through.
	if target, err := url.Parse(address); err == nil {
		req.Header.Set("Referer", target.Scheme+"://"+target.Host+"/")
	}
	resp, err := client.Do(req)
	if err != nil {
		return miss
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return miss
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, imageMaxBytes+1))
	if err != nil || len(body) == 0 || len(body) > imageMaxBytes {
		return miss
	}
	// What the bytes are, not what the header says; AVIF is the one format the
	// sniffer does not know, so for it the header has to be believed.
	declared, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	sniffed := http.DetectContentType(body)
	contentType := sniffed
	if declared == "image/avif" && !strings.HasPrefix(sniffed, "image/") {
		contentType = declared
	}
	if !imageTypes[contentType] {
		return miss
	}
	return imageEntry{body: body, contentType: contentType, expires: time.Now().Add(imageTTL)}
}

// GetConversationImage serves a picture an answer in this conversation shows.
func (s *Server) GetConversationImage(w http.ResponseWriter, r *http.Request, conversationId ConversationId, params GetConversationImageParams) {
	ctx := r.Context()
	if err := s.conversationExists(ctx, conversationId); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	address, err := imageAddress(params.Url)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	found, err := s.q.ToolResultMentions(ctx, sqlc.ToolResultMentionsParams{ConversationID: conversationId, Needle: address})
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	if !found {
		httpx.Fail(w, r, httpx.NewError(http.StatusNotFound, "image_not_found", "no source in this conversation lists that image"))
		return
	}
	serveImage(w, r, address)
}

// imageAddress checks that what was asked for is an http or https address.
func imageAddress(raw string) (string, error) {
	address := strings.TrimSpace(raw)
	target, err := url.Parse(address)
	if err != nil || (target.Scheme != "http" && target.Scheme != "https") || target.Host == "" || len(address) > 2048 {
		return "", httpx.NewError(http.StatusBadRequest, "invalid_url", "not an http or https address")
	}
	return address, nil
}

// serveImage fetches an address through the guarded, cached client and writes
// it, or answers 404 for one that cannot be had.
func serveImage(w http.ResponseWriter, r *http.Request, address string) {
	entry := images.get(r.Context(), address)
	if entry.body == nil {
		// A picture that cannot be had now is asked about again later, not on
		// every render of the answer.
		w.Header().Set("Cache-Control", "private, max-age=600")
		httpx.Fail(w, r, httpx.NewError(http.StatusNotFound, "image_unavailable", "the image could not be fetched"))
		return
	}
	h := w.Header()
	h.Set("Content-Type", entry.contentType)
	h.Set("Cache-Control", "private, max-age=86400")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Security-Policy", "default-src 'none'; sandbox")
	_, _ = w.Write(entry.body)
}
