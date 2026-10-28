package imagesearch

import (
	"context"
	"errors"
	"fmt"
	"html"
	"net/http"
	"net/http/httptest"
	"testing"
)

// bingLink is a result link as Bing's page writes it.
func bingLink(id, picture, page string, width, height int) string {
	m := fmt.Sprintf(`{"purl":%q,"murl":%q,"turl":"https://ts1.mm.bing.net/th?id=OIP.%s&pid=15.1","t":"Ripe strawberries","mid":%q}`, page, picture, id, id)
	return fmt.Sprintf(`<li><a aria-label="x" class="iusc" style="height:187px" m="%s" href="/images/search?view=detailV2&amp;exph=%d&amp;expw=%d&amp;q=x" h="ID=images">`,
		html.EscapeString(m), height, width)
}

func TestBingSkipsStockPreviewsAndSmallPictures(t *testing.T) {
	answer := ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query(); got.Get("q") != "strawberry" || got.Get("adlt") != "strict" {
			t.Errorf("asked %v", got)
		}
		_, _ = fmt.Fprint(w, answer)
	}))
	defer srv.Close()
	b := NewBing(nil)
	b.Endpoint = srv.URL

	answer = `<ul class="dgControl_list">` +
		bingLink("A1", "https://c8.alamy.com/comp/x.jpg", "https://alamy.com/x", 1300, 1390) +
		bingLink("B2", "https://example.org/small.jpg", "https://example.org/", 300, 200) +
		bingLink("C3", "https://img.example.com/strawberry.jpg", "https://www.zhihu.com/question/1", 1600, 1067) + `</ul>`
	image, err := b.Search(context.Background(), "strawberry")
	if err != nil || image == nil {
		t.Fatalf("image = %+v, %v", image, err)
	}
	want := Image{Provider: "bing", ID: "C3", URL: "https://img.example.com/strawberry.jpg", Fallback: "https://ts1.mm.bing.net/th?id=OIP.C3&pid=15.1",
		Width: 1600, Height: 1067, Title: "Ripe strawberries", Landing: "https://www.zhihu.com/question/1", Source: "zhihu.com"}
	if *image != want {
		t.Errorf("image = %+v", *image)
	}

	// Results, none fit: nothing. No results at all (a check for robots): unavailable.
	answer = bingLink("A1", "https://c8.alamy.com/comp/x.jpg", "https://alamy.com/x", 1300, 1390)
	if image, err := b.Search(context.Background(), "strawberry"); image != nil || err != nil {
		t.Errorf("only a stock preview: %+v, %v", image, err)
	}
	answer = `<html><body>Please verify you are a human</body></html>`
	if _, err := b.Search(context.Background(), "strawberry"); !errors.Is(err, ErrUnavailable) {
		t.Errorf("no results: %v", err)
	}
}
