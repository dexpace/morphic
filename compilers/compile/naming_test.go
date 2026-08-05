package compile_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/ir"
)

// TestNamingFor_KeepsTheSpellingAndTheWords pins the pairing that makes a Naming
// neutral: the source spelling is preserved verbatim for anyone who needs the
// original, and the words beside it carry no casing an emitter should own.
func TestNamingFor_KeepsTheSpellingAndTheWords(t *testing.T) {
	t.Parallel()
	assert.Equal(t, ir.Naming{Source: "com.example.User", Canonical: "com_example_user"},
		compile.NamingFor("com.example.User"))
	assert.Equal(t, ir.Naming{Source: "***", Canonical: ""}, compile.NamingFor("***"),
		"a spelling with no words still keeps its spelling")
}

// TestNamingFor_EmptySourceIsMintedAName covers the input that used to produce a
// Naming with nothing in any channel — a legal enum value, property key, tag or
// component name in OpenAPI, and an entity an emitter had no way to name
// (GitHub #251). There is no spelling to keep, so the minted name goes in the
// hint channel rather than being fabricated as a Source the spec never wrote.
func TestNamingFor_EmptySourceIsMintedAName(t *testing.T) {
	t.Parallel()
	got := compile.NamingFor("")
	assert.Equal(t, ir.Naming{Hint: "empty"}, got)
	assert.Empty(t, got.Source, "nothing is fabricated into the source channel")
}

// TestNamingHint_KeepsADerivedHint pins the pass-through half: a hint a compiler
// derived from a position is the name, and nothing here rewrites it.
func TestNamingHint_KeepsADerivedHint(t *testing.T) {
	t.Parallel()
	assert.Equal(t, ir.Naming{Hint: "connection_domain"}, compile.NamingHint("connection_domain"))
}

// TestNamingHint_EmptyHintIsMintedAName is the same defect reached through the
// other channel: a hint derived from a position the source left unnamed comes
// out empty, and passing it through leaves the node nameless just as an empty
// source did.
func TestNamingHint_EmptyHintIsMintedAName(t *testing.T) {
	t.Parallel()
	assert.Equal(t, ir.Naming{Hint: "empty"}, compile.NamingHint(""))
}

// TestNaming_MintedNameIsNeutral holds the minted word to the rule every other
// name in the IR is held to (invariant 4): it must be renderable by an emitter
// that expects neutral words, which means the grammar has to leave it unchanged.
func TestNaming_MintedNameIsNeutral(t *testing.T) {
	t.Parallel()
	minted := compile.NamingHint("").Hint
	assert.Equal(t, minted, ir.CanonicalWords(minted),
		"the minted hint is already what the canonical grammar produces")
}
