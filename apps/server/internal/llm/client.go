package llm

import "context"

// ModelClient is the entire provider surface: one call, one model turn. Adding
// a provider that speaks a protocol we already implement should mean new
// configuration and new fixtures, not a new branch anywhere above this line.
//
// emit faces the run layer, never a browser. It is where structured events go
// to be persisted and fanned out to however many subscribers a Run happens to
// have, so a subscriber hanging up is not visible here and must never surface
// as an error. An error from emit means the run cannot continue — a required
// write failed — and aborts the stream.
//
// The returned ModelResponse is authoritative, and it is returned even when err
// is non-nil: a cancelled or broken stream still produced tokens that were paid
// for, and discarding them bills the user for text nobody ever sees.
//
// Implementations must:
//
//   - Emit at most one EventResponseEnd, and only when the turn reached a
//     boundary worth keeping — a clean finish or a cancellation. A stream that
//     broke mid-frame has no trustworthy boundary, so it gets no such event and
//     the caller decides what the partial content is worth.
//   - Never emit EventToolCallsRequested or EventRunEnd. The tool loop and the
//     Run belong to the layer above; a provider cannot see either.
//   - Return ctx.Err() on cancellation, with the partial response and
//     FinishCancelled set, so callers can tell a stop from a failure without
//     inspecting strings.
type ModelClient interface {
	Stream(ctx context.Context, req Request, emit func(Event) error) (ModelResponse, error)
}
