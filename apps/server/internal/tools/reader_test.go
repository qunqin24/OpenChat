package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// A page the site turns away is read through the reading service; a bot
// check the service met as well is still a refusal; a page that is not there
// is not retried.
func TestWebFetchFallsBackToTheReader(t *testing.T) {
	var asked []string
	reader := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked = append(asked, r.URL.Path)
		if strings.Contains(r.URL.Path, "walled") {
			_, _ = w.Write([]byte(`{"code":200,"data":{"title":"Just a moment...","url":"x","content":"Verifying you are human."}}`))
			return
		}
		_, _ = w.Write([]byte(`{"code":200,"data":{"title":"The Post","url":"https://site.test/post","content":"The real text of the post.","publishedTime":"2026-10-01T08:00:00Z"}}`))
	}))
	defer reader.Close()
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/gone" {
			http.NotFound(w, r)
			return
		}
		http.Error(w, "no bots", http.StatusForbidden)
	}))
	defer site.Close()
	fetch := WebFetch{Client: site.Client(), Reader: reader.URL + "/"}

	out, err := fetch.Execute(context.Background(), json.RawMessage(`{"url":"`+site.URL+`/post"}`))
	if err != nil || !strings.Contains(out.Content, "The real text of the post.") || !strings.Contains(out.Content, "https://site.test/post") {
		t.Fatalf("refused page through the reader: %v %q", err, out.Content)
	}
	if _, err := fetch.Execute(context.Background(), json.RawMessage(`{"url":"`+site.URL+`/walled"}`)); err == nil || !strings.Contains(err.Error(), "403") {
		t.Fatalf("a bot check passed on as the page: %v", err)
	}
	before := len(asked)
	if _, err := fetch.Execute(context.Background(), json.RawMessage(`{"url":"`+site.URL+`/gone"}`)); err == nil || len(asked) != before {
		t.Fatalf("a missing page was retried through the reader: %v, %d asks", err, len(asked)-before)
	}
}

// Off, the service is never asked; first, it is asked before the site and
// with the key, and the site is read only when it fails.
func TestReaderModes(t *testing.T) {
	var keys []string
	reader := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		keys = append(keys, r.Header.Get("Authorization"))
		if strings.Contains(r.URL.Path, "broken") {
			http.Error(w, "down", http.StatusBadGateway)
			return
		}
		_, _ = w.Write([]byte(`{"code":200,"data":{"title":"Via reader","url":"https://site.test/a","content":"Read by the service."}}`))
	}))
	defer reader.Close()
	var direct int
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		direct++
		if r.URL.Path == "/walled" {
			http.Error(w, "no bots", http.StatusForbidden)
			return
		}
		_, _ = w.Write([]byte("<html><head><title>Direct</title></head><body><p>Read directly.</p></body></html>"))
	}))
	defer site.Close()

	off := WebFetch{Client: site.Client(), Reader: reader.URL + "/", Mode: ReaderOff}
	if _, err := off.ReadPage(context.Background(), site.URL+"/walled"); err == nil || len(keys) != 0 {
		t.Fatalf("off asked the service: %v, %d", err, len(keys))
	}

	// site.test stands for a public site, served by the local one.
	siteURL, _ := url.Parse(site.URL)
	client := &http.Client{Transport: hostTo{host: "site.test", to: siteURL.Host}}
	first := WebFetch{Client: client, Reader: reader.URL + "/", Mode: ReaderFirst, ReaderKey: "jina_k"}
	direct = 0
	got, err := first.ReadPage(context.Background(), "http://site.test/a")
	if err != nil || got.Via != "jina" || direct != 0 || keys[len(keys)-1] != "Bearer jina_k" {
		t.Fatalf("first: %+v %v, %d direct, keys %v", got, err, direct, keys)
	}
	got, err = first.ReadPage(context.Background(), "http://site.test/broken")
	if err != nil || got.Via != "builtin" || !strings.Contains(got.Text, "Read directly.") {
		t.Fatalf("first, service down: %+v %v", got, err)
	}
	if _, err := first.ReadPage(context.Background(), "http://127.0.0.1/x"); err == nil {
		t.Fatal("first sent a loopback address to the service")
	}
}

// hostTo sends requests for one host to another address.
type hostTo struct{ host, to string }

func (h hostTo) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.URL.Host == h.host {
		r = r.Clone(r.Context())
		r.URL.Host = h.to
	}
	return http.DefaultTransport.RoundTrip(r)
}
