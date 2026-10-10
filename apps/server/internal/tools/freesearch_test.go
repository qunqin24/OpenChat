package tools

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFreeSearchFailsOver(t *testing.T) {
	var calls []string
	vendor := func(name string, err error) FreeVendor {
		return FreeVendor{Name: name, Search: func(context.Context, *http.Client, SearchRequest) ([]SearchResult, error) {
			calls = append(calls, name)
			if err != nil {
				return nil, err
			}
			return []SearchResult{{Title: name, URL: "https://" + name + ".dev"}}, nil
		}}
	}
	limited := &freeError{vendor: "a", msg: "HTTP 429", passOn: true}
	search := FreeSearch{Vendors: []FreeVendor{vendor("a", limited), vendor("b", limited), vendor("c", nil)}}

	// Whichever vendor the ring starts on, the search ends on c, the only one
	// that answers, having skipped the rate-limited ones.
	for range 3 {
		calls = nil
		results, err := search.Search(context.Background(), SearchRequest{Query: "q", Limit: 3})
		if err != nil {
			t.Fatal(err)
		}
		if len(results) != 1 || results[0].Source != "c" || calls[len(calls)-1] != "c" {
			t.Fatalf("results %+v after %v", results, calls)
		}
	}

	// A failure that is about the query stops the walk.
	calls = nil
	bad := FreeSearch{Vendors: []FreeVendor{
		vendor("x", &freeError{vendor: "x", msg: "HTTP 400", passOn: false}),
		vendor("y", &freeError{vendor: "y", msg: "HTTP 400", passOn: false}),
	}}
	if _, err := bad.Search(context.Background(), SearchRequest{Query: "q"}); err == nil || len(calls) != 1 {
		t.Fatalf("err %v after %v, want one call", err, calls)
	}

	// All four down is one error for the model, not four.
	calls = nil
	down := FreeSearch{Vendors: []FreeVendor{vendor("a", limited), vendor("b", limited)}}
	if _, err := down.Search(context.Background(), SearchRequest{Query: "q"}); err == nil || len(calls) != 2 {
		t.Fatalf("err %v after %v, want both tried", err, calls)
	}
}

func TestFreeVendorsParse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Errorf("%s sent credentials", r.URL.Path)
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		switch r.URL.Path {
		case "/exa":
			w.Header().Set("Content-Type", "text/event-stream")
			text := "Title: Go 1.27\nURL: https://go.dev/doc/go1.27\nPublished: N/A\nHighlights:\nThe latest Go release.\n---\nTitle: Go history\nURL: https://go.dev/doc/devel/release\nHighlights:\nEvery release."
			envelope, _ := json.Marshal(map[string]any{"result": map[string]any{"content": []any{map[string]any{"type": "text", "text": text}}}})
			_, _ = w.Write([]byte("event: message\ndata: " + string(envelope) + "\n\n"))
		case "/parallel":
			inner, _ := json.Marshal(map[string]any{"results": []any{map[string]any{"url": "https://p.dev", "title": "P", "excerpts": []string{"one", "two"}}}})
			envelope, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "result": map[string]any{"content": []any{map[string]any{"type": "text", "text": string(inner)}}}})
			_, _ = w.Write(envelope)
		case "/limited":
			envelope, _ := json.Marshal(map[string]any{"result": map[string]any{"isError": true, "content": []any{map[string]any{"type": "text", "text": "You have hit the rate limit"}}}})
			_, _ = w.Write(envelope)
		case "/v2/search":
			_, _ = w.Write([]byte(`{"success":true,"data":{"web":[{"url":"https://f.dev","title":"F","description":"firecrawl"}]}}`))
		case "/v1/search/public":
			if r.Header.Get("X-Keenable-Title") == "" {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			_, _ = w.Write([]byte(`{"results":[{"url":"https://k.dev","title":"K","snippet":"keenable"}]}`))
		}
	}))
	defer srv.Close()
	ctx, client := context.Background(), srv.Client()
	req := SearchRequest{Query: "go", Limit: 5}

	exa, err := exaSearch(srv.URL+"/exa")(ctx, client, req)
	if err != nil || len(exa) != 2 || exa[0].URL != "https://go.dev/doc/go1.27" || exa[0].Snippet != "The latest Go release." {
		t.Errorf("exa %+v %v", exa, err)
	}
	parallel, err := parallelSearch(srv.URL+"/parallel")(ctx, client, req)
	if err != nil || len(parallel) != 1 || parallel[0].Snippet != "one two" {
		t.Errorf("parallel %+v %v", parallel, err)
	}
	firecrawl, err := firecrawlSearch(srv.URL)(ctx, client, req)
	if err != nil || len(firecrawl) != 1 || firecrawl[0].Title != "F" {
		t.Errorf("firecrawl %+v %v", firecrawl, err)
	}
	keenable, err := keenableSearch(srv.URL)(ctx, client, req)
	if err != nil || len(keenable) != 1 || keenable[0].Snippet != "keenable" {
		t.Errorf("keenable %+v %v", keenable, err)
	}

	_, err = exaSearch(srv.URL+"/limited")(ctx, client, req)
	var vendorErr *freeError
	if !errors.As(err, &vendorErr) || !vendorErr.passOn || !strings.Contains(vendorErr.msg, "rate limit") {
		t.Errorf("a tool-level rate limit should pass on, got %v", err)
	}
}
