package graphql

import (
	"encoding/json"
	"maps"
	"strings"

	"github.com/vektah/gqlparser/v2/ast"

	"github.com/dexpace/morphic/ir"
)

// federationDirectives is the set of directive names owned by Apollo Federation
// (v1 and v2). Applications of these are preserved under the "federation:"
// extension namespace; every other directive goes under "graphql:".
var federationDirectives = map[string]bool{
	"key":              true,
	"external":         true,
	"requires":         true,
	"provides":         true,
	"extends":          true,
	"tag":              true,
	"shareable":        true,
	"inaccessible":     true,
	"override":         true,
	"composeDirective": true,
	"interfaceObject":  true,
	"link":             true,
	"authenticated":    true,
	"requiresScopes":   true,
	"policy":           true,
}

// isFederationDirective reports whether name is a federation directive.
func isFederationDirective(name string) bool { return federationDirectives[name] }

// isHandledDirective reports whether a directive is consumed structurally and so
// is not also emitted into the generic Extensions dump. @deprecated becomes a
// Deprecation and @oneOf turns an input object into a tagged union.
func isHandledDirective(name string) bool {
	return name == "deprecated" || name == "oneOf"
}

// docsFrom maps a GraphQL description onto Docs. GraphQL descriptions are a
// single block with no summary/description split, so the whole text is the
// Description.
func docsFrom(desc string) ir.Docs {
	if desc == "" {
		return ir.Docs{}
	}
	return ir.Docs{Description: desc}
}

// deprecationFrom extracts an @deprecated application into a Deprecation. An
// absent reason leaves Message empty — the Deprecation's presence is the fact;
// an emitter supplies its own default text.
func deprecationFrom(dirs ast.DirectiveList) *ir.Deprecation {
	d := dirs.ForName("deprecated")
	if d == nil {
		return nil
	}
	return &ir.Deprecation{Message: stringArg(d, "reason")}
}

// lowerDirectives lowers every directive application in a list into namespaced,
// ordered-array Extensions (ir-design §8.4): repeatable applications accumulate
// in application order under one key, singletons form a one-element array.
// Structurally-consumed directives are skipped.
func lowerDirectives(dirs ast.DirectiveList) ir.Extensions {
	if len(dirs) == 0 {
		return nil
	}
	byKey := make(map[string][]json.RawMessage)
	for _, d := range dirs {
		if isHandledDirective(d.Name) {
			continue
		}
		key := directiveKey(d.Name)
		byKey[key] = append(byKey[key], directiveAppJSON(d))
	}
	if len(byKey) == 0 {
		return nil
	}
	out := make(ir.Extensions, len(byKey))
	for key, apps := range byKey {
		out[key] = jsonArray(apps)
	}
	return out
}

// directiveKey namespaces a directive name by origin.
func directiveKey(name string) string {
	if isFederationDirective(name) {
		return "federation:@" + name
	}
	return "graphql:@" + name
}

// directiveAppJSON renders one directive application as a JSON object of its
// arguments in source order; an application with no arguments renders as {}.
func directiveAppJSON(d *ast.Directive) json.RawMessage {
	var b strings.Builder
	b.WriteByte('{')
	for i, arg := range d.Arguments {
		if i > 0 {
			b.WriteByte(',')
		}
		key, _ := json.Marshal(arg.Name)
		b.Write(key)
		b.WriteByte(':')
		b.Write(valueJSON(arg.Value))
	}
	b.WriteByte('}')
	return json.RawMessage(b.String())
}

// jsonArray joins pre-rendered JSON values into a JSON array.
func jsonArray(items []json.RawMessage) json.RawMessage {
	var b strings.Builder
	b.WriteByte('[')
	for i, it := range items {
		if i > 0 {
			b.WriteByte(',')
		}
		b.Write(it)
	}
	b.WriteByte(']')
	return json.RawMessage(b.String())
}

// mergeExtensions overlays src onto dst, allocating dst on first write.
func mergeExtensions(dst, src ir.Extensions) ir.Extensions {
	if len(src) == 0 {
		return dst
	}
	if dst == nil {
		dst = make(ir.Extensions, len(src))
	}
	maps.Copy(dst, src)
	return dst
}

// directiveDefinitionsJSON renders the document's directive definitions verbatim
// for the graphql:directive-definitions inventory (ir-design §8.4), or nil when
// there are none.
func directiveDefinitionsJSON(defs ast.DirectiveDefinitionList) json.RawMessage {
	if len(defs) == 0 {
		return nil
	}
	items := make([]json.RawMessage, 0, len(defs))
	for _, d := range defs {
		items = append(items, directiveDefJSON(d))
	}
	return jsonArray(items)
}

// directiveDefJSON renders one directive definition as a JSON object.
func directiveDefJSON(d *ast.DirectiveDefinition) json.RawMessage {
	locs := make([]json.RawMessage, 0, len(d.Locations))
	for _, loc := range d.Locations {
		q, _ := json.Marshal(string(loc))
		locs = append(locs, q)
	}
	args := make([]json.RawMessage, 0, len(d.Arguments))
	for _, a := range d.Arguments {
		args = append(args, argumentDefJSON(a))
	}
	var b strings.Builder
	b.WriteString(`{"name":`)
	name, _ := json.Marshal(d.Name)
	b.Write(name)
	if d.Description != "" {
		desc, _ := json.Marshal(d.Description)
		b.WriteString(`,"description":`)
		b.Write(desc)
	}
	b.WriteString(`,"repeatable":`)
	b.Write(jsonBool(boolText(d.IsRepeatable)))
	b.WriteString(`,"locations":`)
	b.Write(jsonArray(locs))
	b.WriteString(`,"arguments":`)
	b.Write(jsonArray(args))
	b.WriteByte('}')
	return json.RawMessage(b.String())
}

// argumentDefJSON renders one argument definition (name, type, optional default)
// as a JSON object.
func argumentDefJSON(a *ast.ArgumentDefinition) json.RawMessage {
	var b strings.Builder
	b.WriteString(`{"name":`)
	name, _ := json.Marshal(a.Name)
	b.Write(name)
	b.WriteString(`,"type":`)
	typ, _ := json.Marshal(a.Type.String())
	b.Write(typ)
	if a.DefaultValue != nil {
		b.WriteString(`,"defaultValue":`)
		b.Write(valueJSON(a.DefaultValue))
	}
	b.WriteByte('}')
	return json.RawMessage(b.String())
}

// boolText renders a Go bool as its JSON literal text.
func boolText(v bool) string {
	if v {
		return "true"
	}
	return "false"
}
