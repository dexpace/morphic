// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/compilers/openapi/internal/schema"
	"github.com/dexpace/morphic/ir"
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
