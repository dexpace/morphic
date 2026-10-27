package load

import (
	"context"

	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/budget"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
	"github.com/dexpace/morphic/compilers/openapi/internal/sourceindex"
	"github.com/dexpace/morphic/ir"
)

// budgetRefusal builds the diagnostic that refuses a source for crossing one of
// this phase's size budgets, limit, with observed what crossed it. Provenance
// names the source and no position within it: what crossed the budget is the
// document, and no line in it is more answerable than any other.
func budgetRefusal(srcIndex int, format string, observed int, limit budget.Limit) ir.Diagnostic {
	return diag.Newf(ir.SeverityError, diag.BudgetExceeded,
		ir.Provenance{Source: srcIndex}, format, observed, limit.Cap())
}

// OverByteBudget reports whether a source of data is past the byte budget
// limit, and if so the refusal to report at prov. It is exported for
// detection, which reads a source before Load does and is held to the same
// budget with the same words, so a source refused at either reads alike.
//
// The refusal names the budget and not the size: a caller may hand on only the
// first limit+1 bytes of a source past it (compilers.Compiler.SourceLimit), and
// the size of that is not the document's.
func OverByteBudget(prov ir.Provenance, data []byte, limit budget.Limit) (ir.Diagnostic, bool) {
	if limit.Admits(len(data)) {
		return ir.Diagnostic{}, false
	}
	return pastByteBudget(prov, "source", limit), true
}

// pastByteBudget is the refusal of the document what names, a source or an
// overlay, for holding more than the byte budget limit allows.
func pastByteBudget(prov ir.Provenance, what string, limit budget.Limit) ir.Diagnostic {
	return diag.Newf(ir.SeverityError, diag.BudgetExceeded, prov,
		"%s document is past the %d-byte budget", what, limit.Cap())
}

// ReadLimit spells the byte budget limit as a read is held to one in
// compilers.DocumentRequest and compilers.Compiler.SourceLimit: its most, or
// zero for none. A bound of zero bytes, which no budget option resolves to,
// would read as none there; what a read returns is held to limit again after
// it, so a document past it is refused either way.
func ReadLimit(limit budget.Limit) int {
	if most, bounded := limit.Max(); bounded {
		return most
	}
	return 0
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
func nodeCount(ctx context.Context, root *yaml.Node) int {
	return nodeview.Walk{Nodes: maxTreeNodes}.Run(ctx, root).Visited
}
