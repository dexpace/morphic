// Package compilex is named like the framework's package with more after it,
// which puts it outside the framework.
package compilex

import "github.com/dexpace/morphic/ir"

// Edge builds a registry, converts an ID and fills a name channel.
func Edge(name string) (ir.TypeRegistry, ir.TypeID, ir.Naming) {
	reg := ir.TypeRegistry{}                   // want "registrywrites: builds a type registry (a map from ir.TypeID to ir.TypeDef) outside ir and compilers/compile; go through compile.Types"
	id := ir.TypeID(name)                      // want "idderivations: converts a value to ir.TypeID outside compilers/compile; derive the ID through the framework"
	return reg, id, ir.Naming{Canonical: name} // want "namechannels: ir.Naming.Canonical is filled by something other than a call into ir or compilers/compile; use compile.NamingFor, compile.NamingHint or ir.CanonicalWords"
}
