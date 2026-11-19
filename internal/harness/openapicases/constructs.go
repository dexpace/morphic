// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package openapicases

import (
	"fmt"
	"slices"
)

// construct is one keyword a case writes: its name, the versions writing it,
// the targets it is said of, the two values it writes, the components they
// name, and, for a keyword nothing carries, why not.
type construct struct {
	keyword string
	span    span
	// targets are the names of the targets the keyword is said of; nil is
	// every target.
	targets []string
	// values returns the members each of the two values merges into t's body
	// at v: the keyword, and what a value needs beside it.
	values func(targetValue target, versionValue version) [2]object
	// components are the component schemas the values name. The case leaving
	// the keyword out writes none of them, so it differs from the keyword's
	// by them whatever the keyword does: such a keyword is held to its other
	// value alone.
	components object
	// reason says why nothing carries the keyword; empty for one carried.
	reason string
	// stays is where the spellings write a keyword none moves; the comment on
	// each such row of the table says why.
	stays stays
}

// stays is which spellings write a keyword: every one, moving it as one unit
// with the rest of what the case says, or, for a keyword no spelling moves,
// inline and beside, or inline alone.
type stays uint8

// The spellings a keyword is written in.
const (
	// moved is every spelling.
	moved stays = iota
	// besideToo is inline and beside: the keyword stays beside the use.
	besideToo
	// inlineAlone is inline: written beside the use, the keyword changes what
	// the use names.
	inlineAlone
)

// span is the versions a construct is written at.
type span uint8

// The spans a construct takes.
const (
	// every version.
	every span = iota
	// only30 is 3.0 alone.
	only30
	// from31 is 3.1 and every later version, whose schemas read JSON Schema
	// 2020-12.
	from31
)

// holds reports whether s writes at versionValue.
func (s span) holds(versionValue version) bool {
	switch s {
	case only30:
		return !versionValue.dialect
	case from31:
		return versionValue.dialect
	default:
		return s == every
	}
}

// saidOf reports whether c is said of targetValue.
func (c construct) saidOf(targetValue target) bool {
	return c.targets == nil || slices.Contains(c.targets, targetValue.name)
}

// The groups of targets a keyword is said of, by the type JSON Schema gives it
// meaning for.
func stringTargets() []string { return []string{stringTarget, enumTarget, formattedTarget} }
func numberTargets() []string { return []string{integerTarget} }
func arrayTargets() []string  { return []string{arrayTarget} }
func objectTargets() []string { return []string{objectTarget, mapTarget} }

// fixed is the values of a keyword written one and other whatever the target.
func fixed(keyword string, one, other raw) func(target, version) [2]object {
	return func(target, version) [2]object {
		return [2]object{{{keyword, one}}, {{keyword, other}}}
	}
}

// instances is the values of a keyword written as the target's instances,
// each spelled by layout, a fmt layout of one %s.
func instances(keyword, layout string) func(target, version) [2]object {
	return func(targetValue target, _ version) [2]object {
		at := func(i int) object { return object{{keyword, raw(fmt.Sprintf(layout, targetValue.instances[i]))}} }
		return [2]object{at(0), at(1)}
	}
}

// exclusive is the values of an exclusive bound: before 2020-12 a flag on the
// bound beside it, true and false; from it a number of its own.
func exclusive(keyword, bound string) func(target, version) [2]object {
	return func(_ target, versionValue version) [2]object {
		if versionValue.dialect {
			return [2]object{{{keyword, raw("9")}}, {{keyword, raw("8")}}}
		}
		return [2]object{{{bound, raw("9")}, {keyword, raw("true")}}, {{bound, raw("9")}, {keyword, raw("false")}}}
	}
}

// narrowing is the values of a union keyword: two branches, one of which
// says more than the target, or for an object two shapes, so writing the union
// is observable. A union whose branches the target already says changes
// nothing (docs/compilers/openapi.md#allof-refinement), so no case writes one.
func narrowing(keyword string) func(target, version) [2]object {
	return func(targetValue target, _ version) [2]object {
		var one, other string
		switch targetValue.name {
		case stringTarget, enumTarget, formattedTarget:
			one, other = `[{"type": "string", "maxLength": 5}, {"type": "boolean"}]`,
				`[{"type": "string", "maxLength": 6}, {"type": "boolean"}]`
		case integerTarget:
			one, other = `[{"type": "integer", "maximum": 5}, {"type": "boolean"}]`,
				`[{"type": "integer", "maximum": 6}, {"type": "boolean"}]`
		case arrayTarget:
			one, other = `[{"type": "array", "maxItems": 5}, {"type": "boolean"}]`,
				`[{"type": "array", "maxItems": 6}, {"type": "boolean"}]`
		case objectTarget, mapTarget:
			one, other = `[{"type": "object", "properties": {"b": {"type": "string"}}}, `+
				`{"type": "object", "properties": {"c": {"type": "string"}}}]`,
				`[{"type": "object", "properties": {"b": {"type": "integer"}}}, `+
					`{"type": "object", "properties": {"c": {"type": "string"}}}]`
		default:
			one, other = `[{"type": "string"}, {"type": "boolean"}]`, `[{"type": "string"}, {"type": "number"}]`
		}
		return [2]object{{{keyword, raw(one)}}, {{keyword, raw(other)}}}
	}
}

// constructs returns the constructs, in the order the construct axis lists
// them after none.
func constructs() []construct {
	return slices.Concat(sharedConstructs(), openAPIConstructs(), dialectConstructs())
}

// sharedConstructs are the keywords every version takes from JSON Schema.
func sharedConstructs() []construct {
	return []construct{
		// The use's own keyword: a second $ref beside it or on an entry is a second
		// use, not a keyword said of the target (docs/compilers/openapi.md#ref-use-site).
		{keyword: "$ref", values: fixed("$ref", `"#/components/schemas/RefString"`, `"#/components/schemas/RefInteger"`),
			components: object{{"RefString", raw(`{"type": "string"}`)}, {"RefInteger", raw(`{"type": "integer"}`)}},
			stays:      inlineAlone},
		{keyword: "title", values: fixed("title", `"T"`, `"Z"`)},
		{keyword: "multipleOf", targets: numberTargets(), values: fixed("multipleOf", "2", "3")},
		{keyword: "maximum", targets: numberTargets(), values: fixed("maximum", "9", "8")},
		{keyword: "exclusiveMaximum", targets: numberTargets(), values: exclusive("exclusiveMaximum", "maximum")},
		{keyword: "minimum", targets: numberTargets(), values: fixed("minimum", "1", "2")},
		{keyword: "exclusiveMinimum", targets: numberTargets(), values: exclusive("exclusiveMinimum", "minimum")},
		{keyword: "maxLength", targets: stringTargets(), values: fixed("maxLength", "9", "8")},
		{keyword: "minLength", targets: stringTargets(), values: fixed("minLength", "1", "2")},
		{keyword: "pattern", targets: stringTargets(), values: fixed("pattern", `"^a"`, `"^z"`)},
		{keyword: "maxItems", targets: arrayTargets(), values: fixed("maxItems", "9", "8")},
		{keyword: "minItems", targets: arrayTargets(), values: fixed("minItems", "1", "2")},
		{keyword: "uniqueItems", targets: arrayTargets(), values: fixed("uniqueItems", "true", "false")},
		{keyword: "maxProperties", targets: objectTargets(), values: fixed("maxProperties", "9", "8")},
		{keyword: "minProperties", targets: objectTargets(), values: fixed("minProperties", "1", "2")},
		{keyword: "required", targets: objectTargets(), values: fixed("required", `["a"]`, `["a", "b"]`)},
		{keyword: "enum", values: instances("enum", "[%s]")},
		{keyword: "type", targets: []string{unionTarget}, values: fixed("type", `"string"`, `"integer"`)},
		{keyword: "allOf", targets: []string{objectTarget}, values: fixed("allOf",
			`[{"type": "object", "properties": {"q": {"type": "integer"}}}]`,
			`[{"type": "object", "properties": {"q": {"type": "boolean"}}}]`)},
		{keyword: "oneOf", values: narrowing("oneOf")},
		{keyword: "anyOf", values: narrowing("anyOf")},
		{keyword: "not", values: fixed("not", `{"enum": ["n"]}`, `{"enum": ["z"]}`)},
		{keyword: "items", targets: arrayTargets(), values: fixed("items", `{"type": "integer"}`, `{"type": "boolean"}`)},
		{keyword: "properties", targets: objectTargets(), values: fixed("properties",
			`{"p": {"type": "string"}}`, `{"p": {"type": "boolean"}}`)},
		{keyword: "additionalProperties", targets: objectTargets(), values: fixed("additionalProperties",
			`{"type": "integer"}`, `{"type": "boolean"}`)},
		{keyword: "description", values: fixed("description", `"D"`, `"Z"`)},
		// Not of the formatted target: its body writes format already, so there
		// the construct would write the string target's documents again.
		{keyword: "format", targets: []string{stringTarget}, values: fixed("format", `"date"`, `"date-time"`)},
		{keyword: "default", values: instances("default", "%s")},
		{keyword: "readOnly", values: fixed("readOnly", "true", "false")},
		{keyword: "writeOnly", values: fixed("writeOnly", "true", "false")},
		{keyword: "deprecated", values: fixed("deprecated", "true", "false")},
	}
}

// openAPIConstructs are the fields OpenAPI itself defines on a Schema Object.
func openAPIConstructs() []construct {
	return []construct{
		{keyword: "nullable", span: only30, values: fixed("nullable", "true", "false")},
		// It routes the union written beside it (docs/compilers/openapi.md#allof-inline-residue).
		{keyword: "discriminator", targets: []string{objectTarget}, values: fixed("discriminator",
			`{"propertyName": "a"}`, `{"propertyName": "b"}`), stays: besideToo},
		{keyword: "xml", values: fixed("xml", `{"name": "n"}`, `{"name": "z"}`)},
		{keyword: "externalDocs", values: fixed("externalDocs",
			`{"url": "https://example.com/a"}`, `{"url": "https://example.com/z"}`)},
		{keyword: "example", values: instances("example", "%s")},
	}
}

// dialectConstructs are the keywords JSON Schema 2020-12 adds to those every
// version takes.
func dialectConstructs() []construct {
	return slices.Concat(dialectCore(), []construct{
		{keyword: "prefixItems", span: from31, targets: arrayTargets(), values: fixed("prefixItems",
			`[{"type": "integer"}]`, `[{"type": "boolean"}]`)},
		{keyword: "patternProperties", span: from31, targets: objectTargets(), values: fixed("patternProperties",
			`{"^a": {"type": "string"}}`, `{"^a": {"type": "boolean"}}`)},
		{keyword: "dependentSchemas", span: from31, targets: objectTargets(), values: fixed("dependentSchemas",
			`{"a": {"type": "object"}}`, `{"a": {"type": "array"}}`)},
		{keyword: "contains", span: from31, targets: arrayTargets(), values: fixed("contains",
			`{"type": "string"}`, `{"type": "boolean"}`)},
		{keyword: "propertyNames", span: from31, targets: objectTargets(), values: fixed("propertyNames",
			`{"maxLength": 4}`, `{"maxLength": 5}`)},
		{keyword: "if", span: from31, values: fixed("if", `{"const": "a"}`, `{"const": "z"}`)},
		{keyword: "then", span: from31, values: fixed("then", `{"const": "a"}`, `{"const": "z"}`)},
		{keyword: "else", span: from31, values: fixed("else", `{"const": "a"}`, `{"const": "z"}`)},
		// Each of the two applies to what no keyword beside it evaluates, so on an
		// entry it reads that entry alone (docs/compilers/openapi.md#allof-inline-residue).
		{keyword: "unevaluatedItems", span: from31, targets: arrayTargets(), values: fixed("unevaluatedItems",
			`{"type": "string"}`, `{"type": "boolean"}`), stays: besideToo},
		{keyword: "unevaluatedProperties", span: from31, targets: append(objectTargets(), stringTarget),
			values: fixed("unevaluatedProperties", "false", `{"type": "string"}`), stays: besideToo},
		{keyword: "contentSchema", span: from31, targets: []string{stringTarget}, values: fixed("contentSchema",
			`{"type": "object"}`, `{"type": "array"}`)},
		{keyword: "const", span: from31, values: instances("const", "%s")},
		{keyword: "dependentRequired", span: from31, targets: objectTargets(), values: fixed("dependentRequired",
			`{"a": ["b"]}`, `{"a": ["c"]}`)},
		{keyword: "maxContains", span: from31, targets: arrayTargets(), values: fixed("maxContains", "9", "8")},
		{keyword: "minContains", span: from31, targets: arrayTargets(), values: fixed("minContains", "1", "2")},
		{keyword: "contentEncoding", span: from31, targets: []string{stringTarget}, values: fixed("contentEncoding",
			`"base64"`, `"base32"`)},
		{keyword: "contentMediaType", span: from31, targets: []string{stringTarget}, values: fixed("contentMediaType",
			`"image/png"`, `"text/csv"`)},
		{keyword: "examples", span: from31, values: instances("examples", "[%s]")},
	})
}

// dialectCore are the core vocabulary's keywords 2020-12 adds, among them the
// four nothing carries. Each that stays beside the use identifies or configures
// the schema resource it is written in, so it is no conjunct of the use, beside
// it or on an entry (docs/compilers/openapi.md#dialect-keywords).
func dialectCore() []construct {
	return []construct{
		// Beside the use's $ref it is the base that $ref resolves against, so the
		// use names another schema (docs/compilers/openapi.md#references).
		{keyword: "$id", span: from31, values: fixed("$id", `"urn:example:a"`, `"urn:example:z"`), stays: inlineAlone},
		{keyword: "$schema", span: from31, values: fixed("$schema",
			`"https://json-schema.org/draft/2020-12/schema"`, `"https://json-schema.org/draft/2019-09/schema"`),
			stays: besideToo},
		{keyword: "$anchor", span: from31, values: fixed("$anchor", `"a"`, `"z"`), stays: besideToo,
			reason: "read as a reference target: the binder resolves $ref: '#a' to the schema " +
				"declaring it (docs/compilers/openapi.md#references), so the reference carries it; " +
				"declaring one says nothing about the shape"},
		{keyword: "$dynamicRef", span: from31, values: fixed("$dynamicRef", `"#m"`, `"#n"`),
			components: object{
				{"AnchorM", raw(`{"$dynamicAnchor": "m", "type": "string"}`)},
				{"AnchorN", raw(`{"$dynamicAnchor": "n", "type": "integer"}`)},
			}},
		{keyword: "$dynamicAnchor", span: from31, values: fixed("$dynamicAnchor", `"m"`, `"n"`), stays: besideToo,
			reason: "read as a reference target by the binder, which is what lets a " +
				"$dynamicRef expand (docs/compilers/openapi.md#dynamic-ref); declaring one says nothing about the shape"},
		{keyword: "$vocabulary", span: from31, values: fixed("$vocabulary",
			`{"https://json-schema.org/draft/2020-12/vocab/core": true}`,
			`{"https://json-schema.org/draft/2020-12/vocab/validation": true}`)},
		{keyword: "$comment", span: from31, values: fixed("$comment", `"note"`, `"other"`), stays: besideToo,
			reason: "2020-12 §8.3 forbids presenting it to end users, so an SDK emitter must never see it"},
		{keyword: "$defs", span: from31, values: fixed("$defs", `{"D": {"type": "string"}}`, `{"D": {"type": "integer"}}`),
			stays: besideToo,
			reason: "declares nothing about the enclosing shape; a member is lowered on demand " +
				"when a $ref names it (resolveSchemaRef -> hoistSubSchema)"},
	}
}
