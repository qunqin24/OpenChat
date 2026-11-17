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
// tree whose children are trees) ends as a plain object instead of looping.
const maxRefDepth = 8

// toolParameters returns the schema to send as a declaration's parameters, or
// false when there is none to send: this API rejects an object schema whose
// properties are empty, so a tool that takes nothing goes without one.
func toolParameters(raw json.RawMessage) (map[string]any, bool) {
	var root map[string]any
	if len(raw) == 0 || json.Unmarshal(raw, &root) != nil {
		return nil, false
	}
	c := cleaner{defs: map[string]any{}}
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
	defs map[string]any
}

func (c cleaner) clean(node map[string]any, depth int) map[string]any {
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
			choices := make([]any, len(types))
			for i, s := range types {
				choices[i] = map[string]any{"type": s}
			}
			out["anyOf"] = choices
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
			choices = append(choices, c.clean(sub, depth))
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

	if props, ok := node["properties"].(map[string]any); ok {
		cleaned := map[string]any{}
		for name, each := range props {
			if sub, ok := each.(map[string]any); ok {
				cleaned[name] = c.clean(sub, depth)
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
	if items, ok := node["items"].(map[string]any); ok {
		out["items"] = c.clean(items, depth)
	}
	return out
}

// mergeAll folds allOf into the schema holding it: properties and required
// are joined, and otherwise the first to say something wins.
func (c cleaner) mergeAll(node map[string]any, all []any, depth int) map[string]any {
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
