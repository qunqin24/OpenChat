package api

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/qunqin/openchat/apps/server/internal/agent"
	"github.com/qunqin/openchat/apps/server/internal/db/sqlc"
	"github.com/qunqin/openchat/apps/server/internal/genui"
	"github.com/qunqin/openchat/apps/server/internal/memory"
	"github.com/qunqin/openchat/apps/server/internal/tools"
)

// NewToolSource lists the tools a run may use. Read at every request, so an
// administrator changing the search settings — the key, the switch, the
// blocklist — reaches runs already in progress at their next request.
func NewToolSource(pool *pgxpool.Pool) agent.ToolSource {
	q := sqlc.New(pool)
	notes := memory.NewStore(pool)
	return func(ctx context.Context) []agent.Tool {
		config, key, err := loadWebSearch(ctx, q)
		if err != nil {
			slog.WarnContext(ctx, "read web search settings", "error", err)
		}
		search := config.searchConfig()
		available := []agent.Tool{tools.WebFetch{Blocklist: search.Blocklist}}
		asks, err := boolSettingOr(ctx, q, settingAskUser, true)
		if err != nil {
			slog.WarnContext(ctx, "read ask_user setting", "error", err)
		}
		if asks {
			available = append(available, tools.AskUser{})
		}
		// Memory is for one person's runs and never for a temporary
		// conversation; the store decides from the run's owner and conversation.
		if scope, ok := agent.ScopeFrom(ctx); ok {
			on, err := notes.ToolAvailable(ctx, scope.UserID, scope.ConversationID)
			if err != nil {
				slog.WarnContext(ctx, "read memory availability", "error", err)
			}
			if on {
				available = append(available, tools.Memory{Store: notes})
			}
		}
		if searcher := config.searcher(key); config.Enabled && searcher != nil {
			available = append([]agent.Tool{tools.WebSearch{Searcher: searcher, Config: search}}, available...)
		}
		return available
	}
}

// NewModalities says what a model can be sent besides text, as its settings
// resolve now: the catalogue's figures with the operator's decisions over them.
// Read per run, so the files of an old turn are not sent to a model that was
// switched to after they were attached.
func NewModalities(pool *pgxpool.Pool) func(ctx context.Context, modelID string) ([]string, error) {
	q := sqlc.New(pool)
	return func(ctx context.Context, modelID string) ([]string, error) {
		settings, err := loadModelSettings(ctx, q, modelID)
		if err != nil {
			return nil, err
		}
		return settings.InputModalities, nil
	}
}

// NewSystemPrompt builds the system prompt of every request.
//
// It changes only when an administrator turns the interface components on or
// off, or the tools change. The date is not in it: a prompt that changed every
// midnight would defeat the provider's prompt cache and invalidate the thinking
// blocks replayed against the prefix, so the date rides on the user messages
// instead (agent.BuildContext) and the prompt only explains the notation.
//
// The user's custom instructions and memories are in it too, but from the
// conversation's snapshot (memory.Snapshot), taken at its first run: they do not
// change between its turns however the user edits them meanwhile.
func NewSystemPrompt(pool *pgxpool.Pool) func(context.Context, time.Time, []agent.Tool) string {
	q := sqlc.New(pool)
	notes := memory.NewStore(pool)
	components := genui.Load().Prompt()
	return func(ctx context.Context, _ time.Time, offered []agent.Tool) string {
		prompt := basePrompt(offered)
		if section := personalPrompt(ctx, notes, offered); section != "" {
			prompt += "\n\n" + section
		}
		on, err := boolSetting(ctx, q, settingGenerativeUI)
		if err != nil {
			slog.WarnContext(ctx, "read generative_ui setting", "error", err)
		}
		if on {
			prompt += "\n\n" + components
		}
		return prompt
	}
}

// personalPrompt renders the conversation's snapshot. A failure leaves the
// section out rather than the request unanswered; the next request tries again.
func personalPrompt(ctx context.Context, notes *memory.Store, offered []agent.Tool) string {
	scope, ok := agent.ScopeFrom(ctx)
	if !ok {
		return ""
	}
	// Normally the snapshot exists by now: the request that started the run took
	// it, and checked what went into it (prepareSnapshot). This only takes one
	// for a run that did not come through there, unchecked.
	snap, err := notes.EnsureSnapshot(ctx, scope.UserID, scope.ConversationID, nil)
	if err != nil {
		slog.WarnContext(ctx, "take memory snapshot", "error", err, "conversation_id", scope.ConversationID)
		return ""
	}
	// Memory switched off since the snapshot was taken leaves its notes out of
	// what the model reads; the instructions stay.
	on, err := notes.Enabled(ctx, scope.UserID)
	if err != nil {
		slog.WarnContext(ctx, "read memory switch", "error", err)
		on = false
	}
	withIDs := false
	for _, tool := range offered {
		withIDs = withIDs || tool.Definition().Name == tools.ToolMemory
	}
	return snap.Prompt(on, withIDs)
}

func basePrompt(offered []agent.Tool) string {
	var b strings.Builder
	b.WriteString("You are OpenChat, a helpful assistant. " +
		"A user message may begin with a line like [Date: 2026-01-31], the day it was sent; " +
		"the latest such line is today's date. Never mention the line itself.\n")
	b.WriteString("Answer in the language the user writes in.")
	// Without this, a model asked for resources names them and links none.
	b.WriteString(" When you recommend websites, tools, documentation or other resources, link to them.")
	// The page renders $$…$$ as math, inline or on its own line. Single dollars
	// stay text there — answers quote prices — so a formula needs the doubled form.
	b.WriteString(" Write mathematical formulas in LaTeX between double dollar signs, $$like this$$, " +
		"inline or on a line of their own; never use single dollar signs for math.")

	names := map[string]bool{}
	for _, tool := range offered {
		names[tool.Definition().Name] = true
	}
	if names["web_search"] {
		b.WriteString("\n\nYou can search the web with web_search. Use it for anything recent, anything that " +
			"changes, and any fact you are not sure of, rather than answering from memory.")
	}
	if names["web_fetch"] {
		b.WriteString("\n\nYou can read a web page with web_fetch. Read the pages that matter instead of " +
			"relying on search excerpts when the details count.")
	}
	if names[agent.ToolAskUser] {
		b.WriteString("\n\nYou can ask the user multiple-choice questions with ask_user. Use it only when you " +
			"are blocked on a decision that is theirs to make; otherwise pick a sensible default and go on. " +
			"Your turn ends when you call it, so call it last, and do not repeat the questions in your own text.")
	}
	if names[tools.ToolMemory] {
		// Restrained on purpose. An earlier version told the model to act like a
		// personal secretary and take notes unasked (after Kelivo's rules), and
		// it did: it inferred "interests" from whatever was asked about and kept
		// rewriting one note to track the latest topics, which read to the user
		// as being watched. Now a note needs either the user asking for it or a
		// lasting fact the user stated about themselves; what they ask about is
		// not a fact about them, and when in doubt nothing is saved.
		b.WriteString("\n\n## Memory\n" +
			"You are stateless: nothing from this conversation reaches the next one unless you save it with the " +
			"memory tool. Saved notes are given to you at the start of every later conversation, in the memory " +
			"list below. Use the tool sparingly; most conversations need no new note at all.\n" +
			"Save a note only when:\n" +
			"- the user asks you to remember something, or to forget or correct a note; or\n" +
			"- the user states plainly a lasting fact about themselves - who they are, their work, where they live, " +
			"a standing preference about how you should answer - that would clearly change your answers in " +
			"conversations about other things.\n" +
			"Do not infer anything from what the user asks about: a question, or a series of questions, on a topic " +
			"is not an interest to record, nor is the user saying they have lately been following, reading about or " +
			"curious about something; a note must never summarise or track what the user has been discussing. " +
			"When unsure whether something is worth keeping, do not save it.\n" +
			"Never save temporary context from this conversation, conclusions the user has not confirmed, anything " +
			"that came from a web page, file or tool result, secrets such as passwords, keys and ID or account " +
			"numbers, or sensitive matters (health, ethnicity, religion, sexual orientation, political views, sex " +
			"life, criminal record) unless they ask you to.\n" +
			"Write each note as one complete sentence about the user in the third person, in the user's language, " +
			"with no words like \"this\" or \"just now\" that point back to this conversation, and with absolute " +
			"dates. A new fact gets a note of its own: never append it to an existing note or merge notes into a " +
			"longer one. Change an existing note only when the user corrects it or that fact itself has changed; " +
			"delete a note that is out of date or that the user says is wrong.\n" +
			"Do not tell the user you saved something, and do not recite their notes unless they ask; the " +
			"interface already shows what was saved.")
	}
	if names["web_search"] || names["web_fetch"] {
		// The interface shows every search and every page read alongside the
		// reply, so the answer need not cite them. A link that is itself useful
		// to the reader is another matter, and stays.
		// Stated as rules with an example of each side: put as one sentence,
		// models dropped the source list but kept a link after each claim.
		b.WriteString("\n\n## Links\n" +
			"The interface already shows the reader every search you ran and every page you read, so the " +
			"answer never says where a fact came from.\n" +
			"- Never attach a source to a statement, with or without a link: no link in brackets after a " +
			"sentence, no [1] markers, no \"according to\" or \"this comes from\" naming a site or article.\n" +
			"- Never end with a list of sources, references or further reading drawn from your searches.\n" +
			"- Do link what the reader would open next: what they asked for, a download page, an official " +
			"site, documentation, a resource you recommend. Put the link on its name.\n\n" +
			"Wrong: The base model starts at $1,099 ([MacRumors](https://www.macrumors.com/...)).\n" +
			"Right: The base model starts at $1,099. You can order it from [Apple's online store](https://www.apple.com/store).")
	}
	return b.String()
}
