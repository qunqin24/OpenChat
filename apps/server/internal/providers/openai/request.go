package openai

import (
	"encoding/base64"
	"encoding/json"
	"strings"

	"github.com/qunqin24/openchat/apps/server/internal/llm"
)

// reservedKeys are the request fields Options.Extra may not touch. A compat
// configuration is allowed to add vendor knobs; it is not allowed to quietly
// redirect the request to another model, another history or another tool list
// than the one the run layer authorised.
var reservedKeys = map[string]bool{
	"model":          true,
	"messages":       true,
	"stream":         true,
	"stream_options": true,
	"tools":          true,
	"tool_choice":    true,
	// The end-user identifier is the server's to set, from the request.
	"safety_identifier": true,
}

func buildBody(req llm.Request, cfg Config) ([]byte, error) {
	if req.Model == "" {
		return nil, llm.Errorf(llm.ErrorProvider, nil, "openai: the request names no model")
	}

	messages, err := convertMessages(req.Messages, cfg, req.Model)
	if err != nil {
		return nil, err
	}

	body := map[string]any{
		"model":    req.Model,
		"messages": messages,
		"stream":   true,
	}
	if !cfg.Compat.OmitStreamOptions {
		// Without this the trailing usage chunk never arrives and every
		// streamed turn looks free.
		body["stream_options"] = map[string]any{"include_usage": true}
	}
	if len(req.Tools) > 0 {
		body["tools"] = convertTools(req.Tools)
		if req.Options.ToolChoice != llm.ToolChoiceAuto {
			body["tool_choice"] = string(req.Options.ToolChoice)
		}
	}

	if cfg.SendUser && req.User != "" {
		body["safety_identifier"] = req.User
	}

	opts := req.Options
	if opts.Temperature != nil {
		body["temperature"] = *opts.Temperature
	}
	if opts.TopP != nil {
		body["top_p"] = *opts.TopP
	}
	if opts.MaxTokens != nil {
		if cfg.Compat.MaxCompletionTokens {
			body["max_completion_tokens"] = *opts.MaxTokens
		} else {
			body["max_tokens"] = *opts.MaxTokens
		}
	}
	if len(opts.Stop) > 0 {
		body["stop"] = opts.Stop
	}
	for key, value := range thinkingFields(req.Model, opts.Reasoning, cfg.Compat.Thinking) {
		body[key] = value
	}

	for key, value := range opts.Extra {
		if reservedKeys[key] {
			return nil, llm.Errorf(llm.ErrorProvider, nil, "openai: option %q would overwrite a request field", key)
		}
		body[key] = value
	}
	return json.Marshal(body)
}

func convertTools(tools []llm.Tool) []map[string]any {
	out := make([]map[string]any, 0, len(tools))
	for _, tool := range tools {
		parameters := tool.Parameters
		if len(parameters) == 0 {
			// The field is required even for a tool that takes nothing.
			parameters = json.RawMessage(`{"type":"object","properties":{}}`)
		}
		out = append(out, map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        tool.Name,
				"description": tool.Description,
				"parameters":  parameters,
			},
		})
	}
	return out
}

func convertMessages(messages []llm.Message, cfg Config, model string) ([]wireMessage, error) {
	out := make([]wireMessage, 0, len(messages))
	for _, message := range messages {
		switch message.Role {
		case llm.RoleTool:
			results, err := convertToolResults(message)
			if err != nil {
				return nil, err
			}
			out = append(out, results...)
		case llm.RoleAssistant:
			converted, err := convertAssistant(message, cfg, model)
			if err != nil {
				return nil, err
			}
			out = append(out, converted)
		case llm.RoleSystem, llm.RoleUser:
			converted, err := convertPrompt(message)
			if err != nil {
				return nil, err
			}
			out = append(out, converted)
		default:
			return nil, llm.Errorf(llm.ErrorProvider, nil, "openai: unknown role %q", message.Role)
		}
	}
	return out, nil
}

// convertToolResults emits one wire message per result. The protocol matches
// them to calls by tool_call_id, so batching several into one message would
// lose which answer belongs to which question.
func convertToolResults(message llm.Message) ([]wireMessage, error) {
	out := make([]wireMessage, 0, len(message.Parts))
	for _, part := range message.Parts {
		result, ok := part.(llm.ToolResultPart)
		if !ok {
			return nil, llm.Errorf(llm.ErrorProvider, nil, "openai: a tool message may only hold tool results, got %s", part.Kind())
		}
		if result.CallID == "" {
			return nil, llm.Errorf(llm.ErrorProvider, nil, "openai: tool result for %q has no call id", result.Name)
		}
		out = append(out, wireMessage{Role: "tool", ToolCallID: result.CallID, Content: toolResultContent(result)})
	}
	return out, nil
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

func convertAssistant(message llm.Message, cfg Config, model string) (wireMessage, error) {
	out := wireMessage{Role: "assistant"}
	if cfg.Compat.ReasoningDetails {
		out.ReasoningDetails = storedDetails(message, model)
	}

	var text, reasoning strings.Builder
	for _, part := range message.Parts {
		switch p := part.(type) {
		case llm.TextPart:
			text.WriteString(p.Text)
		case llm.ReasoningPart:
			// Dropped unless the endpoint is known to want it back: most
			// implementations of this protocol reject their own
			// reasoning_content on the way in. A signed or encrypted block is
			// another provider's, in a form this protocol has no field for.
			if cfg.Compat.EchoReasoning && out.ReasoningDetails == nil && p.Signature == "" && len(p.Redacted) == 0 {
				reasoning.WriteString(p.Text)
			}
		case llm.ToolCallPart:
			if p.ID == "" {
				return wireMessage{}, llm.Errorf(llm.ErrorProvider, nil, "openai: assistant tool call %q has no id to match a result against", p.Name)
			}
			arguments := string(p.Arguments)
			if !p.Parsed() {
				// Replayed exactly as the model sent it. The tool result for a
				// malformed call says the arguments were malformed, and that
				// only makes sense next to the arguments themselves.
				arguments = p.RawArguments
			}
			out.ToolCalls = append(out.ToolCalls, wireToolCall{
				ID:       p.ID,
				Type:     "function",
				Function: wireToolFunction{Name: p.Name, Arguments: arguments},
			})
		default:
			return wireMessage{}, llm.Errorf(llm.ErrorProvider, nil, "openai: an assistant message cannot carry a %s block", part.Kind())
		}
	}

	if text.Len() > 0 {
		out.Content = text.String()
	}
	out.ReasoningContent = reasoning.String()
	if out.Content == nil && len(out.ToolCalls) == 0 {
		// An assistant turn with neither is not a turn; sending it produces a
		// confusing 400 far from here.
		return wireMessage{}, llm.Errorf(llm.ErrorProvider, nil, "openai: assistant message has no content and no tool calls")
	}
	return out, nil
}

func convertPrompt(message llm.Message) (wireMessage, error) {
	out := wireMessage{Role: string(message.Role)}

	var (
		text   strings.Builder
		blocks []wireContentBlock
	)
	for _, part := range message.Parts {
		switch p := part.(type) {
		case llm.TextPart:
			text.WriteString(p.Text)
		case llm.AttachmentPart:
			block, err := attachmentBlock(p)
			if err != nil {
				return wireMessage{}, err
			}
			blocks = append(blocks, block)
		default:
			return wireMessage{}, llm.Errorf(llm.ErrorProvider, nil, "openai: a %s message cannot carry a %s block", message.Role, part.Kind())
		}
	}

	if len(blocks) == 0 {
		// Plain string content: the block-list form upsets older gateways, and
		// the overwhelming majority of turns have no attachments at all.
		out.Content = text.String()
		return out, nil
	}

	content := make([]wireContentBlock, 0, len(blocks)+1)
	if text.Len() > 0 {
		content = append(content, wireContentBlock{Type: "text", Text: text.String()})
	}
	out.Content = append(content, blocks...)
	return out, nil
}

// attachmentBlock turns a resolved attachment into a content block. An
// unresolved one is a bug in the context builder, not something to send blank:
// the reference is meaningless to the provider, and quietly dropping it would
// have the model answer about a file it never saw.
func attachmentBlock(attachment llm.AttachmentPart) (wireContentBlock, error) {
	isImage := strings.HasPrefix(attachment.MediaType, "image/")
	if !isImage && attachment.MediaType != "application/pdf" {
		return wireContentBlock{}, llm.Errorf(llm.ErrorProvider, nil, "openai: this protocol carries no %q attachment", attachment.MediaType)
	}
	if attachment.URL != "" {
		// A document can only be sent inline here; there is no link form.
		if !isImage {
			return wireContentBlock{}, llm.Errorf(llm.ErrorProvider, nil, "openai: a %q attachment has to be inlined, not linked", attachment.MediaType)
		}
		return wireContentBlock{Type: "image_url", ImageURL: &wireImageURL{URL: attachment.URL}}, nil
	}
	if len(attachment.Data) == 0 {
		return wireContentBlock{}, llm.Errorf(llm.ErrorProvider, nil, "openai: attachment %q was never resolved", attachment.Ref)
	}
	url := "data:" + attachment.MediaType + ";base64," + base64.StdEncoding.EncodeToString(attachment.Data)
	if isImage {
		return wireContentBlock{Type: "image_url", ImageURL: &wireImageURL{URL: url}}, nil
	}
	return wireContentBlock{Type: "file", File: &wireFile{Filename: attachment.Filename, FileData: url}}, nil
}
