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
// is, keeping nothing and reporting nothing. A `format` the branches do not all
// say stays beside the union, and a body keyword they do not all say keeps the
// body: a null the `type` leaves out, a branch writing no `type`, branches
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
`))
	openapitest.RequireNoErrorDiags(t, diags)

	bare, ok := typeByName(doc, "Bare").(*ir.Union)
	require.True(t, ok)
	typed, ok := typeByName(doc, "Typed").(*ir.Union)
	require.True(t, ok, "a type every branch says leaves the union alone")
	assert.Equal(t, bare.Variants, typed.Variants, "as its bare spelling lowers")
	for _, name := range []string{"Typed", "Formatted", "Valued", "Constant", "Listed", "Objects"} {
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

// TestAllOf_AUnionBranchDistributesAsBesideTheAllOf pins a oneOf or anyOf
// written as an inline allOf branch, which distributes as the same union beside
// the allOf does (docs/compilers/openapi.md#union-beside-structure): the
// variants match the beside spelling's, inline branches conjoined by their own
// nodes, the branch keeps nothing, and another branch folds nothing, kept whole
// on each variant. One declaring no shape, one of two that do, one beside the
// schema's own union or discriminator, one distribution declines, one past the
// flattening depth and one beside a branch's $ref, which it conjoins with, stay
// with their branch; an enum beside the allOf decides the position.
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
    RefBranch:
      allOf:
        - {$ref: '#/components/schemas/A', oneOf: [{$ref: '#/components/schemas/B'}]}
        - {type: object, properties: {c: {type: string}}}
    DiscriminatedBranch:
      allOf:
        - oneOf: [{$ref: '#/components/schemas/A'}, {$ref: '#/components/schemas/B'}]
          discriminator: {propertyName: a}
        - {type: object, properties: {c: {type: string}}}
    OwnDiscriminator:
      discriminator: {propertyName: c}
      allOf:
        - oneOf: [{$ref: '#/components/schemas/A'}, {$ref: '#/components/schemas/B'}]
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
		"Shapeless":           {"openapi:allOf/0"},
		"Two":                 {"openapi:allOf/0", "openapi:allOf/1"},
		"NullBranch":          {"openapi:allOf/0"},
		"RefBranch":           nil,
		"DiscriminatedBranch": {"openapi:allOf/0"},
		"OwnDiscriminator":    {"openapi:allOf/0"},
		"Capped":              {capped},
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
	assert.Empty(t, annotated.Docs.Description, "a distributed branch folds nothing, as beside the allOf")
	for _, v := range variantModels(t, doc, "Annotated") {
		assert.Contains(t, v.Unmodeled, "openapi:allOf/1", "each variant keeps it whole")
	}
	for _, v := range variantModels(t, doc, "BesideToo") {
		assert.Contains(t, v.Unmodeled, "openapi:allOf/0", "beside its own union, a branch's stays with the branch")
	}
}

// TestUnion_ASubtypeADiscriminatorRoutesToStaysASubtype pins a union beside or
// on an allOf branch of a schema a discriminated ancestor routes to, by a
// mapping key or its name as a component
// (docs/compilers/openapi.md#union-beside-structure). Distributed, the schema
// would be a union, no subtype of the ancestor, so it stays a tagged model
// under it, its union kept verbatim, and the document validates, a subtype of
// it included. An inline schema no route names distributes beside the allOf,
// its variants untagged, and stays whole on its branch; a mapping key routes to
// one, "" among them.
func TestUnion_ASubtypeADiscriminatorRoutesToStaysASubtype(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, openapitest.ComponentSpecVer("3.1.0", `    Pet:
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
    Dog:
      allOf: [{$ref: '#/components/schemas/Pet'}]
      oneOf: [{$ref: '#/components/schemas/Small'}, {$ref: '#/components/schemas/Big'}]
    Cat:
      allOf: [{$ref: '#/components/schemas/Pet'}]
      oneOf: [{properties: {meow: {type: string}}}, {properties: {purr: {type: string}}}]
    Bird:
      allOf:
        - $ref: '#/components/schemas/Pet'
        - oneOf: [{$ref: '#/components/schemas/Small'}, {$ref: '#/components/schemas/Big'}]
    Fish:
      allOf:
        - $ref: '#/components/schemas/Pet'
        - anyOf: [{properties: {fins: {type: integer}}}, {$ref: '#/components/schemas/Big'}]
    Kennel:
      allOf: [{$ref: '#/components/schemas/Dog'}, {properties: {name: {type: string}}}]
    Holder:
      type: object
      properties:
        beside:
          allOf: [{$ref: '#/components/schemas/Pet'}]
          oneOf: [{$ref: '#/components/schemas/Small'}, {$ref: '#/components/schemas/Big'}]
        branch:
          allOf:
            - $ref: '#/components/schemas/Pet'
            - oneOf: [{$ref: '#/components/schemas/Small'}, {$ref: '#/components/schemas/Big'}]
        hamster:
          allOf: [{$ref: '#/components/schemas/Pet'}]
          oneOf: [{$ref: '#/components/schemas/Small'}, {$ref: '#/components/schemas/Big'}]
`))
	openapitest.RequireNoErrorDiags(t, diags)
	assert.Empty(t, validated(t, doc), "every route names a subtype, so the document validates")

	for name, tc := range map[string]struct{ tag, kept string }{
		"Dog": {"dog", "openapi:oneOf"}, "Cat": {"Cat", "openapi:oneOf"},
		"Bird": {"bird", "openapi:allOf/1"}, "Fish": {"Fish", "openapi:allOf/1"},
	} {
		m, ok := typeByName(doc, name).(*ir.Model)
		require.True(t, ok, "%s stays a model; got %T", name, typeByName(doc, name))
		require.NotNil(t, m.Base, name)
		assert.Equal(t, componentID("Pet"), m.Base.Target, "%s is a subtype of Pet", name)
		assert.Equal(t, tc.tag, m.DiscriminatorValue, name)
		assert.Equal(t, ir.ReasonDegradedLowering, m.Unmodeled[tc.kept].Reason, "%s keeps its union", name)
	}
	assert.Contains(t,
		openapitest.DiagMessageAt(t, diags, diag.DegradedConstruct, ir.SeverityInfo, "/components/schemas/Dog"),
		"a discriminator on an ancestor routes to this schema as its subtype")
	assert.True(t, ir.IsSubtype(doc, componentID("Kennel"), componentID("Pet")), "through Dog")

	beside, ok := doc.Types["t/anon/components/schemas/Holder/properties/beside"].(*ir.Union)
	require.True(t, ok, "an inline schema no route names distributes beside the allOf")
	for _, v := range beside.Variants {
		m, ok := doc.Types[v.Type.Target].(*ir.Model)
		require.True(t, ok)
		assert.Empty(t, m.DiscriminatorValue, "and no variant is tagged")
	}
	_, ok = doc.Types["t/anon/components/schemas/Holder/properties/branch"].(*ir.Model)
	assert.True(t, ok, "and stays a model on its branch")
	hamster, ok := doc.Types["t/anon/components/schemas/Holder/properties/hamster"].(*ir.Model)
	require.True(t, ok, "one a mapping key routes to stays a model, the key \"\" among them")
	assert.Contains(t, hamster.Unmodeled, "openapi:oneOf")
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
