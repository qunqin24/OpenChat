package modellist

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode"
)

// sharedHTTP is the client every adapter uses unless a caller supplies one, so
// repeated refreshes reuse connections. It sets no timeout of its own: the
// deadline comes from the context, which a caller can shorten but not lose.
var sharedHTTP = &http.Client{}

// dialect is the part of listing that differs between vendors: which path,
// which headers, which shape of JSON, and how the endpoint pages. Everything
// else — the deadline, the byte cap, the redirect rule, the redaction — is the
// same for all three and lives in fetcher, where it cannot be forgotten by the
// next adapter.
type dialect interface {
	list(ctx context.Context, f fetcher) ([]Model, error)
}

// lister pairs the transport with one vendor's dialect.
type lister struct {
	fetcher fetcher
	dialect dialect
	timeout time.Duration
}

var _ Lister = lister{}

func newLister(cfg Config, fallbackBase string, d dialect) (Lister, error) {
	base, err := resolveBase(cfg.BaseURL, fallbackBase)
	if err != nil {
		return nil, err
	}
	return lister{
		fetcher: fetcher{
			base: base,
			key:  strings.TrimSpace(cfg.APIKey),
			http: client(cfg.HTTP),
		},
		dialect: d,
		timeout: cmp.Or(cfg.Timeout, DefaultTimeout),
	}, nil
}

// List bounds the whole listing, not one request: the deadline is set here and
// every page of a paging dialect spends the same budget. An endpoint cannot
// stretch it by answering slowly one page at a time.
func (l lister) List(ctx context.Context) ([]Model, error) {
	ctx, cancel := context.WithTimeout(ctx, l.timeout)
	defer cancel()
	return l.dialect.list(ctx, l.fetcher)
}

// fetcher performs the GETs one listing is made of.
type fetcher struct {
	base *url.URL
	key  string
	http *http.Client
}

// get issues one request under the caller's deadline and decodes the body into
// dst.
//
// path is appended to the configured base, which is why the base was stripped
// of its query and trailing slash when it was validated.
func (f fetcher) get(ctx context.Context, path string, query url.Values, header http.Header, dst any) error {
	endpoint := *f.base
	endpoint.Path += path
	endpoint.RawQuery = query.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return fmt.Errorf("%w: %s", ErrBaseURL, redactURL(endpoint.String()))
	}
	req.Header = header.Clone()
	if req.Header == nil {
		req.Header = http.Header{}
	}
	req.Header.Set("Accept", "application/json")

	resp, err := f.http.Do(req)
	if resp != nil {
		defer func() { _ = resp.Body.Close() }()
	}
	if err != nil {
		return f.requestError(err)
	}

	if resp.StatusCode != http.StatusOK {
		return f.statusError(resp)
	}

	// The cap is on the reader rather than checked afterwards, so an endpoint
	// that answers forever is cut off rather than measured.
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxBodyBytes)).Decode(dst); err != nil {
		return fmt.Errorf("%w: %v", ErrResponse, err)
	}
	return nil
}

// requestError rebuilds a transport failure into one that is safe to log.
//
// *url.Error carries the URL it failed on, and for Gemini the key lives in that
// URL's query string. Wrapping it unchanged would put the credential into every
// log line and every error string downstream, so the URL is redacted and the
// cause is re-wrapped — errors.Is still sees ErrRedirect or context.
// DeadlineExceeded through it.
func (f fetcher) requestError(err error) error {
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return fmt.Errorf("modellist: %s %s: %w", urlErr.Op, redactURL(urlErr.URL), urlErr.Err)
	}
	return fmt.Errorf("modellist: %w", err)
}

// statusError reads a short excerpt of a failed response for the log, with any
// occurrence of the key scrubbed out — some gateways echo the request back in
// their error, credential included.
func (f fetcher) statusError(resp *http.Response) error {
	excerpt, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodyBytes))
	return &StatusError{StatusCode: resp.StatusCode, Snippet: f.redactKey(oneLine(string(excerpt)))}
}

// redactKey removes the credential from text that came from somewhere else.
func (f fetcher) redactKey(text string) string {
	if f.key == "" {
		return text
	}
	return strings.ReplaceAll(text, f.key, "[redacted]")
}

// client attaches the redirect policy to a copy of the caller's client, so the
// rule applies to a test's client exactly as it does to the shared one, without
// mutating something the caller also owns.
func client(base *http.Client) *http.Client {
	if base == nil {
		base = sharedHTTP
	}
	copied := *base
	copied.CheckRedirect = refuseForeignRedirect
	return &copied
}

// refuseForeignRedirect allows a redirect only back to the host the operator
// configured.
//
// net/http replays the request's headers on the next hop, so a 302 from a
// compromised or merely sloppy endpoint is a way to deliver the operator's key
// to a host they never authorised. A redirect within the same host is
// ordinary — path normalisation, http to https — and is allowed.
func refuseForeignRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= maxRedirects {
		return fmt.Errorf("%w: stopped after %d redirects", ErrRedirect, maxRedirects)
	}

	origin := via[0].URL
	if !strings.EqualFold(req.URL.Hostname(), origin.Hostname()) ||
		req.URL.Port() != origin.Port() ||
		(req.URL.Scheme != "http" && req.URL.Scheme != "https") {
		// The hosts are named, never the URLs: one of them is ours and carries
		// the key in its query.
		return fmt.Errorf("%w: %s sent us to %s", ErrRedirect, origin.Host, req.URL.Host)
	}
	return nil
}

// redactURL strips anything secret from a URL before it can be logged: every
// query value (Gemini's key is one of them) and any userinfo.
func redactURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return "[unparseable url]"
	}
	if parsed.RawQuery != "" {
		query := parsed.Query()
		for key := range query {
			// A bare word rather than a bracketed marker: this goes back through
			// url encoding, and `%5Bredacted%5D` is harder to recognise at a
			// glance in a log than `key=redacted`.
			query.Set(key, "redacted")
		}
		parsed.RawQuery = query.Encode()
	}
	parsed.User = nil
	return parsed.String()
}

// oneLine flattens an excerpt so a multi-line HTML error page cannot spread
// itself across the log.
func oneLine(text string) string {
	return strings.TrimSpace(strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return ' '
		}
		return r
	}, text))
}

// collector gathers models across pages, keeping the first spelling of an id.
//
// De-duplicating here rather than at the database matters twice: an endpoint
// that repeats an entry across pages would otherwise make the upsert touch the
// same row twice in one statement, which Postgres refuses outright.
type collector struct {
	seen   map[string]struct{}
	models []Model
}

func (c *collector) add(id, displayName string) {
	id = strings.TrimSpace(id)
	// An entry with no id is not addressable: nothing could ever be sent to it.
	if id == "" {
		return
	}
	if c.seen == nil {
		c.seen = make(map[string]struct{})
	}
	if _, ok := c.seen[id]; ok {
		return
	}
	c.seen[id] = struct{}{}
	c.models = append(c.models, Model{ID: id, DisplayName: strings.TrimSpace(displayName)})
}

func (c *collector) result() []Model {
	if c.models == nil {
		return []Model{}
	}
	return c.models
}
