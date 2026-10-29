package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// A period and sites reach Tavily as its own filters, and what comes back is
// held to them again: a result off the sites, or dated before the period,
// goes; an undated one stays, and says so.
func TestWebSearchKeepsToThePeriodAndTheSites(t *testing.T) {
	recent := time.Now().Add(-48 * time.Hour).UTC().Format("2006-01-02")
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&body)
		_ = json.NewEncoder(w).Encode(map[string]any{"results": []any{
			map[string]any{"title": "New", "url": "https://go.dev/blog/new", "content": "new", "published_date": recent + "T08:00:00Z"},
			map[string]any{"title": "Old", "url": "https://go.dev/blog/old", "content": "old", "published_date": "2019-01-01"},
			map[string]any{"title": "Elsewhere", "url": "https://example.com/go", "content": "elsewhere", "published_date": recent},
			map[string]any{"title": "Undated", "url": "https://pkg.go.dev/x", "content": "undated"},
		}})
	}))
	defer srv.Close()
	tool := WebSearch{Searcher: Tavily{APIKey: "k", Endpoint: srv.URL, Client: srv.Client()}, Config: SearchConfig{MaxResults: 5}}

	out, err := tool.Execute(context.Background(), json.RawMessage(`{"query":"go release","recency":"week","sites":["https://www.go.dev/doc"]}`))
	if err != nil {
		t.Fatal(err)
	}
	if body["time_range"] != "week" || body["include_published_date"] != true {
		t.Errorf("the period did not reach the service: %v", body)
	}
	if domains, _ := body["include_domains"].([]any); len(domains) != 1 || domains[0] != "go.dev" {
		t.Errorf("the sites did not reach the service: %v", body["include_domains"])
	}
	for _, want := range []string{"New", "Published: " + recent, "Undated", "Published: unknown"} {
		if !strings.Contains(out.Content, want) {
			t.Errorf("lacks %q:\n%s", want, out.Content)
		}
	}
	for _, gone := range []string{"Old", "Elsewhere"} {
		if strings.Contains(out.Content, gone) {
			t.Errorf("kept %q:\n%s", gone, out.Content)
		}
	}

	for _, bad := range []string{`{"query":"x","recency":"decade"}`, `{"query":"x","sites":["not a domain"]}`, `{"query":"x","sites":["a.com","b.com","c.com","d.com","e.com","f.com"]}`} {
		if _, err := tool.Execute(context.Background(), json.RawMessage(bad)); err == nil {
			t.Errorf("accepted %s", bad)
		}
	}
}

// The free services that take no filters are asked in their own terms: the
// period and the sites in words for those that take an objective, site: in
// the query for those that take operators, Google's time filter for
// Firecrawl.
func TestFreeVendorsAskForThePeriodAndTheSites(t *testing.T) {
	bodies := map[string]map[string]any{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		bodies[r.URL.Path] = body
		switch r.URL.Path {
		case "/exa":
			text := "Title: A\nURL: https://go.dev/a\nPublished: 2026-10-20T00:00:00.000Z\nHighlights:\nA."
			envelope, _ := json.Marshal(map[string]any{"result": map[string]any{"content": []any{map[string]any{"type": "text", "text": text}}}})
			_, _ = w.Write(envelope)
		case "/parallel":
			inner, _ := json.Marshal(map[string]any{"results": []any{map[string]any{"url": "https://go.dev/p", "title": "P", "publish_date": "2026-10-21", "excerpts": []string{"p"}}}})
			envelope, _ := json.Marshal(map[string]any{"result": map[string]any{"content": []any{map[string]any{"type": "text", "text": string(inner)}}}})
			_, _ = w.Write(envelope)
		case "/v2/search":
			_, _ = w.Write([]byte(`{"success":true,"data":{"web":[]}}`))
		case "/v1/search/public":
			_, _ = w.Write([]byte(`{"results":[]}`))
		}
	}))
	defer srv.Close()
	ctx, client := context.Background(), srv.Client()
	req := SearchRequest{Query: "go release", Limit: 5, Recency: "month", Sites: []string{"go.dev", "golang.org"}}

	exa, _ := exaSearch(srv.URL+"/exa")(ctx, client, req)
	parallel, _ := parallelSearch(srv.URL+"/parallel")(ctx, client, req)
	_, _ = firecrawlSearch(srv.URL)(ctx, client, req)
	_, _ = keenableSearch(srv.URL)(ctx, client, req)

	args := func(path string) map[string]any {
		params, _ := bodies[path]["params"].(map[string]any)
		arguments, _ := params["arguments"].(map[string]any)
		return arguments
	}
	if objective, _ := args("/exa")["objective"].(string); !strings.Contains(objective, "past month") || !strings.Contains(objective, "go.dev, golang.org") {
		t.Errorf("exa objective: %q", objective)
	}
	if queries, _ := args("/parallel")["search_queries"].([]any); len(queries) != 2 || queries[1] != "go release site:golang.org" {
		t.Errorf("parallel queries: %v", queries)
	}
	if bodies["/v2/search"]["tbs"] != "qdr:m" || bodies["/v2/search"]["query"] != "go release (site:go.dev OR site:golang.org)" {
		t.Errorf("firecrawl: %v", bodies["/v2/search"])
	}
	if bodies["/v1/search/public"]["query"] != "go release (site:go.dev OR site:golang.org)" {
		t.Errorf("keenable: %v", bodies["/v1/search/public"])
	}
	if len(exa) != 1 || publishedDay(exa[0].Published) != "2026-10-20" || len(parallel) != 1 || parallel[0].Published != "2026-10-21" {
		t.Errorf("dates: %+v %+v", exa, parallel)
	}
}

func TestPublishedDay(t *testing.T) {
	for in, want := range map[string]string{
		"2026-10-21":                    "2026-10-21",
		"2026-10-20T00:00:00.000Z":      "2026-10-20",
		"Thu, 24 Oct 2024 15:04:05 GMT": "2024-10-24",
		"N/A":                           "",
		"":                              "",
	} {
		if got := publishedDay(in); got != want {
			t.Errorf("%q: %q, want %q", in, got, want)
		}
	}
}
