package pass

import "github.com/dexpace/morphic/ir"

// diag builds a Diagnostic located in the document rather than in a source.
// node is an IR-space location — a stable ID, or a path through the document's
// own fields as the reference walk spells it — so it is Provenance.Node, and
// Source is ir.NoSource to stop renderers fabricating a file location for it.
func diag(sev ir.Severity, code, message, node string) ir.Diagnostic {
	return ir.NewDiagnostic(sev, code, message, ir.Provenance{Source: ir.NoSource, Node: node})
}
