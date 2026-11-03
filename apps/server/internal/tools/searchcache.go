package tools

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"slices"
	"strings"
	"time"
)

// SearchStore keeps search results for a while (api: the search_cache table).
type SearchStore interface {
	GetSearch(ctx context.Context, key string) ([]byte, bool)
	PutSearch(ctx context.Context, key string, results []byte, ttl time.Duration)
}

// CachedSearch answers a search it has seen lately from the store instead of
// asking the backend again. Results are nobody's in particular - the services
// are called without cookies or anything of the user's - so one person's
// search answers another's, and the free tiers, rate-limited per server
// address, are asked less (docs/design/deep-research-plan.md §2).
type CachedSearch struct {
	Searcher
	Store SearchStore
	// Backend names the service behind Searcher, so switching backends does
	// not serve the other one's results.
	Backend string
}

// searchTTL is how long results are kept: a day, or an hour for a search
// limited to the last day, which a day-old answer would defeat.
func searchTTL(req SearchRequest) time.Duration {
	if req.Recency == "day" {
		return time.Hour
	}
	return 24 * time.Hour
}

// cachedResult is a result as stored: with the page text, which the
// transcript's JSON leaves out.
type cachedResult struct {
	Title     string `json:"title"`
	URL       string `json:"url"`
	Snippet   string `json:"snippet"`
	Content   string `json:"content,omitempty"`
	Source    string `json:"source,omitempty"`
	Published string `json:"published,omitempty"`
}

// searchKey is what makes two searches the same: the words, however spaced
// and cased, and everything that changes what comes back.
func (c CachedSearch) searchKey(req SearchRequest) string {
	sites := slices.Clone(req.Sites)
	slices.Sort(sites)
	exclude := slices.Clone(req.ExcludeDomains)
	slices.Sort(exclude)
	raw, _ := json.Marshal([]any{
		c.Backend, strings.ToLower(strings.Join(strings.Fields(req.Query), " ")),
		req.Limit, req.Depth, req.PageContent, req.Recency, sites, exclude,
	})
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func (c CachedSearch) Search(ctx context.Context, req SearchRequest) ([]SearchResult, error) {
	key := c.searchKey(req)
	if raw, ok := c.Store.GetSearch(ctx, key); ok {
		var stored []cachedResult
		if err := json.Unmarshal(raw, &stored); err == nil {
			out := make([]SearchResult, len(stored))
			for i, r := range stored {
				out[i] = SearchResult{Title: r.Title, URL: r.URL, Snippet: r.Snippet, Content: r.Content, Source: r.Source, Published: r.Published}
			}
			return out, nil
		}
	}
	results, err := c.Searcher.Search(ctx, req)
	if err != nil {
		return nil, err
	}
	stored := make([]cachedResult, len(results))
	for i, r := range results {
		stored[i] = cachedResult{Title: r.Title, URL: r.URL, Snippet: r.Snippet, Content: r.Content, Source: r.Source, Published: r.Published}
	}
	if raw, err := json.Marshal(stored); err == nil {
		c.Store.PutSearch(ctx, key, raw, searchTTL(req))
	} else {
		slog.WarnContext(ctx, "encode search results for the cache", "error", err)
	}
	return results, nil
}
