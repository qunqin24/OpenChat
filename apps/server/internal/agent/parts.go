package agent

import (
	"encoding/json"
	"fmt"

	"github.com/qunqin/openchat/apps/server/internal/llm"
)

// EncodeParts is the `messages.parts` column value: llm's canonical encoding,
// the same one every other layer uses.
func EncodeParts(parts []llm.Part) []byte {
	data, err := json.Marshal(llm.Parts(parts))
	if err != nil {
		// The part set is closed and every field encodes.
		panic(fmt.Sprintf("agent: encoding parts: %v", err))
	}
	return data
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
	return llm.Message{Parts: parts}.Text()
}
