package api

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/qunqin24/openchat/apps/server/internal/httpx"
)

// Site icons for the sources a reply cites, fetched here rather than by the
// browser. A browser asking each site — or an icon service — for its icon
// would tell a third party what every reader's answers drew on; this way the
// service learns only that this server once wanted a domain's icon, and each
// icon is fetched once per cache period, not once per view.
//
// DuckDuckGo's icon service is the source: one fixed host, so the domain in
// the path never decides where this server connects, and it answers 404 for a
// site with no icon. Ours answers that (and an icon service that is down) with
// a 1×1 transparent PNG rather than an error: a source list is mostly sites
// without one, and every error status is a red line in the reader's console.
// The client sees an image one pixel wide and draws its own letter mark.

const (
	faviconSource   = "https://icons.duckduckgo.com/ip3/"
	faviconMaxBytes = 256 << 10
	faviconTTL      = 7 * 24 * time.Hour
	faviconMissTTL  = 24 * time.Hour
	faviconCacheMax = 4000
)

// faviconDomain is a lowercase host name: letters, digits, dots and hyphens,
// with at least one dot. Anything else is not a site's name.
var faviconDomain = regexp.MustCompile(`^[a-z0-9-]+(\.[a-z0-9-]+)+$`)

type faviconEntry struct {
	body        []byte // nil: the site has no icon
	contentType string
	expires     time.Time
}

type faviconCache struct {
	mu      sync.Mutex
	entries map[string]faviconEntry
	client  *http.Client
	source  string
}

var favicons = &faviconCache{
	entries: map[string]faviconEntry{},
	client:  &http.Client{Timeout: 10 * time.Second},
	source:  faviconSource,
}

func (c *faviconCache) get(ctx context.Context, domain string) (faviconEntry, error) {
	now := time.Now()
	c.mu.Lock()
	entry, ok := c.entries[domain]
	c.mu.Unlock()
	if ok && now.Before(entry.expires) {
		return entry, nil
	}

	entry, err := c.fetch(ctx, domain)
	if err != nil {
		return faviconEntry{}, err
	}
	c.mu.Lock()
	if len(c.entries) >= faviconCacheMax {
		// Crude, but bounded: a full cache starts over rather than tracking
		// recency for what is only a convenience.
		c.entries = map[string]faviconEntry{}
	}
	c.entries[domain] = entry
	c.mu.Unlock()
	return entry, nil
}

func (c *faviconCache) fetch(ctx context.Context, domain string) (faviconEntry, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.source+domain+".ico", nil)
	if err != nil {
		return faviconEntry{}, err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return faviconEntry{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return faviconEntry{expires: time.Now().Add(faviconMissTTL)}, nil
	}
	contentType := resp.Header.Get("Content-Type")
	if resp.StatusCode != http.StatusOK || !strings.HasPrefix(contentType, "image/") {
		return faviconEntry{}, httpx.NewError(http.StatusBadGateway, "favicon_unavailable", "the icon service did not answer")
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, faviconMaxBytes+1))
	if err != nil {
		return faviconEntry{}, err
	}
	// SVG can carry script, which would run on this origin if the icon's URL
	// were opened directly; a site whose only icon is SVG gets the letter mark.
	if len(body) > faviconMaxBytes || strings.Contains(contentType, "svg") {
		return faviconEntry{expires: time.Now().Add(faviconMissTTL)}, nil
	}
	return faviconEntry{body: body, contentType: contentType, expires: time.Now().Add(faviconTTL)}, nil
}

// GetFavicon serves a site's icon from the cache, fetching it on a miss.
func (s *Server) GetFavicon(w http.ResponseWriter, r *http.Request, domain string) {
	domain = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(domain)), "www.")
	if len(domain) > 253 || !faviconDomain.MatchString(domain) {
		httpx.Fail(w, r, httpx.NewError(http.StatusBadRequest, "invalid_domain", "not a domain name"))
		return
	}
	entry, err := favicons.get(r.Context(), domain)
	switch {
	case err != nil:
		slog.WarnContext(r.Context(), "favicon fetch", "domain", domain, "error", err)
		// Asked again in a few minutes: the service may be back by then.
		writeIcon(w, "image/png", noFavicon, "private, max-age=300")
	case entry.body == nil:
		// Cached by the browser too, so a site without an icon is asked about
		// once a day, not on every reply that cites it.
		writeIcon(w, "image/png", noFavicon, "private, max-age=86400")
	default:
		writeIcon(w, entry.contentType, entry.body, "private, max-age=604800")
	}
}

// noFavicon is a 1×1 transparent PNG: "this site has no icon".
var noFavicon = func() []byte {
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewNRGBA(image.Rect(0, 0, 1, 1))); err != nil {
		panic(err)
	}
	return buf.Bytes()
}()

func writeIcon(w http.ResponseWriter, contentType string, body []byte, cache string) {
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", cache)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
	_, _ = w.Write(body)
}
