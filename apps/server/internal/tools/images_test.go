package tools

import (
	"net/url"
	"testing"
)

func TestPageImagesLeadFirstAndSkipsNoise(t *testing.T) {
	base, _ := url.Parse("https://news.example.com/2026/10/story")
	page := `<html><head>
<meta property="og:image" content="/media/lead.jpg">
<meta name="twitter:image" content="https://cdn.example.com/media/lead.jpg">
</head><body>
<header><img src="/brand.png" width="400"></header>
<nav><img src="/menu.jpg"></nav>
<article>
  <img src="/media/lead.jpg" alt="dup of the lead">
  <img src="/static/logo-dark.png" alt="logo">
  <img src="/media/chart.svg">
  <img src="data:image/png;base64,AAAA">
  <img src="/media/tiny.png" width="16" height="16">
  <img data-src="/media/lazy.webp" src="/placeholder.gif" alt="  A  lazy   one ">
  <img src="//img.example.org/photo.jpg" alt="protocol-relative">
</article>
<footer><img src="/footer.jpg"></footer>
</body></html>`
	got := htmlImages(page, base)
	want := []PageImage{
		{URL: "https://news.example.com/media/lead.jpg"},
		{URL: "https://cdn.example.com/media/lead.jpg"},
		{URL: "https://news.example.com/media/lazy.webp", Alt: "A lazy one"},
		{URL: "https://img.example.org/photo.jpg", Alt: "protocol-relative"},
	}
	if len(got) != len(want) {
		t.Fatalf("images = %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("image %d = %#v, want %#v", i, got[i], want[i])
		}
	}
}
