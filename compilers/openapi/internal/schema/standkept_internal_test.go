// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
)

// TestKeepApart_AnObjectThatCannotRenderIsReportedAndClaimsNothing drives the
// state its callers cannot produce, each handing it keywords it has rendered
// one by one (heldAgainst): an object no JSON holds. It is reported lost,
// nothing is announced as kept, and what readers before kept there stands.
func TestKeepApart_AnObjectThatCannotRenderIsReportedAndClaimsNothing(t *testing.T) {
	t.Parallel()
	lowerer, _ := LoweredFor(t, openapitest.ComponentSpec("    S: {allOf: [{type: string, minimum: .nan}], minimum: 2}\n"))
	decl, ok := lowerer.Doc.Components.Schemas.Get("S")
	require.True(t, ok)
	schema := lowerer.Ctx.Statements().Of(decl).Schema
	comp := allOfBranches(lowerer.Ctx, schema, "/components/schemas/S")
	require.Len(t, comp.branches, 1)
	allOfBranch := comp.branches[0]
	held := &principalKept{apart: []string{"type"}, apartReason: ir.ReasonNoIRHome}
	var unmodeled ir.Unmodeled

	diags := keepApart(lowerer.Ctx, lowerer.Types, comp, held, &unmodeled, allOfBranch.schema.Schema, allOfBranch.pointer, []string{"minimum"}, ir.ReasonDegradedLowering)

	require.Len(t, diags, 1)
	assert.Equal(t, diag.UnpreservableConstruct, diags[0].Code)
	assert.Empty(t, unmodeled, "nothing is kept")
	assert.Equal(t, []string{"type"}, held.apart, "nor counted as kept under the entry's key")
}

// TestKeepBuilt_AKeyNoKeywordNamesHeldOtherwiseIsAnInvariantBroken drives the
// state no build produces: an entry a principal's build kept under a key none
// of its keywords names, which the position holds with another value. There is
// no member of the entry's object to keep it as, so it is reported, never
// overwritten.
func TestKeepBuilt_AKeyNoKeywordNamesHeldOtherwiseIsAnInvariantBroken(t *testing.T) {
	t.Parallel()
	lowerer, _ := LoweredFor(t, openapitest.ComponentSpec("    S: {allOf: [{type: string}]}\n"))
	decl, ok := lowerer.Doc.Components.Schemas.Get("S")
	require.True(t, ok)
	comp := allOfBranches(lowerer.Ctx, lowerer.Ctx.Statements().Of(decl).Schema, "/components/schemas/S")
	allOfBranch := comp.branches[0]
	unmodeled := ir.Unmodeled{"openapi:oneOf/0": {Value: ir.RawValue(`1`), Reason: ir.ReasonNoIRHome}}
	built := ir.Unmodeled{"openapi:oneOf/0": {Value: ir.RawValue(`2`), Reason: ir.ReasonNoIRHome}}

	diags := keepBuilt(lowerer.Ctx, comp, &principalKept{}, &unmodeled, allOfBranch.schema.Schema, allOfBranch.pointer, built)

	assertInternalInvariant(t, diags)
	assert.Equal(t, `1`, string(unmodeled["openapi:oneOf/0"].Value), "what the position wrote stands")
}

// TestKeptMembers_OnlyAnObjectHasMembers pins the reading of what an entry
// around a principal's level holds under its key: an object's members, each
// as written, a number's spelling with it, and none of anything else.
func TestKeptMembers_OnlyAnObjectHasMembers(t *testing.T) {
	t.Parallel()
	assert.Equal(t, map[string]ir.RawValue{"minimum": ir.RawValue("1.0"), "foo": ir.RawValue(`[1,2]`)},
		keptMembers(ir.RawValue(`{"foo":[1,2],"minimum":1.0}`)))
	assert.Nil(t, keptMembers(ir.RawValue("false")))
}
