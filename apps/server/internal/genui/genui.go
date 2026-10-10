// Package genui tells a model how to answer with interface components.
//
// A model that is allowed to may put an `openchat-ui` fenced block in its
// Markdown answer. The block holds declarations in OpenChat's UI language, which
// the web app parses as it streams and renders with the chat's own components.
// Nothing in a block is executed: it names components from a fixed catalogue
// and gives them values, and anything outside the catalogue is dropped.
//
// The catalogue is api/genui-catalog.json, copied here by `make generate-genui`.
// The same file is what the web app validates against, so the components the
// model is told about are exactly the components the page can draw.
package genui

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"
)

//go:embed catalog.gen.json
var catalogJSON []byte

// Catalog is the component catalogue.
type Catalog struct {
	Version    int         `json:"version"`
	Components []Component `json:"components"`
}

type Component struct {
	Name        string  `json:"name"`
	Description string  `json:"description"`
	Params      []Param `json:"params"`
	// Retired components are no longer taught to the model, but the page still
	// draws them, so a transcript that used one keeps rendering.
	Retired bool `json:"retired"`
}

type Param struct {
	Name     string   `json:"name"`
	Type     string   `json:"type"`
	Required bool     `json:"required"`
	Values   []string `json:"values"`
	Of       []string `json:"of"`
	Min      *int     `json:"min"`
	Max      *int     `json:"max"`
	Default  any      `json:"default"`
}

// Load reads the embedded catalogue. It is generated, so a failure is a
// build problem, not a runtime condition.
func Load() Catalog {
	var catalog Catalog
	if err := json.Unmarshal(catalogJSON, &catalog); err != nil {
		panic(fmt.Sprintf("genui: the embedded catalogue does not parse: %v", err))
	}
	return catalog
}

// typeNames is how each parameter type is written in a signature.
var typeNames = map[string]string{
	"string":   "string",
	"markdown": "markdown string",
	"integer":  "integer",
	"number":   "number",
	"boolean":  "boolean",
	"strings":  "[string, ...]",
	"numbers":  "[number, ...]",
	"cells":    "[[cell, ...], ...] where a cell is a string or a number",
	"pairs":    `[["label", "value"], ...]`,
	"node":     "component",
	"nodes":    "[component, ...]",
}

// Prompt is the system prompt section that teaches the language and the
// catalogue. It is stable — it changes only with the catalogue — so it does not
// disturb a provider's prompt cache.
func (c Catalog) Prompt() string {
	var b strings.Builder
	b.WriteString(`## Interface components

When structure would make part of an answer clearer — a few key figures, a
chart, a plan, a specification — you may render that part as interface
components instead of Markdown. A table is not one of them: write tables as
ordinary Markdown tables, outside any block. Put them in a fenced code block whose
language is openchat-ui.

Write the answer as prose and drop a block in exactly where its content belongs,
the way a figure sits in an article: a sentence or a heading, then the block,
then the prose carries on. Several small blocks spread through an answer are
better than one large block. Keep each block to one idea — a row of figures, one
chart, one plan — and do not wrap the whole answer, or several unrelated things,
in one Card. Use Card only to group a few things that belong together, such as a
price with its breakdown. Do not repeat in prose what a block already shows, and
never mention the block, its syntax or "openchat-ui" to the reader. Use none for
a short or conversational reply.

Inside the block, write one declaration per line:

    name = Component(arg, arg, key: value)

- The first declaration is what is shown; usually name it root.
- Arguments go in the order listed below; optional ones may be given by key
  instead, in any order, after the positional ones.
- Values are JSON literals ("text", 42, true, null, [..]) or names of other
  declarations. A name may be used before it is declared: declare the outline
  first and the details after, so the layout appears while you write.
- Strings use double quotes and JSON escapes; never break a string across lines.
- Only the components below exist. Do not invent components, parameters or values.

Components:
`)
	for _, component := range c.Components {
		if component.Retired {
			continue
		}
		b.WriteString("\n")
		b.WriteString(signature(component))
		b.WriteString("\n    ")
		b.WriteString(component.Description)
		b.WriteString("\n")
		for _, param := range component.Params {
			if detail := paramDetail(param); detail != "" {
				fmt.Fprintf(&b, "    %s: %s\n", param.Name, detail)
			}
		}
	}

	b.WriteString(`
Example — prose with two small blocks where they belong:

The launch is on track overall; QA needs one more week.

` + "```openchat-ui" + `
root = Grid([s1, s2, s3], columns: 3)
s1 = Stat("Open bugs", "14", change: "-9 this week", trend: "down", tone: "good")
s2 = Stat("Test coverage", "82%", change: "+4%", trend: "up", tone: "good")
s3 = Stat("Days to launch", "12")
` + "```" + `

Regression testing is the last open item:

` + "```openchat-ui" + `
root = Steps([p1, p2, p3])
p1 = Step("Freeze features", status: "done")
p2 = Step("Finish regression testing", detail: "Payments and onboarding remain.", status: "current")
p3 = Step("Ship to production")
` + "```" + `
`)
	return b.String()
}

func signature(component Component) string {
	args := make([]string, 0, len(component.Params))
	for _, param := range component.Params {
		arg := param.Name
		if !param.Required {
			arg += "?"
		}
		args = append(args, arg)
	}
	return fmt.Sprintf("%s(%s)", component.Name, strings.Join(args, ", "))
}

func paramDetail(param Param) string {
	var parts []string
	switch {
	case param.Type == "enum":
		quoted := make([]string, 0, len(param.Values))
		for _, value := range param.Values {
			quoted = append(quoted, `"`+value+`"`)
		}
		parts = append(parts, "one of "+strings.Join(quoted, ", "))
	case param.Type == "nodes" && len(param.Of) > 0:
		parts = append(parts, "["+strings.Join(param.Of, ", ")+", ...]")
	default:
		parts = append(parts, typeNames[param.Type])
	}
	if param.Min != nil && param.Max != nil {
		parts = append(parts, fmt.Sprintf("%d to %d", *param.Min, *param.Max))
	}
	if param.Default != nil {
		parts = append(parts, fmt.Sprintf("default %v", param.Default))
	}
	return strings.Join(parts, ", ")
}
