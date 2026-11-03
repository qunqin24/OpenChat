// Package codemode runs the scripts a model writes to call plugin tools
// (docs/design/plugins-plan.md §6), after OpenCode's Code Mode: instead of one
// tool definition per plugin tool, the model gets one tool that runs
// JavaScript, a catalogue of the tools in its system prompt, and search() for
// the rest. The interpreter is goja, given nothing of the host but the tools.
//
// The package knows nothing of plugins, people or databases: a Host lists the
// tools and carries out each call.
package codemode

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/google/jsonschema-go/jsonschema"
)

// Tool is one tool a script may call, as tools.<Namespace>.<Name>(input).
type Tool struct {
	Namespace string
	Name      string
	// Description is the service's; the catalogue shows its first line.
	Description string
	// Schema is the JSON Schema of its input, an object.
	Schema   json.RawMessage
	ReadOnly bool
}

// Path is how a script calls the tool.
func (t Tool) Path() string { return "tools" + member(t.Namespace) + member(t.Name) }

var identifier = regexp.MustCompile(`^[A-Za-z_$][A-Za-z0-9_$]*$`)

func member(name string) string {
	if identifier.MatchString(name) {
		return "." + name
	}
	quoted, _ := json.Marshal(name)
	return "[" + string(quoted) + "]"
}

// Signature is the tool as a TypeScript call: its path, its input's shape.
func (t Tool) Signature() string {
	return t.Path() + "(input: " + typeOf(t.Schema) + "): Promise<unknown>"
}

// typeOf renders a JSON Schema as a TypeScript type, as far as it is useful
// to a model writing a call: properties and which are required, arrays,
// enums and unions. Nesting stops at three levels.
func typeOf(raw json.RawMessage) string {
	var schema map[string]any
	if json.Unmarshal(raw, &schema) != nil {
		return "object"
	}
	return render(schema, 0)
}

func render(s map[string]any, depth int) string {
	if values, ok := s["enum"].([]any); ok && len(values) > 0 {
		parts := make([]string, 0, len(values))
		for _, v := range values {
			b, _ := json.Marshal(v)
			parts = append(parts, string(b))
		}
		return strings.Join(parts, " | ")
	}
	if v, ok := s["const"]; ok {
		b, _ := json.Marshal(v)
		return string(b)
	}
	for _, key := range []string{"anyOf", "oneOf"} {
		if options, ok := s[key].([]any); ok && len(options) > 0 {
			parts := make([]string, 0, len(options))
			for _, o := range options {
				if m, ok := o.(map[string]any); ok {
					parts = append(parts, render(m, depth))
				}
			}
			return strings.Join(parts, " | ")
		}
	}
	kind := s["type"]
	if kinds, ok := kind.([]any); ok {
		parts := make([]string, 0, len(kinds))
		for _, k := range kinds {
			sub := map[string]any{}
			for key, v := range s {
				sub[key] = v
			}
			sub["type"] = k
			parts = append(parts, render(sub, depth))
		}
		return strings.Join(parts, " | ")
	}
	switch kind {
	case "string":
		return "string"
	case "number", "integer":
		return "number"
	case "boolean":
		return "boolean"
	case "null":
		return "null"
	case "array":
		item, _ := s["items"].(map[string]any)
		if item == nil || depth >= 3 {
			return "unknown[]"
		}
		inner := render(item, depth+1)
		if strings.ContainsAny(inner, "|") {
			inner = "(" + inner + ")"
		}
		return inner + "[]"
	}
	props, _ := s["properties"].(map[string]any)
	if props == nil || depth >= 3 {
		if kind == "object" || props != nil {
			return "object"
		}
		return "unknown"
	}
	required := map[string]bool{}
	if names, ok := s["required"].([]any); ok {
		for _, n := range names {
			if name, ok := n.(string); ok {
				required[name] = true
			}
		}
	}
	names := make([]string, 0, len(props))
	for name := range props {
		names = append(names, name)
	}
	// Required first, then by name: stable bytes for the prompt.
	sort.Slice(names, func(i, j int) bool {
		if required[names[i]] != required[names[j]] {
			return required[names[i]]
		}
		return names[i] < names[j]
	})
	fields := make([]string, 0, len(names))
	for _, name := range names {
		sub, _ := props[name].(map[string]any)
		key := name
		if !identifier.MatchString(name) {
			quoted, _ := json.Marshal(name)
			key = string(quoted)
		}
		if !required[name] {
			key += "?"
		}
		fields = append(fields, key+": "+render(sub, depth+1))
	}
	if len(fields) == 0 {
		return "{}"
	}
	return "{ " + strings.Join(fields, "; ") + " }"
}

// firstLine is a description cut to the line and length a listing shows.
func firstLine(description string, limit int) string {
	line, _, _ := strings.Cut(strings.TrimSpace(description), "\n")
	line = strings.TrimSpace(line)
	if utf8.RuneCountInString(line) > limit {
		runes := []rune(line)
		line = strings.TrimSpace(string(runes[:limit-3])) + "..."
	}
	return line
}

// Namespace is one plugin as the catalogue introduces it.
type Namespace struct {
	Name        string
	Title       string
	Description string
}

// CatalogBudget is about how many characters of tool listings the catalogue
// holds: some 2,000 tokens, as OpenCode's.
const CatalogBudget = 8000

const listingLimit = 120

// Catalog renders the tools for the system prompt: every namespace, always,
// with how many tools it has; then as many tool listings as fit the budget,
// one namespace at a time in turn, tools that only read and short listings
// first (OpenCode's catalog.ts). What is left out, search() finds.
func Catalog(namespaces []Namespace, tools []Tool, budget int) string {
	byNS := map[string][]Tool{}
	for _, t := range tools {
		byNS[t.Namespace] = append(byNS[t.Namespace], t)
	}
	type listing struct {
		line     string
		readOnly bool
		path     string
	}
	queues := make([][]listing, len(namespaces))
	for i, ns := range namespaces {
		for _, t := range byNS[ns.Name] {
			line := "  - " + t.Signature()
			if d := firstLine(t.Description, listingLimit); d != "" {
				line += " // " + d
			}
			queues[i] = append(queues[i], listing{line: line, readOnly: t.ReadOnly, path: t.Path()})
		}
		q := queues[i]
		sort.SliceStable(q, func(a, b int) bool {
			if q[a].readOnly != q[b].readOnly {
				return q[a].readOnly
			}
			if len(q[a].line) != len(q[b].line) {
				return len(q[a].line) < len(q[b].line)
			}
			return q[a].path < q[b].path
		})
	}
	chosen := make([][]listing, len(namespaces))
	remaining := budget
	next := make([]int, len(namespaces))
	for progress := true; progress; {
		progress = false
		for i := range namespaces {
			if next[i] >= len(queues[i]) {
				continue
			}
			l := queues[i][next[i]]
			if len(l.line)+1 > remaining {
				next[i] = len(queues[i]) // the rest are no shorter
				continue
			}
			chosen[i] = append(chosen[i], l)
			remaining -= len(l.line) + 1
			next[i]++
			progress = true
		}
	}

	var b strings.Builder
	shown, total := 0, 0
	for i, ns := range namespaces {
		count := len(queues[i])
		total += count
		fmt.Fprintf(&b, "- %s (%s, %d tools)", "tools"+member(ns.Name), ns.Title, count)
		if d := firstLine(ns.Description, listingLimit); d != "" {
			b.WriteString(": " + d)
		}
		b.WriteString("\n")
		list := chosen[i]
		sort.Slice(list, func(a, c int) bool { return list[a].path < list[c].path })
		for _, l := range list {
			b.WriteString(l.line + "\n")
		}
		shown += len(list)
	}
	if shown < total {
		fmt.Fprintf(&b, "%d of %d tools are listed; find the others with search() inside plugin_code.\n", shown, total)
	}
	return strings.TrimRight(b.String(), "\n")
}

// Found is a tool as search() returns it to a script.
type Found struct {
	Path        string `json:"path"`
	Signature   string `json:"signature"`
	Description string `json:"description"`
	ReadOnly    bool   `json:"readOnly"`
}

// searchLimit is how many tools one search returns at most.
const searchLimit = 10

// Search finds tools by words of their path and description, best first.
// An empty query lists a namespace's tools (or all) by path.
func Search(tools []Tool, query, namespace string, limit int) []Found {
	if limit <= 0 || limit > 50 {
		limit = searchLimit
	}
	words := strings.FieldsFunc(strings.ToLower(query), func(r rune) bool {
		return !(r == '_' || r == '-' || r == '.' || r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r > 127)
	})
	type scored struct {
		tool  Tool
		score int
	}
	var hits []scored
	for _, t := range tools {
		if namespace != "" && t.Namespace != namespace {
			continue
		}
		name := strings.ToLower(t.Namespace + " " + t.Name)
		desc := strings.ToLower(t.Description)
		score := 0
		for _, w := range words {
			switch {
			case strings.Contains(name, w):
				score += 3
			case strings.Contains(desc, w):
				score++
			}
		}
		if len(words) > 0 && score == 0 {
			continue
		}
		hits = append(hits, scored{t, score})
	}
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].score != hits[j].score {
			return hits[i].score > hits[j].score
		}
		return hits[i].tool.Path() < hits[j].tool.Path()
	})
	if len(hits) > limit {
		hits = hits[:limit]
	}
	out := make([]Found, len(hits))
	for i, h := range hits {
		out[i] = Found{Path: h.tool.Path(), Signature: h.tool.Signature(), Description: firstLine(h.tool.Description, 400), ReadOnly: h.tool.ReadOnly}
	}
	return out
}

// Validate checks a call's input against the tool's schema, in words a model
// can act on. A schema that cannot be read is not held against the call: the
// service checks its own input.
func Validate(schema, input json.RawMessage) error {
	var s jsonschema.Schema
	if json.Unmarshal(schema, &s) != nil {
		return nil
	}
	resolved, err := s.Resolve(nil)
	if err != nil {
		return nil
	}
	var value any
	if err := json.Unmarshal(input, &value); err != nil {
		return fmt.Errorf("the input is not JSON: %v", err)
	}
	if err := resolved.Validate(value); err != nil {
		return fmt.Errorf("invalid input: %v", err)
	}
	return nil
}
