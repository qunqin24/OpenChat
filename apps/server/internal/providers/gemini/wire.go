package gemini

import "encoding/json"

// The structs here mirror GenerateContentResponse and the request that produces
// it. They are decoded leniently — unknown fields are ignored, because this API
// gains them regularly and refusing a frame over one would break a working
// endpoint. Frames that are not valid JSON at all are a different matter and do
// fail; see decode.go.

// generateResponse is one SSE frame. Unlike the two delta protocols, every
// frame here is a whole response object: the parts inside it are the new
// content, and everything around them is restated each time.
type generateResponse struct {
	Candidates    []candidate    `json:"candidates"`
	UsageMetadata *usageMetadata `json:"usageMetadata"`

	// ModelVersion is what was actually served, which aliases and silent
	// upgrades make worth keeping.
	ModelVersion string `json:"modelVersion"`
	ResponseID   string `json:"responseId"`

	// PromptFeedback reports a prompt that was refused before generation. It
	// arrives instead of content, not alongside it.
	PromptFeedback *promptFeedback `json:"promptFeedback"`

	// Error appears when the endpoint reports a mid-stream failure inside a
	// data frame instead of an HTTP status.
	Error *wireError `json:"error"`
}

type candidate struct {
	Index   int `json:"index"`
	Content struct {
		Role  string     `json:"role"`
		Parts []wirePart `json:"parts"`
	} `json:"content"`
	FinishReason string `json:"finishReason"`
}

// wirePart is one piece of a candidate's content.
//
// Text is a pointer because an empty text part and an absent one are different
// facts: the first is a preamble to skip, the second is a part carrying
// something this adapter does not understand, which is not the same as nothing.
type wirePart struct {
	Text *string `json:"text"`

	// Thought marks Text as the model's private reasoning. Missing it puts the
	// deliberation into the visible answer.
	Thought bool `json:"thought"`

	// ThoughtSignature is this API's attestation over a thought.
	ThoughtSignature string `json:"thoughtSignature"`

	FunctionCall *wireFunctionCall `json:"functionCall"`
}

// wireFunctionCall arrives complete: Args is a parsed object, not a string
// assembled from fragments. There is nothing to accumulate and nothing to wait
// for, which is why a tool block on this protocol is final the moment it lands.
type wireFunctionCall struct {
	// ID is absent on most deployments. When it is, the adapter mints one —
	// see syntheticID.
	ID   string          `json:"id"`
	Name string          `json:"name"`
	Args json.RawMessage `json:"args"`
}

type usageMetadata struct {
	// PromptTokenCount already contains CachedContentTokenCount, the way Chat
	// Completions reports it and unlike the Messages API. See convertUsage.
	PromptTokenCount int `json:"promptTokenCount"`

	// CandidatesTokenCount counts the visible answer only. Thoughts are counted
	// beside it, not inside it.
	CandidatesTokenCount    int `json:"candidatesTokenCount"`
	ThoughtsTokenCount      int `json:"thoughtsTokenCount"`
	CachedContentTokenCount int `json:"cachedContentTokenCount"`
	TotalTokenCount         int `json:"totalTokenCount"`
}

type promptFeedback struct {
	BlockReason string `json:"blockReason"`
}

type wireError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Status  string `json:"status"`
}

// wireContent is the request-side turn. The role is "user" or "model" — there
// is no "assistant" and no "system" here; a system prompt is a separate
// top-level field, and tool results are user content.
type wireContent struct {
	Role  string            `json:"role,omitempty"`
	Parts []wireRequestPart `json:"parts"`
}

type wireRequestPart struct {
	Text             string                `json:"text,omitempty"`
	Thought          bool                  `json:"thought,omitempty"`
	ThoughtSignature string                `json:"thoughtSignature,omitempty"`
	FunctionCall     *wireFunctionCall     `json:"functionCall,omitempty"`
	FunctionResponse *wireFunctionResponse `json:"functionResponse,omitempty"`
	InlineData       *wireBlob             `json:"inlineData,omitempty"`
	FileData         *wireFileData         `json:"fileData,omitempty"`
}

// wireFunctionResponse answers a call. It is matched to the call by Name, not
// by an id — which is the whole reason ids have to be synthesised on the way
// in, and why a synthesised one must never be sent back out.
type wireFunctionResponse struct {
	ID       string         `json:"id,omitempty"`
	Name     string         `json:"name"`
	Response map[string]any `json:"response"`
}

type wireBlob struct {
	MIMEType string `json:"mimeType"`
	Data     string `json:"data"`
}

type wireFileData struct {
	MIMEType string `json:"mimeType,omitempty"`
	FileURI  string `json:"fileUri"`
}
