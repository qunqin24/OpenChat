package llm

import "context"

// TranscriptionClient turns a recording into text. Like ImageClient it stands
// beside ModelClient: one call, no stream, no conversation
// (docs/design/speech-input-plan.md).
type TranscriptionClient interface {
	Transcribe(ctx context.Context, req TranscriptionRequest) (TranscriptionResponse, error)
}

// TranscriptionRequest is one recording.
type TranscriptionRequest struct {
	Model string
	Audio []byte
	// MediaType is what the recording is: audio/webm, audio/mp4 ...
	MediaType string
	// User is the end-user identifier, sent only to channels that take one.
	User string
}

type TranscriptionResponse struct {
	Text string
	// Seconds is the length the provider says it billed, 0 when it said
	// nothing.
	Seconds float64
	// Usage is what the provider reported in tokens, nil when it said nothing.
	Usage       *Usage
	Provider    string
	ServedModel string
}
