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
	"github.com/dexpace/morphic/compilers/openapi/internal/schema"
	"github.com/dexpace/morphic/ir"
)

// TestOneOf_CoDeclaredKeywordsEveryBranchSaysLeaveTheUnionAlone pins the body
// that adds nothing to its branches (docs/compilers/openapi.md#union-beside-structure):
// a `type` naming every kind they admit, and a `format`, `enum` or `items` each
// is held to, leave the union alone, lowered as its bare spelling is, keeping
// nothing and reporting nothing. A keyword no branch says beyond the body's
// keywords stays beside the union, and one the branches do not all say keeps
// the body: a null the `type` leaves out, a branch writing no `type`, branches
// declaring no shape and a discriminator beside them each keep it.
func TestOneOf_CoDeclaredKeywordsEveryBranchSaysLeaveTheUnionAlone(t *testing.T) {
	t.Parallel()
	doc, diags := schema.LowerSpec(t, openapitest.ComponentSpecVer("3.1.0", `    V4: {type: string, format: ipv4}
    V6: {type: string, format: ipv6}
    A: {type: object, properties: {a: {type: string}}}
    Bare: {anyOf: [{$ref: '#/components/schemas/V4'}, {$ref: '#/components/schemas/V6'}]}
    Typed: {type: string, anyOf: [{$ref: '#/components/schemas/V4'}, {$ref: '#/components/schemas/V6'}]}
    Formatted: {type: string, format: ipv4, anyOf: [{$ref: '#/components/schemas/V4'}, {type: string, format: ipv4, maxLength: 15}]}
    Valued: {enum: [a, b, c], oneOf: [{type: string, enum: [a]}, {type: string, enum: [b, c]}]}
    Listed:
      type: array
      items: {type: string}
      anyOf: [{type: array, items: {type: string, format: uuid}}, {type: array, items: {type: string, format: date}}]
    Objects: {type: object, oneOf: [{$ref: '#/components/schemas/A'}, {type: object, properties: {b: {type: string}}}]}
    NullAdmitted: {type: [string, 'null'], oneOf: [{$ref: '#/components/schemas/V4'}, {type: 'null'}]}
    FormatNotSaid: {type: string, format: ipv4, anyOf: [{type: string}, {$ref: '#/components/schemas/V4'}]}
    NullLeftOut: {type: string, oneOf: [{$ref: '#/components/schemas/V4'}, {type: 'null'}]}
    Untyped: {type: string, anyOf: [{format: ipv4}, {format: ipv6}]}
    Shapeless: {type: string, oneOf: [{type: string, pattern: '^a'}, {type: string, pattern: '^b'}]}
    Discriminated:
      type: object
      discriminator: {propertyName: kind}
      oneOf: [{$ref: '#/components/schemas/A'}]
`))
	openapitest.RequireNoErrorDiags(t, diags)

	bare, ok := typeByName(doc, "Bare").(*ir.Union)
	require.True(t, ok)
	typed, ok := typeByName(doc, "Typed").(*ir.Union)
	require.True(t, ok, "a type every branch says leaves the union alone")
	assert.Equal(t, bare.Variants, typed.Variants, "as its bare spelling lowers")
	for _, name := range []string{"Typed", "Formatted", "Valued", "Listed", "Objects"} {
		u, ok := typeByName(doc, name).(*ir.Union)
		require.True(t, ok, "%s lowers to its union; got %T", name, typeByName(doc, name))
		assert.Empty(t, u.Unmodeled, "%s keeps nothing: each keyword beside it is said", name)
		assert.False(t, openapitest.HasDiagCodeAt(diags, diag.DegradedConstruct, "/components/schemas/"+name), name)
		assert.False(t, openapitest.HasDiagCodeAt(diags, diag.CompositionLowering, "/components/schemas/"+name), name)
	}

	alias, ok := typeByName(doc, "NullAdmitted").(*ir.Scalar)
	require.True(t, ok, "the union alone collapses to its one other branch")
	assert.Equal(t, &ir.TypeRef{Target: componentID("V4"), Nullable: true}, alias.Base)

	formatNotSaid, ok := typeByName(doc, "FormatNotSaid").(*ir.Union)
	require.True(t, ok, "the type is said, so the union is the position")
	assert.Contains(t, formatNotSaid.Unmodeled, "openapi:format", "and the format no branch but one says stays beside it")

	for name, reading := range map[string]ir.UnmodeledReason{
		"NullLeftOut": ir.ReasonDegradedLowering, "Untyped": ir.ReasonDegradedLowering,
		"Shapeless": ir.ReasonValidationOnly, "Discriminated": ir.ReasonDegradedLowering,
	} {
		assert.Equal(t, string(reading), unionReading(doc, name), "%s keeps its body", name)
	}
}

// TestOneOf_CoDeclaredInlineBranchIsConjoinedByItsNode pins that a union
// beside a model body distributes across an inline branch as across a $ref one
// (docs/compilers/openapi.md#union-beside-structure): the branch lowers at its
// own pointer, which an outside $ref to it finds too, and each variant composes
// the body with that node, carried as written, a branch admitting no object and
// one redeclaring a property of the body among them. A bare null branch still
// declines.
func TestOneOf_CoDeclaredInlineBranchIsConjoinedByItsNode(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, openapitest.ComponentSpecVer("3.1.0", `    Shape:
      type: object
      properties: {id: {type: string}, kind: {type: string}}
      oneOf:
        - {type: object, properties: {radius: {type: number}}}
        - {properties: {kind: {type: string, enum: [square]}, side: {type: number}}, required: [side]}
        - {type: string}
    Outside: {$ref: '#/components/schemas/Shape/oneOf/0'}
    WithNull:
      type: object
      properties: {id: {type: string}}
      oneOf: [{type: object, properties: {radius: {type: number}}}, {type: 'null'}]
`))
	openapitest.RequireNoErrorDiags(t, diags)
	assert.Empty(t, validated(t, doc), "the document must validate")

	shape, ok := typeByName(doc, "Shape").(*ir.Union)
	require.True(t, ok, "the union is the value")
	require.Len(t, shape.Variants, 3)
	branches := []ir.TypeID{
		"t/anon/components/schemas/Shape/oneOf/0", "t/anon/components/schemas/Shape/oneOf/1", "t/prim/string",
	}
	for i, branch := range branches {
		v, ok := doc.Types[shape.Variants[i].Type.Target].(*ir.Model)
		require.True(t, ok, "variant %d is a composed model", i)
		require.Len(t, v.Properties, 2, "the body rides on variant %d", i)
		assert.Equal(t, ir.PropID("p/openapi/components/schemas/Shape/properties/kind"), v.Properties[1].ID,
			"under the one identity the body declares it with")
		require.NotNil(t, v.Base)
		assert.Equal(t, branch, v.Base.Target, "variant %d conjoins its branch", i)
	}
	redeclaring, ok := doc.Types[branches[1]].(*ir.Model)
	require.True(t, ok, "the branch redeclaring kind is a model of its own")
	assert.Equal(t, "kind", redeclaring.Properties[0].WireName)

	outside, ok := typeByName(doc, "Outside").(*ir.Scalar)
	require.True(t, ok)
	assert.Equal(t, branches[0], outside.Base.Target, "an outside $ref finds the branch node, not the variant")

	assert.Equal(t, string(ir.ReasonDegradedLowering), unionReading(doc, "WithNull"))
	assert.Contains(t,
		openapitest.DiagMessageAt(t, diags, diag.DegradedConstruct, ir.SeverityInfo, "/components/schemas/WithNull"),
		"a branch is a bare null")
}

// TestOneOf_CoDeclaredBareRequiredConjoinsObjectBranches pins a body writing
// only `required` beside branches that admit only an object or null: each
// variant requires the names of its branch, which keeps its null, and a branch
// that may be no object leaves the body nothing to compose
// (docs/compilers/openapi.md#union-beside-structure).
func TestOneOf_CoDeclaredBareRequiredConjoinsObjectBranches(t *testing.T) {
	t.Parallel()
	doc, diags := schema.LowerSpec(t, openapitest.ComponentSpecVer("3.1.0", `    A: {type: object, properties: {a: {type: string}}}
    Required: {required: [a, b], oneOf: [{$ref: '#/components/schemas/A'}]}
    Nullable:
      required: [a]
      anyOf: [{$ref: '#/components/schemas/A'}, {type: [object, 'null'], properties: {c: {type: string}}}]
    BesideString: {required: [a], oneOf: [{$ref: '#/components/schemas/A'}, {type: string}]}
`))
	openapitest.RequireNoErrorDiags(t, diags)

	required, ok := typeByName(doc, "Required").(*ir.Union)
	require.True(t, ok)
	require.Len(t, required.Variants, 1)
	v, ok := doc.Types[required.Variants[0].Type.Target].(*ir.Model)
	require.True(t, ok)
	assert.Equal(t, []string{"a", "b"}, v.Requires)
	require.NotNil(t, v.Base)
	assert.Equal(t, componentID("A"), v.Base.Target)

	nullable, ok := typeByName(doc, "Nullable").(*ir.Union)
	require.True(t, ok)
	require.Len(t, nullable.Variants, 2)
	assert.False(t, nullable.Variants[0].Type.Nullable)
	assert.True(t, nullable.Variants[1].Type.Nullable, "a required list leaves the branch's null alone")

	assert.Equal(t, string(ir.ReasonDegradedLowering), unionReading(doc, "BesideString"))
	assert.True(t, slices.ContainsFunc(diags, func(d ir.Diagnostic) bool {
		return d.Provenance.Pointer == "/components/schemas/BesideString" && strings.Contains(d.Message, "the body is not a model")
	}), "%+v", diags)
}
