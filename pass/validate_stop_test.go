// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package pass_test

import (
	"context"
	"fmt"
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irtest"
	"github.com/dexpace/morphic/pass"
)

// cutWalk returns a walk of the document as Validate takes it that shows each
// visitor at most n values, as a walk cut short by the limit does, and says it
// stopped once a visitor would have been shown another.
func cutWalk(n int) func(context.Context, any, string, ...ir.Visitor) (bool, error) {
	return func(ctx context.Context, root any, path string, visitors ...ir.Visitor) (bool, error) {
		cut := false
		held := make([]ir.Visitor, len(visitors))
		for i, v := range visitors {
			held[i] = &cutVisitor{inner: v, left: n, cut: &cut}
		}
		truncated, err := ir.WalkValuesContext(ctx, root, path, held...)
		return truncated || cut, err
	}
}

// cutVisitor shows inner the values a walk reaches until left runs out, and
// then none, recording in cut that one went unshown.
type cutVisitor struct {
	inner ir.Visitor
	left  int
	cut   *bool
}

// Visit shows v to the inner visitor while the limit lasts.
func (c *cutVisitor) Visit(v reflect.Value, at ir.WalkPath) bool {
	if c.left == 0 {
		*c.cut = true
		return false
	}
	c.left--
	return c.inner.Visit(v, at)
}

// shownWhole returns how many values a walk of root shows a visitor that prunes
// nothing, the most any visitor of it is shown.
func shownWhole(root any) int {
	var n int
	ir.WalkValues(root, ir.DocumentPath, ir.VisitFunc(func(reflect.Value, ir.WalkPath) bool { n++; return true }))
	return n
}

// TestValidate_AStoppedWalkIsReportedOnce holds Validate to saying that part of
// the document went unchecked once, however many of its checks read operations
// through a walk that stopped: the document walk's stop is reported, and the
// check of nested groups, whose own walk is cut at the same document, is silent
// where that has been said.
func TestValidate_AStoppedWalkIsReportedOnce(t *testing.T) {
	t.Parallel()
	oneway := ir.Operation{ID: "deep", OneWay: true, Responses: []ir.Response{{}}}
	doc := nestGroups(130, oneway)
	require.True(t, ir.ForEachOperation(doc, func(ir.Operation) {}), "130 nested groups are past the depth the group walk reads")

	diags, err := pass.ValidateWith(t.Context(), doc, cutWalk(shownWhole(doc)/2))

	require.NoError(t, err)
	assert.Equal(t, 1, countCode(t, diags, "ir/walk-truncated"), "%v", irtest.DiagCodes(diags))
}

// propPathDocument returns a document whose operation names a property in its
// response body path, which the walk reaches in Services before the model
// declaring it in Types, or one naming nothing when ghost is set.
func propPathDocument(ghost bool) *ir.Document {
	segment := ir.PropID("p/m/a")
	if ghost {
		segment = "p/m/ghost"
	}
	return docWithOperation(propPathOperation([]ir.PropID{segment}))
}

// TestValidate_AStoppedWalkClaimsNoReferenceDangling holds Validate to leaving
// a property reference unjudged when the walk that read the declarations
// stopped: a PropID resolves against the properties the walk read, so a walk
// cut between the reference and its declaration would otherwise call it
// dangling. Every cut point is tried, and the control's dangling reference
// shows the claim is made where the walk read everything.
func TestValidate_AStoppedWalkClaimsNoReferenceDangling(t *testing.T) {
	t.Parallel()
	ghost, err := pass.ValidateWith(t.Context(), propPathDocument(true), ir.WalkValuesContext)
	require.NoError(t, err)
	require.Equal(t, 1, countCode(t, ghost, "ir/dangling-prop-ref"), "the control: a whole walk finds the reference")

	doc := propPathDocument(false)
	whole := shownWhole(doc)
	for n := range whole {
		cut, err := pass.ValidateWith(t.Context(), doc, cutWalk(n))

		require.NoError(t, err)
		assert.Zero(t, countCode(t, cut, "ir/dangling-prop-ref"), "cut at %d of %d values", n, whole)
		assert.Equal(t, 1, countCode(t, cut, "ir/walk-truncated"), "cut at %d of %d values: %s", n, whole,
			fmt.Sprint(irtest.DiagCodes(cut)))
	}
}
