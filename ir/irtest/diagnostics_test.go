// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package irtest_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irtest"
)

// TestDiagCodes_ListsEachCodeInReportOrder pins that repeats are kept and the
// order is the report's, not a sorted one.
func TestDiagCodes_ListsEachCodeInReportOrder(t *testing.T) {
	t.Parallel()
	diags := []ir.Diagnostic{{Code: "c"}, {Code: "a"}, {Code: "b"}, {Code: "a"}}
	assert.Equal(t, []string{"c", "a", "b", "a"}, irtest.DiagCodes(diags))
	assert.Empty(t, irtest.DiagCodes(nil))
}
