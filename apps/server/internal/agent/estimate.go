package agent

import (
	"encoding/json"
	"unicode"

	"github.com/qunqin/openchat/apps/server/internal/db/sqlc"
	"github.com/qunqin/openchat/apps/server/internal/llm"
)

// Estimating how many tokens a request will take, before it is made.
//
// A tokenizer is a model's own and there are dozens of them; what is wanted
// here is only to know that a request is getting close to a window, early
// enough to do something about it. So the count is a rule of thumb that errs
// high: a CJK character is about one token, anything else about a quarter of one
// (four characters to the token is the usual figure for English). The sum is
// then multiplied by estimateSafety. The figure the provider reports for a
// request is the truth, and the meter in the composer uses that; this is for the
// one decision that has to be made before there is one.

const (
	// estimateSafety widens every estimate: guessing low means a request that
	// is refused for its length, guessing high costs one early summary.
	estimateSafety = 1.25
	// messageOverhead is what a message costs besides its text: the role and
	// the framing a wire format puts around it.
	messageOverhead = 4
	// attachmentTokens stands in for a file. A picture costs a model anywhere
	// from a few hundred to a few thousand tokens by its size, and a PDF by
	// its pages; the bytes say little about either.
	attachmentTokens = 1500
)

// EstimateText is the rough token count of a piece of text: a CJK character
// (and the punctuation and full-width forms that go with them) at one token,
// everything else at four characters to the token, rounded up.
func EstimateText(text string) int {
	cjk, other := 0, 0
	for _, r := range text {
		if isCJK(r) {
			cjk++
		} else {
			other++
		}
	}
	return cjk + (other+3)/4
}

func isCJK(r rune) bool {
	switch {
	case r < 0x2E80: // nothing below is CJK; the common case, decided at once
		return false
	case unicode.Is(unicode.Han, r), unicode.Is(unicode.Hiragana, r), unicode.Is(unicode.Katakana, r), unicode.Is(unicode.Hangul, r):
		return true
	case r >= 0x3000 && r <= 0x303F, // CJK symbols and punctuation
		r >= 0xFF00 && r <= 0xFFEF: // half- and full-width forms
		return true
	}
	return false
}

// EstimatePart is the rough token count of one block of a message.
func EstimatePart(part llm.Part) int {
	switch p := part.(type) {
	case llm.TextPart:
		return EstimateText(p.Text)
	case llm.ToolCallPart:
		args := p.RawArguments
		if args == "" {
			args = string(p.Arguments)
		}
		return EstimateText(p.Name) + EstimateText(args)
	case llm.ToolResultPart:
		return EstimateText(p.Content)
	case llm.AttachmentPart:
		return attachmentTokens
	}
	// Reasoning is not counted: providers drop the thinking of earlier turns
	// from the window, and an estimate meant to be early should not be early
	// for text that is not there.
	return 0
}

// EstimateRow is the rough token count of one transcript row as a request
// would carry it. An assistant row's output as the provider counted it stands
// in for the estimate when there is one: it is the better figure, and what the
// model wrote is the bulk of a long conversation.
func EstimateRow(row sqlc.Message) int {
	if row.Role == sqlc.MessageRoleAssistant {
		if row.StopReason != nil && (*row.StopReason == StopError || *row.StopReason == StopCancelled) {
			return 0 // never sent (BuildContext)
		}
		if len(row.Usage) > 0 {
			var u usage
			if json.Unmarshal(row.Usage, &u) == nil && u.OutputTokens > 0 {
				return u.OutputTokens + messageOverhead
			}
		}
	}
	parts, err := DecodeParts(row.Parts)
	if err != nil || len(parts) == 0 {
		return EstimateText(row.Content) + messageOverhead
	}
	n := messageOverhead
	for _, part := range parts {
		n += EstimatePart(part)
	}
	return n
}

// EstimateTools is the rough token count of the tool definitions a request
// offers.
func EstimateTools(tools []llm.Tool) int {
	n := 0
	for _, tool := range tools {
		n += EstimateText(tool.Name) + EstimateText(tool.Description) + EstimateText(string(tool.Parameters)) + messageOverhead
	}
	return n
}

// EstimateRequest is the estimate for a request: the system prompt, the summary
// if there is one, the rows, and the tools offered - widened by the safety
// factor.
func EstimateRequest(system, summary string, rows []sqlc.Message, tools []llm.Tool) int {
	n := EstimateText(system) + EstimateTools(tools)
	if summary != "" {
		n += EstimateText(summary) + messageOverhead
	}
	for _, row := range rows {
		n += EstimateRow(row)
	}
	return int(float64(n) * estimateSafety)
}

// The window a model is taken to have when nothing says, and the shares of it
// that start a compression. The second is higher than the first on purpose:
// right after a compression the context is far below the window, and had it
// the same threshold it could cross it again with the next few messages and
// pay for another summary at once. Once a conversation has a summary the bar is
// raised, which leaves room to grow between two compressions.
const (
	DefaultContextWindow = 128_000
	compressShare        = 0.50
	compressShareAgain   = 0.65
)

// CompressThreshold is the estimate above which a request is compressed first,
// for a model with the given window (0 when unknown) and whether the
// conversation already has a summary.
func CompressThreshold(window int64, summarised bool) int {
	if window <= 0 {
		window = DefaultContextWindow
	}
	share := compressShare
	if summarised {
		share = compressShareAgain
	}
	return int(float64(window) * share)
}
