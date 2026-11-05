// Package arxiv is the built-in arXiv plugin's MCP service, run inside the
// OpenChat process (plugins.LocalScheme): searching arXiv, a paper's details,
// and its full text, from the HTML arXiv renders or else its PDF.
//
// arXiv asks API users for no more than one request every three seconds,
// over one connection (info.arxiv.org/help/api/tou.html): every request to
// arXiv from this process waits its turn (limiter), whoever makes it.
package arxiv

import (
	"container/list"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/qunqin24/openchat/apps/server/internal/doctext"
	"github.com/qunqin24/openchat/apps/server/internal/plugins/localkit"
	"github.com/qunqin24/openchat/apps/server/internal/tools"
)

const (
	apiURL = "https://export.arxiv.org/api/query"
	// requestGap is arXiv's: one request every three seconds.
	requestGap     = 3 * time.Second
	requestTimeout = 30 * time.Second
	maxFeedBytes   = 4 << 20
	maxHTMLBytes   = 15 << 20
	maxPDFBytes    = 40 << 20
	// searchAbstract is how much of each abstract a search shows.
	searchAbstract = 600
	searchAuthors  = 10
	maxResults     = 50
	// The texts read lately, so reading on does not fetch the paper again.
	textCacheSize = 16
	textCacheTTL  = 30 * time.Minute
)

// NewServer is the service, with its tools.
func NewServer(o localkit.Options) *mcp.Server {
	s := &service{client: tools.GuardedClient(requestTimeout), texts: newTextCache()}
	server := mcp.NewServer(&mcp.Implementation{Name: "arxiv", Title: "arXiv", Version: o.Version}, nil)
	readOnly := &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: ptr(true)}
	mcp.AddTool(server, &mcp.Tool{
		Name: "search_papers",
		Description: "Search arXiv papers. query is words to look for - each must appear, common words are dropped, " +
			"and a quoted \"phrase\" must appear as written: quote a title you know - or arXiv's syntax: ti:, au:, " +
			"abs:, cat: fields with AND, OR, ANDNOT (au:hinton AND ti:\"capsule network\"). arXiv's ranking is " +
			"plain: prefer a few distinctive words, a quoted phrase or ti: over long questions. Returns the total " +
			"found and a page of papers with the start of each abstract; page on with start.",
		Annotations: readOnly,
	}, s.search)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_paper",
		Description: "A paper's details: title, every author, the whole abstract, categories, dates, comments, journal reference, DOI and links. id is an arXiv id (2401.12345, 2401.12345v2, hep-th/9901001) or its URL.",
		Annotations: readOnly,
	}, s.paper)
	mcp.AddTool(server, &mcp.Tool{
		Name: "read_paper",
		Description: "A paper's full text, in parts of about 20,000 characters: from the HTML version arXiv " +
			"renders, else from its PDF. Read on with the next_offset a part gives; it is absent at the end.",
		Annotations: readOnly,
	}, s.read)
	return server
}

func ptr[T any](v T) *T { return &v }

type service struct {
	client *http.Client
	texts  *textCache
}

// limiter keeps the process to one request to arXiv every requestGap.
var limiter struct {
	mu   sync.Mutex
	last time.Time
}

func wait(ctx context.Context) (func(), error) {
	limiter.mu.Lock()
	if d := requestGap - time.Since(limiter.last); d > 0 {
		select {
		case <-ctx.Done():
			limiter.mu.Unlock()
			return nil, ctx.Err()
		case <-time.After(d):
		}
	}
	// The next waits from when this one ends, not starts: one at a time.
	return func() {
		limiter.last = time.Now()
		limiter.mu.Unlock()
	}, nil
}

// get fetches address within arXiv's limit, at most limit bytes.
func (s *service) get(ctx context.Context, address string, limit int64) (*http.Response, []byte, error) {
	done, err := wait(ctx)
	if err != nil {
		return nil, nil, err
	}
	defer done()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("User-Agent", "OpenChat arXiv plugin (+https://github.com/qunqin24/openchat)")
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("arXiv could not be reached: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, nil, fmt.Errorf("reading from arXiv: %w", err)
	}
	if int64(len(body)) > limit {
		return nil, nil, errors.New("arXiv's answer is too large to read")
	}
	return resp, body, nil
}

// ---------------------------------------------------------------- the API's feed

type feed struct {
	Total   int     `xml:"http://a9.com/-/spec/opensearch/1.1/ totalResults"`
	Start   int     `xml:"http://a9.com/-/spec/opensearch/1.1/ startIndex"`
	Entries []entry `xml:"http://www.w3.org/2005/Atom entry"`
}

type entry struct {
	ID        string `xml:"http://www.w3.org/2005/Atom id"`
	Title     string `xml:"http://www.w3.org/2005/Atom title"`
	Summary   string `xml:"http://www.w3.org/2005/Atom summary"`
	Published string `xml:"http://www.w3.org/2005/Atom published"`
	Updated   string `xml:"http://www.w3.org/2005/Atom updated"`
	Authors   []struct {
		Name string `xml:"http://www.w3.org/2005/Atom name"`
	} `xml:"http://www.w3.org/2005/Atom author"`
	Primary struct {
		Term string `xml:"term,attr"`
	} `xml:"http://arxiv.org/schemas/atom primary_category"`
	Categories []struct {
		Term string `xml:"term,attr"`
	} `xml:"http://www.w3.org/2005/Atom category"`
	Comment    string `xml:"http://arxiv.org/schemas/atom comment"`
	JournalRef string `xml:"http://arxiv.org/schemas/atom journal_ref"`
	DOI        string `xml:"http://arxiv.org/schemas/atom doi"`
}

// Paper is a paper as the tools give it.
type Paper struct {
	ID         string   `json:"id" jsonschema:"the arXiv id, with its version"`
	Title      string   `json:"title"`
	Authors    []string `json:"authors"`
	Published  string   `json:"published" jsonschema:"the day the first version was submitted"`
	Updated    string   `json:"updated" jsonschema:"the day the latest version was submitted"`
	Categories []string `json:"categories" jsonschema:"the primary category first"`
	Abstract   string   `json:"abstract"`
	Comment    string   `json:"comment,omitempty"`
	JournalRef string   `json:"journal_ref,omitempty"`
	DOI        string   `json:"doi,omitempty"`
	URL        string   `json:"url"`
	PDF        string   `json:"pdf"`
}

func (e entry) paper() Paper {
	id := e.ID
	if i := strings.Index(id, "/abs/"); i >= 0 {
		id = id[i+len("/abs/"):]
	}
	p := Paper{
		ID: id, Title: collapse(e.Title), Abstract: collapse(e.Summary),
		Published: day(e.Published), Updated: day(e.Updated),
		Comment: collapse(e.Comment), JournalRef: collapse(e.JournalRef), DOI: collapse(e.DOI),
		URL: "https://arxiv.org/abs/" + id, PDF: "https://arxiv.org/pdf/" + id,
		Authors: []string{}, Categories: []string{},
	}
	for _, a := range e.Authors {
		p.Authors = append(p.Authors, collapse(a.Name))
	}
	if e.Primary.Term != "" {
		p.Categories = append(p.Categories, e.Primary.Term)
	}
	for _, c := range e.Categories {
		if c.Term != e.Primary.Term {
			p.Categories = append(p.Categories, c.Term)
		}
	}
	return p
}

func (s *service) query(ctx context.Context, params url.Values) (feed, error) {
	resp, body, err := s.get(ctx, apiURL+"?"+params.Encode(), maxFeedBytes)
	if err != nil {
		return feed{}, err
	}
	if resp.StatusCode != http.StatusOK {
		return feed{}, fmt.Errorf("arXiv answered %s", resp.Status)
	}
	var f feed
	if err := xml.Unmarshal(body, &f); err != nil {
		return feed{}, fmt.Errorf("arXiv's answer could not be read: %w", err)
	}
	// An error is a feed of one entry titled Error.
	if len(f.Entries) == 1 && strings.TrimSpace(f.Entries[0].Title) == "Error" {
		return feed{}, fmt.Errorf("arXiv refused the query: %s", collapse(f.Entries[0].Summary))
	}
	return f, nil
}

// ---------------------------------------------------------------- search_papers

type SearchInput struct {
	Query string `json:"query" jsonschema:"words to look for, or arXiv query syntax (ti:, au:, abs:, cat:, AND, OR, ANDNOT)"`
	// Category narrows to one arXiv category, such as cs.CL or math.AG.
	Category string `json:"category,omitempty" jsonschema:"an arXiv category to keep to, such as cs.CL"`
	Sort     string `json:"sort,omitempty" jsonschema:"relevance (the default), submitted (newest first) or updated (latest revised first)"`
	From     string `json:"from,omitempty" jsonschema:"only papers submitted on or after this day, YYYY-MM-DD"`
	To       string `json:"to,omitempty" jsonschema:"only papers submitted on or before this day, YYYY-MM-DD"`
	Max      int    `json:"max_results,omitempty" jsonschema:"how many papers, 1 to 50; 10 when absent"`
	Start    int    `json:"start,omitempty" jsonschema:"how many to skip, to page on"`
}

type SearchOutput struct {
	Total  int     `json:"total" jsonschema:"how many papers match in all"`
	Start  int     `json:"start"`
	Papers []Paper `json:"papers" jsonschema:"each abstract cut to its start, and long author lists to the first ten; get_paper has them whole"`
}

// fielded is a query already in arXiv's syntax.
var fielded = regexp.MustCompile(`(?i)\b(ti|au|abs|co|jr|cat|rn|id|all|submittedDate):`)

var dayPattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

// plainTerm is a quoted phrase or a word.
var plainTerm = regexp.MustCompile(`"[^"]+"|[^\s"]+`)

// stopwords are words arXiv's search leaves out of its index.
var stopwords = map[string]bool{}

func init() {
	for _, w := range strings.Fields("a an and are as at be but by for from has have in into is it its of on or " +
		"that the their this to was were will with you your we our all not no can do does how what which why") {
		stopwords[w] = true
	}
}

// searchQuery is input as arXiv's search_query.
func searchQuery(in SearchInput) (string, error) {
	q := strings.TrimSpace(in.Query)
	if q == "" && in.Category == "" {
		return "", errors.New("query is empty")
	}
	var parts []string
	if q != "" {
		if !fielded.MatchString(q) {
			// Plain words: each must appear somewhere, as a search engine would
			// have it, and a quoted phrase as written. arXiv indexes no common
			// words: one of them required would match nothing.
			var terms []string
			for _, term := range plainTerm.FindAllString(q, -1) {
				if strings.HasPrefix(term, `"`) {
					terms = append(terms, "all:"+term)
				} else if !stopwords[strings.ToLower(term)] {
					terms = append(terms, "all:"+strings.Trim(term, `"`))
				}
			}
			if len(terms) == 0 {
				return "", errors.New("query has only common words; give distinctive ones")
			}
			q = strings.Join(terms, " AND ")
		}
		parts = append(parts, "("+q+")")
	}
	if c := strings.TrimSpace(in.Category); c != "" {
		parts = append(parts, "cat:"+c)
	}
	if in.From != "" || in.To != "" {
		from, to := "000001010000", "999912312359"
		for _, d := range []struct {
			value string
			into  *string
			clock string
		}{{in.From, &from, "0000"}, {in.To, &to, "2359"}} {
			if d.value == "" {
				continue
			}
			if !dayPattern.MatchString(d.value) {
				return "", errors.New("from and to are days, YYYY-MM-DD")
			}
			*d.into = strings.ReplaceAll(d.value, "-", "") + d.clock
		}
		parts = append(parts, fmt.Sprintf("submittedDate:[%s TO %s]", from, to))
	}
	return strings.Join(parts, " AND "), nil
}

func (s *service) search(ctx context.Context, _ *mcp.CallToolRequest, in SearchInput) (*mcp.CallToolResult, SearchOutput, error) {
	q, err := searchQuery(in)
	if err != nil {
		return nil, SearchOutput{}, err
	}
	count := in.Max
	if count <= 0 {
		count = 10
	}
	count = min(count, maxResults)
	params := url.Values{"search_query": {q}, "start": {fmt.Sprint(max(0, in.Start))}, "max_results": {fmt.Sprint(count)}}
	switch in.Sort {
	case "", "relevance":
		params.Set("sortBy", "relevance")
	case "submitted":
		params.Set("sortBy", "submittedDate")
	case "updated":
		params.Set("sortBy", "lastUpdatedDate")
	default:
		return nil, SearchOutput{}, errors.New("sort is relevance, submitted or updated")
	}
	params.Set("sortOrder", "descending")
	f, err := s.query(ctx, params)
	if err != nil {
		return nil, SearchOutput{}, err
	}
	out := SearchOutput{Total: f.Total, Start: f.Start, Papers: []Paper{}}
	for _, e := range f.Entries {
		p := e.paper()
		p.Abstract = cut(p.Abstract, searchAbstract)
		if len(p.Authors) > searchAuthors {
			p.Authors = append(p.Authors[:searchAuthors], "et al.")
		}
		out.Papers = append(out.Papers, p)
	}
	return nil, out, nil
}

// ---------------------------------------------------------------- get_paper

type PaperInput struct {
	ID string `json:"id" jsonschema:"an arXiv id or its URL"`
}

var (
	newID = regexp.MustCompile(`\b(\d{4}\.\d{4,5})(v\d+)?\b`)
	oldID = regexp.MustCompile(`\b([a-z][a-z\-]+(?:\.[A-Z]{2})?/\d{7})(v\d+)?\b`)
)

// paperID is the id in what was given: an id, arXiv:<id>, or a URL with it.
func paperID(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	for _, pattern := range []*regexp.Regexp{newID, oldID} {
		if m := pattern.FindStringSubmatch(raw); m != nil {
			return m[1] + m[2], nil
		}
	}
	return "", fmt.Errorf("%q is not an arXiv id", raw)
}

func (s *service) lookup(ctx context.Context, raw string) (Paper, error) {
	id, err := paperID(raw)
	if err != nil {
		return Paper{}, err
	}
	f, err := s.query(ctx, url.Values{"id_list": {id}, "max_results": {"1"}})
	if err != nil {
		return Paper{}, err
	}
	if len(f.Entries) == 0 || strings.TrimSpace(f.Entries[0].Title) == "" {
		return Paper{}, fmt.Errorf("there is no arXiv paper %s", id)
	}
	return f.Entries[0].paper(), nil
}

func (s *service) paper(ctx context.Context, _ *mcp.CallToolRequest, in PaperInput) (*mcp.CallToolResult, Paper, error) {
	p, err := s.lookup(ctx, in.ID)
	return nil, p, err
}

// ---------------------------------------------------------------- read_paper

type ReadInput struct {
	ID     string `json:"id" jsonschema:"an arXiv id or its URL"`
	Offset int    `json:"offset,omitempty" jsonschema:"where to read on from: a part's next_offset"`
}

type ReadOutput struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	Source     string `json:"source" jsonschema:"html or pdf: what the text was read from"`
	Text       string `json:"text"`
	Offset     int    `json:"offset"`
	Length     int    `json:"length" jsonschema:"the whole text's length in characters"`
	NextOffset *int   `json:"next_offset,omitempty" jsonschema:"where the next part starts; absent at the end"`
}

func (s *service) read(ctx context.Context, _ *mcp.CallToolRequest, in ReadInput) (*mcp.CallToolResult, ReadOutput, error) {
	id, err := paperID(in.ID)
	if err != nil {
		return nil, ReadOutput{}, err
	}
	t, ok := s.texts.get(id)
	if !ok {
		if t, err = s.fullText(ctx, id); err != nil {
			return nil, ReadOutput{}, err
		}
		s.texts.put(id, t)
	}
	part, next := tools.TextPart(t.text, in.Offset)
	out := ReadOutput{ID: id, Title: t.title, Source: t.source, Text: part, Offset: max(0, in.Offset), Length: utf8.RuneCountInString(t.text)}
	if next >= 0 {
		out.NextOffset = &next
	}
	return nil, out, nil
}

type paperText struct {
	title, source, text string
}

// fullText is the paper's words: arXiv's HTML rendering when it has one
// (most papers since late 2023), else the PDF.
func (s *service) fullText(ctx context.Context, id string) (paperText, error) {
	address := "https://arxiv.org/html/" + id
	resp, body, err := s.get(ctx, address, maxHTMLBytes)
	if err != nil {
		return paperText{}, err
	}
	// Without a rendering arXiv answers 404, or sends the abstract page.
	if resp.StatusCode == http.StatusOK && strings.Contains(resp.Request.URL.Path, "/html/") {
		title, text, err := tools.PageText(string(body), resp.Request.URL.String())
		if err == nil && utf8.RuneCountInString(text) > 2000 {
			return paperText{title: title, source: "html", text: text}, nil
		}
	}
	if !doctext.PDFReadable() {
		return paperText{}, errors.New("this paper has no HTML version on arXiv, and this server cannot read PDFs")
	}
	resp, body, err = s.get(ctx, "https://arxiv.org/pdf/"+id, maxPDFBytes)
	if err != nil {
		return paperText{}, err
	}
	if resp.StatusCode != http.StatusOK {
		return paperText{}, fmt.Errorf("arXiv answered %s for the PDF", resp.Status)
	}
	doc, err := doctext.Extract(ctx, doctext.PDFMediaType, body)
	if err != nil {
		return paperText{}, fmt.Errorf("the PDF could not be read: %w", err)
	}
	if doc.Scanned {
		return paperText{}, errors.New("the PDF is pictures of pages, with no text to read")
	}
	title := ""
	if p, err := s.lookup(ctx, id); err == nil {
		title = p.Title
	}
	return paperText{title: title, source: "pdf", text: doc.Content}, nil
}

type textCache struct {
	mu    sync.Mutex
	order *list.List
	items map[string]*list.Element
}

type cached struct {
	id  string
	at  time.Time
	txt paperText
}

func newTextCache() *textCache {
	return &textCache{order: list.New(), items: map[string]*list.Element{}}
}

func (c *textCache) get(id string) (paperText, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	el, ok := c.items[id]
	if !ok || time.Since(el.Value.(*cached).at) > textCacheTTL {
		return paperText{}, false
	}
	c.order.MoveToFront(el)
	return el.Value.(*cached).txt, true
}

func (c *textCache) put(id string, t paperText) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.items[id]; ok {
		c.order.Remove(el)
	}
	c.items[id] = c.order.PushFront(&cached{id: id, at: time.Now(), txt: t})
	for c.order.Len() > textCacheSize {
		last := c.order.Back()
		c.order.Remove(last)
		delete(c.items, last.Value.(*cached).id)
	}
}

// ---------------------------------------------------------------- text

func collapse(s string) string { return strings.Join(strings.Fields(s), " ") }

func day(timestamp string) string {
	if len(timestamp) >= 10 {
		return timestamp[:10]
	}
	return timestamp
}

func cut(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n-1]) + "…"
}
