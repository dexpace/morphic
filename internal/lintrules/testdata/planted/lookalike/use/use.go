// Package use converts strings to types named like the IR's and the
// framework's, from packages that are neither, which draws nothing: the rules
// read what a name resolves to, not how it is spelled.
package use

import (
	"github.com/dexpace/morphic/internal/lintrules/testdata/planted/lookalike/compile"
	"github.com/dexpace/morphic/internal/lintrules/testdata/planted/lookalike/ir"
)

// IDs builds both from name.
func IDs(name string) (ir.OpID, compile.Space) {
	return ir.OpID(name), compile.Space(name)
}
