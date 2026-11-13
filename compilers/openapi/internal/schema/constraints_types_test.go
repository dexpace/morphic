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

// TestConstraints_APositionDeclaringNoTypeReadsEveryBound pins a schema that
// says no instance's type: every bound reaches the position's Constraints.
func TestConstraints_APositionDeclaringNoTypeReadsEveryBound(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, atEverySite("3.1.0", "{minimum: 1, maxLength: 2, minProperties: 1}"))
	openapitest.RequireNoErrorDiags(t, diags)
	for _, site := range sitesOf(t, doc) {
		for _, keyword := range []string{"minimum", "maxLength", "minProperties"} {
			assert.True(t, site.holds(keyword), "%s %s", site.pointer, keyword)
			assert.Zero(t, site.keeps(keyword), "%s %s", site.pointer, keyword)
		}
	}
	assert.Empty(t, diags)
}

// TestConstraints_APositionOfNullAloneReadsNoBound pins a schema whose type is
// null alone, which no value bound applies to: no Constraints holds one, the
// carrier's included, and the Enum with no member the position lowers to
// keeps each once, with one info (docs/compilers/openapi.md#null-alone).
func TestConstraints_APositionOfNullAloneReadsNoBound(t *testing.T) {
	t.Parallel()
	for _, schema := range []string{"{type: 'null', minimum: 1, maxLength: 2, minProperties: 1}",
		"{type: ['null'], minimum: 1, maxLength: 2, minProperties: 1}"} {
		doc, diags := parseFull(t, atEverySite("3.1.0", schema))
		openapitest.RequireNoErrorDiags(t, diags)
		for _, site := range sitesOf(t, doc) {
			_, isEnum := site.node.(*ir.Enum)
			assert.True(t, isEnum, "%s %s: got %T", schema, site.pointer, site.node)
			for _, keyword := range []string{"minimum", "maxLength", "minProperties"} {
				assert.False(t, site.holds(keyword), "%s %s %s", schema, site.pointer, keyword)
				assert.Equal(t, 1, site.keeps(keyword), "%s %s %s", schema, site.pointer, keyword)
			}
			assert.Len(t, degradedAt(diags, site.pointer), 1, "%s %s", schema, site.pointer)
		}
		assert.Len(t, diags, 4, schema)
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

// TestConstraints_ACountATypeCannotTakeIsKeptByTheCensus pins a count past 64 bits
// beside a type that takes no count of its kind: the census keeps it once, with
// its own report, and the reader of the node's bounds keeps and reports nothing.
func TestConstraints_ACountATypeCannotTakeIsKeptByTheCensus(t *testing.T) {
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

// TestConstraints_AModifierATypeCannotTakeIsKeptByTheCensus pins a 3.0 exclusive
// modifier with no bound beside a string: no reader of numeric bounds reads it,
// so it draws no warning that it bounds nothing; the census keeps it once, with
// its info.
func TestConstraints_AModifierATypeCannotTakeIsKeptByTheCensus(t *testing.T) {
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

// TestConstraints_ACarrierRestatesTheBoundsItsNodeTakes pins the carrier of a
// position owning its node: a property and a parameter restate the value bounds
// the node's type takes, a Scalar hoisted for its format, a Model and the
// literals of an enum beside a type list alike, and hold none where the type list
// lowers to its variants, which hold each (docs/compilers/openapi.md#type-arrays).
func TestConstraints_ACarrierRestatesTheBoundsItsNodeTakes(t *testing.T) {
	t.Parallel()
	for schema, want := range map[string]*ir.Constraints{
		"{type: string, format: x-mine, maxLength: 3, minimum: 1}": {MaxLength: new(int64(3))},
		"{type: object, minProperties: 1, maxLength: 3}":           {MinProps: new(int64(1))},
		"{type: [string, integer], maxLength: 3, minimum: 1}":      nil,
		"{type: [string, integer], format: x-mine, maxLength: 3}":  nil,
		"{type: [string, integer], enum: [a, 1], maxLength: 3}":    {MaxLength: new(int64(3))},
	} {
		doc, diags := parseFull(t, atEverySite("3.1.0", schema))
		openapitest.RequireNoErrorDiags(t, diags)
		p := propertyOf(t, doc, "Holder", "p")
		assert.Equal(t, want, p.Constraints, "property %s", schema)
		q, ok := openapitest.ParamBySource(openapitest.FirstOp(t, doc.Services[0]), "q")
		require.True(t, ok)
		assert.Equal(t, want, q.Constraints, "parameter %s", schema)
	}
}

// TestConstraints_ABranchReadsTheBoundsItsOwnTypeTakes pins a value bound on an
// allOf branch folding at a position, where the branch declares a type the bound
// does not apply to: no Constraints in the document holds it, whichever branch
// the position lowers by, so the IR does not turn on the branches' order, and
// the branch keeps it. Neither a type array's variant whose type takes the bound
// nor a composed Model reads it.
func TestConstraints_ABranchReadsTheBoundsItsOwnTypeTakes(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ schema, keyword, branch string }{
		{"{allOf: [{type: string, minimum: 1}, {type: string, minimum: 2}]}", "minimum", "openapi:allOf/1"},
		{"{allOf: [{type: string, minimum: 2}, {type: string, minimum: 1}]}", "minimum", "openapi:allOf/1"},
		{"{type: [string, integer], format: x-mine, allOf: [{type: string, minimum: 1}]}", "minimum", "openapi:allOf/0"},
		{"{allOf: [{$ref: '#/components/schemas/O'}, {type: object, properties: {a: {type: string}}, maxLength: 3}]}",
			"maxLength", "openapi:allOf/1"},
	} {
		doc, diags := parseFull(t, atEverySite("3.1.0", tc.schema)+"    O: {properties: {o: {type: string}}}\n")
		openapitest.RequireNoErrorDiags(t, diags)
		for id, td := range doc.Types {
			assert.False(t, boundField(heldBounds(td), tc.keyword), "%s: %s", tc.schema, id)
		}
		for _, site := range sitesOf(t, doc) {
			assert.False(t, site.holds(tc.keyword), "%s: %s", tc.schema, site.pointer)
		}
		assert.Contains(t, marshalForTest(t, doc), `"`+tc.branch+`"`, tc.schema)
	}
}

// TestConstraints_ABranchCountItsTypeCannotTakeIsKeptWithIt pins a count past 64
// bits on a folding allOf branch declaring a type that takes no count of its
// kind: the branch keeps it, once, with the report of a branch kept whole, and
// it is not kept again as a count no field states.
func TestConstraints_ABranchCountItsTypeCannotTakeIsKeptWithIt(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, openapitest.ComponentSpec("    Num: {type: number}\n"+
		"    C: {allOf: [{$ref: '#/components/schemas/Num'}, {type: number, maxLength: 99999999999999999999}]}\n"))
	openapitest.RequireNoErrorDiags(t, diags)
	assert.Equal(t, []string{"openapi:allOf/1"}, unmodeledKeys(typeByName(doc, "C").Common().Unmodeled))
	assert.Equal(t, 1, strings.Count(marshalForTest(t, doc), "99999999999999999999"))
	require.Len(t, diags, 1, "%+v", diags)
	assert.Contains(t, diags[0].Message, "the branch (maxLength)")
}
