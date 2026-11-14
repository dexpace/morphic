// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package lowering_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/ir"
)

// TestOperations_ReadsEveryGroupToTheCap pins the operations a service holds:
// those of a group nested to ir.MaxGroupDepth among them, as pointers into the
// service, and none of a group nested deeper.
func TestOperations_ReadsEveryGroupToTheCap(t *testing.T) {
	t.Parallel()
	var chain []ir.OperationGroup
	for level := ir.MaxGroupDepth + 1; level >= 1; level-- {
		op := ir.Operation{ID: ir.OpID(fmt.Sprintf("op/%d", level))}
		chain = []ir.OperationGroup{{Operations: []ir.Operation{op}, Groups: chain}}
	}
	svc := ir.Service{Groups: []ir.OperationGroup{{Operations: []ir.Operation{{ID: "op/top"}}}, {Groups: chain}}}
	ops := lowering.Operations(&svc)

	seen := map[ir.OpID]bool{}
	for _, op := range ops {
		seen[op.ID] = true
	}
	assert.Len(t, ops, ir.MaxGroupDepth+1, "the top group's, and one per level below it to the cap")
	assert.True(t, seen["op/top"])
	assert.True(t, seen[ir.OpID(fmt.Sprintf("op/%d", ir.MaxGroupDepth))], "the group at the cap is read")
	assert.False(t, seen[ir.OpID(fmt.Sprintf("op/%d", ir.MaxGroupDepth+1))], "the group past it is not")
	ops[0].Pagination = &ir.Pagination{}
	assert.NotNil(t, svc.Groups[0].Operations[0].Pagination, "an operation is read in place")
}
