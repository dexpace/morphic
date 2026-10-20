// Package mod is the root of the tree laid out like the module, which is no
// part of the framework.
package mod

import "github.com/dexpace/morphic/ir"

// Root builds a registry, and converts an ID outside any compiler.
func Root(name string) (ir.TypeRegistry, ir.TypeID) {
	return ir.TypeRegistry{}, ir.TypeID(name) // want "registrywrites: builds a type registry (a map from ir.TypeID to ir.TypeDef) outside ir and compilers/compile; go through compile.Types"
}
