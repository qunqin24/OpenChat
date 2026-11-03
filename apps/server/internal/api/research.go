package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/qunqin24/openchat/apps/server/internal/agent"
	"github.com/qunqin24/openchat/apps/server/internal/auth"
	"github.com/qunqin24/openchat/apps/server/internal/db/sqlc"
	"github.com/qunqin24/openchat/apps/server/internal/httpx"
	"github.com/qunqin24/openchat/apps/server/internal/llm"
	"github.com/qunqin24/openchat/apps/server/internal/skills"
	"github.com/qunqin24/openchat/apps/server/internal/tools"
)

// Deep research (深度研究, docs/design/deep-research-plan.md).
//
// Only the person starts one: they pick 深度研究 in the composer's "+" menu and
// the message begins with the line `<deep-research/>`. Its run - the plan run
// - drafts a plan and calls research_plan, which, like ask_user, the person
// answers. Starting it counts against their monthly allowance and starts the
// research run (`runs.research`), which continues the plan run: the main
// agent coordinates researchers (sub-agents with their own prompt) and writes
// the report into a writing block. Asking for changes starts another plan
// run; cancelling writes the result and starts nothing.
//
// The server enforces only that: the plan is approved first, the allowance,
// and how many researchers one research may launch. The rest is the prompts.

const (
	settingDeepResearch    = "deep_research"
	settingResearchMonthly = "deep_research_monthly"
	defaultResearchMonthly = 10
	maxResearchMonthly     = 10000

	// researchMaxSubagents is how many researchers one research run may
	// launch: a first round of up to six, and a second for gaps.
	researchMaxSubagents = 8

	// researchLine is how a message asks for a deep research: its first line
	// (chat-core/src/skill-mention.ts writes it).
	researchLine = "<deep-research/>"

	maxPlanFeedback = 4000
)

var (
	errResearchOff = httpx.NewError(http.StatusForbidden, "deep_research_off",
		"deep research is not available")
	errResearchLimit = httpx.NewError(http.StatusTooManyRequests, "research_limit",
		"this month's deep researches are used up")
	errPlanFeedback = httpx.NewFieldError("feedback", "invalid_feedback",
		fmt.Sprintf("say what to change, in at most %d characters", maxPlanFeedback))
)

// splitResearch is whether a message asks for a deep research, and the rest
// of it.
func splitResearch(content string) (bool, string) {
	if content == researchLine {
		return true, ""
	}
	if rest, ok := strings.CutPrefix(content, researchLine+"\n"); ok {
		return true, rest
	}
	return false, content
}

// personWords is what a person wrote in a message, without the lines the
// composer puts before it (deep research, picked skills): for whatever reads
// it as their words - a title, a search excerpt - rather than as instructions.
func personWords(content string) string {
	_, rest := splitResearch(content)
	_, rest = skills.SplitMentions(rest)
	return rest
}

func validResearchLimit(v *int) error {
	if v != nil && (*v < 0 || *v > maxResearchMonthly) {
		return httpx.NewError(http.StatusBadRequest, "invalid_research_limit",
			fmt.Sprintf("the monthly limit must be between 0 and %d", maxResearchMonthly))
	}
	return nil
}

// researchOn is whether people may start a deep research now: the switch is
// on and there is a web search to research with.
func researchOn(ctx context.Context, q *sqlc.Queries) (bool, error) {
	on, err := boolSettingOr(ctx, q, settingDeepResearch, true)
	if err != nil || !on {
		return false, err
	}
	config, key, err := loadWebSearch(ctx, q)
	if err != nil {
		return false, err
	}
	return config.Enabled && config.searcher(key) != nil, nil
}

// allowance is how many researches a person may still start this month.
type allowance struct {
	unlimited bool
	left      int
	resetsAt  time.Time
}

// monthStart is the start of the calendar month t is in, in the server's
// time zone.
func monthStart(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, t.Location())
}

func researchAllowance(ctx context.Context, q *sqlc.Queries, user sqlc.User) (allowance, error) {
	start := monthStart(time.Now())
	a := allowance{resetsAt: start.AddDate(0, 1, 0)}
	if user.Role == sqlc.UserRoleAdmin {
		a.unlimited = true
		return a, nil
	}
	limit, err := intSettingOr(ctx, q, settingResearchMonthly, defaultResearchMonthly)
	if err != nil {
		return a, err
	}
	if limit <= 0 {
		a.unlimited = true
		return a, nil
	}
	used, err := q.CountResearchStarts(ctx, sqlc.CountResearchStartsParams{
		UserID: user.ID, Since: pgtype.Timestamptz{Time: start, Valid: true},
	})
	if err != nil {
		return a, err
	}
	a.left = max(0, limit-int(used))
	return a, nil
}

func (a allowance) contract() DeepResearchAllowance {
	return DeepResearchAllowance{Unlimited: a.unlimited, Left: a.left, ResetsAt: a.resetsAt}
}

// chooseResearch adjusts a run on a person's input that asks for a deep
// research - sending it, editing it, regenerating its answer: that run drafts
// the plan. It is refused when deep research is off or the month's allowance
// is used up, before a plan is drafted for nothing. Web access is on for it
// whatever the conversation's switch says, which is left as it was.
func (s *Server) chooseResearch(ctx context.Context, user sqlc.User, content string, c *runChoice) error {
	asks, words := splitResearch(content)
	if !asks {
		return nil
	}
	if strings.TrimSpace(personWords(words)) == "" {
		return httpx.NewError(http.StatusBadRequest, "empty_content", "say what to research")
	}
	on, err := researchOn(ctx, s.q)
	if err != nil {
		return err
	}
	if !on {
		return errResearchOff
	}
	a, err := researchAllowance(ctx, s.q, user)
	if err != nil {
		return err
	}
	if !a.unlimited && a.left == 0 {
		return errResearchLimit
	}
	c.forceWeb()
	return nil
}

// forceWeb gives the run web access without the conversation remembering it.
func (c *runChoice) forceWeb() {
	c.webSearch = true
	c.webForced = true
}

// ---------------------------------------------------------------- phases

// researchPhase is what part of a deep research a run is.
type researchPhase int

const (
	phaseNone researchPhase = iota
	// phasePlan: the run drafts (or revises) the plan.
	phasePlan
	// phaseResearch: the run carries out the plan the person started.
	phaseResearch
	// phaseResearcher: a sub-agent's run, launched by a research run.
	phaseResearcher
)

type researchPhaseKey struct{}

// researchPhaseOf is the phase of the run ctx belongs to, worked out once per
// run (agent.RunState) since the tools and the prompt ask at every request.
func researchPhaseOf(ctx context.Context, q *sqlc.Queries) researchPhase {
	scope, ok := agent.ScopeFrom(ctx)
	if !ok {
		return phaseNone
	}
	if v, ok := scope.State.Load(researchPhaseKey{}); ok {
		return v.(researchPhase)
	}
	phase := phaseNone
	switch {
	case scope.Subagent():
		if research, err := q.SubagentLaunchedByResearch(ctx, scope.ConversationID); err == nil && research {
			phase = phaseResearcher
		}
	case scope.Research:
		phase = phaseResearch
	default:
		if run, err := q.GetRun(ctx, scope.RunID); err == nil {
			if input, err := q.GetMessage(ctx, run.InputMessageID); err == nil {
				if asks, _ := splitResearch(input.Content); asks {
					phase = phasePlan
				}
			}
		}
	}
	scope.State.Store(researchPhaseKey{}, phase)
	return phase
}

// ---------------------------------------------------------------- answering

// AnswerResearchPlan records what the person did with a research plan: start
// it, have it revised, or cancel it (see the note at the top of the file, and
// ask_user.go, whose rules of what is pending this follows).
func (s *Server) AnswerResearchPlan(w http.ResponseWriter, r *http.Request, conversationId ConversationId, callId string) {
	ctx := r.Context()
	user := auth.MustUser(ctx)

	var body AnswerResearchPlanRequest
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	conv, err := s.q.GetConversation(ctx, sqlc.GetConversationParams{ID: conversationId, UserID: &user.ID})
	if err != nil {
		httpx.Fail(w, r, dbErr(err))
		return
	}
	requestID := trimmed(body.RequestId)
	if found, ok := s.existingPlanAnswer(ctx, conversationId, requestID); ok {
		httpx.JSON(w, http.StatusAccepted, found)
		return
	}
	var decision tools.PlanDecision
	switch body.Decision {
	case Start:
		decision = tools.PlanStarted
	case Revise:
		decision = tools.PlanRevised
	case Cancel:
		decision = tools.PlanCancelled
	default:
		httpx.Fail(w, r, httpx.NewFieldError("decision", "invalid_decision", "decision must be start, revise or cancel"))
		return
	}
	feedback := ""
	if body.Feedback != nil {
		feedback = strings.TrimSpace(*body.Feedback)
	}
	if decision == tools.PlanRevised && (feedback == "" || utf8.RuneCountInString(feedback) > maxPlanFeedback) {
		httpx.Fail(w, r, errPlanFeedback)
		return
	}
	if decision != tools.PlanRevised {
		feedback = ""
	}

	if err := requireRevised(ctx, s.q, conversationId); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	asked, err := pendingPlan(ctx, s.q, conversationId, callId)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	result := planResult(asked.plan, callId, decision, feedback)

	if decision == tools.PlanCancelled {
		s.cancelPlan(w, r, conversationId, callId, asked, result)
		return
	}

	if on, err := researchOn(ctx, s.q); err != nil {
		httpx.Fail(w, r, err)
		return
	} else if !on {
		httpx.Fail(w, r, errResearchOff)
		return
	}
	choice, err := s.chooseRun(ctx, conv, body.Model, body.ReasoningEffort, nil)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	choice.forceWeb()
	if decision == tools.PlanRevised {
		if err := s.moderate(ctx, s.accountSubject(ctx, user, conversationId, ModerationSourceAnswer),
			moderationInput{text: feedback}); err != nil {
			httpx.Fail(w, r, err)
			return
		}
	} else {
		choice.research = true
	}
	if err := s.prepareSnapshot(ctx, user, conv); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	head := asked.run.ID
	if asked.run.Continues != nil {
		head = *asked.run.Continues
	}
	choice.continues = &head

	var (
		run     sqlc.Run
		written sqlc.Message
	)
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		if _, err := lockLive(ctx, q, conversationId); err != nil {
			return err
		}
		if _, err := q.LockRun(ctx, asked.run.ID); errors.Is(err, pgx.ErrNoRows) {
			return errNotPending
		} else if err != nil {
			return err
		}
		if _, err := pendingPlan(ctx, q, conversationId, callId); err != nil {
			return err
		}
		if err := requireRevised(ctx, q, conversationId); err != nil {
			return err
		}
		if choice.research {
			if err := admitResearch(ctx, q, user); err != nil {
				return err
			}
		}
		var err error
		run, written, err = startAnswerRun(ctx, q, conv, user.ID, asked.run.InputMessageID, requestID, choice, result)
		if err != nil {
			return err
		}
		if choice.research {
			return q.RecordResearchStart(ctx, sqlc.RecordResearchStartParams{UserID: user.ID, RunID: &run.ID})
		}
		return nil
	})
	if err != nil {
		s.failRun(w, r, conversationId, requestID, err)
		return
	}
	input, err := s.q.GetMessage(ctx, run.InputMessageID)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	s.startRun(conversationId, run, input, agent.MessageAppended{Message: written})

	page, err := s.messagePage(ctx, []sqlc.Message{input})
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	started := toRun(run)
	httpx.JSON(w, http.StatusAccepted, AnswerResearchPlanResponse{Message: page.Items[0], Result: toMessage(written, nil), Run: &started})
}

// admitResearch counts one more research against the person's month, inside
// the transaction that starts it, under a lock held until it commits: two
// starts with one research left cannot both pass.
func admitResearch(ctx context.Context, q *sqlc.Queries, user sqlc.User) error {
	if err := q.LockResearchStarts(ctx, user.ID.String()); err != nil {
		return err
	}
	a, err := researchAllowance(ctx, q, user)
	if err != nil {
		return err
	}
	if !a.unlimited && a.left == 0 {
		return errResearchLimit
	}
	return nil
}

// cancelPlan writes a cancelled plan's result and starts nothing. The row
// belongs to the run that asked, which stays awaiting input as a skipped
// question's does; the result is what makes the plan no longer pending.
func (s *Server) cancelPlan(w http.ResponseWriter, r *http.Request, conversationID uuid.UUID, callID string, asked planAsked, result llm.ToolResultPart) {
	ctx := r.Context()
	var written sqlc.Message
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		if _, err := lockLive(ctx, q, conversationID); err != nil {
			return err
		}
		if _, err := q.LockRun(ctx, asked.run.ID); errors.Is(err, pgx.ErrNoRows) {
			return errNotPending
		} else if err != nil {
			return err
		}
		if err := requireIdle(ctx, q, conversationID); err != nil {
			return err
		}
		if _, err := pendingPlan(ctx, q, conversationID, callID); err != nil {
			return err
		}
		parts := []llm.Part{result}
		var err error
		written, err = q.AppendEntry(ctx, sqlc.AppendEntryParams{
			ConversationID: conversationID,
			Role:           sqlc.MessageRoleTool,
			Content:        agent.PlainText(parts),
			Parts:          agent.EncodeParts(parts),
			Status:         sqlc.MessageStatusComplete,
			RunID:          &asked.run.ID,
		})
		return err
	})
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	// Other pages showing the conversation fetch its newest rows again.
	s.runner.Hub().Publish(conversationID, agent.Batch{Events: []agent.Event{agent.TranscriptChanged{}}})

	input, err := s.q.GetMessage(ctx, asked.run.InputMessageID)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	page, err := s.messagePage(ctx, []sqlc.Message{input})
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusAccepted, AnswerResearchPlanResponse{Message: page.Items[0], Result: toMessage(written, nil)})
}

func planResult(plan tools.Plan, callID string, decision tools.PlanDecision, feedback string) llm.ToolResultPart {
	out := plan.Result(decision, feedback)
	result := llm.ToolResultPart{CallID: callID, Name: agent.ToolResearchPlan, Content: out.Content, Status: llm.ToolSucceeded}
	if details, err := json.Marshal(out.Details); err == nil {
		result.Details = details
	}
	return result
}

// planAsked is a research plan waiting for the person.
type planAsked struct {
	run  sqlc.Run
	plan tools.Plan
}

// pendingPlan finds the research_plan call with this id if it is the one the
// conversation is waiting on, by the rules pendingQuestion follows.
func pendingPlan(ctx context.Context, q *sqlc.Queries, conversationID uuid.UUID, callID string) (planAsked, error) {
	var out planAsked
	call, run, err := pendingCall(ctx, q, conversationID, callID, agent.ToolResearchPlan)
	if err != nil {
		return out, err
	}
	out.run = run
	if out.plan, err = tools.ParsePlan(call.Arguments); err != nil {
		return out, errNotPending
	}
	return out, nil
}

// existingPlanAnswer is the answer a retried request already started.
func (s *Server) existingPlanAnswer(ctx context.Context, conversationID uuid.UUID, requestID string) (AnswerResearchPlanResponse, bool) {
	found, ok := s.existingAnswer(ctx, conversationID, requestID)
	if !ok {
		return AnswerResearchPlanResponse{}, false
	}
	return AnswerResearchPlanResponse{Message: found.Message, Result: found.Result, Run: &found.Run}, true
}

// ---------------------------------------------------------------- prompts

// researchPlanGuide is the plan run's part of the system prompt.
const researchPlanGuide = "## Deep research: the plan\n" +
	"The user's latest message begins with the line <deep-research/>: they turned on deep research for it. A deep " +
	"research sends several researchers out in parallel and ends in a written report, so it starts only once the " +
	"user approves a plan. In this turn, draft that plan; do not research yet.\n\n" +
	"You may make up to three quick searches first, only to understand the subject well enough to plan - what a " +
	"term refers to, what the main options are. If the request is ambiguous in a way that would change the " +
	"research's direction (an unclear term, a missing region or period that changes the answer) and the " +
	"conversation does not settle it, ask once with ask_user; otherwise take the most sensible reading.\n\n" +
	"Then call research_plan with a short title and three to six steps, in the user's language: what you will look " +
	"into, in order, each a line a reader understands on its own; the last step is writing the report. Build on " +
	"what the conversation has already established instead of planning to research it again. Before the call, " +
	"write at most two sentences on how you read the request, and do not list the steps in your text.\n\n" +
	"If the result says the user wants changes, revise the plan and call research_plan again. If they cancelled, " +
	"acknowledge it in one short sentence."

// researchGuide is the research run's part of the system prompt: the main
// agent coordinates and writes. The method follows Anthropic's research
// prompts (github.com/anthropics/claude-cookbooks, patterns/agents/prompts,
// MIT) and their deep-research skill's division of labour.
const researchGuide = "## Deep research\n" +
	"The user started a deep research with the plan you proposed (the research_plan call above). You coordinate " +
	"it: researchers find and read the sources, and you write the report from what they bring back. Do not search " +
	"or read pages much yourself.\n\n" +
	"1. Split the research into questions that do not overlap and together cover the plan. Size it to the subject: " +
	"one researcher for a single fact to establish; three for a focused topic; four for several facets or criteria; " +
	"five for independent entities or regions; up to six for a large set of comparable items, split into groups. " +
	"When unsure, three.\n" +
	"2. Launch all the researchers in one reply with the subagent tool, so they work in parallel. Each sees nothing " +
	"of this conversation, so brief each fully: the question; the goal and why it matters to the user; the key " +
	"sub-questions; sources to prefer (official documentation, papers, filings, original data, the people involved, " +
	"over aggregators and SEO pages); constraints from the request (period, region); and the languages to search " +
	"in - English for technical and international subjects, Chinese for Chinese ones, both when useful.\n" +
	"3. Read their reports and look at the gaps they name. Send a second round only for a gap that would leave a " +
	"central question unanswered, and at most once: another round costs minutes and runs into search rate limits, " +
	"and if you keep going the user gets no report at all. After that, write with what you have.\n" +
	"4. Write the report in a writing block with the document tool, in the user's language. It is the product of " +
	"the whole research, so make it complete: use what every report brought back - the figures, the comparisons, " +
	"the disagreements - rather than a summary of the summaries. A full report runs to about 6,000-12,000 Chinese " +
	"characters or 3,000-6,000 English words; shorter only when the reports truly hold less. Write it in parts: " +
	"create the document with the title, the opening and the first section, then add each further section with an " +
	"update that gives it as append, so no single call has to hold the whole report.\n" +
	"   Shape: a title of about six words that states the finding; an opening paragraph that answers the question, " +
	"so a reader could stop there; three to six headers that each state a conclusion (\"Migratory birds forage 370 " +
	"more hours a year\", not \"Effects of climate\"); narrative paragraphs rather than bullet lists, a table where " +
	"several things are compared on the same points, key figures in bold with their unit and date. Cite as you " +
	"write, never afterwards: every factual sentence carries its source inline as [title](URL), taken from the " +
	"researchers' reports - write a sentence and its link together. Leave out any claim no report gives a source " +
	"for: five sourced conclusions beat twenty half-sourced ones. Say plainly where sources disagree or a question " +
	"stayed open. End with what is new or what matters, not a recap, and a list of the sources cited.\n" +
	"5. After the writing block, reply with three to five sentences on what the research found. Do not paste the " +
	"report into your reply."

// researcherGuide is added to a researcher's sub-agent prompt. Its search
// results are short excerpts (NewToolSource), so what it knows it has read.
const researcherGuide = "## You are a researcher in a deep research\n" +
	"The main assistant split a research among several researchers working in parallel; your task is one part of " +
	"it. Your findings must come from pages you have read, not from search results: a result here is a title and a " +
	"few lines, enough to choose what to open and never enough to rely on.\n\n" +
	"Work in two moves. First, a few searches - three to six - to find where the answers are: short queries, under " +
	"five words, broad first and narrower after, never the same query twice. Then read: open the five to eight most " +
	"promising pages with web_fetch, primary sources first (official documentation and announcements, papers, " +
	"filings, original data, the people involved) over pages that repeat them. Read each far enough to get what the " +
	"task needs - use find to jump to the passages that matter in a long page, offset to read on. When a page cannot " +
	"be read, open the next candidate instead of searching again. Search again only for a question the pages you " +
	"read left open. Stop when the key questions are answered, or after about twenty tool calls.\n\n" +
	"Note when each source was published, and confirm important facts in two independent sources where you can.\n\n" +
	"Write your report under each key question of the task, in four parts:\n" +
	"- Takeaway: one or two sentences.\n" +
	"- Findings: each fact with the detail that makes it useful - figures with units and dates, names, what exactly " +
	"was measured - followed by its source as [title](URL) of a page you read; when a source contradicts a fact, say so.\n" +
	"- Inferences: what you conclude beyond the sources, marked as yours.\n" +
	"- Gaps: what you could not find or confirm. A fact without a source you read goes here, never under Findings. " +
	"Never invent a source.\n" +
	"End with a list of the pages you read, one [title](URL) a line."
