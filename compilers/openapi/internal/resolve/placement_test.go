// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package resolve_test

import (
	"encoding/json/jsontext"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/bind"
	"github.com/dexpace/morphic/compilers/openapi/internal/load"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/compilers/openapi/internal/resolve"
	"github.com/dexpace/morphic/engine/reader"
)

// TestScope_MappingTarget_WhereTheCompilerResolvedTheValue pins where the
// lowering places what a discriminator mapping value names, read as a $ref
// written at the discriminator is: a pointer or a component, a "#/$defs/..."
// value, which is the definition the nearest ancestor holds, and a value into
// another document, the position it lands on there. One a document part
// leaves reading from the root, one naming no
// definition and a bare name land on none, and neither does a missing
// discriminator, one the bindings do not know or a scope without them.
func TestScope_MappingTarget_WhereTheCompilerResolvedTheValue(t *testing.T) {
	t.Parallel()
	const schemas = `
    Cat: {type: object}
    Pet:
      oneOf: [{$ref: '#/components/schemas/Cat'}]
      $defs: {dog: {type: object}}
      discriminator:
        propertyName: kind
        mapping:
          cat: '#/components/schemas/Cat'
          dog: '#/$defs/dog'
          other: 'ext.yaml#/components/schemas/E'
          documentPart: 'spec.yaml#/$defs/dog'
          missing: '#/$defs/missing'
          bare: Cat
`
	b := loadBound(t, externalDocument(t), openapitest.ComponentSpec(schemas), load.Options{Reader: reader.Disk{}})
	require.NotNil(t, b.component(t, "Pet").GetSchema().GetDiscriminator())
	d := bind.DocumentPointer{Pointer: "/components/schemas/Pet/discriminator"}

	for value, want := range map[string]string{
		"#/components/schemas/Cat": "/components/schemas/Cat",
		"#/$defs/dog":              "/components/schemas/Pet/$defs/dog",
	} {
		got, ok := b.scope.MappingTarget(d, value)
		assert.True(t, ok, value)
		assert.Equal(t, bind.DocumentPointer{Pointer: jsontext.Pointer(want)}, got, value)
	}
	other, ok := b.scope.MappingTarget(d, "ext.yaml#/components/schemas/E")
	require.True(t, ok, "a value into another document lands there")
	assert.NotEqual(t, bind.Source, other.Doc)
	assert.Equal(t, jsontext.Pointer("/components/schemas/E"), other.Pointer)

	for value, why := range map[string]string{
		"spec.yaml#/$defs/dog":           "a document part reads from the root, which holds no dog",
		"#/$defs/missing":                "the $defs rule finds no such definition",
		"Cat":                            "a bare name is a component's name, read as no $ref",
		"#/components/schemas/Unwritten": "the table read no such value",
	} {
		got, ok := b.scope.MappingTarget(d, value)
		assert.False(t, ok, "%s: %s", value, why)
		assert.Zero(t, got, value)
	}

	_, ok = b.scope.MappingTarget(bind.DocumentPointer{}, "#/components/schemas/Cat")
	assert.False(t, ok, "the position of a schema the bindings do not know")
	_, ok = resolve.Scope{}.MappingTarget(d, "#/components/schemas/Cat")
	assert.False(t, ok, "no bindings, no target")
}
