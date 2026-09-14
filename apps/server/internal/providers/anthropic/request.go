package anthropic

import (
	"encoding/base64"
	"encoding/json"
	"strings"

	"github.com/qunqin/openchat/apps/server/internal/llm"
)

// reservedKeys are the request fields Options.Extra may not touch. A vendor
// knob is welcome; quietly redirecting the request to another model, another
// history or another tool list than the one the run layer authorised is not.
var reservedKeys = map[string]bool{
	"model":       true,
	"messages":    true,
	"system":      true,
	"stream":      true,
	"tools":       true,
	"tool_choice": true,
	"max_tokens":  true,
	"thinking":    true,
}

// Extended thinking is a budget carved out of max_tokens, not a separate
// allowance, so these three numbers only mean anything next to it.
const (
	// minThinkingBudget is the API's own floor. Below it the request is
	// rejected outright.
	minThinkingBudget = 1024

	// minAnswerTokens is what we refuse to let the budget eat. A turn that
	// thinks up to its entire ceiling has no room left to answer.
	minAnswerTokens = 512
)

var thinkingBudgets = map[llm.ReasoningEffort]int{
	llm.EffortLow:    minThinkingBudget,
	llm.EffortMedium: 4096,
	llm.EffortHigh:   16384,
}

func buildBody(req llm.Request, cfg Config) ([]byte, error) {
	if req.Model == "" {
		return nil, llm.Errorf(llm.ErrorProvider, nil, "anthropic: the request names no model")
	}

	system, messages, err := convertMessages(req.Messages)
	if err != nil {
		return nil, err
	}
	if len(messages) == 0 {
		// A system prompt on its own is not a turn. Sending it produces a
		// confusing 400 a long way from here.
		return nil, llm.Errorf(llm.ErrorProvider, nil, "anthropic: the request has no messages for the model to answer")
	}

	maxTokens := cfg.MaxTokens
	if maxTokens <= 0 {
		maxTokens = defaultMaxTokens
	}
	if req.Options.MaxTokens != nil {
		maxTokens = *req.Options.MaxTokens
	}
	if maxTokens <= 0 {
		return nil, llm.Errorf(llm.ErrorProvider, nil, "anthropic: max_tokens must be positive, got %d", maxTokens)
	}

	body := map[string]any{
		"model":      req.Model,
		"messages":   messages,
		"max_tokens": maxTokens,
		"stream":     true,
	}
	if system != "" {
		body["system"] = system
	}
	if len(req.Tools) > 0 {
		body["tools"] = convertTools(req.Tools)
		if choice := convertToolChoice(req.Options.ToolChoice); choice != nil {
			body["tool_choice"] = choice
		}
	}

	opts := req.Options
	thinking, err := convertThinking(opts.Reasoning, maxTokens)
	if err != nil {
		return nil, err
	}
	if thinking != nil {
		body["thinking"] = thinking
	}

	// Extended thinking fixes the sampler: this API rejects a temperature or a
	// top_p alongside it. Dropping them is the only way both requests can be
	// served, and it costs a preference rather than content.
	if thinking == nil {
		if opts.Temperature != nil {
			body["temperature"] = *opts.Temperature
		}
		if opts.TopP != nil {
			body["top_p"] = *opts.TopP
		}
	}
	if len(opts.Stop) > 0 {
		body["stop_sequences"] = opts.Stop
	}

	for key, value := range opts.Extra {
		if reservedKeys[key] {
			return nil, llm.Errorf(llm.ErrorProvider, nil, "anthropic: option %q would overwrite a request field", key)
		}
		body[key] = value
	}
	return json.Marshal(body)
}

// convertThinking turns an effort level into a token budget. The budget is
// clamped to fit under max_tokens rather than rejected against it: an operator
// who capped max_tokens meant to cap the turn, not to turn thinking off.
func convertThinking(reasoning *llm.ReasoningOptions, maxTokens int) (map[string]any, error) {
	if reasoning == nil || reasoning.Effort == "" {
		return nil, nil
	}
	budget, known := thinkingBudgets[reasoning.Effort]
	if !known {
		return nil, llm.Errorf(llm.ErrorProvider, nil, "anthropic: unknown reasoning effort %q", reasoning.Effort)
	}
	if limit := maxTokens - minAnswerTokens; budget > limit {
		budget = limit
	}
	if budget < minThinkingBudget {
		return nil, llm.Errorf(llm.ErrorProvider, nil,
			"anthropic: max_tokens %d leaves no room to think, at least %d is required", maxTokens, minThinkingBudget+minAnswerTokens)
	}
	return map[string]any{"type": "enabled", "budget_tokens": budget}, nil
}

func convertTools(tools []llm.Tool) []map[string]any {
	out := make([]map[string]any, 0, len(tools))
	for _, tool := range tools {
		schema := tool.Parameters
		if len(schema) == 0 {
			// The field is required even for a tool that takes nothing.
			schema = json.RawMessage(`{"type":"object","properties":{}}`)
		}
		out = append(out, map[string]any{
			"name":         tool.Name,
			"description":  tool.Description,
			"input_schema": schema,
		})
	}
	return out
}

// convertToolChoice spells our three-way choice the way this protocol does. Auto
// is nil rather than {"type":"auto"} so that the field stays absent, which is
// what every older gateway on this protocol understands.
func convertToolChoice(choice llm.ToolChoice) map[string]any {
	switch choice {
	case llm.ToolChoiceNone:
		return map[string]any{"type": "none"}
	case llm.ToolChoiceRequired:
		// "any" is this protocol's spelling of "call something".
		return map[string]any{"type": "any"}
	default:
		return nil
	}
}

// convertMessages splits our flat history into the two things this protocol
// wants: a system prompt that is not a message, and a message list that
// alternates.
//
// The alternation is produced rather than demanded. Tool results become user
// turns here, so a tool round trip yields two user messages in a row and any
// caller assembling history the obvious way would be rejected for something it
// did nothing wrong to cause. Merging adjacent same-role messages makes the
// result alternate by construction; the one thing that cannot be repaired is a
// history that opens with the assistant, and that is refused.
func convertMessages(messages []llm.Message) (string, []wireMessage, error) {
	var (
		system strings.Builder
		out    []wireMessage
	)

	for _, message := range messages {
		var (
			role   string
			blocks []wireContentBlock
			err    error
		)

		switch message.Role {
		case llm.RoleSystem:
			text := message.Text()
			if text == "" {
				continue
			}
			if system.Len() > 0 {
				system.WriteString("\n\n")
			}
			system.WriteString(text)
			continue
		case llm.RoleUser:
			role = "user"
			blocks, err = convertPrompt(message)
		case llm.RoleAssistant:
			role = "assistant"
			blocks, err = convertAssistant(message)
		case llm.RoleTool:
			// Tool results travel as user content on this protocol. They are
			// not a role of their own, which is exactly why the merge below has
			// to exist.
			role = "user"
			blocks, err = convertToolResults(message)
		default:
			return "", nil, llm.Errorf(llm.ErrorProvider, nil, "anthropic: unknown role %q", message.Role)
		}
		if err != nil {
			return "", nil, err
		}
		if len(blocks) == 0 {
			continue
		}

		if len(out) == 0 && role == "assistant" {
			return "", nil, llm.Errorf(llm.ErrorProvider, nil, "anthropic: a conversation cannot open with an assistant turn")
		}
		if last := len(out) - 1; last >= 0 && out[last].Role == role {
			out[last].Content = append(out[last].Content, blocks...)
			continue
		}
		out = append(out, wireMessage{Role: role, Content: blocks})
	}

	return system.String(), out, nil
}

func convertPrompt(message llm.Message) ([]wireContentBlock, error) {
	blocks := make([]wireContentBlock, 0, len(message.Parts))
	for _, part := range message.Parts {
		switch p := part.(type) {
		case llm.TextPart:
			if p.Text == "" {
				// This API rejects an empty text block outright.
				continue
			}
			blocks = append(blocks, wireContentBlock{Type: "text", Text: p.Text})
		case llm.AttachmentPart:
			block, err := attachmentBlock(p)
			if err != nil {
				return nil, err
			}
			blocks = append(blocks, block)
		default:
			return nil, llm.Errorf(llm.ErrorProvider, nil, "anthropic: a user message cannot carry a %s block", part.Kind())
		}
	}
	return blocks, nil
}

// convertAssistant replays a previous turn.
//
// Reasoning goes back by default here, unlike on Chat Completions where it is
// opt-in: this API requires a thinking block and its signature to be replayed
// verbatim when the turn that follows continues a tool loop, and it verifies
// the signature. Unsigned thinking is dropped rather than sent, because a
// thinking block without one is rejected.
func convertAssistant(message llm.Message) ([]wireContentBlock, error) {
	blocks := make([]wireContentBlock, 0, len(message.Parts))
	for _, part := range message.Parts {
		switch p := part.(type) {
		case llm.TextPart:
			if p.Text == "" {
				continue
			}
			blocks = append(blocks, wireContentBlock{Type: "text", Text: p.Text})
		case llm.ReasoningPart:
			switch {
			case len(p.Redacted) > 0:
				blocks = append(blocks, wireContentBlock{Type: "redacted_thinking", Data: string(p.Redacted)})
			case p.Signature != "" && p.Text != "":
				blocks = append(blocks, wireContentBlock{Type: "thinking", Thinking: p.Text, Signature: p.Signature})
			}
		case llm.ToolCallPart:
			if p.ID == "" {
				return nil, llm.Errorf(llm.ErrorProvider, nil, "anthropic: assistant tool call %q has no id to match a result against", p.Name)
			}
			if !p.Parsed() {
				// Chat Completions can replay a malformed call as the string
				// the model sent. Here `input` is typed as an object, so there
				// is no faithful way to say "the model sent this and it was not
				// JSON" — and inventing `{}` would tell the model it called the
				// tool with no arguments, which is a different turn than the
				// one that happened.
				return nil, llm.Errorf(llm.ErrorProvider, nil,
					"anthropic: assistant tool call %q has arguments that never parsed, and this protocol has no block that can carry them", p.Name)
			}
			blocks = append(blocks, wireContentBlock{Type: "tool_use", ID: p.ID, Name: p.Name, Input: p.Arguments})
		default:
			return nil, llm.Errorf(llm.ErrorProvider, nil, "anthropic: an assistant message cannot carry a %s block", part.Kind())
		}
	}

	if len(blocks) == 0 {
		return nil, llm.Errorf(llm.ErrorProvider, nil, "anthropic: assistant message has no content and no tool calls")
	}
	return blocks, nil
}

func convertToolResults(message llm.Message) ([]wireContentBlock, error) {
	blocks := make([]wireContentBlock, 0, len(message.Parts))
	for _, part := range message.Parts {
		result, ok := part.(llm.ToolResultPart)
		if !ok {
			return nil, llm.Errorf(llm.ErrorProvider, nil, "anthropic: a tool message may only hold tool results, got %s", part.Kind())
		}
		if result.CallID == "" {
			return nil, llm.Errorf(llm.ErrorProvider, nil, "anthropic: tool result for %q has no call id", result.Name)
		}
		blocks = append(blocks, wireContentBlock{
			Type:      "tool_result",
			ToolUseID: result.CallID,
			Content:   toolResultContent(result),
			// is_error is this protocol's own way of saying the tool refused,
			// which is worth more to the model than the same fact buried in
			// prose it has to parse.
			IsError: result.Status != llm.ToolSucceeded,
		})
	}
	return blocks, nil
}

// toolResultContent spells out a non-success rather than sending an empty
// string. A failure the model cannot see is a failure it will happily build an
// answer on top of.
func toolResultContent(result llm.ToolResultPart) string {
	if result.Status == llm.ToolSucceeded {
		return result.Content
	}

	var b strings.Builder
	b.WriteString("tool ")
	b.WriteString(string(result.Status))
	if result.Error != "" {
		b.WriteString(": ")
		b.WriteString(result.Error)
	}
	if result.Content != "" {
		b.WriteString("\n")
		b.WriteString(result.Content)
	}
	return b.String()
}

// attachmentBlock turns a resolved attachment into a content block. An
// unresolved one is a bug in the context builder, not something to send blank:
// the reference is meaningless to the provider, and quietly dropping it would
// have the model answer about a file it never saw.
func attachmentBlock(attachment llm.AttachmentPart) (wireContentBlock, error) {
	var kind string
	switch {
	case strings.HasPrefix(attachment.MediaType, "image/"):
		kind = "image"
	case attachment.MediaType == "application/pdf":
		kind = "document"
	default:
		return wireContentBlock{}, llm.Errorf(llm.ErrorProvider, nil, "anthropic: this protocol carries no %q attachment", attachment.MediaType)
	}

	switch {
	case attachment.URL != "":
		return wireContentBlock{Type: kind, Source: &wireSource{Type: "url", URL: attachment.URL}}, nil
	case len(attachment.Data) > 0:
		return wireContentBlock{Type: kind, Source: &wireSource{
			Type:      "base64",
			MediaType: attachment.MediaType,
			Data:      base64.StdEncoding.EncodeToString(attachment.Data),
		}}, nil
	default:
		return wireContentBlock{}, llm.Errorf(llm.ErrorProvider, nil, "anthropic: attachment %q was never resolved", attachment.Ref)
	}
}
