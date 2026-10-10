package api

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/qunqin/openchat/apps/server/internal/agent"
	"github.com/qunqin/openchat/apps/server/internal/db/sqlc"
	"github.com/qunqin/openchat/apps/server/internal/genui"
	"github.com/qunqin/openchat/apps/server/internal/tools"
)

// NewToolSource lists the tools a run may use. Read at every request, so an
// administrator changing the search settings — the key, the switch, the
// blocklist — reaches runs already in progress at their next request.
func NewToolSource(pool *pgxpool.Pool) agent.ToolSource {
	q := sqlc.New(pool)
	return func(ctx context.Context) []agent.Tool {
		config, key, err := loadWebSearch(ctx, q)
		if err != nil {
			slog.WarnContext(ctx, "read web search settings", "error", err)
		}
		search := config.searchConfig()
		available := []agent.Tool{tools.WebFetch{Blocklist: search.Blocklist}}
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
// It changes once a day, with the date, and when an administrator turns the
// interface components on or off. Anything that changed more often — the
// time, say — would defeat the provider's prompt cache on every request.
func NewSystemPrompt(pool *pgxpool.Pool) func(context.Context, time.Time, []agent.Tool) string {
	q := sqlc.New(pool)
	components := genui.Load().Prompt()
	return func(ctx context.Context, now time.Time, offered []agent.Tool) string {
		prompt := basePrompt(now, offered)
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

func basePrompt(now time.Time, offered []agent.Tool) string {
	var b strings.Builder
	fmt.Fprintf(&b, "You are OpenChat, a helpful assistant. Today's date is %s.\n", now.UTC().Format("2006-01-02"))
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
