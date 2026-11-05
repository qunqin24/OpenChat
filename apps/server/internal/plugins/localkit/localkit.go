// Package localkit is what the built-in plugins that run inside OpenChat
// (plugins.LocalScheme) share: how a service is made, and a polite client for
// the public APIs they read.
package localkit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/qunqin24/openchat/apps/server/internal/tools"
)

// Options are what a service is made with for a session.
type Options struct {
	// Version is OpenChat's, told as the service's own.
	Version string
	// Secret is the plugin's header value as the administrator set it (an
	// API key), empty when none: a service that takes one reads it here.
	Secret string
}

// NewServer is a service with read-only tools only, as all of these are.
type NewServer func(Options) *mcp.Server

// ReadOnly marks a tool that only reads the open web.
var ReadOnly = &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: ptr(true)}

func ptr[T any](v T) *T { return &v }

// UserAgent is how the services name themselves to the APIs they read, as
// Wikimedia and others ask.
const UserAgent = "OpenChat built-in plugin (+https://github.com/qunqin24/openchat)"

// Client reads a public API: through the public-address guard, at most one
// request every Gap to it from the whole process, answers up to MaxBytes.
type Client struct {
	Gap      time.Duration
	MaxBytes int64

	once sync.Once
	http *http.Client
	mu   sync.Mutex
	last time.Time
}

// ErrNotFound is a 404: what was asked for does not exist.
var ErrNotFound = errors.New("not found")

func (c *Client) client() *http.Client {
	c.once.Do(func() { c.http = tools.GuardedClient(30 * time.Second) })
	return c.http
}

// wait keeps to Gap between requests: the next starts Gap after the last.
func (c *Client) wait(ctx context.Context) error {
	if c.Gap <= 0 {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if d := c.Gap - time.Since(c.last); d > 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(d):
		}
	}
	c.last = time.Now()
	return nil
}

// Get fetches address and returns its body; a status other than 200 is an
// error that says it (ErrNotFound for 404).
func (c *Client) Get(ctx context.Context, address string, header map[string]string) ([]byte, error) {
	if err := c.wait(ctx); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", UserAgent)
	req.Header.Set("Accept", "application/json")
	for name, value := range header {
		req.Header.Set(name, value)
	}
	resp, err := c.client().Do(req)
	if err != nil {
		return nil, fmt.Errorf("the service could not be reached: %w", err)
	}
	defer resp.Body.Close()
	limit := c.MaxBytes
	if limit <= 0 {
		limit = 8 << 20
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, fmt.Errorf("reading the answer: %w", err)
	}
	if int64(len(body)) > limit {
		return nil, errors.New("the answer is too large to read")
	}
	switch {
	case resp.StatusCode == http.StatusNotFound:
		return nil, ErrNotFound
	case resp.StatusCode == http.StatusTooManyRequests:
		return nil, errors.New("the service is limiting requests right now; try again later")
	case resp.StatusCode != http.StatusOK:
		return nil, fmt.Errorf("the service answered %s: %s", resp.Status, Cut(Collapse(string(body)), 200))
	}
	return body, nil
}

// JSON fetches address and decodes it into out.
func (c *Client) JSON(ctx context.Context, address string, header map[string]string, out any) error {
	body, err := c.Get(ctx, address, header)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("the answer could not be read: %w", err)
	}
	return nil
}

// Collapse is s on one line, its runs of space made one.
func Collapse(s string) string { return strings.Join(strings.Fields(s), " ") }

// Cut is s at most n characters, with … where it was cut.
func Cut(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n-1]) + "…"
}

var (
	paragraphTag = regexp.MustCompile(`(?i)<\s*(p|br|/p|li|/li|div|/div|pre|/pre)\b[^>]*>`)
	anyTag       = regexp.MustCompile(`<[^>]*>`)
	blankRuns    = regexp.MustCompile(`\n{3,}`)
)

// PlainText is a bit of HTML (a comment, a search snippet) as text: tags
// gone, paragraphs kept as blank lines, entities read.
func PlainText(s string) string {
	s = paragraphTag.ReplaceAllString(s, "\n")
	s = anyTag.ReplaceAllString(s, "")
	s = html.UnescapeString(s)
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimSpace(l)
	}
	return strings.TrimSpace(blankRuns.ReplaceAllString(strings.Join(lines, "\n"), "\n\n"))
}
