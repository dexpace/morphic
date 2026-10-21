// Package plain is a compiler reaching neither ir nor compilers/compile. It
// builds and spells their look-alikes, which draws nothing: the rules read
// what a name resolves to, not how it is spelled.
package plain

import (
	"github.com/dexpace/morphic/internal/lintrules/testdata/mod/lookalike/compile"
	"github.com/dexpace/morphic/internal/lintrules/testdata/mod/lookalike/ir"
)

// Lookalikes builds each from name.
func Lookalikes(name string) (ir.TypeRegistry, ir.OpID, compile.Space, ir.Naming) {
	reg := ir.TypeRegistry{}
	reg[name] = 1
	n := ir.Naming{Canonical: name}
	n.Hint = name
	return reg, ir.OpID(name), compile.Space(name + "x"), n
}
