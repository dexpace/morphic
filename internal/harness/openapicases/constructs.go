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
	values func(t target, v version) [2]object
	// components are the component schemas the values name.
	components object
	// reason says why nothing carries the keyword; empty for one carried.
	reason string
}

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

// holds reports whether s writes at v.
func (s span) holds(v version) bool {
	switch s {
	case only30:
		return !v.dialect
	case from31:
		return v.dialect
	default:
		return s == every
	}
}

// saidOf reports whether c is said of t.
func (c construct) saidOf(t target) bool {
	return c.targets == nil || slices.Contains(c.targets, t.name)
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
	return func(t target, _ version) [2]object {
		at := func(i int) object { return object{{keyword, raw(fmt.Sprintf(layout, t.instances[i]))}} }
		return [2]object{at(0), at(1)}
	}
}

// exclusive is the values of an exclusive bound: before 2020-12 a flag on the
// bound beside it, true and false; from it a number of its own.
func exclusive(keyword, bound string) func(target, version) [2]object {
	return func(_ target, v version) [2]object {
		if v.dialect {
			return [2]object{{{keyword, raw("9")}}, {{keyword, raw("8")}}}
		}
		return [2]object{{{bound, raw("9")}, {keyword, raw("true")}}, {{bound, raw("9")}, {keyword, raw("false")}}}
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
		{keyword: "$ref", values: fixed("$ref", `"#/components/schemas/RefString"`, `"#/components/schemas/RefInteger"`),
			components: object{{"RefString", raw(`{"type": "string"}`)}, {"RefInteger", raw(`{"type": "integer"}`)}}},
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
		{keyword: "oneOf", values: fixed("oneOf", `[{"type": "string"}, {"type": "boolean"}]`,
			`[{"type": "string"}, {"type": "number"}]`)},
		{keyword: "anyOf", values: fixed("anyOf", `[{"type": "string"}, {"type": "boolean"}]`,
			`[{"type": "string"}, {"type": "number"}]`)},
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
		{keyword: "discriminator", targets: []string{objectTarget}, values: fixed("discriminator",
			`{"propertyName": "a"}`, `{"propertyName": "b"}`)},
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
		{keyword: "unevaluatedItems", span: from31, targets: arrayTargets(), values: fixed("unevaluatedItems",
			`{"type": "string"}`, `{"type": "boolean"}`)},
		{keyword: "unevaluatedProperties", span: from31, targets: append(objectTargets(), stringTarget),
			values: fixed("unevaluatedProperties", "false", `{"type": "string"}`)},
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
// four nothing carries.
func dialectCore() []construct {
	return []construct{
		{keyword: "$id", span: from31, values: fixed("$id", `"urn:example:a"`, `"urn:example:z"`)},
		{keyword: "$schema", span: from31, values: fixed("$schema",
			`"https://json-schema.org/draft/2020-12/schema"`, `"https://json-schema.org/draft/2019-09/schema"`)},
		{keyword: "$anchor", span: from31, values: fixed("$anchor", `"a"`, `"z"`),
			reason: "read as a reference target: the binder resolves $ref: '#a' to the schema " +
				"declaring it (docs/compilers/openapi.md#references), so the reference carries it; " +
				"declaring one says nothing about the shape"},
		{keyword: "$dynamicRef", span: from31, values: fixed("$dynamicRef", `"#m"`, `"#n"`),
			components: object{
				{"AnchorM", raw(`{"$dynamicAnchor": "m", "type": "string"}`)},
				{"AnchorN", raw(`{"$dynamicAnchor": "n", "type": "integer"}`)},
			}},
		{keyword: "$dynamicAnchor", span: from31, values: fixed("$dynamicAnchor", `"m"`, `"n"`),
			reason: "read as a reference target by the binder, which is what lets a " +
				"$dynamicRef expand (docs/compilers/openapi.md#dynamic-ref); declaring one says nothing about the shape"},
		{keyword: "$vocabulary", span: from31, values: fixed("$vocabulary",
			`{"https://json-schema.org/draft/2020-12/vocab/core": true}`,
			`{"https://json-schema.org/draft/2020-12/vocab/validation": true}`)},
		{keyword: "$comment", span: from31, values: fixed("$comment", `"note"`, `"other"`),
			reason: "2020-12 §8.3 forbids presenting it to end users, so an SDK emitter must never see it"},
		{keyword: "$defs", span: from31, values: fixed("$defs", `{"D": {"type": "string"}}`, `{"D": {"type": "integer"}}`),
			reason: "declares nothing about the enclosing shape; a member is lowered on demand " +
				"when a $ref names it (resolveSchemaRef -> hoistSubSchema)"},
	}
}
