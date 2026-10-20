package graphql

import (
	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/ir"
)

var _ compile.Settlement = (*rewrite)(nil)

// rewrite is a compile.Settlement: Settle may rewrite a node of the registry
// it is handed, and nothing else.
type rewrite struct {
	id    ir.TypeID
	other ir.TypeRegistry
	later func()
}

// Settle rewrites a node of reg, which is its to rewrite, and writes others,
// which are not. It also leaves a write to reg in a function literal, which
// can run after Settle returns.
func (r *rewrite) Settle(reg ir.TypeRegistry) []ir.Diagnostic {
	reg[r.id] = reg[r.id]
	delete(reg, r.id)   // want "registrywrites: writes a type registry outside ir and compilers/compile; go through compile.Types, or rewrite a node in a compile.Settlement"
	r.other[r.id] = nil // want "registrywrites: writes a type registry outside ir and compilers/compile; go through compile.Types, or rewrite a node in a compile.Settlement"
	r.helper(reg)
	r.later = func() {
		reg[r.id] = nil     // want "registrywrites: writes the registry Settle is handed from a function literal, which can run after Settle returns; write it in Settle itself"
		r.other[r.id] = nil // want "registrywrites: writes a type registry outside ir and compilers/compile; go through compile.Types, or rewrite a node in a compile.Settlement"
	}
	(reg)[r.id] = nil
	return nil
}

// helper is handed the registry by Settle, but is not Settle.
func (r *rewrite) helper(reg ir.TypeRegistry) {
	reg[r.id] = nil // want "registrywrites: writes a type registry outside ir and compilers/compile; go through compile.Types, or rewrite a node in a compile.Settlement"
}

// notASettlement has a method named Settle that is no compile.Settlement's.
type notASettlement struct{}

// Settle takes a registry it was not handed by compile.Types.Settle.
func (notASettlement) Settle(reg ir.TypeRegistry) {
	reg["a"] = nil // want "registrywrites: writes a type registry outside ir and compilers/compile; go through compile.Types, or rewrite a node in a compile.Settlement" "idderivations: spells an ir.TypeID as a constant outside compilers/compile; derive the ID through the framework"
}

// Settle is a function, not a method, whatever its name and what it takes.
func Settle(reg ir.TypeRegistry, id ir.TypeID) []ir.Diagnostic {
	reg[id] = nil // want "registrywrites: writes a type registry outside ir and compilers/compile; go through compile.Types, or rewrite a node in a compile.Settlement"
	return nil
}
