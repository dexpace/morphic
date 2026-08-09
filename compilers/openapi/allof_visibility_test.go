// This file regression-tests GitHub #34: reconcileProperty folded every other
// redeclared property detail (required, docs, default, constraints, ...) but
// never Visibility, so a readOnly/writeOnly declared on the allOf branch that
// redeclares a shared field was silently lost.
package openapi_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
)

// allOfVisibilitySpec is the issue's own reproduction: two allOf branches
// each declare "id" once, and only the second branch — the one that
// redeclares it — marks it readOnly.
const allOfVisibilitySpec = `openapi: 3.1.0
info: {title: t, version: '1'}
paths: {}
components:
  schemas:
    Thing:
      allOf:
        - type: object
          properties:
            id: {type: integer}
        - type: object
          properties:
            id: {type: integer, readOnly: true}
`

// allOfVisibilitySpecReversed is allOfVisibilitySpec with its two branches
// swapped, so readOnly is now the *first* declaration and the plain
// redeclaration comes second.
//
// Before the fix, reconcileProperty kept whichever branch it saw first and
// dropped the other's Visibility entirely. That happened to produce the
// right answer in this direction, purely because the first branch
// (reconcileProperty's dst) was already the readOnly one — so a single-order
// test written only against this spec would have passed on the broken code.
// TestAllOfVisibilityMerge_OrderIndependent compiles both directions and
// diffs them, which is what actually catches that accident rather than
// codifying it.
const allOfVisibilitySpecReversed = `openapi: 3.1.0
info: {title: t, version: '1'}
paths: {}
components:
  schemas:
    Thing:
      allOf:
        - type: object
          properties:
            id: {type: integer, readOnly: true}
        - type: object
          properties:
            id: {type: integer}
`

// allOfVisibilityDisjointSpec composes readOnly against writeOnly on the same
// field: the two branches admit disjoint lifecycle sets, so their
// intersection is empty.
const allOfVisibilityDisjointSpec = `openapi: 3.1.0
info: {title: t, version: '1'}
paths: {}
components:
  schemas:
    Thing:
      allOf:
        - type: object
          properties:
            id: {type: integer, readOnly: true}
        - type: object
          properties:
            id: {type: integer, writeOnly: true}
`

// wantReadOnlyVisibility is the Visibility EffectiveVisibility produces for
// readOnly: visible in every response lifecycle, absent from requests
// (ir-design §5.2).
var wantReadOnlyVisibility = ir.Visibility{
	Only: []ir.Lifecycle{ir.LifecycleRead, ir.LifecycleDelete, ir.LifecycleQuery},
}

// thingIDVisibility compiles spec and returns the merged Thing.id property's
// Visibility, requiring Thing to lower to a Model carrying an "id" property
// first. The diagnostics come back rather than being checked here: each caller
// asserts what its own case expects of them.
func thingIDVisibility(t *testing.T, name, spec string) (ir.Visibility, []ir.Diagnostic) {
	t.Helper()
	doc, diags := compileAnnotationSpec(t, name, spec)
	m, ok := doc.Types[namedID("Thing")].(*ir.Model)
	require.True(t, ok, "Thing must own a Model node")
	p, ok := propByWire(m, "id")
	require.True(t, ok, "Thing must carry a merged \"id\" property")
	return p.Visibility, diags
}

// TestAllOfVisibilityMerge_ReadOnlyOnRedeclarationSurvives is the issue's own
// reproduction: a property left unrestricted by allOf's first branch must
// still pick up readOnly from a later branch's redeclaration, rather than
// silently keep the first, empty Visibility.
func TestAllOfVisibilityMerge_ReadOnlyOnRedeclarationSurvives(t *testing.T) {
	t.Parallel()
	got, diags := thingIDVisibility(t, "allof-visibility", allOfVisibilitySpec)

	assertNoErrorDiags(t, diags)
	assert.Equal(t, wantReadOnlyVisibility, got)
	assert.False(t, openapitest.HasDiag(diags, diag.ConflictingRedecl),
		"a redeclaration adding readOnly to an unrestricted property is not a disagreement")
}

// TestAllOfVisibilityMerge_OrderIndependent compiles the issue's
// reproduction with its two allOf branches in both orders (CLAUDE.md's
// two-order diff for an order-dependent merge) and requires the merged
// property to come out identical either way. See allOfVisibilitySpecReversed
// for why the reversed direction alone would not have caught the original
// defect.
func TestAllOfVisibilityMerge_OrderIndependent(t *testing.T) {
	t.Parallel()
	asReported, diags := thingIDVisibility(t, "allof-visibility-fwd", allOfVisibilitySpec)
	assertNoErrorDiags(t, diags)
	reversed, reversedDiags := thingIDVisibility(t, "allof-visibility-rev", allOfVisibilitySpecReversed)
	assertNoErrorDiags(t, reversedDiags)

	assert.Equal(t, wantReadOnlyVisibility, asReported, "as-reported branch order")
	if diff := cmp.Diff(asReported, reversed); diff != "" {
		t.Errorf("merged Visibility depends on allOf branch order (-as-reported +reversed):\n%s", diff)
	}
}

// TestAllOfVisibilityMerge_DisjointRestrictionsAreInvisibleNotAConflict
// covers the pairing mergeVisibility treats as a genuine empty intersection:
// one branch readOnly, the other writeOnly, so the field satisfies no
// lifecycle both branches admit. That is recorded as None — the IR's
// existing shape for "invisible everywhere" — rather than raised as a
// conflicting-redeclaration: unlike an incompatible-type redeclaration,
// nothing here is arbitrarily discarded to produce it.
//
// Recorded exactly is not the same as recorded silently. The document is still
// warned, under a code of its own, that the composition left "id" in a shape no
// request or response can carry — a merge that produced this and said nothing
// would be indistinguishable, to the author, from one that had understood them.
func TestAllOfVisibilityMerge_DisjointRestrictionsAreInvisibleNotAConflict(t *testing.T) {
	t.Parallel()
	got, diags := thingIDVisibility(t, "allof-visibility-disjoint", allOfVisibilityDisjointSpec)

	assertNoErrorDiags(t, diags)
	assert.Equal(t, ir.Visibility{None: true}, got)
	assert.True(t, openapitest.HasDiag(diags, diag.DisjointVisibility),
		"an allOf that leaves a field visible nowhere is reported, not merged in silence")
	assert.False(t, openapitest.HasDiag(diags, diag.ConflictingRedecl),
		"disjoint readOnly/writeOnly branches intersect to an exact empty set, not an unrepresentable conflict")
}
