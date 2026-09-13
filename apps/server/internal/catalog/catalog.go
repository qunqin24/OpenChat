// Package catalog keeps a local snapshot of the models.dev catalogue: which
// providers exist, which models each one offers, and what those models can do.
//
// The snapshot is a cache of somebody else's file, never a source of truth.
// Whether a model is switched on for this installation is a separate decision,
// kept in model_settings, which a sync never touches.
package catalog

import (
	"encoding/json"
	"time"
)

// StaleAfter is how old a snapshot may get before a refresh is worth making.
// Upstream changes when a provider ships a model, which is a matter of days, so
// a day late costs nothing and a tighter interval would only mean more requests
// to somebody else's server for the same answer.
const StaleAfter = 24 * time.Hour

// Provider is one entry in the upstream file, with its models flattened out of
// the map they arrive in.
type Provider struct {
	ID     string
	Name   string
	Doc    string
	Env    []string
	Models []Model
}

// Model is the part of an upstream entry we query on. Raw carries the entry
// verbatim, so a later feature can read a field this struct does not name
// without waiting for a refresh to store it.
type Model struct {
	ID               string
	Name             string
	Description      string
	Family           string
	Attachment       bool
	Reasoning        bool
	ToolCall         bool
	StructuredOutput bool
	Knowledge        string
	ReleaseDate      string
	InputModalities  []string
	OutputModalities []string
	ContextLimit     int64
	OutputLimit      int64
	InputCost        float64
	OutputCost       float64
	Raw              json.RawMessage
}

// State is what a refresh leaves behind: when the catalogue was last fetched,
// the entity tag to ask about next time, and why the most recent attempt failed
// if it did.
type State struct {
	// FetchedAt stays zero until a fetch has succeeded, which is what makes a
	// database that has never seen the catalogue read as stale.
	FetchedAt time.Time `json:"fetchedAt,omitzero"`
	ETag      string    `json:"etag,omitempty"`
	// LastError survives until a refresh succeeds, because the snapshot it
	// failed to replace is still the one being served and that is worth saying.
	LastError string `json:"lastError,omitempty"`
}

// Stale reports whether the snapshot is old enough to be worth replacing.
func (s State) Stale(now time.Time) bool {
	return now.Sub(s.FetchedAt) >= StaleAfter
}
