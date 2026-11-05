package catalog

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

func findModel(providers []Provider, providerID, modelID string) *Model {
	for _, p := range providers {
		if p.ID != providerID {
			continue
		}
		for i := range p.Models {
			if p.Models[i].ID == modelID {
				return &p.Models[i]
			}
		}
	}
	return nil
}

// The figures are OpenAI's own model page; a typo here is a wrong price on
// every usage row, so they are pinned.
func TestSupplementCarriesChatLatest(t *testing.T) {
	m := findModel(supplement, "openai", "chat-latest")
	if m == nil {
		t.Fatal("supplement has no openai/chat-latest")
	}
	if m.ContextLimit != 400000 || m.OutputLimit != 128000 || m.InputCost != 5 || m.OutputCost != 30 {
		t.Errorf("chat-latest = %+v", m)
	}
	if !m.ToolCall || !m.Attachment || len(m.Raw) == 0 {
		t.Errorf("chat-latest capabilities or raw entry missing: %+v", m)
	}
	if got := ParseReasoningOptions(rawField(t, m.Raw, "reasoning_options")).Levels(); len(got) != 1 || got[0] != "medium" {
		t.Errorf("chat-latest levels = %v, want medium only", got)
	}
}

func TestRefreshAddsSupplementToUpstream(t *testing.T) {
	store := &fakeStore{}
	if _, err := syncer(t, store, servingFixture(t)).Refresh(context.Background()); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if findModel(store.providers, "openai", "chat-latest") == nil {
		t.Error("the stored snapshot lacks the supplement's chat-latest")
	}
	if findModel(store.providers, "openai", "gpt-4o") == nil {
		t.Error("the supplement displaced upstream's models")
	}
	if findModel(store.providers, "anthropic", "chat-latest") != nil {
		t.Error("the supplement leaked into another provider")
	}
}

// Once models.dev lists the model, its entry is the one kept.
func TestUpstreamWinsOverSupplement(t *testing.T) {
	providers := withSupplement([]Provider{{ID: "openai", Models: []Model{{ID: "chat-latest", Name: "Upstream"}}}})
	if len(providers[0].Models) != 1 || providers[0].Models[0].Name != "Upstream" {
		t.Errorf("models = %+v, want upstream's entry alone", providers[0].Models)
	}
	if got := withSupplement([]Provider{{ID: "anthropic"}}); len(got) != 1 || len(got[0].Models) != 0 {
		t.Errorf("a provider without supplement entries changed: %+v", got)
	}
}

// A snapshot written before this build's supplement is rewritten even though
// it is young and upstream would answer 304.
func TestNewSupplementForcesAFullFetch(t *testing.T) {
	store := &fakeStore{state: State{FetchedAt: time.Now().Add(-time.Hour), ETag: `"v1"`}}
	var conditional bool
	s := syncer(t, store, func(w http.ResponseWriter, r *http.Request) {
		conditional = r.Header.Get("If-None-Match") != ""
		if conditional {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		_, _ = w.Write(fixture(t))
	})

	state, err := s.RefreshIfStale(context.Background())
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if conditional || store.replaced != 1 {
		t.Errorf("conditional = %v, replaced = %d; want an unconditional fetch that rewrote the snapshot", conditional, store.replaced)
	}
	if state.Supplement != supplementVersion || state.Stale(time.Now()) {
		t.Errorf("state = %+v, want it fresh and recording the supplement", state)
	}
}

func rawField(t *testing.T, raw []byte, key string) []byte {
	t.Helper()
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatalf("raw entry: %v", err)
	}
	return fields[key]
}
