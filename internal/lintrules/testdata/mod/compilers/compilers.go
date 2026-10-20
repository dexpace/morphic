// Package compilers stands for the compilers' own root package, which the ID
// rule holds as it holds each compiler.
package compilers

import "github.com/dexpace/morphic/ir"

// Root converts an ID.
func Root(name string) ir.TypeID {
	return ir.TypeID(name) // want "idderivations: converts a value to ir.TypeID outside compilers/compile; derive the ID through the framework"
}

// Number converts to a type parameter no ID satisfies, where no compile.Space
// is in reach to satisfy it either.
func Number[N ~int](i int) N { return N(i) }
