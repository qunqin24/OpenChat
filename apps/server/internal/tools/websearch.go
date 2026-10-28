// Package tools holds the tools a run may offer a model.
package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/qunqin24/openchat/apps/server/internal/agent"
	"github.com/qunqin24/openchat/apps/server/internal/llm"
)

// SearchResult is one hit, in the shape the UI renders.
type SearchResult struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Snippet string `json:"snippet"`
	// Content is the page's own text, when it was asked for. Not sent to the
	// browser: the transcript shows the excerpt.
	Content string `json:"-"`
	// Source names the service that answered, where several take turns.
	Source string `json:"-"`
}

// SearchRequest is one search, as a backend runs it.
type SearchRequest struct {
	Query string
	Limit int
	// Depth is "basic" or "advanced"; a backend without the notion ignores it.
	Depth string
	// PageContent asks for each page's text as well as the excerpt.
	PageContent bool
	// ExcludeDomains are dropped by the backend itself, where it can.
	ExcludeDomains []string
}

// Searcher is a web search backend. Tavily is the first; another is a new
// implementation of this, and nothing above it changes.
type Searcher interface {
	Search(ctx context.Context, req SearchRequest) ([]SearchResult, error)
}

// Compression is what a result gives the model.
type Compression string

const (
	// CompressSnippet sends the search service's excerpt.
	CompressSnippet Compression = "snippet"
	// CompressTruncate sends the page's text, cut to a length.
	CompressTruncate Compression = "truncate"
	// CompressNone sends the page's text whole, up to contentCeiling.
	CompressNone Compression = "none"
)

// SearchConfig is how searches run, as an administrator set it.
type SearchConfig struct {
	MaxResults     int
	Depth          string
	Compression    Compression
	TruncateLength int
	Blocklist      Blocklist
}

const (
	defaultMaxResults = 5
	// snippetLimit keeps one excerpt from crowding out the others in a context
	// window that every later request in the run pays for again.
	snippetLimit = 1200
	// contentCeiling bounds one result's page text even uncompressed: twenty
	// pages of a long article each would be a context window on their own.
	contentCeiling = fetchMaxChars
)

func (c SearchConfig) maxResults() int {
	if c.MaxResults <= 0 {
		return defaultMaxResults
	}
	return c.MaxResults
}

// Hit is a result as the model receives it.
type Hit struct {
	Title   string
	URL     string
	Content string
}

// WebSearch is the `web_search` tool.
type WebSearch struct {
	Searcher Searcher
	Config   SearchConfig
}

var _ agent.Tool = WebSearch{}

// UsesWeb: a conversation with web access off is not offered this.
func (WebSearch) UsesWeb() bool { return true }

func (w WebSearch) Definition() llm.Tool {
	max := w.Config.maxResults()
	return llm.Tool{
		Name: "web_search",
		Description: "Search the web. Use it for recent events, live data, specific facts, or anything you are " +
			"not sure of. Results are titles, links and excerpts; cite the links you rely on. Read a page in " +
			"full with web_fetch when an excerpt is not enough.",
		Parameters: json.RawMessage(fmt.Sprintf(`{
  "type": "object",
  "properties": {
    "query": {"type": "string", "description": "What to search for, phrased the way a web page would say it."},
    "max_results": {"type": "integer", "minimum": 1, "maximum": %d, "description": "How many results to return. Default %d."}
  },
  "required": ["query"]
}`, max, max)),
	}
}

// Replayable: a search reads and changes nothing, so running it again after a
// restart is harmless.
func (WebSearch) Replayable() bool { return true }

func (w WebSearch) Execute(ctx context.Context, raw json.RawMessage) (agent.ToolResult, error) {
	var args struct {
		Query      string `json:"query"`
		MaxResults int    `json:"max_results"`
	}
	if err := agent.DecodeArgs(raw, &args); err != nil {
		return agent.ToolResult{}, err
	}
	query := strings.TrimSpace(args.Query)
	if query == "" {
		return agent.ToolResult{}, agent.Errorf("invalid arguments: query is required")
	}

	results, hits, _, err := w.Run(ctx, query, args.MaxResults)
	if err != nil {
		return agent.ToolResult{}, err
	}
	if len(hits) == 0 {
		return agent.ToolResult{Content: "No results.", Details: searchDetails{Query: query, Results: []SearchResult{}}}, nil
	}

	var b strings.Builder
	for i, hit := range hits {
		fmt.Fprintf(&b, "[%d] %s\n%s\n%s\n\n", i+1, hit.Title, hit.URL, hit.Content)
	}
	return agent.ToolResult{
		Content: strings.TrimSpace(b.String()),
		Details: searchDetails{Query: query, Results: results},
	}, nil
}

// Run searches as the tool does — the configured depth, result count,
// compression and blocklist — and returns the results kept, what the model is
// given for each, and how many the blocklist removed. A limit of zero or above
// the configured count is the configured count.
func (w WebSearch) Run(ctx context.Context, query string, limit int) ([]SearchResult, []Hit, int, error) {
	max := w.Config.maxResults()
	if limit <= 0 || limit > max {
		limit = max
	}
	compression := w.Config.Compression
	if compression == "" {
		compression = CompressSnippet
	}

	found, err := w.Searcher.Search(ctx, SearchRequest{
		Query:          query,
		Limit:          limit,
		Depth:          w.Config.Depth,
		PageContent:    compression != CompressSnippet,
		ExcludeDomains: w.Config.Blocklist.Domains(),
	})
	if err != nil {
		return nil, nil, 0, err
	}

	results := make([]SearchResult, 0, len(found))
	hits := make([]Hit, 0, len(found))
	blocked := 0
	for _, r := range found {
		if w.Config.Blocklist.Blocks(r.URL) {
			blocked++
			continue
		}
		if len(results) == limit {
			break
		}
		results = append(results, r)
		hits = append(hits, Hit{Title: r.Title, URL: r.URL, Content: w.compress(r, compression)})
	}
	return results, hits, blocked, nil
}

func (w WebSearch) compress(r SearchResult, compression Compression) string {
	text := r.Content
	if strings.TrimSpace(text) == "" {
		// The service had no page text for this one; the excerpt is what there is.
		text = r.Snippet
	}
	switch compression {
	case CompressTruncate:
		limit := w.Config.TruncateLength
		if limit <= 0 {
			limit = snippetLimit
		}
		return truncateRunes(strings.TrimSpace(text), limit)
	case CompressNone:
		return truncateRunes(strings.TrimSpace(text), contentCeiling)
	default:
		return truncateRunes(r.Snippet, snippetLimit)
	}
}

type searchDetails struct {
	Query   string         `json:"query"`
	Results []SearchResult `json:"results"`
}

// Tavily is the Tavily Search API.
type Tavily struct {
	APIKey string
	// Endpoint overrides the API root, for tests.
	Endpoint string
	Client   *http.Client
}

const tavilyEndpoint = "https://api.tavily.com"

func (t Tavily) Search(ctx context.Context, search SearchRequest) ([]SearchResult, error) {
	endpoint := t.Endpoint
	if endpoint == "" {
		endpoint = tavilyEndpoint
	}
	client := t.Client
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}

	depth := search.Depth
	if depth != "advanced" {
		depth = "basic"
	}
	payload := map[string]any{
		"query":        search.Query,
		"max_results":  search.Limit,
		"search_depth": depth,
		"topic":        "general",
	}
	if search.PageContent {
		payload["include_raw_content"] = "markdown"
	}
	if len(search.ExcludeDomains) > 0 {
		payload["exclude_domains"] = search.ExcludeDomains
	}
	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint+"/search", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+t.APIKey)

	resp, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, agent.Errorf("The search service could not be reached.")
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return nil, agent.Errorf("Web search is misconfigured on this server; it cannot be used right now.")
	case resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == 432 || resp.StatusCode == 433:
		// 432 and 433 are Tavily's plan and pay-as-you-go limits.
		return nil, agent.Errorf("The search service's usage limit has been reached; try again later.")
	case resp.StatusCode != http.StatusOK:
		return nil, agent.Errorf("The search service failed (HTTP %d).", resp.StatusCode)
	}

	var parsed struct {
		Results []struct {
			Title      string `json:"title"`
			URL        string `json:"url"`
			Content    string `json:"content"`
			RawContent string `json:"raw_content"`
		} `json:"results"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 16<<20)).Decode(&parsed); err != nil {
		return nil, agent.Errorf("The search service returned an unreadable response.")
	}
	results := make([]SearchResult, 0, len(parsed.Results))
	for _, r := range parsed.Results {
		results = append(results, SearchResult{
			Title:   r.Title,
			URL:     r.URL,
			Snippet: strings.TrimSpace(r.Content),
			Content: strings.TrimSpace(r.RawContent),
		})
	}
	return results, nil
}

func truncateRunes(s string, limit int) string {
	runes := []rune(s)
	if len(runes) <= limit {
		return s
	}
	return string(runes[:limit]) + "…"
}
