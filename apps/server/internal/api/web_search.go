package api

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/qunqin24/openchat/apps/server/internal/agent"
	"github.com/qunqin24/openchat/apps/server/internal/auth"
	"github.com/qunqin24/openchat/apps/server/internal/db/sqlc"
	"github.com/qunqin24/openchat/apps/server/internal/httpx"
	"github.com/qunqin24/openchat/apps/server/internal/tools"
)

// settingWebSearch holds how searches run, as JSON. The API key lives apart,
// under settingWebSearchKey, so a read of these never carries it.
const settingWebSearch = "web_search"

// settingWebSearchKey holds the search service's API key. Write-only through
// the API, like a channel's key: reads return whether one is set and its last
// four characters.
const settingWebSearchKey = "web_search_key"

// webSearchConfig is the stored form of the settings.
type webSearchConfig struct {
	Enabled        bool     `json:"enabled"`
	Provider       string   `json:"provider"`
	MaxResults     int      `json:"maxResults"`
	Depth          string   `json:"depth"`
	Compression    string   `json:"compression"`
	TruncateLength int      `json:"truncateLength"`
	Blocklist      []string `json:"blocklist"`
}

// defaultWebSearch is what an installation starts with — and what it had
// before these settings existed: search on as soon as a key is stored, five
// excerpts a search.
func defaultWebSearch() webSearchConfig {
	return webSearchConfig{
		Enabled:        true,
		Provider:       string(Tavily),
		MaxResults:     5,
		Depth:          string(Basic),
		Compression:    string(WebSearchCompressionSnippet),
		TruncateLength: 2000,
		Blocklist:      []string{},
	}
}

func loadWebSearch(ctx context.Context, q *sqlc.Queries) (webSearchConfig, string, error) {
	config := defaultWebSearch()
	raw, err := q.GetSetting(ctx, settingWebSearch)
	switch {
	case err == nil:
		// Over the defaults, so a field added later reads as its default in
		// settings saved before it existed.
		if err := json.Unmarshal(raw, &config); err != nil {
			return config, "", err
		}
	case !errors.Is(err, pgx.ErrNoRows):
		return config, "", err
	}
	key, err := stringSetting(ctx, q, settingWebSearchKey)
	return config, key, err
}

// searchConfig is the tools' view of the settings. The blocklist was checked
// when it was saved; a rule that no longer parses drops the list rather than
// the tool.
func (c webSearchConfig) searchConfig() tools.SearchConfig {
	blocklist, err := tools.ParseBlocklist(c.Blocklist)
	if err != nil {
		slog.Warn("web search blocklist does not parse", "error", err)
	}
	return tools.SearchConfig{
		MaxResults:     c.MaxResults,
		Depth:          c.Depth,
		Compression:    tools.Compression(c.Compression),
		TruncateLength: c.TruncateLength,
		Blocklist:      blocklist,
	}
}

// searcher is the configured search service, or nil when it cannot search:
// Tavily without a key. The free search needs none.
func (c webSearchConfig) searcher(key string) tools.Searcher {
	switch c.Provider {
	case string(Free):
		return tools.FreeSearch{}
	default:
		if key == "" {
			return nil
		}
		return tools.Tavily{APIKey: key}
	}
}

func toWebSearchSettings(c webSearchConfig, key string) WebSearchSettings {
	out := WebSearchSettings{
		Enabled:        c.Enabled,
		Provider:       WebSearchProvider(c.Provider),
		KeySet:         key != "",
		MaxResults:     c.MaxResults,
		Depth:          WebSearchDepth(c.Depth),
		Compression:    WebSearchCompression(c.Compression),
		TruncateLength: c.TruncateLength,
		Blocklist:      c.Blocklist,
	}
	if out.Blocklist == nil {
		out.Blocklist = []string{}
	}
	if preview := keyPreview(key); preview != "" {
		out.KeyPreview = &preview
	}
	return out
}

func (s *Server) GetWebSearchSettings(w http.ResponseWriter, r *http.Request) {
	config, key, err := loadWebSearch(r.Context(), s.q)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, toWebSearchSettings(config, key))
}

func (s *Server) UpdateWebSearchSettings(w http.ResponseWriter, r *http.Request) {
	var body UpdateWebSearchSettingsRequest
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	ctx := r.Context()
	config, _, err := loadWebSearch(ctx, s.q)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	invalid := func(code, message string) {
		httpx.Fail(w, r, httpx.NewError(http.StatusBadRequest, code, message))
	}

	if body.Enabled != nil {
		config.Enabled = *body.Enabled
	}
	if body.Provider != nil {
		if !body.Provider.Valid() {
			invalid("invalid_provider", "unknown search provider")
			return
		}
		config.Provider = string(*body.Provider)
	}
	if body.MaxResults != nil {
		if *body.MaxResults < 1 || *body.MaxResults > 20 {
			invalid("invalid_max_results", "results per search must be between 1 and 20")
			return
		}
		config.MaxResults = *body.MaxResults
	}
	if body.Depth != nil {
		if !body.Depth.Valid() {
			invalid("invalid_depth", "unknown search depth")
			return
		}
		config.Depth = string(*body.Depth)
	}
	if body.Compression != nil {
		if !body.Compression.Valid() {
			invalid("invalid_compression", "unknown compression")
			return
		}
		config.Compression = string(*body.Compression)
	}
	if body.TruncateLength != nil {
		if *body.TruncateLength < 200 || *body.TruncateLength > 20000 {
			invalid("invalid_truncate_length", "the length must be between 200 and 20000 characters")
			return
		}
		config.TruncateLength = *body.TruncateLength
	}
	if body.Blocklist != nil {
		rules := make([]string, 0, len(*body.Blocklist))
		seen := map[string]bool{}
		for _, rule := range *body.Blocklist {
			rule = strings.TrimSpace(rule)
			if rule == "" || seen[rule] {
				continue
			}
			seen[rule] = true
			rules = append(rules, rule)
		}
		if _, err := tools.ParseBlocklist(rules); err != nil {
			invalid("invalid_blocklist", err.Error())
			return
		}
		config.Blocklist = rules
	}

	value, err := json.Marshal(config)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	if err := s.q.SetSetting(ctx, sqlc.SetSettingParams{Key: settingWebSearch, Value: value}); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	if body.ApiKey != nil {
		key, _ := json.Marshal(strings.TrimSpace(*body.ApiKey))
		if err := s.q.SetSetting(ctx, sqlc.SetSettingParams{Key: settingWebSearchKey, Value: key}); err != nil {
			httpx.Fail(w, r, err)
			return
		}
	}
	// Never the key itself, not even at debug level.
	slog.InfoContext(ctx, "web search settings changed",
		"enabled", config.Enabled, "key_changed", body.ApiKey != nil, "actor", auth.MustUser(ctx).Email)

	config, key, err := loadWebSearch(ctx, s.q)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, toWebSearchSettings(config, key))
}

// testSearchQuery is searched when the test names nothing: recent and general,
// so a working setup always finds something.
const testSearchQuery = "latest technology news"

func (s *Server) TestWebSearch(w http.ResponseWriter, r *http.Request) {
	var body WebSearchTestRequest
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	config, key, err := loadWebSearch(r.Context(), s.q)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	if body.ApiKey != nil && strings.TrimSpace(*body.ApiKey) != "" {
		key = strings.TrimSpace(*body.ApiKey)
	}
	searcher := config.searcher(key)
	if searcher == nil {
		message := "No API key is stored."
		httpx.JSON(w, http.StatusOK, WebSearchTestResult{Error: &message, Results: []WebSearchTestHit{}})
		return
	}
	query := testSearchQuery
	if body.Query != nil && strings.TrimSpace(*body.Query) != "" {
		query = strings.TrimSpace(*body.Query)
	}

	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	search := tools.WebSearch{Searcher: searcher, Config: config.searchConfig()}
	started := time.Now()
	results, hits, blocked, err := search.Run(ctx, query, 0)
	result := WebSearchTestResult{
		LatencyMs: time.Since(started).Milliseconds(),
		Results:   make([]WebSearchTestHit, 0, len(hits)),
		Blocked:   &blocked,
	}
	if err != nil {
		var toolErr *agent.ToolError
		message := "The search failed."
		if errors.As(err, &toolErr) {
			message = toolErr.Message
		}
		result.Error = &message
		httpx.JSON(w, http.StatusOK, result)
		return
	}
	result.Ok = true
	if len(results) > 0 && results[0].Source != "" {
		result.ServedBy = &results[0].Source
	}
	for _, hit := range hits {
		result.Results = append(result.Results, WebSearchTestHit{Title: hit.Title, Url: hit.URL, Content: hit.Content})
	}
	httpx.JSON(w, http.StatusOK, result)
}
