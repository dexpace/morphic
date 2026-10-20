// Package ir stands for the IR's package, which owns the registry and the
// naming grammar. The ID rule holds compilers only, so converting one here
// draws nothing either.
package ir

import "github.com/dexpace/morphic/ir"

// Count is generic, and the framework's, so a registry passed to it draws
// nothing at the call.
func Count[M ~map[K]V, K comparable, V any](m M) int { return len(m) }

// Owner builds and names.
func Owner(name string) (ir.TypeRegistry, ir.Naming) {
	reg := ir.TypeRegistry{ir.TypeID(name): nil}
	return reg, ir.Naming{Canonical: name, Hint: name}
}
