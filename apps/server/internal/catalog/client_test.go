package catalog

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

// serveFixture stands in for models.dev. Every test in this package points a
// client at one of these, so nothing here touches the network.
func serveFixture(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return &Client{URL: server.URL, HTTP: server.Client()}
}

func fixture(t *testing.T) []byte {
	t.Helper()
	raw, err := os.ReadFile("testdata/api.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	return raw
}

func fetchFixture(t *testing.T) Result {
	t.Helper()
	client := serveFixture(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", `"v1"`)
		_, _ = w.Write(fixture(t))
	})

	result, err := client.Fetch(context.Background(), "")
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	return result
}

func TestFetchDecodesCatalogue(t *testing.T) {
	result := fetchFixture(t)

	if result.ETag != `"v1"` {
		t.Errorf("etag = %q, want %q", result.ETag, `"v1"`)
	}
	if result.NotModified {
		t.Error("a 200 was reported as not modified")
	}

	// Providers and models come out sorted, so two refreshes of the same file
	// write the same rows in the same order.
	ids := make([]string, 0, len(result.Providers))
	for _, provider := range result.Providers {
		ids = append(ids, provider.ID)
	}
	want := []string{"anthropic", "openai", "sparse"}
	if len(ids) != len(want) {
		t.Fatalf("providers = %v, want %v", ids, want)
	}
	for i := range want {
		if ids[i] != want[i] {
			t.Fatalf("providers = %v, want %v", ids, want)
		}
	}

	openai := result.Providers[1]
	if openai.Name != "OpenAI" || openai.Doc == "" {
		t.Errorf("provider = %+v, want the name and doc from the file", openai)
	}
	if len(openai.Env) != 1 || openai.Env[0] != "OPENAI_API_KEY" {
		t.Errorf("env = %v, want [OPENAI_API_KEY]", openai.Env)
	}
	if len(openai.Models) != 2 || openai.Models[0].ID != "gpt-4o" {
		t.Fatalf("models = %+v, want gpt-4o first", openai.Models)
	}

	mini := openai.Models[1]
	switch {
	case mini.Name != "GPT-4o mini":
		t.Errorf("name = %q", mini.Name)
	case mini.Family != "gpt-4o":
		t.Errorf("family = %q", mini.Family)
	case !mini.Attachment || !mini.ToolCall || !mini.StructuredOutput || mini.Reasoning:
		t.Errorf("capabilities = %+v", mini)
	case mini.ContextLimit != 128000 || mini.OutputLimit != 16384:
		t.Errorf("limits = %d/%d", mini.ContextLimit, mini.OutputLimit)
	case mini.InputCost != 0.15 || mini.OutputCost != 0.6:
		t.Errorf("cost = %v/%v", mini.InputCost, mini.OutputCost)
	case mini.Knowledge != "2023-09" || mini.ReleaseDate != "2024-07-18":
		t.Errorf("dates = %q/%q", mini.Knowledge, mini.ReleaseDate)
	case len(mini.InputModalities) != 2 || len(mini.OutputModalities) != 1:
		t.Errorf("modalities = %v/%v", mini.InputModalities, mini.OutputModalities)
	}

	// The raw entry is kept whole, including the fields the struct does not
	// name, which is what a later feature reads without waiting for a refresh.
	var raw map[string]any
	if err := json.Unmarshal(mini.Raw, &raw); err != nil {
		t.Fatalf("raw is not an object: %v", err)
	}
	if raw["cache_read"] != nil {
		t.Errorf("raw = %v, cache_read belongs to cost", raw)
	}
	if cost, ok := raw["cost"].(map[string]any); !ok || cost["cache_read"] != 0.075 {
		t.Errorf("raw cost = %v, want the untouched upstream object", raw["cost"])
	}
}

// An entry with nothing in it still has to produce a usable row: the name falls
// back to the id, and every absent field is a zero rather than a decode error.
func TestFetchToleratesSparseEntries(t *testing.T) {
	result := fetchFixture(t)

	sparse := result.Providers[2]
	if sparse.ID != "sparse" || len(sparse.Models) != 1 {
		t.Fatalf("provider = %+v", sparse)
	}
	bare := sparse.Models[0]
	if bare.ID != "bare" || bare.Name != "bare" {
		t.Errorf("model = %+v, want the key as both id and name", bare)
	}
	if bare.InputModalities != nil || bare.ContextLimit != 0 {
		t.Errorf("model = %+v, want zeroes for what the file did not say", bare)
	}
}

func TestFetchSendsConditionalRequest(t *testing.T) {
	var seen string
	client := serveFixture(t, func(w http.ResponseWriter, r *http.Request) {
		seen = r.Header.Get("If-None-Match")
		w.WriteHeader(http.StatusNotModified)
	})

	result, err := client.Fetch(context.Background(), `"v1"`)
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if seen != `"v1"` {
		t.Errorf("If-None-Match = %q, want the stored etag", seen)
	}
	if !result.NotModified {
		t.Error("a 304 was not reported as not modified")
	}
	// The etag survives a 304 so the next fetch can ask the same question.
	if result.ETag != `"v1"` {
		t.Errorf("etag = %q, want it carried through the 304", result.ETag)
	}
}

func TestFetchRejectsErrorResponses(t *testing.T) {
	client := serveFixture(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "down", http.StatusBadGateway)
	})

	if _, err := client.Fetch(context.Background(), ""); err == nil {
		t.Fatal("a 502 was accepted as a catalogue")
	}
}

func TestFetchRejectsNonObjectBodies(t *testing.T) {
	client := serveFixture(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`["not", "a", "catalogue"]`))
	})

	if _, err := client.Fetch(context.Background(), ""); err == nil {
		t.Fatal("a JSON array was accepted as a catalogue")
	}
}
