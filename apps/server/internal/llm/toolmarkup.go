package llm

import (
	"context"
	"regexp"
	"strings"
)

// A model's own tool-call markup, leaked into its answer as text.
//
// DeepSeek's models write tool calls in DSML — `<｜DSML｜ calls>`, an
// `invoke` per call, a `parameter` per argument — and the server that runs the
// model is meant to turn that into the API's tool_calls (vLLM and SGLang carry a
// parser for each version's tags). Now and then a gateway misses one, and the
// markup arrives as the answer's text.
//
// It is not executed here. A parse of markup that was never meant to reach us
// is a guess, and a wrong guess is a tool actually run; neither OpenCode nor
// Cline do it. What is done is what Cline does with it: keep it out of sight
// and out of the history the model is sent next (where it would teach the
// model to write more of it), and say the answer did not finish, so the person
// can ask again.

// toolMarkupStart opens a block of calls: `function_calls` (V3.2),
// `tool_calls` (V4) or ` calls` (V4.1), the bar full-width as the model writes
// it or plain as some gateways re-encode it.
var toolMarkupStart = regexp.MustCompile(`<[｜|]\s*DSML\s*[｜|]\s*(?:function_calls|tool_calls|calls)\s*>`)

var toolMarkupEnd = regexp.MustCompile(`</[｜|]\s*DSML\s*[｜|]\s*(?:function_calls|tool_calls|calls)\s*>`)

// StripToolMarkup removes leaked blocks of tool-call markup from text, each
// from its opening tag to its closing tag, or to the end where it was cut off.
// found says whether there was any.
func StripToolMarkup(text string) (clean string, found bool) {
	var b strings.Builder
	rest := text
	for {
		start := toolMarkupStart.FindStringIndex(rest)
		if start == nil {
			b.WriteString(rest)
			break
		}
		found = true
		b.WriteString(rest[:start[0]])
		after := rest[start[1]:]
		end := toolMarkupEnd.FindStringIndex(after)
		if end == nil {
			break
		}
		rest = after[end[1]:]
	}
	if !found {
		return text, false
	}
	return strings.TrimRight(b.String(), " \t\r\n"), true
}

// mayBeginMarkup is whether the text from the last `<` on could still grow
// into an opening tag, and so is held back while a reply streams. A tag that
// has closed without matching is ordinary text.
func mayBeginMarkup(tail string) bool {
	if strings.Contains(tail, ">") {
		return false
	}
	for _, lead := range []string{"<｜DSML", "<|DSML"} {
		if strings.HasPrefix(lead, tail) || strings.HasPrefix(tail, lead) {
			return true
		}
	}
	return false
}

// HideToolMarkup wraps a client so leaked tool-call markup never reaches the
// reader: held back while the reply streams, cut from the finished text, and
// the response marked with LeakedToolCalls.
func HideToolMarkup(inner ModelClient) ModelClient { return &markupHider{inner: inner} }

type markupHider struct{ inner ModelClient }

// Unwrap gives the wrapped client, so capability checks can see through.
func (h *markupHider) Unwrap() ModelClient { return h.inner }

type heldText struct {
	text    strings.Builder
	emitted int  // bytes of text already passed on
	hidden  bool // a block has begun: nothing more of this part is shown
}

func (h *markupHider) Stream(ctx context.Context, req Request, emit func(Event) error) (ModelResponse, error) {
	parts := map[int]*heldText{}
	wrapped := func(ev Event) error {
		switch {
		case ev.Type == EventPartDelta && ev.PartKind == PartText:
			held := parts[ev.Index]
			if held == nil {
				held = &heldText{}
				parts[ev.Index] = held
			}
			held.text.WriteString(ev.Delta)
			if held.hidden {
				return nil
			}
			text := held.text.String()
			upTo := len(text)
			if at := toolMarkupStart.FindStringIndex(text); at != nil {
				held.hidden = true
				upTo = at[0]
			} else if at := strings.LastIndex(text, "<"); at >= 0 && mayBeginMarkup(text[at:]) {
				// Could still be the start of a block: wait for more.
				upTo = at
			}
			if upTo <= held.emitted {
				return nil
			}
			ev.Delta = text[held.emitted:upTo]
			held.emitted = upTo
			return emit(ev)
		case ev.Type == EventPartEnd && ev.PartKind == PartText:
			part, ok := ev.Part.(TextPart)
			if !ok {
				return emit(ev)
			}
			clean, _ := StripToolMarkup(part.Text)
			// What was held back and turned out to be ordinary text goes out
			// before the end, so the streamed text matches the finished part.
			if held := parts[ev.Index]; held != nil && !held.hidden && len(clean) > held.emitted {
				if err := emit(Event{Type: EventPartDelta, Index: ev.Index, PartKind: PartText, Delta: clean[held.emitted:]}); err != nil {
					return err
				}
			}
			part.Text = clean
			ev.Part = part
			return emit(ev)
		case ev.Type == EventResponseEnd && ev.Response != nil:
			resp := hideInResponse(*ev.Response)
			ev.Response = &resp
			return emit(ev)
		}
		return emit(ev)
	}
	resp, err := h.inner.Stream(ctx, req, wrapped)
	return hideInResponse(resp), err
}

// hideInResponse cuts leaked markup from a finished response's text parts,
// dropping a part it empties, and notes that it did.
func hideInResponse(resp ModelResponse) ModelResponse {
	var parts []Part
	for _, part := range resp.Message.Parts {
		text, ok := part.(TextPart)
		if !ok {
			parts = append(parts, part)
			continue
		}
		clean, found := StripToolMarkup(text.Text)
		if !found {
			parts = append(parts, part)
			continue
		}
		resp.LeakedToolCalls = true
		if strings.TrimSpace(clean) == "" {
			continue
		}
		text.Text = clean
		parts = append(parts, text)
	}
	if resp.LeakedToolCalls {
		resp.Message.Parts = parts
	}
	return resp
}
