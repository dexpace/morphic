package openapi

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

func TestAllOf_SoleRefBecomesBase(t *testing.T) {
	t.Parallel()
	spec := `openapi: 3.1.0
info: {title: T, version: "1"}
paths: {}
components:
  schemas:
    Animal:
      type: object
      properties:
        name: {type: string}
    Dog:
      allOf:
        - {$ref: "#/components/schemas/Animal"}
        - type: object
          properties:
            bark: {type: string}
`
	doc, diags := lowerSpec(t, spec)
	requireNoErrorDiags(t, diags)
	dog, ok := doc.Types[ir.TypeID("t/openapi/components/schemas/Dog")].(*ir.Model)
	require.True(t, ok, "Dog should be a model")
	require.NotNil(t, dog.Base, "sole $ref becomes Base")
	assert.Equal(t, ir.TypeID("t/openapi/components/schemas/Animal"), dog.Base.Target)
	assert.Empty(t, dog.Mixins, "no extra refs, no mixins")
	require.Len(t, dog.Properties, 1, "only the inline branch's own property")
	assert.Equal(t, "bark", dog.Properties[0].Name.Source)
	assert.Contains(t, dog.Properties[0].Provenance.Pointer, "/allOf/1",
		"merged property provenance points into its allOf branch")
}

func TestAllOf_ExtraRefsBecomeMixins(t *testing.T) {
	t.Parallel()
	spec := `openapi: 3.1.0
info: {title: T, version: "1"}
paths: {}
components:
  schemas:
    A:
      type: object
      properties:
        a: {type: string}
    B:
      type: object
      properties:
        b: {type: string}
    C:
      allOf:
        - {$ref: "#/components/schemas/A"}
        - {$ref: "#/components/schemas/B"}
        - type: object
          properties:
            c: {type: string}
`
	doc, diags := lowerSpec(t, spec)
	requireNoErrorDiags(t, diags)
	c, ok := doc.Types[ir.TypeID("t/openapi/components/schemas/C")].(*ir.Model)
	require.True(t, ok, "C should be a model")
	assert.Nil(t, c.Base, "two non-hierarchy refs, neither sole: no Base")
	require.Len(t, c.Mixins, 2, "both refs become mixins")
	assert.Equal(t, ir.TypeID("t/openapi/components/schemas/A"), c.Mixins[0].Target)
	assert.Equal(t, ir.TypeID("t/openapi/components/schemas/B"), c.Mixins[1].Target)
	require.Len(t, c.Properties, 1)
	assert.Equal(t, "c", c.Properties[0].Name.Source)
}

func TestAllOf_DiscriminatorSubtypeValue(t *testing.T) {
	t.Parallel()
	spec := `openapi: 3.1.0
info: {title: T, version: "1"}
paths: {}
components:
  schemas:
    Pet:
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
`
	doc, diags := lowerSpec(t, spec)
	requireNoErrorDiags(t, diags)

	pet, ok := doc.Types[ir.TypeID("t/openapi/components/schemas/Pet")].(*ir.Model)
	require.True(t, ok, "Pet should be a model")
	require.NotNil(t, pet.Discriminator, "the base carries the discriminator")
	assert.Empty(t, pet.DiscriminatorValue, "the base itself has no wire tag value")

	cat, ok := doc.Types[ir.TypeID("t/openapi/components/schemas/Cat")].(*ir.Model)
	require.True(t, ok, "Cat should be a model")
	assert.Equal(t, ir.TypeID("t/openapi/components/schemas/Pet"), cat.Base.Target)
	assert.Equal(t, "kitty", cat.DiscriminatorValue,
		"mapping key pointing at Cat becomes its wire tag value")

	dog, ok := doc.Types[ir.TypeID("t/openapi/components/schemas/Dog")].(*ir.Model)
	require.True(t, ok, "Dog should be a model")
	assert.Equal(t, ir.TypeID("t/openapi/components/schemas/Pet"), dog.Base.Target)
	assert.Equal(t, "Dog", dog.DiscriminatorValue,
		"a subtype absent from the mapping falls back to its schema name")
}

func TestOneOf_WithDiscriminator(t *testing.T) {
	t.Parallel()
	spec := `openapi: 3.1.0
info: {title: T, version: "1"}
paths: {}
components:
  schemas:
    Cat:
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
`
	doc, diags := lowerSpec(t, spec)
	requireNoErrorDiags(t, diags)
	pet, ok := doc.Types[ir.TypeID("t/openapi/components/schemas/Pet")].(*ir.Union)
	require.True(t, ok, "Pet should be a union")
	assert.True(t, pet.Exclusive, "oneOf is exclusive")
	assert.False(t, pet.WireTagged, "OpenAPI oneOf is not wire-tagged")
	require.Len(t, pet.Variants, 2, "both variants present")
	require.NotNil(t, pet.Discriminator)
	assert.Equal(t, "petType", pet.Discriminator.PropertyName)
	assert.Equal(t, map[string]ir.TypeID{"cat": "t/openapi/components/schemas/Cat"},
		pet.Discriminator.Mapping)
}

func TestAnyOf_IsNonExclusiveUnion(t *testing.T) {
	t.Parallel()
	spec := `openapi: 3.1.0
info: {title: T, version: "1"}
paths: {}
components:
  schemas:
    U:
      anyOf:
        - {type: string}
        - {type: integer}
`
	doc, diags := lowerSpec(t, spec)
	requireNoErrorDiags(t, diags)
	u, ok := doc.Types[ir.TypeID("t/openapi/components/schemas/U")].(*ir.Union)
	require.True(t, ok, "U should be a union")
	assert.False(t, u.Exclusive, "anyOf is non-exclusive")
	require.Len(t, u.Variants, 2)
}

func TestOneOf_NullVariantCollapses(t *testing.T) {
	t.Parallel()
	spec := `openapi: 3.1.0
info: {title: T, version: "1"}
paths: {}
components:
  schemas:
    S:
      type: object
      properties:
        p:
          oneOf:
            - {type: string}
            - {type: "null"}
`
	doc, diags := lowerSpec(t, spec)
	requireNoErrorDiags(t, diags)
	s, ok := doc.Types[ir.TypeID("t/openapi/components/schemas/S")].(*ir.Model)
	require.True(t, ok)
	require.Len(t, s.Properties, 1)
	assert.Equal(t, ir.TypeRef{Target: "t/prim/string", Nullable: true}, s.Properties[0].Type)
	for id, def := range doc.Types {
		_, isUnion := def.(*ir.Union)
		assert.False(t, isUnion, "null-variant oneOf must not produce a union node: %s", id)
	}
}

func TestEnum_StringClosed(t *testing.T) {
	t.Parallel()
	spec := `openapi: 3.1.0
info: {title: T, version: "1"}
paths: {}
components:
  schemas:
    E:
      type: string
      enum: [a, b]
`
	doc, diags := lowerSpec(t, spec)
	requireNoErrorDiags(t, diags)
	e, ok := doc.Types[ir.TypeID("t/openapi/components/schemas/E")].(*ir.Enum)
	require.True(t, ok, "E should be an enum")
	assert.True(t, e.Closed, "JSON Schema enum is closed")
	assert.Equal(t, ir.PrimString, e.ValueType)
	require.Len(t, e.Members, 2)
	assert.Equal(t, "a", e.Members[0].Name.Source)
	assert.Equal(t, ir.Value{Kind: ir.ValueString, Str: "a"}, e.Members[0].Value)
	assert.Equal(t, "b", e.Members[1].Name.Source)
}

func TestConst_BecomesLiteral(t *testing.T) {
	t.Parallel()
	spec := `openapi: 3.1.0
info: {title: T, version: "1"}
paths: {}
components:
  schemas:
    K:
      const: "fixed"
`
	doc, diags := lowerSpec(t, spec)
	requireNoErrorDiags(t, diags)
	k, ok := doc.Types[ir.TypeID("t/openapi/components/schemas/K")].(*ir.Literal)
	require.True(t, ok, "K should be a literal")
	assert.Equal(t, ir.Value{Kind: ir.ValueString, Str: "fixed"}, k.Value)
}
