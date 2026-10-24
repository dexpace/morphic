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
	diags := []ir.Diagnostic{{Code: "b"}, {Code: "a"}, {Code: "b"}}
	assert.Equal(t, []string{"b", "a", "b"}, irtest.DiagCodes(diags))
	assert.Empty(t, irtest.DiagCodes(nil))
}
