package agent

import (
	"encoding/json"

	"github.com/qunqin/openchat/apps/server/internal/db/sqlc"
	"github.com/qunqin/openchat/apps/server/internal/llm"
)

// Estimating how many tokens a request will take, before it is made.
//
// The rule of thumb itself - four characters to the token, a CJK character to
// one, widened by a safety factor - lives in llm (estimate.go), because the
// usage ledger prices a stopped request with the same figures. What is here is
// the part that knows transcript rows. The figure the provider reports for a
// request is the truth, and the meter in the composer uses that; this is for
// the one decision that has to be made before there is one.

const messageOverhead = llm.MessageOverhead

// EstimateText is the rough token count of a piece of text.
func EstimateText(text string) int { return llm.EstimateText(text) }

// EstimatePart is the rough token count of one block of a message.
func EstimatePart(part llm.Part) int { return llm.EstimatePart(part) }

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
func EstimateTools(tools []llm.Tool) int { return llm.EstimateTools(tools) }

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
	return llm.Widen(n)
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
