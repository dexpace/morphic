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

// TestDiscriminator_ARouteToADeadBranchRoutesNowhere holds each route the
// discriminator standing with a distributed union takes to a branch no
// instance of the body satisfies to no route: a mapping entry and a
// defaultMapping left out of the Discriminator and kept verbatim beside it,
// the name of the branch's component leaving the Mapping too, and the one
// warning naming them all, in byte order. A name an entry keys, or one two
// branches name, is no route of its own, and a discriminator the Union cannot
// hold is kept whole, its routes with it.
func TestDiscriminator_ARouteToADeadBranchRoutesNowhere(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpecVer("3.2.0", `    A: {type: object, properties: {kind: {type: string}}}
    Str: {type: string}
    Int: {type: integer}
    Written:
      type: object
      properties: {kind: {type: string}}
      oneOf: [{type: 'null'}, {$ref: '#/components/schemas/A'}, {$ref: '#/components/schemas/Str'}, {$ref: '#/components/schemas/Int'}]
      discriminator:
        propertyName: kind
        mapping: {z: '#/components/schemas/Str', a: '#/components/schemas/A', Int: '#/components/schemas/A', gone: '#/components/schemas/Gone'}
        defaultMapping: '#/components/schemas/Str'
    Twice:
      type: object
      properties: {kind: {type: string}}
      anyOf: [{$ref: '#/components/schemas/A'}, {$ref: '#/components/schemas/Str'}, {$ref: '#/components/schemas/Str'}]
      discriminator: {propertyName: kind}
    Unlocated:
      type: object
      properties: {kind: {type: string}}
      oneOf: [{$ref: '#/components/schemas/A'}, {$ref: '#/components/schemas/Str'}]
      discriminator: {propertyName: '', mapping: {s: '#/components/schemas/Str'}}
`)
	doc, diags := schema.LowerSpec(t, spec)
	require.NotNil(t, doc)
	for name, want := range map[string]string{
		"Written": `2 of its branches admit no object (oneOf/2: string, oneOf/3: integer), so no instance ` +
			`satisfies them: they are no variants of the union, kept verbatim under Unmodeled; no tag of the ` +
			`discriminator routes to them (defaultMapping, mapping "z", the name "Str"), the routes it writes ` +
			`kept verbatim under Unmodeled`,
		"Twice": `2 of its branches admit no object (anyOf/1: string, anyOf/2: string), so no instance ` +
			`satisfies them: they are no variants of the union, kept verbatim under Unmodeled`,
		"Unlocated": `no tag of the discriminator routes to it (mapping "s", the name "Str"), the routes it writes`,
	} {
		var warned []string
		for _, diagnostic := range diagsAtPointer(diags, diag.DegradedConstruct, "/components/schemas/"+name) {
			if diagnostic.Severity == ir.SeverityWarning {
				warned = append(warned, diagnostic.Message)
			}
		}
		require.Len(t, warned, 1, name)
		assert.Contains(t, warned[0], want, name)
	}
	assert.NotContains(t, diagsAtPointer(diags, diag.DegradedConstruct, "/components/schemas/Twice")[0].Message,
		"no tag", "a name two branches name routes to neither")

	written, ok := doc.Types[componentID("Written")].(*ir.Union)
	require.True(t, ok)
	require.Len(t, written.Variants, 1)
	variant := written.Variants[0].Type.Target
	assert.Equal(t, map[string]ir.TypeID{"a": variant, "Int": variant}, written.Discriminator.Mapping,
		"the dead routes leave, the live ones stay, and an entry naming no schema routes nowhere")
	assert.Empty(t, written.Discriminator.Default)
	for key, at := range map[string]string{
		"openapi:discriminator/mapping/z":      "/components/schemas/Written/discriminator/mapping/z",
		"openapi:discriminator/defaultMapping": "/components/schemas/Written/discriminator/defaultMapping",
	} {
		kept, ok := written.Unmodeled[key]
		require.True(t, ok, key)
		assert.JSONEq(t, `"#/components/schemas/Str"`, string(kept.Value), key)
		assert.Equal(t, at, string(kept.Provenance.Pointer), key)
	}
	assert.NotContains(t, written.Unmodeled, "openapi:discriminator/mapping/a")

	unlocated, ok := doc.Types[componentID("Unlocated")].(*ir.Union)
	require.True(t, ok)
	assert.Nil(t, unlocated.Discriminator, "an empty propertyName is no tag the Union can hold")
	assert.Contains(t, unlocated.Unmodeled, "openapi:discriminator", "the census keeps it whole")
	assert.NotContains(t, unlocated.Unmodeled, "openapi:discriminator/mapping/s", "and its routes with it")
}
