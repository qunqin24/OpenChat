package openai

import (
	"encoding/base64"
	"encoding/json"
	"strings"

	"github.com/qunqin/openchat/apps/server/internal/llm"
)

// responsesSignaturePrefix marks a ReasoningPart whose Redacted bytes are an
// encrypted_content blob from this protocol. The model it came from follows the
// colon. Redacted alone cannot say whose it is — Anthropic stores its
// redacted_thinking there too — and a blob replayed to the wrong party, or to a
// different model, is a rejected request instead of a missing hint. So the
// blob is replayed only to the model that produced it, and dropped otherwise.
const responsesSignaturePrefix = llm.SignatureOpenAIResponses

// responsesReservedKeys are the request fields Options.Extra may not touch, for
// the same reason as reservedKeys: a vendor knob is not a way to redirect the
// request, and store/include are what keep multi-turn reasoning stateless.
var responsesReservedKeys = map[string]bool{
	"model":        true,
	"input":        true,
	"instructions": true,
	"stream":       true,
	"tools":        true,
	"tool_choice":  true,
	"store":        true,
	"include":      true,
	// The end-user identifier is the server's to set, from the request.
	"safety_identifier": true,
}

func buildResponsesBody(req llm.Request, cfg Config) ([]byte, error) {
	return buildResponsesBodyWith(req, cfg, true)
}

// asksForReasoning is whether the request carries the reasoning parameters at
// all: they are refused by a model that does not think (gpt-4.1 answers 400 to
// the include), so they go only to one the catalogue says thinks or to a
// request that names a level.
func asksForReasoning(req llm.Request, cfg Config) bool {
	return cfg.Reasoning || (req.Options.Reasoning != nil && req.Options.Reasoning.Effort != "")
}

// buildResponsesBodyWith is buildResponsesBody with the reasoning summary
// optional, for the organisations that may not ask for one (see
// ResponsesClient.Stream).
func buildResponsesBodyWith(req llm.Request, cfg Config, summary bool) ([]byte, error) {
	if req.Model == "" {
		return nil, llm.Errorf(llm.ErrorProvider, nil, "openai: the request names no model")
	}

	instructions, input, err := convertResponsesInput(req)
	if err != nil {
		return nil, err
	}

	body := map[string]any{
		"model":  req.Model,
		"input":  input,
		"stream": true,
		// Nothing is kept server-side: the history is resent every turn, and the
		// encrypted reasoning below is how the model keeps its own context
		// across a tool loop without that.
		"store": false,
	}
	if cfg.SendUser && req.User != "" {
		body["safety_identifier"] = req.User
	}
	opts := req.Options
	thinks := asksForReasoning(req, cfg)
	if thinks {
		body["include"] = []string{"reasoning.encrypted_content"}
	}
	if instructions != "" {
		body["instructions"] = instructions
	}
	if len(req.Tools) > 0 {
		body["tools"] = convertResponsesTools(req.Tools)
		if req.Options.ToolChoice != llm.ToolChoiceAuto {
			body["tool_choice"] = string(req.Options.ToolChoice)
		}
	}

	if opts.Temperature != nil {
		body["temperature"] = *opts.Temperature
	}
	if opts.TopP != nil {
		body["top_p"] = *opts.TopP
	}
	if opts.MaxTokens != nil {
		body["max_output_tokens"] = *opts.MaxTokens
	}
	// Options.Stop has no counterpart on this protocol and is dropped.
	if thinks {
		// The summary is the only thinking the model will show, and without
		// asking for it a model left on its default level shows none.
		reasoning := map[string]any{}
		if summary {
			reasoning["summary"] = "auto"
		}
		if opts.Reasoning != nil && opts.Reasoning.Effort != "" {
			reasoning["effort"] = string(opts.Reasoning.Effort)
		}
		if len(reasoning) > 0 {
			body["reasoning"] = reasoning
		}
	}

	for key, value := range opts.Extra {
		if responsesReservedKeys[key] {
			return nil, llm.Errorf(llm.ErrorProvider, nil, "openai: option %q would overwrite a request field", key)
		}
		body[key] = value
	}
	return json.Marshal(body)
}

func convertResponsesTools(tools []llm.Tool) []map[string]any {
	out := make([]map[string]any, 0, len(tools))
	for _, tool := range tools {
		parameters := tool.Parameters
		if len(parameters) == 0 {
			parameters = json.RawMessage(`{"type":"object","properties":{}}`)
		}
		out = append(out, map[string]any{
			"type":        "function",
			"name":        tool.Name,
			"description": tool.Description,
			"parameters":  parameters,
			// Responses defaults function tools to strict, which demands
			// additionalProperties:false and every property required. The
			// schemas here are written for validation, not for that dialect.
			"strict": false,
		})
	}
	return out
}

func convertResponsesInput(req llm.Request) (string, []map[string]any, error) {
	var (
		instructions []string
		input        = []map[string]any{}
	)
	for _, message := range req.Messages {
		switch message.Role {
		case llm.RoleSystem:
			for _, part := range message.Parts {
				text, ok := part.(llm.TextPart)
				if !ok {
					return "", nil, llm.Errorf(llm.ErrorProvider, nil, "openai: a system message cannot carry a %s block", part.Kind())
				}
				if text.Text != "" {
					instructions = append(instructions, text.Text)
				}
			}
		case llm.RoleUser:
			item, err := responsesUserItem(message)
			if err != nil {
				return "", nil, err
			}
			input = append(input, item)
		case llm.RoleAssistant:
			items, err := responsesAssistantItems(message, req.Model)
			if err != nil {
				return "", nil, err
			}
			input = append(input, items...)
		case llm.RoleTool:
			for _, part := range message.Parts {
				result, ok := part.(llm.ToolResultPart)
				if !ok {
					return "", nil, llm.Errorf(llm.ErrorProvider, nil, "openai: a tool message may only hold tool results, got %s", part.Kind())
				}
				if result.CallID == "" {
					return "", nil, llm.Errorf(llm.ErrorProvider, nil, "openai: tool result for %q has no call id", result.Name)
				}
				input = append(input, map[string]any{
					"type":    "function_call_output",
					"call_id": result.CallID,
					"output":  toolResultContent(result),
				})
			}
		default:
			return "", nil, llm.Errorf(llm.ErrorProvider, nil, "openai: unknown role %q", message.Role)
		}
	}
	return strings.Join(instructions, "\n\n"), input, nil
}

func responsesUserItem(message llm.Message) (map[string]any, error) {
	var (
		content []map[string]any
		text    strings.Builder
	)
	flush := func() {
		if text.Len() > 0 {
			content = append(content, map[string]any{"type": "input_text", "text": text.String()})
			text.Reset()
		}
	}
	for _, part := range message.Parts {
		switch p := part.(type) {
		case llm.TextPart:
			text.WriteString(p.Text)
		case llm.AttachmentPart:
			block, err := responsesAttachment(p)
			if err != nil {
				return nil, err
			}
			flush()
			content = append(content, block)
		default:
			return nil, llm.Errorf(llm.ErrorProvider, nil, "openai: a user message cannot carry a %s block", part.Kind())
		}
	}
	flush()
	if len(content) == 0 {
		content = append(content, map[string]any{"type": "input_text", "text": ""})
	}
	return map[string]any{"type": "message", "role": "user", "content": content}, nil
}

// responsesAttachment mirrors attachmentBlock: an unresolved attachment is a
// bug in the context builder and is refused, never sent blank.
func responsesAttachment(attachment llm.AttachmentPart) (map[string]any, error) {
	isImage := strings.HasPrefix(attachment.MediaType, "image/")
	if !isImage && attachment.MediaType != "application/pdf" {
		return nil, llm.Errorf(llm.ErrorProvider, nil, "openai: this protocol carries no %q attachment", attachment.MediaType)
	}
	if attachment.URL != "" {
		if !isImage {
			return nil, llm.Errorf(llm.ErrorProvider, nil, "openai: a %q attachment has to be inlined, not linked", attachment.MediaType)
		}
		return map[string]any{"type": "input_image", "image_url": attachment.URL}, nil
	}
	if len(attachment.Data) == 0 {
		return nil, llm.Errorf(llm.ErrorProvider, nil, "openai: attachment %q was never resolved", attachment.Ref)
	}
	dataURL := "data:" + attachment.MediaType + ";base64," + base64.StdEncoding.EncodeToString(attachment.Data)
	if isImage {
		return map[string]any{"type": "input_image", "image_url": dataURL}, nil
	}
	filename := attachment.Filename
	if filename == "" {
		// file_data without a filename is rejected.
		filename = "document.pdf"
	}
	return map[string]any{"type": "input_file", "filename": filename, "file_data": dataURL}, nil
}

// responsesAssistantItems keeps the parts in the order the model produced them.
// Text, reasoning and calls are separate items on this protocol, and the model
// is shown its own turn in the sequence it wrote it.
func responsesAssistantItems(message llm.Message, model string) ([]map[string]any, error) {
	var (
		items []map[string]any
		text  strings.Builder
		phase string // of the text being gathered
	)
	flush := func() {
		if text.Len() > 0 {
			item := map[string]any{
				"type": "message",
				"role": "assistant",
				"content": []map[string]any{
					{"type": "output_text", "text": text.String()},
				},
			}
			if phase != "" {
				// Resent as the model wrote it: for gpt-5.3-codex and later,
				// dropping it degrades the next answer.
				item["phase"] = phase
			}
			items = append(items, item)
			text.Reset()
		}
	}

	for _, part := range message.Parts {
		switch p := part.(type) {
		case llm.TextPart:
			next := knownPhase(p.Phase)
			if next != phase {
				// One message item has one phase.
				flush()
				phase = next
			}
			text.WriteString(p.Text)
		case llm.ReasoningPart:
			// Only a blob this model issued goes back. Summary text is never
			// replayed: the endpoint rejects reasoning it did not write.
			if len(p.Redacted) == 0 || p.Signature != responsesSignaturePrefix+model {
				continue
			}
			flush()
			items = append(items, map[string]any{
				"type":              "reasoning",
				"summary":           []any{},
				"encrypted_content": string(p.Redacted),
			})
		case llm.ToolCallPart:
			if p.ID == "" {
				return nil, llm.Errorf(llm.ErrorProvider, nil, "openai: assistant tool call %q has no id to match a result against", p.Name)
			}
			arguments := string(p.Arguments)
			if !p.Parsed() {
				// Replayed as sent, so that the tool result saying "malformed"
				// sits next to the malformed arguments.
				arguments = p.RawArguments
			}
			if arguments == "" {
				arguments = "{}"
			}
			flush()
			items = append(items, map[string]any{
				"type":      "function_call",
				"call_id":   p.ID,
				"name":      p.Name,
				"arguments": arguments,
			})
		default:
			return nil, llm.Errorf(llm.ErrorProvider, nil, "openai: an assistant message cannot carry a %s block", part.Kind())
		}
	}
	flush()

	if len(items) == 0 {
		return nil, llm.Errorf(llm.ErrorProvider, nil, "openai: assistant message has no content and no tool calls")
	}
	return items, nil
}

// knownPhase passes through the two phases the protocol defines and drops
// anything else, so a value from some other provider's history cannot turn
// into a rejected request.
func knownPhase(phase string) string {
	switch phase {
	case "commentary", "final_answer":
		return phase
	}
	return ""
}
