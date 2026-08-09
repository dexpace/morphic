package irverify_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irverify"
)

// unionOf returns a document holding one union over the given variant targets,
// plus the leaf each target names, so the document is referentially closed and
// the only thing a violation can be about is the union itself.
func unionOf(targets ...ir.TypeID) *ir.Document {
	u := &ir.Union{TypeCommon: ir.TypeCommon{
		ID:   "t/x/U",
		Name: ir.Naming{Source: "U", Canonical: "u"},
	}}
	types := ir.TypeRegistry{u.ID: u}
	for _, target := range targets {
		u.Variants = append(u.Variants, ir.Variant{Type: ir.TypeRef{Target: target}})
		types[target] = &ir.Scalar{TypeCommon: ir.TypeCommon{
			ID:   target,
			Name: ir.Naming{Source: "Leaf", Canonical: "leaf"},
		}}
	}
	return &ir.Document{Types: types}
}

// unionViolations returns the ir/union-no-variants violations in doc, so a test
// asserting none is not satisfied by an unrelated violation being absent.
func unionViolations(t *testing.T, doc *ir.Document) []irverify.Violation {
	t.Helper()
	var out []irverify.Violation
	for _, v := range irverify.Verify(doc) {
		if v.Code == "ir/union-no-variants" {
			out = append(out, v)
		}
	}
	return out
}

func TestVerify_UnionWithNoVariantsIsAViolation(t *testing.T) {
	t.Parallel()
	got := unionViolations(t, unionOf())
	require.Len(t, got, 1, "a union of nothing is reported exactly once")
	assert.Equal(t, "ir/union-no-variants", got[0].Code)
	assert.Equal(t, "types[t/x/U]", got[0].Path, "the violation locates the union")
	assert.Contains(t, got[0].Message, "no variants")
}

// TestVerify_SingleVariantUnionIsClean pins the first of the two shapes this
// check deliberately passes. `oneOf: [{$ref: X}]` is a legal schema and the
// OpenAPI compiler lowers it to a union of exactly one variant — verified by
// compiling that spec — so reporting it would fire on a faithful lowering.
// Invariant #2 is what forbids the compiler collapsing it in the first place.
func TestVerify_SingleVariantUnionIsClean(t *testing.T) {
	t.Parallel()
	assert.Empty(t, unionViolations(t, unionOf("t/x/Leaf")))
}

// TestVerify_RepeatedVariantTargetIsClean pins the second. `oneOf: [{$ref: X},
// {$ref: X}]` also lowers to exactly what it says, so a repeated target is
// degenerate rather than impossible. A Violation claims a compiler defect, so
// this channel is the wrong one for it whatever is decided about reporting it
// elsewhere.
func TestVerify_RepeatedVariantTargetIsClean(t *testing.T) {
	t.Parallel()
	assert.Empty(t, unionViolations(t, unionOf("t/x/Leaf", "t/x/Leaf")))
}

// TestVerify_NilTypeBesideAUnionDoesNotPanic holds the report-only guarantee at
// this check: a nil registry entry is checkRegistryKeys' to report, and reaching
// past it here would crash Verify on the malformed document it exists to
// describe.
func TestVerify_NilTypeBesideAUnionDoesNotPanic(t *testing.T) {
	t.Parallel()
	doc := unionOf()
	doc.Types["t/x/Nil"] = nil

	var got []irverify.Violation
	require.NotPanics(t, func() { got = irverify.Verify(doc) })
	assert.Len(t, unionViolations(t, doc), 1, "the empty union is still reported")
	assert.Contains(t, codes(got), "ir/nil-type", "the nil entry is reported by its own check")
}

// codes returns the violation codes in vs, for assertions about which checks
// fired rather than about their order.
func codes(vs []irverify.Violation) []string {
	out := make([]string, 0, len(vs))
	for _, v := range vs {
		out = append(out, v.Code)
	}
	return out
}
