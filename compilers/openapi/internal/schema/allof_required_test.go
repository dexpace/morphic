// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/compilers/openapi/internal/schema"
	"github.com/dexpace/morphic/ir"
)

// TestAllOf_CompositionRequiredAttaches covers issue #29: allOf is an
// intersection, so a `required` list constrains the whole composed object
// regardless of which entry declares the named property, or whether it is
// paired with a properties map at all. Each case names a wire property that
// must end up Required=true, which leaves Requires empty, since every name
// matches an own property.
func TestAllOf_CompositionRequiredAttaches(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string // subtest name, describing the case
		spec  string
		model string // component under test
		wire  string // property expected to end up Required=true
	}{
		{
			name: "required-only entry after the entry declaring the property (issue #29 repro)",
			spec: openapitest.ComponentSpec(`    Thing:
      allOf:
        - type: object
          properties:
            id: {type: integer}
        - required: [id]
`),
			model: "Thing",
			wire:  "id",
		},
		{
			name: "required-only entry before the entry declaring the property",
			spec: openapitest.ComponentSpec(`    ThingB:
      allOf:
        - required: [id]
        - type: object
          properties:
            id: {type: integer}
`),
			model: "ThingB",
			wire:  "id",
		},
		{
			name: "required-only entry, property declared alongside allOf",
			spec: openapitest.ComponentSpec(`    Foo:
      type: object
      properties:
        a: {type: string}
    Bar:
      type: object
      properties:
        type: {type: string}
        name: {type: string}
      allOf:
        - {$ref: "#/components/schemas/Foo"}
        - required: [type]
`),
			model: "Bar",
			wire:  "type",
		},
		{
			name: "entry declares properties: {} (empty, non-nil) plus required",
			spec: openapitest.ComponentSpec(`    ThingF:
      allOf:
        - type: object
          properties:
            id: {type: integer}
        - type: object
          properties: {}
          required: [id]
`),
			model: "ThingF",
			wire:  "id",
		},
		{
			name: "the schema's own required (sibling of allOf, no sibling properties) names a property declared inside an entry",
			spec: openapitest.ComponentSpec(`    ThingG:
      required: [id]
      allOf:
        - type: object
          properties:
            id: {type: integer}
`),
			model: "ThingG",
			wire:  "id",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, diags := schema.LowerSpec(t, tc.spec)
			openapitest.RequireNoErrorDiags(t, diags)
			model, ok := doc.Types[componentID(tc.model)].(*ir.Model)
			require.True(t, ok, "%s should be a model", tc.model)
			property, ok := openapitest.PropertiesByWire(model.Properties)[tc.wire]
			require.True(t, ok, "%s should have a %q property", tc.model, tc.wire)
			assert.True(t, property.Required, "%s.%s should be required via composition-scope required", tc.model, tc.wire)
			assert.Empty(t, model.Requires, "every required name matches an own property, so none is listed beside them")
		})
	}
}

// TestAllOf_RequiredOnlyEntryNamingBaseOwnedPropertyLandsInRequires pins an
// allOf composed of a sole $ref (its Base) and a required-only entry naming a
// property only the base declares. docs/ir/models.md#composition stores a
// model's own properties, never a flattened view, so the name lands in
// Model.Requires: the composed model makes the inherited property required, and
// nothing is misattached to an unrelated property or reported as lost.
func TestAllOf_RequiredOnlyEntryNamingBaseOwnedPropertyLandsInRequires(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpec(`    Human:
      type: object
      properties:
        name: {type: string}
        level: {type: integer}
    SuperBaby:
      allOf:
        - {$ref: "#/components/schemas/Human"}
        - required: [level]
          properties:
            gender: {type: string}
`)
	doc, diags := schema.LowerSpec(t, spec)
	model, ok := doc.Types[componentID("SuperBaby")].(*ir.Model)
	require.True(t, ok, "SuperBaby should be a model")
	require.NotNil(t, model.Base, "the sole $ref still becomes Base")
	assert.Equal(t, componentID("Human"), model.Base.Target)

	_, hasLevel := openapitest.PropertiesByWire(model.Properties)["level"]
	assert.False(t, hasLevel, "level belongs to the base, not to SuperBaby's own properties")
	gender, hasGender := openapitest.PropertiesByWire(model.Properties)["gender"]
	require.True(t, hasGender, "gender is SuperBaby's own property")
	assert.False(t, gender.Required, "level's requiredness never misattaches to gender")
	assert.Equal(t, []string{"level"}, model.Requires, "SuperBaby makes the level Human declares required")
	assert.Empty(t, diags, "the requirement is carried, so nothing is reported")
}

// TestAllOf_RequiredNameNothingDeclaresLandsInRequires covers an allOf with no
// $ref entry at all, so nothing becomes Base or a Mixin and the required name
// is one no schema declares: legal JSON Schema, which requires the member
// whatever its value. It lands in Model.Requires as the inherited one does,
// with no property invented for it.
func TestAllOf_RequiredNameNothingDeclaresLandsInRequires(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpec(`    Thing:
      allOf:
        - type: object
          properties:
            id: {type: integer}
        - required: [ghost]
`)
	doc, diags := schema.LowerSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	model, ok := doc.Types[componentID("Thing")].(*ir.Model)
	require.True(t, ok, "Thing should be a model")
	assert.Nil(t, model.Base, "no $ref entry at all: no Base")
	assert.Empty(t, model.Mixins, "no $ref entry at all: no Mixins")

	id, hasID := openapitest.PropertiesByWire(model.Properties)["id"]
	require.True(t, hasID, "the entry's own id property still lowers")
	assert.False(t, id.Required, "ghost's requiredness never misattaches to id")
	_, hasGhost := openapitest.PropertiesByWire(model.Properties)["ghost"]
	assert.False(t, hasGhost, "ghost is never invented as a property")
	assert.Equal(t, []string{"ghost"}, model.Requires)
	assert.Empty(t, diags, "the requirement is carried, so nothing is reported")
}

// TestAllOf_RefEntryWithSiblingRequired31 pins the real behavior of a $ref
// allOf entry that also carries a sibling `required` (legal in OpenAPI 3.1 /
// JSON Schema 2020-12, where $ref no longer has to be the schema object's only
// keyword). compositionRequired reads an entry's required list off its own
// local schema node (GetSchema()) — never the resolved $ref target, which
// belongs to that target's own model — so this pins what that local schema
// node actually exposes for a $ref-with-siblings entry in this library.
func TestAllOf_RefEntryWithSiblingRequired31(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpec(`    Human:
      type: object
      properties:
        name: {type: string}
    SuperBoyLike:
      allOf:
        - $ref: "#/components/schemas/Human"
          required: [level]
        - type: object
          properties:
            level: {type: integer}
`)
	doc, diags := schema.LowerSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	model, ok := doc.Types[componentID("SuperBoyLike")].(*ir.Model)
	require.True(t, ok, "SuperBoyLike should be a model")
	require.NotNil(t, model.Base, "the sole $ref still becomes Base despite the sibling required")

	level, ok := openapitest.PropertiesByWire(model.Properties)["level"]
	require.True(t, ok, "level is declared by the inline entry")
	assert.True(t, level.Required,
		"a required sibling on a $ref entry is read off the entry's own local schema and attaches to level")
}

// TestAllOf_RequiresIsTheSameInEitherComponentOrder pins that an allOf making
// an inherited property required, and naming one nothing declares, lands both
// names in Model.Requires whichever of the composed model and its base is
// declared first. The composed model has no property of its own to hold either
// name, and the names come from the composition's required lists alone, so
// nothing about the base's declaration may reach them.
func TestAllOf_RequiresIsTheSameInEitherComponentOrder(t *testing.T) {
	t.Parallel()
	const (
		user    = "    User: {type: object, properties: {id: {type: string}}}\n"
		created = `    CreatedUser:
      allOf:
        - {$ref: '#/components/schemas/User'}
        - {required: [id, extra]}
`
	)
	nodes := make([]*ir.Model, 0, 2)
	for _, src := range []string{user + created, created + user} {
		doc, diags := schema.LowerSpec(t, openapitest.ComponentSpec(src))
		openapitest.RequireNoErrorDiags(t, diags)
		model, ok := typeByName(doc, "CreatedUser").(*ir.Model)
		require.True(t, ok, "CreatedUser is a model")
		nodes = append(nodes, model)
	}

	assert.Empty(t, cmp.Diff(nodes[0], nodes[1]), "declaration order must not change the composed model")
	for i, model := range nodes {
		assert.Equal(t, []string{"id", "extra"}, model.Requires, "order %d: in source order, the inherited name included", i)
		assert.Empty(t, model.Properties, "order %d: neither name is declared here, so no property holds one", i)
		require.NotNil(t, model.Base, "order %d", i)
		assert.Equal(t, componentID("User"), model.Base.Target)
	}
}

// TestAllOf_ARequiredNameWrittenInSeveralListsIsListedOnce pins that a name an
// allOf writes in more than one required list, an entry's and the composed
// schema's own, is in Model.Requires once, where it is first written. Each list
// is free of repeats, so the validation reports nothing, and only the lowering
// that gathers them can list a name twice.
func TestAllOf_ARequiredNameWrittenInSeveralListsIsListedOnce(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpec(`    User: {type: object, properties: {id: {type: string}}}
    CreatedUser:
      allOf:
        - {$ref: '#/components/schemas/User'}
        - {required: [id, extra]}
        - {required: [extra, other]}
      required: [extra, id]
`)
	doc, diags := schema.LowerSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	assert.Empty(t, diags, "no list repeats a name, so nothing is reported")

	model, ok := typeByName(doc, "CreatedUser").(*ir.Model)
	require.True(t, ok, "CreatedUser is a model")
	assert.Equal(t, []string{"id", "extra", "other"}, model.Requires, "once each, in the order first written")
}

// TestAllOf_BoolEntryContributesNoRequiredNames covers an allOf entry that is
// a bare boolean schema (`true` here) rather than a $ref or an inline object,
// so its schema is nil. It pins that the bool entry contributes no required
// names and that reading it does not panic, while the sibling object entry's
// property and required still lower normally.
func TestAllOf_BoolEntryContributesNoRequiredNames(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpec(`    Thing:
      allOf:
        - type: object
          required: [id]
          properties:
            id: {type: integer}
        - true
`)
	doc, diags := schema.LowerSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	model, ok := doc.Types[componentID("Thing")].(*ir.Model)
	require.True(t, ok, "Thing should be a model")
	assert.Nil(t, model.Base, "a bare boolean entry is never a $ref, so no Base")
	assert.Empty(t, model.Mixins)

	id, hasID := openapitest.PropertiesByWire(model.Properties)["id"]
	require.True(t, hasID, "the object entry's own property still lowers despite the sibling bool entry")
	assert.True(t, id.Required, "the object entry's own required still attaches to its own property")
	assert.Empty(t, model.Requires, "the boolean entry contributes no required names, so nothing goes unattached")
}
