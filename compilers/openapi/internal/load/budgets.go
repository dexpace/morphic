package load

import (
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/ir"
)

// exceeds reports whether an observed count crosses limit, treating a zero or
// negative limit as unbounded. Both budgets are read through it so that "no
// budget set" cannot be spelled two ways.
func exceeds(observed, limit int) bool {
	return limit > 0 && observed > limit
}

// budgetRefusal builds the diagnostic that refuses a source for crossing one of
// this phase's size budgets. Provenance names the source and no position within
// it: what crossed the budget is the document, and no line in it is more
// answerable than any other.
func budgetRefusal(srcIndex int, format string, observed, limit int) ir.Diagnostic {
	return diag.Newf(ir.SeverityError, diag.BudgetExceeded,
		ir.Provenance{Source: srcIndex}, format, observed, limit)
}

// OverByteBudget reports whether a source of data is past the byte budget limit,
// zero being none, and if so the refusal to report at prov. It is exported for
// detection, which reads a source before Load does and is held to the same
// budget with the same words, so a source refused at either reads alike.
func OverByteBudget(prov ir.Provenance, data []byte, limit int) (ir.Diagnostic, bool) {
	if !exceeds(len(data), limit) {
		return ir.Diagnostic{}, false
	}
	return diag.Newf(ir.SeverityError, diag.BudgetExceeded, prov,
		"source document is %d bytes, past the %d-byte budget", len(data), limit), true
}

// nodeCount returns the number of nodes in the parsed tree rooted at root. An
// alias node has empty Content in yaml.v3 and its edge is never followed, so
// the iterative walk visits each node once.
//
// The alias-amplification budget in scan takes the same measure of the same
// tree. The two walks are deliberately not shared: ten lines over a third-party
// node type are not worth a dependency between two packages with different
// jobs.
func nodeCount(root *yaml.Node) int {
	if root == nil {
		return 0
	}
	count := 0
	stack := []*yaml.Node{root}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		count++
		stack = append(stack, n.Content...)
	}
	return count
}
