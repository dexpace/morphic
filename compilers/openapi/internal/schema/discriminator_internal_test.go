// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"testing"

	"github.com/speakeasy-api/openapi/jsonschema/oas3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering/loweringtest"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
)

func TestPropIDByName_NotFound(t *testing.T) {
	t.Parallel()
	m := &ir.Model{Properties: []ir.Property{{ID: "p1", Name: ir.Naming{Source: "a"}}}}
	_, ok := propIDByName(m, "missing")
	assert.False(t, ok)
	id, ok := propIDByName(m, "a")
	assert.True(t, ok)
	assert.Equal(t, ir.PropID("p1"), id)
}

// TestDiscriminatorMapping_DefsValueNamesItsOwnInternedSibling drives the
// success path end to end: Pet's discriminator mapping values are
// "#/$defs/..." pointers naming Pet's own sibling definitions — the same
// definitions its oneOf branches name — so the mapping and the oneOf branch it
// tags promise the same pointer-derived ID, one node is built for both, and
// each tag keeps its own shape (GitHub #557).
func TestDiscriminatorMapping_DefsValueNamesItsOwnInternedSibling(t *testing.T) {
	t.Parallel()
	doc, diags := LowerSpec(t, openapitest.ComponentSpec(defsSiblingMappingSchemas))
	for _, d := range diags {
		assert.NotEqual(t, ir.SeverityError, d.Severity, "unexpected error diagnostic: %+v", d)
	}

	petID := ids.Document{}.NamedType(ids.Ptr("components", "schemas", "Pet"))
	u, ok := doc.Types[petID].(*ir.Union)
	require.True(t, ok, "a discriminated oneOf lowers to a Union")
	require.NotNil(t, u.Discriminator)

	catID, ok := u.Discriminator.Mapping["cat"]
	require.True(t, ok)
	dogID, ok := u.Discriminator.Mapping["dog"]
	require.True(t, ok)
	assert.NotEqual(t, catID, dogID, "each mapping value names its own definition, not a shared one")

	catModel, ok := doc.Types[catID].(*ir.Model)
	require.True(t, ok)
	_, hasMeow := propIDByName(catModel, "meow")
	assert.True(t, hasMeow, "cat's own property, not dog's")

	dogModel, ok := doc.Types[dogID].(*ir.Model)
	require.True(t, ok)
	_, hasBark := propIDByName(dogModel, "bark")
	assert.True(t, hasBark, "dog's own property, not cat's")

	require.Len(t, u.Variants, 2)
	variantTargets := []ir.TypeID{u.Variants[0].Type.Target, u.Variants[1].Type.Target}
	assert.Contains(t, variantTargets, catID, "the mapping names the node its oneOf branch names")
	assert.Contains(t, variantTargets, dogID)
}

// TestDiscriminatorMapping_DefsValueNothingInternedIsHoistedAtItsOwnPosition
// pins how a "#/$defs/..." mapping value resolves when its definition is not
// interned yet: hoisted where it is written, as any mapping to a sub-schema is
// (GitHub #530), not at the document-rooted /$defs/cat the value spells, which
// this document does not hold (GitHub #557, #910). Each component's tag names
// its own definition, in either declaration order.
func TestDiscriminatorMapping_DefsValueNothingInternedIsHoistedAtItsOwnPosition(t *testing.T) {
	t.Parallel()
	for name, blocks := range map[string]string{
		"Pet declared first": defsMappingPet + defsMappingToy,
		"Toy declared first": defsMappingToy + defsMappingPet,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			doc, diags := LowerSpec(t, openapitest.ComponentSpec(blocks))
			openapitest.RequireNoErrorDiags(t, diags)

			for _, owner := range []string{"Pet", "Toy"} {
				m, ok := doc.Types[ids.Document{}.NamedType(ids.Ptr("components", "schemas", owner))].(*ir.Model)
				require.True(t, ok, owner)
				require.NotNil(t, m.Discriminator, owner)

				want := ids.Document{}.ForPointer(ids.Ptr("components", "schemas", owner, "$defs", "cat"))
				assert.Equal(t, map[string]ir.TypeID{"cat": want}, m.Discriminator.Mapping, owner)
				_, interned := doc.Types[want]
				assert.True(t, interned, "%s's definition is interned where it is written", owner)
			}
		})
	}
}

// TestBesideUnionDiscriminator_ReportsWhatItCannotSettle pins that a settlement
// whose model is missing, nil or no model is announced rather than skipped:
// the discriminator it was deferred for would otherwise be settled nowhere,
// unremarked. A nil map is no settled one.
func TestBesideUnionDiscriminator_ReportsWhatItCannotSettle(t *testing.T) {
	t.Parallel()
	for name, reg := range map[string]ir.TypeRegistry{
		"model missing": {"t/other": &ir.Model{ID: "t/other"}},
		"no model":      {"t/ghost": &ir.Scalar{ID: "t/ghost"}},
		"nil model":     {"t/ghost": (*ir.Model)(nil)},
		"nil map":       {"t/ghost": (*ir.MapT)(nil)},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			c := loweringtest.Context(t, openapitest.EmptySpec, lowering.Policy{})
			d := besideUnionDiscriminator{c: c, pointer: "/components/schemas/Ghost", model: "t/ghost"}

			diags := d.Settle(reg)

			require.Len(t, diags, 1)
			assert.Equal(t, diag.InternalInvariant, diags[0].Code)
			assert.Equal(t, ir.SeverityError, diags[0].Severity)
		})
	}
}

// TestBesideUnionDiscriminator_LeavesASettledMapAlone pins the second
// settlement for one model: the first moved the discriminator and left a MapT,
// so the second has nothing to settle and reports nothing.
func TestBesideUnionDiscriminator_LeavesASettledMapAlone(t *testing.T) {
	t.Parallel()
	mt := &ir.MapT{ID: "t/pet", Value: ir.TypeRef{Target: "t/prim/string"}}
	reg := ir.TypeRegistry{"t/pet": mt}
	c := loweringtest.Context(t, openapitest.EmptySpec, lowering.Policy{})
	d := besideUnionDiscriminator{c: c, pointer: "/components/schemas/Pet", model: "t/pet"}

	assert.Empty(t, d.Settle(reg))
	assert.Same(t, mt, reg["t/pet"], "the map is left in place")
}

// petDiscriminator returns the discriminator the component schema Pet holds
// in l's document.
func petDiscriminator(t *testing.T, l *Lowerer) *oas3.Discriminator {
	t.Helper()
	js, ok := l.Ctx.Doc.Components.GetSchemas().Get("Pet")
	require.True(t, ok)
	d := js.GetSchema().GetDiscriminator()
	require.NotNil(t, d)
	return d
}

// defsSiblingMappingSchemas is the discriminator-mapping shape of GitHub #557:
// Pet's own discriminator maps each tag to a "#/$defs/..." pointer naming its
// own sibling definition, the same one its oneOf branch for that tag already
// $refs.
const defsSiblingMappingSchemas = `    Pet:
      oneOf: [{$ref: "#/$defs/cat"}, {$ref: "#/$defs/dog"}]
      discriminator: {propertyName: kind, mapping: {cat: "#/$defs/cat", dog: "#/$defs/dog"}}
      $defs:
        cat: {type: object, required: [kind], properties: {kind: {type: string}, meow: {type: string}}}
        dog: {type: object, required: [kind], properties: {kind: {type: string}, bark: {type: string}}}
`

// defsMappingPet and defsMappingToy each map a tag to a "#/$defs/cat" definition
// that nothing else in the schema references, so no oneOf branch has interned
// it when the mapping asks. They spell the same pointer for different
// definitions, GitHub #557's shape.
const (
	defsMappingPet = `    Pet:
      type: object
      properties: {kind: {type: string}}
      discriminator: {propertyName: kind, mapping: {cat: "#/$defs/cat"}}
      $defs:
        cat: {type: object, properties: {meow: {type: string}}}
`
	defsMappingToy = `    Toy:
      type: object
      properties: {kind: {type: string}}
      discriminator: {propertyName: kind, mapping: {cat: "#/$defs/cat"}}
      $defs:
        cat: {type: object, properties: {squeak: {type: string}}}
`
)
