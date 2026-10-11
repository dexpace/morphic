package compile

import "github.com/dexpace/morphic/ir"

// emptyNameHint is the hint minted for an entity the source left unnamed: an
// enum member whose value is "", a property or component keyed by it, a tag
// declared as "". Each is a legal document an emitter still has to render an
// identifier for, and a Naming with every channel empty gives it nothing
// (GitHub #251).
//
// It is one neutral word rather than a positional index, which would say where
// but not what; emitters resolve colliding names (emitter-design.md §4.12).
// Minting it is not an inference under invariant 6: Hint already carries names
// no source wrote, like "variant_0" for a bare oneOf branch.
const emptyNameHint = "empty"

// NamingFor builds the neutral Naming of a name the source declares: the
// spelling it used, plus the canonical word sequence derived from it.
//
// Use it wherever a source name becomes an IR name. The pairing is the
// invariant: a Source with no Canonical leaves an emitter to segment the
// spelling itself, the casing decision invariant 4 moves out of the compilers.
//
// An empty name declares no spelling to pair, so it yields a minted hint
// instead. A spelling of only non-word characters ("***") keeps its Source and
// canonicalizes to no words, a name an emitter can still escape from.
func NamingFor(source string) ir.Naming {
	if source == "" {
		return NamingHint("")
	}
	return ir.Naming{Source: source, Canonical: ir.CanonicalWords(source)}
}

// NamingHint builds the Naming of an entity nothing declared a name for, from
// the context-derived hint an emitter should synthesize one from.
//
// It holds for the Hint channel what NamingFor holds for Canonical: an emitter
// renders an anonymous entity's identifier from its hint, so the hint is
// neutral words too (invariant 4). A hint usually derives from a source
// spelling, such as a component key; passed through raw it would carry that
// casing and punctuation (GitHub #54).
//
// An empty hint, or one with no word rune ("***"), is minted a name here rather
// than at every caller.
func NamingHint(hint string) ir.Naming {
	return ir.Naming{Hint: neutralHint(hint)}
}

// SubHint composes the hint of a node named after its position inside another —
// a list's element, a map's value, a composed variant, an enum branch — out of
// the enclosing node's hint and the role or index that distinguishes it.
//
// Each half is neutralized first, because either can arrive from a source
// spelling. Joining "" and "item" by hand gives "_item", a leading separator
// no grammar produces; neutralized, the child is "empty_item" and
// agrees with the node it hangs off. Two neutral words joined by "_" are
// neutral again, so a composed hint can parent the next one.
func SubHint(parent, suffix string) string {
	return neutralHint(parent) + "_" + neutralHint(suffix)
}

// neutralHint returns the neutral word sequence of hint, or the minted name when
// the position it was derived from names nothing a word can be read out of.
func neutralHint(hint string) string {
	if words := ir.CanonicalWords(hint); words != "" {
		return words
	}
	return emptyNameHint
}
