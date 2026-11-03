package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/netip"
	"net/url"
	"regexp"
	"strings"

	"github.com/qunqin24/openchat/apps/server/internal/agent"
)

var isoDay = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

// defaultReader is Jina Reader (r.jina.ai): a keyless service that fetches a
// page as a browser would - past most bot walls, running its JavaScript - and
// returns its text. Sites turn web_fetch away often enough (a deep research's
// researchers had nearly half their reads refused, docs/design/deep-research-plan.md
// §2) that a refused read is asked of it once before the model is told no.
// It is told only the address, as a search service is told the query.
const defaultReader = "https://r.jina.ai/"

// ReaderMode is how web_fetch uses the reading service (后台 → 联网搜索 → 网页读取).
type ReaderMode string

const (
	// ReaderOff reads every page itself.
	ReaderOff ReaderMode = "builtin"
	// ReaderFallback reads a page itself and asks the service only when the
	// site turns it away or the page has no readable text. The default.
	ReaderFallback ReaderMode = "fallback"
	// ReaderFirst asks the service first, and reads the page itself when the
	// service cannot.
	ReaderFirst ReaderMode = "jina"
)

// Valid reports whether m is a mode the settings may hold.
func (m ReaderMode) Valid() bool { return m == ReaderOff || m == ReaderFallback || m == ReaderFirst }

// reader is the service to ask, "" for none.
func (w WebFetch) reader() string {
	if w.Mode == ReaderOff {
		return ""
	}
	if w.Reader != "" {
		return w.Reader
	}
	if w.Client != nil {
		return "" // a test fetching from a local server: never the real service
	}
	return defaultReader
}

// load reads a page as the mode says. Fetched itself, a page is guarded by
// the client on every dial; asked of the service, the connection is the
// service's, so an address that is plainly not public is refused first, as
// the guard would have refused it.
func (w WebFetch) load(ctx context.Context, client *http.Client, target *url.URL) (page, error) {
	reader := w.reader()
	if reader != "" && w.Mode == ReaderFirst {
		if !plainlyPublic(target) {
			return page{}, agent.Errorf("That address is not reachable from here: only public internet addresses can be fetched.")
		}
		read, err := readerFetch(ctx, client, reader, w.ReaderKey, target.String())
		if err == nil {
			return read, nil
		}
		slog.InfoContext(ctx, "reader could not read the page; fetching it directly", "error", err)
		return fetch(ctx, client, target.String())
	}
	got, err := fetch(ctx, client, target.String())
	// Turned away, or nothing readable (a page built by JavaScript): asked
	// once more of the service, which fetches it as a browser would.
	var refused *fetchRefused
	if reader != "" && (errors.As(err, &refused) || (err == nil && strings.TrimSpace(got.text) == "")) {
		read, readErr := readerFetch(ctx, client, reader, w.ReaderKey, target.String())
		if readErr == nil {
			return read, nil
		}
		slog.InfoContext(ctx, "reader could not read the page either", "error", readErr)
	}
	return got, err
}

// plainlyPublic is false for a host that is a non-public address or names
// this machine. A name that resolves to a private address is the service's to
// resolve, on its own network, not ours.
func plainlyPublic(target *url.URL) bool {
	host := strings.ToLower(target.Hostname())
	if host == "localhost" || strings.HasSuffix(host, ".localhost") || strings.HasSuffix(host, ".local") || strings.HasSuffix(host, ".internal") {
		return false
	}
	if addr, err := netip.ParseAddr(host); err == nil {
		return PublicAddress(addr)
	}
	return true
}

// readerFetch asks the reading service for a page. The connection is to the
// service, not the page, through the caller's guarded client; the page's
// final address comes back and is checked against the blocklist by the caller
// as a redirect's is.
func readerFetch(ctx context.Context, client *http.Client, reader, key, target string) (page, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reader+target, nil)
	if err != nil {
		return page{}, err
	}
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Retain-Images", "none")
	resp, err := client.Do(req)
	if err != nil {
		return page{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return page{}, fmt.Errorf("reader: HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, fetchMaxBytes+1))
	if err != nil {
		return page{}, err
	}
	if len(body) > fetchMaxBytes {
		return page{}, fmt.Errorf("reader: larger than %d MB", fetchMaxBytes>>20)
	}
	var out struct {
		Data struct {
			Title         string `json:"title"`
			URL           string `json:"url"`
			Content       string `json:"content"`
			PublishedTime string `json:"publishedTime"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return page{}, fmt.Errorf("reader: %w", err)
	}
	text := strings.TrimSpace(out.Data.Content)
	title := strings.TrimSpace(out.Data.Title)
	if text == "" {
		return page{}, fmt.Errorf("reader: no text")
	}
	// The service met the bot check too and read the check page: that is not
	// the page, and passing it on would have the model quote "Just a moment".
	if challengePage(title, text) {
		return page{}, fmt.Errorf("reader: stopped by a bot check")
	}
	final := out.Data.URL
	if final == "" {
		final = target
	}
	published := ""
	if t := out.Data.PublishedTime; len(t) >= 10 && isoDay.MatchString(t[:10]) {
		published = t[:10]
	}
	return page{url: final, title: title, published: published, text: text, via: string(ReaderFirst)}, nil
}

// challengePage is a bot check's interstitial (Cloudflare's and the like):
// a known title, or a short page that only asks to verify the visitor.
func challengePage(title, text string) bool {
	switch strings.ToLower(title) {
	case "just a moment...", "attention required! | cloudflare", "access denied", "verifying you are human":
		return true
	}
	lower := strings.ToLower(text)
	return len(text) < 1500 && (strings.Contains(lower, "verify you are human") || strings.Contains(lower, "verifying you are human") ||
		strings.Contains(lower, "enable javascript and cookies to continue") || strings.Contains(lower, "checking your browser"))
}

// PageRead is a page as the settings' test reports it.
type PageRead struct {
	URL, Title, Text string
	// Via is "jina" or "builtin": who read it.
	Via string
}

// ReadPage reads a page as web_fetch would - the mode, the key, the blocklist -
// for the administrator's test of the settings.
func (w WebFetch) ReadPage(ctx context.Context, raw string) (PageRead, error) {
	target, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (target.Scheme != "http" && target.Scheme != "https") || target.Host == "" {
		return PageRead{}, agent.Errorf("the address must be an http or https URL")
	}
	if w.Blocklist.Blocks(target.String()) {
		return PageRead{}, errBlockedSite
	}
	client := w.Client
	if client == nil {
		client = guardedClient()
	}
	got, err := w.load(ctx, client, target)
	if err != nil {
		return PageRead{}, err
	}
	if w.Blocklist.Blocks(got.url) {
		return PageRead{}, errBlockedSite
	}
	via := got.via
	if via == "" {
		via = string(ReaderOff)
	}
	return PageRead{URL: got.url, Title: got.title, Text: got.text, Via: via}, nil
}
