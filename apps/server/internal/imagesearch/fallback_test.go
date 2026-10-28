package imagesearch

import (
	"context"
	"errors"
	"testing"
	"time"
)

// fakeSearch answers with its picture, or nothing, or fails; and counts.
type fakeSearch struct {
	image *Image
	err   error
	asked int
}

func (f *fakeSearch) Search(context.Context, string) (*Image, error) {
	f.asked++
	return f.image, f.err
}

func TestFallbackAsksTheNextServiceAndRemembersFailures(t *testing.T) {
	down := &fakeSearch{err: ErrUnavailable}
	empty := &fakeSearch{}
	found := &fakeSearch{image: &Image{ID: "1"}}
	fallback := &Fallback{}
	search := fallback.Of(down, empty, found)

	// Down, then nothing, then a picture: the picture.
	if image, err := search.Search(context.Background(), "pear"); err != nil || image == nil || image.ID != "1" {
		t.Fatalf("image = %+v, %v", image, err)
	}
	// The one that failed is asked last for a while, so not at all here.
	if _, err := search.Search(context.Background(), "pear"); err != nil || down.asked != 1 || empty.asked != 2 {
		t.Fatalf("asked down %d, empty %d (%v)", down.asked, empty.asked, err)
	}
	// After its rest it is asked first again.
	fallback.failed[down] = time.Now().Add(-2 * time.Minute)
	if _, _ = search.Search(context.Background(), "pear"); down.asked != 2 {
		t.Fatalf("down asked %d times", down.asked)
	}

	// Nothing anywhere is nothing; nothing and a failure is unavailable, so
	// the words are not kept as finding nothing.
	if image, err := fallback.Of(empty, &fakeSearch{}).Search(context.Background(), "x"); image != nil || err != nil {
		t.Fatalf("nothing: %+v, %v", image, err)
	}
	if _, err := fallback.Of(empty, down).Search(context.Background(), "x"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("nothing and down: %v", err)
	}
	// One searcher is itself.
	if fallback.Of(found) != Searcher(found) {
		t.Fatal("a single searcher was wrapped")
	}
}
