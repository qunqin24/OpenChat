package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"golang.org/x/text/encoding/simplifiedchinese"
)

func TestReadPreviewPrefersOpenGraph(t *testing.T) {
	base, _ := url.Parse("https://example.com/news/story")
	page := readPreview(strings.NewReader(`<!doctype html><html><head>
		<title>  Plain
		title </title>
		<meta name="description" content="Plain description">
		<meta property="og:title" content="Shared &amp; title">
		<meta property="og:title" content="A second one is ignored">
		<meta name="twitter:description" content="Twitter description">
		<meta property="og:site_name" content="Example">
		<meta property="og:image" content="/img/cover.jpg">
		</head><body><meta property="og:description" content="in the body: not read"></body></html>`), base)
	if page.title != "Shared & title" || page.description != "Twitter description" || page.siteName != "Example" {
		t.Errorf("page = %+v", page)
	}
	if page.image != "https://example.com/img/cover.jpg" {
		t.Errorf("image = %q", page.image)
	}

	plain := readPreview(strings.NewReader(`<html><head><title>Only a title</title><meta property="og:image" content="javascript:alert(1)"></head></html>`), base)
	if plain.title != "Only a title" || plain.image != "" {
		t.Errorf("plain = %+v", plain)
	}
}

func TestPreviewFetchReadsHTMLInItsCharset(t *testing.T) {
	gbk, err := simplifiedchinese.GBK.NewEncoder().String(`<html><head><title>黑墙是什么</title></head></html>`)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/gbk":
			w.Header().Set("Content-Type", "text/html; charset=gbk")
			_, _ = w.Write([]byte(gbk))
		case "/moved":
			http.Redirect(w, r, "/blocked", http.StatusFound)
		case "/blocked":
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte("<title>Blocked page</title>"))
		case "/pdf":
			w.Header().Set("Content-Type", "application/pdf")
			_, _ = w.Write([]byte("%PDF"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	cache := &previewCache{entries: map[string]pagePreview{}, client: srv.Client()}
	none := func(string) bool { return false }

	if got := cache.get(context.Background(), srv.URL+"/gbk", none); got.title != "黑墙是什么" {
		t.Errorf("gbk title = %q", got.title)
	}
	if got := cache.get(context.Background(), srv.URL+"/pdf", none); got.title != "" {
		t.Errorf("a PDF gave a title: %+v", got)
	}
	// The blocklist is asked about where the redirects ended.
	blocked := func(address string) bool { return strings.HasSuffix(address, "/blocked") }
	fresh := &previewCache{entries: map[string]pagePreview{}, client: srv.Client()}
	if got := fresh.fetch(context.Background(), srv.URL+"/moved", none); got.title != "Blocked page" {
		t.Errorf("redirect, nothing blocked = %+v", got)
	}
	if got := fresh.fetch(context.Background(), srv.URL+"/moved", blocked); got.title != "" {
		t.Errorf("redirect to a blocked page = %+v", got)
	}
}
