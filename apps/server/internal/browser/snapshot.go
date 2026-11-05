package browser

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/chromedp/cdproto/accessibility"
	"github.com/chromedp/cdproto/cdp"
)

// A snapshot is the page's accessibility tree as indented text, as Playwright
// MCP gives it: one node a line, `role "name" [state] [ref=e12]`. A ref names
// a node for the actions (click, type...) until the page is snapshotted again.

// SnapshotLimit is how many characters of a snapshot the model gets; past it,
// find and a node's subtree narrow it down.
const SnapshotLimit = 12_000

// roles that only group: printed as their children when nameless.
var flattened = map[string]bool{
	"none": true, "generic": true, "presentation": true, "RootWebArea": true, "InlineTextBox": true,
	"LineBreak": true, "Section": true, "LayoutTable": true, "LayoutTableRow": true, "LayoutTableCell": true,
	"Div": true, "Abbr": true, "Mark": true,
}

// states worth a word on the line, as the AX tree names them.
var shownStates = []string{"checked", "pressed", "selected", "expanded", "disabled", "required", "level", "invalid", "focused"}

// line is one node as printed, with what the page's find needs.
type line struct {
	depth  int
	text   string
	parent int // index of the line above it in the tree, -1 at the top
	node   cdp.BackendNodeID
}

// snapshot renders nodes from the tree's root down. refs maps each ref
// printed to its DOM node; scope is the backend node to start from (0: the
// whole page).
type snapshot struct {
	lines []line
	refs  map[string]cdp.BackendNodeID
}

func render(nodes []*accessibility.Node, scope cdp.BackendNodeID) snapshot {
	byID := make(map[accessibility.NodeID]*accessibility.Node, len(nodes))
	for _, n := range nodes {
		byID[n.NodeID] = n
	}
	out := snapshot{refs: map[string]cdp.BackendNodeID{}}
	var root *accessibility.Node
	for _, n := range nodes {
		if scope != 0 && n.BackendDOMNodeID == scope {
			root = n
			break
		}
		if scope == 0 && n.ParentID == "" {
			root = n
			break
		}
	}
	if root == nil {
		return out
	}
	var walk func(n *accessibility.Node, depth, parent int, parentName string)
	walk = func(n *accessibility.Node, depth, parent int, parentName string) {
		role := axString(n.Role)
		name := collapse(axString(n.Name))
		children := func(d, p int, pn string) {
			for _, id := range n.ChildIDs {
				if child, ok := byID[id]; ok {
					walk(child, d, p, pn)
				}
			}
		}
		switch {
		case n.Ignored:
			children(depth, parent, parentName)
			return
		case role == "StaticText":
			// Text a parent's name already says is said once.
			if name != "" && name != parentName {
				out.lines = append(out.lines, line{depth: depth, text: "text: " + quote(name), parent: parent})
			}
			return
		case flattened[role] && name == "":
			children(depth, parent, parentName)
			return
		}
		text := role
		if name != "" {
			text += " " + quote(name)
		}
		if v := collapse(axString(n.Value)); v != "" && role != "link" {
			text += " [value=" + quote(cut(v, 200)) + "]"
		}
		for _, state := range shownStates {
			for _, p := range n.Properties {
				if string(p.Name) != state {
					continue
				}
				switch v := axString(p.Value); {
				case state == "level":
					text += " [level=" + v + "]"
				case v == "true", v == "mixed":
					text += " [" + state + "]"
				}
			}
		}
		if n.BackendDOMNodeID != 0 {
			ref := fmt.Sprintf("e%d", len(out.refs)+1)
			out.refs[ref] = n.BackendDOMNodeID
			text += " [ref=" + ref + "]"
		}
		out.lines = append(out.lines, line{depth: depth, text: text, parent: parent, node: n.BackendDOMNodeID})
		children(depth+1, len(out.lines)-1, name)
	}
	walk(root, 0, -1, "")
	return out
}

// subtree is the lines of one node and those under it, shifted to the top.
func (s snapshot) subtree(node cdp.BackendNodeID) snapshot {
	out := snapshot{refs: s.refs}
	start := -1
	for i, l := range s.lines {
		if l.node == node {
			start = i
			break
		}
	}
	if start < 0 {
		return out
	}
	top := s.lines[start].depth
	for i := start; i < len(s.lines); i++ {
		l := s.lines[i]
		if i > start && l.depth <= top {
			break
		}
		l.depth -= top
		out.lines = append(out.lines, l)
	}
	return out
}

// String is the snapshot as the model reads it, cut to limit characters.
func (s snapshot) String(limit int) (string, bool) {
	var b strings.Builder
	for _, l := range s.lines {
		row := strings.Repeat("  ", l.depth) + "- " + l.text + "\n"
		if b.Len()+len(row) > limit {
			return b.String(), true
		}
		b.WriteString(row)
	}
	return b.String(), false
}

// find is the lines that contain text, each under the path of lines above it,
// as Playwright MCP's browser_find returns them; at most max matches.
func (s snapshot) find(text string, max int) (string, int) {
	needle := strings.ToLower(text)
	keep := map[int]bool{}
	matches := 0
	for i, l := range s.lines {
		if !strings.Contains(strings.ToLower(l.text), needle) {
			continue
		}
		matches++
		if matches > max {
			continue
		}
		for j := i; j >= 0; j = s.lines[j].parent {
			keep[j] = true
		}
	}
	var b strings.Builder
	last := -1
	for i, l := range s.lines {
		if !keep[i] {
			continue
		}
		if last >= 0 && i != last+1 {
			b.WriteString(strings.Repeat("  ", l.depth) + "...\n")
		}
		b.WriteString(strings.Repeat("  ", l.depth) + "- " + l.text + "\n")
		last = i
	}
	return b.String(), matches
}

func axString(v *accessibility.Value) string {
	if v == nil || len(v.Value) == 0 {
		return ""
	}
	var any any
	if json.Unmarshal(v.Value, &any) != nil {
		return ""
	}
	switch x := any.(type) {
	case string:
		return x
	case bool:
		if x {
			return "true"
		}
		return "false"
	case float64:
		return fmt.Sprint(x)
	}
	return ""
}

func collapse(s string) string { return strings.Join(strings.Fields(s), " ") }

func quote(s string) string { return strconv.Quote(cut(s, 300)) }

func cut(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n-1]) + "…"
}
