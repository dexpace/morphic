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

// TestNamingHint_NeutralizesTheContextItWasDerivedFrom is what makes the hint
// channel a name rather than a transcription. A hint is derived from a position
// the source named — a component key, an operationId, a header name, a $ref
// target — so it arrives carrying whatever casing and punctuation that source
// used, and it is the only name an anonymous type has for an emitter to render
// (GitHub #54).
func TestNamingHint_NeutralizesTheContextItWasDerivedFrom(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ hint, want string }{
		{"connectionDomain", "connection_domain"},
		{"OrderBody", "order_body"},
		{"X-Report-List", "x_report_list"},
		{"rollout.state", "rollout_state"},
		{"get /pets/{petId}", "get_pets_pet_id"},
		{"***", "empty"}, // no words to render, so the same minting an empty hint gets
	} {
		assert.Equal(t, ir.Naming{Hint: tc.want}, compile.NamingHint(tc.hint), "hint %q", tc.hint)
	}
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

// TestSubHint_MintsTheEnclosingHint is the case NamingHint alone does not reach:
// a child named after its position inside another. Concatenating onto an empty
// enclosing hint yields "_item", which is non-empty — so the presence rule
// passes it — while being a shape no grammar produces. Minting first is what
// keeps the child agreeing with the node it hangs off.
func TestSubHint_MintsTheEnclosingHint(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "empty_item", compile.SubHint("", "item"))
	assert.Equal(t, "widget_item", compile.SubHint("widget", "item"),
		"an enclosing hint that is really there is untouched")
}

// TestSubHint_NeutralizesBothHalves pins that a composed hint is neutral however
// its two halves were spelled. Either can arrive from a source name — the
// enclosing position's, and the $ref target a union branch takes its role from —
// so neutralizing only the whole would still be a word sequence whichever half
// carried the casing, and neutralizing only the parent would not.
func TestSubHint_NeutralizesBothHalves(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ parent, suffix, want string }{
		{"Combo", "Alt", "combo_alt"},
		{"X-Report", "item", "x_report_item"},
		{"widget", "0", "widget_0"},
		{"widget", "***", "widget_empty"},
	} {
		assert.Equal(t, tc.want, compile.SubHint(tc.parent, tc.suffix), "%q + %q", tc.parent, tc.suffix)
	}
}

// TestSubHint_IsItselfANeutralHint is the composition property the callers rely
// on: a composed hint is fed back in as the parent of the next one, so joining
// two neutral halves has to produce something the grammar leaves alone. A join
// that introduced a boundary — a doubled or trailing separator, a letter run
// against a digit — would compound one level down.
func TestSubHint_IsItselfANeutralHint(t *testing.T) {
	t.Parallel()
	nested := compile.SubHint(compile.SubHint("Combo_A", "2"), "item")
	assert.Equal(t, "combo_a_2_item", nested)
	assert.Equal(t, nested, ir.CanonicalWords(nested), "the grammar leaves a composed hint alone")
}
