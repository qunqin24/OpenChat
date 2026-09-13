package catalog

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"time"
)

// DefaultURL is where the catalogue comes from.
const DefaultURL = "https://models.dev/api.json"

const (
	// fetchTimeout bounds one refresh. The file was 4.6 MB when this was
	// written, so the budget covers a slow transfer rather than just a slow
	// handshake.
	fetchTimeout = 60 * time.Second

	// maxBodyBytes stops a misbehaving or impersonated endpoint from feeding us
	// an unbounded body. Roomy enough that upstream would have to grow an order
	// of magnitude before anyone noticed the cap.
	maxBodyBytes = 64 << 20
)

// defaultHTTP is shared by every Client built with NewClient so that repeated
// refreshes reuse the connection. It sets no timeout of its own: the deadline
// comes from the context, which a caller can shorten but not lose.
var defaultHTTP = &http.Client{}

// Client fetches the upstream catalogue.
//
// URL and HTTP are fields rather than constants so a test can point the client
// at an httptest server and never reach the network.
type Client struct {
	URL     string
	HTTP    *http.Client
	Timeout time.Duration
}

func NewClient() *Client {
	return &Client{URL: DefaultURL, HTTP: defaultHTTP, Timeout: fetchTimeout}
}

// Result is one answer from upstream.
type Result struct {
	Providers []Provider
	ETag      string
	// NotModified is set when the server answered our If-None-Match with a 304:
	// the snapshot already in the database is the current one, and Providers is
	// empty because nothing was sent.
	NotModified bool
}

// Fetch reads the catalogue, passing etag back as If-None-Match when we have
// one. An empty etag asks for the file unconditionally.
func (c *Client) Fetch(ctx context.Context, etag string) (Result, error) {
	ctx, cancel := context.WithTimeout(ctx, cmp.Or(c.Timeout, fetchTimeout))
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.URL, nil)
	if err != nil {
		return Result{}, err
	}
	req.Header.Set("Accept", "application/json")
	// The common case is that nothing changed since yesterday. Asking
	// conditionally turns that case into a 304 with no body, which saves both
	// ends several megabytes for an answer we already have.
	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}

	resp, err := cmp.Or(c.HTTP, defaultHTTP).Do(req)
	if err != nil {
		return Result{}, err
	}
	defer func() { _ = resp.Body.Close() }()

	switch {
	case resp.StatusCode == http.StatusNotModified:
		return Result{ETag: etag, NotModified: true}, nil
	case resp.StatusCode != http.StatusOK:
		return Result{}, fmt.Errorf("models.dev returned %s", resp.Status)
	}

	providers, err := decode(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil {
		return Result{}, err
	}
	return Result{Providers: providers, ETag: resp.Header.Get("ETag")}, nil
}

// providerJSON mirrors one upstream provider entry. Fields we do not name are
// ignored, which is what lets upstream add one without breaking a refresh.
type providerJSON struct {
	ID     string                     `json:"id"`
	Name   string                     `json:"name"`
	Doc    string                     `json:"doc"`
	Env    []string                   `json:"env"`
	Models map[string]json.RawMessage `json:"models"`
}

type modelJSON struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	Description      string `json:"description"`
	Family           string `json:"family"`
	Attachment       bool   `json:"attachment"`
	Reasoning        bool   `json:"reasoning"`
	ToolCall         bool   `json:"tool_call"`
	StructuredOutput bool   `json:"structured_output"`
	Knowledge        string `json:"knowledge"`
	ReleaseDate      string `json:"release_date"`
	Modalities       struct {
		Input  []string `json:"input"`
		Output []string `json:"output"`
	} `json:"modalities"`
	Limit struct {
		Context int64 `json:"context"`
		Output  int64 `json:"output"`
	} `json:"limit"`
	Cost struct {
		Input  float64 `json:"input"`
		Output float64 `json:"output"`
	} `json:"cost"`
}

// decode reads the upstream object one provider at a time.
//
// The file is a single JSON object of provider id to provider, several
// megabytes of it. Unmarshalling it in one go would hold the whole document in
// memory as bytes and again as values; stepping through the top-level keys with
// a decoder holds one provider at a time.
func decode(r io.Reader) ([]Provider, error) {
	dec := json.NewDecoder(r)

	tok, err := dec.Token()
	if err != nil {
		return nil, fmt.Errorf("catalogue: read opening token: %w", err)
	}
	if delim, ok := tok.(json.Delim); !ok || delim != '{' {
		return nil, fmt.Errorf("catalogue: expected a JSON object, got %v", tok)
	}

	var providers []Provider
	for dec.More() {
		key, err := dec.Token()
		if err != nil {
			return nil, fmt.Errorf("catalogue: read provider key: %w", err)
		}
		id, ok := key.(string)
		if !ok {
			return nil, fmt.Errorf("catalogue: provider key is %T, not a string", key)
		}

		var entry providerJSON
		if err := dec.Decode(&entry); err != nil {
			return nil, fmt.Errorf("catalogue: decode provider %q: %w", id, err)
		}

		provider, err := entry.provider(id)
		if err != nil {
			return nil, err
		}
		providers = append(providers, provider)
	}

	// Maps iterate in a random order, so without this a refresh would write the
	// same catalogue in a different order every time.
	slices.SortFunc(providers, func(a, b Provider) int { return cmp.Compare(a.ID, b.ID) })
	return providers, nil
}

// provider flattens an entry, taking its id from the key rather than the body.
// The key is how the file addresses the entry and how every other reference to
// it is spelled, so an entry whose inner id disagrees still lands where callers
// will look for it.
func (p providerJSON) provider(key string) (Provider, error) {
	out := Provider{
		ID:     key,
		Name:   cmp.Or(p.Name, key),
		Doc:    p.Doc,
		Env:    p.Env,
		Models: make([]Model, 0, len(p.Models)),
	}

	for id, raw := range p.Models {
		var entry modelJSON
		if err := json.Unmarshal(raw, &entry); err != nil {
			return Provider{}, fmt.Errorf("catalogue: decode model %q/%q: %w", key, id, err)
		}
		out.Models = append(out.Models, Model{
			ID:               id,
			Name:             cmp.Or(entry.Name, id),
			Description:      entry.Description,
			Family:           entry.Family,
			Attachment:       entry.Attachment,
			Reasoning:        entry.Reasoning,
			ToolCall:         entry.ToolCall,
			StructuredOutput: entry.StructuredOutput,
			Knowledge:        entry.Knowledge,
			ReleaseDate:      entry.ReleaseDate,
			InputModalities:  entry.Modalities.Input,
			OutputModalities: entry.Modalities.Output,
			ContextLimit:     entry.Limit.Context,
			OutputLimit:      entry.Limit.Output,
			InputCost:        entry.Cost.Input,
			OutputCost:       entry.Cost.Output,
			Raw:              raw,
		})
	}

	slices.SortFunc(out.Models, func(a, b Model) int { return cmp.Compare(a.ID, b.ID) })
	return out, nil
}
