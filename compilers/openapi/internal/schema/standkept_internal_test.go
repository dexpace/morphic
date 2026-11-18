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
	l, _ := LoweredFor(t, openapitest.ComponentSpec("    S: {allOf: [{type: string, minimum: .nan}], minimum: 2}\n"))
	decl, ok := l.Doc.Components.Schemas.Get("S")
	require.True(t, ok)
	s := l.Ctx.Statements().Of(decl).Schema
	comp := allOfBranches(l.Ctx, s, "/components/schemas/S")
	require.Len(t, comp.branches, 1)
	e := comp.branches[0]
	held := &principalKept{apart: []string{"type"}, apartReason: ir.ReasonNoIRHome}
	var p ir.Unmodeled

	diags := keepApart(l.Ctx, l.Types, comp, held, &p, e.schema.Schema, e.pointer, []string{"minimum"}, ir.ReasonDegradedLowering)

	require.Len(t, diags, 1)
	assert.Equal(t, diag.UnpreservableConstruct, diags[0].Code)
	assert.Empty(t, p, "nothing is kept")
	assert.Equal(t, []string{"type"}, held.apart, "nor counted as kept under the entry's key")
}

// TestKeepBuilt_AKeyNoKeywordNamesHeldOtherwiseIsAnInvariantBroken drives the
// state no build produces: an entry a principal's build kept under a key none
// of its keywords names, which the position holds with another value. There is
// no member of the entry's object to keep it as, so it is reported, never
// overwritten.
func TestKeepBuilt_AKeyNoKeywordNamesHeldOtherwiseIsAnInvariantBroken(t *testing.T) {
	t.Parallel()
	l, _ := LoweredFor(t, openapitest.ComponentSpec("    S: {allOf: [{type: string}]}\n"))
	decl, ok := l.Doc.Components.Schemas.Get("S")
	require.True(t, ok)
	comp := allOfBranches(l.Ctx, l.Ctx.Statements().Of(decl).Schema, "/components/schemas/S")
	e := comp.branches[0]
	p := ir.Unmodeled{"openapi:oneOf/0": {Value: ir.RawValue(`1`), Reason: ir.ReasonNoIRHome}}
	built := ir.Unmodeled{"openapi:oneOf/0": {Value: ir.RawValue(`2`), Reason: ir.ReasonNoIRHome}}

	diags := keepBuilt(l.Ctx, comp, &principalKept{}, &p, e.schema.Schema, e.pointer, built)

	assertInternalInvariant(t, diags)
	assert.Equal(t, `1`, string(p["openapi:oneOf/0"].Value), "what the position wrote stands")
}
