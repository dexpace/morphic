// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/compilers/openapi/internal/schema"
	"github.com/dexpace/morphic/ir"
)

// TestAllOf_AUnionAloneOnAnEntryLowersAsItsBareSpelling holds a union on an
// allOf entry, the rest writing nothing or a `type` every branch says, to lower
// as the same union written at the position
// (docs/compilers/openapi.md#union-beside-structure): one branch and a null
// branch are the nullable reference to the branch, keeping nothing. Branches
// only narrowing an object stay beside a `type: object` another entry writes,
// kept as validation logic on a model, as beside the same keyword.
func TestAllOf_AUnionAloneOnAnEntryLowersAsItsBareSpelling(t *testing.T) {
	t.Parallel()
	doc, diags := schema.LowerSpec(t, openapitest.ComponentSpecVer("3.1.0", `    X: {type: object, properties: {id: {type: string}}}
    Bare: {oneOf: [{$ref: '#/components/schemas/X'}, {type: 'null'}]}
    Entry: {allOf: [{oneOf: [{$ref: '#/components/schemas/X'}, {type: 'null'}]}]}
    Typed: {allOf: [{type: [object, 'null']}, {oneOf: [{$ref: '#/components/schemas/X'}, {type: 'null'}]}]}
    NarrowingBare: {type: object, oneOf: [{type: object, required: [a]}, {type: object, required: [b]}]}
    NarrowingEntry: {allOf: [{type: object}, {oneOf: [{type: object, required: [a]}, {type: object, required: [b]}]}]}
`))
	for _, name := range []string{"Bare", "Entry", "Typed"} {
		alias, ok := typeByName(doc, name).(*ir.Scalar)
		require.True(t, ok, "%s is a use of X", name)
		assert.Equal(t, &ir.TypeRef{Target: componentID("X"), Nullable: true}, alias.Base, name)
		assert.Empty(t, alias.Unmodeled, "%s keeps nothing", name)
	}
	for _, name := range []string{"NarrowingBare", "NarrowingEntry"} {
		model, ok := typeByName(doc, name).(*ir.Model)
		require.True(t, ok, "%s is a model", name)
		kept, ok := model.Unmodeled["openapi:oneOf"]
		require.True(t, ok, "%s keeps its oneOf", name)
		assert.Equal(t, ir.ReasonValidationOnly, kept.Reason, name)
	}
	for _, d := range diags {
		assert.NotEqual(t, ir.SeverityWarning, d.Severity, d.Message)
	}
}

// TestAllOf_AUnionBesideTheAllOfKeepsAnEntrysFromStandingAlone holds a union
// on an allOf entry, the schema writing a union of its own beside the allOf,
// to be no principal standing alone (docs/compilers/openapi.md#allof-refinement):
// the two conjoin, so the position is a Union, not the nullable reference to
// the entry's one other branch its union alone would be.
func TestAllOf_AUnionBesideTheAllOfKeepsAnEntrysFromStandingAlone(t *testing.T) {
	t.Parallel()
	doc, _ := schema.LowerSpec(t, openapitest.ComponentSpecVer("3.1.0", `    X: {type: object, properties: {id: {type: string}}}
    Y: {type: object, properties: {n: {type: integer}}}
    Beside: {allOf: [{oneOf: [{$ref: '#/components/schemas/X'}, {type: 'null'}]}], anyOf: [{$ref: '#/components/schemas/Y'}]}
`))
	_, ok := typeByName(doc, "Beside").(*ir.Union)
	assert.True(t, ok, "the position is a Union")
}
