package imagesearch

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Openverse is WordPress's search over Creative Commons and public-domain
// images from Flickr, Wikimedia Commons and others (openverse.org). It is
// asked anonymously, and Openverse limits anonymous callers per address - 20
// searches a minute and 200 a day at the time of writing - so the caller keeps
// what it finds, and this keeps well under the minute's share.
type Openverse struct {
	// HTTP is the client searches go through; http.DefaultClient when nil.
	HTTP *http.Client
	// Endpoint is the search address; the public API when empty.
	Endpoint string

	limit limiter
}

// NewOpenverse is a search through the client.
func NewOpenverse(client *http.Client) *Openverse {
	return &Openverse{HTTP: client, limit: limiter{perMinute: 15}}
}

type openverseImage struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	URL        string `json:"url"`
	Thumbnail  string `json:"thumbnail"`
	Width      int    `json:"width"`
	Height     int    `json:"height"`
	Creator    string `json:"creator"`
	CreatorURL string `json:"creator_url"`
	License    string `json:"license"`
	Version    string `json:"license_version"`
	LicenseURL string `json:"license_url"`
	Landing    string `json:"foreign_landing_url"`
	Source     string `json:"source"`
	Mature     bool   `json:"mature"`
}

func (o *Openverse) Search(ctx context.Context, query string) (*Image, error) {
	if o.limit.perMinute == 0 {
		o.limit.perMinute = 15
	}
	if !o.limit.take(time.Now()) {
		return nil, ErrUnavailable
	}
	address := o.Endpoint
	if address == "" {
		address = "https://api.openverse.org/v1/images/"
	}
	params := url.Values{"q": {query}, "page_size": {"20"}, "mature": {"false"}}
	var page struct {
		Results []openverseImage `json:"results"`
	}
	if err := getJSON(ctx, o.HTTP, address+"?"+params.Encode(), &o.limit, "Retry-After", &page); err != nil {
		return nil, err
	}
	var small *openverseImage
	for i := range page.Results {
		image := &page.Results[i]
		if image.Mature || image.ID == "" || !strings.HasPrefix(image.URL, "http") || strings.HasSuffix(strings.ToLower(image.URL), ".svg") {
			continue
		}
		if image.Width >= minWidth {
			return image.found(), nil
		}
		if small == nil {
			small = image
		}
	}
	if small == nil {
		return nil, nil
	}
	return small.found(), nil
}

func (i *openverseImage) found() *Image {
	return &Image{
		Provider: "openverse", ID: i.ID, URL: i.URL, Fallback: i.Thumbnail, Width: i.Width, Height: i.Height,
		Title: i.Title, Creator: i.Creator, CreatorURL: i.CreatorURL, License: licenseName(i.License, i.Version),
		LicenseURL: i.LicenseURL, Landing: i.Landing, Source: i.Source,
	}
}

// licenseName is the licence as Creative Commons writes it: "CC BY 2.0",
// "CC0 1.0", "Public Domain Mark 1.0".
func licenseName(license, version string) string {
	name := strings.ToLower(license)
	switch name {
	case "":
		return ""
	case "cc0":
		name = "CC0"
	case "pdm":
		name = "Public Domain Mark"
	default:
		name = "CC " + strings.ToUpper(name)
	}
	if version != "" {
		name += " " + version
	}
	return name
}

// getJSON asks a service and decodes its answer. A refusal for asking too
// often blocks the service's searches for the time it names in header.
func getJSON(ctx context.Context, client *http.Client, address string, limit *limiter, header string, out any) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", userAgent)
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusTooManyRequests {
		limit.block(time.Now(), resp.Header.Get(header))
		return fmt.Errorf("%w: rate limited", ErrUnavailable)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%w: status %d", ErrUnavailable, resp.StatusCode)
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(out); err != nil {
		return fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	return nil
}
