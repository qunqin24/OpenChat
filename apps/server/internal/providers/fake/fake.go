// Package fake is a ModelClient that replays a written-down stream.
//
// It exists so the layers above — the runner, the tool loop, the subscription
// protocol — can be exercised with no network, no API key and no wall-clock
// flakiness, which is the only way their interesting cases (a tool call, a
// cancellation halfway through a sentence, a turn that ends without usage) can
// be reproduced on demand.
//
// It deliberately shares no code with the wire adapters. A test double built
// out of the parser it is meant to stand in for would hide the same bug twice.
package fake

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/qunqin24/openchat/apps/server/internal/llm"
)

// StepKind is what one scripted block produces.
type StepKind string

const (
	StepText      StepKind = "text"
	StepReasoning StepKind = "reasoning"
	StepToolCall  StepKind = "tool_call"
)

// Step is one content block, spelled out chunk by chunk. The chunking is part
// of the fixture on purpose: where a block is split is exactly what breaks
// consumers that assume a delta is a whole word, a whole line or valid JSON.
type Step struct {
	Kind   StepKind `json:"kind"`
	Chunks []string `json:"chunks"`

	ToolCallID string `json:"tool_call_id,omitempty"`
	ToolName   string `json:"tool_name,omitempty"`

	// DelayMS is how long to wait before this step. It is only useful for
	// exercising cancellation; fixtures used in assertions leave it at zero so
	// the tests stay fast and deterministic.
	DelayMS int `json:"delay_ms,omitempty"`
}

// Usage mirrors llm.Usage in fixture form, with the same arithmetic rules:
// input excludes the cache, reasoning is part of output.
type Usage struct {
	InputTokens      int `json:"input_tokens"`
	OutputTokens     int `json:"output_tokens"`
	ReasoningTokens  int `json:"reasoning_tokens,omitempty"`
	CacheReadTokens  int `json:"cache_read_tokens,omitempty"`
	CacheWriteTokens int `json:"cache_write_tokens,omitempty"`
}

// Failure injects an error after a given number of steps, so the layers above
// can be tested against a provider that dies mid-answer.
type Failure struct {
	AfterStep int    `json:"after_step"`
	Kind      string `json:"kind"`
	Message   string `json:"message"`
}

type Script struct {
	Provider     string   `json:"provider"`
	Model        string   `json:"model"`
	ResponseID   string   `json:"response_id,omitempty"`
	Steps        []Step   `json:"steps"`
	FinishReason string   `json:"finish_reason"`
	Usage        *Usage   `json:"usage,omitempty"`
	Failure      *Failure `json:"failure,omitempty"`
}

// Provider replays a Script. A zero Provider is not usable: build one through
// New or Load so the script is validated once, up front, rather than failing
// halfway through a stream.
type Provider struct {
	script Script
}

var _ llm.ModelClient = (*Provider)(nil)

func New(script Script) (*Provider, error) {
	if err := validate(script); err != nil {
		return nil, err
	}
	return &Provider{script: script}, nil
}

// Load builds a Provider from fixture JSON.
func Load(data []byte) (*Provider, error) {
	var script Script
	decoder := json.NewDecoder(bytes.NewReader(data))
	// Strict here, unlike the wire adapters: a fixture is ours, and a typo in
	// one silently producing a stream that tests nothing is worse than a
	// failing load.
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&script); err != nil {
		return nil, fmt.Errorf("fake: reading the script: %w", err)
	}
	return New(script)
}

func LoadFile(path string) (*Provider, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("fake: %w", err)
	}
	return Load(data)
}

func validate(script Script) error {
	if _, err := finishReason(script.FinishReason); err != nil {
		return err
	}
	for i, step := range script.Steps {
		switch step.Kind {
		case StepText, StepReasoning:
		case StepToolCall:
			if step.ToolName == "" {
				return fmt.Errorf("fake: step %d is a tool call with no name", i)
			}
		default:
			return fmt.Errorf("fake: step %d has unknown kind %q", i, step.Kind)
		}
	}
	if f := script.Failure; f != nil && f.AfterStep > len(script.Steps) {
		return fmt.Errorf("fake: failure is scheduled after step %d of %d", f.AfterStep, len(script.Steps))
	}
	return nil
}

func finishReason(raw string) (llm.FinishReason, error) {
	switch llm.FinishReason(raw) {
	case llm.FinishStop, llm.FinishLength, llm.FinishToolCalls, llm.FinishContentFilter, llm.FinishCancelled:
		return llm.FinishReason(raw), nil
	}
	return "", fmt.Errorf("fake: unknown finish reason %q", raw)
}

func (p *Provider) Stream(ctx context.Context, req llm.Request, emit func(llm.Event) error) (llm.ModelResponse, error) {
	run := &replay{script: p.script, emit: emit}
	run.resp.Provider = p.script.Provider
	run.resp.ResponseID = p.script.ResponseID
	run.resp.Model = p.script.Model
	if run.resp.Model == "" {
		run.resp.Model = req.Model
	}

	if err := emit(llm.ResponseStartEvent()); err != nil {
		return run.response(), err
	}

	for i, step := range p.script.Steps {
		if err := run.step(ctx, step); err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil && errors.Is(err, ctxErr) {
				return run.cancel(ctxErr)
			}
			return run.response(), err
		}
		if f := p.script.Failure; f != nil && f.AfterStep == i+1 {
			// No response_end: a turn that broke has no boundary to publish,
			// same as the real adapters.
			return run.response(), &llm.Error{Kind: llm.ErrorKind(f.Kind), Message: f.Message}
		}
	}

	reason, err := finishReason(p.script.FinishReason)
	if err != nil {
		return run.response(), err
	}
	run.resp.FinishReason = reason
	run.resp.RawFinishReason = p.script.FinishReason
	if p.script.Usage != nil {
		u := *p.script.Usage
		run.resp.Usage = &llm.Usage{
			InputTokens:      u.InputTokens,
			OutputTokens:     u.OutputTokens,
			ReasoningTokens:  u.ReasoningTokens,
			CacheReadTokens:  u.CacheReadTokens,
			CacheWriteTokens: u.CacheWriteTokens,
		}
	}

	resp := run.response()
	if err := emit(llm.ResponseEndEvent(resp)); err != nil {
		return resp, err
	}
	return resp, nil
}

// replay is the state of one Stream call. Provider itself stays immutable so a
// single one can serve concurrent runs.
type replay struct {
	script Script
	emit   func(llm.Event) error

	parts []llm.Part
	resp  llm.ModelResponse

	// open is the block currently being written, kept so a cancellation can
	// close it with everything that arrived so far.
	open      *llm.Part
	openIndex int
}

func (r *replay) step(ctx context.Context, step Step) error {
	if err := wait(ctx, time.Duration(step.DelayMS)*time.Millisecond); err != nil {
		return err
	}

	index := len(r.parts)
	kind := partKind(step.Kind)

	start := llm.PartStartEvent(index, kind)
	if kind == llm.PartToolCall {
		start = llm.ToolCallStartEvent(index, step.ToolCallID, step.ToolName)
	}
	if err := r.emit(start); err != nil {
		return err
	}

	var body strings.Builder
	r.openIndex = index
	r.setOpen(kind, step, body.String())

	for _, chunk := range step.Chunks {
		if err := ctx.Err(); err != nil {
			return err
		}
		body.WriteString(chunk)
		r.setOpen(kind, step, body.String())

		ev := llm.DeltaEvent(index, kind, chunk)
		if kind == llm.PartToolCall {
			ev.ToolCallID, ev.ToolName = step.ToolCallID, step.ToolName
		}
		if err := r.emit(ev); err != nil {
			return err
		}
	}

	part := buildPart(kind, step, body.String())
	r.open = nil
	r.parts = append(r.parts, part)
	return r.emit(llm.PartEndEvent(index, part))
}

func (r *replay) setOpen(kind llm.PartKind, step Step, body string) {
	part := buildPart(kind, step, body)
	r.open = &part
}

func buildPart(kind llm.PartKind, step Step, body string) llm.Part {
	switch kind {
	case llm.PartReasoning:
		return llm.ReasoningPart{Text: body}
	case llm.PartToolCall:
		call := llm.ToolCallPart{ID: step.ToolCallID, Name: step.ToolName, RawArguments: body}
		// Same rule as the wire adapters: parse once, at the end, and never
		// invent an empty object for something that did not parse.
		if trimmed := strings.TrimSpace(body); trimmed != "" && json.Valid([]byte(trimmed)) {
			call.Arguments = json.RawMessage(trimmed)
		}
		return call
	default:
		return llm.TextPart{Text: body}
	}
}

func partKind(kind StepKind) llm.PartKind {
	switch kind {
	case StepReasoning:
		return llm.PartReasoning
	case StepToolCall:
		return llm.PartToolCall
	default:
		return llm.PartText
	}
}

// response is the turn as it stands, including a block still being written.
func (r *replay) response() llm.ModelResponse {
	resp := r.resp
	parts := make([]llm.Part, 0, len(r.parts)+1)
	parts = append(parts, r.parts...)
	if r.open != nil {
		parts = append(parts, *r.open)
	}
	resp.Message = llm.Message{Role: llm.RoleAssistant, Parts: parts}
	return resp
}

// cancel closes the open block, keeps everything generated so far and publishes
// exactly one response_end — the same shape as a completed turn, so terminal
// events stay countable.
func (r *replay) cancel(cause error) (llm.ModelResponse, error) {
	if r.open != nil {
		part := *r.open
		r.open = nil
		r.parts = append(r.parts, part)
		if err := r.emit(llm.PartEndEvent(r.openIndex, part)); err != nil {
			return r.response(), err
		}
	}

	r.resp.FinishReason = llm.FinishCancelled
	resp := r.response()
	if err := r.emit(llm.ResponseEndEvent(resp)); err != nil {
		return resp, err
	}
	return resp, cause
}

func wait(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
