package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/qunqin24/openchat/apps/server/internal/doctext"
)

func fetchTool(t *testing.T, srv *httptest.Server, args string) string {
	t.Helper()
	out, err := WebFetch{Client: srv.Client()}.Execute(context.Background(), json.RawMessage(args))
	if err != nil {
		t.Fatal(err)
	}
	return out.Content
}

// An article page reads as its article, in Markdown: the sidebar and the
// comments' chrome left out, the table a table, the links absolute.
func TestWebFetchReadsTheMainContentAsMarkdown(t *testing.T) {
	paragraph := strings.Repeat("The harvest came in early this year, and the growers were pleased with the yield. ", 6)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `<html><head><title>Strawberries | Farm News</title>
<meta property="article:published_time" content="2026-05-03T08:00:00Z"></head><body>
<header><a href="/">Farm News</a> <a href="/login">Sign in</a></header>
<aside class="sidebar"><h3>Trending</h3><ul><li><a href="/x">Ten tractors you will love</a></li><li><a href="/y">Celebrity gardens</a></li></ul></aside>
<article><h1>Strawberry season</h1><p>%s</p><h2>Prices</h2>
<table><tr><th>Grade</th><th>Price</th></tr><tr><td>A</td><td>12</td></tr></table>
<p>%s See <a href="/report">the full report</a>.</p></article>
<div class="share">Share on social</div></body></html>`, paragraph, paragraph)
	}))
	defer srv.Close()

	out := fetchTool(t, srv, `{"url":"`+srv.URL+`/story"}`)
	for _, want := range []string{"Title: Strawberries | Farm News", "Published: 2026-05-03", "Strawberry season", "## Prices", "| Grade | Price |", "[the full report](" + srv.URL + "/report)"} {
		if !strings.Contains(out, want) {
			t.Errorf("lacks %q:\n%s", want, out)
		}
	}
	for _, noise := range []string{"Ten tractors", "Celebrity gardens"} {
		if strings.Contains(out, noise) {
			t.Errorf("the sidebar came along (%q):\n%s", noise, out)
		}
	}
}

// A page longer than one read comes in parts, each saying where the next
// starts; find goes straight to the passages that mention something.
func TestWebFetchReadsOnAndFinds(t *testing.T) {
	var lines []string
	for i := range 1200 {
		lines = append(lines, fmt.Sprintf("Line %04d of the long page, with some words to fill it out.", i))
	}
	lines[900] = "Line 0900 holds the NEEDLE we are after."
	body := "<html><body><pre>" + strings.Join(lines, "\n") + "</pre></body></html>"
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, body)
	}))
	defer srv.Close()
	address := srv.URL + "/long"

	first := fetchTool(t, srv, `{"url":"`+address+`"}`)
	var end, total int
	if _, err := fmt.Sscanf(first[strings.LastIndex(first, "[Characters "):], "[Characters 0–%d of %d.", &end, &total); err != nil {
		t.Fatalf("no part marker: %v\n%s", err, first[len(first)-300:])
	}
	if !strings.Contains(first, fmt.Sprintf("offset %d", end)) || strings.Contains(first, "NEEDLE") {
		t.Fatalf("the first part: end %d of %d", end, total)
	}

	second := fetchTool(t, srv, fmt.Sprintf(`{"url":%q,"offset":%d}`, address, end))
	if !strings.Contains(second, fmt.Sprintf("[Characters %d–", end)) {
		t.Errorf("the second part does not start where the first ended:\n%s", second[len(second)-200:])
	}

	found := fetchTool(t, srv, `{"url":"`+address+`","find":"needle"}`)
	if !strings.Contains(found, "1 match of \"needle\"") || !strings.Contains(found, "Line 0900 holds the NEEDLE") || !strings.Contains(found, "[offset ") {
		t.Errorf("find:\n%s", found)
	}
	if strings.Contains(found, "Line 0100") {
		t.Errorf("find showed more than the passage:\n%s", found)
	}
	if none := fetchTool(t, srv, `{"url":"`+address+`","find":"haystack"}`); !strings.Contains(none, "does not occur") {
		t.Errorf("a find with no match:\n%s", none)
	}
	if n := hits.Load(); n != 1 {
		t.Errorf("the page was fetched %d times for one reading", n)
	}

	if _, err := (WebFetch{Client: srv.Client()}).Execute(context.Background(), json.RawMessage(fmt.Sprintf(`{"url":%q,"offset":%d}`, address, total+5))); err == nil {
		t.Error("an offset past the end was read")
	}
}

func TestWindowEndsAtLines(t *testing.T) {
	text := []rune(strings.Repeat(strings.Repeat("x", 99)+"\n", 500))
	start, end := window(text, 150)
	if start != 100 || text[end-1] != '\n' || end-start > fetchMaxChars {
		t.Errorf("window from 150: %d–%d", start, end)
	}
}

// A PDF reads as its pages, served as a PDF or as bytes of no kind.
func TestWebFetchReadsPDFs(t *testing.T) {
	if !doctext.PDFReadable() {
		t.Skip("pdftotext is not installed")
	}
	pdf := onePagePDF("Quarterly revenue rose by nine percent")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".bin") {
			w.Header().Set("Content-Type", "application/octet-stream")
		} else {
			w.Header().Set("Content-Type", "application/pdf")
		}
		_, _ = w.Write(pdf)
	}))
	defer srv.Close()
	for _, path := range []string{"/report%202026.pdf", "/download.bin"} {
		out := fetchTool(t, srv, `{"url":"`+srv.URL+path+`"}`)
		if !strings.Contains(out, "Quarterly revenue rose by nine percent") || !strings.Contains(out, "PDF, 1 page;") || !strings.Contains(out, "[Page 1]") {
			t.Errorf("%s:\n%s", path, out)
		}
	}
	if out := fetchTool(t, srv, `{"url":"`+srv.URL+`/report%202026.pdf"}`); !strings.Contains(out, "Title: report 2026.pdf") {
		t.Errorf("the PDF's name:\n%s", out)
	}
}

// onePagePDF is a PDF of one page with a line of text on it.
func onePagePDF(line string) []byte {
	stream := fmt.Sprintf("BT /F1 24 Tf 72 700 Td (%s) Tj ET", line)
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [4 0 R] /Count 1 >>",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 3 0 R >> >> /Contents 5 0 R >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(stream), stream),
	}
	var b bytes.Buffer
	b.WriteString("%PDF-1.4\n")
	offsets := make([]int, len(objects))
	for i, object := range objects {
		offsets[i] = b.Len()
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, object)
	}
	xref := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(objects)+1)
	for _, offset := range offsets {
		fmt.Fprintf(&b, "%010d 00000 n \n", offset)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objects)+1, xref)
	return b.Bytes()
}

func TestReadableAddress(t *testing.T) {
	for in, want := range map[string]string{
		"https://zh.wikipedia.org/wiki/%E8%8D%89%E8%8E%93":    "https://zh.wikipedia.org/wiki/草莓",
		"https://en.wikipedia.org/wiki/Hybrid_%28biology%29":  "https://en.wikipedia.org/wiki/Hybrid_%28biology%29",
		"https://example.com/a%20b/%E4%BD%A0?q=%E5%A5%BD%26x": "https://example.com/a%20b/你?q=好%26x",
		"https://example.com/%FF%FE":                          "https://example.com/%FF%FE",
		"https://example.com/plain":                           "https://example.com/plain",
	} {
		if got := readableAddress(in); got != want {
			t.Errorf("%s: %s, want %s", in, got, want)
		}
	}
}

// A word on every line of a long page does not bring the page in at once: the
// passages stay within a read's worth, and the find says where to go on from.
func TestFindStaysWithinARead(t *testing.T) {
	text := []rune(strings.Repeat("the quick brown fox jumps over the lazy dog\n", 10000)) // 440,000 characters
	passages, matches, next := findIn(text, "fox", 0)
	shown := 0
	for _, p := range passages {
		if p.end-p.start > findPassageMax {
			t.Errorf("a passage of %d characters", p.end-p.start)
		}
		shown += p.end - p.start
	}
	if shown > fetchMaxChars || len(passages) > findPassages {
		t.Errorf("%d passages, %d characters", len(passages), shown)
	}
	if matches != 10000 || next <= 0 || next < passages[len(passages)-1].start {
		t.Errorf("matches %d, next %d", matches, next)
	}
	// Going on from there shows what follows.
	more, _, _ := findIn(text, "fox", next)
	if len(more) == 0 || more[0].start < next-findBefore-50 {
		t.Errorf("finding on: %+v", more)
	}
	// Few matches: all shown, nothing to go on to.
	if _, n, next := findIn([]rune("one fox\nno\nanother fox"), "fox", 0); n != 2 || next != -1 {
		t.Errorf("few matches: %d, %d", n, next)
	}
}
