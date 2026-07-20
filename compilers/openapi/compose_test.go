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

func TestAllOf_OverlappingInlineBranchesReconcile(t *testing.T) {
	t.Parallel()
	// Two inline allOf branches redeclare the same fields — the shape GitHub's
	// webhook `forkee` uses (a documented object plus a doc-stripped duplicate
	// that marks some fields required). allOf is an intersection, so each wire
	// name must reconcile to a single property, not append a duplicate.
	spec := `openapi: 3.1.0
info: {title: T, version: "1"}
paths: {}
components:
  schemas:
    Forkish:
      allOf:
        - type: object
          properties:
            id: {type: integer, description: the identifier}
            name: {type: string}
        - type: object
          required: [id]
          properties:
            id: {type: integer}
            url: {type: string}
`
	doc, diags := lowerSpec(t, spec)
	requireNoErrorDiags(t, diags)
	m, ok := doc.Types[ir.TypeID("t/openapi/components/schemas/Forkish")].(*ir.Model)
	require.True(t, ok, "Forkish should be a model")

	byWire := map[string]int{}
	for _, p := range m.Properties {
		byWire[p.WireName]++
	}
	assert.Equal(t, 1, byWire["id"], "id declared in both branches reconciles to one property")
	assert.Equal(t, 1, byWire["name"])
	assert.Equal(t, 1, byWire["url"])
	require.Len(t, m.Properties, 3, "no duplicate properties across overlapping inline branches")

	var id ir.Property
	for _, p := range m.Properties {
		if p.WireName == "id" {
			id = p
		}
	}
	assert.True(t, id.Required, "required in the second branch => required on the merged model (allOf intersection)")
	assert.Equal(t, "the identifier", id.Docs.Description,
		"the richer (documented) declaration defines the reconciled shape")
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

func TestOneOf_ThreeVariantsWithNullStripsNullLiftsNullable(t *testing.T) {
	t.Parallel()
	// A oneOf with two non-null branches plus a null branch stays a Union of the
	// two non-null variants (the null branch is NOT emitted as an `any` variant),
	// and the enclosing ref becomes Nullable.
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
            - {type: integer}
            - {type: "null"}
`
	doc, diags := lowerSpec(t, spec)
	requireNoErrorDiags(t, diags)
	s := doc.Types[ir.TypeID("t/openapi/components/schemas/S")].(*ir.Model)
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
