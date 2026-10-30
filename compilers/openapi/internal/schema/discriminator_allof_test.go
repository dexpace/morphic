// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/compilers/openapi/internal/schema"
	"github.com/dexpace/morphic/ir"
)

func TestAllOf_DiscriminatorSubtypeValue(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpec(`    Pet:
      type: object
      discriminator:
        propertyName: petType
        mapping:
          kitty: "#/components/schemas/Cat"
      properties:
        petType: {type: string}
    Cat:
      allOf:
        - {$ref: "#/components/schemas/Pet"}
        - type: object
          properties:
            meow: {type: string}
    Dog:
      allOf:
        - {$ref: "#/components/schemas/Pet"}
        - type: object
          properties:
            bark: {type: string}
`)
	doc, diags := schema.LowerSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)

	pet, ok := doc.Types[componentID("Pet")].(*ir.Model)
	require.True(t, ok, "Pet should be a model")
	require.NotNil(t, pet.Discriminator, "the base carries the discriminator")
	assert.Empty(t, pet.DiscriminatorValue, "the base itself has no wire tag value")

	cat, ok := doc.Types[componentID("Cat")].(*ir.Model)
	require.True(t, ok, "Cat should be a model")
	assert.Equal(t, componentID("Pet"), cat.Base.Target)
	assert.Equal(t, "kitty", cat.DiscriminatorValue,
		"mapping key pointing at Cat becomes its wire tag value")

	dog, ok := doc.Types[componentID("Dog")].(*ir.Model)
	require.True(t, ok, "Dog should be a model")
	assert.Equal(t, componentID("Pet"), dog.Base.Target)
	assert.Equal(t, "Dog", dog.DiscriminatorValue,
		"a subtype absent from the mapping falls back to its schema name")
}

// TestAllOf_InlineSubtypeHasNoImplicitDiscriminatorValue pins GitHub #517.
// OpenAPI's implicit discriminator mapping names a subtype by its component
// schema name; an inline subtype — declared here as a property, once keyed like
// the unrelated component Dog and once keyed like nothing in the document —
// has no schema name of its own and so gets no implicit tag. Before the fix the
// fallback read the subtype's pointer's last token instead, so Kennel's "Dog"
// property claimed the same tag as the component Dog, and its "pet" property
// claimed its own property key as a tag the document never assigned.
func TestAllOf_InlineSubtypeHasNoImplicitDiscriminatorValue(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpec(`    Pet:
      type: object
      required: [kind]
      properties:
        kind: {type: string}
      discriminator:
        propertyName: kind
    Dog:
      allOf:
        - $ref: '#/components/schemas/Pet'
      type: object
      properties:
        bark: {type: string}
    Kennel:
      type: object
      properties:
        Dog:
          allOf:
            - $ref: '#/components/schemas/Pet'
          type: object
          properties:
            woof: {type: string}
        pet:
          allOf:
            - $ref: '#/components/schemas/Pet'
          type: object
          properties:
            meow: {type: string}
`)
	doc, diags := schema.LowerSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)

	kennelDog := ir.TypeID("t/anon/components/schemas/Kennel/properties/Dog")
	kennelPet := ir.TypeID("t/anon/components/schemas/Kennel/properties/pet")
	want := map[ir.TypeID]string{
		componentID("Dog"): "Dog",
		kennelDog:          "",
		kennelPet:          "",
	}
	got := discriminatorValues(t, doc, componentID("Dog"), kennelDog, kennelPet)
	assert.Empty(t, cmp.Diff(want, got),
		"the component keeps its implicit tag; neither inline subtype gets one")
}

// TestAllOf_InlineSubtypeDiscriminatorValueFromMapping pins the path the #517
// fix leaves untouched: a mapping entry can still name an inline subtype by
// JSON reference, and mappingTagsFor — not subtypeDiscriminatorValue's
// implicit-name fallback — is what gives it that tag.
func TestAllOf_InlineSubtypeDiscriminatorValueFromMapping(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpec(`    Kennel:
      type: object
      properties:
        Dog:
          allOf:
            - $ref: '#/components/schemas/Pet'
          type: object
          properties:
            woof: {type: string}
        pet:
          allOf:
            - $ref: '#/components/schemas/Pet'
          type: object
          properties:
            meow: {type: string}
    Pet:
      type: object
      required: [kind]
      properties:
        kind: {type: string}
      discriminator:
        propertyName: kind
        mapping:
          woofer: '#/components/schemas/Kennel/properties/Dog'
    Dog:
      allOf:
        - $ref: '#/components/schemas/Pet'
      type: object
      properties:
        bark: {type: string}
`)
	doc, diags := schema.LowerSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)

	kennelDog := ir.TypeID("t/anon/components/schemas/Kennel/properties/Dog")
	kennelPet := ir.TypeID("t/anon/components/schemas/Kennel/properties/pet")
	want := map[ir.TypeID]string{
		componentID("Dog"): "Dog",
		kennelDog:          "woofer",
		kennelPet:          "",
	}
	got := discriminatorValues(t, doc, componentID("Dog"), kennelDog, kennelPet)
	assert.Empty(t, cmp.Diff(want, got),
		"a mapping entry names Kennel's inline Dog by JSON reference; pet still gets no implicit tag")

	pet, ok := doc.Types[componentID("Pet")].(*ir.Model)
	require.True(t, ok, "Pet should be a model")
	require.NotNil(t, pet.Discriminator)
	assert.Equal(t, map[string]ir.TypeID{"woofer": kennelDog}, pet.Discriminator.Mapping,
		"the base's own mapping resolves the same inline subtype the tag was read from")
}

// TestAllOf_DiscriminatorValueFromDistantMapping pins the mapping-key spelling of
// a tag at depth: the root names a grandchild explicitly, and the grandchild's own
// branch is an intermediate that declares no discriminator, so the key is only
// reachable by walking past the immediate parent (GitHub #305).
//
// The conformance case discriminator-transitive.yaml witnesses the same walk.
func TestAllOf_DiscriminatorValueFromDistantMapping(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpec(`    Pet:
      type: object
      required: [petType]
      properties: {petType: {type: string}}
      discriminator:
        propertyName: petType
        mapping:
          dog: '#/components/schemas/Dog'
          puppy: '#/components/schemas/Puppy'
    Dog:
      allOf:
        - {$ref: '#/components/schemas/Pet'}
      properties: {bark: {type: boolean}}
    Puppy:
      allOf:
        - {$ref: '#/components/schemas/Dog'}
      properties: {weeks: {type: integer}}
`)
	doc, diags := schema.LowerSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)

	dog := typeByName(doc, "Dog").(*ir.Model)
	assert.Equal(t, "dog", dog.DiscriminatorValue, "the direct child takes its own mapping key")

	puppy := typeByName(doc, "Puppy").(*ir.Model)
	require.NotNil(t, puppy.Base)
	assert.Equal(t, componentID("Dog"), puppy.Base.Target,
		"Puppy composes Dog, which declares no discriminator of its own")
	assert.Equal(t, "puppy", puppy.DiscriminatorValue,
		"the key Pet's mapping spells for Puppy survives the intermediate")
}

// TestAllOf_DiscriminatorValueFromNearestAncestor pins which of two discriminated
// ancestors supplies the tag when both mappings name the same subtype. The walk
// is level by level so the answer is the nearer one, rather than whichever chain
// happened to be walked first — a subtype's own hierarchy outranks one it reaches
// only through a longer composition.
func TestAllOf_DiscriminatorValueFromNearestAncestor(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpec(`    Far:
      type: object
      properties: {k: {type: string}}
      discriminator:
        propertyName: k
        mapping: {far: '#/components/schemas/Leaf'}
    Mid:
      allOf:
        - {$ref: '#/components/schemas/Far'}
      properties: {m: {type: string}}
    Near:
      type: object
      properties: {n: {type: string}}
      discriminator:
        propertyName: n
        mapping: {near: '#/components/schemas/Leaf'}
    Leaf:
      allOf:
        - {$ref: '#/components/schemas/Mid'}
        - {$ref: '#/components/schemas/Near'}
      properties: {l: {type: string}}
`)
	doc, diags := schema.LowerSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)

	leaf := typeByName(doc, "Leaf").(*ir.Model)
	assert.Equal(t, "near", leaf.DiscriminatorValue,
		"Near is one hop up and Far two, so Near's mapping key wins")
}

// TestAllOf_DiscriminatorValueUndiscriminatedChain is the boundary of the
// ancestor walk: composing at any depth is not on its own a reason to carry a
// tag. Without this, a walk that fell back to the implicit schema name whenever
// it found a base would stamp every composed model in the document.
func TestAllOf_DiscriminatorValueUndiscriminatedChain(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpec(`    Plant:
      type: object
      properties: {stem: {type: string}}
    Shrub:
      allOf:
        - {$ref: '#/components/schemas/Plant'}
      properties: {twigs: {type: integer}}
    Sapling:
      allOf:
        - {$ref: '#/components/schemas/Shrub'}
      properties: {rings: {type: integer}}
`)
	doc, diags := schema.LowerSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)

	for _, name := range []string{"Shrub", "Sapling"} {
		m := typeByName(doc, name).(*ir.Model)
		require.NotNil(t, m.Base, "%s composes a base", name)
		assert.Empty(t, m.DiscriminatorValue,
			"%s has no discriminated ancestor at any depth", name)
	}
}

// TestAllOf_DiscriminatorValueCyclicComposition drives the ancestor walk over a
// composition cycle. Nothing upstream refuses one — this spec compiles without an
// error diagnostic — so the walk meets it, and the visited set rather than the
// depth cap is what has to stop it.
//
// The fan-out is what makes that a real claim: every schema composes every other,
// so a walk that re-entered a schema it had already left would branch five ways
// per level for the length of the cap instead of terminating.
func TestAllOf_DiscriminatorValueCyclicComposition(t *testing.T) {
	t.Parallel()
	const n = 6
	var b strings.Builder
	for i := range n {
		refs := make([]string, 0, n-1)
		for j := range n {
			if j != i {
				refs = append(refs, fmt.Sprintf("{$ref: '#/components/schemas/X%d'}", j))
			}
		}
		fmt.Fprintf(&b, "    X%d:\n      allOf: [%s]\n      properties: {p%d: {type: string}}\n",
			i, strings.Join(refs, ", "), i)
	}
	doc, diags := schema.LowerSpec(t, openapitest.ComponentSpec(b.String()))
	openapitest.RequireNoErrorDiags(t, diags)

	for i := range n {
		name := fmt.Sprintf("X%d", i)
		m, ok := typeByName(doc, name).(*ir.Model)
		require.True(t, ok, "%s lowers to a Model", name)
		assert.Empty(t, m.DiscriminatorValue, "%s has no discriminated ancestor", name)
	}
}

// TestAllOf_DiscriminatorValueSelfAncestorInCycle pins that a composition cycle
// does not make a schema one of its own ancestors. A declares the discriminator
// and composes B, which composes A back, so walking A's ancestors comes round to
// A — whose mapping names A. A hierarchy of one schema standing above itself is
// not a hierarchy, so the walk skips the schema it started from.
func TestAllOf_DiscriminatorValueSelfAncestorInCycle(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpec(`    A:
      allOf: [{$ref: '#/components/schemas/B'}]
      required: [k]
      properties: {k: {type: string}}
      discriminator:
        propertyName: k
        mapping:
          selfkey: '#/components/schemas/A'
    B:
      allOf: [{$ref: '#/components/schemas/A'}]
      properties: {b: {type: string}}
`)
	doc, diags := schema.LowerSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)

	a, ok := typeByName(doc, "A").(*ir.Model)
	require.True(t, ok, "A lowers to a Model")
	require.NotNil(t, a.Discriminator, "A anchors the hierarchy it declares")
	assert.Empty(t, a.DiscriminatorValue,
		"and takes no tag from its own mapping by way of the cycle")
}

// TestAllOf_DiscriminatorValueChainDeeperThanCap pins the ancestor walk's cap as
// behaviour rather than as a comment. A chain longer than the cap stops being
// searched, so the tag the root would have supplied is not stamped — stated here
// so the limit is a decision on record and a chain that grows past it is a
// failing test rather than a silent reversion to the bug this walk fixed.
//
// The shallower half is the control: the same construction one level inside the
// cap does carry the tag, which is what says the deep case failed on the cap and
// not on the construction.
func TestAllOf_DiscriminatorValueChainDeeperThanCap(t *testing.T) {
	t.Parallel()
	// The two cases below straddle the cap.
	const ancestorCap = schema.MaxDiscriminatorAncestorDepth
	for _, tc := range []struct {
		name  string
		links int
		want  string
	}{
		{"within the cap", ancestorCap - 1, fmt.Sprintf("S%d", ancestorCap-2)},
		{"beyond the cap", ancestorCap + 4, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var b strings.Builder
			b.WriteString("    Root:\n      type: object\n      properties: {k: {type: string}}\n" +
				"      discriminator: {propertyName: k}\n")
			prev := "Root"
			for i := range tc.links {
				fmt.Fprintf(&b, "    S%d:\n      allOf: [{$ref: '#/components/schemas/%s'}]\n"+
					"      properties: {p%d: {type: string}}\n", i, prev, i)
				prev = fmt.Sprintf("S%d", i)
			}
			doc, diags := schema.LowerSpec(t, openapitest.ComponentSpec(b.String()))
			openapitest.RequireNoErrorDiags(t, diags)

			leaf := typeByName(doc, prev).(*ir.Model)
			assert.Equal(t, tc.want, leaf.DiscriminatorValue,
				"%s is %d links below the discriminated root", prev, tc.links)
		})
	}
}

func TestAllOf_ModelWithOwnDiscriminator(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpec(`    Base:
      allOf:
        - {$ref: '#/components/schemas/Common'}
      discriminator: {propertyName: kind}
      properties: {kind: {type: string}}
    Common: {type: object, properties: {id: {type: string}}}
`)
	doc, diags := schema.LowerSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	base := typeByName(doc, "Base").(*ir.Model)
	require.NotNil(t, base.Discriminator, "allOf model may declare its own discriminator")
	assert.NotEmpty(t, base.Discriminator.Property)
}

func TestAllOf_BoolRefBranchHasNoDiscriminator(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpec(`    BoolComp: false
    Sub:
      allOf:
        - {$ref: '#/components/schemas/BoolComp'}
        - {type: object, properties: {a: {type: string}}}
`)
	doc, diags := schema.LowerSpec(t, spec)
	require.NotNil(t, doc)
	sub := typeByName(doc, "Sub").(*ir.Model)
	assert.Empty(t, sub.DiscriminatorValue, "a bool-schema ref target anchors no hierarchy")
	_ = diags
}

// TestAllOf_DiscriminatorAliasTagIsOrderInvariant pins that a subtype two mapping
// keys name takes the same tag whichever key is written first (GitHub #410).
// Model.DiscriminatorValue holds one value, and a mapping is unordered, so the
// key it holds must be chosen by a rule the source order cannot reach: the
// smallest in byte order. The base keeps every key, so nothing is lost, and the
// election is reported so a reader knows the subtype's tag set is wider than
// the field shows.
func TestAllOf_DiscriminatorAliasTagIsOrderInvariant(t *testing.T) {
	t.Parallel()
	const base = `    Pet:
      type: object
      required: [k]
      properties: {k: {type: string}}
      discriminator:
        propertyName: k
        mapping:
`
	const sub = `    Dog:
      allOf: [{$ref: '#/components/schemas/Pet'}]
      properties: {bark: {type: boolean}}
`
	const alpha = "          alpha: '#/components/schemas/Dog'\n"
	const zulu = "          zulu: '#/components/schemas/Dog'\n"

	for _, tc := range []struct{ name, mapping string }{
		{"zulu first", zulu + alpha},
		{"alpha first", alpha + zulu},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, diags := schema.LowerSpec(t, openapitest.ComponentSpec(base+tc.mapping+sub))
			openapitest.RequireNoErrorDiags(t, diags)

			dog, ok := doc.Types[componentID("Dog")].(*ir.Model)
			require.True(t, ok, "Dog should be a model")
			assert.Equal(t, "alpha", dog.DiscriminatorValue,
				"the smallest key in byte order, whichever was written first")

			pet, ok := doc.Types[componentID("Pet")].(*ir.Model)
			require.True(t, ok, "Pet should be a model")
			assert.Equal(t, map[string]ir.TypeID{"alpha": componentID("Dog"), "zulu": componentID("Dog")},
				pet.Discriminator.Mapping, "the base keeps both keys")

			assert.True(t, openapitest.HasDiagCodeAt(diags, diag.DegradedConstruct, "/components/schemas/Dog"),
				"the election is reported at the subtype: %+v", diags)
			assert.True(t, openapitest.HasDiagAt(diags, diag.DegradedConstruct, ir.SeverityInfo),
				"as information, since the base's mapping loses nothing")
		})
	}
}

// discriminatorValues reads the DiscriminatorValue of every Model named in
// typeIDs off doc, so a mismatch anywhere in the set shows every id at once
// rather than only the first one asserted.
func discriminatorValues(t *testing.T, doc *ir.Document, typeIDs ...ir.TypeID) map[ir.TypeID]string {
	t.Helper()
	got := make(map[ir.TypeID]string, len(typeIDs))
	for _, id := range typeIDs {
		m, ok := doc.Types[id].(*ir.Model)
		require.True(t, ok, "%s should be a model", id)
		got[id] = m.DiscriminatorValue
	}
	return got
}
