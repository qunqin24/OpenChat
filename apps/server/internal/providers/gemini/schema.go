package gemini

import "encoding/json"

// A function declaration's parameters on this API are its Schema object, a
// subset of OpenAPI 3.0, not JSON Schema: a keyword it does not know fails the
// whole request with a 400 ("Unknown name \"additionalProperties\""), and every
// request after it in the conversation with it. Our own tools are written as
// JSON Schema for the other two protocols, and a plugin's tools carry whatever
// its MCP server declared ($schema, $ref and $defs, additionalProperties,
// type lists with "null"), so each schema is rewritten into that subset here
// rather than every author having to know this API's dialect.

// schemaKeys are the keywords this API's Schema takes as they are; the ones
// that hold schemas or need rewriting are handled in clean.
var schemaKeys = map[string]bool{
	"title": true, "description": true, "nullable": true, "default": true, "example": true,
	"pattern": true, "minimum": true, "maximum": true,
	"minLength": true, "maxLength": true, "minItems": true, "maxItems": true,
	"minProperties": true, "maxProperties": true, "propertyOrdering": true,
}

// formats are the only formats this API accepts, by type; any other (uri,
// email, uuid) is refused rather than ignored.
var formats = map[string]map[string]bool{
	"string":  {"enum": true, "date-time": true},
	"integer": {"int32": true, "int64": true},
	"number":  {"float": true, "double": true},
}

// maxRefDepth bounds how far $refs are followed, so a recursive definition (a
// tree whose children are trees) ends instead of looping; maxNodes bounds the
// whole rewrite, since a definition that refers to itself k times grows as k
// to the depth (a filter of and/or/not reached half a megabyte at depth 8).
// What is cut off is an object with nothing said about it, which the parent
// leaves out (expressible).
const (
	maxRefDepth = 3
	maxNodes    = 400
)

// toolParameters returns the schema to send as a declaration's parameters, or
// false when there is none to send: this API rejects an object schema whose
// properties are empty, so a tool that takes nothing goes without one.
func toolParameters(raw json.RawMessage) (map[string]any, bool) {
	var root map[string]any
	if len(raw) == 0 || json.Unmarshal(raw, &root) != nil {
		return nil, false
	}
	c := &cleaner{defs: map[string]any{}}
	for _, key := range []string{"$defs", "definitions"} {
		if defs, ok := root[key].(map[string]any); ok {
			for name, def := range defs {
				c.defs["#/"+key+"/"+name] = def
			}
		}
	}
	schema := c.clean(root, 0)
	if _, ok := schema["properties"]; !ok {
		return nil, false
	}
	return schema, true
}

type cleaner struct {
	defs  map[string]any
	nodes int
}

func (c *cleaner) clean(node map[string]any, depth int) map[string]any {
	c.nodes++
	if c.nodes > maxNodes {
		return map[string]any{"type": "object"}
	}
	if ref, ok := node["$ref"].(string); ok {
		target, found := c.defs[ref].(map[string]any)
		if !found || depth >= maxRefDepth {
			// An unresolvable or too-deep reference still names an object
			// in practice; what it was is said by its description, if any.
			target = map[string]any{"type": "object"}
		}
		// Keywords beside the $ref (a description of this use) win over the
		// definition's own.
		merged := make(map[string]any, len(target)+len(node))
		for k, v := range target {
			merged[k] = v
		}
		for k, v := range node {
			if k != "$ref" {
				merged[k] = v
			}
		}
		return c.clean(merged, depth+1)
	}
	if all, ok := node["allOf"].([]any); ok {
		return c.clean(c.mergeAll(node, all, depth), depth+1)
	}

	out := map[string]any{}
	for k, v := range node {
		if schemaKeys[k] {
			out[k] = v
		}
	}

	switch t := node["type"].(type) {
	case string:
		out["type"] = t
	case []any:
		// ["string", "null"] is a nullable string here; several real types
		// become a choice between them.
		var types []string
		for _, each := range t {
			if s, ok := each.(string); ok {
				if s == "null" {
					out["nullable"] = true
				} else {
					types = append(types, s)
				}
			}
		}
		if len(types) == 1 {
			out["type"] = types[0]
		} else if len(types) > 1 {
			// Each type a choice of its own, carrying what goes with it (an
			// array's items, an object's properties) rather than leaving
			// those beside the choice.
			var choices []any
			for _, s := range types {
				one := make(map[string]any, len(node))
				for k, v := range node {
					if k != "anyOf" && k != "oneOf" {
						one[k] = v
					}
				}
				one["type"] = s
				if choice := c.clean(one, depth); expressible(choice) {
					delete(choice, "description")
					choices = append(choices, choice)
				}
			}
			switch len(choices) {
			case 0:
			case 1:
				for k, v := range choices[0].(map[string]any) {
					if _, set := out[k]; !set {
						out[k] = v
					}
				}
			default:
				out["anyOf"] = choices
			}
			return out
		}
	}

	for _, key := range []string{"anyOf", "oneOf"} {
		list, ok := node[key].([]any)
		if !ok {
			continue
		}
		var choices []map[string]any
		for _, each := range list {
			sub, ok := each.(map[string]any)
			if !ok {
				continue
			}
			if sub["type"] == "null" {
				out["nullable"] = true
				continue
			}
			if choice := c.clean(sub, depth); expressible(choice) {
				choices = append(choices, choice)
			}
		}
		switch len(choices) {
		case 0:
		case 1:
			// Optional<T> written as anyOf [T, null]: T itself, nullable.
			for k, v := range choices[0] {
				if _, set := out[k]; !set {
					out[k] = v
				}
			}
		default:
			anyOf := make([]any, len(choices))
			for i, choice := range choices {
				anyOf[i] = choice
			}
			out["anyOf"] = anyOf
		}
	}

	typ, _ := out["type"].(string)
	if format, ok := node["format"].(string); ok && formats[typ][format] {
		out["format"] = format
	}
	// enum is a list of strings on this API; a list of numbers is refused.
	if s, ok := node["const"].(string); ok {
		out["enum"] = []any{s}
	}
	if enum, ok := node["enum"].([]any); ok && len(enum) > 0 && allStrings(enum) {
		out["enum"] = enum
	}

	// What goes with another type is not carried: a choice of a type list is
	// cleaned from the whole node, items and properties included.
	if props, ok := node["properties"].(map[string]any); ok && (typ == "" || typ == "object") {
		cleaned := map[string]any{}
		for name, each := range props {
			if sub, ok := each.(map[string]any); ok {
				// One this API cannot take (a free-form map, an array of
				// them) is left out rather than failing the whole request.
				if schema := c.clean(sub, depth); expressible(schema) {
					cleaned[name] = schema
				}
			}
		}
		if len(cleaned) > 0 {
			out["properties"] = cleaned
			// A required name with no property behind it is refused too.
			if required, ok := node["required"].([]any); ok {
				var kept []any
				for _, name := range required {
					if s, ok := name.(string); ok && cleaned[s] != nil {
						kept = append(kept, s)
					}
				}
				if len(kept) > 0 {
					out["required"] = kept
				}
			}
		}
	}
	if items, ok := node["items"].(map[string]any); ok && (typ == "" || typ == "array") {
		out["items"] = c.clean(items, depth)
	}
	if _, ok := out["properties"]; ok && out["type"] == nil {
		out["type"] = "object"
	}
	// An array must say what it holds here; one that does not (or a tuple,
	// which this API has no form for) is taken to hold strings.
	if out["type"] == "array" && out["items"] == nil {
		out["items"] = map[string]any{"type": "string"}
	}
	return out
}

// expressible is whether this API takes a cleaned schema: an object only with
// properties to name, an array only of something it takes, a choice only with
// choices left.
func expressible(schema map[string]any) bool {
	switch schema["type"] {
	case "object":
		props, _ := schema["properties"].(map[string]any)
		return len(props) > 0
	case "array":
		items, ok := schema["items"].(map[string]any)
		return ok && expressible(items)
	}
	if anyOf, ok := schema["anyOf"].([]any); ok {
		return len(anyOf) > 0
	}
	return true
}

// mergeAll folds allOf into the schema holding it: properties and required
// are joined, and otherwise the first to say something wins.
func (c *cleaner) mergeAll(node map[string]any, all []any, depth int) map[string]any {
	merged := map[string]any{}
	props := map[string]any{}
	var required []any
	for _, each := range append([]any{node}, all...) {
		sub, ok := each.(map[string]any)
		if !ok {
			continue
		}
		if ref, ok := sub["$ref"].(string); ok && depth < maxRefDepth {
			// A member is usually a reference to a shared definition.
			if target, found := c.defs[ref].(map[string]any); found {
				sub = target
			}
		}
		for k, v := range sub {
			switch k {
			case "allOf", "$ref":
			case "properties":
				if p, ok := v.(map[string]any); ok {
					for name, schema := range p {
						props[name] = schema
					}
				}
			case "required":
				if r, ok := v.([]any); ok {
					required = append(required, r...)
				}
			default:
				if _, set := merged[k]; !set {
					merged[k] = v
				}
			}
		}
	}
	if len(props) > 0 {
		merged["properties"] = props
	}
	if len(required) > 0 {
		merged["required"] = required
	}
	return merged
}

func allStrings(values []any) bool {
	for _, v := range values {
		if _, ok := v.(string); !ok {
			return false
		}
	}
	return true
}
