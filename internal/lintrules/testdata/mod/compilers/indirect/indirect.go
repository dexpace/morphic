// Package indirect is a compiler that reaches ir only through
// compilers/compile, as a lowering package can: the rules find ir's types
// through that import, so what it builds of them draws all the same.
package indirect

import "github.com/dexpace/morphic/compilers/compile"

// Indirect fills a name channel and spells an ID without importing ir.
func Indirect() bool {
	n := compile.NamingFor("a")
	n.Hint = "b"                                             // want "namechannels: ir.Naming.Hint is filled by something other than a call into ir or compilers/compile; use compile.NamingFor, compile.NamingHint or ir.CanonicalWords"
	return compile.OpID(compile.Space("x"), "/p") == "o/x/p" // want "idderivations: spells an ir.OpID as a constant outside compilers/compile; derive the ID through the framework"
}
