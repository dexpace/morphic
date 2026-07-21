package graphql

import (
	"fmt"

	"github.com/vektah/gqlparser/v2/ast"

	"github.com/dexpace/morphic/ir"
)

// srcOf returns the Document.Sources index of the source a position belongs to,
// defaulting to 0 for positions without a resolvable source (programmatic or
// prelude nodes never occur here, but the guard keeps provenance total).
func (l *lowerer) srcOf(pos *ast.Position) int {
	if pos == nil || pos.Src == nil {
		return 0
	}
	if i, ok := l.srcIndex[pos.Src]; ok {
		return i
	}
	return 0
}

// nodeProvenance builds a node's provenance from its structural pointer and
// source. The pointer is the stable synthetic path IDs derive from — not
// line:col — so golden snapshots survive reformatting of the SDL.
func (l *lowerer) nodeProvenance(pointer string, pos *ast.Position) ir.Provenance {
	return ir.Provenance{Source: l.srcOf(pos), Pointer: pointer}
}

// positionProvenance builds line:col provenance for a diagnostic from a source
// position. Diagnostics point at the exact offending token, unlike nodes.
func positionProvenance(pos *ast.Position, index map[*ast.Source]int) ir.Provenance {
	if pos == nil {
		return ir.Provenance{}
	}
	src := 0
	if pos.Src != nil {
		if i, ok := index[pos.Src]; ok {
			src = i
		}
	}
	return ir.Provenance{Source: src, Pointer: fmt.Sprintf("%d:%d", pos.Line, pos.Column)}
}
