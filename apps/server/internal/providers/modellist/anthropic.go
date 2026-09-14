package modellist

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
)

const (
	anthropicDefaultBase = "https://api.anthropic.com/v1"

	// anthropicVersion is required on every request. It is pinned rather than
	// tracked: the version header is what keeps this parser reading the shape
	// it was written for, so moving it is a deliberate edit with a fixture
	// behind it.
	anthropicVersion = "2023-06-01"
)

// anthropic lists models the Anthropic way: a custom key header, a required
// version header, and a cursor.
//
//	GET {base}/models?limit=1000[&after_id=<last_id>]
//	x-api-key: <key>
//	anthropic-version: 2023-06-01
//	{"data":[{"type":"model","id":"claude-…","display_name":"Claude …"}],
//	 "has_more":false,"last_id":"claude-…"}
type anthropic struct{}

var _ dialect = anthropic{}

type anthropicList struct {
	Data []struct {
		Type        string `json:"type"`
		ID          string `json:"id"`
		DisplayName string `json:"display_name"`
	} `json:"data"`
	HasMore bool   `json:"has_more"`
	LastID  string `json:"last_id"`
}

func (anthropic) list(ctx context.Context, f fetcher) ([]Model, error) {
	header := http.Header{}
	// Not Authorization: this endpoint ignores a bearer token and answers 401.
	header.Set("x-api-key", f.key)
	header.Set("anthropic-version", anthropicVersion)

	var models collector
	afterID := ""

	for page := 0; page < maxPages; page++ {
		query := url.Values{"limit": {strconv.Itoa(pageSize)}}
		if afterID != "" {
			query.Set("after_id", afterID)
		}

		var body anthropicList
		if err := f.get(ctx, "/models", query, header, &body); err != nil {
			return nil, err
		}

		for _, entry := range body.Data {
			// The list is documented as models only, but the envelope carries a
			// type on every object, so anything else that turns up here is
			// skipped rather than stored as a model.
			if entry.Type != "" && entry.Type != "model" {
				continue
			}
			models.add(entry.ID, entry.DisplayName)
		}

		// Three ways to be done, and the last two are the endpoint contradicting
		// itself: "there is more" with no cursor, or with the cursor we just
		// used, would page over the same rows until the deadline ran out.
		if !body.HasMore || body.LastID == "" || body.LastID == afterID {
			return models.result(), nil
		}
		afterID = body.LastID
	}

	return nil, fmt.Errorf("%w: still more after %d pages", ErrTooManyPages, maxPages)
}
