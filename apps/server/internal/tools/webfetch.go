package tools

import (
	"bytes"
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
	"path"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/net/html"
	"golang.org/x/net/html/charset"

	"github.com/qunqin24/openchat/apps/server/internal/agent"
	"github.com/qunqin24/openchat/apps/server/internal/doctext"
	"github.com/qunqin24/openchat/apps/server/internal/llm"
)

const (
	fetchTimeout  = 20 * time.Second
	fetchMaxBytes = 5 << 20
	// pdfMaxBytes is larger: a paper or a report with figures is often more.
	pdfMaxBytes = 20 << 20
	// fetchMaxChars is how much of a page one read shows; a longer one is
	// read on from an offset (webpage.go).
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
		Description: "Read a web page or PDF now. Returns the page's main content as Markdown (headings, lists, " +
			"tables, links), without its menus and footers. Use it to check details, read a page in full, or " +
			"confirm what a page currently says — search excerpts can be stale. A long page comes in parts: " +
			"the result says where it stopped, and offset reads on from there. find returns only the passages " +
			"that mention something, each with its offset. Pages built entirely by JavaScript come back empty.",
		Parameters: json.RawMessage(`{
  "type": "object",
  "properties": {
    "url": {"type": "string", "description": "The http or https address to read."},
    "offset": {"type": "integer", "minimum": 0, "description": "Where to start reading, in characters, as a previous result gave it. Default 0."},
    "find": {"type": "string", "description": "A word or phrase to look for (ignoring case): returns the passages around it, from offset on, instead of reading in order."}
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
		URL    string `json:"url"`
		Offset int    `json:"offset"`
		Find   string `json:"find"`
	}
	if err := agent.DecodeArgs(raw, &args); err != nil {
		return agent.ToolResult{}, err
	}
	target, err := url.Parse(strings.TrimSpace(args.URL))
	if err != nil || (target.Scheme != "http" && target.Scheme != "https") || target.Host == "" {
		return agent.ToolResult{}, agent.Errorf("invalid arguments: url must be an http or https address")
	}
	if args.Offset < 0 {
		return agent.ToolResult{}, agent.Errorf("invalid arguments: offset must not be negative")
	}

	if w.Blocklist.Blocks(target.String()) {
		return agent.ToolResult{}, errBlockedSite
	}
	page, ok := recentPages.get(target.String())
	if !ok {
		client := w.Client
		if client == nil {
			client = guardedClient()
		}
		if page, err = fetch(ctx, client, target.String()); err != nil {
			return agent.ToolResult{}, err
		}
		recentPages.put(target.String(), page)
	}
	if w.Blocklist.Blocks(page.url) {
		return agent.ToolResult{}, errBlockedSite
	}

	text := []rune(page.text)
	details := fetchDetails{URL: page.url, Title: page.title, Chars: len(text), Images: page.images, Pages: page.pages, Offset: args.Offset}
	if strings.TrimSpace(page.text) == "" {
		return agent.ToolResult{
			Content: fmt.Sprintf("%s was fetched but has no readable text — it is probably rendered by JavaScript. Try another source.", page.url),
			IsError: true,
			Details: details,
		}, nil
	}
	if args.Offset >= len(text) {
		return agent.ToolResult{}, agent.Errorf("offset %d is past the end: the page has %d characters.", args.Offset, len(text))
	}

	var b strings.Builder
	if page.title != "" {
		fmt.Fprintf(&b, "Title: %s\n", page.title)
	}
	fmt.Fprintf(&b, "URL: %s\n", page.url)
	if page.published != "" {
		fmt.Fprintf(&b, "Published: %s\n", page.published)
	}
	if page.pages > 0 {
		fmt.Fprintf(&b, "PDF, %d %s; each page begins with a [Page n] line.\n", page.pages, plural(page.pages, "page", "pages"))
	}
	b.WriteString("\n")

	if find := strings.TrimSpace(args.Find); find != "" {
		passages, matches, next := findIn(text, find, args.Offset)
		details.Find, details.Matches = find, matches
		if matches == 0 {
			fmt.Fprintf(&b, "%q does not occur in the page's %d characters", find, len(text))
			if args.Offset > 0 {
				fmt.Fprintf(&b, " from offset %d on", args.Offset)
			}
			b.WriteString(".")
			return agent.ToolResult{Content: b.String(), Details: details}, nil
		}
		fmt.Fprintf(&b, "%d %s of %q in the page's %d characters", matches, plural(matches, "match", "matches"), find, len(text))
		if args.Offset > 0 {
			fmt.Fprintf(&b, " from offset %d on", args.Offset)
		}
		b.WriteString(". Each passage starts at the offset given; read on from one with offset.")
		if next >= 0 {
			fmt.Fprintf(&b, " Not all are shown: find again with offset %d for the rest.", next)
		}
		for _, p := range passages {
			fmt.Fprintf(&b, "\n\n[offset %d]\n%s", p.start, strings.TrimSpace(string(text[p.start:p.end])))
		}
		return agent.ToolResult{Content: b.String(), Details: details}, nil
	}

	start, end := window(text, args.Offset)
	details.Offset, details.End, details.Truncated = start, end, end < len(text)
	b.WriteString(string(text[start:end]))
	if start > 0 || end < len(text) {
		fmt.Fprintf(&b, "\n\n[Characters %d–%d of %d.", start, end, len(text))
		if end < len(text) {
			fmt.Fprintf(&b, " To read on, call web_fetch with offset %d; to go straight to a passage, use find.", end)
		}
		b.WriteString("]")
	}
	// The pictures belong to the page, not to a part of it: listed once, with
	// its start.
	if start == 0 && len(page.images) > 0 {
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

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// fetchDetails is what the transcript shows of a read: the page, which part of
// it, or what was looked for in it.
type fetchDetails struct {
	URL   string `json:"url"`
	Title string `json:"title,omitempty"`
	// Truncated is a read that stopped before the page's end.
	Truncated bool `json:"truncated"`
	// Chars is the page's length; Offset and End, the part read.
	Chars  int `json:"chars"`
	Offset int `json:"offset,omitempty"`
	End    int `json:"end,omitempty"`
	// Find and Matches are a find's phrase and how often it occurs.
	Find    string      `json:"find,omitempty"`
	Matches int         `json:"matches,omitempty"`
	Pages   int         `json:"pages,omitempty"`
	Images  []PageImage `json:"images,omitempty"`
}

type page struct {
	url       string
	title     string
	published string
	text      string
	// pages is a PDF's page count; 0 for a web page.
	pages  int
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
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; OpenChat/1.0; +https://github.com/qunqin24/openchat)")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/pdf,text/plain;q=0.9,*/*;q=0.5")

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
	// Some servers send a PDF as bytes of no particular kind; its first bytes
	// say what it is.
	maybePDF := mediaType == doctext.PDFMediaType || mediaType == "application/octet-stream" || mediaType == "binary/octet-stream"
	if !isHTML && !isText && !maybePDF {
		return page{}, agent.Errorf("The page is %s, which cannot be read as text.", mediaType)
	}
	limit := int64(fetchMaxBytes)
	if maybePDF {
		limit = pdfMaxBytes
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return page{}, agent.Errorf("The page could not be read completely.")
	}
	if int64(len(body)) > limit {
		return page{}, agent.Errorf("The page is larger than %d MB.", limit>>20)
	}
	final := resp.Request.URL.String()
	if bytes.HasPrefix(body, []byte("%PDF-")) {
		return pdfPage(ctx, final, body)
	}
	if maybePDF {
		return page{}, agent.Errorf("The page is %s, which cannot be read as text.", mediaType)
	}

	decoded, err := charset.NewReader(strings.NewReader(string(body)), resp.Header.Get("Content-Type"))
	if err == nil {
		if converted, err := io.ReadAll(decoded); err == nil {
			body = converted
		}
	}

	if !isHTML {
		return page{url: final, text: strings.TrimSpace(string(body))}, nil
	}
	doc, err := html.Parse(strings.NewReader(string(body)))
	if err != nil {
		return page{}, agent.Errorf("The page could not be read.")
	}
	title, published, text := pageMarkdown(doc, resp.Request.URL)
	return page{url: final, title: title, published: published, text: text, images: htmlImages(doc, resp.Request.URL)}, nil
}

// pdfPage reads a PDF's text as an attached one is read (doctext): page by
// page, each after a "[Page n]" line.
func pdfPage(ctx context.Context, address string, data []byte) (page, error) {
	text, err := doctext.Extract(ctx, doctext.PDFMediaType, data)
	switch {
	case errors.Is(err, doctext.ErrNoReader):
		return page{}, agent.Errorf("This server cannot read PDFs. Try another source.")
	case errors.Is(err, doctext.ErrTooLong):
		return page{}, agent.Errorf("The PDF's text is too long to read here. Try another source.")
	case err != nil:
		if ctx.Err() != nil {
			return page{}, ctx.Err()
		}
		return page{}, agent.Errorf("The PDF could not be read.")
	}
	if text.Scanned {
		return page{}, agent.Errorf("The PDF is scanned pages with no text in them, which cannot be read here. Try another source.")
	}
	title := ""
	if parsed, err := url.Parse(address); err == nil {
		if name, err := url.PathUnescape(path.Base(parsed.Path)); err == nil && name != "/" && name != "." {
			title = name
		}
	}
	return page{url: address, title: title, text: tidyText(text.Content), pages: text.Pages}, nil
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

// ---------------------------------------------------------------- pictures

// htmlImages lists a page's pictures: its declared lead image (og:image or
// twitter:image) and then <img> elements in the body, as absolute http(s)
// addresses. Icons, logos, tracking pixels, inline data and SVG are left out:
// none of them is a picture worth showing, and SVG can carry script.
func htmlImages(doc *html.Node, base *url.URL) []PageImage {
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

// findTitle is the first <title>.
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
