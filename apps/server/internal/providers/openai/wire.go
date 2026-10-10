package openai

import "encoding/json"

// The structs here mirror the Chat Completions streaming payload. They are
// decoded leniently on purpose: providers add fields constantly, and rejecting
// a chunk because it carries one we have never heard of would break a working
// endpoint for no reason. Frames that are not valid JSON at all are a different
// matter and do fail — see decode.go.

type chunk struct {
	ID      string        `json:"id"`
	Model   string        `json:"model"`
	Choices []chunkChoice `json:"choices"`
	Usage   *wireUsage    `json:"usage"`

	// Error appears when a gateway reports a mid-stream failure inside a data
	// frame instead of an HTTP status, which several of them do.
	Error *wireError `json:"error"`
}

type chunkChoice struct {
	Index        int     `json:"index"`
	Delta        delta   `json:"delta"`
	FinishReason *string `json:"finish_reason"`
}

type delta struct {
	Role    string  `json:"role"`
	Content *string `json:"content"`

	// Two spellings for the same thing: DeepSeek and its imitators send
	// reasoning_content, OpenRouter and several gateways send reasoning. Both
	// are accepted because refusing one means refusing a whole family of
	// endpoints over a field name.
	ReasoningContent *string `json:"reasoning_content"`
	Reasoning        *string `json:"reasoning"`

	// ReasoningDetails is OpenRouter's structured record of the same thinking,
	// with the signatures and encrypted state that have to come back. Left raw
	// and parsed on the side (see parseDetails): in another shape it is
	// ignored rather than failing the frame.
	ReasoningDetails json.RawMessage `json:"reasoning_details"`

	ToolCalls []toolCallDelta `json:"tool_calls"`
}

type toolCallDelta struct {
	// Index groups fragments of the same call. It is the only thing that always
	// arrives: id and name may show up on any fragment, or not until the end.
	Index *int   `json:"index"`
	ID    string `json:"id"`
	Type  string `json:"type"`

	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type wireUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`

	PromptTokensDetails *struct {
		CachedTokens int `json:"cached_tokens"`
	} `json:"prompt_tokens_details"`

	CompletionTokensDetails *struct {
		ReasoningTokens int `json:"reasoning_tokens"`
	} `json:"completion_tokens_details"`

	// DeepSeek's own spelling of the cache split, reported alongside a prompt
	// total that already includes both halves.
	PromptCacheHitTokens  int `json:"prompt_cache_hit_tokens"`
	PromptCacheMissTokens int `json:"prompt_cache_miss_tokens"`
}

type wireError struct {
	Message string   `json:"message"`
	Type    string   `json:"type"`
	Code    wireCode `json:"code"`
}

// wireCode is an error code as a string, whichever way it was sent. OpenAI
// sends "rate_limit_exceeded"; OpenRouter sends the HTTP status as a number
// (429) inside a mid-stream error frame, and a field that cannot be decoded
// would take the whole frame — and the message beside it — down with it.
type wireCode string

func (c *wireCode) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		*c = wireCode(s)
		return nil
	}
	var n json.Number
	if err := json.Unmarshal(data, &n); err == nil {
		*c = wireCode(n.String())
		return nil
	}
	// null, or a shape nobody documented: no code is better than no message.
	*c = ""
	return nil
}

// wireMessage is the request-side message. Content is `any` because the
// protocol accepts either a string or a list of typed blocks, and sending the
// list form for plain text upsets older gateways.
type wireMessage struct {
	Role    string `json:"role"`
	Content any    `json:"content,omitempty"`

	// ReasoningContent is the model's earlier thinking handed back, in the
	// field the vendors that want it read it from. Never folded into Content:
	// that would present the thinking as something the model said.
	ReasoningContent string `json:"reasoning_content,omitempty"`

	// ReasoningDetails is OpenRouter's record of that thinking, sent back
	// unmodified instead of ReasoningContent when there is one.
	ReasoningDetails json.RawMessage `json:"reasoning_details,omitempty"`

	ToolCalls  []wireToolCall `json:"tool_calls,omitempty"`
	ToolCallID string         `json:"tool_call_id,omitempty"`
}

type wireToolCall struct {
	ID       string           `json:"id"`
	Type     string           `json:"type"`
	Function wireToolFunction `json:"function"`
}

type wireToolFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type wireContentBlock struct {
	Type     string        `json:"type"`
	Text     string        `json:"text,omitempty"`
	ImageURL *wireImageURL `json:"image_url,omitempty"`
	File     *wireFile     `json:"file,omitempty"`
}

// wireFile is a document sent inline, as a data URL.
type wireFile struct {
	Filename string `json:"filename,omitempty"`
	FileData string `json:"file_data"`
}

type wireImageURL struct {
	URL string `json:"url"`
}
