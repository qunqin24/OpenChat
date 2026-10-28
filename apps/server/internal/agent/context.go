package agent

import (
	"fmt"
	"time"

	"github.com/qunqin24/openchat/apps/server/internal/db/sqlc"
	"github.com/qunqin24/openchat/apps/server/internal/llm"
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

// skippedResult stands in for a question the user never answered: they wrote
// something else, and the next run is built with that message after it.
const skippedResult = "The user did not answer this question and sent a new message instead. " +
	"Take that message as their reply and carry on; ask again only if you still need the answer."

// BuildContext turns transcript rows into a model request's messages.
//
// The transcript is what happened, in the order it was committed. A model
// request has stricter rules than that, and these are pi-durable's context
// derivation rules (spec §2.1) applied to our rows:
//
//   - A message content moderation refused (status blocked) is shown to the
//     person and never sent: the queries that feed this leave it out, and it
//     is skipped here as well, so no caller can send it by accident.
//   - A failed or cancelled response is shown to the user but never sent back:
//     half an answer, or an error, is not context the next request should build
//     on, and a cancelled response may hold a tool call that never ran.
//   - Every tool call is followed directly by its result, in call order — the
//     providers reject anything else. Results are committed in the order the
//     tools finished, so they are moved here rather than at write time.
//   - A call with no result gets a synthesized error result. That happens at an
//     interrupted tail or where the history window cut between the two. For
//     ask_user it is not an error: the question is still unanswered because the
//     user wrote something else, and the result says so.
//   - A result whose call is not in the window is dropped: sent alone it is a
//     protocol error, and the window starting mid-round is the normal way to
//     get one.
//   - A user message that begins a new day is dated (see dateStamp), which is
//     how the model knows the date without the system prompt saying it; one
//     sent after a pause of an hour or more gets the time and how long it has
//     been instead (timeStamp), so the model knows the conversation was taken
//     up again later. Both are in the person's time zone.
func BuildContext(rows []sqlc.Message) ([]llm.Message, error) {
	return BuildContextIn(rows, time.UTC)
}

// TimeGap is the pause after which a user message is told the time: the page
// shows a time above it too (lib/time-gap.ts).
const TimeGap = time.Hour

// BuildContextIn is BuildContext with the dates and times in loc, the
// person's time zone.
func BuildContextIn(rows []sqlc.Message, loc *time.Location) ([]llm.Message, error) {
	if loc == nil {
		loc = time.UTC
	}
	var out []llm.Message
	var round *toolRound
	var stamped string // the day of the last dated user message
	var last time.Time // when the last row sent was written

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
				result = missingResult(call)
			}
			results = append(results, llm.Message{Role: llm.RoleTool, Parts: []llm.Part{result}})
		}
		out = append(out, results...)
		out = append(out, tail...)
		round, tail = nil, nil
	}

	for _, row := range rows {
		if row.Status == sqlc.MessageStatusBlocked {
			continue
		}
		parts, err := DecodeParts(row.Parts)
		if err != nil {
			return nil, err
		}
		// The pause before a user message is from the row before it, whatever
		// it was: an answer, a tool's result, an earlier question.
		previous := last
		if row.CreatedAt.Valid {
			last = row.CreatedAt.Time
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
			// Markup a model leaked as text would teach it to write more.
			parts, _ = HideLeakedMarkup(parts)
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
			if row.Role == sqlc.MessageRoleUser && row.CreatedAt.Valid {
				at := row.CreatedAt.Time.In(loc)
				day := at.Format(time.DateOnly)
				switch {
				case !previous.IsZero() && at.Sub(previous) >= TimeGap:
					stamped = day
					msg.Parts = append([]llm.Part{llm.TextPart{Text: timeStamp(at, at.Sub(previous))}}, msg.Parts...)
				case day != stamped:
					stamped = day
					msg.Parts = append([]llm.Part{llm.TextPart{Text: dateStamp(day)}}, msg.Parts...)
				}
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

// missingResult is what a call without a result row is answered with.
func missingResult(call llm.ToolCallPart) llm.ToolResultPart {
	if call.Name == ToolAskUser {
		return llm.ToolResultPart{CallID: call.ID, Name: call.Name, Content: skippedResult, Status: llm.ToolSucceeded}
	}
	return llm.ToolResultPart{CallID: call.ID, Name: call.Name, Content: interruptedResult, Status: llm.ToolInterrupted}
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

// dateStamp is the line that tells the model what day a message was sent. The
// date lives in the history instead of the system prompt: a system prompt that
// changes at midnight changes the bytes in front of every cached prefix and
// every thinking block a provider checks the history against, so the first
// request of the day would be rejected or paid for in full. A stamp on the
// message it belongs to never changes once written, and only the first message
// of each day carries one, so the history stays a stable prefix.
func dateStamp(day string) string {
	return "[Date: " + day + "]"
}

// timeStamp is the line on a message sent after a pause (TimeGap): the date
// and time it was sent, in the person's time zone, and how long after the row
// before it - so the model knows the conversation was taken up again, and
// does not carry on as though no time had passed. Fixed once written, like
// dateStamp, so the history stays a stable prefix.
func timeStamp(at time.Time, pause time.Duration) string {
	zone := at.Location().String()
	if zone == "Local" || zone == "" {
		zone = at.Format("-07:00")
	}
	return fmt.Sprintf("[Time: %s %s, %s after the previous message]", at.Format("2006-01-02 15:04"), zone, pauseText(pause))
}

// pauseText is a pause as a person would say it: "2 hours", "3 days".
func pauseText(pause time.Duration) string {
	plural := func(n int, unit string) string {
		if n == 1 {
			return "1 " + unit
		}
		return fmt.Sprintf("%d %ss", n, unit)
	}
	switch {
	case pause < 2*time.Hour:
		return plural(int(pause.Minutes()), "minute")
	case pause < 48*time.Hour:
		return plural(int(pause.Hours()), "hour")
	case pause < 60*24*time.Hour:
		return plural(int(pause.Hours()/24), "day")
	default:
		return plural(int(pause.Hours()/24/30), "month")
	}
}
