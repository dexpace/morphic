// Package irx is named like ir with more after it, which puts it outside the
// framework. It is no compiler, so converting an ID draws nothing.
package irx

import "github.com/dexpace/morphic/ir"

// Services builds a slice of the service tree and a message registry.
func Services() ([]ir.Service, map[ir.MessageID]ir.Message) {
	return []ir.Service{}, map[ir.MessageID]ir.Message{} // want "servicetreewrites: builds a slice of the service tree (of ir.Service, ir.OperationGroup or ir.Operation) outside ir and the compilers; read a document's and edit it through an ir.Editor" "registrywrites: builds a message registry (a map from ir.MessageID to ir.Message) outside ir and the compilers; edit a document's registry through an ir.Editor"
}

// Edge builds a registry and fills a name channel.
func Edge(name string) (ir.TypeRegistry, ir.TypeID, ir.Naming) {
	reg := ir.TypeRegistry{}                                // want "registrywrites: builds a type registry (a map from ir.TypeID to ir.TypeDef) outside ir and compilers/compile; edit a document's registry through an ir.Editor"
	return reg, ir.TypeID(name), ir.Naming{Canonical: name} // want "namechannels: ir.Naming.Canonical is filled by something other than a call into ir or compilers/compile; use compile.NamingFor, compile.NamingHint or ir.CanonicalWords"
}
