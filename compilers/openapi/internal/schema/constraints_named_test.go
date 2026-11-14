// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema_test

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
)

// This file holds the rules for a value or collection bound written beside a
// schema that names another (docs/compilers/openapi.md#constraints): the bound
// is read by the instance types the position admits, its own and those of every
// schema it names through $ref, allOf or a union.

const (
	namedS = "/components/schemas/S"
	namedP = "/components/schemas/Holder/properties/p"
)

// namedTargets are the schemas the rows below name. They follow the subjects in
// a document, so a subject is lowered before its targets.
const namedTargets = `    Str: {type: string}
    Arr: {type: array, items: {type: string}}
    Obj: {type: object, properties: {k: {type: string}}}
    Via: {$ref: '#/components/schemas/Arr'}
    Mid: {allOf: [{$ref: '#/components/schemas/Arr'}]}
    Open: {description: declares no type}
`

// namedTargets31 are the targets only a 3.1 document can write.
const namedTargets31 = `    StrOrInt: {type: [string, integer]}
    TextOrList: {oneOf: [{type: string}, {type: array, items: {type: string}}]}
    Nil: {type: 'null'}
    NilAlias: {allOf: [{$ref: '#/components/schemas/Nil'}]}
    Node: {$dynamicAnchor: node, type: string}
`

// namedSpec writes schema as the component S and as the property p of Holder,
// beside the targets and extra, in a document of the version given. With
// targetsFirst the targets are declared before the subjects.
func namedSpec(version, schema, extra string, targetsFirst bool) string {
	targets := namedTargets
	if version == "3.1.0" {
		targets += namedTargets31
	}
	targets += extra
	subjects := "    S: " + schema + "\n    Holder: {type: object, properties: {p: " + schema + "}}\n"
	if targetsFirst {
		return openapitest.ComponentSpecVer(version, targets+subjects)
	}
	return openapitest.ComponentSpecVer(version, subjects+targets)
}

// refTo is a $ref to the component name.
func refTo(name string) string { return "{$ref: '#/components/schemas/" + name + "'}" }

// namedSite is one position a row's schema is written at: where, the
// Constraints it holds, and what it keeps verbatim.
type namedSite struct {
	at   string
	held *ir.Constraints
	kept ir.Unmodeled
	base ir.TypeID
}

// namedSites returns S and the property p of a namedSpec document.
func namedSites(t *testing.T, doc *ir.Document) []namedSite {
	t.Helper()
	s := typeByName(doc, "S")
	require.NotNil(t, s)
	p := propertyOf(t, doc, "Holder", "p")
	return []namedSite{
		{at: namedS, held: heldBounds(s), kept: s.Common().Unmodeled, base: baseOf(s)},
		{at: namedP, held: p.Constraints, kept: p.Unmodeled, base: p.Type.Target},
	}
}

// baseOf returns the type a scalar or a model is declared over, "" for any
// other node.
func baseOf(td ir.TypeDef) ir.TypeID {
	switch n := td.(type) {
	case *ir.Scalar:
		return n.Base.Target
	case *ir.Model:
		if n.Base != nil {
			return n.Base.Target
		}
	}
	return ""
}

// keptAt is an entry a position keeps under Unmodeled["openapi:"+key]. A key
// naming an allOf branch is reported at the branch, any other at the position.
type keptAt struct {
	key   string
	value string // the entry's JSON, "" where the row does not say
	msg   string // a substring of its report, "" for "no home for .*key"
}

// pointer returns where the entry and its report sit, beneath the site at.
func (k keptAt) pointer(at string) string { return at + "/" + k.key }

// reportAt returns where the report of the entry sits.
func (k keptAt) reportAt(at string) string {
	if strings.HasPrefix(k.key, "allOf/") {
		return k.pointer(at)
	}
	return at
}

// namedRow is one schema written at S and at p, with what each position holds
// and keeps.
type namedRow struct {
	name, version, schema, extra string
	// held is the Constraints S and p each carry, nil where none.
	held *ir.Constraints
	// kept are the entries each of S and p keeps, each with one info report.
	kept []keptAt
	// base is the type S is declared over, "" where the row does not say.
	base ir.TypeID
	// msgs are substrings the report at each position holds.
	msgs []string
	// keptOnS are further keys S alone keeps.
	keptOnS []string
	// diags is the number of diagnostics in all, 0 where the row does not say.
	diags int
	// quiet says the row reports nothing and keeps nothing.
	quiet bool
}

// keywordsOf returns the entries of kept that are a keyword's.
func keywordsOf(kept []keptAt) []string {
	var out []string
	for _, k := range kept {
		if !strings.HasPrefix(k.key, "allOf/") {
			out = append(out, k.key)
		}
	}
	return out
}

// assertNamedRow compiles the row and holds S and p to its facts.
func assertNamedRow(t *testing.T, tc namedRow) {
	t.Helper()
	version := tc.version
	if version == "" {
		version = "3.1.0"
	}
	doc, diags := parseFull(t, namedSpec(version, tc.schema, tc.extra, false))
	openapitest.RequireNoErrorDiags(t, diags)
	if tc.diags != 0 {
		assert.Len(t, diags, tc.diags, "%+v", diags)
	}
	if tc.quiet {
		assert.Empty(t, diags)
	}
	sites := namedSites(t, doc)
	if tc.base != "" {
		assert.Equal(t, tc.base, sites[0].base, "S")
	}
	for _, key := range tc.keptOnS {
		assert.Contains(t, sites[0].kept, "openapi:"+key, "S")
	}
	for _, site := range sites {
		assert.Equal(t, tc.held, site.held, site.at)
		for _, k := range tc.kept {
			entry, ok := site.kept["openapi:"+k.key]
			if !assert.True(t, ok, "%s keeps %s: got %v", site.at, k.key, unmodeledKeys(site.kept)) {
				continue
			}
			assert.Equal(t, ir.ReasonDegradedLowering, entry.Reason, "%s %s", site.at, k.key)
			assert.Equal(t, k.pointer(site.at), string(entry.Provenance.Pointer), "%s %s", site.at, k.key)
			if k.value != "" {
				assert.JSONEq(t, k.value, string(entry.Value), "%s %s", site.at, k.key)
			}
			msg := openapitest.DiagMessageAt(t, diags, diag.DegradedConstruct, ir.SeverityInfo, k.reportAt(site.at))
			if k.msg != "" {
				assert.Contains(t, msg, k.msg, "%s %s", site.at, k.key)
			} else {
				assert.Regexp(t, "no home for .*"+k.key, msg, "%s %s", site.at, k.key)
			}
		}
		if len(keywordsOf(tc.kept)) > 0 {
			msg := openapitest.DiagMessageAt(t, diags, diag.DegradedConstruct, ir.SeverityInfo, site.at)
			for _, want := range tc.msgs {
				assert.Contains(t, msg, want, site.at)
			}
		}
		if tc.quiet {
			assert.Empty(t, site.kept, site.at)
		}
	}
}

// TestConstraints_ABoundIsReadByTheTypesItsSchemaNames pins a bound written
// beside a schema that names another: it reaches Constraints only where every
// type the position admits takes it, and is otherwise kept verbatim and
// reported, whichever of $ref, allOf or a union names the other schema.
func TestConstraints_ABoundIsReadByTheTypesItsSchemaNames(t *testing.T) {
	t.Parallel()
	for _, tc := range []namedRow{
		{
			name: "row 1: a refinement of an array", schema: "{allOf: [" + refTo("Arr") + "], minLength: 1}",
			kept: []keptAt{{key: "minLength"}}, base: componentID("Arr"), diags: 2,
		},
		{
			name: "row 2: a $ref site", schema: "{$ref: '#/components/schemas/Arr', minLength: 1}",
			kept: []keptAt{{key: "minLength"}}, msgs: []string{"an alias over its $ref target"},
		},
		{
			name: "row 4: a collapsed nullable union", schema: "{anyOf: [" + refTo("Arr") + ", {enum: [null]}], minLength: 1}",
			kept: []keptAt{{key: "minLength"}}, msgs: []string{"a nullable reference to its one non-null branch"},
		},
		{
			name: "row 5: an expanded $dynamicRef", schema: "{$dynamicRef: '#node', minimum: 1, maxLength: 2}",
			held: &ir.Constraints{MaxLength: new(int64(2))}, kept: []keptAt{{key: "minimum"}}, base: componentID("Node"),
		},
		{
			name: "row 6: a refinement of an inline array", schema: "{allOf: [{type: array, items: {type: string}}], minLength: 1}",
			kept: []keptAt{{key: "minLength"}}, base: "t/anon/components/schemas/S/allOf/0",
		},
		{
			name: "row 7: a refinement of an inline string", schema: "{allOf: [{type: string}], minimum: 1, maxLength: 3}",
			held: &ir.Constraints{MaxLength: new(int64(3))}, kept: []keptAt{{key: "minimum"}}, base: "t/prim/string",
		},
		{
			name: "row 8: a $ref through an alias", schema: "{$ref: '#/components/schemas/Via', minLength: 1}",
			kept: []keptAt{{key: "minLength"}}, base: componentID("Via"),
		},
		{
			name: "row 9: a refinement through an allOf", schema: "{allOf: [" + refTo("Mid") + "], minLength: 1}",
			kept: []keptAt{{key: "minLength"}}, base: componentID("Mid"),
		},
		{
			name: "row 10: a type array", schema: "{allOf: [" + refTo("StrOrInt") + "], minProperties: 1, minLength: 2, minimum: 3}",
			held: &ir.Constraints{MinLength: new(int64(2)), Min: new(ir.BigVal("3"))}, kept: []keptAt{{key: "minProperties"}},
		},
		{
			name: "row 11: null alone", schema: "{allOf: [" + refTo("Nil") + "], minimum: 1}",
			kept: []keptAt{{key: "minimum"}},
		},
		{
			name: "row 12: an alias of null alone", schema: "{allOf: [" + refTo("NilAlias") + "], minimum: 1}",
			kept: []keptAt{{key: "minimum"}}, base: componentID("NilAlias"),
		},
		{
			name:   "row 13: a type beside a $ref that contradicts it",
			schema: "{$ref: '#/components/schemas/Arr', type: string, minLength: 1}",
			kept:   []keptAt{{key: "type", value: `"string"`}, {key: "minLength", value: "1"}},
			msgs:   []string{"type, minLength"},
		},
		{
			name:   "row 14: an allOf beside a oneOf",
			schema: "{allOf: [" + refTo("Arr") + "], oneOf: [{minItems: 1}, {maxItems: 0}], minLength: 1}",
			kept:   []keptAt{{key: "minLength"}}, keptOnS: []string{"oneOf"},
		},
		{
			name: "row 15: an allOf naming a union", schema: "{allOf: [" + refTo("TextOrList") + "], minimum: 1, minLength: 2}",
			held: &ir.Constraints{MinLength: new(int64(2))}, kept: []keptAt{{key: "minimum"}}, base: componentID("TextOrList"),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assertNamedRow(t, tc)
		})
	}
}

// TestConstraints_ABoundBesideARefIn30IsKeptAsIn31 pins row 3: the bound the
// version ignores is read by the types the schema names, as it is in 3.1, and
// is reported as ignored beside it.
func TestConstraints_ABoundBesideARefIn30IsKeptAsIn31(t *testing.T) {
	t.Parallel()
	tc := namedRow{
		version: "3.0.3", schema: "{$ref: '#/components/schemas/Arr', minLength: 1}",
		kept: []keptAt{{key: "minLength"}},
	}
	assertNamedRow(t, tc)

	_, diags := parseFull(t, namedSpec(tc.version, tc.schema, "", false))
	for _, at := range []string{namedS, namedP} {
		assert.Len(t, diagsAtPointer(diags, diag.LaterVersionConstruct, at+"/minLength"), 1, at)
	}
	assert.Len(t, withCode(diags, diag.LaterVersionConstruct), 2, "%+v", diags)
}

// TestConstraints_ABoundANamedTypeTakesIsRead holds the controls: a bound the
// types the position names take, or one beside a schema that names no type,
// stays held and draws no report.
func TestConstraints_ABoundANamedTypeTakesIsRead(t *testing.T) {
	t.Parallel()
	for _, tc := range []namedRow{
		{
			name: "row 16: a string takes string bounds", schema: "{allOf: [" + refTo("Str") + "], minLength: 2, pattern: '^a'}",
			held: &ir.Constraints{MinLength: new(int64(2)), Pattern: "^a"}, base: componentID("Str"), quiet: true,
		},
		{
			name:   "row 17: a schema declaring no type excludes none",
			schema: "{$ref: '#/components/schemas/Open', minimum: 1, minLength: 2, minProperties: 3, minItems: 4}",
			held: &ir.Constraints{
				Min: new(ir.BigVal("1")), MinLength: new(int64(2)), MinProps: new(int64(3)), MinItems: new(int64(4)),
			},
			quiet: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assertNamedRow(t, tc)
		})
	}
}

// TestConstraints_ATypeBesideANamedSchemaStillNarrowsTheRead pins row 18: the
// position's own type is read too, so the bound that type cannot take is kept
// beside one it can, whatever the named schema declares.
func TestConstraints_ATypeBesideANamedSchemaStillNarrowsTheRead(t *testing.T) {
	t.Parallel()
	assertNamedRow(t, namedRow{
		name: "row 18", schema: "{type: array, allOf: [" + refTo("Open") + "], minLength: 1, maxItems: 2}",
		held: &ir.Constraints{MaxItems: new(int64(2))}, base: componentID("Open"),
		kept: []keptAt{{key: "type"}, {key: "minLength"}}, msgs: []string{"type, minLength"},
	})
}

// TestConstraints_AUnionIsReadByWhatItsBranchesAdmit pins a bound beside a union
// that does not collapse: a type is admitted if any branch admits it, so a
// bound no branch takes is not held by the property, and one a branch takes is.
func TestConstraints_AUnionIsReadByWhatItsBranchesAdmit(t *testing.T) {
	t.Parallel()
	const union = "t/anon/components/schemas/Holder/properties/p"
	for _, tc := range []struct{ name, schema string }{
		{"row 19: an array or an object", "{anyOf: [" + refTo("Arr") + ", " + refTo("Obj") + "], minLength: 1}"},
		{"row 20: an array or null alone", "{anyOf: [" + refTo("Arr") + ", " + refTo("Nil") + "], minLength: 1}"},
		{"an array, an object or a null writing no type",
			"{anyOf: [" + refTo("Arr") + ", " + refTo("Obj") + ", {enum: [null]}], minLength: 1}"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, diags := parseFull(t, namedSpec("3.1.0", tc.schema, "", false))
			openapitest.RequireNoErrorDiags(t, diags)

			p := propertyOf(t, doc, "Holder", "p")
			assert.Equal(t, ir.TypeID(union), p.Type.Target)
			assert.Nil(t, p.Constraints)
			assert.Empty(t, p.Unmodeled)
			u, ok := doc.Types[p.Type.Target].(*ir.Union)
			require.True(t, ok, "got %T", doc.Types[p.Type.Target])
			entry, ok := u.Unmodeled["openapi:minLength"]
			require.True(t, ok, "got %v", unmodeledKeys(u.Unmodeled))
			assert.Equal(t, ir.ReasonDegradedLowering, entry.Reason)
			assert.Contains(t, openapitest.DiagMessageAt(t, diags, diag.DegradedConstruct, ir.SeverityInfo, namedP),
				`a node of kind "union"`)
		})
	}
	t.Run("row 21: a string or an object", func(t *testing.T) {
		t.Parallel()
		doc, diags := parseFull(t, namedSpec("3.1.0", "{anyOf: ["+refTo("Str")+", "+refTo("Obj")+"], minLength: 1}", "", false))
		openapitest.RequireNoErrorDiags(t, diags)
		assert.Equal(t, &ir.Constraints{MinLength: new(int64(1))}, propertyOf(t, doc, "Holder", "p").Constraints)
	})
}

// TestCollectionBound_IsReadByTheTypesItsSchemaNames pins the collection bounds
// beside a reference: minItems, maxItems and uniqueItems are held by its alias
// or carrier only where the position admits an array, and kept otherwise.
func TestCollectionBound_IsReadByTheTypesItsSchemaNames(t *testing.T) {
	t.Parallel()
	for _, tc := range []namedRow{
		{
			name: "row 22: a $ref to a string", schema: "{$ref: '#/components/schemas/Str', minItems: 1}",
			kept: []keptAt{{key: "minItems", value: "1"}}, msgs: []string{"an alias over its $ref target"},
		},
		{
			name: "row 23: a refinement of a string", schema: "{allOf: [" + refTo("Str") + "], minItems: 1, uniqueItems: true}",
			kept: []keptAt{{key: "minItems"}, {key: "uniqueItems"}}, msgs: []string{"minItems, uniqueItems"},
		},
		{
			name: "a $ref to an object", schema: "{$ref: '#/components/schemas/Obj', minItems: 1}",
			kept: []keptAt{{key: "minItems", value: "1"}},
		},
		{
			name: "a $ref to a tuple", schema: "{$ref: '#/components/schemas/Tup', minItems: 1}",
			extra: "    Tup: {type: array, prefixItems: [{type: string}]}\n",
			held:  &ir.Constraints{MinItems: new(int64(1))}, quiet: true,
		},
		{
			name: "row 24: a branch of a string", schema: "{allOf: [" + refTo("Str") + ", {maxItems: 2}]}",
			kept: []keptAt{{key: "allOf/1", value: `{"maxItems":2}`, msg: "(maxItems)"}},
		},
		{
			name: "row 25: a $ref to an array", schema: "{$ref: '#/components/schemas/Arr', minItems: 1}",
			held: &ir.Constraints{MinItems: new(int64(1))}, quiet: true,
		},
		{
			name:   "row 26: a branch of an array",
			schema: "{allOf: [" + refTo("Arr") + ", {items: {type: string}, minLength: 1, maxItems: 4}]}",
			held:   &ir.Constraints{MaxItems: new(int64(4))},
			kept: []keptAt{{
				key: "allOf/1", msg: "(minLength)",
				value: `{"items":{"type":"string"},"maxItems":4,"minLength":1}`,
			}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assertNamedRow(t, tc)
		})
	}
}

// TestConstraints_ABranchBoundIsReadByTheTypesItsSchemaNames pins a bound
// written on an inline allOf branch beside a branch naming a type: the branch
// is kept whole where the type cannot take its bound, in either branch order.
func TestConstraints_ABranchBoundIsReadByTheTypesItsSchemaNames(t *testing.T) {
	t.Parallel()
	for i, order := range [][]string{{refTo("Arr"), "{minLength: 1}"}, {"{minLength: 1}", refTo("Arr")}} {
		at := []int{1, 0}[i]
		t.Run(fmt.Sprintf("row 27: the bound is branch %d", at), func(t *testing.T) {
			t.Parallel()
			assertNamedRow(t, namedRow{
				schema: "{allOf: [" + strings.Join(order, ", ") + "]}", base: componentID("Arr"),
				kept: []keptAt{{key: fmt.Sprintf("allOf/%d", at), value: `{"minLength":1}`, msg: "(minLength)"}},
			})
		})
	}
	for n, order := range [][]string{
		{refTo("Str"), "{minLength: 2}", "{minimum: 3}"},
		{refTo("Str"), "{minimum: 3}", "{minLength: 2}"},
		{"{minLength: 2}", "{minimum: 3}", refTo("Str")},
		{"{minimum: 3}", "{minLength: 2}", refTo("Str")},
		{"{minLength: 2}", refTo("Str"), "{minimum: 3}"},
		{"{minimum: 3}", refTo("Str"), "{minLength: 2}"},
	} {
		at := slices.Index(order, "{minimum: 3}")
		t.Run(fmt.Sprintf("row 28: order %d puts the minimum at branch %d", n, at), func(t *testing.T) {
			t.Parallel()
			assertNamedRow(t, namedRow{
				schema: "{allOf: [" + strings.Join(order, ", ") + "]}", held: &ir.Constraints{MinLength: new(int64(2))},
				kept: []keptAt{{key: fmt.Sprintf("allOf/%d", at), value: `{"minimum":3}`, msg: "(minimum)"}},
			})
		})
	}
	for i, order := range [][]string{{refTo("Obj"), "{minLength: 1}"}, {"{minLength: 1}", refTo("Obj")}} {
		at := []int{1, 0}[i]
		t.Run(fmt.Sprintf("row 29: the bound is branch %d", at), func(t *testing.T) {
			t.Parallel()
			doc, diags := parseFull(t, namedSpec("3.1.0", "{allOf: ["+strings.Join(order, ", ")+"]}", "", false))
			openapitest.RequireNoErrorDiags(t, diags)

			s, ok := typeByName(doc, "S").(*ir.Model)
			require.True(t, ok, "got %T", typeByName(doc, "S"))
			require.NotNil(t, s.Base)
			assert.Equal(t, componentID("Obj"), s.Base.Target)
			assert.Nil(t, s.Constraints)
			key := fmt.Sprintf("openapi:allOf/%d", at)
			entry, ok := s.Unmodeled[key]
			require.True(t, ok, "got %v", unmodeledKeys(s.Unmodeled))
			assert.JSONEq(t, `{"minLength":1}`, string(entry.Value))
			msg := openapitest.DiagMessageAt(t, diags, diag.DegradedConstruct, ir.SeverityInfo,
				fmt.Sprintf("/components/schemas/S/allOf/%d", at))
			assert.Contains(t, msg, "inline allOf branch is merged in place")
			assert.Contains(t, msg, "(minLength)")
		})
	}
}

// TestAllOf_ACompositionReadsBoundsByTheTypesItComposes pins a bound beside an
// allOf of an object: the model it lowers to holds the bounds an object takes,
// and keeps and reports the others, at the component and at a property.
func TestAllOf_ACompositionReadsBoundsByTheTypesItComposes(t *testing.T) {
	t.Parallel()
	const schema = "{allOf: [{$ref: '#/components/schemas/Obj'}], minLength: 1, minimum: 2, minProperties: 3}"
	doc, diags := parseFull(t, namedSpec("3.1.0", schema, "", false))
	openapitest.RequireNoErrorDiags(t, diags)
	want := &ir.Constraints{MinProps: new(int64(3))}

	t.Run("row 30: the model holds what an object takes", func(t *testing.T) {
		t.Parallel()
		s, ok := typeByName(doc, "S").(*ir.Model)
		require.True(t, ok, "got %T", typeByName(doc, "S"))
		require.NotNil(t, s.Base)
		assert.Equal(t, componentID("Obj"), s.Base.Target)
		assert.Equal(t, want, s.Constraints)
	})
	t.Run("row 31: the model keeps what it cannot hold", func(t *testing.T) {
		t.Parallel()
		s := typeByName(doc, "S")
		assert.Subset(t, unmodeledKeys(s.Common().Unmodeled), []string{"openapi:minLength", "openapi:minimum"})
		msg := openapitest.DiagMessageAt(t, diags, diag.DegradedConstruct, ir.SeverityInfo, namedS)
		assert.Contains(t, msg, "minLength, minimum")
		assert.Contains(t, msg, `a node of kind "model"`)
	})
	t.Run("row 32: the node a property owns reads the same", func(t *testing.T) {
		t.Parallel()
		p := propertyOf(t, doc, "Holder", "p")
		assert.Equal(t, ir.TypeID("t/anon/components/schemas/Holder/properties/p"), p.Type.Target)
		assert.Equal(t, want, p.Constraints)
		assert.Empty(t, p.Unmodeled)
		node, ok := doc.Types[p.Type.Target].(*ir.Model)
		require.True(t, ok, "got %T", doc.Types[p.Type.Target])
		assert.Equal(t, want, node.Constraints)
		assert.Subset(t, unmodeledKeys(node.Unmodeled), []string{"openapi:minLength", "openapi:minimum"})
	})
}

// TestAllOf_AnEmptyConjunctionKeepsEveryBound pins row 33: the conjuncts of
// the named schema share no kind, so it admits none and none of the bounds is
// read, as it would be were the named schema null alone.
func TestAllOf_AnEmptyConjunctionKeepsEveryBound(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, namedSpec("3.1.0", "{allOf: ["+refTo("ArrAndObj")+"], minLength: 1}",
		"    ArrAndObj: {allOf: ["+refTo("Arr")+", "+refTo("Obj")+"]}\n", false))
	openapitest.RequireNoErrorDiags(t, diags)

	s, ok := typeByName(doc, "S").(*ir.Model)
	require.True(t, ok, "got %T", typeByName(doc, "S"))
	assert.Nil(t, s.Constraints)
	assert.Contains(t, s.Unmodeled, "openapi:minLength")
	assert.Nil(t, propertyOf(t, doc, "Holder", "p").Constraints)

	var warnings []ir.Diagnostic
	for _, d := range diags {
		if d.Severity == ir.SeverityWarning {
			warnings = append(warnings, d)
		}
	}
	require.Len(t, warnings, 1, "%+v", diags)
	assert.Equal(t, "/components/schemas/ArrAndObj", string(warnings[0].Provenance.Pointer))
}

// distributedSpec writes a union beside an allOf and the bounds the variants
// and the union are read for.
const distributedSpec = `openapi: 3.1.0
info: {title: t, version: "1"}
paths: {}
components:
  schemas:
    D:
      allOf: [{$ref: '#/components/schemas/Obj'}]
      oneOf:
        - {properties: {a: {type: string}}, required: [a]}
        - {properties: {b: {type: string}}, required: [b]}
      minLength: 1
      minProperties: 2
    Obj: {type: object, properties: {k: {type: string}}}
`

// TestUnion_ADistributedBodyReadsBoundsByTheTypesItComposes pins a bound beside
// a union distributed over an allOf: each variant holds the bounds an object
// takes, and the union keeps the one no branch takes, once, with its report.
func TestUnion_ADistributedBodyReadsBoundsByTheTypesItComposes(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, distributedSpec)
	openapitest.RequireNoErrorDiags(t, diags)

	t.Run("row 34: each variant holds minProperties", func(t *testing.T) {
		t.Parallel()
		for _, id := range []ir.TypeID{"t/composed/components/schemas/D/oneOf/0", "t/composed/components/schemas/D/oneOf/1"} {
			v, ok := doc.Types[id].(*ir.Model)
			require.True(t, ok, "%s: got %T", id, doc.Types[id])
			assert.Equal(t, &ir.Constraints{MinProps: new(int64(2))}, v.Constraints, id)
		}
	})
	t.Run("row 35: the union keeps minLength and reports it", func(t *testing.T) {
		t.Parallel()
		u, ok := typeByName(doc, "D").(*ir.Union)
		require.True(t, ok, "got %T", typeByName(doc, "D"))
		assert.Contains(t, u.Unmodeled, "openapi:minLength")
		assert.NotContains(t, u.Unmodeled, "openapi:minProperties")
		msg := openapitest.DiagMessageAt(t, diags, diag.DegradedConstruct, ir.SeverityInfo, "/components/schemas/D")
		assert.Contains(t, msg, "minLength")
		assert.NotContains(t, msg, "minProperties")
		assert.NotContains(t, marshalForTest(t, doc), `"openapi:minProperties"`)
	})
}

// budgetSpec writes a position naming a schema of n branches none of which
// declares a type, beside an array, in either order.
func budgetSpec(n int) string {
	var b strings.Builder
	b.WriteString("    Wide: {allOf: [" + strings.TrimSuffix(strings.Repeat(refTo("Open")+", ", n), ", ") + "]}\n")
	b.WriteString("    Open: {description: no type}\n    Arr: {type: array, items: {type: string}}\n")
	b.WriteString("    H:\n      type: object\n      properties:\n")
	b.WriteString("        wideFirst: {allOf: [" + refTo("Wide") + ", " + refTo("Arr") + "], minLength: 1}\n")
	b.WriteString("        arrFirst: {allOf: [" + refTo("Arr") + ", " + refTo("Wide") + "], minLength: 1}\n")
	return openapitest.ComponentSpec(b.String())
}

// TestConstraints_AReadPastItsBudgetReadsEveryBoundInEitherOrder pins that a
// read of the named schemas that spends more than its budget answers nothing,
// so no verdict turns on the order an allOf lists its entries in: past 256
// schemas read, both properties hold the bound, and short of it both keep it.
func TestConstraints_AReadPastItsBudgetReadsEveryBoundInEitherOrder(t *testing.T) {
	t.Parallel()
	t.Run("row 36: 256 reads", func(t *testing.T) {
		t.Parallel()
		doc, diags := parseFull(t, budgetSpec(254))
		openapitest.RequireNoErrorDiags(t, diags)
		for _, wire := range []string{"wideFirst", "arrFirst"} {
			p := propertyOf(t, doc, "H", wire)
			assert.Nil(t, p.Constraints, wire)
			node, ok := doc.Types[p.Type.Target].(*ir.Model)
			require.True(t, ok, "%s: got %T", wire, doc.Types[p.Type.Target])
			assert.Contains(t, node.Unmodeled, "openapi:minLength", wire)
		}
	})
	t.Run("row 37: 257 reads", func(t *testing.T) {
		t.Parallel()
		doc, diags := parseFull(t, budgetSpec(255))
		openapitest.RequireNoErrorDiags(t, diags)
		first, last := propertyOf(t, doc, "H", "wideFirst"), propertyOf(t, doc, "H", "arrFirst")
		assert.Equal(t, &ir.Constraints{MinLength: new(int64(1))}, first.Constraints)
		assert.Equal(t, first.Constraints, last.Constraints)
		assert.Empty(t, first.Unmodeled)
		assert.Empty(t, last.Unmodeled)
		for _, p := range []ir.Property{first, last} {
			node := doc.Types[p.Type.Target]
			require.NotNil(t, node, p.ID)
			assert.NotContains(t, node.Common().Unmodeled, "openapi:minLength", p.ID)
		}
		assert.Equal(t, doc.Types[first.Type.Target].Kind(), doc.Types[last.Type.Target].Kind())
		assert.Equal(t, first.Type.Nullable, last.Type.Nullable)
	})
}

// recursiveSpec writes a $recursiveRef and a $dynamicRef each beside a bound its
// target cannot take.
const recursiveSpec = `openapi: 3.1.0
jsonSchemaDialect: https://json-schema.org/draft/2019-09/schema
info: {title: t, version: "1"}
paths: {}
components:
  schemas:
    Tree:
      $id: https://example.com/tree
      $recursiveAnchor: true
      type: array
      items: {$recursiveRef: '#', minLength: 1}
    Dyn:
      $id: https://example.com/dyn
      $dynamicAnchor: node
      type: array
      items: {$dynamicRef: '#node', minLength: 1}
`

// TestConstraints_AnExpandedReferenceIsNamedByItsKeyword pins row 41: the report
// of a bound kept beside an expanded reference names the keyword the schema
// wrote, a $recursiveRef or a $dynamicRef, and the bound is kept.
func TestConstraints_AnExpandedReferenceIsNamedByItsKeyword(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, recursiveSpec)
	openapitest.RequireNoErrorDiags(t, diags)
	for _, tc := range []struct{ at, has, hasNot string }{
		{"/components/schemas/Tree/items", "its $recursiveRef expands to", "$dynamicRef"},
		{"/components/schemas/Dyn/items", "its $dynamicRef expands to", "$recursiveRef"},
	} {
		msg := openapitest.DiagMessageAt(t, diags, diag.DegradedConstruct, ir.SeverityInfo, tc.at)
		assert.Contains(t, msg, tc.has, tc.at)
		assert.NotContains(t, msg, tc.hasNot, tc.at)
		assert.Contains(t, msg, "minLength", tc.at)
	}
	for _, name := range []string{"Tree", "Dyn"} {
		items, ok := doc.Types[ir.TypeID("t/anon/components/schemas/"+name+"/items")]
		require.True(t, ok, name)
		assert.Contains(t, items.Common().Unmodeled, "openapi:minLength", name)
		assert.Nil(t, heldBounds(items), name)
	}
}

// reportedAt renders diags as a sorted multiset of severity, code and pointer,
// what a declaration order must not change.
func reportedAt(diags []ir.Diagnostic) []string {
	out := make([]string, 0, len(diags))
	for _, d := range diags {
		out = append(out, fmt.Sprintf("%s %s %s", d.Severity, d.Code, d.Provenance.Pointer))
	}
	slices.Sort(out)
	return out
}

// TestConstraints_ABoundIsReadTheSameInEitherDeclarationOrder compiles a row
// with the schemas it names declared after it and before it, and holds the two
// documents equal: the types read are the source's, not the order lowering
// reached them in.
func TestConstraints_ABoundIsReadTheSameInEitherDeclarationOrder(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, schema string }{
		{"row 1", "{allOf: [" + refTo("Arr") + "], minLength: 1}"},
		{"row 2", "{$ref: '#/components/schemas/Arr', minLength: 1}"},
		{"row 19", "{anyOf: [" + refTo("Arr") + ", " + refTo("Obj") + "], minLength: 1}"},
		{"row 22", "{$ref: '#/components/schemas/Str', minItems: 1}"},
		{"row 27", "{allOf: [" + refTo("Arr") + ", {minLength: 1}]}"},
		{"row 30", "{allOf: [" + refTo("Obj") + "], minLength: 1, minimum: 2, minProperties: 3}"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			after, afterDiags := parseFull(t, namedSpec("3.1.0", tc.schema, "", false))
			before, beforeDiags := parseFull(t, namedSpec("3.1.0", tc.schema, "", true))
			assert.Empty(t, cmp.Diff(after.Types, before.Types, orderInvariantIR()...))
			assert.Equal(t, reportedAt(afterDiags), reportedAt(beforeDiags))
			assert.NotEmpty(t, afterDiags, "a row that reports nothing compares nothing")
		})
	}
}

// TestConstraints_ABoundBesideAUnionItsTypeExcludesIsKept pins a type written
// beside a union whose branches admit none of it: the body lowers to a shared
// primitive and the census hoists an alias over it, which holds no bound, since
// the position admits no kind, and keeps the bound with a report, as the union
// beside it is kept.
func TestConstraints_ABoundBesideAUnionItsTypeExcludesIsKept(t *testing.T) {
	t.Parallel()
	schema := "{type: string, oneOf: [" + refTo("Arr") + ", " + refTo("Obj") + "], minLength: 1}"
	doc, diags := parseFull(t, namedSpec("3.1.0", schema, "", false))
	openapitest.RequireNoErrorDiags(t, diags)

	p := propertyOf(t, doc, "Holder", "p")
	assert.Nil(t, p.Constraints)
	for at, id := range map[string]ir.TypeID{namedS: componentID("S"), namedP: "t/anon" + namedP} {
		alias, ok := doc.Types[id].(*ir.Scalar)
		require.True(t, ok, "%s: got %T", at, doc.Types[id])
		assert.Equal(t, ir.TypeID("t/prim/string"), alias.Base.Target, at)
		assert.Nil(t, alias.Constraints, at)
		entry, kept := alias.Unmodeled["openapi:minLength"]
		require.True(t, kept, "%s: got %v", at, unmodeledKeys(alias.Unmodeled))
		assert.Equal(t, ir.ReasonDegradedLowering, entry.Reason, at)
		assert.Equal(t, at+"/minLength", string(entry.Provenance.Pointer), at)
		assert.Contains(t, alias.Unmodeled, "openapi:oneOf", at)
		var bound []string
		for _, d := range diagsAtPointer(diags, diag.DegradedConstruct, at) {
			if strings.Contains(d.Message, "minLength") {
				bound = append(bound, d.Message)
			}
		}
		require.Len(t, bound, 1, at)
		assert.Contains(t, bound[0], `a node of kind "primitive"`, at)
	}
}

// TestAllOf_ABoundBesideABranchsOwnRefIsReadByItsTarget pins a bound written
// beside the $ref of a composed branch: it is that branch's, read by what its
// reference names, so the alias the branch owns keeps one the target cannot
// take, with a report at the branch, and holds one it can.
func TestAllOf_ABoundBesideABranchsOwnRefIsReadByItsTarget(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, schema, branch, keyword string
		held                          *ir.Constraints
	}{
		{name: "a value bound beside an array", branch: "allOf/0", keyword: "minLength",
			schema: "{allOf: [{$ref: '#/components/schemas/Arr', minLength: 1}, " + refTo("Open") + "]}"},
		{name: "a collection bound beside a string", branch: "allOf/1", keyword: "minItems",
			schema: "{allOf: [" + refTo("Open") + ", {$ref: '#/components/schemas/Str', minItems: 2}]}"},
		{name: "a value bound the target takes", branch: "allOf/0", held: &ir.Constraints{MinLength: new(int64(1))},
			schema: "{allOf: [{$ref: '#/components/schemas/Str', minLength: 1}, " + refTo("Open") + "]}"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, diags := parseFull(t, namedSpec("3.1.0", tc.schema, "", false))
			openapitest.RequireNoErrorDiags(t, diags)

			_, composed := typeByName(doc, "S").(*ir.Model)
			require.True(t, composed, "got %T", typeByName(doc, "S"))
			at := namedS + "/" + tc.branch
			alias, ok := doc.Types["t/anon"+ir.TypeID(at)].(*ir.Scalar)
			require.True(t, ok, "got %T", doc.Types["t/anon"+ir.TypeID(at)])
			assert.Equal(t, tc.held, alias.Constraints)
			if tc.held != nil {
				assert.Empty(t, alias.Unmodeled)
				assert.Empty(t, diags)
				return
			}
			entry, kept := alias.Unmodeled["openapi:"+tc.keyword]
			require.True(t, kept, "got %v", unmodeledKeys(alias.Unmodeled))
			assert.Equal(t, at+"/"+tc.keyword, string(entry.Provenance.Pointer))
			msg := openapitest.DiagMessageAt(t, diags, diag.DegradedConstruct, ir.SeverityInfo, at)
			assert.Contains(t, msg, "an alias over its $ref target")
			assert.Contains(t, msg, "no home for "+tc.keyword)
		})
	}
}

// TestConstraints_ATypeListReadsWhatThePositionAdmits pins a bound written
// beside a type list an allOf narrows: the Union's variants and the census
// beside them both read the kinds the position admits, so a bound only an
// excluded variant would take is held by none and kept once, whether it is
// written on a branch or beside the allOf, while the variant the position
// admits holds its own.
func TestConstraints_ATypeListReadsWhatThePositionAdmits(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, schema, key, value, reportAt string }{
		{name: "a bound on a branch", key: "allOf/1", value: `{"minimum":1}`, reportAt: "/allOf/1",
			schema: "{type: [string, integer], allOf: [{type: [string, boolean]}, {minimum: 1}], minLength: 2}"},
		{name: "a bound beside the allOf", key: "minimum", value: "1",
			schema: "{type: [string, integer], allOf: [{type: [string, boolean]}], minimum: 1, minLength: 2}"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, diags := parseFull(t, namedSpec("3.1.0", tc.schema, "", false))
			openapitest.RequireNoErrorDiags(t, diags)

			assert.Nil(t, propertyOf(t, doc, "Holder", "p").Constraints)
			for at, id := range map[string]ir.TypeID{namedS: componentID("S"), namedP: "t/anon" + namedP} {
				u, ok := doc.Types[id].(*ir.Union)
				require.True(t, ok, "%s: got %T", at, doc.Types[id])
				var held []*ir.Constraints
				for _, v := range u.Variants {
					if cons := heldBounds(doc.Types[v.Type.Target]); cons != nil {
						held = append(held, cons)
					}
				}
				assert.Equal(t, []*ir.Constraints{{MinLength: new(int64(2))}}, held, at)
				entry, kept := u.Unmodeled["openapi:"+tc.key]
				require.True(t, kept, "%s: got %v", at, unmodeledKeys(u.Unmodeled))
				assert.JSONEq(t, tc.value, string(entry.Value), at)
				assert.Contains(t, openapitest.DiagMessageAt(t, diags, diag.DegradedConstruct, ir.SeverityInfo, at+tc.reportAt),
					"minimum", at)
			}
		})
	}
}

// TestConstraints_ANamedSchemasDynamicReferenceIsReadThrough pins a schema a
// position names that writes a dynamic reference of its own: the bound is read
// by what that reference expands to, an array here, so it is kept, and one the
// expansion takes is held.
func TestConstraints_ANamedSchemasDynamicReferenceIsReadThrough(t *testing.T) {
	t.Parallel()
	const extra = `    DL: {$dynamicRef: '#list'}
    DS: {$dynamicRef: '#node'}
    List: {$dynamicAnchor: list, type: array, items: {type: string}}
`
	t.Run("beside a $ref", func(t *testing.T) {
		t.Parallel()
		assertNamedRow(t, namedRow{
			name: "beside a $ref", schema: "{$ref: '#/components/schemas/DL', minLength: 1}", extra: extra,
			kept: []keptAt{{key: "minLength"}}, msgs: []string{"an alias over its $ref target"},
		})
	})
	for _, tc := range []struct {
		name, target string
		held         *ir.Constraints
	}{
		{name: "beside an allOf naming an array", target: "DL"},
		{name: "beside an allOf naming a string", target: "DS", held: &ir.Constraints{MinLength: new(int64(1))}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, diags := parseFull(t, namedSpec("3.1.0", "{allOf: ["+refTo(tc.target)+"], minLength: 1}", extra, false))
			openapitest.RequireNoErrorDiags(t, diags)
			for at, id := range map[string]ir.TypeID{namedS: componentID("S"), namedP: "t/anon" + namedP} {
				m, ok := doc.Types[id].(*ir.Model)
				require.True(t, ok, "%s: got %T", at, doc.Types[id])
				assert.Equal(t, tc.held, m.Constraints, at)
				if tc.held != nil {
					assert.Empty(t, m.Unmodeled, at)
					continue
				}
				assert.Contains(t, m.Unmodeled, "openapi:minLength", at)
				assert.Contains(t, openapitest.DiagMessageAt(t, diags, diag.DegradedConstruct, ir.SeverityInfo, at),
					"no home for minLength", at)
			}
		})
	}
}

// TestConstraints_AnInlineBranchIsReadAtItsOwnPointer pins that an inline
// branch is read where it is written: its dynamic reference expands in its own
// schema resource, so one under an identifier of its own names no anchor of the
// document's and excludes nothing, while one beside it expands to the array.
func TestConstraints_AnInlineBranchIsReadAtItsOwnPointer(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, branch string
		held         *ir.Constraints
	}{
		{name: "in the document's resource", branch: "{$dynamicRef: '#list'}"},
		{name: "in a resource of its own", branch: "{$id: 'https://example.com/inner', $dynamicRef: '#list'}",
			held: &ir.Constraints{MinLength: new(int64(1))}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, diags := parseFull(t, openapitest.ComponentSpecVer("3.1.0", "    S: {allOf: ["+tc.branch+"], minLength: 1}\n"+
				"    List: {$dynamicAnchor: list, type: array, items: {type: string}}\n"))
			openapitest.RequireNoErrorDiags(t, diags)
			m, ok := typeByName(doc, "S").(*ir.Model)
			require.True(t, ok, "got %T", typeByName(doc, "S"))
			assert.Equal(t, tc.held, m.Constraints)
			_, kept := m.Unmodeled["openapi:minLength"]
			assert.Equal(t, tc.held == nil, kept, "got %v", unmodeledKeys(m.Unmodeled))
		})
	}
}
