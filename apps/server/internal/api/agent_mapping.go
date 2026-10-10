package api

import (
	"encoding/json"
	"log/slog"

	"github.com/qunqin/openchat/apps/server/internal/agent"
	"github.com/qunqin/openchat/apps/server/internal/db/sqlc"
	"github.com/qunqin/openchat/apps/server/internal/llm"
)

// The agent layer speaks llm's internal protocol; these map it onto the
// contract. The mapping is where provider-private data is dropped: a reasoning
// signature and redacted reasoning are kept in storage because the provider
// needs them back, and are never sent to a browser.

func toMessage(row sqlc.Message, attachments []Attachment) Message {
	m := Message{
		Id:             row.ID,
		ConversationId: row.ConversationID,
		Role:           Role(row.Role),
		Content:        row.Content,
		Parts:          []MessagePart{},
		Status:         MessageStatus(row.Status),
		RunId:          row.RunID,
		Model:          row.Model,
		ErrorCode:      row.ErrorCode,
		CreatedAt:      row.CreatedAt.Time,
	}
	if parts, err := agent.DecodeParts(row.Parts); err != nil {
		// One unreadable row must not take the whole conversation down with it;
		// its plain text still shows.
		slog.Error("decode message parts", "message_id", row.ID, "error", err)
	} else {
		if row.Role == sqlc.MessageRoleAssistant {
			// An answer written before leaked markup was hidden as it streamed
			// reads the way one written since does.
			var leaked bool
			if parts, leaked = agent.HideLeakedMarkup(parts); leaked {
				m.Content = agent.PlainText(parts)
				if row.StopReason == nil || *row.StopReason != agent.StopError {
					stop, code := agent.StopError, agent.CodeToolMarkup
					row.StopReason, m.ErrorCode = &stop, &code
				}
			}
		}
		m.Parts = toMessageParts(parts)
	}
	if row.StopReason != nil {
		stop := MessageStopReason(*row.StopReason)
		m.StopReason = &stop
	}
	if len(row.Usage) > 0 {
		var usage Usage
		if err := json.Unmarshal(row.Usage, &usage); err == nil {
			m.Usage = &usage
		}
	}
	if len(attachments) > 0 {
		m.Attachments = &attachments
	}
	return m
}

func toMessageParts(parts []llm.Part) []MessagePart {
	out := make([]MessagePart, 0, len(parts))
	for _, part := range parts {
		if mapped, ok := toMessagePart(part); ok {
			out = append(out, mapped)
		}
	}
	return out
}

// toMessagePart maps one block. ok is false for a block a browser has no
// business seeing — an attachment reference is shown through the message's
// attachments, with a signed URL, never as a raw storage handle.
func toMessagePart(part llm.Part) (MessagePart, bool) {
	var out MessagePart
	var err error
	switch p := part.(type) {
	case llm.TextPart:
		err = out.FromTextPart(TextPart{Type: TextPartTypeText, Text: p.Text})
	case llm.ReasoningPart:
		err = out.FromReasoningPart(ReasoningPart{Type: ReasoningPartTypeReasoning, Text: p.Text, DurationMs: p.DurationMS})
	case llm.ToolCallPart:
		call := ToolCallPart{Type: ToolCallPartTypeToolCall, Id: p.ID, Name: p.Name, RawArguments: p.RawArguments}
		if p.Parsed() {
			var args map[string]any
			if json.Unmarshal(p.Arguments, &args) == nil {
				call.Arguments = &args
			}
		}
		err = out.FromToolCallPart(call)
	case llm.ToolResultPart:
		result := ToolResultPart{
			Type:       ToolResult,
			CallId:     p.CallID,
			Name:       p.Name,
			Content:    p.Content,
			Status:     ToolResultPartStatus(p.Status),
			DurationMs: p.DurationMS,
		}
		if len(p.Details) > 0 {
			var details map[string]any
			if json.Unmarshal(p.Details, &details) == nil {
				result.Details = &details
			}
		}
		err = out.FromToolResultPart(result)
	default:
		return out, false
	}
	return out, err == nil
}

func toRun(row sqlc.Run) Run {
	run := Run{
		Id:             row.ID,
		ConversationId: row.ConversationID,
		Status:         RunStatus(row.Status),
		Model:          row.ModelID,
		ErrorCode:      row.ErrorCode,
		Continues:      row.Continues,
		Version:        row.Version,
		ReasoningEffort: func() *ReasoningEffort {
			effort := toEffort(row.ReasoningEffort)
			return &effort
		}(),
		CreatedAt: row.CreatedAt.Time,
	}
	if row.FinishedAt.Valid {
		t := row.FinishedAt.Time
		run.FinishedAt = &t
	}
	return run
}

func toLiveState(live agent.Live) LiveState {
	var out LiveState
	if g := live.Generation; g != nil {
		out.Generation = &struct {
			Attempt int           `json:"attempt"`
			Parts   []MessagePart `json:"parts"`
			Retry   *RetryInfo    `json:"retry,omitempty"`
		}{Attempt: g.Attempt, Parts: toMessageParts(g.Parts)}
		if g.Retry != nil {
			out.Generation.Retry = &RetryInfo{At: g.Retry.At, ErrorCode: g.Retry.ErrorCode}
		}
	}
	if len(live.Tools) > 0 {
		slots := make([]ToolSlot, 0, len(live.Tools))
		for _, slot := range live.Tools {
			slots = append(slots, ToolSlot{
				CallId:    slot.CallID,
				Name:      slot.Name,
				Status:    ToolSlotStatus(slot.Status),
				MessageId: slot.Message,
			})
		}
		out.Tools = &slots
	}
	return out
}

// toRunEvent maps one agent event. ok is false for an event with nothing to
// tell a browser.
func toRunEvent(event agent.Event) (RunEvent, bool) {
	var out RunEvent
	var err error
	switch e := event.(type) {
	case agent.RunStarted:
		err = out.FromRunStartedEvent(RunStartedEvent{Type: RunStarted, Run: toRun(e.Run), Message: toMessage(e.Input, nil)})
	case agent.RunFinished:
		err = out.FromRunFinishedEvent(RunFinishedEvent{Type: RunFinished, Run: toRun(e.Run)})
	case agent.GenerationStarted:
		err = out.FromGenerationStartedEvent(GenerationStartedEvent{Type: GenerationStarted, Attempt: e.Attempt})
	case agent.PartStarted:
		part, ok := toMessagePart(e.Part)
		if !ok {
			return out, false
		}
		err = out.FromPartStartedEvent(PartStartedEvent{Type: PartStarted, Index: e.Index, Part: part})
	case agent.PartDelta:
		err = out.FromPartDeltaEvent(PartDeltaEvent{Type: PartDelta, Index: e.Index, Delta: e.Delta})
	case agent.PartEnded:
		part, ok := toMessagePart(e.Part)
		if !ok {
			return out, false
		}
		err = out.FromPartEndedEvent(PartEndedEvent{Type: PartEnded, Index: e.Index, Part: part})
	case agent.MessageAppended:
		err = out.FromMessageAppendedEvent(MessageAppendedEvent{Type: MessageAppended, Message: toMessage(e.Message, nil)})
	case agent.ToolStarted:
		err = out.FromToolStartedEvent(ToolStartedEvent{Type: ToolStarted, CallId: e.CallID, Name: e.Name})
	case agent.ToolFinished:
		err = out.FromToolFinishedEvent(ToolFinishedEvent{Type: ToolFinished, CallId: e.CallID, MessageId: e.Message})
	case agent.RetryScheduled:
		err = out.FromRetryScheduledEvent(RetryScheduledEvent{Type: RetryScheduled, Attempt: e.Attempt, At: e.At, ErrorCode: e.ErrorCode})
	case agent.TitleChanged:
		err = out.FromTitleChangedEvent(TitleChangedEvent{Type: TitleChanged, Title: e.Title})
	case agent.TranscriptChanged:
		err = out.FromTranscriptChangedEvent(TranscriptChangedEvent{Type: TranscriptChanged})
	default:
		return out, false
	}
	return out, err == nil
}

func toBatchEvent(batch agent.Batch) BatchEvent {
	events := make([]RunEvent, 0, len(batch.Events))
	for _, event := range batch.Events {
		if mapped, ok := toRunEvent(event); ok {
			events = append(events, mapped)
		}
	}
	return BatchEvent{Type: Batch, RunId: batch.RunID, Version: batch.Version, Events: events}
}
