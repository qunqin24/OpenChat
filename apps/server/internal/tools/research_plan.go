package tools

import (
	"context"
	"encoding/json"
	"strings"
	"unicode/utf8"

	"github.com/qunqin24/openchat/apps/server/internal/agent"
	"github.com/qunqin24/openchat/apps/server/internal/llm"
)

// Limits of one research plan, stated to the model and checked here.
const (
	planMinSteps    = 2
	planMaxSteps    = 8
	planMaxTitle    = 80
	planMaxStepText = 200
)

// ResearchPlan is the `research_plan` tool: before a deep research starts, the
// model sets out what it will look into and the person starts it, asks for
// changes or cancels it (docs/design/deep-research-plan.md §3).
//
// Like ask_user it is answered by the person, not executed: the run stops at
// the call (agent.UserTool) and the answer, from the HTTP layer, is its result
// in the run that follows - the research itself, or a plan revised.
type ResearchPlan struct{}

var _ agent.UserTool = ResearchPlan{}

// Plan is a call's arguments.
type Plan struct {
	Title string   `json:"title"`
	Steps []string `json:"steps"`
}

func (ResearchPlan) Definition() llm.Tool {
	return llm.Tool{
		Name: agent.ToolResearchPlan,
		Description: "Put your research plan to the user before the deep research starts. The user starts it, asks for " +
			"changes, or cancels it; nothing is researched until they start it. Give a short title naming the research " +
			"and the steps you will take, one line each, in the user's language. The call ends your turn: the user's " +
			"decision arrives as its result.",
		Parameters: json.RawMessage(`{
  "type": "object",
  "properties": {
    "title": {"type": "string", "description": "What is being researched, in a few words, e.g. \"Research the ChatGPT plugin architecture\"."},
    "steps": {
      "type": "array",
      "description": "Two to eight steps, one short line each, in the order you will take them.",
      "items": {"type": "string"}
    }
  },
  "required": ["title", "steps"]
}`),
	}
}

// Replayable is moot: the tool never runs.
func (ResearchPlan) Replayable() bool { return false }

// Execute is never reached; the runner stops at the call (agent.UserTool).
func (ResearchPlan) Execute(context.Context, json.RawMessage) (agent.ToolResult, error) {
	return agent.ToolResult{}, agent.Errorf("This tool is answered by the user, not executed.")
}

// Check reports why a call is not a plan that can be put.
func (ResearchPlan) Check(raw json.RawMessage) error {
	_, err := ParsePlan(raw)
	return err
}

// ParsePlan reads and checks a call's arguments. The error is written for the
// model.
func ParsePlan(raw json.RawMessage) (Plan, error) {
	var in Plan
	if err := agent.DecodeArgs(raw, &in); err != nil {
		return in, err
	}
	in.Title = strings.Join(strings.Fields(in.Title), " ")
	if in.Title == "" || utf8.RuneCountInString(in.Title) > planMaxTitle {
		return in, agent.Errorf("invalid arguments: the title must be 1 to %d characters", planMaxTitle)
	}
	steps := in.Steps[:0]
	for _, step := range in.Steps {
		if step = strings.Join(strings.Fields(step), " "); step != "" {
			steps = append(steps, step)
		}
	}
	in.Steps = steps
	if n := len(in.Steps); n < planMinSteps || n > planMaxSteps {
		return in, agent.Errorf("invalid arguments: give between %d and %d steps", planMinSteps, planMaxSteps)
	}
	for i, step := range in.Steps {
		if utf8.RuneCountInString(step) > planMaxStepText {
			return in, agent.Errorf("invalid arguments: step %d is longer than %d characters; keep each to one line", i+1, planMaxStepText)
		}
	}
	return in, nil
}

// PlanDecision is what the person did with a plan.
type PlanDecision string

const (
	PlanStarted   PlanDecision = "started"
	PlanRevised   PlanDecision = "revised"
	PlanCancelled PlanDecision = "cancelled"
)

// Result is the call's result for a decision: what the model reads, and for
// the interface the decision and the person's words.
func (in Plan) Result(decision PlanDecision, feedback string) agent.ToolResult {
	var content string
	switch decision {
	case PlanStarted:
		content = "The user started the research with this plan. Carry it out now, following the research " +
			"instructions in your system prompt."
	case PlanRevised:
		content = "The user wants the plan changed before the research starts. Their words:\n\n" + feedback +
			"\n\nRevise the plan accordingly and call research_plan again with the new plan. Do not start researching."
	default:
		content = "The user cancelled this research. Do not research it; acknowledge in one short sentence."
	}
	details := map[string]any{"decision": decision}
	if feedback != "" {
		details["feedback"] = feedback
	}
	return agent.ToolResult{Content: content, Details: details}
}
