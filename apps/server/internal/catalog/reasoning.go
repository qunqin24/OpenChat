package catalog

import (
	"encoding/json"
	"slices"

	"github.com/qunqin/openchat/apps/server/internal/llm"
)

// ReasoningProfile is how a model's thinking is controlled, read from the
// models.dev `reasoning_options` of its entry.
//
// Two controls exist, and a model offers one: a set of effort levels (Claude
// 4.6 and later, Gemini 3, the OpenAI reasoning models), or a token budget
// (Claude 4.5 and earlier, Gemini 2.5). Sending the other is a rejected
// request, which is why the profile, not the adapter, decides.
type ReasoningProfile struct {
	// Efforts are the levels the model accepts, lowest first.
	Efforts []llm.ReasoningEffort
	// Budget is set when the model is controlled by a token budget instead.
	Budget *BudgetRange
	// allowed, when restricted, are the levels an operator lets people choose.
	allowed    []llm.ReasoningEffort
	restricted bool
}

// BudgetRange bounds a thinking budget. Zero Max is no stated upper bound.
type BudgetRange struct {
	Min int
	Max int
}

// effortOrder is every level a provider uses, lowest first. Levels outside it —
// models.dev also lists `default` — are not offered.
var effortOrder = []llm.ReasoningEffort{
	llm.EffortNone, llm.EffortMinimal, llm.EffortLow, llm.EffortMedium,
	llm.EffortHigh, llm.EffortXHigh, llm.EffortMax,
}

// budgetLevels are what a budget-controlled model is offered, and the budget
// each stands for before the model's own bounds apply.
var budgetLevels = map[llm.ReasoningEffort]int{
	llm.EffortLow:    1024,
	llm.EffortMedium: 4096,
	llm.EffortHigh:   16384,
}

// ParseReasoningOptions reads a models.dev `reasoning_options` value. Anything
// unreadable is an empty profile: a model that can only be asked to think by
// default is a safer misreading than a parameter it rejects.
func ParseReasoningOptions(raw []byte) ReasoningProfile {
	var options []struct {
		Type   string   `json:"type"`
		Values []string `json:"values"`
		Min    *int     `json:"min"`
		Max    *int     `json:"max"`
	}
	var profile ReasoningProfile
	if len(raw) == 0 || json.Unmarshal(raw, &options) != nil {
		return profile
	}
	for _, option := range options {
		switch option.Type {
		case "effort":
			listed := map[string]bool{}
			for _, value := range option.Values {
				listed[value] = true
			}
			for _, level := range effortOrder {
				if listed[string(level)] {
					profile.Efforts = append(profile.Efforts, level)
				}
			}
		case "budget_tokens":
			budget := &BudgetRange{}
			if option.Min != nil {
				budget.Min = *option.Min
			}
			if option.Max != nil {
				budget.Max = *option.Max
			}
			profile.Budget = budget
		}
	}
	return profile
}

// Levels are what a person may choose for this model, beyond leaving it to
// the model: its own effort levels, or three budgets standing in for them.
func (p ReasoningProfile) Levels() []llm.ReasoningEffort {
	levels := p.Supported()
	if !p.restricted {
		return levels
	}
	var out []llm.ReasoningEffort
	for _, level := range levels {
		if slices.Contains(p.allowed, level) {
			out = append(out, level)
		}
	}
	return out
}

// Supported are the levels the model itself has, whatever an operator allows.
func (p ReasoningProfile) Supported() []llm.ReasoningEffort {
	if len(p.Efforts) > 0 {
		return p.Efforts
	}
	if p.Budget != nil {
		return []llm.ReasoningEffort{llm.EffortLow, llm.EffortMedium, llm.EffortHigh}
	}
	return nil
}

// Restrict narrows the levels people may choose to allowed; an empty list
// offers none. A model the catalogue gives no levels — a relay's own model,
// say — takes the allowed ones as effort levels: the operator is vouching for
// a model nothing else here can describe.
func (p ReasoningProfile) Restrict(allowed []llm.ReasoningEffort) ReasoningProfile {
	if len(p.Supported()) == 0 {
		for _, level := range effortOrder {
			if slices.Contains(allowed, level) {
				p.Efforts = append(p.Efforts, level)
			}
		}
	}
	p.allowed = slices.Clone(allowed)
	if p.allowed == nil {
		p.allowed = []llm.ReasoningEffort{}
	}
	p.restricted = true
	return p
}

// Offers reports whether the level is one of the model's.
func (p ReasoningProfile) Offers(level llm.ReasoningEffort) bool {
	for _, offered := range p.Levels() {
		if offered == level {
			return true
		}
	}
	return false
}

// Options turns a chosen level into the request this model takes, or nil
// when the model does not offer it.
func (p ReasoningProfile) Options(level llm.ReasoningEffort) *llm.ReasoningOptions {
	if !p.Offers(level) {
		return nil
	}
	if len(p.Efforts) > 0 {
		return &llm.ReasoningOptions{Effort: level}
	}
	budget := budgetLevels[level]
	if budget < p.Budget.Min {
		budget = p.Budget.Min
	}
	if p.Budget.Max > 0 && budget > p.Budget.Max {
		budget = p.Budget.Max
	}
	if budget <= 0 {
		return nil
	}
	return &llm.ReasoningOptions{Budget: budget}
}
