// Package wikipedia is the built-in Wikipedia plugin's service, run inside
// OpenChat (plugins.LocalScheme): searching and reading articles of any
// language edition through the MediaWiki API, which needs no key.
// Wikimedia asks clients to name themselves (localkit.UserAgent) and to make
// requests one at a time.
package wikipedia

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/qunqin24/openchat/apps/server/internal/plugins/localkit"
	"github.com/qunqin24/openchat/apps/server/internal/tools"
)

var client = &localkit.Client{Gap: 100 * time.Millisecond}

// NewServer is the service, with its tools.
func NewServer(o localkit.Options) *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{Name: "wikipedia", Title: "Wikipedia", Version: o.Version}, nil)
	mcp.AddTool(server, &mcp.Tool{
		Name: "search",
		Description: "Search Wikipedia articles in one language edition. Returns titles with a snippet of where the " +
			"words matched. Search in the language the article is most likely written in; Chinese results are in " +
			"simplified characters unless language asks for zh-tw or zh-hk.",
		Annotations: localkit.ReadOnly,
	}, search)
	mcp.AddTool(server, &mcp.Tool{
		Name: "read_article",
		Description: "An article's text by its exact title (as search gives it), with == section headings ==. Set " +
			"intro for only the lead section; otherwise the whole article comes in parts of about 20,000 characters - " +
			"read on with next_offset. Also gives the article's titles in other languages.",
		Annotations: localkit.ReadOnly,
	}, readArticle)
	return server
}

// edition is the wiki and the script variant a language asks for: zh is
// mainland simplified Chinese, zh-tw and zh-hk the traditional ones.
func edition(language string) (wiki, variant string, err error) {
	language = strings.ToLower(strings.TrimSpace(language))
	if language == "" {
		language = "en"
	}
	switch language {
	case "zh", "zh-cn", "zh-hans":
		return "zh", "zh-cn", nil
	case "zh-tw", "zh-hant":
		return "zh", "zh-tw", nil
	case "zh-hk":
		return "zh", "zh-hk", nil
	}
	if !wikiCode.MatchString(language) {
		return "", "", errors.New("language is a Wikipedia language code such as en, zh, ja, de")
	}
	return language, "", nil
}

var wikiCode = regexp.MustCompile(`^[a-z]{2,10}(-[a-z]{2,10})?$`)

func api(wiki string, params url.Values) string {
	params.Set("format", "json")
	params.Set("formatversion", "2")
	return "https://" + wiki + ".wikipedia.org/w/api.php?" + params.Encode()
}

func articleURL(wiki, title string) string {
	return "https://" + wiki + ".wikipedia.org/wiki/" + url.PathEscape(strings.ReplaceAll(title, " ", "_"))
}

// ---------------------------------------------------------------- search

type SearchInput struct {
	Query      string `json:"query" jsonschema:"words to look for"`
	Language   string `json:"language,omitempty" jsonschema:"the edition: en (the default), zh, zh-tw, ja, de, fr... "`
	MaxResults int    `json:"max_results,omitempty" jsonschema:"1 to 20; 8 when absent"`
}

type Hit struct {
	Title   string `json:"title"`
	Snippet string `json:"snippet"`
	Words   int    `json:"words"`
	URL     string `json:"url"`
}

type SearchOutput struct {
	Total int   `json:"total"`
	Hits  []Hit `json:"hits"`
	// Suggestion is the search Wikipedia thinks was meant, when it found little.
	Suggestion string `json:"suggestion,omitempty"`
}

func search(ctx context.Context, _ *mcp.CallToolRequest, in SearchInput) (*mcp.CallToolResult, SearchOutput, error) {
	q := strings.TrimSpace(in.Query)
	if q == "" {
		return nil, SearchOutput{}, errors.New("query is empty")
	}
	wiki, variant, err := edition(in.Language)
	if err != nil {
		return nil, SearchOutput{}, err
	}
	n := in.MaxResults
	if n <= 0 {
		n = 8
	}
	params := url.Values{"action": {"query"}, "list": {"search"}, "srsearch": {q}, "srlimit": {fmt.Sprint(min(n, 20))},
		"srprop": {"snippet|wordcount"}, "srinfo": {"totalhits|suggestion"}}
	if variant != "" {
		params.Set("variant", variant)
	}
	var r struct {
		Query struct {
			Info struct {
				Total      int    `json:"totalhits"`
				Suggestion string `json:"suggestion"`
			} `json:"searchinfo"`
			Search []struct {
				Title   string `json:"title"`
				Snippet string `json:"snippet"`
				Words   int    `json:"wordcount"`
			} `json:"search"`
		} `json:"query"`
	}
	if err := client.JSON(ctx, api(wiki, params), nil, &r); err != nil {
		return nil, SearchOutput{}, err
	}
	out := SearchOutput{Total: r.Query.Info.Total, Suggestion: r.Query.Info.Suggestion, Hits: []Hit{}}
	for _, h := range r.Query.Search {
		out.Hits = append(out.Hits, Hit{Title: h.Title, Snippet: localkit.Collapse(localkit.PlainText(h.Snippet)), Words: h.Words, URL: articleURL(wiki, h.Title)})
	}
	return nil, out, nil
}

// ---------------------------------------------------------------- read_article

type ReadInput struct {
	Title    string `json:"title" jsonschema:"the article's title, as search gives it"`
	Language string `json:"language,omitempty" jsonschema:"the edition: en (the default), zh, zh-tw, ja..."`
	Intro    bool   `json:"intro,omitempty" jsonschema:"only the lead section, before the first heading"`
	Offset   int    `json:"offset,omitempty" jsonschema:"where to read on from: a part's next_offset"`
}

type ReadOutput struct {
	Title      string            `json:"title"`
	URL        string            `json:"url"`
	Text       string            `json:"text"`
	Offset     int               `json:"offset"`
	Length     int               `json:"length" jsonschema:"the whole text's length in characters"`
	NextOffset *int              `json:"next_offset,omitempty" jsonschema:"where the next part starts; absent at the end"`
	Languages  map[string]string `json:"other_languages,omitempty" jsonschema:"the article's title in other editions, for the ones most asked about"`
}

// languages of other editions worth naming; all of them would be hundreds.
const otherLanguages = "en|zh|ja|ko|de|fr|es|ru"

func readArticle(ctx context.Context, _ *mcp.CallToolRequest, in ReadInput) (*mcp.CallToolResult, ReadOutput, error) {
	title := strings.TrimSpace(in.Title)
	if title == "" {
		return nil, ReadOutput{}, errors.New("title is empty")
	}
	wiki, variant, err := edition(in.Language)
	if err != nil {
		return nil, ReadOutput{}, err
	}
	params := url.Values{"action": {"query"}, "titles": {title}, "redirects": {"1"},
		"prop": {"extracts|langlinks"}, "explaintext": {"1"}, "exsectionformat": {"wiki"},
		"lllimit": {"max"}, "llprop": {"url"}}
	if in.Intro {
		params.Set("exintro", "1")
	}
	if variant != "" {
		params.Set("variant", variant)
	}
	var r struct {
		Query struct {
			Pages []struct {
				Title     string `json:"title"`
				Missing   bool   `json:"missing"`
				Extract   string `json:"extract"`
				LangLinks []struct {
					Lang  string `json:"lang"`
					Title string `json:"title"`
				} `json:"langlinks"`
			} `json:"pages"`
		} `json:"query"`
	}
	if err := client.JSON(ctx, api(wiki, params), nil, &r); err != nil {
		return nil, ReadOutput{}, err
	}
	if len(r.Query.Pages) == 0 || r.Query.Pages[0].Missing {
		return nil, ReadOutput{}, fmt.Errorf("%s Wikipedia has no article titled %q; search for it first", wiki, title)
	}
	page := r.Query.Pages[0]
	text := strings.TrimSpace(page.Extract)
	if text == "" {
		return nil, ReadOutput{}, fmt.Errorf("%q has no text to read (a disambiguation or empty page?)", page.Title)
	}
	part, next := tools.TextPart(text, in.Offset)
	out := ReadOutput{Title: page.Title, URL: articleURL(wiki, page.Title), Text: part, Offset: max(0, in.Offset), Length: utf8.RuneCountInString(text)}
	if next >= 0 {
		out.NextOffset = &next
	}
	for _, l := range page.LangLinks {
		if strings.Contains("|"+otherLanguages+"|", "|"+l.Lang+"|") && l.Lang != wiki {
			if out.Languages == nil {
				out.Languages = map[string]string{}
			}
			out.Languages[l.Lang] = l.Title
		}
	}
	return nil, out, nil
}
