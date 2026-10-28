package catalog

import (
	"slices"
	"strings"

	"github.com/qunqin24/openchat/apps/server/internal/llm"
)

// Kind is what a model is for. Only text models are offered in chat; the
// others are listed so an operator can see and correct how a model was read.
type Kind string

const (
	KindText      Kind = "text"
	KindImage     Kind = "image"
	KindEmbedding Kind = "embedding"
	KindRerank    Kind = "rerank"
)

// DefaultKind reads a model's kind from what the catalogue says it outputs,
// and failing that from its id: models.dev lists few embedding or rerank
// models, and a relay's own list says nothing but the id.
func DefaultKind(modelID string, outputModalities []string) Kind {
	id := strings.ToLower(modelID)
	switch {
	case strings.Contains(id, "rerank"):
		return KindRerank
	case strings.Contains(id, "embed"):
		return KindEmbedding
	case len(outputModalities) > 0 && !slices.Contains(outputModalities, "text") &&
		slices.Contains(outputModalities, "image"):
		return KindImage
	}
	return KindText
}

// Modalities a model may take besides text, in the order they are shown.
var Modalities = []string{"image", "audio", "video", "pdf"}

// Defaults are what the catalogue says about a model, before an operator has
// a say.
type Defaults struct {
	Kind            Kind
	Reasoning       bool
	ToolCall        bool
	InputModalities []string
	ContextLimit    int64
	InputLimit      int64
	OutputLimit     int64
	Profile         ReasoningProfile
}

// UnknownDefaults are a model the catalogue has never heard of. Tools stay on:
// that is how such a model was always called, and an operator who knows it
// cannot take them switches them off.
func UnknownDefaults(modelID string) Defaults {
	return Defaults{Kind: DefaultKind(modelID, nil), ToolCall: true}
}

// Override is what an operator has decided. A nil field follows the catalogue.
type Override struct {
	Kind             *Kind
	Reasoning        *bool
	ToolCall         *bool
	InputModalities  []string // nil follows the catalogue; empty is text only
	ContextLimit     *int64
	InputLimit       *int64
	OutputLimit      *int64
	ReasoningEfforts []llm.ReasoningEffort // nil offers every level; empty offers none
	AdminOnly        bool                  // offered to administrators only; the catalogue has no say
}

// Settings are how a model is actually offered and called.
type Settings struct {
	Kind            Kind
	Reasoning       bool
	ToolCall        bool
	InputModalities []string
	ContextLimit    int64
	InputLimit      int64
	OutputLimit     int64
	// OutputLimitSet is whether the operator chose the output limit, which is
	// then sent with every request rather than only where a protocol needs one.
	OutputLimitSet bool
	// Profile is restricted to the levels people may choose: Levels, Offers and
	// Options all answer within it, so the picker, the request check and the
	// wire agree.
	Profile ReasoningProfile
	// AdminOnly keeps the model from everyone but administrators. The server's
	// own tasks, which no person asks for, are not held back by it.
	AdminOnly bool
}

// Resolve applies an operator's decisions to the catalogue's.
func Resolve(d Defaults, o Override) Settings {
	s := Settings{
		Kind:            d.Kind,
		Reasoning:       d.Reasoning,
		ToolCall:        d.ToolCall,
		InputModalities: textless(d.InputModalities),
		ContextLimit:    d.ContextLimit,
		InputLimit:      d.InputLimit,
		OutputLimit:     d.OutputLimit,
		Profile:         d.Profile,
		AdminOnly:       o.AdminOnly,
	}
	if o.Kind != nil {
		s.Kind = *o.Kind
	}
	if o.Reasoning != nil {
		s.Reasoning = *o.Reasoning
	}
	if o.ToolCall != nil {
		s.ToolCall = *o.ToolCall
	}
	if o.InputModalities != nil {
		s.InputModalities = textless(o.InputModalities)
	}
	if o.ContextLimit != nil {
		s.ContextLimit = *o.ContextLimit
	}
	if o.InputLimit != nil {
		s.InputLimit = *o.InputLimit
	}
	if o.OutputLimit != nil {
		s.OutputLimit = *o.OutputLimit
		s.OutputLimitSet = true
	}
	switch {
	case !s.Reasoning && (o.Reasoning != nil || len(d.Profile.Levels()) == 0):
		// Switched off, or nothing to offer: no thinking control at all. A
		// model the catalogue says does not reason but that has levels anyway
		// keeps them — the flag and the options are separate upstream fields.
		s.Profile = d.Profile.Restrict([]llm.ReasoningEffort{})
	case o.ReasoningEfforts != nil:
		s.Profile = d.Profile.Restrict(o.ReasoningEfforts)
	}
	return s
}

// textless drops "text", which every chat model takes and is not a choice.
func textless(modalities []string) []string {
	out := make([]string, 0, len(modalities))
	for _, m := range Modalities {
		if slices.Contains(modalities, m) {
			out = append(out, m)
		}
	}
	return out
}
