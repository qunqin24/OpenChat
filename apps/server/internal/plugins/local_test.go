package plugins

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// TestLocalServicesLive calls each built-in service that runs here, as a
// conversation would: only with LOCAL_LIVE=1, as it reaches the services.
func TestLocalServicesLive(t *testing.T) {
	if os.Getenv("LOCAL_LIVE") == "" {
		t.Skip("set LOCAL_LIVE=1 to reach the public APIs")
	}
	ctx := context.Background()
	for _, c := range []struct {
		service, tool string
		args          string
		want          string
	}{
		{"openalex", "search_works", `{"query":"attention is all you need","sort":"cited","max_results":3}`, "Attention Is All You Need"},
		{"openalex", "get_work", `{"id":"arXiv:1706.03762"}`, "Vaswani"},
		{"openalex", "citing_works", `{"id":"1706.03762","max_results":2}`, `"cited_by"`},
		{"openalex", "references", `{"id":"1706.03762","max_results":3}`, `"works"`},
		{"openalex", "search_authors", `{"query":"Geoffrey Hinton","max_results":2}`, "h_index"},
		{"weather", "find_place", `{"name":"北京","language":"zh"}`, "北京"},
		{"weather", "forecast", `{"place":"上海","days":2,"hourly":true}`, "temperature_c"},
		{"weather", "forecast", `{"latitude":51.5,"longitude":-0.12,"days":1}`, "Europe/London"},
		{"weather", "history", `{"place":"Tokyo","start_date":"2025-01-01","end_date":"2025-01-03"}`, "2025-01-02"},
		{"wikipedia", "search", `{"query":"自行车","language":"zh","max_results":3}`, "自行车"},
		{"wikipedia", "read_article", `{"title":"自行車","language":"zh","intro":true}`, "自行车"},
		{"wikipedia", "read_article", `{"title":"Go (programming language)"}`, "next_offset"},
		{"hackernews", "search", `{"query":"chromedp","max_results":3}`, "Chromedp"},
		{"hackernews", "front_page", `{}`, "discussion"},
		{"hackernews", "get_story", `{"id":"https://news.ycombinator.com/item?id=8863"}`, "comments_shown"},
	} {
		conn := Open(Endpoint{URL: LocalScheme + c.service, Version: "test"})
		res, err := conn.Call(ctx, c.tool, json.RawMessage(c.args))
		conn.Close()
		if err != nil || res.IsError || !strings.Contains(res.Text, c.want) {
			t.Errorf("%s.%s: %v %v\n%.600s", c.service, c.tool, err, res.IsError, res.Text)
			continue
		}
		t.Logf("%s.%s: %.200s", c.service, c.tool, res.Text)
	}
}
