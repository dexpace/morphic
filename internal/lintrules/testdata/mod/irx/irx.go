// Package irx is named like ir with more after it, which puts it outside the
// framework. It is no compiler, so converting an ID draws nothing.
package irx

import "github.com/dexpace/morphic/ir"

// Edge builds a registry and fills a name channel.
func Edge(name string) (ir.TypeRegistry, ir.TypeID, ir.Naming) {
	reg := ir.TypeRegistry{}                                // want "registrywrites: builds a type registry (a map from ir.TypeID to ir.TypeDef) outside ir and compilers/compile; go through compile.Types"
	return reg, ir.TypeID(name), ir.Naming{Canonical: name} // want "namechannels: ir.Naming.Canonical is filled by something other than a call into ir or compilers/compile; use compile.NamingFor, compile.NamingHint or ir.CanonicalWords"
}
