package openai

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"

	"github.com/qunqin24/openchat/apps/server/internal/llm"
	"github.com/qunqin24/openchat/apps/server/internal/providers/sse"
)

// The wire structs below are decoded leniently, like their Chat Completions
// counterparts: unknown fields and unknown event types are ignored, a frame
// that is not JSON is a protocol error.

type respEvent struct {
	Type         string        `json:"type"`
	OutputIndex  *int          `json:"output_index"`
	SummaryIndex int           `json:"summary_index"`
	Delta        string        `json:"delta"`
	Arguments    string        `json:"arguments"`
	Item         *respItem     `json:"item"`
	Response     *respResponse `json:"response"`

	// An `error` event carries its fields at the top level on OpenAI and
	// nested under "error" on some gateways.
	Code    wireCode   `json:"code"`
	Message string     `json:"message"`
	Error   *wireError `json:"error"`
}

type respItem struct {
	ID               string `json:"id"`
	Type             string `json:"type"`
	CallID           string `json:"call_id"`
	Name             string `json:"name"`
	Arguments        string `json:"arguments"`
	EncryptedContent string `json:"encrypted_content"`
	// Phase is set on an assistant message by gpt-5.3-codex and later:
	// commentary while it works, final_answer for the reply.
	Phase   string `json:"phase"`
	Summary []struct {
		Text string `json:"text"`
	} `json:"summary"`
	Content []struct {
		Type    string `json:"type"`
		Text    string `json:"text"`
		Refusal string `json:"refusal"`
	} `json:"content"`
}

type respResponse struct {
	ID                string     `json:"id"`
	Model             string     `json:"model"`
	Status            string     `json:"status"`
	Error             *wireError `json:"error"`
	IncompleteDetails *struct {
		Reason string `json:"reason"`
	} `json:"incomplete_details"`
	Usage *struct {
		InputTokens        int `json:"input_tokens"`
		OutputTokens       int `json:"output_tokens"`
		InputTokensDetails *struct {
			CachedTokens int `json:"cached_tokens"`
		} `json:"input_tokens_details"`
		OutputTokensDetails *struct {
			ReasoningTokens int `json:"reasoning_tokens"`
		} `json:"output_tokens_details"`
	} `json:"usage"`
}

// respBlock is a block plus what only this protocol adds: the encrypted
// reasoning blob and the summary section the last delta belonged to.
type respBlock struct {
	block
	redacted     []byte
	summaryIndex int
	phase        string
}

func (b *respBlock) part(model string) llm.Part {
	if b.kind == llm.PartReasoning {
		part := llm.ReasoningPart{Text: b.text.String()}
		if len(b.redacted) > 0 {
			part.Redacted = b.redacted
			part.Signature = responsesSignaturePrefix + model
		}
		return part
	}
	if b.kind == llm.PartText {
		return llm.TextPart{Text: b.text.String(), Phase: b.phase}
	}
	return b.block.part()
}

type respAssembler struct {
	cfg   Config
	model string // as requested; stamped on reasoning so it only goes back to the same model
	emit  func(llm.Event) error

	blocks  []*respBlock
	byIndex map[int]*respBlock // wire output_index -> block

	started bool
	sawTool bool
	resp    llm.ModelResponse
}

// decodeResponses turns one Responses SSE body into events and a response.
//
// Unlike Chat Completions there is no [DONE]: the stream ends with a terminal
// event (response.completed, .incomplete, .failed) or with an `error`. A body
// that simply stops is a cut-off stream, never a finished one.
func decodeResponses(ctx context.Context, body io.Reader, cfg Config, model string, emit func(llm.Event) error) (llm.ModelResponse, error) {
	a := &respAssembler{cfg: cfg, model: model, emit: emit, byIndex: map[int]*respBlock{}}
	a.resp.Provider = cfg.Provider
	a.resp.Model = model

	scanner := sse.NewScanner(body)
	for {
		if err := ctx.Err(); err != nil {
			return a.cancel(err)
		}
		if !scanner.Scan() {
			break
		}

		data := strings.TrimSpace(scanner.Event().Data)
		if data == "" || data == doneSentinel {
			continue
		}
		done, err := a.frame([]byte(data))
		if err != nil {
			return a.abort(unwrapEmit(err))
		}
		if done {
			return a.finish()
		}
	}

	if err := scanner.Err(); err != nil {
		if ctx.Err() != nil {
			return a.cancel(ctx.Err())
		}
		if errors.Is(err, sse.ErrTruncated) {
			return a.abort(llm.Errorf(llm.ErrorProtocol, err, "the model stream was cut off"))
		}
		return a.abort(llm.Errorf(llm.ErrorTransport, err, "reading the model stream"))
	}
	return a.abort(llm.Errorf(llm.ErrorProtocol, nil, "the model stream ended before the response completed"))
}

func (a *respAssembler) send(ev llm.Event) error {
	if err := a.emit(ev); err != nil {
		return emitFailure{err: err}
	}
	return nil
}

// frame handles one event and reports whether it was the terminal one.
func (a *respAssembler) frame(data []byte) (bool, error) {
	var ev respEvent
	if err := json.Unmarshal(data, &ev); err != nil {
		return false, llm.Errorf(llm.ErrorProtocol, err, "frame is not valid JSON")
	}

	switch ev.Type {
	case "":
		return false, nil
	case "error":
		wire := wireError{Code: ev.Code, Message: ev.Message}
		if ev.Error != nil {
			wire = *ev.Error
		}
		return false, eventError(wire)
	case "response.failed":
		wire := wireError{Message: "the response failed"}
		if ev.Response != nil && ev.Response.Error != nil {
			wire = *ev.Response.Error
		}
		return false, eventError(wire)
	}

	if !a.started {
		a.started = true
		if err := a.send(llm.ResponseStartEvent()); err != nil {
			return false, err
		}
	}

	switch ev.Type {
	case "response.created", "response.in_progress":
		a.noteResponse(ev.Response)
	case "response.output_item.added":
		return false, a.itemAdded(ev)
	case "response.output_text.delta", "response.refusal.delta":
		return false, a.textDelta(ev, llm.PartText, ev.Delta)
	case "response.reasoning_summary_text.delta":
		return false, a.reasoningDelta(ev, ev.SummaryIndex)
	case "response.reasoning_text.delta":
		return false, a.reasoningDelta(ev, 0)
	case "response.function_call_arguments.delta":
		return false, a.argumentsDelta(ev)
	case "response.function_call_arguments.done":
		if b := a.lookup(ev); b != nil && !b.closed && ev.Arguments != "" {
			b.args.Reset()
			b.args.WriteString(ev.Arguments)
		}
	case "response.output_item.done":
		return false, a.itemDone(ev)
	case "response.completed", "response.incomplete":
		a.noteResponse(ev.Response)
		return true, a.terminal(ev)
	}
	// Everything else — content_part.*, *.done text, web search progress,
	// queued — carries nothing the deltas and item-done events do not.
	return false, nil
}

func (a *respAssembler) noteResponse(r *respResponse) {
	if r == nil {
		return
	}
	if r.ID != "" {
		a.resp.ResponseID = r.ID
	}
	if r.Model != "" {
		a.resp.Model = r.Model
	}
}

// terminal records how the response ended. A "completed" event whose status
// says otherwise is believed over its name.
func (a *respAssembler) terminal(ev respEvent) error {
	status, reason := "completed", ""
	if ev.Response != nil {
		if ev.Response.Status != "" {
			status = ev.Response.Status
		}
		if ev.Response.IncompleteDetails != nil {
			reason = ev.Response.IncompleteDetails.Reason
		}
		a.resp.Usage = responsesUsage(ev.Response)
	}
	if ev.Type == "response.incomplete" && status == "completed" {
		status = "incomplete"
	}

	switch status {
	case "completed":
		a.resp.RawFinishReason = "completed"
		a.resp.FinishReason = llm.FinishStop
		if a.sawTool {
			a.resp.FinishReason = llm.FinishToolCalls
		}
	case "incomplete":
		a.resp.RawFinishReason = "incomplete"
		if reason != "" {
			a.resp.RawFinishReason = "incomplete:" + reason
		}
		a.resp.FinishReason = llm.FinishLength
		if reason == "content_filter" {
			a.resp.FinishReason = llm.FinishContentFilter
		}
	case "cancelled":
		// Only a background response can be cancelled, and nothing here asks
		// for one: this is someone else's stop, not ours, and the answer is
		// not complete.
		return eventError(wireError{Message: "the response was cancelled by the provider"})
	case "failed":
		wire := wireError{Message: "the response failed"}
		if ev.Response != nil && ev.Response.Error != nil {
			wire = *ev.Response.Error
		}
		return eventError(wire)
	default:
		return llm.Errorf(llm.ErrorProtocol, nil, "unknown response status %q", status)
	}
	return nil
}

func responsesUsage(r *respResponse) *llm.Usage {
	u := r.Usage
	if u == nil {
		return nil
	}
	cached := 0
	if u.InputTokensDetails != nil {
		cached = u.InputTokensDetails.CachedTokens
	}
	// input_tokens already contains the cached half, as on Chat Completions.
	usage := &llm.Usage{
		InputTokens:     max(u.InputTokens-cached, 0),
		OutputTokens:    u.OutputTokens,
		CacheReadTokens: cached,
	}
	if u.OutputTokensDetails != nil {
		usage.ReasoningTokens = u.OutputTokensDetails.ReasoningTokens
	}
	return usage
}

// eventError maps a failure the stream reported in-band onto the same kinds an
// HTTP status would have produced.
func eventError(e wireError) error {
	err := &llm.Error{Kind: llm.ErrorProvider, Code: string(e.Code), Message: e.Message}
	if err.Message == "" {
		err.Message = "the model stream reported an error"
	}
	switch {
	case isContextLength(e):
		err.Kind = llm.ErrorContextLength
	case e.Code == "invalid_api_key" || e.Type == "authentication_error":
		err.Kind = llm.ErrorAuth
	case e.Code == "429" || e.Code == "rate_limit_exceeded" || e.Type == "rate_limit_error" || e.Code == "insufficient_quota":
		err.Kind, err.Retryable = llm.ErrorRateLimit, e.Code != "insufficient_quota"
	case e.Code == "server_error" || e.Code == "overloaded" || e.Type == "server_error":
		err.Retryable = true
	}
	return err
}

func (a *respAssembler) lookup(ev respEvent) *respBlock {
	if ev.OutputIndex == nil {
		return nil
	}
	return a.byIndex[*ev.OutputIndex]
}

// open starts a block for a wire output index.
func (a *respAssembler) open(index int, kind llm.PartKind, callID, name string) (*respBlock, error) {
	b := &respBlock{block: block{index: len(a.blocks), kind: kind, callID: callID, name: name}, summaryIndex: -1}
	a.blocks = append(a.blocks, b)
	a.byIndex[index] = b

	if kind == llm.PartToolCall {
		a.sawTool = true
		return b, a.send(llm.ToolCallStartEvent(b.index, callID, name))
	}
	return b, a.send(llm.PartStartEvent(b.index, kind))
}

func outputIndex(ev respEvent) int {
	if ev.OutputIndex == nil {
		return 0
	}
	return *ev.OutputIndex
}

func (a *respAssembler) itemAdded(ev respEvent) error {
	if ev.Item == nil || ev.Item.Type != "function_call" {
		// Messages and reasoning open lazily, on their first text. An item that
		// never says anything must not leave an empty block in the history.
		return nil
	}
	index := outputIndex(ev)
	if _, known := a.byIndex[index]; known {
		return nil
	}
	_, err := a.open(index, llm.PartToolCall, ev.Item.CallID, ev.Item.Name)
	return err
}

// textBlock is the open block for this output index, or a new one. Nil means
// the block already ended and the delta is stale.
func (a *respAssembler) textBlock(ev respEvent, kind llm.PartKind) (*respBlock, error) {
	index := outputIndex(ev)
	b, ok := a.byIndex[index]
	if !ok {
		return a.open(index, kind, "", "")
	}
	if b.closed || b.kind != kind {
		return nil, nil
	}
	return b, nil
}

func (a *respAssembler) textDelta(ev respEvent, kind llm.PartKind, delta string) error {
	if delta == "" {
		return nil
	}
	b, err := a.textBlock(ev, kind)
	if b == nil || err != nil {
		return err
	}
	return a.write(b, delta)
}

func (a *respAssembler) write(b *respBlock, delta string) error {
	b.text.WriteString(delta)
	return a.send(llm.DeltaEvent(b.index, b.kind, delta))
}

func (a *respAssembler) reasoningDelta(ev respEvent, section int) error {
	if ev.Delta == "" {
		return nil
	}
	b, err := a.textBlock(ev, llm.PartReasoning)
	if b == nil || err != nil {
		return err
	}
	delta := ev.Delta
	if section > b.summaryIndex && b.text.Len() > 0 {
		// A new summary section: keep them apart the way the model wrote them.
		delta = "\n\n" + delta
	}
	b.summaryIndex = max(b.summaryIndex, section)
	return a.write(b, delta)
}

func (a *respAssembler) argumentsDelta(ev respEvent) error {
	if ev.Delta == "" {
		return nil
	}
	b := a.lookup(ev)
	if b == nil {
		var err error
		if b, err = a.open(outputIndex(ev), llm.PartToolCall, "", ""); err != nil {
			return err
		}
	}
	if b.closed || b.kind != llm.PartToolCall {
		return nil
	}
	return a.writeArguments(b, ev.Delta)
}

func (a *respAssembler) writeArguments(b *respBlock, fragment string) error {
	if limit := a.maxToolArgumentBytes(); b.args.Len()+len(fragment) > limit {
		return llm.Errorf(llm.ErrorProtocol, nil, "tool call %q arguments exceed %d bytes", b.name, limit)
	}
	b.args.WriteString(fragment)
	ev := llm.DeltaEvent(b.index, llm.PartToolCall, fragment)
	ev.ToolCallID, ev.ToolName = b.callID, b.name
	return a.send(ev)
}

func (a *respAssembler) maxToolArgumentBytes() int {
	if a.cfg.MaxToolArgumentBytes > 0 {
		return a.cfg.MaxToolArgumentBytes
	}
	return defaultMaxToolArgumentBytes
}

// itemDone is the authoritative end of an item. Anything the deltas did not
// deliver — a gateway that streams only the finished item, a reasoning item
// that is nothing but an encrypted blob — is reconciled here.
func (a *respAssembler) itemDone(ev respEvent) error {
	item := ev.Item
	if item == nil {
		return nil
	}
	index := outputIndex(ev)
	b := a.byIndex[index]

	switch item.Type {
	case "reasoning":
		var summary []string
		for _, s := range item.Summary {
			if s.Text != "" {
				summary = append(summary, s.Text)
			}
		}
		if b == nil && len(summary) == 0 && item.EncryptedContent == "" {
			return nil
		}
		if b == nil {
			var err error
			if b, err = a.open(index, llm.PartReasoning, "", ""); err != nil {
				return err
			}
		}
		if b.text.Len() == 0 && len(summary) > 0 {
			if err := a.write(b, strings.Join(summary, "\n\n")); err != nil {
				return err
			}
		}
		if item.EncryptedContent != "" {
			b.redacted = []byte(item.EncryptedContent)
		}
	case "message":
		var text strings.Builder
		for _, c := range item.Content {
			text.WriteString(c.Text)
			text.WriteString(c.Refusal)
		}
		if b == nil && text.Len() == 0 {
			return nil
		}
		if b == nil {
			var err error
			if b, err = a.open(index, llm.PartText, "", ""); err != nil {
				return err
			}
		}
		if b.text.Len() == 0 && text.Len() > 0 {
			if err := a.write(b, text.String()); err != nil {
				return err
			}
		}
		b.phase = item.Phase
	case "function_call":
		if b == nil {
			var err error
			if b, err = a.open(index, llm.PartToolCall, item.CallID, item.Name); err != nil {
				return err
			}
		}
		if item.CallID != "" {
			b.callID = item.CallID
		}
		if item.Name != "" {
			b.name = item.Name
		}
		if item.Arguments != "" && item.Arguments != b.args.String() {
			if b.args.Len() == 0 {
				if err := a.writeArguments(b, item.Arguments); err != nil {
					return err
				}
			} else {
				// The finished item is the model's final word on its arguments.
				b.args.Reset()
				b.args.WriteString(item.Arguments)
			}
		}
	default:
		return nil
	}
	return a.closeBlock(b)
}

func (a *respAssembler) closeBlock(b *respBlock) error {
	if b == nil || b.closed {
		return nil
	}
	b.closed = true
	return a.send(llm.PartEndEvent(b.index, b.part(a.model)))
}

func (a *respAssembler) closeAll() error {
	for _, b := range a.blocks {
		if err := a.closeBlock(b); err != nil {
			return err
		}
	}
	return nil
}

func (a *respAssembler) response() llm.ModelResponse {
	resp := a.resp
	parts := make([]llm.Part, 0, len(a.blocks))
	for _, b := range a.blocks {
		parts = append(parts, b.part(a.model))
	}
	resp.Message = llm.Message{Role: llm.RoleAssistant, Parts: parts}
	return resp
}

func (a *respAssembler) finish() (llm.ModelResponse, error) {
	if err := a.closeAll(); err != nil {
		return a.response(), unwrapEmit(err)
	}
	resp := a.response()
	if err := a.send(llm.ResponseEndEvent(resp)); err != nil {
		return resp, unwrapEmit(err)
	}
	return resp, nil
}

// cancel keeps what arrived and emits the single response_end a Stop owes.
func (a *respAssembler) cancel(cause error) (llm.ModelResponse, error) {
	if err := a.closeAll(); err != nil {
		return a.response(), unwrapEmit(err)
	}
	a.resp.FinishReason = llm.FinishCancelled
	resp := a.response()
	if err := a.send(llm.ResponseEndEvent(resp)); err != nil {
		return resp, unwrapEmit(err)
	}
	return resp, cause
}

// abort is the broken-stream path: no blocks closed, no response_end, and no
// finish reason, so nothing downstream can take it for a finished answer.
func (a *respAssembler) abort(err error) (llm.ModelResponse, error) {
	return a.response(), err
}
