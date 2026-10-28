package imagesearch

import (
	"context"
	"encoding/json"
	"html"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Bing is Bing Images, asked through the endpoint its own results page loads
// more results from (/images/async). It is not an API: there is no key, no
// documentation and no promise it stays as it is, so it is asked the way a
// browser asks it, and a page without results in the expected form counts as
// the service being unavailable, not as nothing found. It finds a picture for
// almost anything, quickly, with safe search on - but the pictures are other
// sites' and carry their rights, not a free licence, which the settings page
// says. Stock libraries, whose pictures it finds only as watermarked previews,
// are passed over.
type Bing struct {
	// HTTP is the client searches go through; http.DefaultClient when nil.
	HTTP *http.Client
	// Endpoint is the search address; Bing's when empty.
	Endpoint string

	limit limiter
}

// NewBing is a search through the client.
func NewBing(client *http.Client) *Bing {
	return &Bing{HTTP: client, limit: limiter{perMinute: 30}}
}

// bingUserAgent is a browser's: the endpoint answers browsers.
const bingUserAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0.0.0 Safari/537.36"

var (
	// bingResult is a result's link: its m attribute holds the picture's
	// details as JSON, its href the full size (expw, exph).
	bingResult = regexp.MustCompile(`<a [^>]*class="iusc"[^>]*>`)
	bingAttr   = regexp.MustCompile(`\s(m|href)="([^"]*)"`)
	// stockHosts serve watermarked previews of pictures they sell.
	stockHosts = []string{
		"alamy.com", "shutterstock.com", "gettyimages.", "istockphoto.com", "dreamstime.com", "123rf.com",
		"depositphotos.com", "stock.adobe.com", "ftcdn.net", "vectorstock.com", "canstockphoto.", "pond5.com",
		"bigstockphoto.com", "agefotostock.com", "superstock.com", "vecteezy.com", "freepik.com",
	}
)

type bingDetails struct {
	Page      string `json:"purl"`
	Picture   string `json:"murl"`
	Thumbnail string `json:"turl"`
	Title     string `json:"t"`
	ID        string `json:"mid"`
}

func (b *Bing) Search(ctx context.Context, query string) (*Image, error) {
	if b.limit.perMinute == 0 {
		b.limit.perMinute = 30
	}
	if !b.limit.take(time.Now()) {
		return nil, ErrUnavailable
	}
	address := b.Endpoint
	if address == "" {
		address = "https://www.bing.com/images/async"
	}
	// count 35 is what the page itself asks for; others return more or less
	// than asked. adlt=strict is safe search.
	params := url.Values{"q": {query}, "first": {"1"}, "count": {"35"}, "mkt": {"zh-CN"}, "adlt": {"strict"}}
	body, err := getBody(ctx, b.HTTP, address+"?"+params.Encode(), &b.limit, "Retry-After", http.Header{
		"Accept": {"text/html"}, "Accept-Language": {"zh-CN,zh;q=0.9,en;q=0.8"}, "User-Agent": {bingUserAgent},
	})
	if err != nil {
		return nil, err
	}
	links := bingResult.FindAllString(string(body), -1)
	if len(links) == 0 {
		// A check for robots, or a changed page: nothing can be read from it.
		return nil, ErrUnavailable
	}
	for _, link := range links {
		if image := bingImage(link); image != nil {
			return image, nil
		}
	}
	return nil, nil
}

// bingImage is the picture a result's link describes, or nil for one that is
// too small, not on the web, or a stock library's preview.
func bingImage(link string) *Image {
	var details bingDetails
	var size url.Values
	for _, attr := range bingAttr.FindAllStringSubmatch(link, -1) {
		value := html.UnescapeString(attr[2])
		switch attr[1] {
		case "m":
			if json.Unmarshal([]byte(value), &details) != nil {
				return nil
			}
		case "href":
			if parsed, err := url.Parse(value); err == nil {
				size = parsed.Query()
			}
		}
	}
	picture, err := url.Parse(details.Picture)
	if err != nil || (picture.Scheme != "https" && picture.Scheme != "http") || details.ID == "" {
		return nil
	}
	host := strings.ToLower(picture.Hostname())
	for _, stock := range stockHosts {
		if strings.Contains(host, stock) {
			return nil
		}
	}
	width, _ := strconv.Atoi(size.Get("expw"))
	height, _ := strconv.Atoi(size.Get("exph"))
	if width < minWidth || height <= 0 {
		return nil
	}
	source := host
	if page, err := url.Parse(details.Page); err == nil && page.Hostname() != "" {
		source = strings.TrimPrefix(strings.ToLower(page.Hostname()), "www.")
	}
	fallback := ""
	if strings.HasPrefix(details.Thumbnail, "https://") {
		fallback = details.Thumbnail
	}
	return &Image{
		Provider: "bing", ID: details.ID, URL: details.Picture, Fallback: fallback, Width: width, Height: height,
		Title: strings.TrimSpace(details.Title), Landing: details.Page, Source: source,
	}
}
