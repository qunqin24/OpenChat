// Package tools holds the tools a run may offer a model.
package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
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
	// Published is the day the page was published or last updated, as the
	// service judged it (YYYY-MM-DD); empty when it did not say.
	Published string `json:"published,omitempty"`
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
	// Recency is how far back pages may date from: "day", "week", "month",
	// "year", or empty for any time. A backend that cannot filter by date
	// asks for it in words where it takes words; dated results outside it are
	// dropped after (Run).
	Recency string
	// Sites are the only domains to search, where the model named some. A
	// backend that cannot restrict to them adds site: to the query; results
	// elsewhere are dropped after (Run).
	Sites []string
}

// recencies are the periods a search can be limited to, and how far back
// each reaches.
var recencies = map[string]time.Duration{
	"day":   24 * time.Hour,
	"week":  7 * 24 * time.Hour,
	"month": 31 * 24 * time.Hour,
	"year":  366 * 24 * time.Hour,
}

// maxSites is how many domains one search may be limited to.
const maxSites = 5

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
	Title     string
	URL       string
	Published string
	Content   string
}

// Query is one search the model asked for.
type Query struct {
	Text string
	// Limit is how many results; zero or above the configured count is the
	// configured count.
	Limit   int
	Recency string
	Sites   []string
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
			"not sure of. Results are titles, links, the date each page was published or updated when known, " +
			"and excerpts. Read a page in full with web_fetch when an excerpt is not enough.",
		Parameters: json.RawMessage(fmt.Sprintf(`{
  "type": "object",
  "properties": {
    "query": {"type": "string", "description": "What to search for, phrased the way a web page would say it. Plain words: no site: or other operators, and no quotes - use sites to limit the domains."},
    "max_results": {"type": "integer", "minimum": 1, "maximum": %d, "description": "How many results to return. Default %d."},
    "recency": {"type": "string", "enum": ["day", "week", "month", "year"], "description": "Only pages published or updated within this past period. For news, prices, releases and anything else that changes."},
    "sites": {"type": "array", "items": {"type": "string"}, "maxItems": %d, "description": "Only results from these domains, such as go.dev (its subdomains included): domains only, no paths. For official documentation, a source the user named, or one site's coverage."}
  },
  "required": ["query"]
}`, max, max, maxSites)),
	}
}

// Replayable: a search reads and changes nothing, so running it again after a
// restart is harmless.
func (WebSearch) Replayable() bool { return true }

func (w WebSearch) Execute(ctx context.Context, raw json.RawMessage) (agent.ToolResult, error) {
	var args struct {
		Query      string   `json:"query"`
		MaxResults int      `json:"max_results"`
		Recency    string   `json:"recency"`
		Sites      []string `json:"sites"`
	}
	if err := agent.DecodeArgs(raw, &args); err != nil {
		return agent.ToolResult{}, err
	}
	query := strings.TrimSpace(args.Query)
	if query == "" {
		return agent.ToolResult{}, agent.Errorf("invalid arguments: query is required")
	}
	recency := strings.ToLower(strings.TrimSpace(args.Recency))
	if _, ok := recencies[recency]; recency != "" && !ok {
		return agent.ToolResult{}, agent.Errorf("invalid arguments: recency must be day, week, month or year")
	}
	sites, err := siteDomains(args.Sites)
	if err != nil {
		return agent.ToolResult{}, err
	}

	results, hits, _, err := w.Run(ctx, Query{Text: query, Limit: args.MaxResults, Recency: recency, Sites: sites})
	if err != nil {
		return agent.ToolResult{}, err
	}
	details := searchDetails{Query: query, Recency: recency, Sites: sites, Results: results}
	if len(hits) == 0 {
		details.Results = []SearchResult{}
		return agent.ToolResult{Content: "No results.", Details: details}, nil
	}

	var b strings.Builder
	for i, hit := range hits {
		fmt.Fprintf(&b, "[%d] %s\n%s\n", i+1, hit.Title, hit.URL)
		if hit.Published != "" {
			fmt.Fprintf(&b, "Published: %s\n", hit.Published)
		} else if recency != "" {
			// Kept, as the service could not date it: the model should know.
			b.WriteString("Published: unknown\n")
		}
		fmt.Fprintf(&b, "%s\n\n", hit.Content)
	}
	return agent.ToolResult{Content: strings.TrimSpace(b.String()), Details: details}, nil
}

// siteDomains are the domains a search is limited to, as bare host names: the
// model may write a site as an address.
func siteDomains(raw []string) ([]string, error) {
	var sites []string
	for _, site := range raw {
		site = strings.ToLower(strings.TrimSpace(site))
		if site == "" {
			continue
		}
		if !strings.Contains(site, "://") {
			site = "https://" + site
		}
		host := ""
		if parsed, err := url.Parse(site); err == nil {
			host = strings.TrimPrefix(parsed.Hostname(), "www.")
		}
		if host == "" || !strings.Contains(host, ".") {
			return nil, agent.Errorf("invalid arguments: %q is not a domain", strings.TrimSpace(site))
		}
		if !slices.Contains(sites, host) {
			sites = append(sites, host)
		}
	}
	if len(sites) > maxSites {
		return nil, agent.Errorf("invalid arguments: at most %d sites", maxSites)
	}
	return sites, nil
}

// onSite is whether an address is on one of the sites or a subdomain of one.
func onSite(address string, sites []string) bool {
	parsed, err := url.Parse(address)
	if err != nil {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	for _, site := range sites {
		if host == site || strings.HasSuffix(host, "."+site) {
			return true
		}
	}
	return false
}

// publishedDay is a service's date for a page as YYYY-MM-DD, whichever of
// the usual forms it came in; empty when it is missing or unreadable.
func publishedDay(raw string) string {
	raw = strings.TrimSpace(raw)
	for _, layout := range []string{
		"2006-01-02", time.RFC3339, time.RFC3339Nano, "2006-01-02T15:04:05", "2006-01-02 15:04:05",
		time.RFC1123, time.RFC1123Z, "Mon, 2 Jan 2006 15:04:05 MST", "Mon, 2 Jan 2006 15:04:05 -0700",
	} {
		if t, err := time.Parse(layout, raw); err == nil {
			return t.UTC().Format("2006-01-02")
		}
	}
	return ""
}

// inWords is a search as a sentence, for a service that takes an objective
// in words rather than filters.
func (r SearchRequest) inWords() string {
	text := "Find: " + r.Query + "."
	if r.Recency != "" {
		text += " Only pages published or updated within the past " + r.Recency + "."
	}
	if len(r.Sites) > 0 {
		text += " Only pages from " + strings.Join(r.Sites, ", ") + "."
	}
	return text
}

// withSites is the query with site: for each site, for a service that takes
// search-engine operators rather than a domain filter.
func (r SearchRequest) withSites() string {
	switch len(r.Sites) {
	case 0:
		return r.Query
	case 1:
		return r.Query + " site:" + r.Sites[0]
	}
	terms := make([]string, len(r.Sites))
	for i, site := range r.Sites {
		terms[i] = "site:" + site
	}
	return r.Query + " (" + strings.Join(terms, " OR ") + ")"
}

// Run searches as the tool does — the configured depth, result count,
// compression and blocklist, and the query's period and sites — and returns
// the results kept, what the model is given for each, and how many the
// blocklist removed.
//
// The period and the sites are applied twice: by the service, as far as it
// can (SearchRequest), and here, on what comes back. A result off the sites
// goes; one dated before the period goes; one with no date stays, marked as
// undated, since a service that cannot date a page may still have filtered
// by its date. With sites, the service is asked for twice as many, as some of
// what a service without a domain filter returns will go.
func (w WebSearch) Run(ctx context.Context, q Query) ([]SearchResult, []Hit, int, error) {
	max := w.Config.maxResults()
	limit := q.Limit
	if limit <= 0 || limit > max {
		limit = max
	}
	compression := w.Config.Compression
	if compression == "" {
		compression = CompressSnippet
	}
	asked := limit
	if len(q.Sites) > 0 {
		asked = min(2*limit, 20)
	}

	found, err := w.Searcher.Search(ctx, SearchRequest{
		Query:          q.Text,
		Limit:          asked,
		Depth:          w.Config.Depth,
		PageContent:    compression != CompressSnippet,
		ExcludeDomains: w.Config.Blocklist.Domains(),
		Recency:        q.Recency,
		Sites:          q.Sites,
	})
	if err != nil {
		return nil, nil, 0, err
	}

	var oldest string
	if period, ok := recencies[q.Recency]; ok {
		oldest = time.Now().Add(-period).UTC().Format("2006-01-02")
	}
	results := make([]SearchResult, 0, len(found))
	hits := make([]Hit, 0, len(found))
	blocked := 0
	for _, r := range found {
		if w.Config.Blocklist.Blocks(r.URL) {
			blocked++
			continue
		}
		if len(q.Sites) > 0 && !onSite(r.URL, q.Sites) {
			continue
		}
		r.Published = publishedDay(r.Published)
		if oldest != "" && r.Published != "" && r.Published < oldest {
			continue
		}
		if len(results) == limit {
			break
		}
		results = append(results, r)
		hits = append(hits, Hit{Title: r.Title, URL: r.URL, Published: r.Published, Content: w.compress(r, compression)})
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
	Recency string         `json:"recency,omitempty"`
	Sites   []string       `json:"sites,omitempty"`
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
	// Dates on every result, so the model can tell old from new; a date
	// window when one was asked for (pages with no date stay: Run marks them).
	payload["include_published_date"] = true
	if search.Recency != "" {
		payload["time_range"] = search.Recency
	}
	if len(search.Sites) > 0 {
		payload["include_domains"] = search.Sites
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
			Title         string `json:"title"`
			URL           string `json:"url"`
			Content       string `json:"content"`
			RawContent    string `json:"raw_content"`
			PublishedDate string `json:"published_date"`
		} `json:"results"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 16<<20)).Decode(&parsed); err != nil {
		return nil, agent.Errorf("The search service returned an unreadable response.")
	}
	results := make([]SearchResult, 0, len(parsed.Results))
	for _, r := range parsed.Results {
		results = append(results, SearchResult{
			Title:     r.Title,
			URL:       r.URL,
			Snippet:   strings.TrimSpace(r.Content),
			Content:   strings.TrimSpace(r.RawContent),
			Published: r.PublishedDate,
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
