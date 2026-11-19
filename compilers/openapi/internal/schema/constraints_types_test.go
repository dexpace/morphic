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
// written, the node the position owns, nil where it owns none, and whether it
// has a carrier, with the Constraints and Unmodeled of the carrier.
type boundSite struct {
	pointer string
	node    ir.TypeDef
	carrier bool
	held    *ir.Constraints
	kept    ir.Unmodeled
}

// sitesOf returns the four positions of an atEverySite document.
func sitesOf(t *testing.T, doc *ir.Document) []boundSite {
	t.Helper()
	property := propertyOf(t, doc, "Holder", "p")
	parameter, ok := openapitest.ParamBySource(openapitest.FirstOperation(t, doc.Services[0]), "q")
	require.True(t, ok)
	owned := func(id ir.TypeID, ref ir.TypeRef) ir.TypeDef {
		if ref.Target != id {
			return nil
		}
		return doc.Types[id]
	}
	return []boundSite{
		{pointer: "/components/schemas/C", node: typeByName(doc, "C")},
		{pointer: "/components/schemas/Holder/properties/p", carrier: true, held: property.Constraints, kept: property.Unmodeled,
			node: owned("type/anonymous/components/schemas/Holder/properties/p", property.Type)},
		{pointer: "/components/schemas/Items/items", node: doc.Types["type/anonymous/components/schemas/Items/items"]},
		{pointer: "/paths/~1a/get/parameters/0/schema", carrier: true, held: parameter.Constraints, kept: schemaKept(parameter.Unmodeled),
			node: owned("type/anonymous/paths/~1a/get/parameters/0/schema", parameter.Type)},
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

// boundField reports whether constraints holds the field the value bound
// keyword fills.
func boundField(constraints *ir.Constraints, keyword string) bool {
	if constraints == nil {
		return false
	}
	switch keyword {
	case "minimum":
		return constraints.Min != nil
	case "maximum":
		return constraints.Max != nil
	case "exclusiveMinimum":
		return constraints.ExclusiveMin != nil
	case "exclusiveMaximum":
		return constraints.ExclusiveMax != nil
	case "multipleOf":
		return constraints.MultipleOf != nil
	case "minLength":
		return constraints.MinLength != nil
	case "maxLength":
		return constraints.MaxLength != nil
	case "pattern":
		return constraints.Pattern != ""
	case "minProperties":
		return constraints.MinProperties != nil
	case "maxProperties":
		return constraints.MaxProperties != nil
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
// each of the six types, a number hoisted as a Scalar for its format, and a
// string or integer a format encodes as an instant or bytes, at a component, a
// property, items and a parameter: it
// reaches Constraints exactly where the type is one it applies to, and is kept
// verbatim under openapi:<keyword> once, with one info, everywhere else
// (docs/compilers/openapi.md#constraints).
func TestConstraints_ABoundIsReadWhereItsTypeTakesIt(t *testing.T) {
	t.Parallel()
	shapes := map[string]struct{ typ, shape string }{
		"string": {"string", "type: string"}, "integer": {"integer", "type: integer"},
		"number": {"number", "type: number"}, "boolean": {"boolean", "type: boolean"},
		"object": {"object", "type: object"}, "array": {"array", "type: array, items: {type: string}"},
		"formatted number":      {"number", "type: number, format: x-mine"},
		"string as an instant":  {"string", "type: string, format: http-date"},
		"string as bytes":       {"string", "type: string, format: base64url"},
		"integer as an instant": {"integer", "type: integer, format: unixtime"},
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
					infos := diagsAtPointer(diags, diag.DegradedConstruct, site.pointer)
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
		infos := diagsAtPointer(diags, diag.DegradedConstruct, site.pointer)
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
			assert.Len(t, diagsAtPointer(diags, diag.DegradedConstruct, site.pointer), 1, "%s %s", schema, site.pointer)
		}
		assert.Len(t, diags, 4, schema)
	}
}

// TestConstraints_AReferenceOutcomeReadsByTheTypesItNames pins the positions
// lowering to a reference: a $ref site, a collapsed union, a refinement and an
// expanded $dynamicRef. A bound is held where the type the position names takes
// it and kept and reported where it does not, and one declaring a type beside
// its $ref or its allOf reads by that type as well
// (docs/compilers/openapi.md#constraints).
func TestConstraints_AReferenceOutcomeReadsByTheTypesItNames(t *testing.T) {
	t.Parallel()
	const targets = "    Str: {type: string}\n    Arr: {type: array, items: {type: string}}\n" +
		"    Node: {$dynamicAnchor: node, type: string}\n"
	stringReference := "'#/components/schemas/Str'"
	arr := "'#/components/schemas/Arr'"
	for _, tc := range []struct {
		name, schema string
		held, kept   []string
	}{
		{"a $ref site", "{$ref: " + stringReference + ", minimum: 1, maxLength: 2}", []string{"maxLength"}, []string{"minimum"}},
		{"a collapsed union", "{anyOf: [{$ref: " + stringReference + "}, {type: 'null'}], minimum: 1, maxLength: 2}",
			[]string{"maxLength"}, []string{"minimum"}},
		{"a refinement", "{allOf: [{$ref: " + stringReference + "}], minimum: 1, maxLength: 2}", []string{"maxLength"}, []string{"minimum"}},
		{"an expanded $dynamicRef", "{$dynamicRef: '#node', minimum: 1, maxLength: 2}",
			[]string{"maxLength"}, []string{"minimum"}},
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
					message := openapitest.DiagMessageAt(t, diags, diag.DegradedConstruct, ir.SeverityInfo, site.pointer)
					assert.Regexp(t, "no home for .*"+keyword, message, site.pointer)
				}
			}
		})
	}
}

// heldCount reports whether the collection count keyword reached a
// Constraints field at the site, its node's or its carrier's.
func heldCount(boundSiteValue boundSite, keyword string) bool {
	has := func(constraints *ir.Constraints) bool {
		return constraints != nil && ((keyword == "maxItems" && constraints.MaxItems != nil) || (keyword == "minItems" && constraints.MinItems != nil))
	}
	return has(boundSiteValue.held) || (boundSiteValue.node != nil && has(heldBounds(boundSiteValue.node)))
}

// TestConstraints_ABoundAListKeepsIsKeptOnce pins a value bound beside an
// array at a property: the List keeps it, with its report, and the property
// holds none of it, so it is in the document once.
func TestConstraints_ABoundAListKeepsIsKeptOnce(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, openapitest.ComponentSpec(
		"    Holder: {type: object, properties: {a: {type: array, items: {type: string}, maxLength: 5}}}\n"))
	openapitest.RequireNoErrorDiags(t, diags)
	property := propertyOf(t, doc, "Holder", "a")
	assert.Nil(t, property.Constraints)
	assert.Empty(t, property.Unmodeled)
	list, ok := doc.Types[property.Type.Target].(*ir.List)
	require.True(t, ok, "got %T", doc.Types[property.Type.Target])
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
	for _, diagnostic := range diags {
		assert.Equal(t, ir.SeverityInfo, diagnostic.Severity, "no warning from a reader")
	}
}

// TestConstraints_ACarrierOwningItsNodeHoldsNoBound pins the carrier of a
// position owning its node: a property and a parameter hold no bound, and the
// node holds those its kind takes, a Scalar hoisted for its format, a Model and
// a union alike, so each bound is written once. The Union a type list lowers to
// holds none, its variants holding each (docs/compilers/openapi.md#type-arrays),
// and neither does one beside which no branch takes the bound.
func TestConstraints_ACarrierOwningItsNodeHoldsNoBound(t *testing.T) {
	t.Parallel()
	for schema, want := range map[string]*ir.Constraints{
		"{type: string, format: x-mine, maxLength: 3, minimum: 1}":     {MaxLength: new(int64(3))},
		"{type: object, minProperties: 1, maxLength: 3}":               {MinProperties: new(int64(1))},
		"{oneOf: [{type: string}, {type: integer}], maxLength: 3}":     {MaxLength: new(int64(3))},
		"{type: [string, integer], maxLength: 3, minimum: 1}":          nil,
		"{type: [string, integer], format: x-mine, maxLength: 3}":      nil,
		"{type: [string, integer], enum: [a, 1], maxLength: 3}":        nil,
		"{oneOf: [{type: string}, {type: integer}], minProperties: 1}": nil,
	} {
		doc, diags := parseFull(t, atEverySite("3.1.0", schema))
		openapitest.RequireNoErrorDiags(t, diags)
		for _, site := range sitesOf(t, doc) {
			if !site.carrier {
				continue
			}
			require.NotNil(t, site.node, "%s: %s owns its node", schema, site.pointer)
			assert.Nil(t, site.held, "the carrier at %s: %s", site.pointer, schema)
			assert.Equal(t, want, heldBounds(site.node), "the node at %s: %s", site.pointer, schema)
		}
	}
}

// TestConstraints_ABoundBesideAValueSetOrAUnionHasOneHome pins a value bound
// written beside a union, a value set or a branch folding into either, at every
// site atEverySite writes: a oneOf or anyOf holds it in its Union's
// Constraints, whether or not its branches list values, and an Enum, a Literal
// or the Union of literals a mixed enum lowers to keeps it verbatim, once, with
// one report. A carrier holds none either way (GitHub #1924, #1988).
func TestConstraints_ABoundBesideAValueSetOrAUnionHasOneHome(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		schema string
		held   *ir.Constraints
		// kept is the key the node keeps the bound under where it holds none,
		// reported once at the site's pointer and under, naming bound.
		kept, under, bound string
	}{
		{schema: "{oneOf: [{type: string}, {type: integer}], minLength: 1}", held: &ir.Constraints{MinLength: new(int64(1))}},
		{schema: "{anyOf: [{type: string}, {type: 'null'}, {type: integer}], minLength: 1}", held: &ir.Constraints{MinLength: new(int64(1))}},
		{schema: "{oneOf: [{type: string}, {type: integer}], allOf: [{maxLength: 9}], minLength: 1}",
			held: &ir.Constraints{MinLength: new(int64(1)), MaxLength: new(int64(9))}},
		{schema: "{oneOf: [{enum: [a, bb]}, {type: integer}], maxLength: 1}", held: &ir.Constraints{MaxLength: new(int64(1))}},
		{schema: "{oneOf: [{const: a}, {const: bb}], maxLength: 1}", held: &ir.Constraints{MaxLength: new(int64(1))}},
		{schema: "{anyOf: [{const: 1}, {enum: [2, 3]}], maximum: 1}", held: &ir.Constraints{Max: new(ir.BigVal("1"))}},
		{schema: "{type: string, enum: [a, bb], minLength: 1}", kept: "minLength", bound: "minLength"},
		{schema: "{type: [string, 'null'], enum: [a, null], minLength: 1}", kept: "minLength", bound: "minLength"},
		{schema: "{const: abc, minLength: 1}", kept: "minLength", bound: "minLength"},
		{schema: "{enum: [a, 1], minLength: 1}", kept: "minLength", bound: "minLength"},
		{schema: "{type: string, enum: [a, bb], allOf: [{maxLength: 9}]}", kept: "maxLength", under: "/allOf/0", bound: "maxLength"},
	} {
		doc, diags := parseFull(t, atEverySite("3.1.0", tc.schema))
		openapitest.RequireNoErrorDiags(t, diags)
		for _, site := range sitesOf(t, doc) {
			require.NotNil(t, site.node, "%s: %s owns its node", tc.schema, site.pointer)
			if tc.held != nil {
				assert.Equal(t, tc.held, heldBounds(site.node), "%s: %s", tc.schema, site.pointer)
				assert.Empty(t, site.node.Common().Unmodeled, "%s: %s", tc.schema, site.pointer)
				assert.Nil(t, site.held, "%s: the carrier at %s", tc.schema, site.pointer)
				assert.Empty(t, diagsAtPointer(diags, diag.DegradedConstruct, site.pointer), "%s: %s", tc.schema, site.pointer)
				continue
			}
			assert.Nil(t, site.held, "%s: the carrier at %s", tc.schema, site.pointer)
			assert.Nil(t, heldBounds(site.node), "%s: %s", tc.schema, site.pointer)
			assert.Equal(t, 1, site.keeps(tc.kept), "%s: %s", tc.schema, site.pointer)
			var reports int
			for _, diagnostic := range diagsAtPointer(diags, diag.DegradedConstruct, site.pointer+tc.under) {
				if strings.Contains(diagnostic.Message, tc.bound) {
					reports++
				}
			}
			assert.Equal(t, 1, reports, "%s: %s", tc.schema, site.pointer)
		}
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
		{"{allOf: [{type: string, minimum: 2}, {type: string, minimum: 1}]}", "minimum", "openapi:allOf/0"},
		{"{type: [string, integer], format: x-mine, allOf: [{type: string, minimum: 1}]}", "minimum", "openapi:allOf/0"},
		{"{allOf: [{$ref: '#/components/schemas/O'}, {type: object, properties: {a: {type: string}}, maxLength: 3}]}",
			"maxLength", "openapi:maxLength"},
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
// kind: kept under its own key, once, with the report of a keyword with no
// home, and not kept again as a count no field states.
func TestConstraints_ABranchCountItsTypeCannotTakeIsKeptWithIt(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, openapitest.ComponentSpec("    Num: {type: number}\n"+
		"    C: {allOf: [{$ref: '#/components/schemas/Num'}, {type: number, maxLength: 99999999999999999999}]}\n"))
	openapitest.RequireNoErrorDiags(t, diags)
	assert.Equal(t, []string{"openapi:maxLength"}, unmodeledKeys(typeByName(doc, "C").Common().Unmodeled))
	assert.Equal(t, 1, strings.Count(marshalForTest(t, doc), "99999999999999999999"))
	require.Len(t, diags, 1, "%+v", diags)
	assert.Contains(t, diags[0].Message, "no home for maxLength written on this inline branch")
}

// schemaKept returns what a parameter or a header keeps of what its schema
// writes, beneath openapi:schema/, keyed as a node of the schema's own keeps
// it, so one reading compares every carrier.
func schemaKept(unmodeled ir.Unmodeled) ir.Unmodeled {
	out := ir.Unmodeled{}
	for key, entry := range unmodeled {
		if rest, ok := strings.CutPrefix(key, "openapi:schema/"); ok {
			out["openapi:"+rest] = entry
		}
	}
	return out
}
