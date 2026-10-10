package anthropic

import (
	"strings"

	"github.com/qunqin/openchat/apps/server/internal/llm"
)

// generation reads the version out of a Claude model id: "claude-opus-4-5-
// 20251101" is 4.5, "claude-3-7-sonnet-20250219" is 3.7, "claude-sonnet-4-
// 20250514" is 4.0 and "claude-fable-5-1" is 5.1. The id is not trusted to
// start with the vendor, since Bedrock ("anthropic.claude-…-v1:0"), Vertex
// ("…@20250929") and routers ("anthropic/claude-opus-4.5") all wrap it.
//
// ok is false for an id that names no version, which every caller reads as
// "do not guess": a model this cannot place keeps whatever it was asked for.
func generation(model string) (major, minor int, ok bool) {
	id := strings.ToLower(model)
	at := strings.Index(id, "claude")
	if at < 0 {
		return 0, 0, false
	}
	fields := strings.FieldsFunc(id[at+len("claude"):], func(r rune) bool {
		return (r < '0' || r > '9') && (r < 'a' || r > 'z')
	})

	var nums []int
	for _, field := range fields {
		n, isVersion := versionNumber(field)
		if !isVersion {
			// Family names ("opus") come before the number or after it
			// ("3-7-sonnet"); once a number has been read, anything else — a
			// date, "v1", "latest" — is past the version.
			if len(nums) > 0 {
				break
			}
			continue
		}
		nums = append(nums, n)
		if len(nums) == 2 {
			break
		}
	}
	switch len(nums) {
	case 0:
		return 0, 0, false
	case 1:
		return nums[0], 0, true
	}
	return nums[0], nums[1], true
}

// versionNumber is a one- or two-digit number. Longer digit runs are dates.
func versionNumber(field string) (int, bool) {
	if field == "" || len(field) > 2 {
		return 0, false
	}
	n := 0
	for _, r := range field {
		if r < '0' || r > '9' {
			return 0, false
		}
		n = n*10 + int(r-'0')
	}
	return n, true
}

// ExtendedThinkingOnly reports whether a Claude model takes thinking only as
// an enabled token budget: 4.5 and earlier (Opus, Sonnet and Haiku alike).
// They reject the adaptive form, and the catalogue cannot say so — it lists
// effort levels for Opus 4.5 beside its budget, and its effort is something
// else there — so a caller that has the model id asks here. 4.6 and later take
// adaptive thinking.
func ExtendedThinkingOnly(model string) bool {
	major, minor, ok := generation(model)
	return ok && (major < 4 || major == 4 && minor <= 5)
}

// thinksByDefault reports whether the model thinks adaptively when the request
// says nothing about it: the Claude 5 family. Earlier models, Opus 4.6 to 4.8
// and Sonnet 4.6 included, do not think unless asked.
func thinksByDefault(model string) bool {
	major, _, ok := generation(model)
	return ok && major >= 5
}

// thinksUnasked reports whether this request leaves thinking to a model that
// does it on its own — the case where only the display needs setting.
func thinksUnasked(reasoning *llm.ReasoningOptions, model string) bool {
	if reasoning != nil && (reasoning.Effort != "" || reasoning.Budget > 0) {
		return false
	}
	return thinksByDefault(model)
}
