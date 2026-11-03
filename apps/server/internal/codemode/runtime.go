package codemode

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"runtime/metrics"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/dop251/goja"
)

// Host lists the tools a script may call and carries out each call.
type Host interface {
	// Call runs one call. Its error's message is what the script catches, so
	// it must be fit for the model to read.
	Call(ctx context.Context, call Call) (any, error)
}

// Call is one call a script made.
type Call struct {
	// Seq numbers the script's calls from 1, in the order they were made.
	Seq   int
	Tool  Tool
	Input json.RawMessage
}

// Limits bound one script. Zero values take the defaults.
type Limits struct {
	// Timeout is the script's active time: what the host spends waiting for
	// the person (Pause) is not counted.
	Timeout time.Duration
	// MaxCalls counts tool calls and searches.
	MaxCalls int
	// MaxConcurrent calls run at once; the rest wait their turn.
	MaxConcurrent int
	// MaxOutput is how many characters of result and logs are kept.
	MaxOutput int
	// MaxMemory is how far the process's heap may grow while the script runs
	// before it is stopped: goja has no memory limit of its own, and a script
	// doubling a string thirty times would take the server down.
	MaxMemory uint64
}

func (l Limits) withDefaults() Limits {
	if l.Timeout <= 0 {
		l.Timeout = 120 * time.Second
	}
	if l.MaxCalls <= 0 {
		l.MaxCalls = 50
	}
	if l.MaxConcurrent <= 0 {
		l.MaxConcurrent = 8
	}
	if l.MaxOutput <= 0 {
		l.MaxOutput = 20_000
	}
	if l.MaxMemory == 0 {
		l.MaxMemory = 512 << 20
	}
	return l
}

// Diagnostic kinds, as the model reads them.
const (
	KindSyntax       = "SyntaxError"
	KindUnknownTool  = "UnknownTool"
	KindCallLimit    = "ToolCallLimitExceeded"
	KindTimeout      = "TimeoutExceeded"
	KindMemory       = "MemoryLimitExceeded"
	KindExecution    = "ExecutionFailure"
	KindInvalidValue = "InvalidDataValue"
	KindStuck        = "NeverSettled"
)

// Diagnostic is why a script failed, with where when it is known.
type Diagnostic struct {
	Kind    string `json:"kind"`
	Message string `json:"message"`
	Line    int    `json:"line,omitempty"`
	Column  int    `json:"column,omitempty"`
}

// CallRecord is one call as the result lists it.
type CallRecord struct {
	Seq   int    `json:"seq"`
	Path  string `json:"path"`
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

// SearchRecord is one search() a script made.
type SearchRecord struct {
	Query     string `json:"query"`
	Namespace string `json:"namespace,omitempty"`
	Found     int    `json:"found"`
}

// Result is how a script ended.
type Result struct {
	// Value is what it returned, as JSON; null when nothing.
	Value json.RawMessage `json:"value,omitempty"`
	Logs  []string        `json:"logs,omitempty"`
	Calls []CallRecord    `json:"calls"`
	// Searches are the script's search() calls, in order.
	Searches  []SearchRecord `json:"searches,omitempty"`
	Error     *Diagnostic    `json:"error,omitempty"`
	Truncated bool           `json:"truncated,omitempty"`
}

type pauseKey struct{}

// Pause stops the script's clock until resume is called: the host is waiting
// for the person, not working. Outside a script it does nothing.
func Pause(ctx context.Context) (resume func()) {
	c, ok := ctx.Value(pauseKey{}).(*clock)
	if !ok {
		return func() {}
	}
	c.pause()
	var once sync.Once
	return func() { once.Do(c.resume) }
}

// clock measures a script's active time.
type clock struct {
	mu     sync.Mutex
	used   time.Duration
	since  time.Time
	paused int
}

func (c *clock) pause() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.paused == 0 {
		c.used += time.Since(c.since)
	}
	c.paused++
}

func (c *clock) resume() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.paused--
	if c.paused == 0 {
		c.since = time.Now()
	}
}

func (c *clock) elapsed() time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.paused > 0 {
		return c.used
	}
	return c.used + time.Since(c.since)
}

// lineOffset is the line the wrapper adds before the script's first.
const lineOffset = 1

// Run runs a script against the tools until it settles, fails or runs out of
// time. ctx ending stops it; its calls get a ctx that ends with the script.
func Run(ctx context.Context, code string, tools []Tool, host Host, limits Limits) Result {
	limits = limits.withDefaults()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	clk := &clock{since: time.Now()}
	ctx = context.WithValue(ctx, pauseKey{}, clk)

	r := &run{
		ctx: ctx, host: host, limits: limits, tools: tools,
		vm:      goja.New(),
		settled: make(chan func(), limits.MaxCalls+1),
		slots:   make(chan struct{}, limits.MaxConcurrent),
	}
	r.vm.SetMaxCallStackSize(512)
	r.vm.SetFieldNameMapper(goja.TagFieldNameMapper("json", true))
	if err := r.install(); err != nil {
		return Result{Error: &Diagnostic{Kind: KindExecution, Message: err.Error()}}
	}

	// The watchdog stops a script that runs too long or grows the heap too
	// far, from outside the VM's goroutine (goja's Interrupt is safe there).
	stop := make(chan struct{})
	defer close(stop)
	var why stopReason
	var whyMu sync.Mutex
	halt := func(reason stopReason) {
		whyMu.Lock()
		if why == "" {
			why = reason
		}
		whyMu.Unlock()
		r.vm.Interrupt(reason)
		cancel()
	}
	base := heapBytes()
	go func() {
		tick := time.NewTicker(50 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ctx.Done():
				return
			case <-tick.C:
				if clk.elapsed() > limits.Timeout {
					halt(stopTimeout)
					return
				}
				if heap := heapBytes(); heap > base && heap-base > limits.MaxMemory {
					halt(stopMemory)
					return
				}
			}
		}
	}()

	result := r.execute(code)
	whyMu.Lock()
	reason := why
	whyMu.Unlock()
	switch reason {
	case stopTimeout:
		result.Error = &Diagnostic{Kind: KindTimeout, Message: fmt.Sprintf("the script ran for more than %s; do less in one script, or split it", limits.Timeout)}
		result.Value = nil
	case stopMemory:
		result.Error = &Diagnostic{Kind: KindMemory, Message: "the script used too much memory and was stopped"}
		result.Value = nil
	}
	return r.finish(result)
}

type stopReason string

const (
	stopTimeout stopReason = "timeout"
	stopMemory  stopReason = "memory"
)

func heapBytes() uint64 {
	sample := []metrics.Sample{{Name: "/memory/classes/heap/objects:bytes"}}
	metrics.Read(sample)
	if sample[0].Value.Kind() != metrics.KindUint64 {
		return 0
	}
	return sample[0].Value.Uint64()
}

type run struct {
	ctx    context.Context
	host   Host
	limits Limits
	tools  []Tool
	vm     *goja.Runtime

	// settled carries what a finished call does to the VM, run on the VM's
	// goroutine (goja is not safe for concurrent use).
	settled  chan func()
	slots    chan struct{}
	pending  int
	calls    int
	records  []CallRecord
	searches []SearchRecord
	logs     []string
	logSize  int
}

// install gives the script its globals: tools, search and console.
func (r *run) install() error {
	byNS := map[string]map[string]Tool{}
	for _, t := range r.tools {
		if byNS[t.Namespace] == nil {
			byNS[t.Namespace] = map[string]Tool{}
		}
		byNS[t.Namespace][t.Name] = t
	}
	namespaces := map[string]goja.Value{}
	for ns, tools := range byNS {
		obj := r.vm.NewObject()
		for name, tool := range tools {
			if err := obj.Set(name, r.callable(tool)); err != nil {
				return err
			}
		}
		namespaces[ns] = r.guard(obj, func(property string) string {
			return fmt.Sprintf("%s has no tool %q; find tools with search(%q, { plugin: %q })", "tools"+member(ns), property, property, ns)
		})
	}
	root := r.vm.NewObject()
	for ns, v := range namespaces {
		if err := root.Set(ns, v); err != nil {
			return err
		}
	}
	if err := r.vm.Set("tools", r.guard(root, func(property string) string {
		return fmt.Sprintf("no plugin %q among the tools; the catalogue lists the plugins, and search() finds their tools", property)
	})); err != nil {
		return err
	}
	if err := r.vm.Set("search", r.search); err != nil {
		return err
	}
	console := r.vm.NewObject()
	for _, name := range []string{"log", "info", "warn", "error", "debug"} {
		if err := console.Set(name, r.log); err != nil {
			return err
		}
	}
	return r.vm.Set("console", console)
}

// guard makes reading a name that is not there an error that says what to
// do, instead of `undefined is not a function`.
func (r *run) guard(target *goja.Object, missing func(string) string) goja.Value {
	return r.vm.ToValue(r.vm.NewProxy(target, &goja.ProxyTrapConfig{
		Get: func(t *goja.Object, property string, _ goja.Value) goja.Value {
			if v := t.Get(property); v != nil {
				return v
			}
			// Awaiting or printing an object reads these; they are simply absent.
			switch property {
			case "then", "toJSON", "constructor", "toString", "valueOf", "inspect":
				return goja.Undefined()
			}
			panic(r.vm.NewTypeError(missing(property)))
		},
	}))
}

// callable is one tool as a script calls it: a function returning a promise.
func (r *run) callable(tool Tool) func(goja.FunctionCall) goja.Value {
	return func(call goja.FunctionCall) goja.Value {
		r.calls++
		if r.calls > r.limits.MaxCalls {
			panic(r.vm.NewTypeError(fmt.Sprintf("%s: more than %d tool calls in one script", KindCallLimit, r.limits.MaxCalls)))
		}
		input := "{}"
		if arg := call.Argument(0); !goja.IsUndefined(arg) && !goja.IsNull(arg) {
			s, err := r.stringify(arg)
			if err != nil {
				panic(r.vm.NewTypeError(fmt.Sprintf("%s: the input of %s could not be made JSON: %v", KindInvalidValue, tool.Path(), err)))
			}
			input = s
		}
		var obj map[string]any
		if json.Unmarshal([]byte(input), &obj) != nil {
			panic(r.vm.NewTypeError(fmt.Sprintf("%s takes one object as its input", tool.Path())))
		}
		seq := r.calls
		promise, resolve, reject := r.vm.NewPromise()
		r.pending++
		r.records = append(r.records, CallRecord{Seq: seq, Path: tool.Path()})
		go func() {
			var (
				value any
				err   error
			)
			select {
			case r.slots <- struct{}{}:
				value, err = r.host.Call(r.ctx, Call{Seq: seq, Tool: tool, Input: json.RawMessage(input)})
				<-r.slots
			case <-r.ctx.Done():
				err = errors.New("the script ended before this call ran")
			}
			r.settled <- func() {
				r.pending--
				rec := &r.records[seq-1]
				if err != nil {
					rec.Error = err.Error()
					_ = reject(r.newError(err.Error()))
					return
				}
				rec.OK = true
				v, perr := r.parse(value)
				if perr != nil {
					rec.OK, rec.Error = false, perr.Error()
					_ = reject(r.newError(perr.Error()))
					return
				}
				_ = resolve(v)
			}
		}()
		return r.vm.ToValue(promise)
	}
}

// search(query, { plugin, limit }) finds tools. It counts as a call.
func (r *run) search(call goja.FunctionCall) goja.Value {
	r.calls++
	if r.calls > r.limits.MaxCalls {
		panic(r.vm.NewTypeError(fmt.Sprintf("%s: more than %d tool calls in one script", KindCallLimit, r.limits.MaxCalls)))
	}
	query := ""
	if q := call.Argument(0); !goja.IsUndefined(q) && !goja.IsNull(q) {
		query = q.String()
	}
	namespace, limit := "", 0
	if opts, ok := call.Argument(1).(*goja.Object); ok {
		if v := opts.Get("plugin"); v != nil && !goja.IsUndefined(v) {
			namespace = v.String()
		}
		if v := opts.Get("limit"); v != nil && !goja.IsUndefined(v) {
			limit = int(v.ToInteger())
		}
	}
	found := Search(r.tools, query, namespace, limit)
	r.searches = append(r.searches, SearchRecord{Query: query, Namespace: namespace, Found: len(found)})
	v, err := r.parse(found)
	if err != nil {
		panic(r.vm.NewGoError(err))
	}
	return v
}

func (r *run) log(call goja.FunctionCall) goja.Value {
	parts := make([]string, 0, len(call.Arguments))
	for _, arg := range call.Arguments {
		if s, ok := arg.Export().(string); ok {
			parts = append(parts, s)
			continue
		}
		s, err := r.stringify(arg)
		if err != nil {
			s = arg.String()
		}
		parts = append(parts, s)
	}
	line := strings.Join(parts, " ")
	if r.logSize < r.limits.MaxOutput {
		r.logs = append(r.logs, line)
	}
	r.logSize += utf8.RuneCountInString(line)
	return goja.Undefined()
}

func (r *run) stringify(v goja.Value) (string, error) {
	stringify, _ := goja.AssertFunction(r.vm.Get("JSON").ToObject(r.vm).Get("stringify"))
	out, err := stringify(goja.Undefined(), v)
	if err != nil {
		return "", err
	}
	if goja.IsUndefined(out) {
		return "null", nil
	}
	return out.String(), nil
}

// parse makes a Go value the script's own plain data.
func (r *run) parse(value any) (goja.Value, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("the result could not be read: %v", err)
	}
	parse, _ := goja.AssertFunction(r.vm.Get("JSON").ToObject(r.vm).Get("parse"))
	return parse(goja.Undefined(), r.vm.ToValue(string(raw)))
}

func (r *run) newError(message string) goja.Value {
	ctor := r.vm.Get("Error")
	obj, err := r.vm.New(ctor, r.vm.ToValue(message))
	if err != nil {
		return r.vm.ToValue(message)
	}
	return obj
}

// execute runs the script and its calls to the end.
func (r *run) execute(code string) Result {
	wrapped := "(async () => {\n" + code + "\n})()"
	program, err := goja.Compile("script.js", wrapped, true)
	if err != nil {
		return Result{Error: syntaxDiagnostic(err)}
	}
	v, err := r.vm.RunProgram(program)
	if err != nil {
		return Result{Error: r.thrown(err)}
	}
	promise, ok := v.Export().(*goja.Promise)
	if !ok {
		return Result{Error: &Diagnostic{Kind: KindExecution, Message: "the script did not run"}}
	}
	for promise.State() == goja.PromiseStatePending {
		if r.pending == 0 {
			return Result{Error: &Diagnostic{Kind: KindStuck, Message: "the script waits on something that never finishes (only tool calls settle; there are no timers)"}}
		}
		select {
		case apply := <-r.settled:
			if err := r.guarded(apply); err != nil {
				return Result{Error: r.thrown(err)}
			}
		case <-r.ctx.Done():
			return Result{Error: &Diagnostic{Kind: KindExecution, Message: "the script was stopped"}}
		}
	}
	if promise.State() == goja.PromiseStateRejected {
		return Result{Error: r.rejection(promise.Result())}
	}
	out := promise.Result()
	if goja.IsUndefined(out) {
		return Result{Value: json.RawMessage("null")}
	}
	s, err := r.stringify(out)
	if err != nil {
		return Result{Error: &Diagnostic{Kind: KindInvalidValue, Message: fmt.Sprintf("the returned value could not be made JSON: %v", err)}}
	}
	if depth(json.RawMessage(s)) > 32 {
		return Result{Error: &Diagnostic{Kind: KindInvalidValue, Message: "the returned value is nested more than 32 levels deep"}}
	}
	return Result{Value: json.RawMessage(s)}
}

// guarded runs a settlement on the VM, turning an interrupt into an error.
func (r *run) guarded(apply func()) (err error) {
	defer func() {
		if p := recover(); p != nil {
			if e, ok := p.(error); ok {
				err = e
				return
			}
			err = fmt.Errorf("%v", p)
		}
	}()
	apply()
	return nil
}

func (r *run) thrown(err error) *Diagnostic {
	var ex *goja.Exception
	if errors.As(err, &ex) {
		d := r.rejection(ex.Value())
		if d.Line == 0 {
			for _, frame := range ex.Stack() {
				if pos := frame.Position(); pos.Line > 0 {
					d.Line, d.Column = pos.Line-lineOffset, pos.Column
					break
				}
			}
		}
		return d
	}
	var interrupted *goja.InterruptedError
	if errors.As(err, &interrupted) {
		return &Diagnostic{Kind: KindExecution, Message: "the script was stopped"}
	}
	return &Diagnostic{Kind: KindExecution, Message: err.Error()}
}

// rejection describes what a script threw: an Error's message and line, or
// the value itself.
func (r *run) rejection(v goja.Value) *Diagnostic {
	d := &Diagnostic{Kind: KindExecution}
	obj, ok := v.(*goja.Object)
	if !ok {
		d.Message = "the script threw " + v.String()
		return d
	}
	if msg := obj.Get("message"); msg != nil && !goja.IsUndefined(msg) {
		d.Message = msg.String()
		if name := obj.Get("name"); name != nil && name.String() != "Error" {
			d.Message = name.String() + ": " + d.Message
		}
	} else {
		d.Message = "the script threw " + v.String()
	}
	for _, kind := range []string{KindCallLimit, KindInvalidValue} {
		if strings.Contains(d.Message, kind+":") {
			d.Kind = kind
		}
	}
	if strings.Contains(d.Message, "has no tool") || strings.Contains(d.Message, "no plugin") {
		d.Kind = KindUnknownTool
	}
	if stack := obj.Get("stack"); stack != nil {
		d.Line, d.Column = scriptPosition(stack.String())
	}
	return d
}

// scriptPosition finds the first `script.js:line:column` in a stack.
func scriptPosition(stack string) (int, int) {
	i := strings.Index(stack, "script.js:")
	if i < 0 {
		return 0, 0
	}
	var line, col int
	if _, err := fmt.Sscanf(stack[i+len("script.js:"):], "%d:%d", &line, &col); err != nil || line <= lineOffset {
		return 0, 0
	}
	return line - lineOffset, col
}

// syntaxAt reads goja's `script.js: Line 3:7 Unexpected token = (and 2 more errors)`.
var syntaxAt = regexp.MustCompile(`Line (\d+):(\d+) (.*?)(?: \(and \d+ more errors?\))?$`)

func syntaxDiagnostic(err error) *Diagnostic {
	d := &Diagnostic{Kind: KindSyntax, Message: err.Error()}
	if m := syntaxAt.FindStringSubmatch(err.Error()); m != nil {
		line, _ := strconv.Atoi(m[1])
		d.Column, _ = strconv.Atoi(m[2])
		d.Line, d.Message = line-lineOffset, m[3]
	}
	return d
}

func depth(raw json.RawMessage) int {
	max, cur := 0, 0
	inString, escaped := false, false
	for _, c := range raw {
		switch {
		case escaped:
			escaped = false
		case inString && c == '\\':
			escaped = true
		case c == '"':
			inString = !inString
		case inString:
		case c == '{' || c == '[':
			cur++
			if cur > max {
				max = cur
			}
		case c == '}' || c == ']':
			cur--
		}
	}
	return max
}

// finish cuts what the model will read to the output limit.
func (r *run) finish(result Result) Result {
	result.Searches = r.searches
	result.Calls = r.records
	if result.Calls == nil {
		result.Calls = []CallRecord{}
	}
	result.Logs = r.logs
	budget := r.limits.MaxOutput
	if n := utf8.RuneCount(result.Value); n > budget {
		// A string is cut as text; anything else as its JSON.
		text := string(result.Value)
		var s string
		if json.Unmarshal(result.Value, &s) == nil {
			text, n = s, utf8.RuneCountInString(s)
		}
		cut := []rune(text)[:min(budget, len([]rune(text)))]
		marked, _ := json.Marshal(string(cut) + fmt.Sprintf(" ... [cut: the value has %d characters; return less, or pick out what you need]", n))
		result.Value = marked
		result.Truncated = true
		budget = 0
	} else {
		budget -= n
	}
	kept := result.Logs[:0]
	for _, line := range result.Logs {
		n := utf8.RuneCountInString(line)
		if n > budget {
			result.Truncated = true
			break
		}
		budget -= n
		kept = append(kept, line)
	}
	result.Logs = kept
	if r.logSize > 0 && len(kept) < len(r.logs) {
		result.Truncated = true
	}
	return result
}
