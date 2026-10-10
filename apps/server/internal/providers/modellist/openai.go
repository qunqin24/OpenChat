package modellist

import (
	"context"
	"net/http"
)

// openAIDefaultBase is the vendor's own endpoint. Anything else speaking this
// protocol — a relay, vLLM, Ollama — arrives as a base URL instead.
const openAIDefaultBase = "https://api.openai.com/v1"

// openAI lists models the OpenAI way: one unpaged GET, bearer authentication.
//
//	GET {base}/models
//	Authorization: Bearer <key>
//	{"object":"list","data":[{"id":"gpt-4o","owned_by":"openai"}]}
type openAI struct{}

var _ dialect = openAI{}

type openAIList struct {
	Data []struct {
		ID      string `json:"id"`
		OwnedBy string `json:"owned_by"`
	} `json:"data"`
}

func (openAI) list(ctx context.Context, f fetcher) ([]Model, error) {
	header := http.Header{}
	if f.key != "" {
		header.Set("Authorization", "Bearer "+f.key)
	}

	var body openAIList
	if err := f.get(ctx, "/models", nil, header, &body); err != nil {
		return nil, err
	}

	// This endpoint returns everything in one answer — `object: list` is a
	// shape, not a cursor — so there is no paging to do. A relay that decides
	// to paginate it is inventing something the protocol does not have.
	var models collector
	for _, entry := range body.Data {
		// owned_by is read and discarded: it names who published the model, not
		// what it is called, and every endpoint fills it in differently.
		models.add(entry.ID, "")
	}
	return models.result(), nil
}
