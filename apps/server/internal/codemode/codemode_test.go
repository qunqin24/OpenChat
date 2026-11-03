package codemode

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

var issueSchema = json.RawMessage(`{"type":"object","properties":{"owner":{"type":"string"},"repo":{"type":"string"},"number":{"type":"integer"},"state":{"enum":["open","closed"]}},"required":["owner","repo","number"]}`)

var testTools = []Tool{
	{Namespace: "github", Name: "get_issue", Description: "Get an issue.\nMore words.", Schema: issueSchema, ReadOnly: true},
	{Namespace: "github", Name: "create_issue", Description: "Create an issue in a repository", Schema: json.RawMessage(`{"type":"object","properties":{"title":{"type":"string"}}}`)},
	{Namespace: "my-crm", Name: "find-contact", Description: "Find a contact by name", Schema: json.RawMessage(`{"type":"object"}`), ReadOnly: true},
}

// echo answers each call with its input and the tool's path, after delay.
type echo struct {
	delay  time.Duration
	fail   string
	active atomic.Int32
	most   atomic.Int32
	pause  time.Duration
}

func (h *echo) Call(ctx context.Context, call Call) (any, error) {
	n := h.active.Add(1)
	defer h.active.Add(-1)
	for {
		m := h.most.Load()
		if n <= m || h.most.CompareAndSwap(m, n) {
			break
		}
	}
	if h.pause > 0 {
		resume := Pause(ctx)
		time.Sleep(h.pause)
		resume()
	}
	select {
	case <-time.After(h.delay):
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	if h.fail != "" && call.Tool.Name == h.fail {
		return nil, errors.New("the service said no")
	}
	var input any
	_ = json.Unmarshal(call.Input, &input)
	return map[string]any{"path": call.Tool.Path(), "input": input, "seq": call.Seq}, nil
}

func runScript(t *testing.T, code string, host Host, limits Limits) Result {
	t.Helper()
	return Run(context.Background(), code, testTools, host, limits)
}

func TestCallsComposeAndRunTogether(t *testing.T) {
	host := &echo{delay: 30 * time.Millisecond}
	res := runScript(t, `
const numbers = [1, 2, 3, 4]
const issues = await Promise.all(numbers.map((n) => tools.github.get_issue({ owner: "o", repo: "r", number: n })))
console.log("got", issues.length, { first: issues[0].input.number })
return issues.map((i) => i.input.number * 10)`, host, Limits{MaxConcurrent: 2})
	if res.Error != nil {
		t.Fatalf("error: %+v", res.Error)
	}
	if string(res.Value) != "[10,20,30,40]" {
		t.Fatalf("value = %s", res.Value)
	}
	if len(res.Calls) != 4 || !res.Calls[3].OK || res.Calls[0].Path != "tools.github.get_issue" {
		t.Fatalf("calls = %+v", res.Calls)
	}
	if got := host.most.Load(); got != 2 {
		t.Fatalf("at most %d calls ran at once, want 2", got)
	}
	if len(res.Logs) != 1 || res.Logs[0] != `got 4 {"first":1}` {
		t.Fatalf("logs = %q", res.Logs)
	}
}

func TestBracketNamespaceAndNoReturn(t *testing.T) {
	res := runScript(t, `await tools["my-crm"]["find-contact"]({ name: "Ada" })`, &echo{}, Limits{})
	if res.Error != nil || string(res.Value) != "null" || len(res.Calls) != 1 || !res.Calls[0].OK {
		t.Fatalf("result = %+v", res)
	}
}

func TestFailedCallIsCatchable(t *testing.T) {
	res := runScript(t, `
try { await tools.github.create_issue({ title: "x" }) } catch (e) { return "caught: " + e.message }`,
		&echo{fail: "create_issue"}, Limits{})
	if res.Error != nil || string(res.Value) != `"caught: the service said no"` {
		t.Fatalf("result = %+v", res)
	}
	if res.Calls[0].OK || res.Calls[0].Error != "the service said no" {
		t.Fatalf("calls = %+v", res.Calls)
	}
}

func TestUncaughtFailureSaysWhere(t *testing.T) {
	res := runScript(t, "const a = 1\nawait tools.github.create_issue({ title: 'x' })", &echo{fail: "create_issue"}, Limits{})
	if res.Error == nil || res.Error.Message != "the service said no" {
		t.Fatalf("error = %+v", res.Error)
	}
}

func TestUnknownToolAndPlugin(t *testing.T) {
	res := runScript(t, "\n\nawait tools.github.delete_repo({})", &echo{}, Limits{})
	if res.Error == nil || res.Error.Kind != KindUnknownTool || !strings.Contains(res.Error.Message, `search("delete_repo"`) {
		t.Fatalf("error = %+v", res.Error)
	}
	if res.Error.Line != 3 {
		t.Fatalf("line = %d, want 3", res.Error.Line)
	}
	res = runScript(t, "await tools.jira.get({})", &echo{}, Limits{})
	if res.Error == nil || res.Error.Kind != KindUnknownTool || !strings.Contains(res.Error.Message, `no plugin "jira"`) {
		t.Fatalf("error = %+v", res.Error)
	}
}

func TestSyntaxErrorLine(t *testing.T) {
	res := runScript(t, "const a = 1\nconst = 2", &echo{}, Limits{})
	if res.Error == nil || res.Error.Kind != KindSyntax || res.Error.Line != 2 {
		t.Fatalf("error = %+v", res.Error)
	}
}

func TestSearch(t *testing.T) {
	res := runScript(t, `return search("issue", { plugin: "github" }).map((f) => f.path)`, &echo{}, Limits{})
	if res.Error != nil || string(res.Value) != `["tools.github.create_issue","tools.github.get_issue"]` {
		t.Fatalf("result = %+v %s", res.Error, res.Value)
	}
	found := Search(testTools, "contact", "", 0)
	if len(found) != 1 || found[0].Path != `tools["my-crm"]["find-contact"]` || !found[0].ReadOnly {
		t.Fatalf("found = %+v", found)
	}
}

func TestCallLimit(t *testing.T) {
	res := runScript(t, `for (let i = 0; i < 5; i++) await tools.github.get_issue({ owner: "o", repo: "r", number: i })`, &echo{}, Limits{MaxCalls: 3})
	if res.Error == nil || res.Error.Kind != KindCallLimit || len(res.Calls) != 3 {
		t.Fatalf("result = %+v", res)
	}
}

func TestTimeoutStopsLoopsAndCalls(t *testing.T) {
	started := time.Now()
	res := runScript(t, `while (true) {}`, &echo{}, Limits{Timeout: 200 * time.Millisecond})
	if res.Error == nil || res.Error.Kind != KindTimeout {
		t.Fatalf("error = %+v", res.Error)
	}
	res = runScript(t, `await tools.github.get_issue({ owner: "o", repo: "r", number: 1 })`, &echo{delay: time.Minute}, Limits{Timeout: 200 * time.Millisecond})
	if res.Error == nil || res.Error.Kind != KindTimeout {
		t.Fatalf("error = %+v", res.Error)
	}
	if time.Since(started) > 5*time.Second {
		t.Fatal("timeouts took too long")
	}
}

func TestPausedTimeIsNotCounted(t *testing.T) {
	res := runScript(t, `return (await tools.github.get_issue({ owner: "o", repo: "r", number: 1 })).seq`,
		&echo{pause: 500 * time.Millisecond}, Limits{Timeout: 300 * time.Millisecond})
	if res.Error != nil || string(res.Value) != "1" {
		t.Fatalf("result = %+v %s", res.Error, res.Value)
	}
}

func TestMemoryLimit(t *testing.T) {
	res := runScript(t, `let s = "x"; while (true) { s = s + s }`, &echo{}, Limits{MaxMemory: 64 << 20, Timeout: 30 * time.Second})
	if res.Error == nil || (res.Error.Kind != KindMemory && !strings.Contains(res.Error.Message, "length")) {
		t.Fatalf("error = %+v", res.Error)
	}
}

func TestNeverSettles(t *testing.T) {
	res := runScript(t, `await new Promise(() => {})`, &echo{}, Limits{})
	if res.Error == nil || res.Error.Kind != KindStuck {
		t.Fatalf("error = %+v", res.Error)
	}
}

func TestNoHostGlobals(t *testing.T) {
	res := runScript(t, `return [typeof require, typeof setTimeout, typeof fetch, typeof process, typeof globalThis.tools]`, &echo{}, Limits{})
	if string(res.Value) != `["undefined","undefined","undefined","undefined","object"]` {
		t.Fatalf("value = %s %+v", res.Value, res.Error)
	}
}

func TestOutputIsCut(t *testing.T) {
	res := runScript(t, `console.log("a".repeat(50)); return "b".repeat(100)`, &echo{}, Limits{MaxOutput: 60})
	if !res.Truncated || len(res.Logs) != 0 || !strings.Contains(string(res.Value), "[cut: the value has 100 characters") {
		t.Fatalf("result = %+v %s", res, res.Value)
	}
}

func TestSignatureAndCatalog(t *testing.T) {
	sig := testTools[0].Signature()
	want := `tools.github.get_issue(input: { number: number; owner: string; repo: string; state?: "open" | "closed" }): Promise<unknown>`
	if sig != want {
		t.Fatalf("signature =\n%s\nwant\n%s", sig, want)
	}
	namespaces := []Namespace{{Name: "github", Title: "GitHub", Description: "Repositories"}, {Name: "my-crm", Title: "CRM"}}
	full := Catalog(namespaces, testTools, CatalogBudget)
	if !strings.Contains(full, "- tools.github (GitHub, 2 tools): Repositories\n  - tools.github.create_issue(") ||
		!strings.Contains(full, `// Get an issue.`) || strings.Contains(full, "More words") || strings.Contains(full, "search()") {
		t.Fatalf("catalog =\n%s", full)
	}
	tight := Catalog(namespaces, testTools, 120)
	if !strings.Contains(tight, `- tools["my-crm"] (CRM, 1 tools)`) || !strings.Contains(tight, "of 3 tools are listed") {
		t.Fatalf("catalog =\n%s", tight)
	}
	// Read-only tools are listed first when room is short.
	if strings.Contains(tight, "create_issue") && !strings.Contains(tight, "get_issue") {
		t.Fatalf("catalog =\n%s", tight)
	}
}

func TestValidate(t *testing.T) {
	if err := Validate(issueSchema, json.RawMessage(`{"owner":"o","repo":"r","number":1}`)); err != nil {
		t.Fatal(err)
	}
	if err := Validate(issueSchema, json.RawMessage(`{"owner":"o","repo":"r"}`)); err == nil || !strings.Contains(err.Error(), "number") {
		t.Fatalf("err = %v", err)
	}
	if err := Validate(json.RawMessage(`{"type":"object","properties":{"a":{"$ref":"#/nowhere"}}}`), json.RawMessage(`{}`)); err != nil {
		t.Fatalf("an unreadable schema held the call: %v", err)
	}
}
