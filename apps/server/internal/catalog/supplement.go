package catalog

import (
	"bytes"
	"cmp"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"slices"
)

// supplementJSON holds models a provider serves that models.dev has not listed
// under that provider, in models.dev's own shape so the same decoder reads it
// and the raw entry (prices included) is stored the same way.
//
// OpenAI's `chat-latest` is the case that started it: models.dev lists it under
// relays and Azure, under other spellings, but not as `openai/chat-latest`, so
// an OpenAI channel serving it showed a bare card with no limits or prices.
// Figures come from the provider's own model page
// (developers.openai.com/api/docs/models/chat-latest). The page is silent on
// thinking levels; tried against the API (2026-11-05), every level but
// `medium` is refused with "Supported values are: 'medium'", so that is the
// only one listed.
//
// An entry here only fills a gap: once models.dev lists the same id under the
// same provider, upstream's entry is the one kept and this one is dead weight
// worth deleting.
//
//go:embed supplement.json
var supplementJSON []byte

// supplement is the decoded file. It is decoded once at start-up, and a file
// that does not decode is a build mistake a test catches, so a panic is the
// right answer rather than a catalogue silently missing models.
var supplement = mustDecodeSupplement()

// supplementVersion changes whenever the file does. The refresh state records
// it, so a server that ships a new entry fetches afresh instead of trusting a
// 304 for a snapshot written without it.
var supplementVersion = func() string {
	sum := sha256.Sum256(supplementJSON)
	return hex.EncodeToString(sum[:8])
}()

func mustDecodeSupplement() []Provider {
	providers, err := decode(bytes.NewReader(supplementJSON))
	if err != nil {
		panic(err)
	}
	return providers
}

// withSupplement adds the supplement's models to the providers upstream sent,
// skipping any model upstream already has under that provider. A provider
// upstream does not list at all is left out: its name and docs would have to
// be invented here too.
func withSupplement(providers []Provider) []Provider {
	for _, extra := range supplement {
		i := slices.IndexFunc(providers, func(p Provider) bool { return p.ID == extra.ID })
		if i < 0 {
			continue
		}
		provider := providers[i]
		for _, model := range extra.Models {
			if slices.ContainsFunc(provider.Models, func(m Model) bool { return m.ID == model.ID }) {
				continue
			}
			provider.Models = append(provider.Models, model)
		}
		slices.SortFunc(provider.Models, func(a, b Model) int { return cmp.Compare(a.ID, b.ID) })
		providers[i] = provider
	}
	return providers
}
