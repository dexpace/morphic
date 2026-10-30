// Package ir stands for the IR's package, which owns the registry and the
// naming grammar. The ID rule holds compilers only, so converting one here
// draws nothing either.
package ir

import (
	"maps"

	"github.com/dexpace/morphic/ir"
)

// Copy is generic, the framework's, and named like a function of package maps
// that writes, so a registry passed to it draws nothing at the call.
func Copy[M ~map[K]V, K comparable, V any](dst, src M) { maps.Copy(dst, src) }

// Clone is generic, and the framework's, so a registry it returns draws
// nothing at the call.
func Clone[M ~map[K]V, K comparable, V any](m M) M { return m }

// Tree builds and writes the service tree and a channel registry, which the
// framework owns, so it draws nothing.
func Tree(doc *ir.Document, op ir.Operation, id ir.ChannelID) {
	doc.Services = append(doc.Services, ir.Service{Groups: []ir.OperationGroup{{}}})
	doc.Services[0].Groups[0].Operations = []ir.Operation{op}
	doc.Channels = map[ir.ChannelID]ir.Channel{id: {}}
}

// Owner builds and names.
func Owner(name string) (ir.TypeRegistry, ir.Naming) {
	reg := ir.TypeRegistry{ir.TypeID(name): nil}
	return reg, ir.Naming{Canonical: name, Hint: name}
}
