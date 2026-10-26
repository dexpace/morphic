package load

import (
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
	"github.com/dexpace/morphic/compilers/openapi/internal/sourceindex"
	"github.com/dexpace/morphic/ir"
)

// exceeds reports whether an observed count crosses limit, treating a zero or
// negative limit as unbounded. Every comparison of a count against the byte,
// node or external-document budget in this package goes through it, so none of
// them can read "no budget set" another way.
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
//
// The refusal names the budget and not the size: a caller may hand on only the
// first limit+1 bytes of a source past it (compilers.Compiler.SourceLimit), and
// the size of that is not the document's.
func OverByteBudget(prov ir.Provenance, data []byte, limit int) (ir.Diagnostic, bool) {
	if !exceeds(len(data), limit) {
		return ir.Diagnostic{}, false
	}
	return pastByteBudget(prov, "source", limit), true
}

// pastByteBudget is the refusal of the document what names, a source or an
// overlay, for holding more than the limit-byte budget allows.
func pastByteBudget(prov ir.Provenance, what string, limit int) ir.Diagnostic {
	return diag.Newf(ir.SeverityError, diag.BudgetExceeded, prov,
		"%s document is past the %d-byte budget", what, limit)
}

// maxTreeNodes bounds every walk of a tree the load phase holds. The pre-parse
// refusals refuse a tree past sourceindex.MaxIndexedNodes before any of these
// walks runs over it, so the bound, one more for the document node, cuts no
// tree they are handed.
const maxTreeNodes = sourceindex.MaxIndexedNodes + 1

// nodeCount returns the number of nodes in the parsed tree rooted at root. An
// alias is counted as itself and its edge is never followed, so each node is
// counted once.
//
// sourceindex.Index.Nodes, which scan's alias-amplification bound reads, takes
// the same measure of the document's content, leaving the document node out.
// The node budget does not read it, since refusals builds that index and keeps
// none of it.
func nodeCount(root *yaml.Node) int {
	return nodeview.Walk{Nodes: maxTreeNodes}.Run(root).Visited
}
