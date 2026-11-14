// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package pass_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irtest"
	"github.com/dexpace/morphic/pass"
)

// stoppedWalk walks the document as Validate does and reports it stopped, as a
// walk cut by the limit does.
func stoppedWalk(ctx context.Context, root any, path string, visitors ...ir.Visitor) (bool, error) {
	if _, err := ir.WalkValuesContext(ctx, root, path, visitors...); err != nil {
		return false, err
	}
	return true, nil
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

	diags, err := pass.ValidateWith(t.Context(), doc, stoppedWalk)

	require.NoError(t, err)
	assert.Equal(t, 1, countCode(t, diags, "ir/walk-truncated"), "%v", irtest.DiagCodes(diags))
}

// TestValidate_AStoppedWalkClaimsNoReferenceDangling holds Validate to leaving a
// reference unjudged when the walk that read the declarations stopped.
func TestValidate_AStoppedWalkClaimsNoReferenceDangling(t *testing.T) {
	t.Parallel()
	doc := docWithOperation(ir.Operation{
		ID:          "op/x/o",
		Idempotency: ir.Idempotency{Kind: ir.IdempotencyToken, TokenParam: "param/x/o/nowhere"},
	})
	whole, err := pass.ValidateWith(t.Context(), doc, ir.WalkValuesContext)
	require.NoError(t, err)
	require.Equal(t, 1, countCode(t, whole, "ir/dangling-param-ref"), "the control: a whole walk finds the reference")

	cut, err := pass.ValidateWith(t.Context(), doc, stoppedWalk)

	require.NoError(t, err)
	assert.Zero(t, countCode(t, cut, "ir/dangling-param-ref"))
	assert.Equal(t, 1, countCode(t, cut, "ir/walk-truncated"), fmt.Sprint(irtest.DiagCodes(cut)))
}
