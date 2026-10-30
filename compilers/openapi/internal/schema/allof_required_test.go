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
// regardless of which branch declares the named property, or whether it is
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
			name: "required-only branch after the branch declaring the property (issue #29 repro)",
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
			name: "required-only branch before the branch declaring the property",
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
			name: "required-only branch, property declared alongside allOf",
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
			name: "branch declares properties: {} (empty, non-nil) plus required",
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
			name: "the schema's own required (sibling of allOf, no sibling properties) names a property declared inside a branch",
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
			m, ok := doc.Types[componentID(tc.model)].(*ir.Model)
			require.True(t, ok, "%s should be a model", tc.model)
			p, ok := openapitest.PropsByWire(m.Properties)[tc.wire]
			require.True(t, ok, "%s should have a %q property", tc.model, tc.wire)
			assert.True(t, p.Required, "%s.%s should be required via composition-scope required", tc.model, tc.wire)
			assert.Empty(t, m.Requires, "every required name matches an own property, so none is listed beside them")
		})
	}
}

// TestAllOf_RequiredOnlyBranchNamingBaseOwnedPropertyLandsInRequires pins an
// allOf composed of a sole $ref (its Base) and a required-only branch naming a
// property only the base declares. ir-design §4.3 stores a model's own
// properties, never a flattened view, so the name lands in Model.Requires: the
// composed model makes the inherited property required, and nothing is
// misattached to an unrelated property or reported as lost.
func TestAllOf_RequiredOnlyBranchNamingBaseOwnedPropertyLandsInRequires(t *testing.T) {
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
	m, ok := doc.Types[componentID("SuperBaby")].(*ir.Model)
	require.True(t, ok, "SuperBaby should be a model")
	require.NotNil(t, m.Base, "the sole $ref still becomes Base")
	assert.Equal(t, componentID("Human"), m.Base.Target)

	_, hasLevel := openapitest.PropsByWire(m.Properties)["level"]
	assert.False(t, hasLevel, "level belongs to the base, not to SuperBaby's own properties")
	gender, hasGender := openapitest.PropsByWire(m.Properties)["gender"]
	require.True(t, hasGender, "gender is SuperBaby's own property")
	assert.False(t, gender.Required, "level's requiredness never misattaches to gender")
	assert.Equal(t, []string{"level"}, m.Requires, "SuperBaby makes the level Human declares required")
	assert.Empty(t, diags, "the requirement is carried, so nothing is reported")
}

// TestAllOf_RequiredNameNothingDeclaresLandsInRequires covers an allOf with no
// $ref branch at all, so nothing becomes Base or a Mixin and the required name
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
	m, ok := doc.Types[componentID("Thing")].(*ir.Model)
	require.True(t, ok, "Thing should be a model")
	assert.Nil(t, m.Base, "no $ref branch at all: no Base")
	assert.Empty(t, m.Mixins, "no $ref branch at all: no Mixins")

	id, hasID := openapitest.PropsByWire(m.Properties)["id"]
	require.True(t, hasID, "the branch's own id property still lowers")
	assert.False(t, id.Required, "ghost's requiredness never misattaches to id")
	_, hasGhost := openapitest.PropsByWire(m.Properties)["ghost"]
	assert.False(t, hasGhost, "ghost is never invented as a property")
	assert.Equal(t, []string{"ghost"}, m.Requires)
	assert.Empty(t, diags, "the requirement is carried, so nothing is reported")
}

// TestAllOf_RefBranchWithSiblingRequired31 pins the real behavior of a $ref
// allOf branch that also carries a sibling `required` (legal in OpenAPI 3.1 /
// JSON Schema 2020-12, where $ref no longer has to be the schema object's only
// keyword). compositionRequired reads a branch's required list off its own
// local schema node (GetSchema()) — never the resolved $ref target, which
// belongs to that target's own model — so this pins what that local schema
// node actually exposes for a $ref-with-siblings branch in this library.
func TestAllOf_RefBranchWithSiblingRequired31(t *testing.T) {
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
	m, ok := doc.Types[componentID("SuperBoyLike")].(*ir.Model)
	require.True(t, ok, "SuperBoyLike should be a model")
	require.NotNil(t, m.Base, "the sole $ref still becomes Base despite the sibling required")

	level, ok := openapitest.PropsByWire(m.Properties)["level"]
	require.True(t, ok, "level is declared by the inline branch")
	assert.True(t, level.Required,
		"a required sibling on a $ref branch is read off the branch's own local schema and attaches to level")
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
		m, ok := typeByName(doc, "CreatedUser").(*ir.Model)
		require.True(t, ok, "CreatedUser is a model")
		nodes = append(nodes, m)
	}

	assert.Empty(t, cmp.Diff(nodes[0], nodes[1]), "declaration order must not change the composed model")
	for i, m := range nodes {
		assert.Equal(t, []string{"id", "extra"}, m.Requires, "order %d: in source order, the inherited name included", i)
		assert.Empty(t, m.Properties, "order %d: neither name is declared here, so no property holds one", i)
		require.NotNil(t, m.Base, "order %d", i)
		assert.Equal(t, componentID("User"), m.Base.Target)
	}
}

// TestAllOf_ARequiredNameWrittenInSeveralListsIsListedOnce pins that a name an
// allOf writes in more than one required list, a branch's and the composed
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

	m, ok := typeByName(doc, "CreatedUser").(*ir.Model)
	require.True(t, ok, "CreatedUser is a model")
	assert.Equal(t, []string{"id", "extra", "other"}, m.Requires, "once each, in the order first written")
}

// TestAllOf_BoolBranchSkippedInCompositionRequired covers an allOf branch that
// is a bare boolean schema (`true` here) rather than a $ref or an inline
// object, so b.GetSchema() returns nil for it. It pins that the bool branch
// contributes no required names, while the sibling object branch's property and
// required still lower normally.
func TestAllOf_BoolBranchSkippedInCompositionRequired(t *testing.T) {
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
	m, ok := doc.Types[componentID("Thing")].(*ir.Model)
	require.True(t, ok, "Thing should be a model")
	assert.Nil(t, m.Base, "a bare boolean branch is never a $ref, so no Base")
	assert.Empty(t, m.Mixins)

	id, hasID := openapitest.PropsByWire(m.Properties)["id"]
	require.True(t, hasID, "the object branch's own property still lowers despite the sibling bool branch")
	assert.True(t, id.Required, "the object branch's own required still attaches to its own property")
	assert.Empty(t, m.Requires, "the boolean branch contributes no required names, so nothing goes unattached")
}
