// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/compilers/openapi/internal/schema"
	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irtest"
)

func TestModelDiscriminator_UndeclaredPropertyAndBadMapping(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpec(`    Vehicle:
      type: object
      discriminator:
        propertyName: kind
        mapping:
          car: '#'
      properties: {name: {type: string}}
`)
	doc, diags := schema.LowerSpec(t, spec)
	v := typeByName(doc, "Vehicle").(*ir.Model)
	require.NotNil(t, v.Discriminator)
	assert.Empty(t, v.Discriminator.Property, "undeclared property")
	assert.Equal(t, "kind", v.Discriminator.PropertyName)
	assert.True(t, openapitest.HasDiag(diags, diag.UnresolvedRef), "bad mapping target diagnostic")
}

func TestOneOf_DiscriminatorWithDefault(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpecVer("3.2.0", `    Shape:
      oneOf:
        - {$ref: '#/components/schemas/Circle'}
        - {$ref: '#/components/schemas/Square'}
      discriminator:
        propertyName: shapeType
        mapping: {circle: '#/components/schemas/Circle'}
        defaultMapping: '#/components/schemas/Square'
    Circle: {type: object, properties: {r: {type: number}}}
    Square: {type: object, properties: {s: {type: number}}}
`)
	doc, _ := schema.LowerSpec(t, spec)
	u := typeByName(doc, "Shape").(*ir.Union)
	require.NotNil(t, u.Discriminator)
	assert.Equal(t, "shapeType", u.Discriminator.PropertyName)
	assert.NotEmpty(t, u.Discriminator.Default)
}

// TestDiscriminatorMapping_RoutesToTheVariantAliasingItsTarget pins that a
// mapping names a variant where a keyword beside a branch's $ref gave the branch
// an alias over the component the mapping names, spelled either way and past a
// null branch, and that a component a variant names itself stays the target.
func TestDiscriminatorMapping_RoutesToTheVariantAliasingItsTarget(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, `openapi: 3.1.0
info: {title: t, version: "1"}
paths: {}
components:
  schemas:
    Cat: {type: object, properties: {kind: {type: string}}}
    Dog: {type: object, properties: {kind: {type: string}}}
    Pet:
      oneOf:
        - {type: "null"}
        - {$ref: '#/components/schemas/Cat', description: a cat}
        - {$ref: '#/components/schemas/Dog'}
      discriminator:
        propertyName: kind
        mapping: {cat: '#/components/schemas/Cat', tom: Cat, dog: '#/components/schemas/Dog'}
    Pair:
      oneOf:
        - {$ref: '#/components/schemas/Cat'}
        - {$ref: '#/components/schemas/Cat', description: an alias beside the type itself}
      discriminator: {propertyName: kind, mapping: {cat: '#/components/schemas/Cat'}}
`)
	openapitest.RequireNoErrorDiags(t, diags)
	assert.Empty(t, validated(t, doc))
	pet, ok := typeByName(doc, "Pet").(*ir.Union)
	require.True(t, ok)
	alias := pet.Variants[0].Type.Target
	assert.NotEqual(t, ir.TypeID("t/openapi/components/schemas/Cat"), alias)
	assert.Empty(t, cmp.Diff(map[string]ir.TypeID{
		"cat": alias, "tom": alias, "dog": "t/openapi/components/schemas/Dog",
	}, pet.Discriminator.Mapping))
	pair, ok := typeByName(doc, "Pair").(*ir.Union)
	require.True(t, ok)
	assert.Equal(t, ir.TypeID("t/openapi/components/schemas/Cat"), pair.Discriminator.Mapping["cat"],
		"the type itself is a variant, so the mapping stays on it")
}

// TestDiscriminatorMapping_TwoVariantsAliasingATypeRouteToNeither pins the rule
// for a target that two branches alias, each with its own keyword beside its
// $ref: nothing in the mapping says which variant it means, so it stays on the
// type, in either branch order, and validation reports it.
func TestDiscriminatorMapping_TwoVariantsAliasingATypeRouteToNeither(t *testing.T) {
	t.Parallel()
	spec := func(first, second string) string {
		return `openapi: 3.1.0
info: {title: t, version: "1"}
paths: {}
components:
  schemas:
    Cat: {type: object, properties: {kind: {type: string}}}
    Pet:
      oneOf:
        - {$ref: '#/components/schemas/Cat', description: ` + first + `}
        - {$ref: '#/components/schemas/Cat', description: ` + second + `}
      discriminator: {propertyName: kind, mapping: {cat: '#/components/schemas/Cat'}}
`
	}
	for _, src := range []string{spec("one", "two"), spec("two", "one")} {
		doc, diags := parseFull(t, src)
		openapitest.RequireNoErrorDiags(t, diags)
		u, ok := typeByName(doc, "Pet").(*ir.Union)
		require.True(t, ok)
		assert.Equal(t, ir.TypeID("t/openapi/components/schemas/Cat"), u.Discriminator.Mapping["cat"])
		assert.Contains(t, irtest.DiagCodes(validated(t, doc)), "pass/discriminator-missing-variant")
	}
}
