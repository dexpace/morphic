package compile

import "github.com/dexpace/morphic/ir"

// emptyNameHint is the hint minted for an entity the source left unnamed — an
// enum member whose value is the empty string, a property or component keyed by
// it, a tag declared as "". Every one of those is a legal document that an
// emitter still has to render an identifier for, and a Naming with all three
// channels empty gives it nothing to render (GitHub #251).
//
// One neutral word rather than a positional index: the word says what happened
// where "member_3" would only say where, and emitters already uniquify names
// that collide — "red" and "Red" canonicalize to the same words today. It
// matches what oapi-codegen mints for the same input, so the two agree on the
// name a reader of both would expect.
//
// Minting it is not an inference under invariant 6. Hint is the channel a
// compiler already fills with names no source wrote — "variant_0" for a bare
// oneOf branch, "get_pets" for an operation with no operationId — and this is
// one more of those, not a guess about what the entity means.
const emptyNameHint = "empty"

// NamingFor builds the neutral Naming of a name the source declares: the
// spelling it used, plus the canonical word sequence derived from it.
//
// It is the constructor to reach for wherever a source name becomes an IR name,
// because the pairing is the invariant — a Source with no Canonical leaves an
// emitter to segment the spelling itself, which is the casing decision invariant
// 4 moves out of the compilers.
//
// A source name that is the empty string declares no spelling to pair with, so
// it yields a minted hint instead: the entity exists and has to be nameable. A
// spelling made only of non-word characters ("***") does not come through here
// — it keeps its Source and canonicalizes to no words at all, which is a name an
// emitter can still escape from rather than the absence of one.
func NamingFor(source string) ir.Naming {
	if source == "" {
		return NamingHint("")
	}
	return ir.Naming{Source: source, Canonical: ir.CanonicalWords(source)}
}

// NamingHint builds the Naming of an entity nothing declared a name for, from
// the context-derived hint an emitter should synthesize one from.
//
// It is the second half of the same invariant NamingFor holds, and holds it the
// same way. A hint is the only name an anonymous entity carries, so it is what
// an emitter renders that entity's identifier from — the job Canonical does for
// a declared name — and it is therefore neutral words here too (invariant 4).
// That matters because a hint is nearly always derived from something a source
// *did* spell: a component key, an operationId, a header name, a $ref target.
// Passing those through carried their casing and their punctuation into the one
// channel no rule was holding (GitHub #54).
//
// A position that carries no name of its own derives an empty hint, and a
// spelling with no word rune in it ("***") derives no words; both leave the node
// with no name in any channel, so both are minted one here rather than at every
// caller.
func NamingHint(hint string) ir.Naming {
	return ir.Naming{Hint: neutralHint(hint)}
}

// SubHint composes the hint of a node named after its position inside another —
// a list's element, a map's value, a composed variant, an enum branch — out of
// the enclosing node's hint and the role or index that distinguishes it.
//
// Composing by hand is what NamingHint cannot protect: "" + "_item" is "_item",
// which is non-empty, so the presence rule passes it, and which is a leading
// separator no grammar produces. Neutralizing each half first makes the child
// agree with the node it hangs off, "empty_item" under "empty", rather than
// leaking the emptiness one level down.
//
// Both halves go through the same minting because either can arrive from a
// source spelling: the enclosing position's name, and the $ref target a
// composition branch takes its role from. Two neutral words joined by a single
// "_" are a neutral word sequence again, which is what lets a composed hint be
// fed back in as the parent of the next one.
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
