package modellist

import (
	"context"
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"
)

const (
	geminiDefaultBase = "https://generativelanguage.googleapis.com/v1beta"

	// modelsPrefix is how this API spells a resource name — `models/x` is the
	// path the model lives at, not what the model is called. Everything else,
	// this product included, refers to it as `x`.
	modelsPrefix = "models/"

	// generateContent is the method a chat needs. A model that does not support
	// it — an embedder, a tokeniser — is real, listed, and useless here.
	generateContent = "generateContent"
)

// gemini lists models the Google way: the key in the query string, and a page
// token.
//
//	GET {base}/models?key=<key>&pageSize=1000[&pageToken=…]
//	{"models":[{"name":"models/gemini-2.0-flash","displayName":"Gemini 2.0 Flash",
//	            "supportedGenerationMethods":["generateContent"]}],
//	 "nextPageToken":"…"}
//
// The key being in the URL is the reason redactURL exists: a URL from this
// adapter is a credential, so no error and no log line may carry one whole.
type gemini struct{}

var _ dialect = gemini{}

type geminiList struct {
	Models []struct {
		Name                       string   `json:"name"`
		DisplayName                string   `json:"displayName"`
		SupportedGenerationMethods []string `json:"supportedGenerationMethods"`
	} `json:"models"`
	NextPageToken string `json:"nextPageToken"`
}

func (gemini) list(ctx context.Context, f fetcher) ([]Model, error) {
	var models collector
	pageToken := ""

	for page := 0; page < maxPages; page++ {
		query := url.Values{
			"key":      {f.key},
			"pageSize": {strconv.Itoa(pageSize)},
		}
		if pageToken != "" {
			query.Set("pageToken", pageToken)
		}

		var body geminiList
		if err := f.get(ctx, "/models", query, nil, &body); err != nil {
			return nil, err
		}

		for _, entry := range body.Models {
			// Filtered rather than stored and hidden: this list is what a
			// channel offers, and offering an embedding model as something to
			// chat with is an error the operator would only discover mid-chat.
			// The field is absent on some relays, and absent is not the same as
			// "does not support it" — an entry that says nothing is kept.
			if len(entry.SupportedGenerationMethods) > 0 &&
				!slices.Contains(entry.SupportedGenerationMethods, generateContent) {
				continue
			}
			models.add(strings.TrimPrefix(entry.Name, modelsPrefix), entry.DisplayName)
		}

		if body.NextPageToken == "" || body.NextPageToken == pageToken {
			return models.result(), nil
		}
		pageToken = body.NextPageToken
	}

	return nil, fmt.Errorf("%w: still more after %d pages", ErrTooManyPages, maxPages)
}
