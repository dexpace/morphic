// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema_test

import (
	"encoding/json/jsontext"
	"fmt"
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
// a `type` naming every kind they admit, and a `format`, `enum`, `const` or
// `items` each is held to, leave the union alone, lowered as its bare spelling
// is, keeping nothing and reporting nothing, beside a discriminator too. A
// `format` the branches do not all say stays beside the union, and a body
// keyword they do not all say keeps the body: a null the `type` leaves out, a
// branch writing no `type`, branches declaring no shape and a discriminator
// routing to a branch composing the body each keep it.
func TestOneOf_CoDeclaredKeywordsEveryBranchSaysLeaveTheUnionAlone(t *testing.T) {
	t.Parallel()
	doc, diags := schema.LowerSpec(t, openapitest.ComponentSpecVer("3.1.0", `    V4: {type: string, format: ipv4}
    V6: {type: string, format: ipv6}
    A: {type: object, properties: {a: {type: string}}}
    Bare: {anyOf: [{$ref: '#/components/schemas/V4'}, {$ref: '#/components/schemas/V6'}]}
    Typed: {type: string, anyOf: [{$ref: '#/components/schemas/V4'}, {$ref: '#/components/schemas/V6'}]}
    Formatted: {type: string, format: ipv4, anyOf: [{$ref: '#/components/schemas/V4'}, {type: string, format: ipv4, maxLength: 15}]}
    Valued: {enum: [a, b, c], oneOf: [{type: string, enum: [a]}, {type: string, enum: [b, c]}]}
    CA: {type: string, const: a}
    EA: {type: string, enum: [a], description: an a}
    Constant: {const: a, anyOf: [{$ref: '#/components/schemas/CA'}, {$ref: '#/components/schemas/EA'}]}
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
    DiscriminatedBase:
      type: object
      discriminator: {propertyName: kind}
      oneOf: [{$ref: '#/components/schemas/A'}, {$ref: '#/components/schemas/Sub'}]
    Sub: {allOf: [{$ref: '#/components/schemas/DiscriminatedBase'}]}
`))
	openapitest.RequireNoErrorDiags(t, diags)

	bare, ok := typeByName(doc, "Bare").(*ir.Union)
	require.True(t, ok)
	typed, ok := typeByName(doc, "Typed").(*ir.Union)
	require.True(t, ok, "a type every branch says leaves the union alone")
	assert.Equal(t, bare.Variants, typed.Variants, "as its bare spelling lowers")
	for _, name := range []string{"Typed", "Formatted", "Valued", "Constant", "Listed", "Objects", "Discriminated"} {
		u, ok := typeByName(doc, name).(*ir.Union)
		require.True(t, ok, "%s lowers to its union; got %T", name, typeByName(doc, name))
		assert.Empty(t, u.Unmodeled, "%s keeps nothing: each keyword beside it is said", name)
		assert.False(t, openapitest.HasDiagCodeAt(diags, diag.DegradedConstruct, "/components/schemas/"+name), name)
		assert.False(t, openapitest.HasDiagCodeAt(diags, diag.CompositionLowering, "/components/schemas/"+name), name)
	}

	alias, ok := typeByName(doc, "NullAdmitted").(*ir.Scalar)
	require.True(t, ok, "the union alone collapses to its one other branch")
	assert.Equal(t, &ir.TypeRef{Target: componentID("V4"), Nullable: true}, alias.Base)

	discriminated := typeByName(doc, "Discriminated").(*ir.Union)
	require.NotNil(t, discriminated.Discriminator, "the discriminator routes the union")
	assert.Equal(t, "kind", discriminated.Discriminator.PropertyName)

	formatNotSaid, ok := typeByName(doc, "FormatNotSaid").(*ir.Union)
	require.True(t, ok, "the type is said, so the union is the position")
	assert.Contains(t, formatNotSaid.Unmodeled, "openapi:format", "and the format no branch but one says stays beside it")

	for name, reading := range map[string]ir.UnmodeledReason{
		"NullLeftOut": ir.ReasonDegradedLowering, "Untyped": ir.ReasonDegradedLowering,
		"Shapeless": ir.ReasonValidationOnly, "DiscriminatedBase": ir.ReasonDegradedLowering,
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
		"a branch admits null alone")
}

// TestOneOf_CoDeclaredBareRequiredConjoinsObjectBranches pins a body writing
// only `required` beside branches that admit only an object or null: each
// variant requires the names of its branch, which keeps its null, and a branch
// that may be no object, a keyword beside `required` or a branch read past its
// bounds leaves the body nothing to compose
// (docs/compilers/openapi.md#union-beside-structure).
func TestOneOf_CoDeclaredBareRequiredConjoinsObjectBranches(t *testing.T) {
	t.Parallel()
	doc, diags := schema.LowerSpec(t, openapitest.ComponentSpecVer("3.1.0", `    A: {type: object, properties: {a: {type: string}}}
    Required: {required: [a, b], oneOf: [{$ref: '#/components/schemas/A'}]}
    Nullable:
      required: [a]
      anyOf: [{$ref: '#/components/schemas/A'}, {type: [object, 'null'], properties: {c: {type: string}}}]
    BesideString: {required: [a], oneOf: [{$ref: '#/components/schemas/A'}, {type: string}]}
    BesideItems: {required: [a], items: {type: string}, oneOf: [{$ref: '#/components/schemas/A'}]}
    CutShort: {required: [a], oneOf: [{$ref: '#/components/schemas/A'}, {type: object, allOf: [{$ref: '#/components/schemas/D0'}]}]}
`+deepChain(300)))
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

	for _, name := range []string{"BesideString", "BesideItems", "CutShort"} {
		assert.Equal(t, string(ir.ReasonDegradedLowering), unionReading(doc, name), name)
		assert.True(t, slices.ContainsFunc(diags, func(d ir.Diagnostic) bool {
			return d.Provenance.Pointer == jsontext.Pointer("/components/schemas/"+name) &&
				strings.Contains(d.Message, "the body is not a model")
		}), "%s: %+v", name, diags)
	}
}

// deepChain declares D0 to Dn, each an allOf naming the next and Dn an object,
// a chain whose read runs past what one reading of a schema may read.
func deepChain(n int) string {
	var b strings.Builder
	for i := range n {
		fmt.Fprintf(&b, "    D%d: {allOf: [{$ref: '#/components/schemas/D%d'}]}\n", i, i+1)
	}
	fmt.Fprintf(&b, "    D%d: {type: object}\n", n)
	return b.String()
}

// TestOneOf_CoDeclaredBesideAComposingAllOfDistributes pins that an allOf that
// composes is a body a union beside it distributes across, as one that refines
// is not (docs/compilers/openapi.md#allof-refinement): two $refs, or an inline
// branch an object, lower to a union of models composed over each branch, in
// either declaration order.
func TestOneOf_CoDeclaredBesideAComposingAllOfDistributes(t *testing.T) {
	t.Parallel()
	for _, doc := range discriminatedOrders(t, "3.1.0", `    TwoRefs:
      allOf: [{$ref: '#/components/schemas/A'}, {$ref: '#/components/schemas/B'}]
      oneOf: [{$ref: '#/components/schemas/C'}, {$ref: '#/components/schemas/D'}]
    Inline:
      allOf: [{type: object, properties: {a: {type: string}}}]
      anyOf: [{$ref: '#/components/schemas/C'}, {$ref: '#/components/schemas/D'}]
`, `    A: {type: object, properties: {a: {type: string}}}
    B: {type: object, properties: {b: {type: string}}}
    C: {type: object, properties: {c: {type: string}}}
    D: {type: object, properties: {d: {type: string}}}
`) {
		for _, name := range []string{"TwoRefs", "Inline"} {
			assert.Equal(t, readDistributed, unionReading(doc, name), name)
		}
	}
}

// TestAllOf_AUnionBranchDistributesAsBesideTheAllOf pins a oneOf or anyOf
// written as an inline allOf branch, which distributes as the same union beside
// the allOf does (docs/compilers/openapi.md#union-beside-structure): the
// variants match the beside spelling's, inline branches conjoined by their own
// nodes, the branch keeps nothing, another branch folds onto the union, and an
// unexpanded $dynamicRef stays beside it. One declaring no shape, one of two
// that do, one beside the schema's own union, one distribution declines, one
// past the flattening depth and one beside a branch's $ref, which it conjoins
// with, stay with their branch; an enum beside the allOf decides.
func TestAllOf_AUnionBranchDistributesAsBesideTheAllOf(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, openapitest.ComponentSpecVer("3.1.0", `    A: {type: object, properties: {a: {type: string}}}
    B: {type: object, properties: {b: {type: string}}}
    Branch:
      allOf:
        - {type: object, anyOf: [{$ref: '#/components/schemas/A'}, {$ref: '#/components/schemas/B'}]}
        - {type: object, required: [id], properties: {id: {type: string}}}
    Beside:
      type: object
      required: [id]
      properties: {id: {type: string}}
      anyOf: [{$ref: '#/components/schemas/A'}, {$ref: '#/components/schemas/B'}]
    Inline:
      allOf:
        - oneOf: [{type: object, properties: {x: {type: string}}}, {type: object, properties: {y: {type: string}}}]
        - {properties: {enabled: {type: boolean}}}
    WithShapeless:
      allOf:
        - oneOf: [{$ref: '#/components/schemas/A'}, {$ref: '#/components/schemas/B'}]
        - anyOf: [{required: [a]}, {required: [b]}]
        - {type: object, properties: {c: {type: string}}}
    Outside: {$ref: '#/components/schemas/Inline/allOf/0/oneOf/1'}
    Shapeless:
      allOf:
        - anyOf: [{required: [p]}, {required: [q]}]
        - {type: object, properties: {p: {type: string}, q: {type: string}}}
    Two:
      allOf:
        - oneOf: [{$ref: '#/components/schemas/A'}]
        - anyOf: [{$ref: '#/components/schemas/B'}]
        - {type: object, properties: {c: {type: string}}}
    BesideToo:
      allOf:
        - oneOf: [{$ref: '#/components/schemas/A'}]
        - {type: object, properties: {c: {type: string}}}
      anyOf: [{$ref: '#/components/schemas/B'}]
    NullBranch:
      allOf:
        - oneOf: [{$ref: '#/components/schemas/A'}, {type: 'null'}]
        - {type: object, properties: {c: {type: string}}}
    Annotated:
      allOf:
        - oneOf: [{$ref: '#/components/schemas/A'}, {$ref: '#/components/schemas/B'}]
        - {description: mine, properties: {c: {type: string}}}
    AnnotatedBeside:
      allOf:
        - {description: mine, properties: {c: {type: string}}}
      oneOf: [{$ref: '#/components/schemas/A'}, {$ref: '#/components/schemas/B'}]
    Node: {$dynamicAnchor: node, type: object, properties: {n: {type: string}}}
    DynamicRef:
      $dynamicRef: '#node'
      allOf:
        - oneOf: [{$ref: '#/components/schemas/A'}, {$ref: '#/components/schemas/B'}]
        - {type: object, properties: {c: {type: string}}}
    RefBranch:
      allOf:
        - {$ref: '#/components/schemas/A', oneOf: [{$ref: '#/components/schemas/B'}]}
        - {type: object, properties: {c: {type: string}}}
    EnumBeside:
      type: string
      enum: [x, y]
      allOf: [{oneOf: [{$ref: '#/components/schemas/A'}, {$ref: '#/components/schemas/B'}]}]
    Capped:
      allOf:
        - `+strings.Repeat("{allOf: [", schema.MaxAllOfFlattenDepth)+
		`{allOf: [{type: object}], oneOf: [{$ref: '#/components/schemas/A'}, {$ref: '#/components/schemas/B'}]}`+
		strings.Repeat("]}", schema.MaxAllOfFlattenDepth)+`
        - {type: object, properties: {c: {type: string}}}
`))
	openapitest.RequireNoErrorDiags(t, diags)
	assert.Empty(t, validated(t, doc), "the document must validate")

	branch, beside := variantModels(t, doc, "Branch"), variantModels(t, doc, "Beside")
	require.Len(t, branch, 2)
	for i := range branch {
		require.NotNil(t, branch[i].Base)
		assert.Equal(t, beside[i].Base, branch[i].Base, "variant %d conjoins the branch the beside spelling's does", i)
		require.Len(t, branch[i].Properties, 1)
		assert.Equal(t, "id", branch[i].Properties[0].WireName)
		assert.True(t, branch[i].Properties[0].Required)
		assert.Empty(t, branch[i].Unmodeled, "the branch keeps nothing: its union is the variants' to give")
	}
	assert.Contains(t,
		openapitest.DiagMessageAt(t, diags, diag.CompositionLowering, ir.SeverityInfo, "/components/schemas/Branch"),
		"written on an allOf branch")

	inline := variantModels(t, doc, "Inline")
	require.Len(t, inline, 2)
	for i, v := range inline {
		require.NotNil(t, v.Base)
		assert.Equal(t, ir.TypeID(fmt.Sprintf("t/anon/components/schemas/Inline/allOf/0/oneOf/%d", i)), v.Base.Target)
	}
	outside, ok := typeByName(doc, "Outside").(*ir.Scalar)
	require.True(t, ok)
	assert.Equal(t, inline[1].Base.Target, outside.Base.Target, "an outside $ref finds the branch node")

	for _, v := range variantModels(t, doc, "WithShapeless") {
		assert.Contains(t, v.Unmodeled, "openapi:allOf/1", "the union declaring no shape stays with its branch")
		assert.NotContains(t, v.Unmodeled, "openapi:allOf/0")
	}

	capped := "openapi:" + strings.TrimPrefix(strings.Repeat("/allOf/0", schema.MaxAllOfFlattenDepth+1), "/")
	for name, kept := range map[string][]string{
		"Shapeless":  {"openapi:allOf/0"},
		"Two":        {"openapi:allOf/0", "openapi:allOf/1"},
		"NullBranch": {"openapi:allOf/0"},
		"RefBranch":  nil,
		"Capped":     {capped},
	} {
		m, ok := typeByName(doc, name).(*ir.Model)
		require.True(t, ok, "%s stays a model; got %T", name, typeByName(doc, name))
		for _, key := range kept {
			assert.Contains(t, m.Unmodeled, key, "%s keeps its union with its branch", name)
		}
	}
	enum, ok := typeByName(doc, "EnumBeside").(*ir.Enum)
	require.True(t, ok, "the enum beside the allOf decides it; got %T", typeByName(doc, "EnumBeside"))
	assert.Contains(t, enum.Unmodeled, "openapi:allOf")
	annotated, ok := typeByName(doc, "Annotated").(*ir.Union)
	require.True(t, ok)
	assert.Equal(t, "mine", annotated.Docs.Description, "another branch folds onto the union, as beside the allOf")
	besideDocs := typeByName(doc, "AnnotatedBeside").Common().Docs
	assert.Equal(t, besideDocs, annotated.Docs)
	for _, v := range variantModels(t, doc, "Annotated") {
		assert.Empty(t, v.Unmodeled, "a branch folding whole is kept on no variant")
	}
	dynamic, ok := typeByName(doc, "DynamicRef").(*ir.Union)
	require.True(t, ok, "beside a $dynamicRef it keeps unexpanded, the branch union distributes as beside the allOf")
	assert.Contains(t, dynamic.Unmodeled, "openapi:$dynamicRef")
	for _, v := range variantModels(t, doc, "BesideToo") {
		assert.Contains(t, v.Unmodeled, "openapi:allOf/0", "beside its own union, a branch's stays with the branch")
	}
}

// TestAllOf_ADiscriminatedBranchUnionDistributesAsBeside pins a union on an allOf
// branch standing with a discriminator, on its branch or beside the allOf
// (docs/compilers/openapi.md#union-beside-structure): it distributes as the
// same union beside the body does, the discriminator routing the composed
// variants, a mapping's entries and the routes implied by name alike, its
// branch keeping nothing, in either declaration order. A schema composing it
// that the union does not list carries no tag. Two discriminators, or one
// routing to branches composing the schema, keep the union whole with its
// branch, the model holding the schema's own (GitHub #1698).
func TestAllOf_ADiscriminatedBranchUnionDistributesAsBeside(t *testing.T) {
	t.Parallel()
	const cardBank = "oneOf: [{$ref: '#/components/schemas/Card'}, {$ref: '#/components/schemas/Bank'}]"
	const method = "discriminator: {propertyName: method}"
	for _, doc := range discriminatedOrders(t, "3.1.0", `    Payment:
      allOf:
        - {type: object, properties: {amount: {type: integer}}}
        - {`+cardBank+`, `+method+`}
    Refund:
      `+method+`
      allOf:
        - {type: object, properties: {amount: {type: integer}}}
        - {`+cardBank+`}
    Mapped:
      allOf:
        - {type: object, properties: {amount: {type: integer}}}
        - {`+cardBank+`, discriminator: {propertyName: method, mapping: {card: '#/components/schemas/Card'}}}
    Both:
      `+method+`
      allOf:
        - {type: object, properties: {amount: {type: integer}}}
        - {`+cardBank+`, `+method+`}
    Root:
      `+method+`
      allOf:
        - {type: object, properties: {method: {type: string}}}
        - {oneOf: [{$ref: '#/components/schemas/Sub1'}, {$ref: '#/components/schemas/Sub2'}]}
    Sub: {allOf: [{$ref: '#/components/schemas/Refund'}]}
`, `    Beside: {type: object, properties: {amount: {type: integer}}, `+cardBank+`, `+method+`}
    Sub1: {allOf: [{$ref: '#/components/schemas/Root'}, {properties: {one: {type: string}}}]}
    Sub2: {allOf: [{$ref: '#/components/schemas/Root'}, {properties: {two: {type: string}}}]}
    Card: {type: object, required: [method], properties: {method: {type: string}, last4: {type: string}}}
    Bank: {type: object, required: [method], properties: {method: {type: string}, iban: {type: string}}}
`) {
		want := routedBases(t, doc, "Beside")
		require.Equal(t, map[string]ir.TypeID{"Card": componentID("Card"), "Bank": componentID("Bank")}, want)
		for _, name := range []string{"Payment", "Refund"} {
			assert.Equal(t, want, routedBases(t, doc, name), name)
			for _, v := range variantModels(t, doc, name) {
				assert.Empty(t, v.Unmodeled, "%s: the branch gives its union and discriminator up", name)
			}
		}
		assert.Equal(t, map[string]ir.TypeID{"card": componentID("Card"), "Bank": componentID("Bank")},
			routedBases(t, doc, "Mapped"), "a mapping entry routes beside the route a name implies")
		for _, name := range []string{"Both", "Root"} {
			m, ok := typeByName(doc, name).(*ir.Model)
			require.True(t, ok, "%s keeps its union whole; got %T", name, typeByName(doc, name))
			require.NotNil(t, m.Discriminator, name)
			assert.Contains(t, m.Unmodeled, "openapi:allOf/1", name)
		}
		assert.Equal(t, "Sub1", typeByName(doc, "Sub1").(*ir.Model).DiscriminatorValue, "a subtype the union lists")
		sub, ok := typeByName(doc, "Sub").(*ir.Model)
		require.True(t, ok)
		assert.Empty(t, sub.DiscriminatorValue, "a schema the union does not list narrows it, untagged")
	}
}

// TestAllOf_AHierarchyBaseBranchUnionHoldsItsDiscriminatorToItsRoutes pins a
// union on an allOf branch whose schema's discriminator routes to a branch
// composing the schema (docs/compilers/openapi.md#union-beside-structure): the
// union stays with its branch, and the discriminator on the model only where
// every type it routes to is a subtype, as beside the union, else kept verbatim
// and reported, so the document validates in either declaration order.
func TestAllOf_AHierarchyBaseBranchUnionHoldsItsDiscriminatorToItsRoutes(t *testing.T) {
	t.Parallel()
	for _, doc := range discriminatedOrders(t, "3.1.0", `    Mixed:
      discriminator: {propertyName: kind, mapping: {cat: '#/components/schemas/Cat'}}
      allOf:
        - {type: object, properties: {kind: {type: string}}}
        - oneOf: [{$ref: '#/components/schemas/Puppy'}, {$ref: '#/components/schemas/Cat'}]
    ByName:
      discriminator: {propertyName: kind}
      allOf:
        - {type: object, properties: {kind: {type: string}}}
        - oneOf: [{$ref: '#/components/schemas/Kitten'}, {$ref: '#/components/schemas/Cat'}]
`, `    Puppy:
      allOf: [{$ref: '#/components/schemas/Mixed'}, {type: object, properties: {age: {type: integer}}}]
    Kitten:
      allOf: [{$ref: '#/components/schemas/ByName'}, {type: object, properties: {age: {type: integer}}}]
    Cat: {type: object, properties: {kind: {type: string}, meow: {type: boolean}}}
`) {
		for _, name := range []string{"Mixed", "ByName"} {
			m, ok := typeByName(doc, name).(*ir.Model)
			require.True(t, ok, "%s is a hierarchy's base; got %T", name, typeByName(doc, name))
			assert.Nil(t, m.Discriminator, "%s routes to Cat, no subtype of it, by a mapping or by name", name)
			assert.Contains(t, m.Unmodeled, "openapi:discriminator", name)
			assert.Contains(t, m.Unmodeled, "openapi:allOf/1", "%s: the union stays with its branch", name)
			assert.True(t, openapitest.HasDiagCodeAt(doc.Diagnostics, diag.DegradedConstruct, "/components/schemas/"+name+"/discriminator"))
		}
	}
}

// TestAllOf_ADiscriminatedBranchUnionReportsItsMappingAtTheBranch pins where a
// mapping entry of a discriminator written on an allOf branch that names no
// schema is reported: at the entry, under the branch writing it.
func TestAllOf_ADiscriminatedBranchUnionReportsItsMappingAtTheBranch(t *testing.T) {
	t.Parallel()
	_, diags := parseFull(t, openapitest.ComponentSpecVer("3.1.0", `    Payment:
      allOf:
        - {type: object, properties: {amount: {type: integer}}}
        - oneOf: [{$ref: '#/components/schemas/Card'}, {$ref: '#/components/schemas/Bank'}]
          discriminator: {propertyName: method, mapping: {gone: '#/components/schemas/Gone'}}
    Card: {type: object, properties: {last4: {type: string}}}
    Bank: {type: object, properties: {iban: {type: string}}}
`))
	assert.Len(t, diagsAtPointer(diags, diag.UnresolvedRef, "/components/schemas/Payment/allOf/1/discriminator/mapping/gone"), 1, "%+v", diags)
}

// TestAllOf_ABranchDiscriminatorKeepsWhatNoFieldHolds pins where the extensions
// and undefined keys of a discriminator an allOf branch writes beside its union
// go: a branch has no node, so they ride on the Union holding the discriminator,
// the position's own scoped by their path from it, a routed subtype's minted
// Union's by the keyword, each reported as such a key is anywhere.
func TestAllOf_ABranchDiscriminatorKeepsWhatNoFieldHolds(t *testing.T) {
	t.Parallel()
	const cardBank = "oneOf: [{$ref: '#/components/schemas/Card'}, {$ref: '#/components/schemas/Bank'}]"
	const disc = "discriminator: {propertyName: method, x-note: kept, extra: 1}"
	doc, diags := parseFull(t, openapitest.ComponentSpecVer("3.1.0", `    Payment:
      allOf: [{type: object, properties: {amount: {type: integer}}}, {`+cardBank+`, `+disc+`}]
    Pet:
      type: object
      properties: {kind: {type: string}}
      discriminator: {propertyName: kind}
    Dog:
      allOf: [{$ref: '#/components/schemas/Pet'}, {`+cardBank+`, `+disc+`}]
    Card: {type: object, required: [method], properties: {method: {type: string}}}
    Bank: {type: object, required: [method], properties: {method: {type: string}}}
`))
	openapitest.RequireNoErrorDiags(t, diags)
	for id, scope := range map[ir.TypeID]string{
		componentID("Payment"):                            "openapi:allOf/1/discriminator/",
		"t/composed/components/schemas/Dog/allOf/1/oneOf": "openapi:discriminator/",
	} {
		u, ok := doc.Types[id].(*ir.Union)
		require.True(t, ok, "%s is a union; got %T", id, doc.Types[id])
		require.NotNil(t, u.Discriminator, id)
		assert.Equal(t, ir.ReasonVendorExtension, u.Unmodeled[scope+"x-note"].Reason, id)
		assert.Equal(t, ir.ReasonOutOfScope, u.Unmodeled[scope+"extra"].Reason, id)
	}
	for _, name := range []string{"Payment", "Dog"} {
		assert.Len(t, diagsAtPointer(diags, diag.UnknownObjectKey, "/components/schemas/"+name+"/allOf/1/discriminator/extra"), 1, name)
	}
}

// routedBases spells the mapping of the union the component name lowers to by
// the Base of the composed variant each tag routes to.
func routedBases(t *testing.T, doc *ir.Document, name string) map[string]ir.TypeID {
	t.Helper()
	u, ok := typeByName(doc, name).(*ir.Union)
	require.True(t, ok, "%s is a union; got %T", name, typeByName(doc, name))
	require.NotNil(t, u.Discriminator, name)
	assert.True(t, u.WireTagged, name)
	out := map[string]ir.TypeID{}
	for tag, target := range u.Discriminator.Mapping {
		m, ok := doc.Types[target].(*ir.Model)
		require.True(t, ok, "%s routes %q to a composed variant", name, tag)
		require.NotNil(t, m.Base)
		out[tag] = m.Base.Target
	}
	return out
}

// TestUnion_ARoutedSubtypeComposesItsUnionAsAMixin pins a union beside or on an
// allOf branch of a schema a discriminated ancestor routes to, by a mapping key
// or its name (docs/compilers/openapi.md#union-beside-structure). Distributed,
// the schema would be a union, no subtype of the ancestor, so it stays a tagged
// model under it, naming as its last Mixin a Union minted for the union, the
// node a component holding the union would be. The model keeps nothing of the
// union, what a branch writes beside it merges, a discriminator beside it routes
// the Union, and the document validates, in either order (GitHub #1694).
func TestUnion_ARoutedSubtypeComposesItsUnionAsAMixin(t *testing.T) {
	t.Parallel()
	const sizes = "oneOf: [{$ref: '#/components/schemas/Small'}, {$ref: '#/components/schemas/Big'}]"
	for _, doc := range discriminatedOrders(t, "3.1.0", `    Dog: {allOf: [{$ref: '#/components/schemas/Pet'}], `+sizes+`}
    Cat:
      allOf: [{$ref: '#/components/schemas/Pet'}]
      oneOf: [{properties: {meow: {type: string}}}, {properties: {purr: {type: string}}}]
    Bird: {allOf: [{$ref: '#/components/schemas/Pet'}, {`+sizes+`}]}
    Fish:
      allOf:
        - $ref: '#/components/schemas/Pet'
        - {properties: {fins: {type: integer}}, anyOf: [{properties: {gills: {type: integer}}}, {$ref: '#/components/schemas/Big'}]}
    Hound:
      discriminator: {propertyName: size}
      allOf: [{$ref: '#/components/schemas/Pet'}, {`+sizes+`}]
    Lab: {allOf: [{$ref: '#/components/schemas/Pet'}, {$ref: '#/components/schemas/Collar'}], `+sizes+`}
    Wolf:
      discriminator: {propertyName: size, x-pack: true}
      allOf: [{$ref: '#/components/schemas/Pet'}]
      `+sizes+`
    Kennel: {allOf: [{$ref: '#/components/schemas/Dog'}, {properties: {name: {type: string}}}]}
    Outside: {$ref: '#/components/schemas/Bird/allOf/1'}
    Holder:
      type: object
      properties:
        hamster: {allOf: [{$ref: '#/components/schemas/Pet'}], `+sizes+`}
`, `    Pet:
      type: object
      required: [kind]
      properties: {kind: {type: string}}
      discriminator:
        propertyName: kind
        mapping:
          dog: '#/components/schemas/Dog'
          bird: '#/components/schemas/Bird'
          '': '#/components/schemas/Holder/properties/hamster'
    Small: {type: object, properties: {size: {type: integer}}}
    Big: {type: object, properties: {weight: {type: integer}}}
    Sizes: {`+sizes+`}
    Collar: {type: object, properties: {tag: {type: string}}}
`) {
		sizes := typeByName(doc, "Sizes").(*ir.Union)
		for name, tc := range map[ir.TypeID]struct {
			tag, at string
			own     []string
		}{
			componentID("Dog"): {"dog", "/components/schemas/Dog/oneOf", nil}, componentID("Cat"): {"Cat", "/components/schemas/Cat/oneOf", nil},
			componentID("Bird"):  {"bird", "/components/schemas/Bird/allOf/1/oneOf", nil},
			componentID("Fish"):  {"Fish", "/components/schemas/Fish/allOf/1/anyOf", []string{"fins"}},
			componentID("Hound"): {"Hound", "/components/schemas/Hound/allOf/1/oneOf", nil},
			componentID("Lab"):   {"Lab", "/components/schemas/Lab/oneOf", nil},
			"t/anon/components/schemas/Holder/properties/hamster": {"", "/components/schemas/Holder/properties/hamster/oneOf", nil},
		} {
			m, ok := doc.Types[name].(*ir.Model)
			require.True(t, ok, "%s stays a model; got %T", name, doc.Types[name])
			require.NotNil(t, m.Base, name)
			assert.Equal(t, componentID("Pet"), m.Base.Target, "%s is a subtype of Pet", name)
			assert.Equal(t, tc.tag, m.DiscriminatorValue, name)
			assert.Empty(t, m.Unmodeled, "%s keeps nothing of its union", name)
			assert.Nil(t, m.Discriminator, name)
			assert.Equal(t, tc.own, wireNames(m.Properties), "%s merges what the branch writes beside its union", name)
			require.NotEmpty(t, m.Mixins, name)
			mixin := m.Mixins[len(m.Mixins)-1].Target
			assert.Equal(t, ir.TypeID("t/composed"+tc.at), mixin, "%s names its union, minted where the keyword is", name)
			u, ok := doc.Types[mixin].(*ir.Union)
			require.True(t, ok, "%s's mixin is a union; got %T", name, doc.Types[mixin])
			assert.Equal(t, jsontext.Pointer(tc.at), u.Provenance.Pointer, name)
			if tc.at != "/components/schemas/Cat/oneOf" && tc.at != "/components/schemas/Fish/allOf/1/anyOf" {
				assert.Equal(t, sizes.Variants, u.Variants, "%s's union is what a component holding it is", name)
			}
		}
		assert.Equal(t, componentID("Collar"), typeByName(doc, "Lab").(*ir.Model).Mixins[0].Target,
			"the $refs the source writes classify first")
		hound := doc.Types["t/composed/components/schemas/Hound/allOf/1/oneOf"].(*ir.Union)
		require.NotNil(t, hound.Discriminator, "a discriminator beside the union routes the Union")
		assert.Equal(t, "size", hound.Discriminator.PropertyName)
		wolf := typeByName(doc, "Wolf").(*ir.Model)
		assert.Equal(t, ir.ReasonVendorExtension, wolf.Unmodeled["openapi:discriminator/x-pack"].Reason,
			"what the schema's own discriminator adds rides on its node")
		wolfUnion := doc.Types["t/composed/components/schemas/Wolf/oneOf"].(*ir.Union)
		require.NotNil(t, wolfUnion.Discriminator)
		assert.Empty(t, wolfUnion.Unmodeled, "and only there")
		assert.True(t, ir.IsSubtype(doc, componentID("Kennel"), componentID("Pet")), "through Dog")
		outside, ok := typeByName(doc, "Outside").(*ir.Scalar)
		require.True(t, ok, "an outside $ref names the branch itself; got %T", typeByName(doc, "Outside"))
		assert.Equal(t, ir.TypeID("t/anon/components/schemas/Bird/allOf/1"), outside.Base.Target)
	}
}

// TestUnion_ARoutedSubtypeSharedByTwoOperationsComposesItsUnionOnce pins a
// routed subtype written in a request body two operations share, so its
// position lowers once for each: the first composes the union, and the second
// finds it, minting nothing and composing nothing more.
func TestUnion_ARoutedSubtypeSharedByTwoOperationsComposesItsUnionOnce(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a:
    post:
      requestBody: {$ref: '#/components/requestBodies/PetBody'}
      responses: {'204': {description: ok}}
  /b:
    put:
      requestBody: {$ref: '#/components/requestBodies/PetBody'}
      responses: {'204': {description: ok}}
components:
  requestBodies:
    PetBody:
      content:
        application/json:
          schema:
            allOf:
              - $ref: '#/components/schemas/Pet'
              - oneOf: [{$ref: '#/components/schemas/Small'}, {$ref: '#/components/schemas/Big'}]
  schemas:
    Pet:
      type: object
      required: [kind]
      properties: {kind: {type: string}}
      discriminator:
        propertyName: kind
        mapping: {body: '#/components/requestBodies/PetBody/content/application~1json/schema'}
    Small: {type: object, properties: {size: {type: integer}}}
    Big: {type: object, properties: {weight: {type: integer}}}
`)
	openapitest.RequireNoErrorDiags(t, diags)
	assert.Empty(t, validated(t, doc))
	body, ok := doc.Types["t/anon/components/requestBodies/PetBody/content/application~1json/schema"].(*ir.Model)
	require.True(t, ok)
	assert.Equal(t, "body", body.DiscriminatorValue)
	assert.Equal(t, []ir.TypeRef{{Target: "t/composed/components/requestBodies/PetBody/content/application~1json/schema/allOf/1/oneOf"}},
		body.Mixins, "one union, composed once")
}

// wireNames lists the wire names of props, nil for none.
func wireNames(props []ir.Property) []string {
	if len(props) == 0 {
		return nil
	}
	out := make([]string, 0, len(props))
	for _, p := range props {
		out = append(out, p.WireName)
	}
	return out
}

// TestUnion_ABranchUnionNoRouteNamesDistributesAsBeside pins a union on an
// allOf branch of a schema below a discriminated base, which is routed or not at
// its position (docs/compilers/openapi.md#union-beside-structure): where no
// route names it, it distributes as the same union beside the allOf does, in
// either declaration order, and where a mapping key or the name of its
// component routes to it, it stays a tagged model composing the union as a
// mixin (GitHub #1695).
func TestUnion_ABranchUnionNoRouteNamesDistributesAsBeside(t *testing.T) {
	t.Parallel()
	const sizes = "oneOf: [{$ref: '#/components/schemas/Small'}, {$ref: '#/components/schemas/Big'}]"
	for _, doc := range discriminatedOrders(t, "3.1.0", `    Pet:
      type: object
      properties: {kind: {type: string}}
      discriminator:
        propertyName: kind
        mapping: {gerbil: '#/components/schemas/Holder/properties/gerbil'}
`, `    Small: {type: object, properties: {size: {type: integer}}}
    Big: {type: object, properties: {weight: {type: integer}}}
    Fish:
      allOf: [{$ref: '#/components/schemas/Pet'}, {`+sizes+`}]
    Holder:
      type: object
      properties:
        beside: {allOf: [{$ref: '#/components/schemas/Pet'}], `+sizes+`}
        branch: {allOf: [{$ref: '#/components/schemas/Pet'}, {`+sizes+`}]}
        gerbil: {allOf: [{$ref: '#/components/schemas/Pet'}, {`+sizes+`}]}
`) {
		beside, ok := doc.Types["t/anon/components/schemas/Holder/properties/beside"].(*ir.Union)
		require.True(t, ok, "beside the allOf, the union distributes")
		branch, ok := doc.Types["t/anon/components/schemas/Holder/properties/branch"].(*ir.Union)
		require.True(t, ok, "and on its branch, got %T", doc.Types["t/anon/components/schemas/Holder/properties/branch"])
		require.Len(t, branch.Variants, len(beside.Variants))
		for i := range beside.Variants {
			want, got := doc.Types[beside.Variants[i].Type.Target].(*ir.Model), doc.Types[branch.Variants[i].Type.Target].(*ir.Model)
			require.NotNil(t, got, "variant %d is a composed model", i)
			assert.Equal(t, want.Base, got.Base, "variant %d", i)
			assert.Equal(t, want.Mixins, got.Mixins, "variant %d", i)
			assert.Empty(t, got.DiscriminatorValue, "variant %d is no routed subtype", i)
		}
		for id, tag := range map[ir.TypeID]string{componentID("Fish"): "Fish", "t/anon/components/schemas/Holder/properties/gerbil": "gerbil"} {
			m, ok := doc.Types[id].(*ir.Model)
			require.True(t, ok, "%s, a schema a route names, stays a model; got %T", id, doc.Types[id])
			assert.Equal(t, tag, m.DiscriminatorValue, id)
			require.Len(t, m.Mixins, 1, id)
			_, isUnion := doc.Types[m.Mixins[0].Target].(*ir.Union)
			assert.True(t, isUnion, "%s composes its union", id)
		}
	}
}

// TestUnion_ASchemaNoRouteNamesDistributesBesideItsAllOf pins a union beside
// the allOf of a component composing a schema whose discriminator is written
// beside a union that does not list it (docs/compilers/openapi.md#union-beside-structure):
// no route names the component (docs/compilers/openapi.md#discriminator-value),
// so the union distributes over its composition, its variants untagged, where
// one the union lists stays a tagged subtype composing its union. Each is read
// in both declaration orders.
func TestUnion_ASchemaNoRouteNamesDistributesBesideItsAllOf(t *testing.T) {
	t.Parallel()
	const sizes = "      oneOf: [{$ref: '#/components/schemas/Small'}, {$ref: '#/components/schemas/Big'}]\n"
	const rest = "    Small: {type: object, properties: {size: {type: integer}}}\n" +
		"    Big: {type: object, properties: {weight: {type: integer}}}\n" +
		"    Stored:\n      allOf: [{$ref: '#/components/schemas/Rule'}, {properties: {id: {type: string}}}]\n" + sizes
	for name, tc := range map[string]struct {
		base   string
		listed bool // whether the union Rule's discriminator is written beside lists Listed
	}{
		"beside a union": {"    Rule:\n      discriminator: {propertyName: action}\n" +
			"      oneOf: [{$ref: '#/components/schemas/Block'}, {$ref: '#/components/schemas/Log'}]\n" + pinnedRules, false},
		"beside a hierarchy's union": {"    Rule:\n      type: object\n      properties: {action: {type: string}}\n" +
			"      discriminator: {propertyName: action}\n      oneOf: [{$ref: '#/components/schemas/Listed'}]\n" +
			"    Listed:\n      allOf: [{$ref: '#/components/schemas/Rule'}, {properties: {note: {type: string}}}]\n" + sizes, true},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			for _, doc := range taggedOrders(t, tc.base, rest) {
				u, ok := typeByName(doc, "Stored").(*ir.Union)
				require.True(t, ok, "Stored distributes; got %T", typeByName(doc, "Stored"))
				for _, v := range variantModels(t, doc, "Stored") {
					assert.Equal(t, componentID("Rule"), v.Base.Target, "each variant composes Rule")
					assert.Empty(t, v.DiscriminatorValue, "and no variant is tagged")
				}
				assert.Len(t, u.Variants, 2)
				if !tc.listed {
					continue
				}
				listed, ok := typeByName(doc, "Listed").(*ir.Model)
				require.True(t, ok, "Listed stays a model; got %T", typeByName(doc, "Listed"))
				assert.Equal(t, "Listed", listed.DiscriminatorValue, "a component the union lists is routed by name")
				assert.Empty(t, listed.Unmodeled, "and composes its union")
				require.NotEmpty(t, listed.Mixins)
				assert.Equal(t, ir.TypeID("t/composed/components/schemas/Listed/oneOf"), listed.Mixins[len(listed.Mixins)-1].Target)
			}
		})
	}
}

// TestOneOf_CoDeclaredTypeReadsNullableByItsDialect pins the null a `type`
// beside a union is compared with (docs/compilers/openapi.md#union-beside-structure):
// a `nullable: true` beside it adds null where the dialect defines the keyword,
// so in 3.0 the type says a branch admitting null and the union is the schema,
// and in 3.1 it adds nothing, so the body is kept beside the union.
func TestOneOf_CoDeclaredTypeReadsNullableByItsDialect(t *testing.T) {
	t.Parallel()
	for version, tc := range map[string]struct {
		nullString string
		alone      bool
	}{
		"3.0.3": {"{type: string, nullable: true}", true},
		"3.1.0": {"{type: [string, 'null']}", false},
	} {
		doc, diags := schema.LowerSpec(t, openapitest.ComponentSpecVer(version, `    V4: {type: string, format: ipv4}
    N: `+tc.nullString+`
    S: {type: string, nullable: true, oneOf: [{$ref: '#/components/schemas/V4'}, {$ref: '#/components/schemas/N'}]}
`))
		openapitest.RequireNoErrorDiags(t, diags)
		_, alone := typeByName(doc, "S").(*ir.Union)
		assert.Equal(t, tc.alone, alone, "%s: S lowers to the union alone", version)
	}
}

// variantModels returns the composed models the component name's Union
// distributes its body across, in variant order.
func variantModels(t *testing.T, doc *ir.Document, name string) []*ir.Model {
	t.Helper()
	u, ok := typeByName(doc, name).(*ir.Union)
	require.True(t, ok, "%s is a union; got %T", name, typeByName(doc, name))
	out := make([]*ir.Model, 0, len(u.Variants))
	for _, v := range u.Variants {
		m, ok := doc.Types[v.Type.Target].(*ir.Model)
		require.True(t, ok, "%s variant %s is a composed model", name, v.Type.Target)
		out = append(out, m)
	}
	return out
}

// TestOneOf_CoDeclaredDiscriminatorRoutesTheComposedVariants pins a
// discriminator beside a union none of whose branches composes the body beside
// it (docs/compilers/openapi.md#union-beside-structure): the body distributes,
// and the discriminator routes each variant composed over a branch. A mapping
// target or the default naming a branch's component routes to its variant, and
// a variant no entry routes takes the component name its branch names, the
// route OpenAPI implies, unless another branch names it too or an entry spells
// that key. An inline branch names none, so its variant takes no route.
func TestOneOf_CoDeclaredDiscriminatorRoutesTheComposedVariants(t *testing.T) {
	t.Parallel()
	const variants = "    A: {type: object, properties: {kind: {type: string}}}\n" +
		"    B: {type: object, properties: {kind: {type: string}}}\n"
	body := func(discriminator, branches string) string {
		return "    U:\n      type: object\n      properties: {note: {type: string}}\n" +
			"      discriminator: " + discriminator + "\n      oneOf: [" + branches + "]\n"
	}
	const ab = "{$ref: '#/components/schemas/A'}, {$ref: '#/components/schemas/B'}"
	cases := []struct {
		name, version, discriminator, branches string
		want                                   map[string]int // tag -> variant index
		fallback                               int            // the default's variant, -1 for none
	}{
		{"mapped", "3.1.0", "{propertyName: kind, mapping: {a: '#/components/schemas/A', b: B}}", ab,
			map[string]int{"a": 0, "b": 1}, -1},
		{"unmapped", "3.1.0", "{propertyName: kind}", ab, map[string]int{"A": 0, "B": 1}, -1},
		{"mapped in part", "3.1.0", "{propertyName: kind, mapping: {a: '#/components/schemas/A'}}", ab,
			map[string]int{"a": 0, "B": 1}, -1},
		{"a key spelled for another", "3.1.0", "{propertyName: kind, mapping: {B: '#/components/schemas/A'}}", ab,
			map[string]int{"B": 0}, -1},
		{"one component twice", "3.1.0", "{propertyName: kind}",
			"{$ref: '#/components/schemas/A'}, {$ref: '#/components/schemas/A'}, {$ref: '#/components/schemas/B'}",
			map[string]int{"B": 2}, -1},
		{"an inline branch", "3.1.0", "{propertyName: kind}",
			"{$ref: '#/components/schemas/A'}, {type: object, properties: {kind: {type: string}}}",
			map[string]int{"A": 0}, -1},
		{"default", "3.2.0", "{propertyName: kind, mapping: {a: A}, defaultMapping: '#/components/schemas/B'}", ab,
			map[string]int{"a": 0, "B": 1}, 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			union := body(c.discriminator, c.branches)
			for _, src := range []string{union + variants, variants + union} {
				doc, diags := parseFull(t, openapitest.ComponentSpecVer(c.version, src))
				openapitest.RequireNoErrorDiags(t, diags)
				assert.Empty(t, validated(t, doc), "every route names a variant")
				u, ok := typeByName(doc, "U").(*ir.Union)
				require.True(t, ok, "the body distributes; got %T", typeByName(doc, "U"))
				require.NotNil(t, u.Discriminator)
				want := make(map[string]ir.TypeID, len(c.want))
				for tag, i := range c.want {
					want[tag] = u.Variants[i].Type.Target
				}
				assert.Equal(t, want, u.Discriminator.Mapping)
				if c.fallback >= 0 {
					assert.Equal(t, u.Variants[c.fallback].Type.Target, u.Discriminator.Default)
				}
				assert.Empty(t, u.Unmodeled, "the union homes the discriminator")
				assert.Empty(t, u.Provenance.Inferred, "a route OpenAPI implies is no inference")
				for _, v := range u.Variants {
					m, ok := doc.Types[v.Type.Target].(*ir.Model)
					require.True(t, ok)
					assert.Empty(t, m.DiscriminatorValue, "the union's mapping holds the tag")
				}
			}
		})
	}
}

// TestOneOf_CoDeclaredDiscriminatorImpliesNoRouteIntoAnotherDocument pins
// that the route OpenAPI implies for a composed variant is the name of a
// component in the document writing the union: a branch naming another
// document's component takes none, though the union's own document declares
// one of the same name.
func TestOneOf_CoDeclaredDiscriminatorImpliesNoRouteIntoAnotherDocument(t *testing.T) {
	t.Parallel()
	doc, diags, _ := compileBeside(t,
		map[string]string{"other.yaml": "components:\n  schemas:\n    Cat: {type: object, properties: {kind: {type: string}}}\n"},
		`openapi: 3.1.0
info: {title: T, version: "1"}
paths: {}
components:
  schemas:
    U:
      type: object
      properties: {note: {type: string}}
      discriminator: {propertyName: kind}
      oneOf: [{$ref: './other.yaml#/components/schemas/Cat'}, {$ref: '#/components/schemas/Dog'}]
    Cat: {type: object, properties: {meow: {type: string}}}
    Dog: {type: object, properties: {kind: {type: string}}}
`)
	openapitest.RequireNoErrorDiags(t, diags)
	u, ok := typeByName(doc, "U").(*ir.Union)
	require.True(t, ok, "the body distributes; got %T", typeByName(doc, "U"))
	require.Len(t, u.Variants, 2)
	require.NotNil(t, u.Discriminator)
	assert.Equal(t, map[string]ir.TypeID{"Dog": u.Variants[1].Type.Target}, u.Discriminator.Mapping)
}
