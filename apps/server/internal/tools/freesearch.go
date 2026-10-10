package tools

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"sync/atomic"
	"time"

	"github.com/qunqin/openchat/apps/server/internal/agent"
)

// FreeSearch searches through the public, keyless tiers of four search
// services — Exa, Parallel, Firecrawl and Keenable — taking them in turn. A
// service that is rate-limiting, refusing anonymous callers or failing is
// skipped for the next one, so a search fails only when all four do.
//
// The approach and the endpoints follow Hermes Agent's keyless ring
// (github.com/NousResearch/hermes-agent, plugins/web/keyless_mcp.py). These
// tiers are meant for light use: they are rate-limited per caller address, and
// the services may change or withdraw them. No user identifier is sent; the
// only correlation id is a random one per process, which Parallel asks for.
type FreeSearch struct {
	// Vendors override the four services, for tests. Nil is all four.
	Vendors []FreeVendor
	Client  *http.Client
}

// FreeVendor is one keyless search service.
type FreeVendor struct {
	Name   string
	Search func(ctx context.Context, client *http.Client, req SearchRequest) ([]SearchResult, error)
}

// freeCursor is where the next search starts in the ring, so load spreads
// across the services rather than always landing on the first.
var freeCursor atomic.Uint64

// freeSessionID is Parallel's rate-limit correlation id: random per process,
// never stored, tied to nothing about any user.
var freeSessionID = func() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}()

// FreeVendors are the services, in ring order.
func FreeVendors() []FreeVendor {
	return []FreeVendor{
		{Name: "exa", Search: exaSearch("https://mcp.exa.ai/mcp")},
		{Name: "parallel", Search: parallelSearch("https://search.parallel.ai/mcp")},
		{Name: "firecrawl", Search: firecrawlSearch("https://api.firecrawl.dev")},
		{Name: "keenable", Search: keenableSearch("https://api.keenable.ai")},
	}
}

func (f FreeSearch) Search(ctx context.Context, req SearchRequest) ([]SearchResult, error) {
	vendors := f.Vendors
	if vendors == nil {
		vendors = FreeVendors()
	}
	client := f.Client
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}

	start := int(freeCursor.Add(1)-1) % len(vendors)
	var last error
	for i := range vendors {
		vendor := vendors[(start+i)%len(vendors)]
		results, err := vendor.Search(ctx, client, req)
		if err == nil {
			for j := range results {
				results[j].Source = vendor.Name
			}
			return results, nil
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		last = err
		var vendorErr *freeError
		if !errors.As(err, &vendorErr) || !vendorErr.passOn {
			break // a malformed query fails everywhere; asking the others wastes their quota
		}
		slog.InfoContext(ctx, "free search vendor unavailable, trying the next", "vendor", vendor.Name, "error", err)
	}
	slog.WarnContext(ctx, "free search failed", "error", last)
	return nil, agent.Errorf("The free search services are busy or unavailable right now; try again shortly.")
}

// freeError is a service's failure. passOn is whether another service may
// still answer: a rate limit, an access refusal or a server fault is about
// this service, not the query.
type freeError struct {
	vendor string
	msg    string
	passOn bool
}

func (e *freeError) Error() string { return e.vendor + ": " + e.msg }

var (
	rateLimited = regexp.MustCompile(`(?i)rate.?limit|too many requests|quota|slow down`)
	sseLineEnd  = regexp.MustCompile(`\r\n|\r|\n`)
)

// httpFailure classifies a non-2xx response.
func httpFailure(vendor string, status int, body []byte) error {
	snippet := strings.TrimSpace(string(body))
	if len(snippet) > 200 {
		snippet = snippet[:200]
	}
	passOn := status == http.StatusTooManyRequests || status == 401 || status == 402 || status == 403 ||
		status >= 500 || rateLimited.MatchString(snippet)
	return &freeError{vendor: vendor, msg: fmt.Sprintf("HTTP %d: %s", status, snippet), passOn: passOn}
}

// post sends JSON and returns the body of a 2xx response.
func post(ctx context.Context, client *http.Client, vendor, url string, payload any, headers map[string]string) ([]byte, error) {
	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "OpenChat")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		// Unreachable or timed out: another service may well be reachable.
		return nil, &freeError{vendor: vendor, msg: "request failed", passOn: true}
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, &freeError{vendor: vendor, msg: "response could not be read", passOn: true}
	}
	if resp.StatusCode >= 300 {
		return nil, httpFailure(vendor, resp.StatusCode, data)
	}
	return data, nil
}

// mcpCall runs one MCP `tools/call` over Streamable HTTP and returns the first
// text content. The answer is either plain JSON or SSE `data:` lines.
func mcpCall(ctx context.Context, client *http.Client, vendor, url, tool string, args map[string]any) (string, error) {
	payload := map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "tools/call",
		"params": map[string]any{"name": tool, "arguments": args},
	}
	body, err := post(ctx, client, vendor, url, payload, map[string]string{"Accept": "application/json, text/event-stream"})
	if err != nil {
		return "", err
	}

	candidates := []string{}
	if trimmed := strings.TrimSpace(string(body)); strings.HasPrefix(trimmed, "{") {
		candidates = append(candidates, trimmed)
	}
	// Split on SSE line ends only: page text can hold U+2028 and friends.
	for _, line := range sseLineEnd.Split(string(body), -1) {
		if strings.HasPrefix(line, "data:") {
			candidates = append(candidates, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
	}
	for _, candidate := range candidates {
		var envelope struct {
			Error *struct {
				Message string `json:"message"`
			} `json:"error"`
			Result struct {
				IsError bool `json:"isError"`
				Content []struct {
					Text string `json:"text"`
				} `json:"content"`
			} `json:"result"`
		}
		if json.Unmarshal([]byte(candidate), &envelope) != nil {
			continue
		}
		if envelope.Error != nil {
			return "", &freeError{vendor: vendor, msg: envelope.Error.Message, passOn: rateLimited.MatchString(envelope.Error.Message)}
		}
		var texts []string
		for _, c := range envelope.Result.Content {
			if c.Text != "" {
				texts = append(texts, c.Text)
			}
		}
		if envelope.Result.IsError {
			msg := strings.Join(texts, " ")
			// A tool error is how Exa reports its free-tier limit.
			return "", &freeError{vendor: vendor, msg: msg, passOn: true}
		}
		if len(texts) > 0 {
			return texts[0], nil
		}
	}
	return "", &freeError{vendor: vendor, msg: "unrecognised response", passOn: true}
}

// exaSearch is Exa's hosted MCP server, whose `web_search_exa` answers in
// `---`-separated blocks of `Title:`, `URL:` and `Highlights:` lines.
func exaSearch(endpoint string) func(context.Context, *http.Client, SearchRequest) ([]SearchResult, error) {
	return func(ctx context.Context, client *http.Client, req SearchRequest) ([]SearchResult, error) {
		text, err := mcpCall(ctx, client, "exa", endpoint, "web_search_exa",
			map[string]any{"query": req.Query, "numResults": max(req.Limit, 1)})
		if err != nil {
			return nil, err
		}
		var results []SearchResult
		for _, block := range strings.Split(text, "\n---\n") {
			var r SearchResult
			var highlights []string
			inHighlights := false
			for _, line := range strings.Split(block, "\n") {
				trimmed := strings.TrimSpace(line)
				switch {
				case strings.HasPrefix(trimmed, "Title:"):
					r.Title, inHighlights = strings.TrimSpace(strings.TrimPrefix(trimmed, "Title:")), false
				case strings.HasPrefix(trimmed, "URL:"):
					r.URL, inHighlights = strings.TrimSpace(strings.TrimPrefix(trimmed, "URL:")), false
				case strings.HasPrefix(trimmed, "Published:"), strings.HasPrefix(trimmed, "Author:"):
					inHighlights = false
				case strings.HasPrefix(trimmed, "Highlights:"):
					inHighlights = true
				case inHighlights && trimmed != "":
					highlights = append(highlights, trimmed)
				}
			}
			if r.URL == "" {
				continue
			}
			r.Snippet = strings.Join(highlights, " ")
			r.Content = strings.Join(highlights, "\n")
			results = append(results, r)
			if req.Limit > 0 && len(results) == req.Limit {
				break
			}
		}
		return results, nil
	}
}

// parallelSearch is Parallel's hosted Search MCP, whose `web_search` answers
// with a JSON document of results and their excerpts.
func parallelSearch(endpoint string) func(context.Context, *http.Client, SearchRequest) ([]SearchResult, error) {
	return func(ctx context.Context, client *http.Client, req SearchRequest) ([]SearchResult, error) {
		text, err := mcpCall(ctx, client, "parallel", endpoint, "web_search", map[string]any{
			"objective":      req.Query,
			"search_queries": []string{req.Query},
			"session_id":     freeSessionID,
		})
		if err != nil {
			return nil, err
		}
		var parsed struct {
			Results []struct {
				URL      string   `json:"url"`
				Title    string   `json:"title"`
				Excerpts []string `json:"excerpts"`
			} `json:"results"`
		}
		if err := json.Unmarshal([]byte(text), &parsed); err != nil {
			return nil, &freeError{vendor: "parallel", msg: "unexpected payload", passOn: true}
		}
		var results []SearchResult
		for _, r := range parsed.Results {
			results = append(results, SearchResult{
				Title:   r.Title,
				URL:     r.URL,
				Snippet: strings.Join(r.Excerpts, " "),
				Content: strings.Join(r.Excerpts, "\n\n"),
			})
			if req.Limit > 0 && len(results) == req.Limit {
				break
			}
		}
		return results, nil
	}
}

// firecrawlSearch is Firecrawl's keyless cloud API: the ordinary search
// endpoint, called without an Authorization header.
func firecrawlSearch(base string) func(context.Context, *http.Client, SearchRequest) ([]SearchResult, error) {
	return func(ctx context.Context, client *http.Client, req SearchRequest) ([]SearchResult, error) {
		body, err := post(ctx, client, "firecrawl", base+"/v2/search",
			map[string]any{"query": req.Query, "limit": max(req.Limit, 1)}, nil)
		if err != nil {
			return nil, err
		}
		var parsed struct {
			Success bool `json:"success"`
			Data    struct {
				Web []struct {
					URL         string `json:"url"`
					Title       string `json:"title"`
					Description string `json:"description"`
				} `json:"web"`
			} `json:"data"`
		}
		if err := json.Unmarshal(body, &parsed); err != nil {
			return nil, &freeError{vendor: "firecrawl", msg: "unexpected payload", passOn: true}
		}
		var results []SearchResult
		for _, r := range parsed.Data.Web {
			results = append(results, SearchResult{Title: r.Title, URL: r.URL, Snippet: r.Description, Content: r.Description})
		}
		return results, nil
	}
}

// keenableSearch is Keenable's public search endpoint, which asks only for an
// application name.
func keenableSearch(base string) func(context.Context, *http.Client, SearchRequest) ([]SearchResult, error) {
	return func(ctx context.Context, client *http.Client, req SearchRequest) ([]SearchResult, error) {
		body, err := post(ctx, client, "keenable", base+"/v1/search/public",
			map[string]any{"query": req.Query, "max_results": max(req.Limit, 1)},
			map[string]string{"X-Keenable-Title": "openchat"})
		if err != nil {
			return nil, err
		}
		var parsed struct {
			Results []struct {
				URL         string `json:"url"`
				Title       string `json:"title"`
				Snippet     string `json:"snippet"`
				Description string `json:"description"`
			} `json:"results"`
		}
		if err := json.Unmarshal(body, &parsed); err != nil {
			return nil, &freeError{vendor: "keenable", msg: "unexpected payload", passOn: true}
		}
		var results []SearchResult
		for _, r := range parsed.Results {
			text := r.Snippet
			if text == "" {
				text = r.Description
			}
			results = append(results, SearchResult{Title: r.Title, URL: r.URL, Snippet: text, Content: text})
		}
		return results, nil
	}
}
