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
	id := r.id
	reg[id] = reg[id]
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

// rebound, addressed, ranged and fed each rebind the registry parameter of
// Settle, so a write through it, before the rebinding or after, may not be one
// to the registry it was handed.
type (
	rebound   struct{ rewrite }
	addressed struct{ rewrite }
	ranged    struct {
		rewrite
		all []ir.TypeRegistry
	}
	fed struct {
		rewrite
		feed chan ir.TypeRegistry
	}
)

func (r *rebound) Settle(reg ir.TypeRegistry) []ir.Diagnostic {
	reg[r.id] = nil // want "registrywrites: writes through the registry parameter Settle rebinds, which may name a registry it was not handed; leave the parameter bound"
	(reg) = r.other
	reg[r.id] = nil // want "registrywrites: writes through the registry parameter Settle rebinds, which may name a registry it was not handed; leave the parameter bound"
	return nil
}

func (a *addressed) Settle(reg ir.TypeRegistry) []ir.Diagnostic {
	_ = &reg
	reg[a.id] = nil // want "registrywrites: writes through the registry parameter Settle rebinds, which may name a registry it was not handed; leave the parameter bound"
	return nil
}

func (r *ranged) Settle(reg ir.TypeRegistry) []ir.Diagnostic {
	for _, reg = range r.all {
		reg[r.id] = nil // want "registrywrites: writes through the registry parameter Settle rebinds, which may name a registry it was not handed; leave the parameter bound"
	}
	return nil
}

func (f *fed) Settle(reg ir.TypeRegistry) []ir.Diagnostic {
	for reg = range f.feed {
		reg[f.id] = nil // want "registrywrites: writes through the registry parameter Settle rebinds, which may name a registry it was not handed; leave the parameter bound"
	}
	return nil
}

// notASettlement has a method named Settle that is no compile.Settlement's.
type notASettlement struct{}

// Settle takes a registry it was not handed by compile.Types.Finish.
func (notASettlement) Settle(reg ir.TypeRegistry) {
	reg["a"] = nil // want "registrywrites: writes a type registry outside ir and compilers/compile; go through compile.Types, or rewrite a node in a compile.Settlement" "idderivations: spells an ir.TypeID as a constant outside compilers/compile; derive the ID through the framework"
}

// Settle is a function, not a method, whatever its name and what it takes.
func Settle(reg ir.TypeRegistry, id ir.TypeID) []ir.Diagnostic {
	reg[id] = nil // want "registrywrites: writes a type registry outside ir and compilers/compile; go through compile.Types, or rewrite a node in a compile.Settlement"
	return nil
}
