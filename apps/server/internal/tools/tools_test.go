package tools

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"

	"github.com/qunqin/openchat/apps/server/internal/agent"
)

func TestPublicAddress(t *testing.T) {
	for addr, want := range map[string]bool{
		"8.8.8.8":              true,
		"2606:4700:4700::1111": true,
		"127.0.0.1":            false,
		"10.1.2.3":             false,
		"172.16.0.1":           false,
		"192.168.1.1":          false,
		"169.254.169.254":      false, // cloud metadata
		"100.64.0.1":           false,
		"0.0.0.0":              false,
		"::1":                  false,
		"::ffff:127.0.0.1":     false, // mapped loopback
		"::ffff:7f00:1":        false, // the same, as new(URL) spells it
		"fc00::1":              false,
		"fe80::1":              false,
		"2002:7f00:1::":        false, // 6to4 around loopback
		"64:ff9b::7f00:1":      false, // NAT64 around loopback
		"224.0.0.1":            false,
		"255.255.255.255":      false,
	} {
		if got := PublicAddress(netip.MustParseAddr(addr)); got != want {
			t.Errorf("PublicAddress(%s) = %v, want %v", addr, got, want)
		}
	}
}

func TestWebFetchRefusesLocalAddresses(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("secret"))
	}))
	defer srv.Close()

	_, err := WebFetch{}.Execute(context.Background(), json.RawMessage(`{"url":"`+srv.URL+`"}`))
	var toolErr *agent.ToolError
	if !errors.As(err, &toolErr) || !strings.Contains(toolErr.Message, "public") {
		t.Fatalf("fetching a loopback server: %v", err)
	}
}

func TestWebFetchRefusesRedirectsToLocalAddresses(t *testing.T) {
	// A public-looking first hop is not enough: the guard runs on every dial.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://127.0.0.1:1/", http.StatusFound)
	}))
	defer srv.Close()
	client := guardedClient()
	// Let the first hop through, as if it were public, to reach the redirect.
	client.Transport = &firstHopOpen{guarded: client.Transport}
	_, err := WebFetch{Client: client}.Execute(context.Background(), json.RawMessage(`{"url":"`+srv.URL+`"}`))
	var toolErr *agent.ToolError
	if !errors.As(err, &toolErr) || !strings.Contains(toolErr.Message, "public") {
		t.Fatalf("following a redirect to loopback: %v", err)
	}
}

type firstHopOpen struct {
	guarded http.RoundTripper
	used    bool
}

func (f *firstHopOpen) RoundTrip(req *http.Request) (*http.Response, error) {
	if !f.used {
		f.used = true
		return http.DefaultTransport.RoundTrip(req)
	}
	return f.guarded.RoundTrip(req)
}

func TestWebFetchExtractsText(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<html><head><title> The  Title </title><style>p{}</style></head>
<body><nav>menu</nav><h1>Heading</h1><p>First   paragraph.</p><script>alert(1)</script><p>Second.</p></body></html>`))
	}))
	defer srv.Close()

	out, err := WebFetch{Client: srv.Client()}.Execute(context.Background(), json.RawMessage(`{"url":"`+srv.URL+`"}`))
	if err != nil {
		t.Fatal(err)
	}
	want := "Title: The Title\nURL: " + srv.URL + "\n\nHeading\nFirst paragraph.\nSecond."
	if out.Content != want {
		t.Fatalf("content:\n%q\nwant\n%q", out.Content, want)
	}
}

func TestWebSearchTavily(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tvly-test" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["query"] != "go 1.26" || body["max_results"] != float64(2) {
			t.Errorf("request body %v", body)
		}
		_, _ = w.Write([]byte(`{"results":[{"title":"Go 1.26","url":"https://go.dev/doc/go1.26","content":"Release notes."}]}`))
	}))
	defer srv.Close()

	tool := WebSearch{Searcher: Tavily{APIKey: "tvly-test", Endpoint: srv.URL, Client: srv.Client()}}
	out, err := tool.Execute(context.Background(), json.RawMessage(`{"query":"go 1.26","max_results":2}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.Content, "https://go.dev/doc/go1.26") {
		t.Fatalf("content %q", out.Content)
	}

	bad := WebSearch{Searcher: Tavily{APIKey: "wrong", Endpoint: srv.URL, Client: srv.Client()}}
	_, err = bad.Execute(context.Background(), json.RawMessage(`{"query":"x"}`))
	var toolErr *agent.ToolError
	if !errors.As(err, &toolErr) || strings.Contains(toolErr.Message, "wrong") {
		t.Fatalf("auth failure: %v", err)
	}
}

func TestBlocklist(t *testing.T) {
	b, err := ParseBlocklist([]string{"example.com", "*.tracker.io", "https://www.Spam.net/page", `/\.pdf$/`, " "})
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]bool{
		"https://example.com/a":          true,
		"https://news.example.com/a":     true,
		"https://notexample.com/a":       false,
		"https://tracker.io/":            false,
		"https://a.tracker.io/":          true,
		"https://spam.net/x":             true,
		"https://docs.go.dev/report.pdf": true,
		"https://go.dev/doc":             false,
	}
	for u, want := range cases {
		if got := b.Blocks(u); got != want {
			t.Errorf("Blocks(%s) = %v, want %v", u, got, want)
		}
	}
	if got := b.Domains(); len(got) != 2 || got[0] != "example.com" || got[1] != "spam.net" {
		t.Errorf("Domains() = %v", got)
	}
	if _, err := ParseBlocklist([]string{"/(unclosed/"}); err == nil {
		t.Error("a bad regular expression parsed")
	}
}

func TestWebSearchAppliesSettings(t *testing.T) {
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&body)
		_, _ = w.Write([]byte(`{"results":[
			{"title":"Blocked","url":"https://ads.example.com/x","content":"ad","raw_content":"ad page"},
			{"title":"One","url":"https://one.dev/","content":"short","raw_content":"` + strings.Repeat("a", 500) + `"},
			{"title":"Two","url":"https://two.dev/","content":"excerpt two","raw_content":""},
			{"title":"Three","url":"https://three.dev/","content":"three","raw_content":"three page"}
		]}`))
	}))
	defer srv.Close()

	blocklist, _ := ParseBlocklist([]string{"example.com"})
	tool := WebSearch{
		Searcher: Tavily{APIKey: "k", Endpoint: srv.URL, Client: srv.Client()},
		Config: SearchConfig{MaxResults: 2, Depth: "advanced", Compression: CompressTruncate,
			TruncateLength: 300, Blocklist: blocklist},
	}
	results, hits, blocked, err := tool.Run(context.Background(), "q", 10)
	if err != nil {
		t.Fatal(err)
	}
	if body["max_results"] != float64(2) || body["search_depth"] != "advanced" || body["include_raw_content"] != "markdown" {
		t.Errorf("request body %v", body)
	}
	if excluded, _ := body["exclude_domains"].([]any); len(excluded) != 1 || excluded[0] != "example.com" {
		t.Errorf("exclude_domains = %v", body["exclude_domains"])
	}
	if blocked != 1 || len(results) != 2 || len(hits) != 2 {
		t.Fatalf("blocked %d, results %d, hits %d", blocked, len(results), len(hits))
	}
	if got := len([]rune(hits[0].Content)); got != 301 { // 300 and the ellipsis
		t.Errorf("truncated content is %d runes", got)
	}
	if hits[1].Content != "excerpt two" {
		t.Errorf("a result without page text falls back to the excerpt, got %q", hits[1].Content)
	}
}

func TestWebFetchRefusesBlockedSites(t *testing.T) {
	blocklist, _ := ParseBlocklist([]string{"example.com"})
	_, err := WebFetch{Blocklist: blocklist}.Execute(context.Background(), json.RawMessage(`{"url":"https://www.example.com/page"}`))
	var toolErr *agent.ToolError
	if !errors.As(err, &toolErr) || !strings.Contains(toolErr.Message, "blocked") {
		t.Fatalf("err = %v, want the blocked-site error", err)
	}
}
