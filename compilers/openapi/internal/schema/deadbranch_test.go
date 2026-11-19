// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema_test

import (
	"maps"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/compilers/openapi/internal/schema"
	"github.com/dexpace/morphic/ir"
)

// TestOneOf_AnUnpreservableDeadBranchIsNotAnnounced pins the pairing GitHub
// #144 exists for at a branch no instance of the body satisfies: one that
// fails to convert is reported as such, the union keeps nothing for it, and the
// warning naming it claims nothing was kept.
func TestOneOf_AnUnpreservableDeadBranchIsNotAnnounced(t *testing.T) {
	t.Parallel()
	doc, diags := schema.LowerSpec(t, openapitest.ComponentSpec(`    A: {type: object, properties: {a: {type: string}}}
    Combo:
      type: object
      properties: {kind: {type: string}}
      oneOf: [{$ref: '#/components/schemas/A'}, {type: string, x-t: `+unpreservableValue+`}]
`))

	u, ok := typeByName(doc, "Combo").(*ir.Union)
	require.True(t, ok)
	require.Len(t, u.Variants, 1)
	assert.NotContains(t, u.Unmodeled, "openapi:oneOf/1", "the conversion failed, so nothing was kept")
	assert.True(t, openapitest.HasDiag(diags, diag.UnpreservableConstruct), "the failure itself is reported")
	assert.Empty(t, openapitest.PreservationClaims(diags),
		"nothing was written under Unmodeled, so nothing may announce that it was")
	reports := diagsAtPointer(diags, diag.DegradedConstruct, "/components/schemas/Combo")
	require.Len(t, reports, 1, "the branch is still named as admitting no object")
	assert.Contains(t, reports[0].Message, "oneOf/1: string")
}

// TestOneOf_ADeadBranchIsReadAgainstItsEntrysOwnType pins the rest a branch of
// a union written on an allOf entry is read against: the entry's own type stays
// in it, its union left out (restAdmitsObjects), so beside `type: object` on
// that entry a branch admitting no object is dead, and the warning says so.
func TestOneOf_ADeadBranchIsReadAgainstItsEntrysOwnType(t *testing.T) {
	t.Parallel()
	doc, diags := schema.LowerSpec(t, openapitest.ComponentSpecVer("3.1.0", `    A: {type: object, properties: {a: {type: string}}}
    S: {allOf: [{type: object, oneOf: [{type: string}, {$ref: '#/components/schemas/A'}]}]}
`))
	openapitest.RequireNoErrorDiags(t, diags)

	u, ok := typeByName(doc, "S").(*ir.Union)
	require.True(t, ok, "%T", typeByName(doc, "S"))
	assert.Len(t, u.Variants, 1, "the string branch is no variant")
	reports := diagsAtPointer(diags, diag.DegradedConstruct, "/components/schemas/S")
	require.Len(t, reports, 1, "%+v", diags)
	assert.Contains(t, reports[0].Message, "(allOf/0/oneOf/0: string)")
}

// TestOneOf_ADeadBranchIsReadForTheNullTheBodyAdmits pins a branch admitting
// no object beside a body that may admit null: it lives where it admits null
// too, and is dead where it does not. Beside a body writing `nullable: true`
// where its dialect defines no such keyword, the nullable-inference policy
// reads the body as one that may admit null, so the branch admitting null
// lives, and with the policy off the body keeps null out, so it is dead.
func TestOneOf_ADeadBranchIsReadForTheNullTheBodyAdmits(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpecVer("3.1.0", `    A: {type: object, properties: {a: {type: string}}}
    Typed:
      type: [object, 'null']
      oneOf: [{$ref: '#/components/schemas/A'}, {type: string}, {type: [string, 'null']}]
    Flagged:
      type: object
      nullable: true
      oneOf: [{$ref: '#/components/schemas/A'}, {type: [string, 'null']}]
`)
	for _, off := range []bool{false, true} {
		doc, diags := schema.LowerSpecWith(t, spec, lowering.Policy{NullableInference: lowering.NullableInference{Disabled: off}})
		openapitest.RequireNoErrorDiags(t, diags)

		typed, ok := typeByName(doc, "Typed").(*ir.Union)
		require.True(t, ok)
		assert.Len(t, typed.Variants, 2, "the string or null lives beside a body admitting null, policy off %t", off)
		assert.Equal(t, []string{"openapi:oneOf/1"}, slices.Sorted(maps.Keys(typed.Unmodeled)), "the string alone is dead, policy off %t", off)

		flagged, ok := typeByName(doc, "Flagged").(*ir.Union)
		require.True(t, ok)
		want := map[bool]int{false: 2, true: 1}[off]
		assert.Len(t, flagged.Variants, want, "policy off %t", off)
		assert.Len(t, diagsAtPointer(diags, diag.DegradedConstruct, "/components/schemas/Flagged"), 2-want,
			"a warning where the branch is dead, policy off %t", off)
	}
}
