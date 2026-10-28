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
	// Pictures is the rule for every picture a component shows: described
	// for the server to find when image search is on, an address a source
	// listed when it is off.
	Pictures struct {
		Search  string `json:"search"`
		Sources string `json:"sources"`
	} `json:"pictures"`
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
	// Retired parameters are no longer taught to the model, but the page still
	// reads them, so a transcript that gave one keeps rendering.
	Retired bool `json:"retired"`
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
// catalogue, with the pictures rule for image search on or off. It is stable —
// it changes only with the catalogue and that switch — so it does not disturb a
// provider's prompt cache.
func (c Catalog) Prompt(imageSearch bool) string {
	var b strings.Builder
	b.WriteString(`## Interface components

You may render parts of an answer as interface components, in a fenced code
block whose language is openchat-ui. Prose is the default. A component earns
its place only when the reader takes the content in, or acts on it, faster
than they would the same thing as prose: figures to compare, a trend, a
procedure to follow, options to choose between, what something looks like.
Whether to use one, and which, is your judgement from the question, the
conversation and the content; that a component exists is never a reason to
use it.

Which kind of content calls for what:
- What something looks like - an animal, a dish, a place, a device, an app's
  screen: a picture (Image; Gallery for a few views to compare).
- Numbers to compare or a trend to see: a few Stats for the key figures, a
  Chart when the shape of three or more values matters.
- Something the reader does in order: Steps, with a picture per step when
  seeing the step helps.
- Options to browse and choose between: Cards in a Grid, with pictures in a
  Carousel; several items compared on the same attributes: a Markdown table.
- Dated events, a schedule, an itinerary: a Timeline.
- One thing's specification or details: KeyValues.
- Concepts, explanations, code, maths, simple facts and opinions: prose and
  code, no components.

For example:
- "What does this fruit look like?" - an Image.
- "How do I clean this device?" - Steps, pictures on each step.
- "How did our numbers change this year?" - Stats, then a Chart.
- "How does this protocol work?" - prose, perhaps a code block; no components.
- A greeting, a quick follow-up, a personal or emotional message - no
  components.

Always:
- Never invent data, pictures or links to fill a component; with nothing
  real to show, write prose.
- Colour carries meaning: tones, Badges and Callouts mark a status or a risk,
  never decoration. A conclusion or a summary is prose, not a Callout.
- Offer Choices only when the reader really has a direction to pick; never
  for yes or no, never out of habit.
- Do not turn a simple question into a dashboard, and do not wrap the whole
  answer, or several unrelated things, in one Card.
- Tables are ordinary Markdown tables, outside any block.

How to place them:
- Write the answer as prose and drop a block in exactly where its content
  belongs, the way a figure sits in an article: a sentence or a heading, then
  the block, then the prose carries on. Several small blocks spread through an
  answer are better than one large block; keep each to one idea.
- Write the text inside components in the reader's language. Do not repeat in
  prose what a block already shows, and never mention the block, its syntax or
  "openchat-ui" to the reader.

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
			if param.Retired {
				continue
			}
			if detail := paramDetail(param); detail != "" {
				fmt.Fprintf(&b, "    %s: %s\n", param.Name, detail)
			}
		}
	}

	pictures := c.Pictures.Sources
	if imageSearch {
		pictures = c.Pictures.Search
	}
	if pictures != "" {
		b.WriteString("\nPictures (Image, Gallery, Media, and a Card's, Step's or Article's image):\n")
		b.WriteString(pictures)
		b.WriteString("\n")
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
		if param.Retired {
			continue
		}
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
