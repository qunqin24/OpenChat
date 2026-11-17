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

	"golang.org/x/net/html"
	"golang.org/x/net/html/charset"
	"golang.org/x/sync/singleflight"

	"github.com/qunqin24/openchat/apps/server/internal/db/sqlc"
	"github.com/qunqin24/openchat/apps/server/internal/httpx"
	"github.com/qunqin24/openchat/apps/server/internal/tools"
)

// The card over a link in an answer: the page's title, summary, site name and
// sharing picture (Open Graph), read here rather than by the reader's browser.
//
// The rules are the image proxy's (images.go): only an address one of the
// conversation's tool results contains is fetched - so a hover never sends a
// request to an address the model made up or a prompt injection wrote into
// its prose, which could carry what the conversation holds in its query - and
// only through the guarded client, which dials public addresses only. The
// web search blocklist is checked on the address asked for and the one the
// redirects ended at. The picture is served through the image proxy's cache.
//
// What a page says is kept in memory for a day and shared by every reader; a
// page that could not be read is asked about again after ten minutes.

const (
	previewHeadBytes = 512 << 10
	previewTTL       = 24 * time.Hour
	previewMissTTL   = 10 * time.Minute
	previewTimeout   = 10 * time.Second
	previewCacheMax  = 4000
	previewTitleMax  = 300
	previewDescMax   = 400
)

type pagePreview struct {
	title, description, siteName string
	image                        string // absolute http(s) address, or empty
	expires                      time.Time
}

type previewCache struct {
	mu      sync.Mutex
	entries map[string]pagePreview
	flight  singleflight.Group
	client  *http.Client
}

var previews = &previewCache{entries: map[string]pagePreview{}}

func (c *previewCache) get(ctx context.Context, address string, blocked func(string) bool) pagePreview {
	c.mu.Lock()
	entry, ok := c.entries[address]
	c.mu.Unlock()
	if ok && time.Now().Before(entry.expires) {
		return entry
	}
	// One fetch for everyone hovering the same link at once; detached from
	// the first asker's request so its leaving does not fail the others.
	v, _, _ := c.flight.Do(address, func() (any, error) {
		fetchCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), previewTimeout)
		defer cancel()
		entry := c.fetch(fetchCtx, address, blocked)
		c.mu.Lock()
		if len(c.entries) >= previewCacheMax {
			// Bounded the favicon cache's way: a full cache starts over.
			c.entries = map[string]pagePreview{}
		}
		c.entries[address] = entry
		c.mu.Unlock()
		return entry, nil
	})
	return v.(pagePreview)
}

func (c *previewCache) fetch(ctx context.Context, address string, blocked func(string) bool) pagePreview {
	miss := pagePreview{expires: time.Now().Add(previewMissTTL)}
	client := c.client
	if client == nil {
		client = tools.GuardedClient(previewTimeout)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return miss
	}
	req.Header.Set("Accept", "text/html,application/xhtml+xml;q=0.9,*/*;q=0.5")
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; OpenChat/1.0; +https://github.com/qunqin24/openchat)")
	resp, err := client.Do(req)
	if err != nil {
		return miss
	}
	defer resp.Body.Close()
	final := resp.Request.URL
	if resp.StatusCode != http.StatusOK || blocked(final.String()) {
		return miss
	}
	mediaType, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if mediaType != "text/html" && mediaType != "application/xhtml+xml" {
		return miss
	}
	body, err := charset.NewReader(io.LimitReader(resp.Body, previewHeadBytes), resp.Header.Get("Content-Type"))
	if err != nil {
		return miss
	}
	page := readPreview(body, final)
	page.expires = time.Now().Add(previewTTL)
	return page
}

// readPreview reads a page's head: Open Graph first, then Twitter's cards,
// then the plain <title> and description. It stops at <body>: everything a
// card needs is above it, and a page's head is small.
func readPreview(r io.Reader, base *url.URL) pagePreview {
	meta := map[string]string{}
	var title strings.Builder
	inTitle := false
	z := html.NewTokenizer(r)
loop:
	for {
		switch z.Next() {
		case html.ErrorToken:
			break loop
		case html.StartTagToken, html.SelfClosingTagToken:
			name, hasAttr := z.TagName()
			switch string(name) {
			case "body":
				break loop
			case "title":
				inTitle = title.Len() == 0
			case "meta":
				var key, content string
				for hasAttr {
					var k, v []byte
					k, v, hasAttr = z.TagAttr()
					switch string(k) {
					case "property", "name":
						if key == "" {
							key = strings.ToLower(strings.TrimSpace(string(v)))
						}
					case "content":
						content = string(v)
					}
				}
				if key != "" && content != "" {
					if _, seen := meta[key]; !seen {
						meta[key] = content
					}
				}
			}
		case html.TextToken:
			if inTitle {
				title.Write(z.Text())
			}
		case html.EndTagToken:
			if name, _ := z.TagName(); string(name) == "title" {
				inTitle = false
			} else if string(name) == "head" {
				break loop
			}
		}
	}
	first := func(keys ...string) string {
		for _, k := range keys {
			if v := clean(meta[k]); v != "" {
				return v
			}
		}
		return ""
	}
	page := pagePreview{
		title:       clip(first("og:title", "twitter:title"), previewTitleMax),
		description: clip(first("og:description", "twitter:description", "description"), previewDescMax),
		siteName:    clip(first("og:site_name", "application-name"), previewTitleMax),
	}
	if page.title == "" {
		page.title = clip(clean(title.String()), previewTitleMax)
	}
	if raw := first("og:image:secure_url", "og:image", "og:image:url", "twitter:image", "twitter:image:src"); raw != "" {
		if ref, err := url.Parse(raw); err == nil {
			abs := base.ResolveReference(ref)
			if (abs.Scheme == "http" || abs.Scheme == "https") && abs.Host != "" && len(abs.String()) <= 2048 {
				page.image = abs.String()
			}
		}
	}
	return page
}

// clean folds a value's runs of whitespace into single spaces.
func clean(s string) string {
	return strings.Join(strings.Fields(strings.ToValidUTF8(s, "")), " ")
}

// previewTarget checks the address and that this conversation's tool results
// contain it, as written or without its fragment (a model citing a section
// keeps the page's address).
func (s *Server) previewTarget(ctx context.Context, conversationId ConversationId, raw string) (string, error) {
	if err := s.conversationExists(ctx, conversationId); err != nil {
		return "", err
	}
	address, err := imageAddress(raw)
	if err != nil {
		return "", err
	}
	needles := []string{address}
	if i := strings.IndexByte(address, '#'); i > 0 {
		needles = append(needles, address[:i])
	}
	for _, needle := range needles {
		found, err := s.q.ToolResultMentions(ctx, sqlc.ToolResultMentionsParams{ConversationID: conversationId, Needle: needle})
		if err != nil {
			return "", err
		}
		if found {
			return needle, nil
		}
	}
	return "", httpx.NewError(http.StatusNotFound, "link_not_found", "no source in this conversation lists that page")
}

// previewBlocked is the web search blocklist as a test; a setting that cannot
// be read blocks nothing, as the image proxy does not consult it at all.
func (s *Server) previewBlocked(ctx context.Context) func(string) bool {
	config, _, err := loadWebSearch(ctx, s.q)
	if err != nil {
		return func(string) bool { return false }
	}
	list := config.searchConfig().Blocklist
	return list.Blocks
}

// GetLinkPreview answers what the card over a link shows.
func (s *Server) GetLinkPreview(w http.ResponseWriter, r *http.Request, conversationId ConversationId, params GetLinkPreviewParams) {
	ctx := r.Context()
	address, err := s.previewTarget(ctx, conversationId, params.Url)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	out := LinkPreview{Url: params.Url}
	blocked := s.previewBlocked(ctx)
	if !blocked(address) {
		page := previews.get(ctx, address, blocked)
		out.Title = optional(page.title)
		out.Description = optional(page.description)
		out.SiteName = optional(page.siteName)
		out.Image = page.image != ""
	}
	w.Header().Set("Cache-Control", "private, max-age=3600")
	httpx.JSON(w, http.StatusOK, out)
}

// GetLinkPreviewImage serves the picture a linked page names for sharing.
func (s *Server) GetLinkPreviewImage(w http.ResponseWriter, r *http.Request, conversationId ConversationId, params GetLinkPreviewImageParams) {
	ctx := r.Context()
	address, err := s.previewTarget(ctx, conversationId, params.Url)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	blocked := s.previewBlocked(ctx)
	if blocked(address) {
		httpx.Fail(w, r, httpx.NewError(http.StatusNotFound, "image_unavailable", "the image could not be fetched"))
		return
	}
	page := previews.get(ctx, address, blocked)
	if page.image == "" || blocked(page.image) {
		w.Header().Set("Cache-Control", "private, max-age=600")
		httpx.Fail(w, r, httpx.NewError(http.StatusNotFound, "image_unavailable", "the page names no picture"))
		return
	}
	serveImage(w, r, page.image)
}

func optional(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
