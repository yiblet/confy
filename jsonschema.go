package confy

import "strings"

// JSONSchema renders the schema as a JSON Schema (draft 2020-12) document,
// as a map ready for encoding/json. Objects use unevaluatedProperties:false
// (additionalProperties:false would reject union-arm keys); tag unions
// become oneOf with a const tag and an OpenAPI-style discriminator hint;
// kind unions become oneOf by type; Rec definitions go to $defs.
func JSONSchema(s Schema) map[string]any {
	out := jsonNode(s.Root, s.Defs)
	out["$schema"] = "https://json-schema.org/draft/2020-12/schema"
	if len(s.Defs) > 0 {
		defs := map[string]any{}
		for _, name := range sortedDefNames(s.Defs) {
			defs[name] = jsonNode(s.Defs[name], s.Defs)
		}
		out["$defs"] = defs
	}
	return out
}

func jsonMeta(m map[string]any, meta Meta) map[string]any {
	if meta.Doc != "" {
		m["description"] = meta.Doc
	}
	if meta.Deprecated != "" {
		m["deprecated"] = true
		if meta.Doc == "" {
			m["description"] = "deprecated: " + meta.Deprecated
		}
	}
	if meta.Secret {
		m["writeOnly"] = true
	}
	return m
}

func jsonNode(n Node, defs map[string]Node) map[string]any {
	switch x := n.(type) {
	case Empty:
		return jsonMeta(map[string]any{"type": "object"}, x.Meta)
	case Leaf:
		m := jsonLeaf(x)
		return jsonMeta(m, x.Meta)
	case Object:
		return jsonMeta(jsonObject(x, defs), x.Meta)
	case Array:
		return jsonMeta(map[string]any{"type": "array", "items": jsonNode(x.Elem, defs)}, x.Meta)
	case Table:
		return jsonMeta(map[string]any{"type": "object", "additionalProperties": jsonNode(x.Elem, defs)}, x.Meta)
	case Union:
		return jsonMeta(jsonObject(Object{Unions: []Union{x}}, defs), x.Meta)
	case Ref:
		return jsonMeta(map[string]any{"$ref": "#/$defs/" + x.Name}, x.Meta)
	}
	return map[string]any{}
}

func jsonLeaf(l Leaf) map[string]any {
	m := map[string]any{}
	switch {
	case l.Type == "string":
		m["type"] = "string"
	case l.Type == "int" || l.Type == "int64":
		m["type"] = "integer"
	case l.Type == "float":
		m["type"] = "number"
	case l.Type == "bool":
		m["type"] = "boolean"
	case strings.HasPrefix(l.Type, "one of: "):
		vals := strings.Split(strings.TrimPrefix(l.Type, "one of: "), ", ")
		enum := make([]any, len(vals))
		for i, v := range vals {
			enum[i] = v
		}
		m["type"] = "string"
		m["enum"] = enum
	default:
		m["type"] = "string"
		m["format"] = l.Type
	}
	if l.Default != nil && !l.Secret {
		m["default"] = *l.Default
	}
	return m
}

// jsonObject renders one mapping level. Fields become properties; each
// ByTag union becomes a oneOf whose arms carry the tag const and the arm's
// own object schema; a ByKind union at object level becomes a oneOf by type.
func jsonObject(o Object, defs map[string]Node) map[string]any {
	props := map[string]any{}
	var required []any
	for _, f := range o.Fields {
		props[f.Key] = jsonNode(f.Node, defs)
		if !f.Optional && !isOptional(f.Node) {
			required = append(required, f.Key)
		}
	}
	m := map[string]any{"type": "object", "properties": props, "unevaluatedProperties": false}
	if len(required) > 0 {
		m["required"] = required
	}
	var allOf []any
	for _, u := range o.Unions {
		switch u.Discriminant.Kind {
		case ByTag:
			var oneOf []any
			for _, a := range u.Arms {
				arm := jsonArmObject(a.Node, defs)
				armProps := arm["properties"].(map[string]any)
				armProps[u.Discriminant.Key] = map[string]any{"const": a.Tag}
				req, _ := arm["required"].([]any)
				arm["required"] = append([]any{u.Discriminant.Key}, req...)
				delete(arm, "unevaluatedProperties") // the enclosing object owns that
				oneOf = append(oneOf, arm)
			}
			sel := map[string]any{"oneOf": oneOf, "discriminator": map[string]any{"propertyName": u.Discriminant.Key}}
			if u.Optional {
				sel = map[string]any{"anyOf": []any{sel, map[string]any{"not": map[string]any{"required": []any{u.Discriminant.Key}}}}}
			}
			allOf = append(allOf, sel)
		case ByKind:
			var oneOf []any
			for _, a := range u.Arms {
				oneOf = append(oneOf, jsonNode(a.Node, defs))
			}
			// A kind union standing alone is not an object at all.
			if len(o.Fields) == 0 && len(o.Unions) == 1 {
				return map[string]any{"oneOf": oneOf}
			}
			allOf = append(allOf, map[string]any{"oneOf": oneOf})
		}
	}
	if len(allOf) > 0 {
		m["allOf"] = allOf
	}
	return m
}

// jsonArmObject renders a union arm as an object fragment with properties.
// A Ref arm stays a $ref (expanding it would recurse forever on a Rec that
// refers to itself through a Switch); the tag const sits beside the $ref,
// which draft 2019-09+ allows.
func jsonArmObject(n Node, defs map[string]Node) map[string]any {
	if r, ok := n.(Ref); ok {
		return jsonMeta(map[string]any{"$ref": "#/$defs/" + r.Name, "properties": map[string]any{}}, r.Meta)
	}
	switch x := n.(type) {
	case Object:
		return jsonObject(x, defs)
	case Empty:
		return map[string]any{"type": "object", "properties": map[string]any{}}
	default:
		m := jsonNode(n, defs)
		if _, ok := m["properties"]; !ok {
			m["properties"] = map[string]any{}
		}
		return m
	}
}
