package openai

import (
	"strconv"
	"strings"

	"github.com/qunqin24/openchat/apps/server/internal/llm"
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
	out := levelFields(model, r, style)
	return preserveFields(out, model, r, style)
}

// levelFields is the request fields for the level the run picked, if it
// picked one.
func levelFields(model string, r *llm.ReasoningOptions, style ThinkingStyle) map[string]any {
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
	case ThinkingSwitch, ThinkingGLM:
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
	case ThinkingToggle:
		// MiMo has an on/off switch and nothing in between.
		if effort != "" {
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

// preserveFields adds what makes a vendor read the earlier turns' thinking we
// send back (Compat.EchoReasoning). Sending reasoning_content is not enough for
// these two: the vendor ignores it unless told to use it.
//
// Z.ai GLM: `thinking.clear_thinking` defaults to true, which drops earlier
// turns' reasoning_content server-side; false is "Preserved Thinking", and
// asks for the full, unmodified history we send. The parameter is GLM-4.6 and
// later only. https://docs.z.ai/guides/capabilities/thinking-mode and
// https://docs.z.ai/api-reference/llm/chat-completion
//
// DashScope: `preserve_thinking` defaults to false and is what makes the model
// read the reasoning_content in earlier assistant messages; it is a top-level
// parameter, supported by Qwen3.6 and later (Qwen3.8 has it on already) and
// accepted without any reasoning_content in the history.
// https://help.aliyun.com/zh/model-studio/deep-thinking ("传递思考过程") and
// https://help.aliyun.com/zh/model-studio/qwen-api-via-openai-chat-completions
//
// Neither is sent to a request that turns thinking off.
func preserveFields(out map[string]any, model string, r *llm.ReasoningOptions, style ThinkingStyle) map[string]any {
	if r != nil && r.Effort == llm.EffortNone {
		return out
	}
	switch style {
	case ThinkingGLM:
		if !glmKeepsThinking(model) {
			return out
		}
		thinking, _ := out["thinking"].(map[string]any)
		if thinking == nil {
			thinking = map[string]any{}
		}
		if r != nil && (r.Effort != "" || r.Budget > 0) {
			// A level was chosen, so thinking is asked for outright. Left
			// unchosen, the model's own default stands and only the flag is set.
			thinking["type"] = "enabled"
		}
		thinking["clear_thinking"] = false
		if out == nil {
			out = map[string]any{}
		}
		out["thinking"] = thinking
	case ThinkingDashScope:
		if !qwenKeepsThinking(model) {
			return out
		}
		if out == nil {
			out = map[string]any{}
		}
		out["preserve_thinking"] = true
	}
	return out
}

// glmKeepsThinking reports whether the model takes `thinking.clear_thinking`:
// GLM-4.6 and later ("glm-4.6", "glm-4.7", "glm-5.1", "glm-5.3-flash").
func glmKeepsThinking(model string) bool {
	major, minor, ok := versionAfter(strings.ToLower(model), "glm-")
	return ok && (major > 4 || (major == 4 && minor >= 6))
}

// qwenKeepsThinking reports whether DashScope documents `preserve_thinking`
// for the model: Qwen3.6 and later ("qwen3.6-plus", "qwen3.7-max", "qwen3.8-flash").
// The list is by family because the docs name snapshots; an older model is left
// alone rather than sent a parameter nothing says it accepts.
func qwenKeepsThinking(model string) bool {
	major, minor, ok := versionAfter(strings.ToLower(model), "qwen")
	return ok && (major > 3 || (major == 3 && minor >= 6))
}

// versionAfter reads "N.M" (M optional) right after prefix, wherever in the
// name prefix first occurs, so "glm-4.6v" and "zai-org/glm-5" both parse.
func versionAfter(name, prefix string) (major, minor int, ok bool) {
	_, rest, found := strings.Cut(name, prefix)
	if !found {
		return 0, 0, false
	}
	digits := func(s string) (int, string, bool) {
		end := 0
		for end < len(s) && s[end] >= '0' && s[end] <= '9' {
			end++
		}
		if end == 0 || end > 4 {
			return 0, s, false
		}
		n, _ := strconv.Atoi(s[:end])
		return n, s[end:], true
	}
	major, rest, ok = digits(rest)
	if !ok {
		return 0, 0, false
	}
	if strings.HasPrefix(rest, ".") {
		if m, _, fine := digits(rest[1:]); fine {
			minor = m
		}
	}
	return major, minor, true
}
