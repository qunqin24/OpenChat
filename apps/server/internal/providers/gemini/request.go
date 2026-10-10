package gemini

import (
	"encoding/base64"
	"encoding/json"
	"strconv"
	"strings"

	"github.com/qunqin/openchat/apps/server/internal/llm"
)

// reservedKeys are the request fields Options.Extra may not touch. A vendor
// knob is welcome; quietly redirecting the request to another history, another
// system prompt or another tool list than the one the run layer authorised is
// not. The model is not listed because it is not in the body at all on this
// protocol — it is in the URL, where Extra cannot reach it.
var reservedKeys = map[string]bool{
	"contents":           true,
	"systemInstruction":  true,
	"system_instruction": true,
	"tools":              true,
	"toolConfig":         true,
	"tool_config":        true,
	"generationConfig":   true,
	"generation_config":  true,
}

// levels are the thinking levels this API accepts. Gemini 3 models take a
// level; Gemini 2.5 models take a budget instead, and which one a model takes
// is the catalogue's to say — the caller sends one or the other. A budget is
// not clamped against maxOutputTokens: this API takes any pair, but thoughts
// are generated inside maxOutputTokens as on the Messages API, so a small cap
// can be spent on thinking and end the turn MAX_TOKENS with little or no
// visible answer. That is the caller's cap to size, not something to hide here.
var levels = map[llm.ReasoningEffort]bool{
	llm.EffortMinimal: true, llm.EffortLow: true, llm.EffortMedium: true, llm.EffortHigh: true,
}

func buildBody(req llm.Request) ([]byte, error) {
	if req.Model == "" {
		return nil, llm.Errorf(llm.ErrorProvider, nil, "gemini: the request names no model")
	}

	system, contents, err := convertMessages(req.Messages, req.Model)
	if err != nil {
		return nil, err
	}
	if len(contents) == 0 {
		// A system prompt on its own is not a turn. Sending it produces a
		// confusing 400 a long way from here.
		return nil, llm.Errorf(llm.ErrorProvider, nil, "gemini: the request has no messages for the model to answer")
	}

	body := map[string]any{"contents": contents}
	if len(system) > 0 {
		// systemInstruction is a Content, and a roleless one: giving it a role
		// is how it stops being an instruction and becomes a turn.
		body["systemInstruction"] = wireContent{Parts: system}
	}
	if len(req.Tools) > 0 {
		// One tool object holding every declaration. The field is a list
		// because this API also carries its own built-in tools there, and we
		// declare none of those.
		body["tools"] = []map[string]any{{"functionDeclarations": convertTools(req.Tools)}}
		if mode := toolMode(req.Options.ToolChoice); mode != "" {
			body["toolConfig"] = map[string]any{"functionCallingConfig": map[string]any{"mode": mode}}
		}
	}

	opts := req.Options
	generation := map[string]any{}
	if opts.Temperature != nil {
		generation["temperature"] = *opts.Temperature
	}
	if opts.TopP != nil {
		generation["topP"] = *opts.TopP
	}
	if opts.MaxTokens != nil {
		generation["maxOutputTokens"] = *opts.MaxTokens
	}
	if len(opts.Stop) > 0 {
		generation["stopSequences"] = opts.Stop
	}
	if r := opts.Reasoning; r != nil {
		// includeThoughts is what makes the thoughts arrive at all. Without it
		// the turn is billed for reasoning the user never gets to see.
		switch {
		case r.Effort != "":
			if !levels[r.Effort] {
				return nil, llm.Errorf(llm.ErrorProvider, nil, "gemini: unknown thinking level %q", r.Effort)
			}
			generation["thinkingConfig"] = map[string]any{"includeThoughts": true, "thinkingLevel": string(r.Effort)}
		case r.Budget > 0:
			generation["thinkingConfig"] = map[string]any{"includeThoughts": true, "thinkingBudget": r.Budget}
		}
	}
	if len(generation) > 0 {
		body["generationConfig"] = generation
	}

	for key, value := range opts.Extra {
		if reservedKeys[key] {
			return nil, llm.Errorf(llm.ErrorProvider, nil, "gemini: option %q would overwrite a request field", key)
		}
		body[key] = value
	}
	return json.Marshal(body)
}

func convertTools(tools []llm.Tool) []map[string]any {
	out := make([]map[string]any, 0, len(tools))
	for _, tool := range tools {
		declaration := map[string]any{"name": tool.Name, "description": tool.Description}
		if len(tool.Parameters) > 0 {
			// Omitted rather than defaulted for a tool that takes nothing: this
			// API rejects an empty properties object, unlike the other two.
			declaration["parameters"] = tool.Parameters
		}
		out = append(out, declaration)
	}
	return out
}

// toolMode spells our three-way choice the way this protocol does. Auto is
// empty rather than "AUTO" so the whole toolConfig stays absent, which is what
// every relay on this protocol understands.
func toolMode(choice llm.ToolChoice) string {
	switch choice {
	case llm.ToolChoiceNone:
		return "NONE"
	case llm.ToolChoiceRequired:
		return "ANY"
	default:
		return ""
	}
}

// convertMessages splits our flat history into the two things this protocol
// wants: a system instruction that is not a turn, and a list of contents whose
// roles are "user" and "model".
//
// Tool results are user content here, so a tool round trip yields two user
// turns in a row; adjacent same-role turns are merged for the same reason as on
// the Messages API. Unlike there, this API does not insist the history open
// with the user, so nothing is refused on that ground.
func convertMessages(messages []llm.Message, model string) ([]wireRequestPart, []wireContent, error) {
	var (
		system   []wireRequestPart
		contents []wireContent
	)

	for _, message := range messages {
		var (
			role  string
			parts []wireRequestPart
			err   error
		)

		switch message.Role {
		case llm.RoleSystem:
			if text := message.Text(); text != "" {
				system = append(system, wireRequestPart{Text: text})
			}
			continue
		case llm.RoleUser:
			role = "user"
			parts, err = convertPrompt(message)
		case llm.RoleAssistant:
			// Not "assistant". This protocol has never used that word, and a
			// role it does not know is a 400.
			role = "model"
			parts, err = convertAssistant(message, model)
		case llm.RoleTool:
			role = "user"
			parts, err = convertToolResults(message)
		default:
			return nil, nil, llm.Errorf(llm.ErrorProvider, nil, "gemini: unknown role %q", message.Role)
		}
		if err != nil {
			return nil, nil, err
		}
		if len(parts) == 0 {
			continue
		}

		if last := len(contents) - 1; last >= 0 && contents[last].Role == role {
			contents[last].Parts = append(contents[last].Parts, parts...)
			continue
		}
		contents = append(contents, wireContent{Role: role, Parts: parts})
	}

	return system, contents, nil
}

func convertPrompt(message llm.Message) ([]wireRequestPart, error) {
	parts := make([]wireRequestPart, 0, len(message.Parts))
	for _, part := range message.Parts {
		switch p := part.(type) {
		case llm.TextPart:
			if p.Text == "" {
				continue
			}
			parts = append(parts, wireRequestPart{Text: p.Text})
		case llm.AttachmentPart:
			converted, err := attachmentPart(p)
			if err != nil {
				return nil, err
			}
			parts = append(parts, converted)
		default:
			return nil, llm.Errorf(llm.ErrorProvider, nil, "gemini: a user message cannot carry a %s block", part.Kind())
		}
	}
	return parts, nil
}

// convertAssistant replays a previous turn.
//
// Reasoning is dropped. This API will not take a thought part back without the
// signature that came with it, and a signature belongs to the exact model and
// configuration that issued it — so replaying one is at best ignored and at
// worst a rejected request. Dropping it costs the model its own deliberation
// from earlier turns, which is the same trade the Chat Completions adapter
// makes by default.
func convertAssistant(message llm.Message, model string) ([]wireRequestPart, error) {
	parts := make([]wireRequestPart, 0, len(message.Parts))
	sawCall := false
	for _, part := range message.Parts {
		switch p := part.(type) {
		case llm.TextPart:
			signature, ours := strings.CutPrefix(p.Signature, llm.SignatureGemini)
			if !ours {
				// Another adapter's: it would only be rejected here.
				signature = ""
			}
			if p.Text == "" {
				// A part needs content, and a signature alone is not any; the
				// text it vouches for is the part before it.
				if last := len(parts) - 1; signature != "" && last >= 0 && parts[last].Text != "" && parts[last].ThoughtSignature == "" {
					parts[last].ThoughtSignature = signature
				}
				continue
			}
			// Sent back on the part it came on, as the docs recommend; a
			// reply without it is not rejected, only answered a little worse.
			// https://ai.google.dev/gemini-api/docs/generate-content/thought-signatures
			parts = append(parts, wireRequestPart{Text: p.Text, ThoughtSignature: signature})
		case llm.ReasoningPart:
			continue
		case llm.ToolCallPart:
			if !p.Parsed() {
				// `args` is typed as an object here, so there is no faithful
				// way to say "the model sent this and it was not JSON", and
				// inventing `{}` would tell the model it called the tool with
				// no arguments — a different turn than the one that happened.
				return nil, llm.Errorf(llm.ErrorProvider, nil,
					"gemini: assistant tool call %q has arguments that never parsed, and this protocol has no part that can carry them", p.Name)
			}
			call := &wireFunctionCall{Name: p.Name, Args: p.Arguments}
			if !isSynthetic(p.ID) {
				// Only an id the provider itself minted goes back. Ours is
				// bookkeeping for the run layer and means nothing here.
				call.ID = p.ID
			}
			part := wireRequestPart{FunctionCall: call}
			if !sawCall {
				// Only the first call of a step is validated, and only that
				// one carries a signature when the model made several at once.
				part.ThoughtSignature = callThoughtSignature(p, model)
				sawCall = true
			}
			parts = append(parts, part)
		default:
			return nil, llm.Errorf(llm.ErrorProvider, nil, "gemini: an assistant message cannot carry a %s block", part.Kind())
		}
	}

	if len(parts) == 0 {
		return nil, llm.Errorf(llm.ErrorProvider, nil, "gemini: assistant message has no content and no tool calls")
	}
	return parts, nil
}

// skipSignatureValidation is the value the API documents for a function call
// that has no signature of its own: history that predates this adapter keeping
// them, a trace from another vendor, or a call a client made up. It switches
// the check off for that call; it does not claim the model reasoned anything.
const skipSignatureValidation = "skip_thought_signature_validator"

// callThoughtSignature is what goes on the first functionCall of a replayed
// step. Gemini 3 answers 400 to a step in the current turn whose first call
// lacks one, so a call with no usable signature gets the documented dummy
// there. Earlier models do not validate, and a signature they never issued is
// better left off than guessed at.
func callThoughtSignature(call llm.ToolCallPart, model string) string {
	if raw, ok := strings.CutPrefix(call.Signature, llm.SignatureGemini); ok && raw != "" {
		// A signature another adapter wrote has a different prefix and falls
		// through: it would only be rejected here.
		return raw
	}
	if validatesSignatures(model) {
		return skipSignatureValidation
	}
	return ""
}

// validatesSignatures reports whether a model refuses function-call history
// without signatures, which is true from Gemini 3 on. The generation is read
// out of the name ("gemini-3-pro-preview", "models/gemini-3.1-flash") because
// a relay can serve any model and nothing else here says which one it is.
func validatesSignatures(model string) bool {
	_, rest, ok := strings.Cut(strings.ToLower(model), "gemini-")
	if !ok {
		return false
	}
	end := 0
	for end < len(rest) && rest[end] >= '0' && rest[end] <= '9' {
		end++
	}
	generation, err := strconv.Atoi(rest[:end])
	return err == nil && generation >= 3
}

// convertToolResults answers calls by NAME. This protocol has no id to match
// on, which is exactly why the decoder has to mint one and why the minted one
// must never be sent back.
func convertToolResults(message llm.Message) ([]wireRequestPart, error) {
	parts := make([]wireRequestPart, 0, len(message.Parts))
	for _, part := range message.Parts {
		result, ok := part.(llm.ToolResultPart)
		if !ok {
			return nil, llm.Errorf(llm.ErrorProvider, nil, "gemini: a tool message may only hold tool results, got %s", part.Kind())
		}
		if result.Name == "" {
			return nil, llm.Errorf(llm.ErrorProvider, nil, "gemini: tool result %q has no function name, which is the only thing this protocol matches on", result.CallID)
		}

		response := &wireFunctionResponse{Name: result.Name, Response: toolResultPayload(result)}
		if !isSynthetic(result.CallID) {
			response.ID = result.CallID
		}
		parts = append(parts, wireRequestPart{FunctionResponse: response})
	}
	return parts, nil
}

// toolResultPayload spells out a non-success rather than sending an empty
// output. A failure the model cannot see is a failure it will happily build an
// answer on top of, and this API's response field is an object, so the two can
// be kept in separate keys instead of glued into one string.
func toolResultPayload(result llm.ToolResultPart) map[string]any {
	if result.Status == llm.ToolSucceeded {
		return map[string]any{"output": result.Content}
	}

	payload := map[string]any{"status": string(result.Status)}
	if result.Error != "" {
		payload["error"] = result.Error
	}
	if result.Content != "" {
		payload["output"] = result.Content
	}
	return payload
}

// attachmentPart turns a resolved attachment into a part. An unresolved one is
// a bug in the context builder, not something to send blank: the reference is
// meaningless to the provider, and quietly dropping it would have the model
// answer about a file it never saw.
//
// The media type is passed through rather than checked against a list. This
// API accepts images, audio, video, PDFs and plain text, and which of them a
// given model takes is the model's business — a list here would go stale and
// start refusing attachments that work.
func attachmentPart(attachment llm.AttachmentPart) (wireRequestPart, error) {
	if attachment.MediaType == "" {
		return wireRequestPart{}, llm.Errorf(llm.ErrorProvider, nil, "gemini: attachment %q has no media type, and this protocol requires one", attachment.Ref)
	}
	switch {
	case attachment.URL != "":
		return wireRequestPart{FileData: &wireFileData{MIMEType: attachment.MediaType, FileURI: attachment.URL}}, nil
	case len(attachment.Data) > 0:
		return wireRequestPart{InlineData: &wireBlob{
			MIMEType: attachment.MediaType,
			Data:     base64.StdEncoding.EncodeToString(attachment.Data),
		}}, nil
	default:
		return wireRequestPart{}, llm.Errorf(llm.ErrorProvider, nil, "gemini: attachment %q was never resolved", attachment.Ref)
	}
}

func isSynthetic(id string) bool { return strings.HasPrefix(id, syntheticIDPrefix) }
