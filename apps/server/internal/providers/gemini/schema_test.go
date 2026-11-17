package gemini

import (
	"encoding/json"
	"reflect"
	"testing"
)

// allowed is every keyword this API's Schema object knows. A request carrying
// any other is refused whole.
var allowed = map[string]bool{
	"type": true, "format": true, "title": true, "description": true, "nullable": true,
	"enum": true, "maxItems": true, "minItems": true, "properties": true, "required": true,
	"minProperties": true, "maxProperties": true, "minLength": true, "maxLength": true,
	"pattern": true, "example": true, "anyOf": true, "propertyOrdering": true,
	"default": true, "items": true, "minimum": true, "maximum": true,
}

// checkKeys walks a cleaned schema and fails on any keyword the API refuses,
// and on the two shapes it refuses: an object without properties and an array
// without items.
func checkKeys(t *testing.T, path string, schema map[string]any) {
	t.Helper()
	switch schema["type"] {
	case "object":
		if props, _ := schema["properties"].(map[string]any); len(props) == 0 {
			t.Errorf("%s: an object without properties would be refused", path)
		}
	case "array":
		if _, ok := schema["items"].(map[string]any); !ok {
			t.Errorf("%s: an array without items would be refused", path)
		}
	}
	for k, v := range schema {
		if !allowed[k] {
			t.Errorf("%s: %q would be refused", path, k)
		}
		switch k {
		case "properties":
			for name, sub := range v.(map[string]any) {
				checkKeys(t, path+".properties."+name, sub.(map[string]any))
			}
		case "items":
			checkKeys(t, path+".items", v.(map[string]any))
		case "anyOf":
			for _, sub := range v.([]any) {
				checkKeys(t, path+".anyOf", sub.(map[string]any))
			}
		}
	}
}

func clean(t *testing.T, raw string) map[string]any {
	t.Helper()
	schema, ok := toolParameters(json.RawMessage(raw))
	if !ok {
		t.Fatalf("toolParameters(%s) sent nothing", raw)
	}
	// Round-trip so numbers and lists compare as the wire would carry them.
	data, err := json.Marshal(schema)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	checkKeys(t, "parameters", out)
	return out
}

func wantSchema(t *testing.T, raw string) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

// image_generate's schema closes itself with additionalProperties, which is
// what took every Gemini run that offered the tool down with a 400.
func TestToolParametersDropsAdditionalProperties(t *testing.T) {
	got := clean(t, `{
  "type": "object",
  "properties": {
    "prompt": {"type": "string", "description": "The whole picture."},
    "aspect": {"type": "string", "enum": ["auto", "1:1"]},
    "count": {"type": "integer", "minimum": 1},
    "images": {"type": "array", "items": {"type": "string"}}
  },
  "required": ["prompt"],
  "additionalProperties": false
}`)
	want := wantSchema(t, `{
  "type": "object",
  "properties": {
    "prompt": {"type": "string", "description": "The whole picture."},
    "aspect": {"type": "string", "enum": ["auto", "1:1"]},
    "count": {"type": "integer", "minimum": 1},
    "images": {"type": "array", "items": {"type": "string"}}
  },
  "required": ["prompt"]
}`)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v\nwant %v", got, want)
	}
}

// A plugin's tool carries what its MCP server generated: pydantic and zod
// output use $defs/$ref, anyOf with null for optionals, type lists, formats
// and allOf.
func TestToolParametersRewritesAnMCPSchema(t *testing.T) {
	got := clean(t, `{
  "$schema": "http://json-schema.org/draft-07/schema#",
  "type": "object",
  "$defs": {
    "Point": {"type": "object", "properties": {"x": {"type": "number"}, "y": {"type": "number"}}, "required": ["x", "y"], "additionalProperties": false}
  },
  "properties": {
    "url": {"type": "string", "format": "uri"},
    "when": {"type": "string", "format": "date-time"},
    "note": {"anyOf": [{"type": "string"}, {"type": "null"}], "default": null, "title": "Note"},
    "limit": {"type": ["integer", "null"], "exclusiveMinimum": 0},
    "level": {"type": "integer", "enum": [1, 2, 3]},
    "kind": {"const": "point", "type": "string"},
    "at": {"$ref": "#/$defs/Point", "description": "Where."},
    "both": {"allOf": [{"$ref": "#/$defs/Point"}, {"properties": {"z": {"type": "number"}}}]},
    "tuple": {"type": "array", "items": [{"type": "string"}], "prefixItems": [{"type": "string"}]}
  },
  "required": ["url", "missing"],
  "additionalProperties": false
}`)
	want := wantSchema(t, `{
  "type": "object",
  "properties": {
    "url": {"type": "string"},
    "when": {"type": "string", "format": "date-time"},
    "note": {"type": "string", "nullable": true, "default": null, "title": "Note"},
    "limit": {"type": "integer", "nullable": true},
    "level": {"type": "integer"},
    "kind": {"type": "string", "enum": ["point"]},
    "at": {"type": "object", "description": "Where.", "properties": {"x": {"type": "number"}, "y": {"type": "number"}}, "required": ["x", "y"]},
    "both": {"type": "object", "properties": {"x": {"type": "number"}, "y": {"type": "number"}, "z": {"type": "number"}}, "required": ["x", "y"]},
    "tuple": {"type": "array", "items": {"type": "string"}}
  },
  "required": ["url"]
}`)
	if !reflect.DeepEqual(got, want) {
		gotJSON, _ := json.MarshalIndent(got, "", "  ")
		wantJSON, _ := json.MarshalIndent(want, "", "  ")
		t.Errorf("got %s\nwant %s", gotJSON, wantJSON)
	}
}

func TestToolParametersKeepsAChoiceBetweenTypes(t *testing.T) {
	got := clean(t, `{"type": "object", "properties": {
  "id": {"type": ["string", "integer"]},
  "value": {"oneOf": [{"type": "string"}, {"type": "number"}, {"type": "null"}]}
}}`)
	want := wantSchema(t, `{"type": "object", "properties": {
  "id": {"anyOf": [{"type": "string"}, {"type": "integer"}]},
  "value": {"nullable": true, "anyOf": [{"type": "string"}, {"type": "number"}]}
}}`)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v\nwant %v", got, want)
	}
}

func TestToolParametersEndsARecursiveDefinition(t *testing.T) {
	got := clean(t, `{"type": "object", "properties": {"root": {"$ref": "#/definitions/Node"}},
  "definitions": {"Node": {"type": "object", "properties": {"name": {"type": "string"}, "children": {"type": "array", "items": {"$ref": "#/definitions/Node"}}}}}}`)
	depth := 0
	for node := got["properties"].(map[string]any)["root"].(map[string]any); ; depth++ {
		children, ok := node["properties"].(map[string]any)["children"].(map[string]any)
		if !ok {
			break
		}
		node = children["items"].(map[string]any)
	}
	if depth == 0 || depth > maxRefDepth {
		t.Errorf("recursion unrolled %d levels, want between 1 and %d", depth, maxRefDepth)
	}
}

// This API refuses an object with empty properties, so a tool that takes
// nothing - however it says so - sends no parameters at all.
func TestToolParametersSendsNothingForATakesNothingTool(t *testing.T) {
	for _, raw := range []string{
		``,
		`{"type":"object"}`,
		`{"type":"object","properties":{}}`,
		`{"type":"object","properties":{},"additionalProperties":false}`,
		`not json`,
	} {
		if schema, ok := toolParameters(json.RawMessage(raw)); ok {
			t.Errorf("toolParameters(%q) = %v, want nothing", raw, schema)
		}
	}
}

// What this API has no form for - a free-form map, an array of them - is left
// out, with its name out of required; an array that does not say what it holds
// holds strings; each type of a type list carries what goes with it.
func TestToolParametersLeavesOutWhatTheAPICannotTake(t *testing.T) {
	got := clean(t, `{"type": "object", "properties": {
  "title": {"type": "string"},
  "metadata": {"type": "object", "additionalProperties": {"type": "string"}},
  "rows": {"type": "array", "items": {"type": "object", "additionalProperties": true}},
  "tags": {"type": "array"},
  "ids": {"type": ["string", "array"], "items": {"type": "integer"}},
  "anything": {"description": "Any value."}
}, "required": ["title", "metadata"]}`)
	want := wantSchema(t, `{"type": "object", "properties": {
  "title": {"type": "string"},
  "tags": {"type": "array", "items": {"type": "string"}},
  "ids": {"anyOf": [{"type": "string"}, {"type": "array", "items": {"type": "integer"}}]},
  "anything": {"description": "Any value."}
}, "required": ["title"]}`)
	if !reflect.DeepEqual(got, want) {
		gotJSON, _ := json.MarshalIndent(got, "", "  ")
		wantJSON, _ := json.MarshalIndent(want, "", "  ")
		t.Errorf("got %s\nwant %s", gotJSON, wantJSON)
	}
}

// A filter that refers to itself three times (and, or, not) grew as 3 to the
// depth; it stays small.
func TestToolParametersBoundsASelfReferentialSchema(t *testing.T) {
	got := clean(t, `{"type": "object", "properties": {"filter": {"$ref": "#/$defs/Filter"}},
  "$defs": {"Filter": {"type": "object", "properties": {
    "field": {"type": "string"},
    "and": {"type": "array", "items": {"$ref": "#/$defs/Filter"}},
    "or": {"type": "array", "items": {"$ref": "#/$defs/Filter"}},
    "not": {"$ref": "#/$defs/Filter"}
  }}}}`)
	data, _ := json.Marshal(got)
	if len(data) > 32<<10 {
		t.Errorf("parameters are %d bytes", len(data))
	}
	if _, ok := got["properties"].(map[string]any)["filter"]; !ok {
		t.Error("the filter itself was left out")
	}
}
