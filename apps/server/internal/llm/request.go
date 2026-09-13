package llm

import "encoding/json"

// Request is one model turn's input. It is already resolved: the context
// builder has picked the history, inlined the authorised attachments and
// assembled the tool list, so an adapter never has to ask who is calling.
type Request struct {
	// Model is the catalog id to ask for. Which endpoint and credentials serve
	// it is the adapter's configuration, not a field the caller — and certainly
	// not the model — gets to redirect.
	Model    string
	Messages []Message
	Tools    []Tool
	Options  Options
}

// Tool is a definition the model may ask for. Being listed here is visibility,
// not permission: the registry decides what exists, and execution re-checks the
// name, the arguments and the ownership of everything they point at.
type Tool struct {
	Name        string
	Description string

	// Parameters is a JSON Schema object, kept raw because it travels to the
	// provider untouched. The schema that actually validates a call lives with
	// the tool implementation, where execution is authorised.
	Parameters json.RawMessage
}

// ToolChoice constrains whether the model may call a tool this turn.
type ToolChoice string

const (
	// ToolChoiceAuto is the zero value: let the model decide.
	ToolChoiceAuto     ToolChoice = ""
	ToolChoiceNone     ToolChoice = "none"
	ToolChoiceRequired ToolChoice = "required"
)

// ReasoningEffort is how hard a reasoning model should think. The catalog says
// which values a model accepts; sending one it does not know is a request error,
// not something to paper over.
type ReasoningEffort string

const (
	EffortLow    ReasoningEffort = "low"
	EffortMedium ReasoningEffort = "medium"
	EffortHigh   ReasoningEffort = "high"
)

type ReasoningOptions struct {
	Effort ReasoningEffort
}

// Options are the sampling knobs.
//
// The numeric ones are pointers so that "leave the provider's default alone"
// and "I deliberately want zero" stay different requests — temperature 0 is a
// very real choice, and a plain float64 cannot express the absence of one.
type Options struct {
	Temperature *float64
	TopP        *float64
	MaxTokens   *int
	Stop        []string
	ToolChoice  ToolChoice

	// Reasoning is nil when the caller has no opinion, which is not the same as
	// asking a reasoning model to stop thinking.
	Reasoning *ReasoningOptions

	// Extra carries vendor knobs a compat configuration passes through
	// verbatim. Adapters must refuse to let it overwrite the fields above: a
	// stray extra key that silently retargets the model or the tool list turns
	// a configuration typo into a different request than the one we authorised.
	Extra map[string]any
}
