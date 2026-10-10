package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/qunqin/openchat/apps/server/internal/agent"
	"github.com/qunqin/openchat/apps/server/internal/llm"
)

// Limits of one ask_user call, stated to the model in the tool's description
// and checked here. Four questions of four options is already a form; past it
// the model is deciding nothing for itself.
const (
	askMaxQuestions = 4
	askMinOptions   = 2
	askMaxOptions   = 4
	// askMaxOther bounds the text a person types in place of the options.
	askMaxOther = 2000
)

// AskUser is the `ask_user` tool: the model puts questions to the person and
// goes on with their answers.
//
// It is the one tool nothing here executes. The run stops where the call is,
// and the person's answer (Answer, from the HTTP layer) becomes its result in a
// new run (agent.UserTool). So there is no waiting connection and nothing to
// time out: the question stays on the transcript until it is answered, skipped
// or the turn is redone.
type AskUser struct{}

var _ agent.UserTool = AskUser{}

// Question is one question of a call. Options are the choices offered; the
// person can always write an answer of their own as well.
type Question struct {
	Question    string   `json:"question"`
	Header      string   `json:"header"`
	Options     []Option `json:"options"`
	MultiSelect bool     `json:"multiSelect"`
}

type Option struct {
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
}

// Questions are a call's arguments.
type Questions struct {
	Questions []Question `json:"questions"`
}

func (AskUser) Definition() llm.Tool {
	return llm.Tool{
		Name: agent.ToolAskUser,
		Description: "Ask the user one to four multiple-choice questions and wait for their answers. " +
			"Use it only when you are blocked on a decision that is genuinely the user's to make and the " +
			"request cannot be answered well without it. Do not ask about anything that has a sensible " +
			"default — pick the default and say so. Do not use it for small talk, to confirm what you can " +
			"see for yourself, or to ask whether you may continue. Each question has two to four options, " +
			"most likely first; the user can always write their own answer as well, so do not add an " +
			"\"other\" option. Call it at most once per response and put every question in that one call: a " +
			"second call in the same response is refused. The call ends your turn: the answers arrive as its result.",
		Parameters: json.RawMessage(`{
  "type": "object",
  "properties": {
    "questions": {
      "type": "array",
      "description": "One to four questions, asked together.",
      "items": {
        "type": "object",
        "properties": {
          "question": {"type": "string", "description": "The full question, ending with a question mark."},
          "header": {"type": "string", "description": "A very short label for the question, a word or two."},
          "options": {
            "type": "array",
            "description": "Two to four choices that exclude each other unless multiSelect is true.",
            "items": {
              "type": "object",
              "properties": {
                "label": {"type": "string", "description": "The choice, in a few words."},
                "description": {"type": "string", "description": "What choosing it means or implies."}
              },
              "required": ["label"]
            }
          },
          "multiSelect": {"type": "boolean", "description": "Whether more than one option can be chosen. Defaults to false."}
        },
        "required": ["question", "header", "options"]
      }
    }
  },
  "required": ["questions"]
}`),
	}
}

// Replayable is moot: the tool never runs.
func (AskUser) Replayable() bool { return false }

// Execute is never reached; the runner stops at the call (agent.UserTool).
func (AskUser) Execute(context.Context, json.RawMessage) (agent.ToolResult, error) {
	return agent.ToolResult{}, agent.Errorf("This tool is answered by the user, not executed.")
}

// Check reports why a call is not a question that can be put.
func (AskUser) Check(raw json.RawMessage) error {
	_, err := ParseQuestions(raw)
	return err
}

// ParseQuestions reads and checks a call's arguments. The error is written for
// the model.
func ParseQuestions(raw json.RawMessage) (Questions, error) {
	var in Questions
	if err := agent.DecodeArgs(raw, &in); err != nil {
		return in, err
	}
	if n := len(in.Questions); n < 1 || n > askMaxQuestions {
		return in, agent.Errorf("invalid arguments: ask between 1 and %d questions in one call", askMaxQuestions)
	}
	for i := range in.Questions {
		q := &in.Questions[i]
		q.Question = strings.TrimSpace(q.Question)
		q.Header = strings.TrimSpace(q.Header)
		if q.Question == "" {
			return in, agent.Errorf("invalid arguments: question %d has no text", i+1)
		}
		if n := len(q.Options); n < askMinOptions || n > askMaxOptions {
			return in, agent.Errorf("invalid arguments: question %d needs between %d and %d options", i+1, askMinOptions, askMaxOptions)
		}
		seen := map[string]bool{}
		for j := range q.Options {
			o := &q.Options[j]
			o.Label = strings.TrimSpace(o.Label)
			o.Description = strings.TrimSpace(o.Description)
			if o.Label == "" || seen[o.Label] {
				return in, agent.Errorf("invalid arguments: the options of question %d need distinct, non-empty labels", i+1)
			}
			seen[o.Label] = true
		}
	}
	return in, nil
}

// Answer is the person's reply to one question: the options they chose, and
// what they wrote in place of or beside them.
type Answer struct {
	Selected []string `json:"selected"`
	Other    string   `json:"other,omitempty"`
}

// ErrInvalidAnswers is a reply that does not fit the questions.
var ErrInvalidAnswers = errors.New("the answers do not fit the questions")

// Result checks the answers against the questions and writes them as the
// call's result: the text the model reads, and for the interface the same
// answers by position.
func (in Questions) Result(answers []Answer) (agent.ToolResult, error) {
	if len(answers) != len(in.Questions) {
		return agent.ToolResult{}, fmt.Errorf("%w: expected %d answers, got %d", ErrInvalidAnswers, len(in.Questions), len(answers))
	}
	clean := make([]Answer, len(answers))
	var b strings.Builder
	b.WriteString("The user answered your questions.\n")
	for i, q := range in.Questions {
		a := answers[i]
		other := strings.TrimSpace(a.Other)
		if utf8.RuneCountInString(other) > askMaxOther {
			return agent.ToolResult{}, fmt.Errorf("%w: question %d: the written answer is too long", ErrInvalidAnswers, i+1)
		}
		selected := make([]string, 0, len(a.Selected))
		for _, label := range a.Selected {
			if !slices.ContainsFunc(q.Options, func(o Option) bool { return o.Label == label }) {
				return agent.ToolResult{}, fmt.Errorf("%w: question %d has no option %q", ErrInvalidAnswers, i+1, label)
			}
			if !slices.Contains(selected, label) {
				selected = append(selected, label)
			}
		}
		chosen := len(selected)
		if other != "" {
			chosen++
		}
		if chosen == 0 {
			return agent.ToolResult{}, fmt.Errorf("%w: question %d is not answered", ErrInvalidAnswers, i+1)
		}
		if chosen > 1 && !q.MultiSelect {
			return agent.ToolResult{}, fmt.Errorf("%w: question %d takes one answer", ErrInvalidAnswers, i+1)
		}
		clean[i] = Answer{Selected: selected, Other: other}

		fmt.Fprintf(&b, "\n%d. ", i+1)
		if q.Header != "" {
			fmt.Fprintf(&b, "[%s] ", q.Header)
		}
		b.WriteString(q.Question + "\n")
		if len(selected) > 0 {
			b.WriteString("   Chose: " + strings.Join(selected, "; ") + "\n")
		}
		if other != "" {
			b.WriteString("   Wrote: " + other + "\n")
		}
	}
	return agent.ToolResult{
		Content: strings.TrimRight(b.String(), "\n"),
		Details: map[string]any{"answers": clean},
	}, nil
}
