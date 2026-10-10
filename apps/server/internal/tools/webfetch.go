package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/net/html"
	"golang.org/x/net/html/charset"

	"github.com/qunqin/openchat/apps/server/internal/agent"
	"github.com/qunqin/openchat/apps/server/internal/llm"
)

const (
	fetchTimeout   = 20 * time.Second
	fetchMaxBytes  = 5 << 20
	fetchMaxChars  = 20000
	fetchRedirects = 5
)

// WebFetch is the `web_fetch` tool.
//
// The URL comes from model output, and model output is steered by whatever the
// model has read — a search result, a page, the user. That makes this a
// request-forgery primitive pointed at our own network unless every connection
// is checked. The check is on the address actually dialled, at connect time,
// for every redirect hop: resolving the name first and checking the answer is
// defeated by a name that resolves differently the second time.
type WebFetch struct {
	// Client overrides the guarded client, for tests that fetch from a local
	// server. Production code leaves it nil.
	Client *http.Client
	// Blocklist is the sites an administrator has ruled out, checked against
	// the address asked for and the one a redirect lands on.
	Blocklist Blocklist
}

var _ agent.Tool = WebFetch{}

// UsesWeb: a conversation with web access off is not offered this.
func (WebFetch) UsesWeb() bool { return true }

func (WebFetch) Definition() llm.Tool {
	return llm.Tool{
		Name: "web_fetch",
		Description: "Read a web page now and return its text. Use it to check details, read a page in full, or " +
			"confirm what a page currently says — search excerpts can be stale. Pages built entirely by " +
			"JavaScript come back empty.",
		Parameters: json.RawMessage(`{
  "type": "object",
  "properties": {
    "url": {"type": "string", "description": "The http or https address to read."}
  },
  "required": ["url"]
}`),
	}
}

func (WebFetch) Replayable() bool { return true }

// errBlockedSite is what the model is told about a site on the blocklist: that
// it is ruled out here, so it looks elsewhere rather than retrying.
var errBlockedSite = agent.Errorf("This site is blocked by the administrator of this server. Use another source.")

func (w WebFetch) Execute(ctx context.Context, raw json.RawMessage) (agent.ToolResult, error) {
	var args struct {
		URL string `json:"url"`
	}
	if err := agent.DecodeArgs(raw, &args); err != nil {
		return agent.ToolResult{}, err
	}
	target, err := url.Parse(strings.TrimSpace(args.URL))
	if err != nil || (target.Scheme != "http" && target.Scheme != "https") || target.Host == "" {
		return agent.ToolResult{}, agent.Errorf("invalid arguments: url must be an http or https address")
	}

	if w.Blocklist.Blocks(target.String()) {
		return agent.ToolResult{}, errBlockedSite
	}
	client := w.Client
	if client == nil {
		client = guardedClient()
	}
	page, err := fetch(ctx, client, target.String())
	if err != nil {
		return agent.ToolResult{}, err
	}
	if w.Blocklist.Blocks(page.url) {
		return agent.ToolResult{}, errBlockedSite
	}

	text, truncated := page.text, false
	if runes := []rune(text); len(runes) > fetchMaxChars {
		text, truncated = string(runes[:fetchMaxChars]), true
	}
	details := fetchDetails{URL: page.url, Title: page.title, Truncated: truncated, Chars: len([]rune(page.text)), Images: page.images}
	if strings.TrimSpace(text) == "" {
		return agent.ToolResult{
			Content: fmt.Sprintf("%s was fetched but has no readable text — it is probably rendered by JavaScript. Try another source.", page.url),
			IsError: true,
			Details: details,
		}, nil
	}

	var b strings.Builder
	if page.title != "" {
		fmt.Fprintf(&b, "Title: %s\n", page.title)
	}
	fmt.Fprintf(&b, "URL: %s\n\n%s", page.url, text)
	if truncated {
		fmt.Fprintf(&b, "\n\n[Truncated at %d characters.]", fetchMaxChars)
	}
	if len(page.images) > 0 {
		b.WriteString("\n\nImages on this page, the lead image first:")
		for _, image := range page.images {
			if image.Alt != "" {
				fmt.Fprintf(&b, "\n- %s (%s)", image.URL, image.Alt)
			} else {
				fmt.Fprintf(&b, "\n- %s", image.URL)
			}
		}
	}
	return agent.ToolResult{Content: b.String(), Details: details}, nil
}

type fetchDetails struct {
	URL       string      `json:"url"`
	Title     string      `json:"title,omitempty"`
	Truncated bool        `json:"truncated"`
	Chars     int         `json:"chars"`
	Images    []PageImage `json:"images,omitempty"`
}

type page struct {
	url    string
	title  string
	text   string
	images []PageImage
}

// PageImage is a picture on a fetched page: its lead image (og:image) first,
// then the larger ones in its body. They are listed to the model so it can
// show one, and only an address listed here — in a tool result of the same
// conversation — is ever fetched for the page (see the image proxy).
type PageImage struct {
	URL string `json:"url"`
	Alt string `json:"alt,omitempty"`
}

// pageImagesMax is how many images a page lists: enough for a gallery, few
// enough not to crowd the model's context.
const pageImagesMax = 6

func fetch(ctx context.Context, client *http.Client, target string) (page, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return page{}, agent.Errorf("invalid arguments: %v", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; OpenChat/1.0; +https://github.com/qunqin/openchat)")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,text/plain;q=0.9,*/*;q=0.5")

	resp, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return page{}, ctx.Err()
		}
		if errors.Is(err, errBlockedAddress) {
			return page{}, agent.Errorf("That address is not reachable from here: only public internet addresses can be fetched.")
		}
		if errors.Is(err, errTooManyRedirects) {
			return page{}, agent.Errorf("The page redirected too many times.")
		}
		return page{}, agent.Errorf("The page could not be fetched: the connection failed or timed out.")
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return page{}, agent.Errorf("The page could not be fetched (HTTP %d).", resp.StatusCode)
	}

	mediaType, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	isHTML := mediaType == "text/html" || mediaType == "application/xhtml+xml" || mediaType == ""
	isText := strings.HasPrefix(mediaType, "text/") || mediaType == "application/json"
	if !isHTML && !isText {
		return page{}, agent.Errorf("The page is %s, which cannot be read as text.", mediaType)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, fetchMaxBytes+1))
	if err != nil {
		return page{}, agent.Errorf("The page could not be read completely.")
	}
	if len(body) > fetchMaxBytes {
		return page{}, agent.Errorf("The page is larger than %d MB.", fetchMaxBytes>>20)
	}

	decoded, err := charset.NewReader(strings.NewReader(string(body)), resp.Header.Get("Content-Type"))
	if err == nil {
		if converted, err := io.ReadAll(decoded); err == nil {
			body = converted
		}
	}

	final := resp.Request.URL.String()
	if !isHTML {
		return page{url: final, text: strings.TrimSpace(string(body))}, nil
	}
	title, text := htmlText(string(body))
	return page{url: final, title: title, text: text, images: htmlImages(string(body), resp.Request.URL)}, nil
}

// ---------------------------------------------------------------- SSRF guard

var (
	errBlockedAddress   = errors.New("tools: address is not public")
	errTooManyRedirects = errors.New("tools: too many redirects")
)

// guardedClient connects only to public addresses, ignores proxy settings in
// the environment (a proxy would dial on our behalf, past the check), and
// re-checks every redirect hop by dialling it through the same guard.
func guardedClient() *http.Client { return GuardedClient(fetchTimeout) }

// GuardedClient is an HTTP client that connects only to public addresses —
// see WebFetch for why — giving up after timeout. The image proxy fetches
// through it too.
func GuardedClient(timeout time.Duration) *http.Client {
	dialer := &net.Dialer{
		Timeout: 10 * time.Second,
		Control: func(network, address string, _ syscall.RawConn) error {
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				return errBlockedAddress
			}
			addr, err := netip.ParseAddr(host)
			if err != nil || !PublicAddress(addr) {
				return errBlockedAddress
			}
			return nil
		},
	}
	transport := &http.Transport{
		Proxy:                 nil,
		DialContext:           dialer.DialContext,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 15 * time.Second,
		MaxIdleConns:          10,
		IdleConnTimeout:       30 * time.Second,
	}
	return &http.Client{
		Transport: transport,
		Timeout:   timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= fetchRedirects {
				return errTooManyRedirects
			}
			if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
				return errBlockedAddress
			}
			return nil
		},
	}
}

// Ranges that are not the public internet beyond what netip's predicates
// cover, plus the IPv6 transition ranges that embed an IPv4 address: rather
// than decode each embedding, they are refused whole. No public page needs to
// be reached through one.
var blockedPrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"), // carrier-grade NAT
	netip.MustParsePrefix("192.0.0.0/24"),  // IETF protocol assignments
	netip.MustParsePrefix("192.0.2.0/24"),  // documentation
	netip.MustParsePrefix("198.18.0.0/15"), // benchmarking
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("240.0.0.0/4"),    // reserved, and broadcast
	netip.MustParsePrefix("64:ff9b::/96"),   // NAT64
	netip.MustParsePrefix("64:ff9b:1::/48"), // local-use NAT64
	netip.MustParsePrefix("2001::/32"),      // Teredo
	netip.MustParsePrefix("2002::/16"),      // 6to4
	netip.MustParsePrefix("2001:db8::/32"),  // documentation
}

// PublicAddress reports whether an address is on the public internet.
func PublicAddress(addr netip.Addr) bool {
	// An IPv4-mapped IPv6 address (::ffff:127.0.0.1) is the IPv4 address; judge
	// it as one, or loopback walks straight through.
	addr = addr.Unmap()
	if !addr.IsValid() || !addr.IsGlobalUnicast() {
		return false
	}
	if addr.IsPrivate() || addr.IsLoopback() || addr.IsLinkLocalUnicast() || addr.IsUnspecified() {
		return false
	}
	for _, prefix := range blockedPrefixes {
		if prefix.Contains(addr) {
			return false
		}
	}
	return true
}

// ---------------------------------------------------------------- HTML to text

// skipped elements hold no reading text.
var skipped = map[string]bool{
	"script": true, "style": true, "noscript": true, "template": true, "svg": true,
	"canvas": true, "iframe": true, "head": true, "nav": true, "footer": true, "form": true,
}

// blocks end a line.
var blocks = map[string]bool{
	"p": true, "div": true, "br": true, "li": true, "tr": true, "section": true, "article": true,
	"header": true, "main": true, "aside": true, "blockquote": true, "pre": true, "table": true,
	"ul": true, "ol": true, "dl": true, "dt": true, "dd": true, "figcaption": true, "hr": true,
	"h1": true, "h2": true, "h3": true, "h4": true, "h5": true, "h6": true,
}

// htmlText extracts a page's title and its readable text, one block per line.
func htmlText(source string) (title, text string) {
	doc, err := html.Parse(strings.NewReader(source))
	if err != nil {
		return "", ""
	}

	title = findTitle(doc)

	var b strings.Builder
	var walk func(n *html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && skipped[n.Data] {
			return
		}
		if n.Type == html.TextNode {
			b.WriteString(n.Data)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
		if n.Type == html.ElementNode && blocks[n.Data] {
			b.WriteString("\n")
		}
	}
	walk(doc)

	var lines []string
	for _, line := range strings.Split(b.String(), "\n") {
		if line = strings.Join(strings.Fields(line), " "); line != "" {
			lines = append(lines, line)
		}
	}
	return title, strings.Join(lines, "\n")
}

// htmlImages lists a page's pictures: its declared lead image (og:image or
// twitter:image) and then <img> elements in the body, as absolute http(s)
// addresses. Icons, logos, tracking pixels, inline data and SVG are left out:
// none of them is a picture worth showing, and SVG can carry script.
func htmlImages(source string, base *url.URL) []PageImage {
	doc, err := html.Parse(strings.NewReader(source))
	if err != nil {
		return nil
	}
	var images []PageImage
	seen := map[string]bool{}
	add := func(raw, alt string) {
		if len(images) >= pageImagesMax {
			return
		}
		ref, err := url.Parse(strings.TrimSpace(raw))
		if err != nil || raw == "" {
			return
		}
		abs := base.ResolveReference(ref)
		if abs.Scheme != "http" && abs.Scheme != "https" {
			return
		}
		address := abs.String()
		lower := strings.ToLower(abs.Path)
		if seen[address] || len(address) > 2048 || strings.HasSuffix(lower, ".svg") || strings.HasSuffix(lower, ".ico") {
			return
		}
		for _, marker := range []string{"logo", "icon", "avatar", "sprite", "pixel", "badge", "emoji", "spacer"} {
			if strings.Contains(lower, marker) {
				return
			}
		}
		seen[address] = true
		images = append(images, PageImage{URL: address, Alt: strings.Join(strings.Fields(alt), " ")})
	}

	attr := func(n *html.Node, name string) string {
		for _, a := range n.Attr {
			if strings.EqualFold(a.Key, name) {
				return a.Val
			}
		}
		return ""
	}
	// The lead image first, wherever its <meta> is.
	var meta func(n *html.Node)
	meta = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "meta" {
			key := strings.ToLower(attr(n, "property") + attr(n, "name"))
			if key == "og:image" || key == "og:image:url" || key == "og:image:secure_url" || key == "twitter:image" {
				add(attr(n, "content"), "")
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			meta(c)
		}
	}
	meta(doc)
	var body func(n *html.Node)
	body = func(n *html.Node) {
		if n.Type == html.ElementNode && (n.Data == "nav" || n.Data == "footer" || n.Data == "header" || n.Data == "aside") {
			return
		}
		if n.Type == html.ElementNode && n.Data == "img" {
			// Small declared sizes are icons and spacers.
			if w, err := strconv.Atoi(attr(n, "width")); err == nil && w < 200 {
				return
			}
			if h, err := strconv.Atoi(attr(n, "height")); err == nil && h < 120 {
				return
			}
			src := attr(n, "src")
			// Lazy-loading pages keep the real address aside.
			for _, lazy := range []string{"data-src", "data-original", "data-lazy-src"} {
				if v := attr(n, lazy); v != "" {
					src = v
				}
			}
			add(src, attr(n, "alt"))
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			body(c)
		}
	}
	body(doc)
	return images
}

// findTitle is the first <title>, which lives in the <head> the text walk skips.
func findTitle(n *html.Node) string {
	if n.Type == html.ElementNode && n.Data == "title" {
		if n.FirstChild != nil {
			return strings.Join(strings.Fields(n.FirstChild.Data), " ")
		}
		return ""
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if title := findTitle(c); title != "" {
			return title
		}
	}
	return ""
}
