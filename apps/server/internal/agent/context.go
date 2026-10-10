package agent

import (
	"github.com/qunqin/openchat/apps/server/internal/db/sqlc"
	"github.com/qunqin/openchat/apps/server/internal/llm"
)

// Stop reasons as stored in `messages.stop_reason`.
const (
	StopStop          = "stop"
	StopLength        = "length"
	StopToolCalls     = "tool_calls"
	StopContentFilter = "content_filter"
	StopCancelled     = "cancelled"
	StopError         = "error"
)

// interruptedResult stands in for a tool call that has no result row.
const interruptedResult = "The tool call was interrupted before it returned a result."

// BuildContext turns transcript rows into a model request's messages.
//
// The transcript is what happened, in the order it was committed. A model
// request has stricter rules than that, and these are pi-durable's context
// derivation rules (spec §2.1) applied to our rows:
//
//   - A failed or cancelled response is shown to the user but never sent back:
//     half an answer, or an error, is not context the next request should build
//     on, and a cancelled response may hold a tool call that never ran.
//   - Every tool call is followed directly by its result, in call order — the
//     providers reject anything else. Results are committed in the order the
//     tools finished, so they are moved here rather than at write time.
//   - A call with no result gets a synthesized error result. That happens at an
//     interrupted tail or where the history window cut between the two.
//   - A result whose call is not in the window is dropped: sent alone it is a
//     protocol error, and the window starting mid-round is the normal way to
//     get one.
func BuildContext(rows []sqlc.Message) ([]llm.Message, error) {
	var out []llm.Message
	var round *toolRound

	// flush closes the open tool round: the results go directly after the
	// assistant message that asked for them, before anything that came later.
	var tail []llm.Message
	flush := func() {
		if round == nil {
			return
		}
		results := make([]llm.Message, 0, len(round.calls))
		for _, call := range round.calls {
			result, ok := round.results[call.ID]
			if !ok {
				result = llm.ToolResultPart{
					CallID:  call.ID,
					Name:    call.Name,
					Content: interruptedResult,
					Status:  llm.ToolInterrupted,
				}
			}
			results = append(results, llm.Message{Role: llm.RoleTool, Parts: []llm.Part{result}})
		}
		out = append(out, results...)
		out = append(out, tail...)
		round, tail = nil, nil
	}

	for _, row := range rows {
		parts, err := DecodeParts(row.Parts)
		if err != nil {
			return nil, err
		}

		switch row.Role {
		case sqlc.MessageRoleTool:
			if round == nil {
				continue // its call is outside the window
			}
			for _, part := range parts {
				result, ok := part.(llm.ToolResultPart)
				if !ok || !round.hasCall(result.CallID) {
					continue
				}
				round.results[result.CallID] = result
			}

		case sqlc.MessageRoleAssistant:
			if row.StopReason != nil && (*row.StopReason == StopError || *row.StopReason == StopCancelled) {
				continue
			}
			flush()
			msg := toLLMMessage(llm.RoleAssistant, parts)
			if len(msg.Parts) == 0 {
				continue
			}
			out = append(out, msg)
			if calls := msg.ToolCalls(); len(calls) > 0 {
				round = &toolRound{calls: calls, results: map[string]llm.ToolResultPart{}}
			}

		case sqlc.MessageRoleUser, sqlc.MessageRoleSystem:
			msg := toLLMMessage(llm.Role(row.Role), parts)
			if len(msg.Parts) == 0 {
				if row.Content == "" {
					continue
				}
				msg = llm.TextMessage(llm.Role(row.Role), row.Content)
			}
			if round != nil {
				// Something arrived between a call and its result. It goes after
				// the results, which must stay adjacent to the call.
				tail = append(tail, msg)
				continue
			}
			out = append(out, msg)
		}
	}
	flush()
	return out, nil
}

// toolRound is an assistant message's calls, waiting for their results.
type toolRound struct {
	calls   []llm.ToolCallPart
	results map[string]llm.ToolResultPart
}

func (p *toolRound) hasCall(id string) bool {
	for _, call := range p.calls {
		if call.ID == id {
			return true
		}
	}
	return false
}

// toLLMMessage keeps an attachment as the reference it is stored as. Its
// content is filled in afterwards, once the request knows what the model takes
// and how much fits (attachmentResolver).
func toLLMMessage(role llm.Role, parts llm.Parts) llm.Message {
	return llm.Message{Role: role, Parts: parts}
}
