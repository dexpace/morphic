// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema_test

import (
	"encoding/json/jsontext"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/budget"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/compilers/openapi/internal/schema"
	"github.com/dexpace/morphic/ir"
)

// nullBranchSpellings are the spellings of a union branch admitting null alone,
// each with the version that spells it.
var nullBranchSpellings = []struct{ version, branch string }{
	{"3.1.0", `{type: "null"}`},
	{"3.1.0", `{type: ["null"]}`},
	{"3.1.0", `{enum: [null]}`},
	{"3.1.0", `{const: null}`},
	{"3.1.0", `{type: "null", enum: [null]}`},
	{"3.1.0", `{type: [object, "null"], enum: [null]}`},
	{"3.0.3", `{type: object, enum: [null], nullable: true}`},
	{"3.0.3", `{enum: [null]}`},
}

// nullBranchSpec writes the union shapes a null branch stands in, the branch
// spelled as branch: beside one other branch, first or last, under either
// combinator, twice, and beside two others under either combinator.
func nullBranchSpec(version, branch string) string {
	return openapitest.ComponentSpecVer(version, `    X: {type: object, properties: {id: {type: string}}}
    Y: {type: object, properties: {n: {type: integer}}}
    Holder:
      type: object
      properties:
        last: {oneOf: [{$ref: '#/components/schemas/X'}, `+branch+`]}
        first: {oneOf: [`+branch+`, {$ref: '#/components/schemas/X'}]}
        any: {anyOf: [{$ref: '#/components/schemas/X'}, `+branch+`]}
        twice: {oneOf: [`+branch+`, {$ref: '#/components/schemas/X'}, `+branch+`]}
        many: {oneOf: [{$ref: '#/components/schemas/X'}, {$ref: '#/components/schemas/Y'}, `+branch+`]}
        manyAny: {anyOf: [`+branch+`, {$ref: '#/components/schemas/X'}, {$ref: '#/components/schemas/Y'}]}
`)
}

// TestNullBranch_IsTheReferencesInEverySpelling pins a union branch admitting
// null alone, in every spelling, to the reading a `type: "null"` branch had
// alone: no variant, its null the reference's, so a union of one other branch
// collapses to a nullable reference to it, and one of more is a Union of the
// others behind a nullable reference, exclusive as its combinator says
// (docs/compilers/openapi.md#union-null-variant).
func TestNullBranch_IsTheReferencesInEverySpelling(t *testing.T) {
	t.Parallel()
	x, y := componentID("X"), componentID("Y")
	for _, sp := range nullBranchSpellings {
		t.Run(sp.version+" "+sp.branch, func(t *testing.T) {
			t.Parallel()
			doc, diags := parseFull(t, nullBranchSpec(sp.version, sp.branch))
			openapitest.RequireNoErrorDiags(t, diags)
			for _, wire := range []string{"last", "first", "any", "twice"} {
				assert.Equal(t, ir.TypeRef{Target: x, Nullable: true}, propertyOf(t, doc, "Holder", wire).Type,
					"%s collapses to its one other branch, nullable", wire)
			}
			for wire, exclusive := range map[string]bool{"many": true, "manyAny": false} {
				ref := propertyOf(t, doc, "Holder", wire).Type
				assert.True(t, ref.Nullable, "%s admits the null", wire)
				u, ok := doc.Types[ref.Target].(*ir.Union)
				require.True(t, ok, "%s is a union, got %T", wire, doc.Types[ref.Target])
				assert.Equal(t, []ir.TypeID{x, y}, variantTargets(u), "%s: the null branch is no variant", wire)
				assert.Equal(t, exclusive, u.Exclusive, wire)
			}
			for id, td := range doc.Types {
				e, isEnum := td.(*ir.Enum)
				assert.False(t, isEnum && len(e.Members) == 0, "no null branch lowered as a variant: %s", id)
			}
		})
	}
}

// variantTargets returns what each variant of u names.
func variantTargets(u *ir.Union) []ir.TypeID {
	out := make([]ir.TypeID, 0, len(u.Variants))
	for _, v := range u.Variants {
		out = append(out, v.Type.Target)
	}
	return out
}

// TestNullBranch_EverySpellingLowersAlike holds the spellings of a null branch
// to one IR: the types and the diagnostics a document holding each compiles to
// are those `type: "null"` gives.
func TestNullBranch_EverySpellingLowersAlike(t *testing.T) {
	t.Parallel()
	want, wantDiags := parseFull(t, nullBranchSpec("3.1.0", `{type: "null"}`))
	for _, sp := range nullBranchSpellings {
		if sp.version != "3.1.0" {
			continue
		}
		doc, diags := parseFull(t, nullBranchSpec(sp.version, sp.branch))
		if diff := cmp.Diff(want.Types, doc.Types); diff != "" {
			t.Errorf("%s lowers otherwise (-type null +it):\n%s", sp.branch, diff)
		}
		assert.Equal(t, wantDiags, diags, sp.branch)
	}
}

// TestNullBranch_ARefToANullAloneSchemaStaysAVariant pins the one spelling of a
// null branch that is no union's null: a $ref, which names a node the source
// declares, the Enum with no member its target lowers to. The variant names it
// behind a nullable reference, the union's reference keeping no null of its
// own, whichever of the two schemas is declared first.
func TestNullBranch_ARefToANullAloneSchemaStaysAVariant(t *testing.T) {
	t.Parallel()
	const nilSchema = "    Nil: {type: \"null\"}\n"
	const holder = "    X: {type: object, properties: {id: {type: string}}}\n" +
		"    Holder: {type: object, properties: {p: {oneOf: [{$ref: '#/components/schemas/X'}, {$ref: '#/components/schemas/Nil'}]}}}\n"
	docs := make([]*ir.Document, 0, 2)
	for _, schemas := range []string{nilSchema + holder, holder + nilSchema} {
		doc, diags := parseFull(t, openapitest.ComponentSpecVer("3.1.0", schemas))
		openapitest.RequireNoErrorDiags(t, diags)
		ref := propertyOf(t, doc, "Holder", "p").Type
		assert.False(t, ref.Nullable, "the union's own reference says no null")
		u, ok := doc.Types[ref.Target].(*ir.Union)
		require.True(t, ok, "got %T", doc.Types[ref.Target])
		require.Len(t, u.Variants, 2)
		assert.Equal(t, ir.TypeRef{Target: componentID("Nil"), Nullable: true}, u.Variants[1].Type)
		assertNullAloneAt(t, doc, "Nil", u.Variants[1].Type)
		docs = append(docs, doc)
	}
	assert.Empty(t, cmp.Diff(docs[0].Types, docs[1].Types), "the declaration order changes nothing")
}

// TestNullBranch_ADiscriminatorLinesItsVariantsUpPastTheNull pins the readers
// that line a union's variants up with its branches, skipping the null branches
// no variant stands for: a mapping naming a $ref branch's target routes to the
// variant the branch lowered to, and the implicit mapping a discriminator with
// none reads is one tag per variant.
func TestNullBranch_ADiscriminatorLinesItsVariantsUpPastTheNull(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, openapitest.ComponentSpecVer("3.1.0", `    X: {type: object, required: [kind], properties: {kind: {type: string}}}
    Y: {type: object, required: [kind], properties: {kind: {type: string}}}
    Holder:
      type: object
      properties:
        mapped:
          oneOf:
            - {$ref: '#/components/schemas/X', description: an x}
            - {const: null}
            - {$ref: '#/components/schemas/Y'}
          discriminator: {propertyName: kind, mapping: {x: '#/components/schemas/X', y: '#/components/schemas/Y'}}
        implied:
          oneOf: [{enum: [null]}, {$ref: '#/components/schemas/X'}, {$ref: '#/components/schemas/Y'}]
          discriminator: {propertyName: kind}
`))
	openapitest.RequireNoErrorDiags(t, diags)
	mapped, ok := doc.Types[propertyOf(t, doc, "Holder", "mapped").Type.Target].(*ir.Union)
	require.True(t, ok)
	require.Len(t, mapped.Variants, 2)
	require.NotNil(t, mapped.Discriminator)
	assert.Equal(t, map[string]ir.TypeID{"x": mapped.Variants[0].Type.Target, "y": componentID("Y")},
		mapped.Discriminator.Mapping, "each entry routes to the variant its branch lowered to")
	assert.NotEqual(t, componentID("X"), mapped.Variants[0].Type.Target, "the $ref with a sibling owns its variant")

	implied, ok := doc.Types[propertyOf(t, doc, "Holder", "implied").Type.Target].(*ir.Union)
	require.True(t, ok)
	assert.Equal(t, []ir.TypeID{componentID("X"), componentID("Y")}, variantTargets(implied))
}

// TestNullBranch_KeepsWhatTheBranchWrites pins the null branch writing more than
// its null: whole under `openapi:<combinator>/<index>` (no_ir_home), with an info
// at the branch, where the position keeps what it cannot hold, its carrier for a
// property, an alias for a component or an element, and the Union where the
// branches leave one (docs/compilers/openapi.md#union-null-variant).
func TestNullBranch_KeepsWhatTheBranchWrites(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, openapitest.ComponentSpecVer("3.1.0", `    X: {type: object, properties: {id: {type: string}}}
    Y: {type: object, properties: {n: {type: integer}}}
    Described: {oneOf: [{$ref: '#/components/schemas/X'}, {type: "null", description: nothing}]}
    Holder:
      type: object
      properties:
        described: {oneOf: [{$ref: '#/components/schemas/X'}, {type: "null", description: nothing}]}
        list: {type: array, items: {anyOf: [{const: null, title: t}, {$ref: '#/components/schemas/X'}]}}
        many: {oneOf: [{$ref: '#/components/schemas/X'}, {$ref: '#/components/schemas/Y'}, {enum: [null], x-why: dry}]}
        twice: {anyOf: [{type: "null", description: a}, {$ref: '#/components/schemas/X'}, {const: null, description: b}]}
        merged: {oneOf: [{$ref: '#/components/schemas/X'}, {<<: {description: merged}, type: "null"}]}
        said: {oneOf: [{$ref: '#/components/schemas/X'}, {type: "null", enum: [null], const: null, nullable: true}]}
        leftOut: {oneOf: [{$ref: '#/components/schemas/X'}, {type: "null", description: null}]}
`))
	openapitest.RequireNoErrorDiags(t, diags)
	const at = "/components/schemas/Holder/properties/"
	holder := func(wire string) ir.Unmodeled { return propertyOf(t, doc, "Holder", wire).Unmodeled }
	node := func(ref ir.TypeRef) ir.Unmodeled { return doc.Types[ref.Target].Common().Unmodeled }
	list, ok := doc.Types[propertyOf(t, doc, "Holder", "list").Type.Target].(*ir.List)
	require.True(t, ok)

	for _, tc := range []struct {
		name    string
		p       ir.Unmodeled
		key, at string // the entry's key, and the branch's pointer
		raw     string
	}{
		{"a property's carrier", holder("described"), "openapi:oneOf/1", at + "described/oneOf/1",
			`{"description":"nothing","type":"null"}`},
		{"a component's alias", typeByName(doc, "Described").Common().Unmodeled, "openapi:oneOf/1",
			"/components/schemas/Described/oneOf/1", `{"description":"nothing","type":"null"}`},
		{"an element's alias", node(list.Elem), "openapi:anyOf/0", at + "list/items/anyOf/0", `{"const":null,"title":"t"}`},
		{"the union", node(propertyOf(t, doc, "Holder", "many").Type), "openapi:oneOf/2", at + "many/oneOf/2",
			`{"enum":[null],"x-why":"dry"}`},
		{"the first of two", holder("twice"), "openapi:anyOf/0", at + "twice/anyOf/0", `{"description":"a","type":"null"}`},
		{"the second of two", holder("twice"), "openapi:anyOf/2", at + "twice/anyOf/2", `{"const":null,"description":"b"}`},
		{"a merge", holder("merged"), "openapi:oneOf/1", at + "merged/oneOf/1", `{"description":"merged","type":"null"}`},
	} {
		entry, ok := tc.p[tc.key]
		require.True(t, ok, "%s keeps %s, got %v", tc.name, tc.key, tc.p)
		assert.Equal(t, ir.ReasonNoIRHome, entry.Reason, tc.name)
		assert.JSONEq(t, tc.raw, string(entry.Value), tc.name)
		assert.Equal(t, jsontext.Pointer(tc.at), entry.Provenance.Pointer, tc.name)
		assert.Equal(t, []ir.Diagnostic{{Severity: ir.SeverityInfo, Code: diag.DegradedConstruct}},
			codesAt(diags, tc.at), "%s is reported once, at the branch", tc.name)
	}
	for _, wire := range []string{"said", "leftOut"} {
		assert.Empty(t, holder(wire), "%s writes nothing beside its null", wire)
		assert.Empty(t, codesAt(diags, at+wire+"/oneOf/1"), wire)
	}
}

// codesAt returns the severity and code of each diagnostic in diags at pointer.
func codesAt(diags []ir.Diagnostic, pointer string) []ir.Diagnostic {
	var out []ir.Diagnostic
	for _, d := range diags {
		if d.Provenance.Pointer == jsontext.Pointer(pointer) {
			out = append(out, ir.Diagnostic{Severity: d.Severity, Code: d.Code})
		}
	}
	return out
}

// TestNullBranch_AKeptBranchTheKeeperCannotConvertIsReportedByIt pins the one
// branch the keeper stores nothing of: a value no IR value holds, which it
// reports itself, so nothing claims the branch was kept.
func TestNullBranch_AKeptBranchTheKeeperCannotConvertIsReportedByIt(t *testing.T) {
	t.Parallel()
	_, diags := parseFull(t, openapitest.ComponentSpecVer("3.1.0", `    X: {type: object}
    Holder:
      type: object
      properties:
        p: {oneOf: [{$ref: '#/components/schemas/X'}, {type: "null", x-v: .nan}]}
`))
	at := "/components/schemas/Holder/properties/p/oneOf/1"
	got := codesAt(diags, at)
	require.Len(t, got, 1, "%+v", diags)
	assert.Equal(t, diag.UnpreservableConstruct, got[0].Code)
}

// TestNullBranch_BesideABodyIsNotLifted pins a null branch, in every spelling,
// in a union written beside structure: the union is kept verbatim beside the
// body it intersects with, and its null branch lifts nothing onto the
// reference, which says only what the body says of null
// (docs/compilers/openapi.md#union-beside-structure, shape 2).
func TestNullBranch_BesideABodyIsNotLifted(t *testing.T) {
	t.Parallel()
	for _, sp := range nullBranchSpellings {
		t.Run(sp.version+" "+sp.branch, func(t *testing.T) {
			t.Parallel()
			doc, diags := parseFull(t, openapitest.ComponentSpecVer(sp.version, `    X: {type: object, properties: {id: {type: string}}}
    Holder:
      type: object
      properties:
        typed: {type: object, properties: {p: {type: string}}, oneOf: [{$ref: '#/components/schemas/X'}, `+sp.branch+`]}
        untyped: {properties: {p: {type: string}}, oneOf: [`+sp.branch+`, {$ref: '#/components/schemas/X'}]}
`))
			openapitest.RequireNoErrorDiags(t, diags)
			for _, wire := range []string{"typed", "untyped"} {
				ref := propertyOf(t, doc, "Holder", wire).Type
				assert.False(t, ref.Nullable, "%s: the null branch lifts nothing", wire)
				m, ok := doc.Types[ref.Target].(*ir.Model)
				require.True(t, ok, "%s lowers to its body, got %T", wire, doc.Types[ref.Target])
				assert.Contains(t, m.Unmodeled, "openapi:oneOf", "%s keeps its union verbatim", wire)
				assert.Contains(t, openapitest.DiagMessageAt(t, diags, diag.DegradedConstruct, ir.SeverityInfo,
					"/components/schemas/Holder/properties/"+wire), "a branch admits null alone", wire)
			}
		})
	}
}

// TestNullBranch_KeepsANullableNothingReads pins a null branch's `nullable`:
// it says the null where it is read, by the 3.0 dialect defining it or by the
// nullable-widening policy, and keeps nothing; under 3.1 with the policy off it
// is a keyword the dialect does not define, which the branch is kept whole for,
// as a schema writing it alone keeps it (docs/compilers/openapi.md#nullable).
func TestNullBranch_KeepsANullableNothingReads(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		version, branch string
		off, kept       bool
	}{
		{version: "3.1.0", branch: `{type: "null", nullable: true}`},
		{version: "3.1.0", branch: `{type: "null", nullable: true}`, off: true, kept: true},
		{version: "3.0.3", branch: `{enum: [null], nullable: true}`},
		{version: "3.0.3", branch: `{enum: [null], nullable: true}`, off: true},
	} {
		doc, diags := compileNullable(t, openapitest.HolderSpec(tc.version,
			"{oneOf: [{$ref: '#/components/schemas/Pet'}, "+tc.branch+"]}", ""), tc.off)
		openapitest.RequireNoErrorDiags(t, diags)
		name := tc.version + " " + tc.branch
		assert.Equal(t, ir.TypeRef{Target: componentID("Pet"), Nullable: true}, holderP(t, doc), name)
		p := propertyOf(t, doc, "Holder", "p")
		_, kept := p.Unmodeled["openapi:oneOf/1"]
		assert.Equal(t, tc.kept, kept, "%s, policy off: %v", name, tc.off)
	}
}

// TestNullBranch_AnEnumPastTheMemberBudgetIsNoNullBranch pins a branch listing
// null alone past Limits.MaxEnumMembers: an enum past the budget lowers to the
// top type with an error rather than to an Enum, so it is no branch of null
// alone, and it stays a variant whose refusal is reported, as on its own.
func TestNullBranch_AnEnumPastTheMemberBudgetIsNoNullBranch(t *testing.T) {
	t.Parallel()
	const spec = `openapi: 3.1.0
info: {title: T, version: "1"}
paths: {}
components:
  schemas:
    X: {type: object, properties: {id: {type: string}}}
    H: {oneOf: [{$ref: '#/components/schemas/X'}, {enum: [null, null, null]}]}
`
	for _, tc := range []struct {
		limit   int
		refused bool
	}{{limit: 3}, {limit: 2, refused: true}} {
		l, _ := schema.LoweredFor(t, spec)
		l.Ctx.Limits = lowering.Limits{MaxEnumMembers: budget.AtMost(tc.limit)}
		diags := schema.LowerComponentSchemas(t.Context(), l.Ctx, l.Types)
		doc := l.Document(t)
		u, isUnion := typeByName(doc, "H").(*ir.Union)
		assert.Equal(t, tc.refused, isUnion, "limit %d: a union only where the branch is past the budget", tc.limit)
		refusals := openapitest.PointersOf(diags, diag.BudgetExceeded)
		if !tc.refused {
			assert.Empty(t, refusals, "limit %d", tc.limit)
			continue
		}
		require.Len(t, u.Variants, 2, "the branch is a variant")
		assert.Equal(t, []jsontext.Pointer{"/components/schemas/H/oneOf/1"}, refusals)
	}
}
