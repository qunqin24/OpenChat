// Package openalex is the built-in OpenAlex plugin's service, run inside
// OpenChat (plugins.LocalScheme): scholarly works across every field and
// publisher - their citations, references and authors - from the OpenAlex
// API (docs.openalex.org). It complements arXiv, which has preprints but no
// citations.
//
// OpenAlex meters its API by a daily budget: without a key, everyone
// keyless on the same address shares a small one. The administrator's key
// (the plugin's header value, localkit.Options.Secret) is sent as api_key.
package openalex

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/qunqin24/openchat/apps/server/internal/plugins/localkit"
)

const (
	apiURL      = "https://api.openalex.org"
	maxPerPage  = 25
	listAuthors = 8
	listExcerpt = 400
)

// One client for the process: OpenAlex asks for no more than ten requests a
// second; a little under keeps clear of it.
var client = &localkit.Client{Gap: 120 * time.Millisecond}

type service struct{ key string }

// NewServer is the service, with its tools.
func NewServer(o localkit.Options) *mcp.Server {
	s := &service{key: strings.TrimSpace(o.Secret)}
	server := mcp.NewServer(&mcp.Implementation{Name: "openalex", Title: "OpenAlex", Version: o.Version}, nil)
	mcp.AddTool(server, &mcp.Tool{
		Name: "search_works",
		Description: "Search scholarly works (papers, books, datasets) in every field. query is words to look for in " +
			"titles, abstracts and full text. Sort by relevance, citations (most cited first) or newest. Each work has its " +
			"citation count, venue, authors, links and the start of its abstract.",
		Annotations: localkit.ReadOnly,
	}, s.searchWorks)
	mcp.AddTool(server, &mcp.Tool{
		Name: "get_work",
		Description: "One work in full: the whole abstract, every author with their institutions, topics, citations by " +
			"year, and how many works it cites. id is an OpenAlex id (W2626778328), a DOI, an arXiv id or a URL of one.",
		Annotations: localkit.ReadOnly,
	}, s.getWork)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "citing_works",
		Description: "The works that cite a work, most cited or newest first: who built on it.",
		Annotations: localkit.ReadOnly,
	}, s.citingWorks)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "references",
		Description: "The works a work cites (its reference list, as OpenAlex matched it), most cited first.",
		Annotations: localkit.ReadOnly,
	}, s.references)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "search_authors",
		Description: "Find researchers by name: their works and citations, h-index, institutions and main topics.",
		Annotations: localkit.ReadOnly,
	}, s.searchAuthors)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "author_works",
		Description: "An author's works (author_id from search_authors), most cited or newest first.",
		Annotations: localkit.ReadOnly,
	}, s.authorWorks)
	return server
}

func (s *service) get(ctx context.Context, path string, params url.Values, out any) error {
	if params == nil {
		params = url.Values{}
	}
	if s.key != "" {
		params.Set("api_key", s.key)
	}
	err := client.JSON(ctx, apiURL+path+"?"+params.Encode(), nil, out)
	if errors.Is(err, localkit.ErrNotFound) {
		return errors.New("OpenAlex has no such record")
	}
	return err
}

// ---------------------------------------------------------------- records

type apiWork struct {
	ID              string   `json:"id"`
	DOI             string   `json:"doi"`
	Title           string   `json:"display_name"`
	Year            int      `json:"publication_year"`
	Date            string   `json:"publication_date"`
	Type            string   `json:"type"`
	CitedBy         int      `json:"cited_by_count"`
	ReferencedCount int      `json:"referenced_works_count"`
	ReferencedWorks []string `json:"referenced_works"`
	Authorships     []struct {
		Author struct {
			ID   string `json:"id"`
			Name string `json:"display_name"`
		} `json:"author"`
		Institutions []struct {
			Name string `json:"display_name"`
		} `json:"institutions"`
	} `json:"authorships"`
	Primary struct {
		Source *struct {
			Name string `json:"display_name"`
		} `json:"source"`
		Landing string `json:"landing_page_url"`
	} `json:"primary_location"`
	OpenAccess struct {
		IsOA bool   `json:"is_oa"`
		URL  string `json:"oa_url"`
	} `json:"open_access"`
	Abstract map[string][]int `json:"abstract_inverted_index"`
	Topics   []struct {
		Name string `json:"display_name"`
	} `json:"topics"`
	CountsByYear []struct {
		Year    int `json:"year"`
		CitedBy int `json:"cited_by_count"`
	} `json:"counts_by_year"`
}

// Work is a work as the tools give it.
type Work struct {
	ID       string   `json:"id" jsonschema:"the OpenAlex id, for get_work, citing_works and references"`
	Title    string   `json:"title"`
	Year     int      `json:"year,omitempty"`
	Date     string   `json:"date,omitempty"`
	Type     string   `json:"type,omitempty"`
	Venue    string   `json:"venue,omitempty" jsonschema:"the journal, conference or repository"`
	CitedBy  int      `json:"cited_by"`
	Authors  []string `json:"authors"`
	DOI      string   `json:"doi,omitempty"`
	URL      string   `json:"url,omitempty" jsonschema:"where to read it: an open-access copy if there is one, else its page"`
	OpenAccs bool     `json:"open_access"`
	Abstract string   `json:"abstract,omitempty"`
}

// WorkDetail is one work in full.
type WorkDetail struct {
	Work
	Authors      []Author       `json:"authors"`
	Topics       []string       `json:"topics"`
	CitesCount   int            `json:"cites_count" jsonschema:"how many works it cites"`
	CitedByYears map[string]int `json:"cited_by_years,omitempty" jsonschema:"citations it received each year, recent years"`
}

// Author is an author of a work.
type Author struct {
	ID           string   `json:"id,omitempty"`
	Name         string   `json:"name"`
	Institutions []string `json:"institutions,omitempty"`
}

func shortID(id string) string { return strings.TrimPrefix(id, "https://openalex.org/") }

func (w apiWork) brief(excerpt int) Work {
	out := Work{
		ID: shortID(w.ID), Title: localkit.Collapse(w.Title), Year: w.Year, Date: w.Date, Type: w.Type,
		CitedBy: w.CitedBy, DOI: w.DOI, OpenAccs: w.OpenAccess.IsOA, Authors: []string{},
	}
	if w.Primary.Source != nil {
		out.Venue = w.Primary.Source.Name
	}
	out.URL = w.OpenAccess.URL
	if out.URL == "" {
		out.URL = w.Primary.Landing
	}
	if out.URL == "" {
		out.URL = w.DOI
	}
	for i, a := range w.Authorships {
		if excerpt > 0 && i == listAuthors {
			out.Authors = append(out.Authors, fmt.Sprintf("and %d more", len(w.Authorships)-listAuthors))
			break
		}
		out.Authors = append(out.Authors, a.Author.Name)
	}
	if abstract := abstractText(w.Abstract); abstract != "" {
		if excerpt > 0 {
			abstract = localkit.Cut(abstract, excerpt)
		}
		out.Abstract = abstract
	}
	return out
}

// abstractText puts an abstract back together from OpenAlex's index of each
// word's positions.
func abstractText(index map[string][]int) string {
	if len(index) == 0 {
		return ""
	}
	type placed struct {
		at   int
		word string
	}
	var words []placed
	for word, positions := range index {
		for _, at := range positions {
			words = append(words, placed{at, word})
		}
	}
	sort.Slice(words, func(i, j int) bool { return words[i].at < words[j].at })
	parts := make([]string, len(words))
	for i, w := range words {
		parts[i] = w.word
	}
	return strings.Join(parts, " ")
}

type workPage struct {
	Meta struct {
		Count int `json:"count"`
	} `json:"meta"`
	Results []apiWork `json:"results"`
}

// WorkList is a page of works.
type WorkList struct {
	Total int    `json:"total" jsonschema:"how many match in all"`
	Page  int    `json:"page"`
	Works []Work `json:"works" jsonschema:"each abstract cut to its start and long author lists shortened; get_work has them whole"`
}

const listFields = "id,doi,display_name,publication_year,publication_date,type,cited_by_count,authorships,primary_location,open_access,abstract_inverted_index"

func (s *service) works(ctx context.Context, params url.Values, page int) (WorkList, error) {
	params.Set("select", listFields)
	params.Set("page", fmt.Sprint(max(page, 1)))
	var p workPage
	if err := s.get(ctx, "/works", params, &p); err != nil {
		return WorkList{}, err
	}
	out := WorkList{Total: p.Meta.Count, Page: max(page, 1), Works: []Work{}}
	for _, w := range p.Results {
		out.Works = append(out.Works, w.brief(listExcerpt))
	}
	return out, nil
}

func perPage(n int) string {
	if n <= 0 {
		n = 10
	}
	return fmt.Sprint(min(n, maxPerPage))
}

// ---------------------------------------------------------------- ids

var (
	workID  = regexp.MustCompile(`(?i)\b(W\d{4,})\b`)
	doiID   = regexp.MustCompile(`(?i)\b(10\.\d{4,9}/[^\s"<>]+)`)
	arxivID = regexp.MustCompile(`\b(\d{4}\.\d{4,5})(v\d+)?\b`)
)

// workKey is a work id as OpenAlex takes it: W…, doi:…, or an arXiv id as
// the DOI arXiv gives every paper.
func workKey(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if m := workID.FindStringSubmatch(raw); m != nil {
		return strings.ToUpper(m[1]), nil
	}
	if m := doiID.FindStringSubmatch(raw); m != nil {
		return "doi:" + strings.TrimRight(m[1], ".,;"), nil
	}
	if m := arxivID.FindStringSubmatch(raw); m != nil {
		return "doi:10.48550/arXiv." + m[1], nil
	}
	return "", fmt.Errorf("%q is not an OpenAlex id, DOI or arXiv id", raw)
}

// ---------------------------------------------------------------- tools

type SearchWorksInput struct {
	Query      string `json:"query" jsonschema:"words to look for"`
	Sort       string `json:"sort,omitempty" jsonschema:"relevance (the default), cited (most cited first) or newest"`
	FromYear   int    `json:"from_year,omitempty" jsonschema:"only works published in or after this year"`
	ToYear     int    `json:"to_year,omitempty" jsonschema:"only works published in or before this year"`
	OpenAccess bool   `json:"open_access,omitempty" jsonschema:"only works free to read"`
	Type       string `json:"type,omitempty" jsonschema:"only this type: article, preprint, review, book, book-chapter, dataset, dissertation"`
	MaxResults int    `json:"max_results,omitempty" jsonschema:"1 to 25; 10 when absent"`
	Page       int    `json:"page,omitempty"`
}

var typePattern = regexp.MustCompile(`^[a-z-]{2,30}$`)

func (s *service) searchWorks(ctx context.Context, _ *mcp.CallToolRequest, in SearchWorksInput) (*mcp.CallToolResult, WorkList, error) {
	q := strings.TrimSpace(in.Query)
	if q == "" {
		return nil, WorkList{}, errors.New("query is empty")
	}
	params := url.Values{"search": {q}, "per-page": {perPage(in.MaxResults)}}
	var filters []string
	if in.FromYear > 0 {
		filters = append(filters, fmt.Sprintf("from_publication_date:%04d-01-01", in.FromYear))
	}
	if in.ToYear > 0 {
		filters = append(filters, fmt.Sprintf("to_publication_date:%04d-12-31", in.ToYear))
	}
	if in.OpenAccess {
		filters = append(filters, "is_oa:true")
	}
	if in.Type != "" {
		if !typePattern.MatchString(in.Type) {
			return nil, WorkList{}, errors.New("type is a word such as article or preprint")
		}
		filters = append(filters, "type:"+in.Type)
	}
	if len(filters) > 0 {
		params.Set("filter", strings.Join(filters, ","))
	}
	order, err := sortOf(in.Sort, true)
	if err != nil {
		return nil, WorkList{}, err
	}
	if order != "" {
		params.Set("sort", order)
	}
	out, err := s.works(ctx, params, in.Page)
	return nil, out, err
}

// sortOf is a sort as OpenAlex takes it; empty is its relevance order.
func sortOf(asked string, searching bool) (string, error) {
	switch asked {
	case "", "relevance":
		if searching {
			return "", nil
		}
		return "cited_by_count:desc", nil
	case "cited":
		return "cited_by_count:desc", nil
	case "newest":
		return "publication_date:desc", nil
	}
	return "", errors.New("sort is relevance, cited or newest")
}

type WorkInput struct {
	ID string `json:"id" jsonschema:"an OpenAlex id (W…), a DOI, an arXiv id, or a URL with one"`
}

func (s *service) work(ctx context.Context, raw string) (apiWork, error) {
	key, err := workKey(raw)
	if err != nil {
		return apiWork{}, err
	}
	var w apiWork
	err = s.get(ctx, "/works/"+key, nil, &w)
	return w, err
}

func (s *service) getWork(ctx context.Context, _ *mcp.CallToolRequest, in WorkInput) (*mcp.CallToolResult, WorkDetail, error) {
	w, err := s.work(ctx, in.ID)
	if err != nil {
		return nil, WorkDetail{}, err
	}
	out := WorkDetail{Work: w.brief(0), Topics: []string{}, CitesCount: len(w.ReferencedWorks)}
	if w.ReferencedCount > 0 {
		out.CitesCount = w.ReferencedCount
	}
	for _, a := range w.Authorships {
		author := Author{ID: shortID(a.Author.ID), Name: a.Author.Name}
		for _, inst := range a.Institutions {
			author.Institutions = append(author.Institutions, inst.Name)
		}
		out.Authors = append(out.Authors, author)
	}
	out.Work.Authors = nil
	for i, t := range w.Topics {
		if i == 5 {
			break
		}
		out.Topics = append(out.Topics, t.Name)
	}
	if len(w.CountsByYear) > 0 {
		out.CitedByYears = map[string]int{}
		for _, c := range w.CountsByYear {
			out.CitedByYears[fmt.Sprint(c.Year)] = c.CitedBy
		}
	}
	return nil, out, nil
}

type CitingInput struct {
	ID         string `json:"id" jsonschema:"the cited work: an OpenAlex id, a DOI or an arXiv id"`
	Sort       string `json:"sort,omitempty" jsonschema:"cited (the default: most cited first) or newest"`
	MaxResults int    `json:"max_results,omitempty" jsonschema:"1 to 25; 10 when absent"`
	Page       int    `json:"page,omitempty"`
}

func (s *service) citingWorks(ctx context.Context, _ *mcp.CallToolRequest, in CitingInput) (*mcp.CallToolResult, WorkList, error) {
	w, err := s.work(ctx, in.ID)
	if err != nil {
		return nil, WorkList{}, err
	}
	order, err := sortOf(in.Sort, false)
	if err != nil {
		return nil, WorkList{}, err
	}
	params := url.Values{"filter": {"cites:" + shortID(w.ID)}, "sort": {order}, "per-page": {perPage(in.MaxResults)}}
	out, err := s.works(ctx, params, in.Page)
	return nil, out, err
}

type ReferencesInput struct {
	ID         string `json:"id" jsonschema:"the citing work: an OpenAlex id, a DOI or an arXiv id"`
	MaxResults int    `json:"max_results,omitempty" jsonschema:"1 to 25; 25 when absent"`
}

func (s *service) references(ctx context.Context, _ *mcp.CallToolRequest, in ReferencesInput) (*mcp.CallToolResult, WorkList, error) {
	w, err := s.work(ctx, in.ID)
	if err != nil {
		return nil, WorkList{}, err
	}
	if len(w.ReferencedWorks) == 0 {
		return nil, WorkList{Works: []Work{}}, nil
	}
	ids := make([]string, 0, len(w.ReferencedWorks))
	for _, id := range w.ReferencedWorks {
		ids = append(ids, shortID(id))
	}
	n := in.MaxResults
	if n <= 0 {
		n = maxPerPage
	}
	// OpenAlex takes up to 100 ids in one filter; a reference list longer
	// than that is read from its first hundred.
	if len(ids) > 100 {
		ids = ids[:100]
	}
	params := url.Values{"filter": {"openalex_id:" + strings.Join(ids, "|")}, "sort": {"cited_by_count:desc"}, "per-page": {perPage(n)}}
	out, err := s.works(ctx, params, 1)
	out.Total = len(w.ReferencedWorks)
	return nil, out, err
}

type apiAuthor struct {
	ID      string `json:"id"`
	Name    string `json:"display_name"`
	Works   int    `json:"works_count"`
	CitedBy int    `json:"cited_by_count"`
	ORCID   string `json:"orcid"`
	Summary struct {
		HIndex int `json:"h_index"`
	} `json:"summary_stats"`
	Institutions []struct {
		Name    string `json:"display_name"`
		Country string `json:"country_code"`
	} `json:"last_known_institutions"`
	Topics []struct {
		Name string `json:"display_name"`
	} `json:"topics"`
}

// Researcher is an author as search_authors gives them.
type Researcher struct {
	ID           string   `json:"id" jsonschema:"for author_works"`
	Name         string   `json:"name"`
	Works        int      `json:"works"`
	CitedBy      int      `json:"cited_by"`
	HIndex       int      `json:"h_index"`
	Institutions []string `json:"institutions,omitempty"`
	Topics       []string `json:"topics,omitempty"`
	ORCID        string   `json:"orcid,omitempty"`
}

type AuthorsInput struct {
	Query      string `json:"query" jsonschema:"the researcher's name"`
	MaxResults int    `json:"max_results,omitempty" jsonschema:"1 to 25; 5 when absent"`
}

type AuthorList struct {
	Total   int          `json:"total"`
	Authors []Researcher `json:"authors" jsonschema:"several people can share a name: tell them apart by institution and topics"`
}

func (s *service) searchAuthors(ctx context.Context, _ *mcp.CallToolRequest, in AuthorsInput) (*mcp.CallToolResult, AuthorList, error) {
	q := strings.TrimSpace(in.Query)
	if q == "" {
		return nil, AuthorList{}, errors.New("query is empty")
	}
	n := in.MaxResults
	if n <= 0 {
		n = 5
	}
	var p struct {
		Meta struct {
			Count int `json:"count"`
		} `json:"meta"`
		Results []apiAuthor `json:"results"`
	}
	if err := s.get(ctx, "/authors", url.Values{"search": {q}, "per-page": {perPage(n)}}, &p); err != nil {
		return nil, AuthorList{}, err
	}
	out := AuthorList{Total: p.Meta.Count, Authors: []Researcher{}}
	for _, a := range p.Results {
		r := Researcher{ID: shortID(a.ID), Name: a.Name, Works: a.Works, CitedBy: a.CitedBy, HIndex: a.Summary.HIndex, ORCID: a.ORCID}
		for _, inst := range a.Institutions {
			name := inst.Name
			if inst.Country != "" {
				name += " (" + inst.Country + ")"
			}
			r.Institutions = append(r.Institutions, name)
		}
		for i, t := range a.Topics {
			if i == 5 {
				break
			}
			r.Topics = append(r.Topics, t.Name)
		}
		out.Authors = append(out.Authors, r)
	}
	return nil, out, nil
}

type AuthorWorksInput struct {
	AuthorID   string `json:"author_id" jsonschema:"the author's OpenAlex id (A…), from search_authors"`
	Sort       string `json:"sort,omitempty" jsonschema:"cited (the default: most cited first) or newest"`
	MaxResults int    `json:"max_results,omitempty" jsonschema:"1 to 25; 10 when absent"`
	Page       int    `json:"page,omitempty"`
}

var authorID = regexp.MustCompile(`(?i)\b(A\d{4,})\b`)

func (s *service) authorWorks(ctx context.Context, _ *mcp.CallToolRequest, in AuthorWorksInput) (*mcp.CallToolResult, WorkList, error) {
	m := authorID.FindStringSubmatch(in.AuthorID)
	if m == nil {
		return nil, WorkList{}, errors.New("author_id is an OpenAlex author id (A…) from search_authors")
	}
	order, err := sortOf(in.Sort, false)
	if err != nil {
		return nil, WorkList{}, err
	}
	params := url.Values{"filter": {"author.id:" + strings.ToUpper(m[1])}, "sort": {order}, "per-page": {perPage(in.MaxResults)}}
	out, err := s.works(ctx, params, in.Page)
	return nil, out, err
}
