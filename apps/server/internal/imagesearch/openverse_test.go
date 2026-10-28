package imagesearch

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestOpenversePicksTheFirstFitPicture(t *testing.T) {
	var asked string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked = r.URL.RawQuery
		_, _ = fmt.Fprint(w, `{"results":[
			{"id":"a","url":"https://x/a.jpg","width":1200,"mature":true},
			{"id":"b","url":"https://x/b.svg","width":1200},
			{"id":"c","url":"https://x/c.jpg","width":120},
			{"id":"d","url":"https://x/d.jpg","thumbnail":"https://x/d/thumb","width":1024,"height":768,"license":"by-nc","license_version":"2.0","creator":"alcomm"}
		]}`)
	}))
	defer srv.Close()
	o := NewOpenverse(nil)
	o.Endpoint = srv.URL
	image, err := o.Search(context.Background(), "laptop keyboard")
	if err != nil || image == nil || image.ID != "d" || image.License != "CC BY-NC 2.0" || image.Fallback != "https://x/d/thumb" {
		t.Fatalf("image = %+v, %v", image, err)
	}
	if asked != "mature=false&page_size=20&q=laptop+keyboard" {
		t.Errorf("asked %q", asked)
	}
}

func TestOpenverseFindsNothingOrIsRefused(t *testing.T) {
	status := http.StatusOK
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if status != http.StatusOK {
			w.Header().Set("Retry-After", "30")
			w.WriteHeader(status)
			return
		}
		_, _ = fmt.Fprint(w, `{"results":[]}`)
	}))
	defer srv.Close()
	o := NewOpenverse(nil)
	o.Endpoint = srv.URL
	if image, err := o.Search(context.Background(), "nothing"); image != nil || err != nil {
		t.Fatalf("nothing: %+v, %v", image, err)
	}
	status = http.StatusTooManyRequests
	if _, err := o.Search(context.Background(), "x"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("429: %v", err)
	}
	// Openverse asked for 30 seconds: no search is made until then.
	status = http.StatusOK
	if _, err := o.Search(context.Background(), "x"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("while blocked: %v", err)
	}
}

func TestPixabayTakesTheFirstPhoto(t *testing.T) {
	var asked string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked = r.URL.Query().Get("safesearch") + " " + r.URL.Query().Get("key")
		_, _ = fmt.Fprint(w, `{"hits":[{"id":195893,"pageURL":"https://pixabay.com/photos/blossom-195893/","tags":"blossom, flower",
			"webformatURL":"https://pixabay.com/get/a_640.jpg","largeImageURL":"https://pixabay.com/get/a_1280.jpg",
			"imageWidth":4000,"imageHeight":2250,"user":"Josch13","user_id":48777}]}`)
	}))
	defer srv.Close()
	p := NewPixabay("k", nil)
	p.Endpoint = srv.URL
	image, err := p.Search(context.Background(), "flower")
	if err != nil || image == nil || image.ID != "195893" || image.Width != 1280 || image.Height != 720 ||
		image.CreatorURL != "https://pixabay.com/users/Josch13-48777/" || image.Fallback != "https://pixabay.com/get/a_640.jpg" {
		t.Fatalf("image = %+v, %v", image, err)
	}
	if asked != "true k" {
		t.Errorf("asked %q", asked)
	}
}

func TestLimiterKeepsToTheMinutesShare(t *testing.T) {
	l := limiter{perMinute: 3}
	now := time.Now()
	for i := 0; i < 3; i++ {
		if !l.take(now) {
			t.Fatalf("search %d refused", i+1)
		}
	}
	if l.take(now) {
		t.Fatal("one over the share was allowed")
	}
	if !l.take(now.Add(time.Minute)) {
		t.Fatal("the next minute was refused")
	}
}

func TestWikimediaReadsCommonsMetadata(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("gsrsearch"); got != "filetype:bitmap keyboard cleaning brush" {
			t.Errorf("searched %q", got)
		}
		_, _ = fmt.Fprint(w, `{"query":{"pages":[
			{"pageid":2,"title":"File:Small.jpg","index":1,"imageinfo":[{"url":"https://u/s.jpg","width":200,"height":100,"mime":"image/jpeg"}]},
			{"pageid":3,"title":"File:Map.svg","index":2,"imageinfo":[{"url":"https://u/m.svg","width":2000,"height":1000,"mime":"image/svg+xml"}]},
			{"pageid":7,"title":"File:Keyboard cleaner 1.jpg","index":3,"imageinfo":[{"url":"https://u/k.jpg","thumburl":"https://u/1280px-k.jpg",
				"thumbwidth":1280,"thumbheight":853,"width":4896,"height":3264,"mime":"image/jpeg","descriptionurl":"https://commons.wikimedia.org/wiki/File:Keyboard_cleaner_1.jpg",
				"extmetadata":{"Artist":{"value":"<a href=\"//commons.wikimedia.org/wiki/User:Ann\">Ann &amp; Co</a>"},
				"LicenseShortName":{"value":"CC BY-SA 4.0"},"LicenseUrl":{"value":"https://creativecommons.org/licenses/by-sa/4.0"}}}]}
		]}}`)
	}))
	defer srv.Close()
	w := NewWikimedia(nil)
	w.Endpoint = srv.URL
	image, err := w.Search(context.Background(), "keyboard cleaning brush")
	if err != nil || image == nil {
		t.Fatalf("image = %+v, %v", image, err)
	}
	want := Image{Provider: "wikimedia", ID: "7", URL: "https://u/1280px-k.jpg", Fallback: "https://u/k.jpg", Width: 1280, Height: 853,
		Title: "Keyboard cleaner 1", Creator: "Ann & Co", CreatorURL: "https://commons.wikimedia.org/wiki/User:Ann",
		License: "CC BY-SA 4.0", LicenseURL: "https://creativecommons.org/licenses/by-sa/4.0",
		Landing: "https://commons.wikimedia.org/wiki/File:Keyboard_cleaner_1.jpg", Source: "wikimedia"}
	if *image != want {
		t.Errorf("image = %+v", *image)
	}
}

func TestASearchDownForAMomentIsAskedAgain(t *testing.T) {
	defer func(waits []time.Duration) { retryWaits = waits }(retryWaits)
	retryWaits = []time.Duration{0, time.Millisecond, time.Millisecond}
	var asked int
	failures := 2
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked++
		if asked <= failures {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = fmt.Fprint(w, `{"query":{"pages":[]}}`)
	}))
	defer srv.Close()
	w := NewWikimedia(nil)
	w.Endpoint = srv.URL
	// Down twice, then answering: the third try finds the answer.
	if image, err := w.Search(context.Background(), "pear"); image != nil || err != nil || asked != 3 {
		t.Fatalf("image = %+v, %v after %d", image, err, asked)
	}
	// Down every time: three tries, then unavailable.
	asked, failures = 0, 10
	if _, err := w.Search(context.Background(), "pear"); !errors.Is(err, ErrUnavailable) || asked != 3 {
		t.Fatalf("err = %v after %d", err, asked)
	}
}
