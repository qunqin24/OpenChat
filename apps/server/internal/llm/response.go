package llm

// FinishReason is why one model turn stopped.
//
// The zero value means the provider never said, which is a protocol failure
// rather than a quiet "stop": a stream that ends without a reason is
// indistinguishable from a stream that was cut off, and treating the two alike
// is how a truncated answer gets presented as a complete one.
type FinishReason string

const (
	FinishStop          FinishReason = "stop"
	FinishLength        FinishReason = "length"
	FinishToolCalls     FinishReason = "tool_calls"
	FinishContentFilter FinishReason = "content_filter"

	// FinishCancelled is the user pressing Stop or the Run's deadline expiring.
	// It is a legitimate end to a turn — the content generated so far is kept —
	// and never a failure.
	FinishCancelled FinishReason = "cancelled"
)

// Usage is what the turn cost.
//
// The field split matters more than the numbers: providers disagree about what
// their own totals include, so the arithmetic is pinned down here once. Adding
// every field together must never double-count.
type Usage struct {
	// InputTokens excludes CacheReadTokens and CacheWriteTokens. Adapters
	// subtract them out, because most wire formats report a prompt total that
	// already contains the cached part and billing them twice is a real bill.
	InputTokens int

	// OutputTokens is everything the model generated, reasoning included.
	OutputTokens int

	// ReasoningTokens is a subset of OutputTokens, not an addition to it.
	ReasoningTokens int

	CacheReadTokens  int
	CacheWriteTokens int
}

// ModelResponse is the authoritative version of one model turn. It is what the
// run layer stores; the events that preceded it are a live view of the same
// thing arriving piece by piece.
type ModelResponse struct {
	// Message holds the assembled content in block order.
	Message Message

	// Model is what the provider says it actually served, which is not always
	// what was asked for: aliases, gateways and silent upgrades all show up
	// here, and a support question six months from now will want the truth.
	Model string

	// Provider is the configured identity that served the turn — not the wire
	// protocol, which many providers share.
	Provider string

	// ResponseID is the provider's id for this turn, kept because some
	// protocols need it to continue a conversation on their side.
	ResponseID string

	FinishReason FinishReason

	// RawFinishReason is the provider's own spelling. Our mapping is lossy on
	// purpose; the original is cheap to keep and impossible to reconstruct.
	RawFinishReason string

	// Usage is nil when the provider reported nothing. Nil and zero are
	// different facts: an unreported turn was not a free one.
	Usage *Usage

	// Metadata holds provider-native leftovers worth keeping — tool namespaces,
	// routing hints, reasoning details a specific vendor needs echoed back.
	// Keys are owned by the adapter that wrote them; nothing generic reads it.
	Metadata map[string]any
}
