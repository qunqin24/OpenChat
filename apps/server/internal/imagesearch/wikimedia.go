package imagesearch

import (
	"context"
	"html"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Wikimedia searches Wikimedia Commons' own API for photos: freely licensed,
// needing no key, and answering quickly. Commons has no safe-search filter;
// what it offers for a few plain keywords is overwhelmingly documentary, and
// Openverse (which filters) or Pixabay remain the choice where that is not
// enough. Wikimedia asks callers to name themselves in the User-Agent and to
// keep to a modest pace, which the minute's share below does.
type Wikimedia struct {
	// HTTP is the client searches go through; http.DefaultClient when nil.
	HTTP *http.Client
	// Endpoint is the API address; Commons' when empty.
	Endpoint string

	limit limiter
}

// NewWikimedia is a search through the client.
func NewWikimedia(client *http.Client) *Wikimedia {
	return &Wikimedia{HTTP: client, limit: limiter{perMinute: 60}}
}

type wikimediaPage struct {
	PageID    int    `json:"pageid"`
	Title     string `json:"title"`
	Index     int    `json:"index"`
	ImageInfo []struct {
		URL         string `json:"url"`
		ThumbURL    string `json:"thumburl"`
		ThumbWidth  int    `json:"thumbwidth"`
		ThumbHeight int    `json:"thumbheight"`
		Width       int    `json:"width"`
		Height      int    `json:"height"`
		Mime        string `json:"mime"`
		Page        string `json:"descriptionurl"`
		Meta        map[string]struct {
			Value string `json:"value"`
		} `json:"extmetadata"`
	} `json:"imageinfo"`
}

func (w *Wikimedia) Search(ctx context.Context, query string) (*Image, error) {
	if w.limit.perMinute == 0 {
		w.limit.perMinute = 60
	}
	if !w.limit.take(time.Now()) {
		return nil, ErrUnavailable
	}
	address := w.Endpoint
	if address == "" {
		address = "https://commons.wikimedia.org/w/api.php"
	}
	params := url.Values{
		"action": {"query"}, "format": {"json"}, "formatversion": {"2"},
		// Files (namespace 6) that are photos or drawings in a bitmap format.
		"generator": {"search"}, "gsrnamespace": {"6"}, "gsrlimit": {"10"}, "gsrsearch": {"filetype:bitmap " + query},
		"prop": {"imageinfo"}, "iiprop": {"url|size|mime|extmetadata"}, "iiurlwidth": {"1280"},
		"iiextmetadatafilter": {"Artist|LicenseShortName|LicenseUrl|ObjectName"},
	}
	var answer struct {
		Query struct {
			Pages []wikimediaPage `json:"pages"`
		} `json:"query"`
	}
	if err := getJSON(ctx, w.HTTP, address+"?"+params.Encode(), &w.limit, "Retry-After", &answer); err != nil {
		return nil, err
	}
	pages := answer.Query.Pages
	// The search's order, which the API does not keep in the list.
	sort.SliceStable(pages, func(i, j int) bool { return pages[i].Index < pages[j].Index })
	for _, page := range pages {
		if len(page.ImageInfo) == 0 {
			continue
		}
		info := page.ImageInfo[0]
		if info.Width < minWidth || !imageMimes[info.Mime] || !strings.HasPrefix(info.URL, "http") {
			continue
		}
		meta := func(key string) string { return strings.TrimSpace(info.Meta[key].Value) }
		width, height := info.Width, info.Height
		if info.ThumbURL != "" && info.ThumbWidth > 0 {
			width, height = info.ThumbWidth, info.ThumbHeight
		}
		title := plainText(meta("ObjectName"))
		if title == "" {
			title = strings.TrimSuffix(strings.TrimPrefix(page.Title, "File:"), extOf(page.Title))
		}
		// The scaled copy is the one kept: a Commons original is often tens
		// of megapixels.
		kept, fallback := info.ThumbURL, info.URL
		if kept == "" {
			kept, fallback = info.URL, ""
		}
		return &Image{
			Provider: "wikimedia", ID: strconv.Itoa(page.PageID), URL: kept, Fallback: fallback,
			Width: width, Height: height, Title: title, Creator: plainText(meta("Artist")),
			CreatorURL: firstLink(meta("Artist")), License: meta("LicenseShortName"), LicenseURL: meta("LicenseUrl"),
			Landing: info.Page, Source: "wikimedia",
		}, nil
	}
	return nil, nil
}

// imageMimes are the formats Commons serves that are kept.
var imageMimes = map[string]bool{"image/jpeg": true, "image/png": true, "image/webp": true, "image/gif": true}

var (
	tags     = regexp.MustCompile(`<[^>]*>`)
	hrefAttr = regexp.MustCompile(`href="([^"]+)"`)
)

// plainText is Commons' HTML metadata as text: tags dropped, entities read.
func plainText(markup string) string {
	return strings.Join(strings.Fields(html.UnescapeString(tags.ReplaceAllString(markup, " "))), " ")
}

// firstLink is the first link in Commons' HTML metadata, made absolute.
func firstLink(markup string) string {
	match := hrefAttr.FindStringSubmatch(markup)
	if match == nil {
		return ""
	}
	link := html.UnescapeString(match[1])
	if strings.HasPrefix(link, "//") {
		link = "https:" + link
	}
	if !strings.HasPrefix(link, "https://") && !strings.HasPrefix(link, "http://") {
		return ""
	}
	return link
}

func extOf(name string) string {
	if dot := strings.LastIndex(name, "."); dot > 0 {
		return name[dot:]
	}
	return ""
}
