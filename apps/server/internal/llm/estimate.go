package llm

import "unicode"

// Estimating how many tokens a piece of text, a request or an answer takes,
// without a tokenizer.
//
// A tokenizer is a model's own and there are dozens of them; what is wanted
// here is only a figure that is close and errs high: a CJK character is about
// one token, anything else about a quarter of one (four characters to the token
// is the usual figure for English). Two things lean on it: the run layer, to
// know that a request is getting close to a window, early enough to do
// something about it; and the usage ledger, to price a request whose provider
// never said what it cost (a stream that was stopped, or broke, before the
// usage frame). The figure the provider reports is always the truth; this is
// for the decision, or the row, that has to be made without one.

const (
	// EstimateSafety widens every estimate: guessing low means a request that
	// is refused for its length, or a spend nobody counts; guessing high costs
	// one early summary, or a cap that trips a little early.
	EstimateSafety = 1.25
	// MessageOverhead is what a message costs besides its text: the role and
	// the framing a wire format puts around it.
	MessageOverhead = 4
	// AttachmentTokens stands in for a file. A picture costs a model anywhere
	// from a few hundred to a few thousand tokens by its size, and a PDF by
	// its pages; the bytes say little about either.
	AttachmentTokens = 1500
)

// EstimateText is the rough token count of a piece of text: a CJK character
// (and the punctuation and full-width forms that go with them) at one token,
// everything else at four characters to the token, rounded up.
func EstimateText(text string) int {
	cjk, other := 0, 0
	for _, r := range text {
		if isCJK(r) {
			cjk++
		} else {
			other++
		}
	}
	return cjk + (other+3)/4
}

func isCJK(r rune) bool {
	switch {
	case r < 0x2E80: // nothing below is CJK; the common case, decided at once
		return false
	case unicode.Is(unicode.Han, r), unicode.Is(unicode.Hiragana, r), unicode.Is(unicode.Katakana, r), unicode.Is(unicode.Hangul, r):
		return true
	case r >= 0x3000 && r <= 0x303F, // CJK symbols and punctuation
		r >= 0xFF00 && r <= 0xFFEF: // half- and full-width forms
		return true
	}
	return false
}

// EstimateCall is the rough token count of a tool call: its name and whatever
// arguments arrived, parsed or not.
func EstimateCall(call ToolCallPart) int {
	args := call.RawArguments
	if args == "" {
		args = string(call.Arguments)
	}
	return EstimateText(call.Name) + EstimateText(args)
}

// EstimatePart is the rough token count of one block of a message in a
// request's history. Reasoning is not counted: providers drop the thinking of
// earlier turns from the window, and an estimate meant to be early should not
// be early for text that is not there.
func EstimatePart(part Part) int {
	switch p := part.(type) {
	case TextPart:
		return EstimateText(p.Text)
	case ToolCallPart:
		return EstimateCall(p)
	case ToolResultPart:
		return EstimateText(p.Content)
	case AttachmentPart:
		// A text file goes as its words: about a token for every three bytes
		// (Chinese is three bytes a character, English four a token). A long
		// one goes as a card - unless the model cannot call tools, which is
		// rare enough to leave to the recovery from a context too long.
		// A document goes as the words read out of it, a PDF too when it is
		// longer than a model reads whole.
		size := int64(0)
		switch ModalityOf(p.MediaType) {
		case ModalityText:
			size = p.Size
		case ModalityDocument:
			size = p.TextSize
		case ModalityPDF:
			if p.Pages > NativePDFPages {
				size = p.TextSize
			} else if p.Pages > 0 {
				return p.Pages*AttachmentTokens + MessageOverhead
			}
		}
		if size > InlineTextBytes {
			return FileCardBytes/3 + MessageOverhead
		}
		if size > 0 {
			return int(size/3) + MessageOverhead
		}
		return AttachmentTokens
	}
	return 0
}

// EstimateTools is the rough token count of the tool definitions a request
// offers.
func EstimateTools(tools []Tool) int {
	n := 0
	for _, tool := range tools {
		n += EstimateText(tool.Name) + EstimateText(tool.Description) + EstimateText(string(tool.Parameters)) + MessageOverhead
	}
	return n
}

// Widen applies the safety factor to a raw count.
func Widen(n int) int { return int(float64(n) * EstimateSafety) }

// EstimateInput is the estimate for what a request sends, as it will go out:
// every message, the tools offered, widened by the safety factor. Unlike the
// history estimate of the run layer it counts a reasoning block too, since this
// is the figure a request is billed by when the provider does not say.
func EstimateInput(req Request) int {
	n := EstimateTools(req.Tools)
	for _, msg := range req.Messages {
		n += MessageOverhead
		for _, part := range msg.Parts {
			if r, ok := part.(ReasoningPart); ok {
				n += EstimateText(r.Text)
				continue
			}
			n += EstimatePart(part)
		}
	}
	return Widen(n)
}

// EstimateOutput is the estimate for what a model has written so far: its text,
// its reasoning and the arguments of its tool calls, widened by the safety
// factor. Reasoning is returned apart as well, since a ledger row reports it as
// a subset of the output.
func EstimateOutput(msg Message) (output, reasoning int) {
	for _, part := range msg.Parts {
		switch p := part.(type) {
		case TextPart:
			output += EstimateText(p.Text)
		case ReasoningPart:
			n := EstimateText(p.Text)
			output += n
			reasoning += n
		case ToolCallPart:
			output += EstimateCall(p)
		}
	}
	return Widen(output), Widen(reasoning)
}
