// Package hackernews is the built-in Hacker News plugin's service, run
// inside OpenChat (plugins.LocalScheme): searching stories and comments, the
// front page, and a story with its discussion, from the Algolia Hacker News
// API (hn.algolia.com/api), which needs no key.
package hackernews

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
)

const (
	apiURL = "https://hn.algolia.com/api/v1"
	// discussionChars bounds the comments one get_story gives.
	discussionChars = 20_000
	commentChars    = 1_500
)

var client = &localkit.Client{Gap: 100 * time.Millisecond}

// NewServer is the service, with its tools.
func NewServer(o localkit.Options) *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{Name: "hackernews", Title: "Hacker News", Version: o.Version}, nil)
	mcp.AddTool(server, &mcp.Tool{
		Name: "search",
		Description: "Search Hacker News stories (the default) or comments. Sort by relevance or newest; narrow by days " +
			"and by points. Each story has its link, points, comment count and its Hacker News discussion page.",
		Annotations: localkit.ReadOnly,
	}, search)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "front_page",
		Description: "The stories on the Hacker News front page now, as ranked there.",
		Annotations: localkit.ReadOnly,
	}, frontPage)
	mcp.AddTool(server, &mcp.Tool{
		Name: "get_story",
		Description: "A story (or comment) by id with its discussion: the comments in thread order, each with its depth, " +
			"author and points of its own. Long discussions are cut to the first 20,000 characters of comments.",
		Annotations: localkit.ReadOnly,
	}, getStory)
	return server
}

// Story is a story or a comment as the search gives it.
type Story struct {
	ID         string `json:"id"`
	Title      string `json:"title,omitempty"`
	URL        string `json:"url,omitempty" jsonschema:"what the story links to; absent for Ask HN and other text posts"`
	Author     string `json:"author"`
	Points     int    `json:"points,omitempty"`
	Comments   int    `json:"comments,omitempty"`
	Created    string `json:"created" jsonschema:"when it was posted, UTC"`
	Discussion string `json:"discussion" jsonschema:"its page on Hacker News"`
	Text       string `json:"text,omitempty" jsonschema:"a text post's or comment's words, cut"`
	StoryTitle string `json:"story_title,omitempty" jsonschema:"for a comment: the story it is under"`
}

type hit struct {
	ID         string `json:"objectID"`
	Title      string `json:"title"`
	URL        string `json:"url"`
	Author     string `json:"author"`
	Points     *int   `json:"points"`
	Comments   *int   `json:"num_comments"`
	Created    int64  `json:"created_at_i"`
	StoryText  string `json:"story_text"`
	Comment    string `json:"comment_text"`
	StoryTitle string `json:"story_title"`
}

func discussion(id string) string { return "https://news.ycombinator.com/item?id=" + id }

func (h hit) story() Story {
	s := Story{ID: h.ID, Title: h.Title, URL: h.URL, Author: h.Author, Discussion: discussion(h.ID), StoryTitle: h.StoryTitle,
		Created: time.Unix(h.Created, 0).UTC().Format("2006-01-02 15:04")}
	if h.Points != nil {
		s.Points = *h.Points
	}
	if h.Comments != nil {
		s.Comments = *h.Comments
	}
	if text := localkit.PlainText(h.StoryText + h.Comment); text != "" {
		s.Text = localkit.Cut(text, 600)
	}
	return s
}

type SearchInput struct {
	Query      string `json:"query" jsonschema:"words to look for"`
	Type       string `json:"type,omitempty" jsonschema:"story (the default), comment, ask_hn or show_hn"`
	Sort       string `json:"sort,omitempty" jsonschema:"relevance (the default) or newest"`
	Days       int    `json:"days,omitempty" jsonschema:"only what was posted in the last this many days"`
	MinPoints  int    `json:"min_points,omitempty" jsonschema:"only stories with at least this many points"`
	MaxResults int    `json:"max_results,omitempty" jsonschema:"1 to 30; 10 when absent"`
	Page       int    `json:"page,omitempty" jsonschema:"0 is the first"`
}

type StoryList struct {
	Total   int     `json:"total"`
	Page    int     `json:"page"`
	Pages   int     `json:"pages"`
	Stories []Story `json:"stories"`
}

func list(ctx context.Context, endpoint string, params url.Values) (StoryList, error) {
	var r struct {
		Hits  []hit `json:"hits"`
		Total int   `json:"nbHits"`
		Page  int   `json:"page"`
		Pages int   `json:"nbPages"`
	}
	if err := client.JSON(ctx, apiURL+endpoint+"?"+params.Encode(), nil, &r); err != nil {
		return StoryList{}, err
	}
	out := StoryList{Total: r.Total, Page: r.Page, Pages: r.Pages, Stories: []Story{}}
	for _, h := range r.Hits {
		out.Stories = append(out.Stories, h.story())
	}
	return out, nil
}

func search(ctx context.Context, _ *mcp.CallToolRequest, in SearchInput) (*mcp.CallToolResult, StoryList, error) {
	q := strings.TrimSpace(in.Query)
	if q == "" {
		return nil, StoryList{}, errors.New("query is empty")
	}
	tag := "story"
	switch in.Type {
	case "", "story":
	case "comment", "ask_hn", "show_hn":
		tag = in.Type
	default:
		return nil, StoryList{}, errors.New("type is story, comment, ask_hn or show_hn")
	}
	endpoint := "/search"
	switch in.Sort {
	case "", "relevance":
	case "newest":
		endpoint = "/search_by_date"
	default:
		return nil, StoryList{}, errors.New("sort is relevance or newest")
	}
	n := in.MaxResults
	if n <= 0 {
		n = 10
	}
	params := url.Values{"query": {q}, "tags": {tag}, "hitsPerPage": {fmt.Sprint(min(n, 30))}, "page": {fmt.Sprint(max(in.Page, 0))}}
	var numeric []string
	if in.Days > 0 {
		numeric = append(numeric, fmt.Sprintf("created_at_i>%d", time.Now().Add(-time.Duration(in.Days)*24*time.Hour).Unix()))
	}
	if in.MinPoints > 0 {
		numeric = append(numeric, fmt.Sprintf("points>=%d", in.MinPoints))
	}
	if len(numeric) > 0 {
		params.Set("numericFilters", strings.Join(numeric, ","))
	}
	out, err := list(ctx, endpoint, params)
	return nil, out, err
}

type FrontPageInput struct{}

func frontPage(ctx context.Context, _ *mcp.CallToolRequest, _ FrontPageInput) (*mcp.CallToolResult, StoryList, error) {
	out, err := list(ctx, "/search", url.Values{"tags": {"front_page"}, "hitsPerPage": {"30"}})
	return nil, out, err
}

// ---------------------------------------------------------------- get_story

type StoryInput struct {
	ID string `json:"id" jsonschema:"the story's id, or its news.ycombinator.com/item?id= link"`
}

type Comment struct {
	ID     int    `json:"id"`
	Depth  int    `json:"depth" jsonschema:"1 answers the story, 2 answers a depth-1 comment..."`
	Author string `json:"author"`
	Text   string `json:"text"`
}

type StoryOutput struct {
	Story
	Thread      []Comment `json:"comments_shown"`
	CommentsCut bool      `json:"comments_cut,omitempty" jsonschema:"the discussion is longer than what is shown"`
}

type item struct {
	ID       int     `json:"id"`
	Type     string  `json:"type"`
	Author   string  `json:"author"`
	Title    string  `json:"title"`
	URL      string  `json:"url"`
	Text     string  `json:"text"`
	Points   *int    `json:"points"`
	Created  int64   `json:"created_at_i"`
	Children []*item `json:"children"`
}

var itemID = regexp.MustCompile(`\d{1,10}`)

func getStory(ctx context.Context, _ *mcp.CallToolRequest, in StoryInput) (*mcp.CallToolResult, StoryOutput, error) {
	id := itemID.FindString(in.ID)
	if id == "" {
		return nil, StoryOutput{}, errors.New("id is a Hacker News item id")
	}
	var it item
	if err := client.JSON(ctx, apiURL+"/items/"+id, nil, &it); err != nil {
		if errors.Is(err, localkit.ErrNotFound) {
			return nil, StoryOutput{}, fmt.Errorf("there is no Hacker News item %s", id)
		}
		return nil, StoryOutput{}, err
	}
	out := StoryOutput{Story: Story{
		ID: id, Title: it.Title, URL: it.URL, Author: it.Author, Discussion: discussion(id),
		Created: time.Unix(it.Created, 0).UTC().Format("2006-01-02 15:04"), Text: localkit.PlainText(it.Text),
	}, Thread: []Comment{}}
	if it.Points != nil {
		out.Points = *it.Points
	}
	out.Comments = countComments(it.Children)
	// Thread order, depth first, as the page shows it; deleted comments
	// (no author) are skipped but their answers kept.
	used := 0
	var walk func(children []*item, depth int)
	walk = func(children []*item, depth int) {
		for _, c := range children {
			if out.CommentsCut {
				return
			}
			if c.Author != "" {
				text := localkit.Cut(localkit.PlainText(c.Text), commentChars)
				if used+utf8.RuneCountInString(text) > discussionChars {
					out.CommentsCut = true
					return
				}
				used += utf8.RuneCountInString(text)
				out.Thread = append(out.Thread, Comment{ID: c.ID, Depth: depth, Author: c.Author, Text: text})
			}
			walk(c.Children, depth+1)
		}
	}
	walk(it.Children, 1)
	return nil, out, nil
}

func countComments(children []*item) int {
	n := 0
	for _, c := range children {
		if c.Type == "comment" && c.Author != "" {
			n++
		}
		n += countComments(c.Children)
	}
	return n
}
