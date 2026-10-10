package openai

import (
	"strings"

	"github.com/qunqin/openchat/apps/server/internal/llm"
)

// thinkingFields turns a chosen thinking level into the request fields a
// vendor takes it in. It translates exactly what the catalogue picked — an
// effort or a budget — and never converts one into the other; levels a model
// does not have never get here, because the caller already dropped them.
//
// Each case is what the vendor's own documentation says (DashScope: Deep
// thinking and the Chat Completions reference; DeepSeek: Thinking Mode;
// Z.ai: Chat Completion's `thinking` and `reasoning_effort`; Moonshot:
// Thinking Models; OpenRouter: Reasoning Tokens), not a guess from the
// model's name.
func thinkingFields(model string, r *llm.ReasoningOptions, style ThinkingStyle) map[string]any {
	if r == nil || (r.Effort == "" && r.Budget <= 0) {
		return nil
	}
	effort := string(r.Effort)
	off := r.Effort == llm.EffortNone
	out := map[string]any{}

	switch style {
	case ThinkingDashScope:
		// Hybrid models switch on `enable_thinking`; the level or budget only
		// means something while it is true.
		out["enable_thinking"] = !off
		switch {
		case off:
		case effort != "":
			out["reasoning_effort"] = effort
		default:
			out["thinking_budget"] = r.Budget
		}
	case ThinkingSwitch:
		switch {
		case off:
			out["thinking"] = map[string]any{"type": "disabled"}
		case effort != "":
			out["reasoning_effort"] = effort
		}
	case ThinkingMoonshot:
		switch {
		case strings.Contains(strings.ToLower(model), "kimi-k3"):
			// K3 always thinks; it has a level and nothing to switch.
			if effort != "" && !off {
				out["reasoning_effort"] = effort
			}
		case effort != "":
			// The K2 models take no level at all, and only K2.6 can be turned
			// off; the catalogue offers `none` only where it works.
			kind := "enabled"
			if off {
				kind = "disabled"
			}
			out["thinking"] = map[string]any{"type": kind}
		}
	case ThinkingOpenRouter:
		if effort != "" {
			out["reasoning"] = map[string]any{"effort": effort}
		} else {
			out["reasoning"] = map[string]any{"max_tokens": r.Budget}
		}
	default:
		if effort != "" {
			out["reasoning_effort"] = effort
		}
	}
	return out
}
