package compile

import "github.com/dexpace/morphic/ir"

// NamingFor builds the neutral Naming of a name the source declares: the
// spelling it used, plus the canonical word sequence derived from it.
//
// It is the constructor to reach for wherever a source name becomes an IR name,
// because the pairing is the invariant — a Source with no Canonical leaves an
// emitter to segment the spelling itself, which is the casing decision invariant
// 4 moves out of the compilers. A Naming that carries only a Hint is a different
// case and does not come through here: nothing declared it.
func NamingFor(source string) ir.Naming {
	return ir.Naming{Source: source, Canonical: ir.CanonicalWords(source)}
}
