package imagesearch

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Pixabay is Pixabay's free photo search, with an administrator's API key.
// Its terms ask that searches be kept for a day and that pictures be shown
// from the caller's own server rather than linked to Pixabay - which is what
// api/images.go does with every picture - and allow 100 searches a minute per
// key, of which this takes most.
type Pixabay struct {
	Key string
	// HTTP is the client searches go through; http.DefaultClient when nil.
	HTTP *http.Client
	// Endpoint is the search address; the public API when empty.
	Endpoint string

	limit limiter
}

// NewPixabay is a search with the key, through the client.
func NewPixabay(key string, client *http.Client) *Pixabay {
	return &Pixabay{Key: key, HTTP: client, limit: limiter{perMinute: 80}}
}

type pixabayHit struct {
	ID            int    `json:"id"`
	PageURL       string `json:"pageURL"`
	Tags          string `json:"tags"`
	WebformatURL  string `json:"webformatURL"`
	LargeImageURL string `json:"largeImageURL"`
	ImageWidth    int    `json:"imageWidth"`
	ImageHeight   int    `json:"imageHeight"`
	User          string `json:"user"`
	UserID        int    `json:"user_id"`
}

func (p *Pixabay) Search(ctx context.Context, query string) (*Image, error) {
	if p.limit.perMinute == 0 {
		p.limit.perMinute = 80
	}
	if !p.limit.take(time.Now()) {
		return nil, ErrUnavailable
	}
	address := p.Endpoint
	if address == "" {
		address = "https://pixabay.com/api/"
	}
	// Pixabay takes at most 100 characters of query.
	if runes := []rune(query); len(runes) > 100 {
		query = string(runes[:100])
	}
	params := url.Values{
		"key": {p.Key}, "q": {query}, "image_type": {"photo"}, "safesearch": {"true"}, "per_page": {"20"},
	}
	var page struct {
		Hits []pixabayHit `json:"hits"`
	}
	if err := getJSON(ctx, p.HTTP, address+"?"+params.Encode(), &p.limit, "X-RateLimit-Reset", &page); err != nil {
		return nil, err
	}
	for _, hit := range page.Hits {
		if hit.ID == 0 || !strings.HasPrefix(hit.LargeImageURL, "http") {
			continue
		}
		// largeImageURL is at most 1280 pixels wide; scale the size to it.
		width, height := hit.ImageWidth, hit.ImageHeight
		if width > 1280 {
			height, width = height*1280/width, 1280
		}
		creator := ""
		if hit.UserID != 0 && hit.User != "" {
			creator = "https://pixabay.com/users/" + url.PathEscape(hit.User) + "-" + strconv.Itoa(hit.UserID) + "/"
		}
		return &Image{
			Provider: "pixabay", ID: strconv.Itoa(hit.ID), URL: hit.LargeImageURL, Fallback: hit.WebformatURL,
			Width: width, Height: height, Title: hit.Tags, Creator: hit.User, CreatorURL: creator,
			License: "Pixabay Content License", LicenseURL: "https://pixabay.com/service/license-summary/",
			Landing: hit.PageURL, Source: "pixabay",
		}, nil
	}
	return nil, nil
}
