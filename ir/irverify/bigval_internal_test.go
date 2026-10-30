package irverify

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irtest"
)

// TestCheckBigVals_VisitingCostsNoAllocation holds the check's cost for a
// numeric literal in canonical form at nothing, whatever its parts: it asks
// ir.NewBigVal for the canonical form, which hands such a literal back as it
// is, where rendering the canonical form to compare costs a string per
// literal. Both carriers are reached, a Value's Num and a bound held by
// pointer. It is not run in parallel: AllocsPerRun measures the process.
func TestCheckBigVals_VisitingCostsNoAllocation(t *testing.T) {
	bound := func(s ir.BigVal) *ir.BigVal { return &s }
	nums := []ir.BigVal{"0", "-0", "42", "-1.5", "2.5E-3", "1e+10", "12345678901234567890.12345678901234567890"}
	values := make([]ir.Value, 0, len(nums))
	for _, num := range nums {
		values = append(values, ir.Value{Kind: ir.ValueNumber, Num: num})
	}
	doc := &ir.Document{Types: ir.TypeRegistry{"t/x/S": &ir.Scalar{
		ID:          "t/x/S",
		Examples:    []ir.Example{{Value: &ir.Value{Kind: ir.ValueList, List: values}}},
		Constraints: &ir.Constraints{Min: bound("-0.5"), Max: bound("1e400"), MultipleOf: bound("0.25")},
	}}}
	check := checkBigVals(doc)
	assert.Empty(t, irtest.AllocatingVisits(doc, check))
	assert.Empty(t, check.report(declarations{}), "every literal is a canonical decimal")
}
