package api

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"testing"
)

// The table is edited by hand and read by the picker, so the mistakes worth
// catching are the quiet ones: a vendor with no name to draw, a group the tabs
// do not know, an id served twice.
func TestChannelTypesTableIsWellFormed(t *testing.T) {
	seen := map[ChannelType]bool{}
	for _, def := range channelTypes {
		if def.Label == "" {
			t.Errorf("%s has no label", def.ID)
		}
		if !def.Group.Valid() {
			t.Errorf("%s has unknown group %q", def.ID, def.Group)
		}
		if !def.WireApi.Valid() {
			t.Errorf("%s has unknown wire protocol %q", def.ID, def.WireApi)
		}
		if seen[def.ID] {
			t.Errorf("%s appears twice", def.ID)
		}
		seen[def.ID] = true
	}
}

// Every value the contract allows must be offered, or an operator could never
// pick it — and every id served must be one the contract accepts back.
//
// Go has no way to enumerate the constants of a type, so this reads them out of
// the generated file; a hand-kept list here would be a second table to forget.
func TestChannelTypesCoverTheContractEnum(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "openapi.gen.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var enum []ChannelType
	ast.Inspect(file, func(n ast.Node) bool {
		spec, ok := n.(*ast.ValueSpec)
		if !ok {
			return true
		}
		if ident, ok := spec.Type.(*ast.Ident); !ok || ident.Name != "ChannelType" {
			return true
		}
		for _, value := range spec.Values {
			if lit, ok := value.(*ast.BasicLit); ok && lit.Kind == token.STRING {
				if v, err := strconv.Unquote(lit.Value); err == nil {
					enum = append(enum, ChannelType(v))
				}
			}
		}
		return true
	})
	if len(enum) == 0 {
		t.Fatal("found no ChannelType constants in openapi.gen.go")
	}

	served := map[ChannelType]bool{}
	for _, def := range channelTypes {
		served[def.ID] = true
		if !def.ID.Valid() {
			t.Errorf("%s is served but not in the contract enum", def.ID)
		}
	}
	for _, id := range enum {
		if !served[id] {
			t.Errorf("%s is in the contract enum but not in the table", id)
		}
	}
}
