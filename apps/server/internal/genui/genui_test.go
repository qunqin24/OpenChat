package genui

import (
	"strings"
	"testing"
)

func TestCatalogueLoadsAndIsWellFormed(t *testing.T) {
	catalog := Load()
	if len(catalog.Components) == 0 {
		t.Fatal("empty catalogue")
	}
	names := map[string]bool{}
	for _, c := range catalog.Components {
		if names[c.Name] {
			t.Errorf("%s is declared twice", c.Name)
		}
		names[c.Name] = true
	}
	for _, c := range catalog.Components {
		optional := false
		for _, p := range c.Params {
			if _, known := typeNames[p.Type]; !known && p.Type != "enum" {
				t.Errorf("%s.%s has unknown type %q", c.Name, p.Name, p.Type)
			}
			if p.Type == "enum" && len(p.Values) == 0 {
				t.Errorf("%s.%s is an enum with no values", c.Name, p.Name)
			}
			for _, of := range p.Of {
				if !names[of] {
					t.Errorf("%s.%s accepts unknown component %q", c.Name, p.Name, of)
				}
			}
			// Positional arguments fill parameters in order, so a required
			// parameter after an optional one could never be given by position.
			if !p.Required {
				optional = true
			} else if optional {
				t.Errorf("%s.%s is required but follows an optional parameter", c.Name, p.Name)
			}
		}
	}
}

func TestPromptListsEveryComponent(t *testing.T) {
	catalog := Load()
	prompt := catalog.Prompt(false)
	for _, c := range catalog.Components {
		if c.Retired {
			continue
		}
		if !strings.Contains(prompt, "\n"+signature(c)+"\n") {
			t.Errorf("prompt does not list %s", signature(c))
		}
	}
	if !strings.Contains(prompt, "```openchat-ui") {
		t.Error("prompt does not name the block's language")
	}
}

func TestRetiredComponentsAreNotTaught(t *testing.T) {
	prompt := Load().Prompt(false)
	if strings.Contains(prompt, "\nTable(") {
		t.Error("the retired Table component is still in the model's instructions")
	}
	if !strings.Contains(prompt, "Markdown tables") {
		t.Error("the instructions no longer say tables are Markdown")
	}
}

// Pictures are described for the server to find with image search on, and
// are addresses a source listed with it off; never both rules at once.
func TestPromptPicturesFollowImageSearch(t *testing.T) {
	catalog := Load()
	on, off := catalog.Prompt(true), catalog.Prompt(false)
	if !strings.Contains(on, catalog.Pictures.Search) || strings.Contains(on, catalog.Pictures.Sources) {
		t.Error("image search on: the prompt does not teach described pictures alone")
	}
	if !strings.Contains(off, catalog.Pictures.Sources) || strings.Contains(off, catalog.Pictures.Search) {
		t.Error("image search off: the prompt does not teach source addresses alone")
	}
}
