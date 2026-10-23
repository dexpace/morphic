package schema_test

import (
	"encoding/json/jsontext"
	"fmt"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/pass"
)

// TestDistributedUnion_EachVariantCarriesTheRequiresOfItsBody pins that every
// variant model a union is distributed into takes the whole body's required
// list: a declared name is Required on the variant's copy of the property, and
// one nothing declares is in the variant's Requires. Each variant is a model of
// its own, so a variant that lowered the body's properties but not its
// required list would leave a name required on one reading and not the other.
func TestDistributedUnion_EachVariantCarriesTheRequiresOfItsBody(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpec(`    X: {type: object, properties: {x: {type: string}}}
    Y: {type: object, properties: {y: {type: string}}}
    Shape:
      properties: {a: {type: string}}
      required: [a, z]
      oneOf:
        - {$ref: '#/components/schemas/X'}
        - {$ref: '#/components/schemas/Y'}
`)
	doc, diags := lowerSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)

	u, ok := typeByName(doc, "Shape").(*ir.Union)
	require.True(t, ok, "Shape distributes into a union")
	require.Len(t, u.Variants, 2)
	for i, v := range u.Variants {
		m, ok := doc.Types[v.Type.Target].(*ir.Model)
		require.True(t, ok, "variant %d is a composed model", i)
		require.Len(t, m.Properties, 1, "variant %d keeps the body's property", i)
		assert.Equal(t, "a", m.Properties[0].WireName)
		assert.True(t, m.Properties[0].Required, "variant %d: a is declared, so it is required on its property", i)
		assert.Equal(t, []string{"z"}, m.Requires, "variant %d: z is declared nowhere, so it is listed", i)
	}
}

// TestDistributedUnion_VariantCarriesTheBranchNullability pins that a union
// distributed across its branches answers what the plain union over the same
// branches does: the TypeRef naming each synthesized variant model carries the
// branch's Nullable bit, unless the body's own type forbids null.
//
// The two distributed rows differ only in the enclosing `type: object`, because
// the variant is the body conjoined with the branch, so the body's own type
// keyword decides as much as the branch does. Copying the branch's bit
// unconditionally passes the untyped row and fails the typed one.
func TestDistributedUnion_VariantCarriesTheBranchNullability(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpec(`    NullableModel:
      type: [object, "null"]
      properties: {a: {type: string}}
    PlainModel:
      type: object
      properties: {b: {type: string}}
    PlainUnion:
      oneOf:
        - {$ref: '#/components/schemas/NullableModel'}
        - {$ref: '#/components/schemas/PlainModel'}
    DistributedUntyped:
      properties: {z: {type: string}}
      oneOf:
        - {$ref: '#/components/schemas/NullableModel'}
        - {$ref: '#/components/schemas/PlainModel'}
    DistributedTyped:
      type: object
      properties: {z: {type: string}}
      oneOf:
        - {$ref: '#/components/schemas/NullableModel'}
        - {$ref: '#/components/schemas/PlainModel'}
`)
	doc, diags := lowerSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)

	nullableOf := func(name string) []bool {
		t.Helper()
		u, isUnion := typeByName(doc, name).(*ir.Union)
		require.True(t, isUnion, "%s is a union", name)
		require.Len(t, u.Variants, 2, "%s keeps one variant per branch", name)
		return []bool{u.Variants[0].Type.Nullable, u.Variants[1].Type.Nullable}
	}

	assert.Equal(t, []bool{true, false}, nullableOf("PlainUnion"),
		"a plain union's variant carries its branch's nullability")
	assert.Equal(t, nullableOf("PlainUnion"), nullableOf("DistributedUntyped"),
		"distributing a body that declares no type does not change what a branch admits")
	assert.Equal(t, []bool{false, false}, nullableOf("DistributedTyped"),
		"a body declaring type: object conjoins with every branch and forbids null")

	// The synthesized variant model still names its branch as a plain conjunct.
	base, isModel := doc.Types[ir.TypeID("t/composed/components/schemas/DistributedUntyped/oneOf/0")].(*ir.Model)
	require.True(t, isModel, "the untyped distribution synthesizes a variant model")
	require.NotNil(t, base.Base)
	assert.False(t, base.Base.Nullable, "the variant model's Base names a conjunct and carries no bit")
}

// TestUnionVariant_IsNamedAsTheNodeItHolds pins GitHub #521: a union variant
// naming a $ref is named as the type it holds, not the pointer's own ordinal
// or keyword. Choice/oneOf/0 targets X's inline first branch and Choice/oneOf/1
// a plain structural position (A's items). The other variants target a branch
// that is itself a $ref, so naming them chases it on to its own target
// (naming.Namer.Through): Choice/oneOf/2 and Z/oneOf/0 take two hops to reach Y's items,
// and Choice/oneOf/3 three, so a hop cap below three fails that row.
func TestUnionVariant_IsNamedAsTheNodeItHolds(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpec(`    Choice: {oneOf: [{$ref: '#/components/schemas/X/oneOf/0'}, {$ref: '#/components/schemas/A/items'},
                     {$ref: '#/components/schemas/X/oneOf/1'}, {$ref: '#/components/schemas/Z/oneOf/0'}]}
    X: {oneOf: [{type: object, properties: {a: {type: string}}}, {$ref: '#/components/schemas/Y/items'}]}
    Z: {oneOf: [{$ref: '#/components/schemas/X/oneOf/1'}]}
    A: {type: array, items: {type: object, properties: {b: {type: string}}}}
    Y: {type: array, items: {type: object, properties: {y: {type: string}}}}
`)
	doc, diags := lowerSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)

	choice, ok := typeByName(doc, "Choice").(*ir.Union)
	require.True(t, ok, "Choice should be a union")
	require.Len(t, choice.Variants, 4)
	z, ok := typeByName(doc, "Z").(*ir.Union)
	require.True(t, ok, "Z should be a union")
	require.Len(t, z.Variants, 1)

	got := variantsNamingTheirNodes(t, doc, map[string]ir.Variant{
		"Choice/oneOf/0 (X's own inline branch)": choice.Variants[0],
		"Choice/oneOf/1 (A's items)":             choice.Variants[1],
		"Choice/oneOf/2 (X/oneOf/1, two hops)":   choice.Variants[2],
		"Choice/oneOf/3 (Z/oneOf/0, three hops)": choice.Variants[3],
		"Z/oneOf/0 (X/oneOf/1, two hops)":        z.Variants[0],
	})
	want := map[string]string{
		"Choice/oneOf/0 (X's own inline branch)": "variant_0",
		"Choice/oneOf/1 (A's items)":             "a_item",
		"Choice/oneOf/2 (X/oneOf/1, two hops)":   "y_item",
		"Choice/oneOf/3 (Z/oneOf/0, three hops)": "y_item",
		"Z/oneOf/0 (X/oneOf/1, two hops)":        "y_item",
	}
	assert.Empty(t, cmp.Diff(want, got))
}

// TestUnionVariant_UnderPathsIsNamedAsTheNodeItHolds is the same property for
// variants naming positions under /paths, where the pointer does not spell the
// enclosing hint. A branch is named by its ordinal, a branch holding a $ref
// after its target — through a second branch here, for two hops — and a
// property by its key. A position its response or a parameter names, the body
// itself or a parameter's schema, is named for them (GitHub #729). Each variant
// agrees with the node it holds.
func TestUnionVariant_UnderPathsIsNamedAsTheNodeItHolds(t *testing.T) {
	t.Parallel()
	const body = "#/paths/~1x/get/responses/200/content/application~1json/schema/properties/"
	doc, diags := parseFull(t, `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /x:
    get:
      operationId: getX
      parameters:
        - {name: filterBy, in: query, schema: {type: object, properties: {f: {type: string}}}}
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema:
                type: object
                properties:
                  held: {$ref: '#/components/schemas/T'}
                  choice:
                    oneOf:
                      - {type: object, properties: {a: {type: string}}}
                      - {$ref: '#/components/schemas/T'}
                      - {$ref: '`+body+`choice/oneOf/1'}
components:
  schemas:
    T: {type: object, properties: {t: {type: string}}}
    Choice:
      oneOf:
        - $ref: '`+body+`choice/oneOf/0'
        - $ref: '`+body+`choice/oneOf/1'
        - $ref: '`+body+`choice/oneOf/2'
        - $ref: '`+body+`held'
        - $ref: '#/paths/~1x/get/responses/200/content/application~1json/schema'
        - $ref: '#/paths/~1x/get/parameters/0/schema'
`)
	openapitest.RequireNoErrorDiags(t, diags)

	choice, ok := typeByName(doc, "Choice").(*ir.Union)
	require.True(t, ok, "Choice should be a union")
	require.Len(t, choice.Variants, 6)

	got := variantsNamingTheirNodes(t, doc, map[string]ir.Variant{
		"an inline branch":               choice.Variants[0],
		"a branch holding a $ref":        choice.Variants[1],
		"a branch holding a $ref to one": choice.Variants[2],
		"a property holding a $ref":      choice.Variants[3],
		"the response's body":            choice.Variants[4],
		"a parameter's schema":           choice.Variants[5],
	})
	want := map[string]string{
		"an inline branch":               "variant_0",
		"a branch holding a $ref":        "t",
		"a branch holding a $ref to one": "t",
		"a property holding a $ref":      "held",
		"the response's body":            "response",
		"a parameter's schema":           "filter_by",
	}
	assert.Empty(t, cmp.Diff(want, got))
}

// TestUnionVariant_AWholeDocumentIsNamedForIt is the same property for a
// branch whose $ref names another document whole: the document's root, which
// no owner holds, is named for the document, so the variant and the node it
// holds are both named after the file, as is a wrapped branch's variant
// (GitHub #1018). A position inside the document keeps its own name.
func TestUnionVariant_AWholeDocumentIsNamedForIt(t *testing.T) {
	t.Parallel()
	doc, diags, _ := compileBeside(t, map[string]string{
		"Cat.yaml": "type: object\nproperties:\n  toy: {type: object, properties: {name: {type: string}}}\n",
	}, `openapi: 3.0.3
info: {title: T, version: "1"}
paths: {}
components:
  schemas:
    Pet:
      oneOf:
        - $ref: './Cat.yaml'
        - {allOf: [{$ref: './Cat.yaml'}], description: a cat}
        - $ref: './Cat.yaml#/properties/toy'
`)
	openapitest.RequireNoErrorDiags(t, diags)

	pet, ok := typeByName(doc, "Pet").(*ir.Union)
	require.True(t, ok, "Pet should be a union")
	require.Len(t, pet.Variants, 3)
	got := variantsNamingTheirNodes(t, doc, map[string]ir.Variant{
		"the document":     pet.Variants[0],
		"a position in it": pet.Variants[2],
	})
	assert.Empty(t, cmp.Diff(map[string]string{"the document": "cat_yaml", "a position in it": "toy"}, got))
	assert.Equal(t, "cat_yaml", pet.Variants[1].Name.Hint, "the wrapped branch is named for its $ref")
}

func TestOneOf_WithDiscriminator(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpec(`    Cat:
      type: object
      properties:
        petType: {type: string}
    Dog:
      type: object
      properties:
        petType: {type: string}
    Pet:
      oneOf:
        - {$ref: "#/components/schemas/Cat"}
        - {$ref: "#/components/schemas/Dog"}
      discriminator:
        propertyName: petType
        mapping:
          cat: "#/components/schemas/Cat"
`)
	doc, diags := lowerSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	pet, ok := doc.Types[componentID("Pet")].(*ir.Union)
	require.True(t, ok, "Pet should be a union")
	assert.True(t, pet.Exclusive, "oneOf is exclusive")
	assert.True(t, pet.WireTagged, "the discriminator's tag property names the variant (ir-design §4.4)")
	require.Len(t, pet.Variants, 2, "both variants present")
	require.NotNil(t, pet.Discriminator)
	assert.Equal(t, "petType", pet.Discriminator.PropertyName)
	assert.Equal(t, map[string]ir.TypeID{"cat": "t/openapi/components/schemas/Cat"},
		pet.Discriminator.Mapping)
}

func TestAnyOf_IsNonExclusiveUnion(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpec(`    U:
      anyOf:
        - {type: string}
        - {type: integer}
`)
	doc, diags := lowerSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	u, ok := doc.Types[componentID("U")].(*ir.Union)
	require.True(t, ok, "U should be a union")
	assert.False(t, u.Exclusive, "anyOf is non-exclusive")
	assert.False(t, u.WireTagged, "with no discriminator, validation infers the variant")
	require.Len(t, u.Variants, 2)
}

func TestOneOf_NullVariantCollapses(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpec(`    S:
      type: object
      properties:
        p:
          oneOf:
            - {type: string}
            - {type: "null"}
`)
	doc, diags := lowerSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	s, ok := doc.Types[componentID("S")].(*ir.Model)
	require.True(t, ok)
	require.Len(t, s.Properties, 1)
	assert.Equal(t, ir.TypeRef{Target: "t/prim/string", Nullable: true}, s.Properties[0].Type)
	for id, def := range doc.Types {
		_, isUnion := def.(*ir.Union)
		assert.False(t, isUnion, "null-variant oneOf must not produce a union node: %s", id)
	}
}

// TestOneOfAnyOf_NullOnlyLowersToNullableAny covers a oneOf/anyOf whose every
// branch is a bare `type: null` schema (GitHub #416). nullUnionCollapse has no
// non-null branch to collapse a set like this onto, so without a guard for the
// shape, buildUnion's null-branch strip leaves nothing behind it and interns a
// Union with no variants — the empty value space irverify's
// ir/union-no-variants rule rejects. This position instead lowers the way a
// bare `{type: null}` schema at it would: the shared `any` primitive with
// Nullable set, with the combinator that produced it kept verbatim under
// Unmodeled since it has no shape a Scalar's fields could hold.
func TestOneOfAnyOf_NullOnlyLowersToNullableAny(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpec(`    AnyOfNull:
      anyOf: [{type: "null"}]
    OneOfNull:
      oneOf: [{type: "null"}]
    AnyOfTwoNull:
      anyOf: [{type: "null"}, {type: "null"}]
`)
	doc, diags := lowerSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)

	for _, tc := range []struct{ name, key string }{
		{"AnyOfNull", "openapi:anyOf"},
		{"OneOfNull", "openapi:oneOf"},
		{"AnyOfTwoNull", "openapi:anyOf"},
	} {
		s, ok := doc.Types[componentID(tc.name)].(*ir.Scalar)
		require.True(t, ok, "%s lowers to a scalar over any, the same node a bare "+
			"`{type: null}` schema at this position would", tc.name)
		require.NotNil(t, s.Base)
		assert.Equal(t, ir.TypeID("t/prim/any"), s.Base.Target)
		assert.True(t, s.Base.Nullable, "%s: the null-only branch set lifts onto the reference", tc.name)
		entry, ok := s.Unmodeled[tc.key]
		require.True(t, ok, "%s: the union is kept verbatim beside the approximation", tc.name)
		assert.Equal(t, ir.ReasonDegradedLowering, entry.Reason)
	}
	for id, def := range doc.Types {
		_, isUnion := def.(*ir.Union)
		assert.False(t, isUnion, "a null-only branch set must not produce a union node: %s", id)
	}
}

func TestOneOf_ThreeVariantsWithNullStripsNullLiftsNullable(t *testing.T) {
	t.Parallel()
	// A oneOf with two non-null branches plus a null branch stays a Union of the
	// two non-null variants (the null branch is NOT emitted as an `any` variant),
	// and the enclosing ref becomes Nullable.
	spec := openapitest.ComponentSpec(`    S:
      type: object
      properties:
        p:
          oneOf:
            - {type: string}
            - {type: integer}
            - {type: "null"}
`)
	doc, diags := lowerSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	s := doc.Types[componentID("S")].(*ir.Model)
	require.Len(t, s.Properties, 1)
	ref := s.Properties[0].Type
	assert.True(t, ref.Nullable, "the null branch lifts onto the enclosing ref")

	u, ok := doc.Types[ref.Target].(*ir.Union)
	require.True(t, ok, "the non-null branches form a Union")
	require.Len(t, u.Variants, 2, "the null branch is stripped, not emitted as a variant")
	for _, v := range u.Variants {
		assert.NotEqual(t, ir.TypeID("t/prim/any"), v.Type.Target,
			"no variant degraded to any")
	}
}

func TestAnyOf_ThreeVariantsWithNull(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpec(`    N:
      anyOf:
        - {type: string}
        - {type: integer}
        - {type: 'null'}
`)
	doc, diags := lowerSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	u := typeByName(doc, "N").(*ir.Union)
	assert.Len(t, u.Variants, 2, "null branch stripped from variants")
	assert.False(t, u.Exclusive, "anyOf is not exclusive")
}

func TestUnion_VariantHints(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpec(`    U:
      oneOf:
        - {$ref: '#/components/schemas/Named', description: sibling}
        - {type: string}
    Named: {type: object, properties: {a: {type: string}}}
`)
	doc, diags := lowerSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	u := typeByName(doc, "U").(*ir.Union)
	require.Len(t, u.Variants, 2)
	hints := []string{u.Variants[0].Name.Hint, u.Variants[1].Name.Hint}
	assert.Contains(t, hints, "named", "ref-with-siblings hint from target name")
	assert.Contains(t, hints, "variant_1", "inline branch positional hint")
}

// TestUnion_ADecoratedBranchIsHintedAsItsRef pins GitHub #1018. OpenAPI 3.0
// cannot write keywords beside a $ref, so a document annotates a union branch by
// wrapping it, and the wrapped branch names its variant for the $ref as 3.1's
// `{$ref: X, description: …}` does. A branch that says anything about a value
// beyond its annotations is a type of its own and keeps the positional hint.
// Each row also holds the branch's own node to the hint an outside $ref to the
// branch derives: positional, or its target's name where the branch is itself a
// $ref.
func TestUnion_ADecoratedBranchIsHintedAsItsRef(t *testing.T) {
	t.Parallel()
	const pet = "'#/components/schemas/Pet'"
	for _, tc := range []struct {
		name         string
		version      string // 3.0.3 unless the row is a 3.1 spelling
		keyword      string // oneOf unless the row is an anyOf
		branch       string // the second branch, beside a plain $ref to User
		extra        string // components the branch needs beyond User and Pet
		bare         string // a third branch, the same $ref written bare, which want is then not read for
		want         string // the second variant's hint
		wantNode     string // the second branch's own node's hint; variant_1 unless set
		wantNullable bool
		wantDangling bool // the branch holds a $ref that names nothing
	}{
		{
			name:   "a description beside the wrapped ref",
			branch: "{allOf: [{$ref: " + pet + "}], description: a pet}",
			want:   "pet",
		},
		{
			name:   "the ref wrapped twice, which flattens as it lowers",
			branch: "{allOf: [{allOf: [{$ref: " + pet + "}]}], description: a pet}",
			want:   "pet",
		},
		{
			name:     "3.1 writes the description beside the ref itself",
			version:  "3.1.0",
			branch:   "{$ref: " + pet + ", description: a pet}",
			want:     "pet",
			wantNode: "pet",
		},
		{
			name:   "a type beside the wrapped ref",
			branch: "{allOf: [{$ref: " + pet + "}], type: object, description: a pet}",
			want:   "pet",
		},
		{
			name:         "nullable beside the wrapped ref",
			branch:       "{allOf: [{$ref: " + pet + "}], nullable: true}",
			want:         "pet",
			wantNullable: true,
		},
		{
			name:    "a sibling on the wrapped ref itself",
			version: "3.1.0",
			branch:  "{allOf: [{$ref: " + pet + ", description: x}], description: y}",
			want:    "pet",
		},
		{
			name:   "an empty schema before the wrapped ref",
			branch: "{allOf: [{}, {$ref: " + pet + "}]}",
			want:   "pet",
		},
		{
			name:    "under anyOf",
			keyword: "anyOf",
			branch:  "{allOf: [{$ref: " + pet + "}], description: a pet}",
			want:    "pet",
		},
		{
			name:   "a ref to a position inside another schema",
			branch: "{allOf: [{$ref: '#/components/schemas/Holder/properties/inner'}], description: d}",
			extra:  "    Holder: {type: object, properties: {inner: {type: object, properties: {x: {type: string}}}}}\n",
			bare:   "{$ref: '#/components/schemas/Holder/properties/inner'}",
		},
		{
			name:   "a ref to a schema whose name is empty",
			branch: "{allOf: [{$ref: '#/components/schemas/'}], description: d}",
			extra:  "    '': {type: object, properties: {x: {type: string}}}\n",
			want:   "variant_1",
		},
		{
			name:    "a ref with an allOf written beside it",
			version: "3.1.0",
			branch:  "{$ref: '#/components/schemas/User', allOf: [{$ref: " + pet + "}]}",
			want:    "user", wantNode: "user",
		},
		{
			name:   "a property beside the wrapped ref",
			branch: "{allOf: [{$ref: " + pet + "}], properties: {extra: {type: string}}}",
			want:   "variant_1",
		},
		{
			name:   "a constraint beside the wrapped ref",
			branch: "{allOf: [{$ref: " + pet + "}], minProperties: 1}",
			want:   "variant_1",
		},
		{
			name:   "a required name beside the wrapped ref",
			branch: "{allOf: [{$ref: " + pet + "}], required: [name]}",
			want:   "variant_1",
		},
		{
			name:   "two refs in the allOf",
			branch: "{allOf: [{$ref: '#/components/schemas/User'}, {$ref: " + pet + "}], description: both}",
			want:   "variant_1",
		},
		{
			name:         "a wrapped ref that names nothing",
			branch:       "{allOf: [{$ref: '#/components/schemas/Ghost'}], description: a ghost}",
			want:         "variant_1",
			wantDangling: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			version, keyword := "3.0.3", "oneOf"
			if tc.version != "" {
				version = tc.version
			}
			if tc.keyword != "" {
				keyword = tc.keyword
			}
			spec := `    User: {type: object, properties: {id: {type: string}}}
    Pet: {type: object, properties: {name: {type: string}}}
` + tc.extra + "    U:\n      " + keyword + ":\n        - {$ref: '#/components/schemas/User'}\n        - " + tc.branch + "\n"
			wantVariants := 2
			if tc.bare != "" {
				spec += "        - " + tc.bare + "\n"
				wantVariants = 3
			}
			doc, diags := lowerSpec(t, openapitest.ComponentSpecVer(version, spec))
			if tc.wantDangling {
				assert.True(t, openapitest.HasDiagAt(diags, diag.UnresolvedRef, ir.SeverityError),
					"the dangling ref is reported: %+v", diags)
			} else {
				openapitest.RequireNoErrorDiags(t, diags)
			}

			u, ok := typeByName(doc, "U").(*ir.Union)
			require.True(t, ok, "U should be a union")
			require.Len(t, u.Variants, wantVariants)
			assert.Equal(t, "user", u.Variants[0].Name.Hint, "a bare $ref names its variant for its target")
			if tc.bare != "" {
				assert.Equal(t, u.Variants[2].Name.Hint, u.Variants[1].Name.Hint,
					"the wrapped ref is named as the same ref written bare")
				assert.NotEqual(t, "variant_1", u.Variants[1].Name.Hint, "and as what it names, not where it sits")
			} else {
				assert.Equal(t, tc.want, u.Variants[1].Name.Hint)
			}
			assert.Equal(t, tc.wantNullable, u.Variants[1].Type.Nullable)

			nodeID := ir.TypeID("t/anon/components/schemas/U/" + keyword + "/1")
			assert.Equal(t, nodeID, u.Variants[1].Type.Target, "the variant holds the branch's own node")
			node, ok := doc.Types[nodeID]
			require.True(t, ok, "the branch lowers to a node of its own")
			wantNode := tc.wantNode
			if wantNode == "" {
				wantNode = "variant_1"
			}
			assert.Equal(t, wantNode, node.Common().Name.Hint)
		})
	}
}

// TestUnion_ADecoratedBranchIsHintedTheSameInEitherOrder pins that naming a
// variant for its wrapped $ref (GitHub #1018) leaves the branch's own node
// alone. A $ref elsewhere to the branch hoists that node from its pointer, so a
// node hinted for the variant would read one way when the union lowers first and
// another when the $ref does. Declared before the union and after it, the
// variant, the union and the node must agree.
func TestUnion_ADecoratedBranchIsHintedTheSameInEitherOrder(t *testing.T) {
	t.Parallel()
	const members = `    User: {type: object, properties: {id: {type: string}}}
    Pet: {type: object, properties: {name: {type: string}}}
`
	const host = `    U:
      oneOf:
        - {$ref: '#/components/schemas/User'}
        - {allOf: [{$ref: '#/components/schemas/Pet'}], description: a pet}
`
	const outsider = "    Outside: {$ref: '#/components/schemas/U/oneOf/1'}\n"
	first, diags := parseFull(t, openapitest.ComponentSpecVer("3.0.3", members+outsider+host))
	openapitest.RequireNoErrorDiags(t, diags)
	last, diags := parseFull(t, openapitest.ComponentSpecVer("3.0.3", members+host+outsider))
	openapitest.RequireNoErrorDiags(t, diags)

	branch := ir.TypeID("t/anon/components/schemas/U/oneOf/1")
	for _, doc := range []*ir.Document{first, last} {
		u, ok := typeByName(doc, "U").(*ir.Union)
		require.True(t, ok, "U should be a union")
		require.Len(t, u.Variants, 2)
		assert.Equal(t, "pet", u.Variants[1].Name.Hint)
		require.Equal(t, branch, u.Variants[1].Type.Target)
		node, ok := doc.Types[branch]
		require.True(t, ok, "the branch lowers to a node of its own")
		assert.Equal(t, "variant_1", node.Common().Name.Hint)
	}
	assert.Empty(t, cmp.Diff(first.Types[componentID("U")], last.Types[componentID("U")]), "the union")
	assert.Empty(t, cmp.Diff(first.Types[branch], last.Types[branch]), "the branch's node")
}

// TestOneOf_CoDeclaredCompositionDistributes is the regression from
// reference-learnings §B11: an allOf composition co-declared with a oneOf used
// to survive while the union was dropped to Unmodeled. Both must survive, and
// the shape that says so is a Union whose every variant carries the composition
// — `Base ∧ (A | B)` written as `(Base ∧ A) | (Base ∧ B)`.
func TestOneOf_CoDeclaredCompositionDistributes(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpec(`    Base: {type: object, properties: {id: {type: string}}}
    A: {type: object, properties: {a: {type: string}}}
    B: {type: object, properties: {b: {type: string}}}
    Combo:
      allOf: [{$ref: '#/components/schemas/Base'}]
      oneOf:
        - {$ref: '#/components/schemas/A'}
        - {$ref: '#/components/schemas/B'}
`)
	doc, diags := lowerSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)

	u, ok := typeByName(doc, "Combo").(*ir.Union)
	require.True(t, ok, "the union is the schema's value, not a preserved sibling")
	assert.True(t, u.Exclusive, "oneOf stays exclusive")
	require.Len(t, u.Variants, 2)
	assert.Empty(t, u.Unmodeled, "distribution loses nothing, so nothing is kept verbatim")

	for i, branch := range []string{"A", "B"} {
		v := doc.Types[u.Variants[i].Type.Target].(*ir.Model)
		require.NotNil(t, v.Base, "the enclosing allOf's sole $ref stays Base (ir-design §4.3)")
		assert.Equal(t, componentID("Base"), v.Base.Target)
		require.Len(t, v.Mixins, 1, "the branch joins as a mixin, the composition already having a base")
		assert.Equal(t, componentID(branch), v.Mixins[0].Target)
		assert.Equal(t, ir.CanonicalWords(branch), u.Variants[i].Name.Hint,
			"the variant is named after its branch, in the neutral words every name channel carries")
	}
	assert.Equal(t, 1, openapitest.CountDiagsAt(diags, diag.CompositionLowering, ir.SeverityInfo),
		"the reshaping is reported once; got %+v", diags)
}

// TestAnyOf_CoDeclaredPropertiesDistributeAsBase covers the other classification
// arm: with no enclosing composition to take Base, the branch takes it, and an
// anyOf stays non-exclusive through distribution.
func TestAnyOf_CoDeclaredPropertiesDistributeAsBase(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpec(`    A: {type: object, properties: {a: {type: string}}}
    B: {type: object, properties: {b: {type: string}}}
    Combo:
      type: object
      required: [kind]
      additionalProperties: false
      properties: {kind: {type: string}}
      anyOf:
        - {$ref: '#/components/schemas/A'}
        - {$ref: '#/components/schemas/B'}
`)
	doc, diags := lowerSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)

	u, ok := typeByName(doc, "Combo").(*ir.Union)
	require.True(t, ok)
	assert.False(t, u.Exclusive, "anyOf stays non-exclusive")
	require.Len(t, u.Variants, 2)

	v := doc.Types[u.Variants[0].Type.Target].(*ir.Model)
	require.NotNil(t, v.Base, "with nothing composed yet, the branch becomes Base")
	assert.Equal(t, componentID("A"), v.Base.Target)
	assert.Empty(t, v.Mixins)
	require.Len(t, v.Properties, 1, "the body's own properties ride on every variant")
	assert.Equal(t, "kind", v.Properties[0].WireName)
	assert.True(t, v.Properties[0].Required, "so does its required list")
	assert.Equal(t, ir.AdditionalClosed, v.Additional, "and its openness")
}

// TestOneOf_CoDeclaredVariantNotStolenByRefToBranch is the regression for the
// pointer collision: the composed variants used to be interned at the branch
// pointers, which a $ref elsewhere in the document denotes too. Whichever
// lowered first took the pointer, so an unrelated reference could leave one
// variant a bare alias of its branch while its siblings carried the body — the
// disagreement ir-design §4.3 forbids, decided by declaration order.
func TestOneOf_CoDeclaredVariantNotStolenByRefToBranch(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name          string
		branch        int
		outsiderFirst bool
	}{
		{"outsider declared first", 0, true},
		{"outsider declared last", 0, false},
		{"ref to the second branch", 1, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, diags := lowerSpec(t, comboRefStealSpec(tc.branch, tc.outsiderFirst))
			openapitest.RequireNoErrorDiags(t, diags)

			u, ok := typeByName(doc, "Combo").(*ir.Union)
			require.True(t, ok)
			require.Len(t, u.Variants, 2)
			for i, branch := range []string{"A", "B"} {
				v, ok := doc.Types[u.Variants[i].Type.Target].(*ir.Model)
				require.True(t, ok, "variant %d keeps the composed body", i)
				require.NotNil(t, v.Base)
				assert.Equal(t, componentID("Base"), v.Base.Target)
				require.Len(t, v.Mixins, 1)
				assert.Equal(t, componentID(branch), v.Mixins[0].Target)
			}

			// The outside reference gets the branch schema, which is what its
			// pointer names — never the conjunction the variant stands for.
			outsider, ok := typeByName(doc, "Outsider").(*ir.Model)
			require.True(t, ok)
			require.Len(t, outsider.Properties, 1)
			branchNode, ok := doc.Types[outsider.Properties[0].Type.Target].(*ir.Scalar)
			require.True(t, ok, "the branch pointer hoists an alias of the branch's target")
			require.NotNil(t, branchNode.Base)
			assert.Equal(t, componentID([]string{"A", "B"}[tc.branch]), branchNode.Base.Target)
		})
	}
}

// TestOneOf_CoDeclaredDistributionIsOrderIndependent states the property over
// the whole compiled document: the same components in two declaration orders
// compile to the same IR. Each case permutes a site the distribution shares
// with something outside it: an outside $ref aimed at either union branch, and
// the discriminated base the variants take their tag from.
//
// These cases permute only components that emit no diagnostics, since
// diagnostics are appended in traversal order and so differ in order even when
// they match. The registry is also compared on its own, with nothing excluded.
func TestOneOf_CoDeclaredDistributionIsOrderIndependent(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		// spec writes the document with the permuted component declared first.
		spec func(first bool) string
	}{
		{"outside ref at the first branch", func(f bool) string { return comboRefStealSpec(0, f) }},
		{"outside ref at the second branch", func(f bool) string { return comboRefStealSpec(1, f) }},
		{"discriminated base", comboDiscriminatedSpec},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			first, diags := parseFull(t, tc.spec(true))
			openapitest.RequireNoErrorDiags(t, diags)
			last, diags := parseFull(t, tc.spec(false))
			openapitest.RequireNoErrorDiags(t, diags)

			assert.Empty(t, cmp.Diff(first, last, orderInvariantIR()...),
				"declaring the permuted component before or after the union must not change the IR")
			assert.Empty(t, cmp.Diff(first.Types, last.Types), "nor any name hint in the registry")
		})
	}
}

// TestNullCollapse_BranchHintIsOrderIndependent is
// TestComposition_BranchAliasIsOrderIndependent's rule at the site the collapse
// takes instead. The
// collapse lowers the surviving branch at the branch pointer but used to hand it
// the *enclosing* schema's hint, while an outside $ref naming that same pointer
// derived variant_<index> from the pointer — so whichever lowering arrived
// first decided the name and the two declaration orders produced two different
// documents, with no diagnostic on either side (GitHub #281).
//
// The single-order assertion is written against the order that was wrong: with S
// declared first the collapse reaches the pointer first, which is where the
// enclosing schema's hint used to land.
func TestNullCollapse_BranchHintIsOrderIndependent(t *testing.T) {
	t.Parallel()
	first, diags := parseFull(t, nullCollapseSpec(true))
	openapitest.RequireNoErrorDiags(t, diags)
	last, diags := parseFull(t, nullCollapseSpec(false))
	openapitest.RequireNoErrorDiags(t, diags)

	branch := ir.TypeID("t/anon/components/schemas/S/oneOf/0")
	require.Contains(t, first.Types, branch, "the surviving branch owns a node of its own")
	assert.Equal(t, "variant_0", first.Types[branch].Common().Name.Hint,
		"the branch takes the hint its composition gives it, not the enclosing schema's")
	assert.Empty(t, cmp.Diff(first.Types, last.Types),
		"declaring the collapse before or after the outside $ref must not change the registry")
	assert.Empty(t, cmp.Diff(first, last, orderInvariantIR()...), "nor the rest of the document")
}

// TestOneOf_CoDeclaredVariantCarriesDiscriminatorValue pins the tag the variants
// inherit. The enclosing schema is an allOf subtype of a discriminated base, so
// every variant is written on the wire with that subtype's tag — and the tag
// comes from the base's mapping entry for the *enclosing* schema, since that is
// the node the mapping names.
func TestOneOf_CoDeclaredVariantCarriesDiscriminatorValue(t *testing.T) {
	t.Parallel()
	doc, diags := lowerSpec(t, comboDiscriminatedSpec(true))
	openapitest.RequireNoErrorDiags(t, diags)

	u, ok := typeByName(doc, "Combo").(*ir.Union)
	require.True(t, ok)
	require.Len(t, u.Variants, 2)
	for i := range u.Variants {
		v, ok := doc.Types[u.Variants[i].Type.Target].(*ir.Model)
		require.True(t, ok)
		assert.Equal(t, "combo", v.DiscriminatorValue,
			"variant %d carries the mapping key naming the enclosing schema, not its own hint", i)
	}
}

// TestOneOf_InlineCoDeclaredDistributionHasNoImplicitDiscriminatorValue extends
// GitHub #517 to buildComposedVariant, the union-branch caller of
// subtypeDiscriminatorValue: the enclosing schema of this co-declared oneOf is
// hoisted inline (a property, not a component), so it has no schema name and
// its distributed variants get no implicit tag either — the same rule the allOf
// case pins, at the other of the two call sites the fix touches. Before the fix
// both variants took the property key "Combo" as their tag, a value the document
// never assigned and one a component named Combo would also answer to.
func TestOneOf_InlineCoDeclaredDistributionHasNoImplicitDiscriminatorValue(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpec(`    Pet:
      type: object
      required: [kind]
      properties:
        kind: {type: string}
      discriminator:
        propertyName: kind
    A: {type: object, properties: {a: {type: string}}}
    B: {type: object, properties: {b: {type: string}}}
    Kennel:
      type: object
      properties:
        Combo:
          allOf:
            - $ref: '#/components/schemas/Pet'
          oneOf:
            - $ref: '#/components/schemas/A'
            - $ref: '#/components/schemas/B'
`)
	doc, diags := lowerSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)

	u, ok := doc.Types[ir.TypeID("t/anon/components/schemas/Kennel/properties/Combo")].(*ir.Union)
	require.True(t, ok, "the co-declared oneOf hoists a union at the inline property")
	require.Len(t, u.Variants, 2)
	for i := range u.Variants {
		v, ok := doc.Types[u.Variants[i].Type.Target].(*ir.Model)
		require.True(t, ok)
		assert.Empty(t, v.DiscriminatorValue,
			"variant %d has no mapping entry and its enclosing schema has no name to fall back to", i)
	}
}

// TestOneOf_CoDeclaredAdditionalPropsHintNamesTheBody covers the naming of the
// one node every variant shares. additionalProperties is declared once, on the
// enclosing schema, and lowers at its pointer — so the first variant to reach it
// must not stamp its own branch name on the shared node.
func TestOneOf_CoDeclaredAdditionalPropsHintNamesTheBody(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpec(`    A: {type: object, properties: {a: {type: string}}}
    B: {type: object, properties: {b: {type: string}}}
    Combo:
      type: object
      properties: {kind: {type: string}}
      additionalProperties: {type: object, properties: {v: {type: string}}}
      oneOf: [{$ref: '#/components/schemas/A'}, {$ref: '#/components/schemas/B'}]
`)
	doc, diags := lowerSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)

	u := typeByName(doc, "Combo").(*ir.Union)
	v, ok := doc.Types[u.Variants[0].Type.Target].(*ir.Model)
	require.True(t, ok)
	require.NotNil(t, v.AdditionalProps)
	assert.Equal(t, "combo_value", doc.Types[v.AdditionalProps.Value.Target].Common().Name.Hint)
}

// TestOneOf_CoDeclaredNonModelBranchIsCarriedAsWritten pins what §4.3 says
// Base/Mixins may name. JSON Schema lets a composition conjoin any schema, so a
// branch can reference a scalar, an enum or another union; the referent is
// carried as written, because narrowing it would mean dropping a branch the
// source declared. fillAllOf classifies an allOf entry the same way.
func TestOneOf_CoDeclaredNonModelBranchIsCarriedAsWritten(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpec(`    Scalar: {type: string, minLength: 2}
    Choice: {enum: [a, b]}
    Combo:
      type: object
      properties: {kind: {type: string}}
      oneOf: [{$ref: '#/components/schemas/Scalar'}, {$ref: '#/components/schemas/Choice'}]
`)
	doc, diags := lowerSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)

	u, ok := typeByName(doc, "Combo").(*ir.Union)
	require.True(t, ok)
	require.Len(t, u.Variants, 2)
	for i, want := range []string{"Scalar", "Choice"} {
		v, ok := doc.Types[u.Variants[i].Type.Target].(*ir.Model)
		require.True(t, ok)
		require.NotNil(t, v.Base, "variant %d conjoins its branch", i)
		assert.Equal(t, componentID(want), v.Base.Target)
	}
	assert.IsType(t, &ir.Scalar{}, doc.Types[componentID("Scalar")])
	assert.IsType(t, &ir.Enum{}, doc.Types[componentID("Choice")])
}

// TestOneOf_CoDeclaredUnresolvableBranchIsNotDistributed covers the branches
// that are $ref-shaped but name nothing this compilation can resolve. Each
// would otherwise half-distribute — the variant carrying the body while its
// branch vanished — so all of them keep the verbatim lowering, which loses
// nothing, and each still reports the broken reference.
//
// It drives the lowering alone (lowerSpecOnly), so the report checked is the
// lowering's own, at the branch pointer, with the reason the bindings give. A
// full compile
// keeps the load phase's report of the same $ref instead; see
// TestCompile_AResolutionFailureIsReportedOnceAtItsRef.
func TestOneOf_CoDeclaredUnresolvableBranchIsNotDistributed(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpec(`    A: {type: object, properties: {a: {type: string}}}
    Undeclared:
      type: object
      properties: {kind: {type: string}}
      oneOf: [{$ref: '#/components/schemas/Ghost'}, {$ref: '#/components/schemas/A'}]
    CrossDocument:
      type: object
      properties: {kind: {type: string}}
      oneOf: [{$ref: 'other.yaml#/components/schemas/A'}]
    EmptyRef:
      type: object
      properties: {kind: {type: string}}
      oneOf: [{$ref: ''}]
    NoSuchPointer:
      type: object
      properties: {kind: {type: string}}
      oneOf: [{$ref: '#/components/schemas/A/properties/missing'}]
`)
	doc, diags := lowerSpecOnly(t, spec)

	for name, why := range map[string]string{
		"Undeclared":    `"#/components/schemas/Ghost" is unresolved: it names nothing in the document it points into`,
		"CrossDocument": `"other.yaml#/components/schemas/A" is unresolved: it names another document, and external references are not read`,
		"EmptyRef":      `"" is unresolved: it names a position that holds no schema`,
		"NoSuchPointer": `"#/components/schemas/A/properties/missing" is unresolved: it names nothing in the document it points into`,
	} {
		m, ok := typeByName(doc, name).(*ir.Model)
		require.True(t, ok, "%s keeps its structural body rather than becoming a union", name)
		entry, ok := m.Unmodeled["openapi:oneOf"]
		require.True(t, ok, "%s keeps every branch verbatim", name)
		assert.Equal(t, ir.ReasonDegradedLowering, entry.Reason, "%s", name)
		assert.Contains(t,
			openapitest.DiagMessageAt(t, diags, diag.DegradedConstruct, ir.SeverityInfo, "/components/schemas/"+name),
			"names no referent this compilation resolves", name)
		// One error per offending branch, at the branch itself.
		assert.Equal(t, "union branch $ref "+why+"; the branch is kept verbatim",
			openapitest.DiagMessageAt(t, diags, diag.UnresolvedRef, ir.SeverityError, "/components/schemas/"+name+"/oneOf/0"),
			name)
	}
	for _, d := range diags {
		assert.NotEqual(t, jsontext.Pointer("/components/schemas/Undeclared/oneOf/1"), d.Provenance.Pointer,
			"the branch beside the broken one resolves, so nothing is reported against it")
	}
}

// TestOneOf_CoDeclaredNotDistributedReasons pins the five shapes distribution
// declines, each kept verbatim under ReasonDegradedLowering with its own reason
// in the diagnostic (docs/compilers/openapi.md#union-beside-structure).
// Half-distributing any of them would leave a Union whose variants disagree
// about whether they carry the body. The messages are asserted, not just the
// code: the reasons are the only thing telling the otherwise identical
// diagnostics apart.
func TestOneOf_CoDeclaredNotDistributedReasons(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpec(`    A: {type: object, properties: {a: {type: string}}}
    NotAModel:
      const: fixed
      oneOf: [{$ref: '#/components/schemas/A'}]
    InlineBranch:
      type: object
      properties: {kind: {type: string}}
      oneOf: [{$ref: '#/components/schemas/A'}, {type: string}]
    NullBranch:
      properties: {kind: {type: string}}
      oneOf: [{$ref: '#/components/schemas/A'}, {type: 'null'}]
    BothCombinators:
      type: object
      properties: {kind: {type: string}}
      oneOf: [{$ref: '#/components/schemas/A'}]
      anyOf: [{$ref: '#/components/schemas/A'}]
    Discriminated:
      type: object
      properties: {kind: {type: string}}
      discriminator: {propertyName: kind}
      oneOf: [{$ref: '#/components/schemas/A'}]
`)
	doc, diags := lowerSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)

	for name, reason := range map[string]string{
		"NotAModel":       "the body is not a model, so it carries no composition to distribute into",
		"InlineBranch":    "a branch is written inline, so it names no referent to conjoin the body with",
		"NullBranch":      "a branch is written inline, so it names no referent to conjoin the body with",
		"BothCombinators": "oneOf and anyOf are both declared, so distributing either would drop the other",
		"Discriminated":   "a declared discriminator binds the branches by name, which distributing them would break",
	} {
		entry, ok := typeByName(doc, name).Common().Unmodeled["openapi:oneOf"]
		require.True(t, ok, "%s keeps its union verbatim", name)
		assert.Equal(t, ir.ReasonDegradedLowering, entry.Reason, "%s", name)
		assert.Contains(t,
			openapitest.DiagMessageAt(t, diags, diag.DegradedConstruct, ir.SeverityInfo, "/components/schemas/"+name),
			reason, name)
	}
	both := typeByName(doc, "BothCombinators").(*ir.Model)
	_, ok := both.Unmodeled["openapi:anyOf"]
	assert.True(t, ok, "the second combinator is kept too, which is why neither is distributed")
	nullBranch := typeByName(doc, "NullBranch").(*ir.Model)
	assert.Contains(t, string(nullBranch.Unmodeled["openapi:oneOf"].Value), `"null"`,
		"a null branch is written inline, so it blocks distribution rather than lifting to Nullable")
	// The sixth is settling's, once the registry is whole: Discriminated's
	// discriminator routes to no subtype of its model, so it stays beside the
	// union it was written for.
	assert.Contains(t,
		openapitest.DiagMessageAt(t, diags, diag.DegradedConstruct, ir.SeverityInfo,
			"/components/schemas/Discriminated/discriminator"),
		"not a subtype of the model holding it")
	assert.Equal(t, 6, openapitest.CountDiagsAt(diags, diag.DegradedConstruct, ir.SeverityInfo),
		"each declined shape is reported once, and the kept discriminator once; got %+v", diags)
}

// TestUnionCombinators_CoDeclaredKeepsTheBoundsWrittenBesideIt pins that
// keeping a union verbatim does not cost the position the value constraints
// written beside it. The alias holding the union owns the pointer, so
// hoistDeclarationHome does not hoist the alias that would carry the bounds,
// and this alias must carry them itself (GitHub #343). Both reasons that keep a
// union hoist the same alias, and anyOf takes the same path as oneOf. The last
// case pins that a co-declared bound pair adds nothing beside the union.
func TestUnionCombinators_CoDeclaredKeepsTheBoundsWrittenBesideIt(t *testing.T) {
	t.Parallel()
	three := int64(3)
	ten, five, zero := ir.BigVal("10"), ir.BigVal("5"), ir.BigVal("0")
	cases := []struct {
		name, schemas, unionKey string
		reason                  ir.UnmodeledReason
		want                    ir.Constraints
		wantKept                []string
	}{
		{
			name:     "a validation-only union",
			schemas:  "    A: {type: string, minLength: 3, oneOf: [{minLength: 1}, {minLength: 2}]}\n",
			unionKey: "openapi:oneOf",
			reason:   ir.ReasonValidationOnly,
			want:     ir.Constraints{MinLength: &three},
			wantKept: []string{"openapi:oneOf"},
		},
		{
			name:     "a union kept as a degraded lowering",
			schemas:  "    A: {type: number, minimum: 10, multipleOf: 5, oneOf: [{type: string}, {type: integer}]}\n",
			unionKey: "openapi:oneOf",
			reason:   ir.ReasonDegradedLowering,
			want:     ir.Constraints{Min: &ten, MultipleOf: &five},
			wantKept: []string{"openapi:oneOf"},
		},
		{
			name:     "an anyOf kept in place of a oneOf",
			schemas:  "    A: {type: string, minLength: 3, anyOf: [{minLength: 1}, {minLength: 2}]}\n",
			unionKey: "openapi:anyOf",
			reason:   ir.ReasonValidationOnly,
			want:     ir.Constraints{MinLength: &three},
			wantKept: []string{"openapi:anyOf"},
		},
		{
			// Both bound keywords reach a field, so the union is the only
			// entry on the node: a co-declared pair adds nothing beside it.
			name:     "co-declared bounds beside a union",
			schemas:  "    A: {type: number, minimum: 10, exclusiveMinimum: 0, oneOf: [{minLength: 1}, {minLength: 2}]}\n",
			unionKey: "openapi:oneOf",
			reason:   ir.ReasonValidationOnly,
			want:     ir.Constraints{Min: &ten, ExclusiveMin: &zero},
			wantKept: []string{"openapi:oneOf"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, diags := lowerSpec(t, openapitest.ComponentSpec(tc.schemas))
			openapitest.RequireNoErrorDiags(t, diags)

			sc, ok := typeByName(doc, "A").(*ir.Scalar)
			require.True(t, ok, "the preserved union hoists an alias over the shared primitive")
			entry, ok := sc.Unmodeled[tc.unionKey]
			require.True(t, ok, "and keeps the union on it")
			assert.Equal(t, tc.reason, entry.Reason)
			require.NotNil(t, sc.Constraints, "while keeping the bounds written beside it")
			assert.Empty(t, cmp.Diff(tc.want, *sc.Constraints))
			assert.Equal(t, tc.wantKept, unmodeledKeys(sc.Unmodeled),
				"the union is the only entry kept on the node")
		})
	}
}

// TestUnionCombinators_PassedOverBranchSetIsKept covers the preference nothing
// used to record (GitHub #35). unionBranches takes oneOf whenever it is written
// and falls back to anyOf only when it is not, so a schema declaring both lost
// the anyOf outright — no variant, no Unmodeled entry, no diagnostic. The two
// conjoin, and one ir.Union carries one branch set, so the elected set stays the
// union and the other is kept beside it.
func TestUnionCombinators_PassedOverBranchSetIsKept(t *testing.T) {
	t.Parallel()
	doc, diags := lowerSpec(t, openapitest.ComponentSpec(`    S:
      oneOf: [{type: string}, {type: integer}]
      anyOf: [{type: number}, {type: boolean}]
`))
	openapitest.RequireNoErrorDiags(t, diags)

	u, ok := typeByName(doc, "S").(*ir.Union)
	require.True(t, ok, "the elected branch set still becomes the union")
	assert.Len(t, u.Variants, 2, "one variant per oneOf branch")
	entry, ok := u.Unmodeled["openapi:anyOf"]
	require.True(t, ok, "the set unionBranches passed over is kept; got %v", u.Unmodeled)
	assert.Equal(t, ir.ReasonDegradedLowering, entry.Reason)
	assert.JSONEq(t, `[{"type":"number"},{"type":"boolean"}]`, string(entry.Value))
	assert.Contains(t,
		openapitest.DiagMessageAt(t, diags, diag.DegradedConstruct, ir.SeverityInfo, "/components/schemas/S"),
		"lowered as its oneOf, with anyOf kept verbatim under Unmodeled")
}

// TestUnionCombinators_NullBranchDoesNotCollapsePastTheAnyOf covers the second
// site of the same preference. A {X, null} oneOf normally collapses to nullable
// X, which resolves the position straight to X's own node — the shared string
// primitive here — leaving the co-declared anyOf nowhere of its own to sit; it
// was dropped there too. A schema writing both combinators is not nullable X in
// the first place, so it stays a Union and keeps the loser on it.
func TestUnionCombinators_NullBranchDoesNotCollapsePastTheAnyOf(t *testing.T) {
	t.Parallel()
	doc, diags := lowerSpec(t, openapitest.ComponentSpec(`    S:
      oneOf: [{type: string}, {type: "null"}]
      anyOf: [{type: number}, {type: boolean}]
`))
	openapitest.RequireNoErrorDiags(t, diags)

	u, ok := typeByName(doc, "S").(*ir.Union)
	require.True(t, ok, "the collapse is declined; got %v", typeByName(doc, "S"))
	require.Len(t, u.Variants, 1, "the null branch still lifts off the variant list")
	assert.Equal(t, ir.TypeID("t/prim/string"), u.Variants[0].Type.Target)
	assert.Contains(t, u.Unmodeled, "openapi:anyOf",
		"and the node the position now owns carries the set passed over")
}

// TestUnionCombinators_UnpreservableIsNotAnnounced pins the pairing GitHub #144
// exists for, at this site: a position may only announce what it actually kept.
// The anyOf here fails to convert, so the union lowers, the failure is reported
// at the keyword, and nothing claims the branch set survived.
func TestUnionCombinators_UnpreservableIsNotAnnounced(t *testing.T) {
	t.Parallel()
	doc, diags := lowerSpec(t, openapitest.ComponentSpec(`    S:
      oneOf: [{type: string}, {type: integer}]
      anyOf: [{type: number, x-t: `+unpreservableValue+`}]
`))

	u, ok := typeByName(doc, "S").(*ir.Union)
	require.True(t, ok)
	assert.NotContains(t, u.Unmodeled, "openapi:anyOf", "the conversion failed, so nothing was kept")
	assert.True(t, openapitest.HasDiag(diags, diag.UnpreservableConstruct), "the failure itself is reported")
	assert.Empty(t, preservationClaims(diags),
		"nothing was written under Unmodeled, so nothing may announce that it was")
}

// TestUnionCombinators_KeepingIsOrderIndependent states the property over the
// whole document: declining the collapse gives this position a Union of its own
// and makes its branches ordinary union branches, so an outside $ref naming one
// of them must reach the same IR whichever is declared first.
//
// The branch writes a description deliberately. A bare `{type: string}` branch
// resolves through the union to the shared primitive, so the outside $ref alone
// hoists a node at the branch pointer and no hint ever has to agree with
// another. Pinning the union's own variant to that node puts both lowerings on
// the pointer (#181).
func TestUnionCombinators_KeepingIsOrderIndependent(t *testing.T) {
	t.Parallel()
	host := `    S:
      oneOf:
        - {type: string, description: the branch}
        - {type: "null"}
      anyOf: [{type: number}, {type: boolean}]
`
	outsider := "    Outsider: {$ref: '#/components/schemas/S/oneOf/0'}\n"
	first, diags := parseFull(t, openapitest.ComponentSpec(outsider+host))
	openapitest.RequireNoErrorDiags(t, diags)
	last, diags := parseFull(t, openapitest.ComponentSpec(host+outsider))
	openapitest.RequireNoErrorDiags(t, diags)

	for _, doc := range []*ir.Document{first, last} {
		u, ok := typeByName(doc, "S").(*ir.Union)
		require.True(t, ok, "the position is the Union that keeps the passed-over set")
		require.Len(t, u.Variants, 1, "the null branch still lifts off the variant list")
		require.Equal(t, ir.TypeID("t/anon/components/schemas/S/oneOf/0"), u.Variants[0].Type.Target,
			"the union reaches the branch's own node, or the $ref is the only lowering that does")
	}
	assert.Empty(t, cmp.Diff(first, last, orderInvariantIR()...),
		"declaring the reference before or after the union must not change the IR")
	assert.Empty(t, cmp.Diff(first.Types, last.Types), "nor any name hint in the registry")
}

func TestLower_ConstraintOnlyUnionIsValidationOnly(t *testing.T) {
	t.Parallel()
	// The "exactly one of" idiom co-declares object structure with a oneOf whose
	// branches only narrow which properties are required. That is validation
	// logic, not shape — dependentRequired's sibling — so the structural body
	// survives and the union is preserved under ReasonValidationOnly, the reason
	// a validation emitter selects on (ir-design §4.7).
	spec := openapitest.ComponentSpec(`    Thing:
      type: object
      additionalProperties: false
      required: [common]
      properties:
        common: {type: string}
      oneOf:
        - {required: [a]}
        - {required: [b]}
`)
	doc, diags := lowerSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)

	m, ok := doc.Types[componentID("Thing")].(*ir.Model)
	require.True(t, ok, "structural body lowers to a Model, not a bare Union")
	require.Len(t, m.Properties, 1)
	assert.Equal(t, "common", m.Properties[0].Name.Source)
	assert.True(t, m.Properties[0].Required)
	assert.Equal(t, ir.AdditionalClosed, m.Additional)
	raw, ok := m.Unmodeled["openapi:oneOf"]
	require.True(t, ok, "the union is kept verbatim under Unmodeled")
	assert.Contains(t, string(raw.Value), "required")
	assert.Equal(t, ir.ReasonValidationOnly, raw.Reason,
		"constraint-only branches narrow the body without reshaping it (ir-design §4.7)")
	assert.Equal(t, jsontext.Pointer("/components/schemas/Thing/oneOf"), raw.Provenance.Pointer)
	assert.Equal(t, 1, openapitest.CountDiagsAt(diags, diag.ValidationOnlyKeyword, ir.SeverityInfo),
		"the union is reported with §4.7's keyword family; got %+v", diags)
}

func TestLower_BooleanUnionBranchDeclaresNoShape(t *testing.T) {
	t.Parallel()
	// A JSON Schema boolean branch is a whole schema: `true` accepts anything,
	// `false` accepts nothing. Neither states a data shape, so a union built
	// only from them narrows the co-declared body without reshaping it, exactly
	// as a constraint-only branch does, and takes the same validation-only
	// lowering. The branch carries no schema object at all, which is what
	// separates it from a branch that declares nothing structural.
	spec := openapitest.ComponentSpec(`    Flag:
      type: object
      required: [common]
      properties:
        common: {type: string}
      oneOf:
        - true
        - false
`)
	doc, diags := lowerSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)

	m, ok := doc.Types[componentID("Flag")].(*ir.Model)
	require.True(t, ok, "the structural body survives as a Model")
	require.Len(t, m.Properties, 1)
	assert.Equal(t, "common", m.Properties[0].Name.Source)

	raw, ok := m.Unmodeled["openapi:oneOf"]
	require.True(t, ok, "the union is kept verbatim under Unmodeled")
	assert.Equal(t, ir.ReasonValidationOnly, raw.Reason,
		"boolean branches declare no shape, so the union is validation-only (ir-design §4.7)")
	assert.Equal(t, jsontext.Pointer("/components/schemas/Flag/oneOf"), raw.Provenance.Pointer)
}

func TestLower_AllOfWithOneOfKeepsBoth(t *testing.T) {
	t.Parallel()
	// allOf co-declared with oneOf must not drop the allOf composition.
	spec := openapitest.ComponentSpec(`    Base:
      type: object
      properties:
        id: {type: string}
    Combo:
      allOf:
        - {$ref: "#/components/schemas/Base"}
      oneOf:
        - {type: string}
        - {type: integer}
`)
	doc, diags := lowerSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	m, ok := doc.Types[componentID("Combo")].(*ir.Model)
	require.True(t, ok, "allOf composition survives (Model), oneOf preserved raw")
	require.NotNil(t, m.Base, "the allOf $ref becomes Base")
	assert.Equal(t, componentID("Base"), m.Base.Target)
	_, ok = m.Unmodeled["openapi:oneOf"]
	assert.True(t, ok, "the oneOf is kept verbatim under Unmodeled")
}

func TestLower_MultiTypeUnion(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpec(`    MT:
      type: [object, array, string]
      properties: {x: {type: string}}
      items: {type: integer}
`)
	doc, diags := lowerSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	u, ok := typeByName(doc, "MT").(*ir.Union)
	require.True(t, ok)
	require.Len(t, u.Variants, 3)
	assert.True(t, u.Exclusive)
}

func TestLower_UnionWithStructuralSiblingVariants(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpec(`    A:
      type: object
      properties: {x: {type: string}}
      required: [x]
      additionalProperties: {type: integer}
      oneOf: [{type: string}, {type: integer}]
    B:
      patternProperties: {'^x-': {type: string}}
      anyOf: [{type: string}, {type: integer}]
    C:
      const: fixed
      oneOf: [{type: string}, {type: integer}]
    D:
      type: string
      oneOf: [{$ref: '#/components/schemas/A'}, {type: integer}]
`)
	doc, diags := lowerSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	for _, name := range []string{"A", "B", "C", "D"} {
		td := typeByName(doc, name)
		require.NotNil(t, td, "type %s present", name)
		c := td.Common()
		_, hasOneOf := c.Unmodeled["openapi:oneOf"]
		_, hasAnyOf := c.Unmodeled["openapi:anyOf"]
		assert.True(t, hasOneOf || hasAnyOf, "union preserved for %s", name)
	}
}

func TestSchema_UnionSiblingsAdditionalAndRequired(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpec(`    A:
      additionalProperties: {type: string}
      oneOf: [{type: string}, {type: integer}]
    B:
      required: [x]
      oneOf: [{type: string}, {type: integer}]
`)
	doc, diags := lowerSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	for _, name := range []string{"A", "B"} {
		_, ok := typeByName(doc, name).Common().Unmodeled["openapi:oneOf"]
		assert.True(t, ok, "%s preserves its union", name)
	}
}

func TestSchema_OneOfWithBoolBranch(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpec(`    U:
      anyOf:
        - {type: string}
        - true
`)
	doc, diags := lowerSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	u, ok := typeByName(doc, "U").(*ir.Union)
	require.True(t, ok)
	assert.Len(t, u.Variants, 2, "the boolean branch is a variant, not a null strip")
}

// TestUnionSiblings_UnpreservableIsReportedNotClaimed pins the announcement
// pairing at the union-sibling site. A oneOf the lowering keeps beside a
// structural body is kept verbatim when it converts; when it does not, the
// failure is reported at the keyword and nothing claims it was kept. The
// structural sibling here is unconvertible too, so the case has no legitimately
// preserved construct that a claim could be about.
func TestUnionSiblings_UnpreservableIsReportedNotClaimed(t *testing.T) {
	t.Parallel()
	_, diags := lowerSpec(t, openapitest.ComponentSpec(`    S:
      items: {type: string, x-t: `+unpreservableValue+`}
      oneOf: [{type: string, x-t: `+unpreservableValue+`}, {type: integer}]
`))

	var at []string
	for _, d := range diags {
		if d.Code == diag.UnpreservableConstruct {
			at = append(at, string(d.Provenance.Pointer))
		}
	}
	assert.Contains(t, at, "/components/schemas/S/oneOf",
		"the union siblings are reported where they are written: %+v", diags)
	assert.Empty(t, preservationClaims(diags),
		"nothing converted, so nothing may say it was kept")
}

// TestCollapsedUnion_KeepsItsDiscriminatorAtEveryPosition pins that a
// discriminator beside a oneOf/anyOf that collapses to a nullable reference is
// kept verbatim, whole, by whatever owns the position, with an info diagnostic,
// instead of vanishing (GitHub #595). The reference itself is unchanged. The
// component, the property, an array's items, additionalProperties, a parameter
// and a response body each reach the collapse by their own path.
func TestCollapsedUnion_KeepsItsDiscriminatorAtEveryPosition(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, collapseSpec)
	openapitest.RequireNoErrorDiags(t, diags)
	assert.Empty(t, pass.Validate(doc))
	cat := componentID("Cat")

	w, ok := typeByName(doc, "W").(*ir.Scalar)
	require.True(t, ok, "the component is an alias over the nullable reference")
	assert.Equal(t, ir.TypeRef{Target: cat, Nullable: true}, *w.Base, "the reference is unchanged")
	entry, ok := w.Unmodeled["openapi:discriminator"]
	require.True(t, ok, "the component keeps its discriminator")
	assert.Equal(t, ir.ReasonDegradedLowering, entry.Reason)
	assert.Contains(t, string(entry.Value), "defaultMapping", "the whole object is kept, not just the property name")
	assert.Contains(t, string(entry.Value), `"mapping"`)

	holder, ok := typeByName(doc, "Holder").(*ir.Model)
	require.True(t, ok)
	require.Len(t, holder.Properties, 1)
	assert.Equal(t, ir.TypeRef{Target: cat, Nullable: true}, holder.Properties[0].Type, "the property's reference is unchanged")
	assert.Contains(t, holder.Properties[0].Unmodeled, "openapi:discriminator", "the property carrier keeps it")

	for _, id := range []ir.TypeID{"t/anon/components/schemas/L/items", "t/anon/components/schemas/M/additionalProperties"} {
		node, ok := doc.Types[id]
		require.True(t, ok, id)
		assert.Contains(t, node.Common().Unmodeled, "openapi:discriminator", "%s keeps it on the node it hoists", id)
	}

	for _, pointer := range []string{
		"/components/schemas/W", "/components/schemas/Holder/properties/w",
		"/components/schemas/L/items", "/components/schemas/M/additionalProperties",
		"/paths/~1p/get/parameters/0/schema", "/paths/~1p/get/responses/200/content/application~1json/schema",
	} {
		assert.Contains(t,
			openapitest.DiagMessageAt(t, diags, diag.DegradedConstruct, ir.SeverityInfo, pointer),
			"lowered to a nullable reference to its one non-null branch, which has no home for discriminator", pointer)
	}
	assert.Equal(t, 6, strings.Count(marshalForTest(t, doc), `"openapi:discriminator"`),
		"one entry per position, including the parameter and the response, none duplicated")
}

// TestCollapsedUnion_KeepsEveryCensusKeywordItCannotHold pins that the collapse
// takes the whole census a $ref site takes, not a discriminator rule: a format
// or a collection bound is no more at home on a nullable reference. A null
// type, which the reference's Nullable bit carries, is, so the component's
// alias keeps nothing for it.
func TestCollapsedUnion_KeepsEveryCensusKeywordItCannotHold(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpec(`    Cat: {type: object, properties: {kind: {type: string}}}
    Bounded: {` + nullableCat + `, format: uuid, maxItems: 2}
    NullTyped: {` + nullableCat + `, type: 'null'}
`)
	doc, diags := parseFull(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)

	bounded, ok := typeByName(doc, "Bounded").(*ir.Scalar)
	require.True(t, ok)
	assert.Equal(t, []string{"openapi:format", "openapi:maxItems"}, slices.Sorted(maps.Keys(bounded.Unmodeled)))
	assert.Contains(t, openapitest.DiagMessageAt(t, diags, diag.DegradedConstruct, ir.SeverityInfo,
		"/components/schemas/Bounded"), "no home for format, maxItems")

	assert.Empty(t, typeByName(doc, "NullTyped").Common().Unmodeled)
	assert.False(t, openapitest.HasDiagCodeAt(diags, diag.DegradedConstruct, "/components/schemas/NullTyped"))
}

// TestCollapsedUnion_WithoutACensusKeywordHoistsNothing pins that the same
// positions without one stay as they were: no entry, no diagnostic, and no
// node hoisted for a position that has none.
func TestCollapsedUnion_WithoutACensusKeywordHoistsNothing(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpec(`    Cat: {type: object, properties: {kind: {type: string}}}
    L: {type: array, items: {` + nullableCat + `}}
`)
	doc, diags := parseFull(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	assert.Zero(t, openapitest.CountDiagsAt(diags, diag.DegradedConstruct, ir.SeverityInfo), "%+v", diags)
	assert.NotContains(t, marshalForTest(t, doc), "openapi:discriminator")
	assert.NotContains(t, doc.Types, ir.TypeID("t/anon/components/schemas/L/items"), "a bare nullable reference needs no node")
}

// TestNullOnlyUnion_ReportsItsCensusAsABareNullSchemaDoes pins that a null-only
// branch set, which lowers as a bare `{type: null}` schema does, names what a
// keyword it cannot hold sits beside in the same words: the node the position
// lowered to, not the alias holding the keyword.
func TestNullOnlyUnion_ReportsItsCensusAsABareNullSchemaDoes(t *testing.T) {
	t.Parallel()
	_, diags := parseFull(t, openapitest.ComponentSpec(
		"    NullOnly: {oneOf: [{type: 'null'}], format: uuid}\n    NullTyped: {type: 'null', format: uuid}\n"))
	openapitest.RequireNoErrorDiags(t, diags)
	census := func(name string) string {
		i := slices.IndexFunc(diags, func(d ir.Diagnostic) bool {
			return string(d.Provenance.Pointer) == "/components/schemas/"+name && strings.Contains(d.Message, "which has no home for")
		})
		require.GreaterOrEqual(t, i, 0, "%s reports what it kept", name)
		return diags[i].Message
	}
	assert.Equal(t, census("NullTyped"), census("NullOnly"))
}

// variantsNamingTheirNodes asserts that each labeled variant is named as the
// node its Type.Target holds, and returns the variants' hints by label.
func variantsNamingTheirNodes(t *testing.T, doc *ir.Document, labeled map[string]ir.Variant) map[string]string {
	t.Helper()
	got := make(map[string]string, len(labeled))
	for label, v := range labeled {
		target, ok := doc.Types[v.Type.Target]
		require.True(t, ok, "%s: variant target %s is interned", label, v.Type.Target)
		assert.Equal(t, v.Name.Hint, target.Common().Name.Hint,
			"%s: the variant must be named as the node its Type.Target holds", label)
		got[label] = v.Name.Hint
	}
	return got
}

// comboRefStealSpec is the co-declared union of Base with A|B, plus one $ref
// aimed at a union branch's own pointer. The two orders of the same four
// components must lower identically: the branch pointer denotes the branch
// schema in both, and the variants are Morphic's own nodes in both.
func comboRefStealSpec(branch int, outsiderFirst bool) string {
	outsider := fmt.Sprintf(
		"    Outsider: {type: object, properties: {x: {$ref: '#/components/schemas/Combo/oneOf/%d'}}}\n", branch)
	combo := `    Base: {type: object, properties: {id: {type: string}}}
    A: {type: object, properties: {a: {type: string}}}
    B: {type: object, properties: {b: {type: string}}}
    Combo:
      allOf: [{$ref: '#/components/schemas/Base'}]
      oneOf: [{$ref: '#/components/schemas/A'}, {$ref: '#/components/schemas/B'}]
`
	if outsiderFirst {
		return openapitest.ComponentSpec(outsider + combo)
	}
	return openapitest.ComponentSpec(combo + outsider)
}

// comboDiscriminatedSpec is the co-declared union of a discriminated Base with
// A|B. Every variant is written on the wire with the enclosing schema's tag,
// which the base's mapping supplies, so the base and the schemas naming it are
// permutable against each other — components lower in source order.
func comboDiscriminatedSpec(baseFirst bool) string {
	base := `    Base:
      type: object
      properties: {kind: {type: string}}
      discriminator:
        propertyName: kind
        mapping: {combo: '#/components/schemas/Combo'}
`
	rest := `    A: {type: object, properties: {a: {type: string}}}
    B: {type: object, properties: {b: {type: string}}}
    Combo:
      allOf: [{$ref: '#/components/schemas/Base'}]
      oneOf: [{$ref: '#/components/schemas/A'}, {$ref: '#/components/schemas/B'}]
`
	if baseFirst {
		return openapitest.ComponentSpec(base + rest)
	}
	return openapitest.ComponentSpec(rest + base)
}

// nullCollapseSpec writes the ordinary single-combinator `{X, null}` collapse
// with an outside component naming its surviving branch's pointer. The branch
// declares a description on purpose: a bare `{type: string}` branch resolves to
// the shared primitive and owns no node, so nothing would compete for the
// pointer and no hint would ever have to agree with another. hostFirst permutes
// which of the two components is declared first, and components lower in source
// order, so the two spellings are the two orders the pointer can be reached in.
func nullCollapseSpec(hostFirst bool) string {
	host := "    S:\n      oneOf:\n        - {type: string, description: the branch}\n" +
		"        - {type: \"null\"}\n"
	outside := "    Outsider: {$ref: '#/components/schemas/S/oneOf/0'}\n"
	if hostFirst {
		return openapitest.ComponentSpec(host + outside)
	}
	return openapitest.ComponentSpec(outside + host)
}

// collapseSpec declares a nullable Cat at every position a schema can sit at
// with a discriminator beside it, plus a parameter and a response body.
const collapseSpec = `openapi: 3.2.0
info: {title: T, version: "1"}
paths:
  /p:
    get:
      parameters:
        - name: q
          in: query
          schema:
            ` + nullableCat + `
            discriminator: {propertyName: kind}
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema:
                ` + nullableCat + `
                discriminator: {propertyName: kind}
components:
  schemas:
    Cat: {type: object, properties: {kind: {type: string}}}
    W:
      ` + nullableCat + `
      discriminator: {propertyName: kind, defaultMapping: '#/components/schemas/Cat', mapping: {cat: '#/components/schemas/Cat'}}
    Holder:
      type: object
      properties:
        w:
          anyOf: [{$ref: '#/components/schemas/Cat'}, {type: 'null'}]
          discriminator: {propertyName: kind}
    L:
      type: array
      items:
        ` + nullableCat + `
        discriminator: {propertyName: kind}
    M:
      type: object
      additionalProperties:
        ` + nullableCat + `
        discriminator: {propertyName: kind}
`

const nullableCat = "oneOf: [{$ref: '#/components/schemas/Cat'}, {type: 'null'}]"
