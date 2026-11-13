// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
)

// atEverySite writes schema at a component, a property, an array's items and a
// query parameter, in a document of the version given.
func atEverySite(version, schema string) string {
	return "openapi: " + version + "\ninfo: {title: T, version: \"1\"}\n" +
		"paths:\n  /a:\n    get:\n      parameters:\n        - {name: q, in: query, schema: " + schema + "}\n" +
		"      responses: {\"200\": {description: ok}}\n" +
		"components:\n  schemas:\n    C: " + schema + "\n" +
		"    Holder: {type: object, properties: {p: " + schema + "}}\n" +
		"    Items: {type: array, items: " + schema + "}\n"
}

// boundSite is one position atEverySite writes a schema at: where it is
// written, the node the position owns, nil where it owns none, and the
// Constraints and Unmodeled of its carrier, where it has one.
type boundSite struct {
	pointer string
	node    ir.TypeDef
	held    *ir.Constraints
	kept    ir.Unmodeled
}

// sitesOf returns the four positions of an atEverySite document.
func sitesOf(t *testing.T, doc *ir.Document) []boundSite {
	t.Helper()
	p := propertyOf(t, doc, "Holder", "p")
	q, ok := openapitest.ParamBySource(openapitest.FirstOp(t, doc.Services[0]), "q")
	require.True(t, ok)
	owned := func(id ir.TypeID, ref ir.TypeRef) ir.TypeDef {
		if ref.Target != id {
			return nil
		}
		return doc.Types[id]
	}
	return []boundSite{
		{pointer: "/components/schemas/C", node: typeByName(doc, "C")},
		{pointer: "/components/schemas/Holder/properties/p", held: p.Constraints, kept: p.Unmodeled,
			node: owned("t/anon/components/schemas/Holder/properties/p", p.Type)},
		{pointer: "/components/schemas/Items/items", node: doc.Types["t/anon/components/schemas/Items/items"]},
		{pointer: "/paths/~1a/get/parameters/0/schema", held: q.Constraints, kept: q.Unmodeled,
			node: owned("t/anon/paths/~1a/get/parameters/0/schema", q.Type)},
	}
}

// holds reports whether keyword reached a Constraints field at the site, its
// node's or its carrier's.
func (s boundSite) holds(keyword string) bool {
	return boundField(s.held, keyword) || (s.node != nil && boundField(heldBounds(s.node), keyword))
}

// keeps returns how many entries the site keeps keyword verbatim under, on its
// node and its carrier.
func (s boundSite) keeps(keyword string) int {
	n := 0
	if _, ok := s.kept["openapi:"+keyword]; ok {
		n++
	}
	if s.node != nil {
		if _, ok := s.node.Common().Unmodeled["openapi:"+keyword]; ok {
			n++
		}
	}
	return n
}

// boundField reports whether c holds the field the value bound keyword fills.
func boundField(c *ir.Constraints, keyword string) bool {
	if c == nil {
		return false
	}
	switch keyword {
	case "minimum":
		return c.Min != nil
	case "maximum":
		return c.Max != nil
	case "exclusiveMinimum":
		return c.ExclusiveMin != nil
	case "exclusiveMaximum":
		return c.ExclusiveMax != nil
	case "multipleOf":
		return c.MultipleOf != nil
	case "minLength":
		return c.MinLength != nil
	case "maxLength":
		return c.MaxLength != nil
	case "pattern":
		return c.Pattern != ""
	case "minProperties":
		return c.MinProps != nil
	case "maxProperties":
		return c.MaxProps != nil
	default:
		return false
	}
}

// boundTypes are the types each value bound applies to, as JSON Schema
// Validation 2020-12 §6.2, §6.3 and §6.5 group them, written apart from the
// table the compiler reads so a row moved there disagrees with this one.
var boundTypes = map[string][]string{
	"minimum":          {"integer", "number"},
	"maximum":          {"integer", "number"},
	"exclusiveMinimum": {"integer", "number"},
	"exclusiveMaximum": {"integer", "number"},
	"multipleOf":       {"integer", "number"},
	"minLength":        {"string"},
	"maxLength":        {"string"},
	"pattern":          {"string"},
	"minProperties":    {"object"},
	"maxProperties":    {"object"},
}

// boundLiteral is a value each value bound keyword is valid with.
func boundLiteral(keyword string) string {
	if keyword == "pattern" {
		return "'^a'"
	}
	return "2"
}

// TestConstraints_ABoundIsReadWhereItsTypeTakesIt pins every value bound beside
// each of the six types, and a number hoisted as a Scalar for its format, at a
// component, a property, items and a parameter: it
// reaches Constraints exactly where the type is one it applies to, and is kept
// verbatim under openapi:<keyword> once, with one info, everywhere else
// (docs/compilers/openapi.md#constraints).
func TestConstraints_ABoundIsReadWhereItsTypeTakesIt(t *testing.T) {
	t.Parallel()
	shapes := map[string]struct{ typ, shape string }{
		"string": {"string", "type: string"}, "integer": {"integer", "type: integer"},
		"number": {"number", "type: number"}, "boolean": {"boolean", "type: boolean"},
		"object": {"object", "type: object"}, "array": {"array", "type: array, items: {type: string}"},
		"formatted number": {"number", "type: number, format: x-mine"},
	}
	for keyword, takers := range boundTypes {
		for name, tc := range shapes {
			t.Run(keyword+"/"+name, func(t *testing.T) {
				t.Parallel()
				doc, diags := parseFull(t, atEverySite("3.1.0", "{"+tc.shape+", "+keyword+": "+boundLiteral(keyword)+"}"))
				openapitest.RequireNoErrorDiags(t, diags)
				taken := slices.Contains(takers, tc.typ)
				for _, site := range sitesOf(t, doc) {
					assert.Equal(t, taken, site.holds(keyword), site.pointer)
					infos := degradedAt(diags, site.pointer)
					if taken {
						assert.Zero(t, site.keeps(keyword), site.pointer)
						assert.Empty(t, infos, site.pointer)
						continue
					}
					assert.Equal(t, 1, site.keeps(keyword), site.pointer)
					require.Len(t, infos, 1, site.pointer)
					assert.Equal(t, ir.SeverityInfo, infos[0].Severity)
					assert.Contains(t, infos[0].Message, "no home for "+keyword, site.pointer)
				}
			})
		}
	}
}

// TestConstraints_SeveralBoundsATypeCannotTakeDrawOneReport pins a position
// writing bounds of three groups none of which its type takes: each is kept,
// with one info naming them all.
func TestConstraints_SeveralBoundsATypeCannotTakeDrawOneReport(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, atEverySite("3.1.0", "{type: boolean, minimum: 1, maxLength: 2, minProperties: 1}"))
	openapitest.RequireNoErrorDiags(t, diags)
	for _, site := range sitesOf(t, doc) {
		for _, keyword := range []string{"minimum", "maxLength", "minProperties"} {
			assert.False(t, site.holds(keyword), site.pointer)
			assert.Equal(t, 1, site.keeps(keyword), site.pointer)
		}
		infos := degradedAt(diags, site.pointer)
		require.Len(t, infos, 1, site.pointer)
		assert.Contains(t, infos[0].Message, "no home for maxLength, minProperties, minimum")
	}
	assert.Len(t, diags, 4)
}

// TestConstraints_APositionDeclaringNoTypeReadsEveryBound pins the schemas that
// say no instance's type: one writing no type, and one writing null alone, which
// lowers as one writing none. Every bound reaches the position's Constraints.
func TestConstraints_APositionDeclaringNoTypeReadsEveryBound(t *testing.T) {
	t.Parallel()
	for _, schema := range []string{"{minimum: 1, maxLength: 2, minProperties: 1}",
		"{type: 'null', minimum: 1, maxLength: 2, minProperties: 1}"} {
		doc, diags := parseFull(t, atEverySite("3.1.0", schema))
		openapitest.RequireNoErrorDiags(t, diags)
		for _, site := range sitesOf(t, doc) {
			for _, keyword := range []string{"minimum", "maxLength", "minProperties"} {
				assert.True(t, site.holds(keyword), "%s %s %s", schema, site.pointer, keyword)
				assert.Zero(t, site.keeps(keyword), "%s %s %s", schema, site.pointer, keyword)
			}
		}
		assert.Empty(t, diags, schema)
	}
}

// TestConstraints_AReferenceOutcomeReadsByThePositionsOwnType pins the
// positions lowering to a reference: a $ref site, a collapsed union, a
// refinement and an expanded $dynamicRef. One declaring no type reads every
// bound, whatever its target is, and one declaring a type beside its $ref or
// its allOf reads by that type and keeps the rest, never by the target's type
// (docs/compilers/openapi.md#constraints).
func TestConstraints_AReferenceOutcomeReadsByThePositionsOwnType(t *testing.T) {
	t.Parallel()
	const targets = "    Str: {type: string}\n    Arr: {type: array, items: {type: string}}\n" +
		"    Node: {$dynamicAnchor: node, type: string}\n"
	str := "'#/components/schemas/Str'"
	arr := "'#/components/schemas/Arr'"
	for _, tc := range []struct {
		name, schema string
		held, kept   []string
	}{
		{"a $ref site", "{$ref: " + str + ", minimum: 1, maxLength: 2}", []string{"minimum", "maxLength"}, nil},
		{"a collapsed union", "{anyOf: [{$ref: " + str + "}, {type: 'null'}], minimum: 1, maxLength: 2}",
			[]string{"minimum", "maxLength"}, nil},
		{"a refinement", "{allOf: [{$ref: " + str + "}], minimum: 1, maxLength: 2}", []string{"minimum", "maxLength"}, nil},
		{"an expanded $dynamicRef", "{$dynamicRef: '#node', minimum: 1, maxLength: 2}",
			[]string{"minimum", "maxLength"}, nil},
		{"a $ref site with a type", "{$ref: " + arr + ", type: array, minimum: 1, maxItems: 2}",
			[]string{"maxItems"}, []string{"minimum"}},
		{"a refinement with a type", "{type: array, allOf: [{$ref: " + arr + "}], minLength: 1, maxItems: 2}",
			[]string{"maxItems"}, []string{"minLength"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, diags := parseFull(t, atEverySite("3.1.0", tc.schema)+targets)
			openapitest.RequireNoErrorDiags(t, diags)
			for _, site := range sitesOf(t, doc) {
				for _, keyword := range tc.held {
					assert.True(t, site.holds(keyword) || heldCount(site, keyword), "%s %s", site.pointer, keyword)
					assert.Zero(t, site.keeps(keyword), "%s %s", site.pointer, keyword)
				}
				for _, keyword := range tc.kept {
					assert.False(t, site.holds(keyword), "%s %s", site.pointer, keyword)
					assert.Equal(t, 1, site.keeps(keyword), "%s %s", site.pointer, keyword)
					msg := openapitest.DiagMessageAt(t, diags, diag.DegradedConstruct, ir.SeverityInfo, site.pointer)
					assert.Regexp(t, "no home for .*"+keyword, msg, site.pointer)
				}
			}
		})
	}
}

// heldCount reports whether the collection count keyword reached a
// Constraints field at the site, its node's or its carrier's.
func heldCount(s boundSite, keyword string) bool {
	has := func(c *ir.Constraints) bool {
		return c != nil && ((keyword == "maxItems" && c.MaxItems != nil) || (keyword == "minItems" && c.MinItems != nil))
	}
	return has(s.held) || (s.node != nil && has(heldBounds(s.node)))
}

// TestConstraints_ABoundAListKeepsIsKeptOnce pins a value bound beside an
// array at a property: the List keeps it, with its report, and the property
// holds none of it, so it is in the document once.
func TestConstraints_ABoundAListKeepsIsKeptOnce(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, openapitest.ComponentSpec(
		"    Holder: {type: object, properties: {a: {type: array, items: {type: string}, maxLength: 5}}}\n"))
	openapitest.RequireNoErrorDiags(t, diags)
	p := propertyOf(t, doc, "Holder", "a")
	assert.Nil(t, p.Constraints)
	assert.Empty(t, p.Unmodeled)
	list, ok := doc.Types[p.Type.Target].(*ir.List)
	require.True(t, ok, "got %T", doc.Types[p.Type.Target])
	assert.Equal(t, []string{"openapi:maxLength"}, unmodeledKeys(list.Unmodeled))
	assert.Equal(t, 1, strings.Count(marshalForTest(t, doc), `"openapi:maxLength"`))
	assert.Len(t, diags, 1)
}

// TestConstraints_ACountATypeCannotTakeIsTheCensuss pins a count past 64 bits
// beside a type that takes no count of its kind: the census keeps it once, with
// its own report, and the reader of the node's bounds keeps and reports nothing.
func TestConstraints_ACountATypeCannotTakeIsTheCensuss(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, atEverySite("3.1.0", "{type: integer, maxLength: 99999999999999999999}"))
	openapitest.RequireNoErrorDiags(t, diags)
	for _, site := range sitesOf(t, doc) {
		assert.Equal(t, 1, site.keeps("maxLength"), site.pointer)
		assert.Contains(t, openapitest.DiagMessageAt(t, diags, diag.DegradedConstruct, ir.SeverityInfo, site.pointer),
			"no home for maxLength")
	}
	assert.Len(t, diags, 4)
}

// TestConstraints_AModifierATypeCannotTakeIsTheCensuss pins a 3.0 exclusive
// modifier with no bound beside a string: no reader of numeric bounds reads it,
// so it draws no warning that it bounds nothing; the census keeps it once, with
// its info.
func TestConstraints_AModifierATypeCannotTakeIsTheCensuss(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, atEverySite("3.0.3", "{type: string, exclusiveMinimum: true}"))
	openapitest.RequireNoErrorDiags(t, diags)
	for _, site := range sitesOf(t, doc) {
		assert.Equal(t, 1, site.keeps("exclusiveMinimum"), site.pointer)
		assert.Contains(t, openapitest.DiagMessageAt(t, diags, diag.DegradedConstruct, ir.SeverityInfo, site.pointer),
			"no home for exclusiveMinimum")
	}
	require.Len(t, diags, 4, "%+v", diags)
	for _, d := range diags {
		assert.Equal(t, ir.SeverityInfo, d.Severity, "no warning from a reader")
	}
}
