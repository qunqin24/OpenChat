package arxiv

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/qunqin24/openchat/apps/server/internal/plugins/localkit"
)

func TestSearchQuery(t *testing.T) {
	for _, c := range []struct {
		in   SearchInput
		want string
	}{
		{SearchInput{Query: "sparse attention"}, "(all:sparse AND all:attention)"},
		{SearchInput{Query: `the "attention is all you need" paper`}, `(all:"attention is all you need" AND all:paper)`},
		{SearchInput{Query: `au:hinton AND ti:"capsule"`}, `(au:hinton AND ti:"capsule")`},
		{SearchInput{Query: "llm", Category: "cs.CL"}, "(all:llm) AND cat:cs.CL"},
		{SearchInput{Category: "cs.CL", From: "2025-01-01"}, "cat:cs.CL AND submittedDate:[202501010000 TO 999912312359]"},
	} {
		if got, err := searchQuery(c.in); err != nil || got != c.want {
			t.Errorf("%+v: %q %v, want %q", c.in, got, err, c.want)
		}
	}
	if _, err := searchQuery(SearchInput{Query: "x", From: "2025/01/01"}); err == nil {
		t.Error("a malformed day was taken")
	}
	if _, err := searchQuery(SearchInput{}); err == nil {
		t.Error("an empty query was taken")
	}
}

func TestPaperID(t *testing.T) {
	for raw, want := range map[string]string{
		"2401.12345":                            "2401.12345",
		"arXiv:2401.12345v2":                    "2401.12345v2",
		"https://arxiv.org/abs/1706.03762v7":    "1706.03762v7",
		"https://arxiv.org/pdf/1706.03762":      "1706.03762",
		"hep-th/9901001":                        "hep-th/9901001",
		"https://arxiv.org/abs/math.AG/0309136": "math.AG/0309136",
	} {
		if got, err := paperID(raw); err != nil || got != want {
			t.Errorf("%s: %q %v, want %q", raw, got, err, want)
		}
	}
	if _, err := paperID("not a paper"); err == nil {
		t.Error("nonsense taken as an id")
	}
}

// TestLive reaches arXiv: only with ARXIV_LIVE=1.
func TestLive(t *testing.T) {
	if os.Getenv("ARXIV_LIVE") == "" {
		t.Skip("set ARXIV_LIVE=1 to reach arXiv")
	}
	ctx := context.Background()
	clientEnd, serverEnd := mcp.NewInMemoryTransports()
	if _, err := NewServer(localkit.Options{Version: "test"}).Connect(ctx, serverEnd, nil); err != nil {
		t.Fatal(err)
	}
	session, err := mcp.NewClient(&mcp.Implementation{Name: "t"}, nil).Connect(ctx, clientEnd, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	call := func(name string, args map[string]any) string {
		out, err := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if out.IsError {
			t.Fatalf("%s: %s", name, out.Content[0].(*mcp.TextContent).Text)
		}
		return out.Content[0].(*mcp.TextContent).Text
	}
	if s := call("search_papers", map[string]any{"query": `"attention is all you need"`, "max_results": 3}); !strings.Contains(s, "1706.03762") {
		t.Errorf("search: %.500s", s)
	}
	if s := call("get_paper", map[string]any{"id": "1706.03762"}); !strings.Contains(s, "Vaswani") {
		t.Errorf("paper: %.500s", s)
	}
	s := call("read_paper", map[string]any{"id": "2310.06825"})
	if !strings.Contains(s, `"next_offset"`) {
		t.Errorf("read: %.800s", s)
	}
	t.Logf("read: %.300s", s)
}
