package agent

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/qunqin24/openchat/apps/server/internal/llm"
)

// EncodeParts is the `messages.parts` column value: llm's canonical encoding,
// the same one every other layer uses, made storable (StorableJSON).
func EncodeParts(parts []llm.Part) []byte {
	data, err := json.Marshal(llm.Parts(parts))
	if err != nil {
		// The part set is closed and every field encodes.
		panic(fmt.Sprintf("agent: encoding parts: %v", err))
	}
	return StorableJSON(data)
}

// StorableJSON is JSON without the NUL character, which a jsonb column refuses
// ("unsupported Unicode escape sequence") - and with it the row, failing the
// run that wrote it. Text from the web carries one now and then: a search
// result read out of a PDF killed a deep research's researcher. Each \u0000
// escape is dropped; an escaped backslash before the letters is kept, since
// escapes are read in pairs. json.Marshal already replaces invalid UTF-8.
func StorableJSON(data []byte) []byte {
	nul := []byte(`\u0000`)
	if !bytes.Contains(data, nul) {
		return data
	}
	out := make([]byte, 0, len(data))
	for i := 0; i < len(data); i++ {
		if data[i] != '\\' || i+1 >= len(data) {
			out = append(out, data[i])
			continue
		}
		if bytes.HasPrefix(data[i:], nul) {
			i += len(nul) - 1
			continue
		}
		out = append(out, data[i], data[i+1])
		i++
	}
	return out
}

// StorableText is text a text column takes: no NUL, valid UTF-8 (see StorableJSON).
func StorableText(s string) string {
	return strings.ToValidUTF8(strings.ReplaceAll(s, "\x00", ""), "\uFFFD")
}

func DecodeParts(data []byte) (llm.Parts, error) {
	if len(data) == 0 {
		return nil, nil
	}
	var parts llm.Parts
	if err := json.Unmarshal(data, &parts); err != nil {
		return nil, err
	}
	return parts, nil
}

// PlainText is the `content` column: the text blocks in order, reasoning left
// out as llm.Message.Text leaves it out.
func PlainText(parts []llm.Part) string {
	return StorableText(llm.Message{Parts: parts}.Text())
}
