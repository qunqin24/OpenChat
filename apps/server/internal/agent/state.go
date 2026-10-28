package agent

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/qunqin24/openchat/apps/server/internal/llm"
)

// Phases of a run's checkpoint. A run alternates between them — prepare a
// request and stream it, then run the tools it asked for — until a response
// asks for nothing.
const (
	PhasePrepare = "prepare"
	PhaseTools   = "tools"
)

// Checkpoint is `runs.checkpoint`: where a restarted process picks a run up.
type Checkpoint struct {
	Phase string `json:"phase"`
	// Attempt counts retries of the current request, from 1.
	Attempt int `json:"attempt"`
	// Round counts tool rounds so far. It bounds the run: past the limit the
	// model is asked once more with no tools, and has to answer.
	Round int `json:"round"`
	// Assistant is the response whose calls the tools phase is running.
	Assistant *uuid.UUID `json:"assistant,omitempty"`
}

// Tool slot statuses.
const (
	SlotPending = "pending"
	SlotRunning = "running"
	SlotDone    = "done"
	// SlotAwaiting is a call to a UserTool: nothing runs, and the person's
	// answer is its result. It ends the run once the round's other calls have.
	SlotAwaiting = "awaiting"
)

// Live is `runs.live`: the work in flight, which is not a message yet.
//
// It is pi-durable's `pi.live` document. Everything here is committed before a
// subscriber sees it, and everything here disappears once it has become a
// message, so it stays small: one partial response, or one round of tool slots.
type Live struct {
	Generation *LiveGeneration `json:"generation,omitempty"`
	Tools      []ToolSlot      `json:"tools,omitempty"`
	// Compacting is set while the conversation's context is being compressed
	// before a request: work in flight, shown as a status line. It is only
	// true for the length of one compression (settleCompaction clears what a
	// process that died left).
	Compacting bool `json:"compacting,omitempty"`
}

// LiveGeneration is the response being streamed, as of its last commit.
type LiveGeneration struct {
	Attempt int       `json:"attempt"`
	Parts   llm.Parts `json:"parts,omitempty"`
	// Retry is set while the run waits out a backoff before this attempt.
	Retry *Retry `json:"retry,omitempty"`
}

type Retry struct {
	At        time.Time `json:"at"`
	ErrorCode string    `json:"errorCode"`
}

// ToolSlot is one call of the current round.
type ToolSlot struct {
	CallID string `json:"callId"`
	Name   string `json:"name"`
	Status string `json:"status"`
	// Arguments are recorded with the intent, before the tool runs: the effect
	// sandwich's first slice. A process that dies after this commit knows the
	// call may have happened.
	Arguments json.RawMessage `json:"arguments,omitempty"`
	// Message is the tool row holding the result, once there is one.
	Message *uuid.UUID `json:"message,omitempty"`
}

func decodeCheckpoint(data []byte) (Checkpoint, error) {
	cp := Checkpoint{Phase: PhasePrepare, Attempt: 1}
	if len(data) == 0 {
		return cp, nil
	}
	if err := json.Unmarshal(data, &cp); err != nil {
		return cp, fmt.Errorf("agent: decoding checkpoint: %w", err)
	}
	if cp.Attempt < 1 {
		cp.Attempt = 1
	}
	return cp, nil
}

func decodeLive(data []byte) (Live, error) {
	var live Live
	if len(data) == 0 {
		return live, nil
	}
	if err := json.Unmarshal(data, &live); err != nil {
		return live, fmt.Errorf("agent: decoding live state: %w", err)
	}
	return live, nil
}

// DecodeLive is decodeLive for the HTTP layer's snapshot.
func DecodeLive(data []byte) (Live, error) { return decodeLive(data) }

func mustJSON(v any) []byte {
	data, err := json.Marshal(v)
	if err != nil {
		panic(fmt.Sprintf("agent: encoding %T: %v", v, err))
	}
	return data
}
