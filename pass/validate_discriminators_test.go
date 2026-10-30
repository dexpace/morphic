// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package pass_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irtest"
)

func TestValidate_DiscriminatorMissingVariant(t *testing.T) {
	t.Parallel()
	doc := validDoc()
	doc.Types["t/u"] = &ir.Union{
		ID:       "t/u",
		Variants: []ir.Variant{{Type: ir.TypeRef{Target: "t/m"}}},
		Discriminator: &ir.Discriminator{
			PropertyName: "kind",
			// t/prim/string exists but is not one of the union's variants.
			Mapping: map[string]ir.TypeID{"a": "t/prim/string"},
		},
	}
	diags := validated(t, doc)
	require.NotEmpty(t, diags)
	assert.Contains(t, irtest.DiagCodes(diags), "pass/discriminator-missing-variant")
}

// TestValidate_ModelDiscriminator drives checkModelDiscriminator over the ways
// a mapping target can stand to the base: a subtype through Base and one
// through Implements, which are legal, and a non-model target and a model that
// is neither, which are not. TestValidate_SubtypeThroughAliasChain covers a
// base reached through an alias.
func TestValidate_ModelDiscriminator(t *testing.T) {
	t.Parallel()
	doc := validDoc()
	base := &ir.Model{ID: "t/base", Abstract: true}
	doc.Types["t/base"] = base
	// Legal subtype via Base.
	doc.Types["t/viaBase"] = &ir.Model{
		ID: "t/viaBase", Base: &ir.TypeRef{Target: "t/base"},
	}
	// Legal subtype via Implements.
	doc.Types["t/viaImpl"] = &ir.Model{
		ID: "t/viaImpl", Implements: []ir.TypeRef{{Target: "t/base"}},
	}
	// Model that is neither a subtype nor implementer of base.
	doc.Types["t/unrelated"] = &ir.Model{ID: "t/unrelated"}

	base.Discriminator = &ir.Discriminator{
		PropertyName: "kind",
		Mapping: map[string]ir.TypeID{
			"a": "t/viaBase",     // valid via Base
			"b": "t/viaImpl",     // valid via Implements
			"c": "t/prim/string", // not a model -> invalid
			"d": "t/unrelated",   // model but not a subtype -> invalid
		},
	}
	diags := validated(t, doc)
	assert.Equal(t, 2, countCode(t, diags, "pass/discriminator-missing-variant"))
}

// TestValidate_SubtypeThroughAliasChain pins that a subtype composing an alias
// of the base is still a subtype. A `$ref` carrying siblings composes the
// branch's own node (ir-design §4.3), so a hierarchy written that way names its
// base one hop away — two hops where the alias itself was reached through
// another, which a single dereference would still miss.
//
// The cyclic chain is here because the IR permits one and the walk must
// terminate on it: the alias resolves to no base, so the mapping is reported
// rather than hanging the pass.
func TestValidate_SubtypeThroughAliasChain(t *testing.T) {
	t.Parallel()
	doc := validDoc()
	doc.Types["t/base"] = &ir.Model{ID: "t/base", Abstract: true}
	doc.Types["t/alias"] = &ir.Scalar{
		ID: "t/alias", Base: &ir.TypeRef{Target: "t/aliasOfAlias"},
	}
	doc.Types["t/aliasOfAlias"] = &ir.Scalar{
		ID: "t/aliasOfAlias", Base: &ir.TypeRef{Target: "t/base"},
	}
	doc.Types["t/sub"] = &ir.Model{
		ID: "t/sub", Base: &ir.TypeRef{Target: "t/alias"},
	}
	// A two-node alias cycle: following it must stop, not recurse forever.
	doc.Types["t/loopA"] = &ir.Scalar{
		ID: "t/loopA", Base: &ir.TypeRef{Target: "t/loopB"},
	}
	doc.Types["t/loopB"] = &ir.Scalar{
		ID: "t/loopB", Base: &ir.TypeRef{Target: "t/loopA"},
	}
	doc.Types["t/looped"] = &ir.Model{
		ID: "t/looped", Base: &ir.TypeRef{Target: "t/loopA"},
	}
	doc.Types["t/base"].(*ir.Model).Discriminator = &ir.Discriminator{
		PropertyName: "kind",
		Mapping:      map[string]ir.TypeID{"a": "t/sub", "b": "t/looped"},
	}

	diags := validated(t, doc)
	assert.Equal(t, 1, countCode(t, diags, "pass/discriminator-missing-variant"),
		"only the cyclic alias fails to reach the base: %v", irtest.DiagCodes(diags))
	assert.Contains(t, messageForCode(t, diags, "pass/discriminator-missing-variant"), "t/looped")
}

// TestValidate_TransitiveSubtypeIsAVariant pins that a mapping onto a descendant
// further away than one hop is legal. A three-level allOf hierarchy is ordinary
// OpenAPI and the base's discriminator routes every descendant, so reading
// composition one hop deep refused valid IR with a severity error — fatal at the
// CLI, on a document the spec permits.
//
// The unrelated model is the other half: the walk must still refuse a model that
// composes nothing, or the case would pass on one that accepts everything.
func TestValidate_TransitiveSubtypeIsAVariant(t *testing.T) {
	t.Parallel()
	doc := validDoc()
	doc.Types["t/base"] = &ir.Model{ID: "t/base", Abstract: true}
	doc.Types["t/child"] = &ir.Model{
		ID: "t/child", Base: &ir.TypeRef{Target: "t/base"},
	}
	doc.Types["t/grandchild"] = &ir.Model{
		ID: "t/grandchild", Base: &ir.TypeRef{Target: "t/child"},
	}
	// Conformance reached through an inherited interface: the same distance, on
	// the other composition relation.
	doc.Types["t/viaIface"] = &ir.Model{
		ID: "t/viaIface", Implements: []ir.TypeRef{{Target: "t/child"}},
	}
	doc.Types["t/base"].(*ir.Model).Discriminator = &ir.Discriminator{
		PropertyName: "kind",
		Mapping: map[string]ir.TypeID{
			"child": "t/child",
			"grand": "t/grandchild",
			"iface": "t/viaIface",
			"other": "t/m", // composes nothing: still not a variant.
		},
	}

	assert.Contains(t, messageForCode(t, validated(t, doc), "pass/discriminator-missing-variant"),
		"t/m", "only the model composing nothing is refused")
}

// TestValidate_SubtypeWalkTerminatesOnACompositionCycle pins that walking the
// composition chain terminates. Nothing rejects a model whose Base chain closes
// on itself, so the walk must return and report the mapping it could not
// justify rather than spin — the same obligation the alias walk carries.
func TestValidate_SubtypeWalkTerminatesOnACompositionCycle(t *testing.T) {
	t.Parallel()
	doc := validDoc()
	doc.Types["t/base"] = &ir.Model{
		ID:       "t/base",
		Abstract: true,
		Discriminator: &ir.Discriminator{
			PropertyName: "kind",
			Mapping:      map[string]ir.TypeID{"a": "t/loopA"},
		},
	}
	doc.Types["t/loopA"] = &ir.Model{
		ID: "t/loopA", Base: &ir.TypeRef{Target: "t/loopB"},
	}
	doc.Types["t/loopB"] = &ir.Model{
		ID: "t/loopB", Base: &ir.TypeRef{Target: "t/loopA"},
	}

	assert.Contains(t, messageForCode(t, validated(t, doc), "pass/discriminator-missing-variant"),
		"t/loopA", "a cycle reaches no base, so the mapping is reported")
}

// discriminatedUnion returns a document whose union declares t/m as its only
// variant and maps the wire value "a" onto target.
func discriminatedUnion(target ir.TypeID) *ir.Document {
	doc := validDoc()
	doc.Types["t/u"] = &ir.Union{
		ID:       "t/u",
		Variants: []ir.Variant{{Type: ir.TypeRef{Target: "t/m"}}},
		Discriminator: &ir.Discriminator{
			PropertyName: "kind",
			Mapping:      map[string]ir.TypeID{"a": target},
		},
	}
	return doc
}

// TestValidate_DanglingDiscriminatorTargetIsReportedTwice pins the deliberate
// double report. A mapping target no type declares breaks two separate
// guarantees, so both codes fire and each says its own thing: the reference is
// not closed, and the discriminator cannot route that wire value. Collapsing to
// one code would make an emitter reading discriminator completeness subscribe to
// a reference-integrity code to learn about its own defect.
func TestValidate_DanglingDiscriminatorTargetIsReportedTwice(t *testing.T) {
	t.Parallel()
	diags := validated(t, discriminatedUnion("t/ghost"))

	assert.Equal(t, 1, countCode(t, diags, "ir/dangling-type-ref"))
	assert.Contains(t, messageForCode(t, diags, "pass/discriminator-missing-variant"),
		"no type in the document declares",
		"the mapping diagnostic must name the cause it found, not the one it did not")
}

// TestValidate_DeclaredNonVariantIsReportedOnce is the other half: a target that
// resolves leaves the document referentially closed, so only the discriminator's
// own code fires — the double report above tracks the dangling reference, not the
// discriminator check firing twice.
func TestValidate_DeclaredNonVariantIsReportedOnce(t *testing.T) {
	t.Parallel()
	diags := validated(t, discriminatedUnion("t/prim/string"))

	assert.Equal(t, 0, countCode(t, diags, "ir/dangling-type-ref"))
	assert.Contains(t, messageForCode(t, diags, "pass/discriminator-missing-variant"),
		"is not a variant of it")
}

// discriminatedModel returns a document whose base model maps the wire value "a"
// onto target, with t/sub composing the base so a legal member exists.
func discriminatedModel(target ir.TypeID) *ir.Document {
	doc := validDoc()
	doc.Types["t/base"] = &ir.Model{
		ID:       "t/base",
		Abstract: true,
		Discriminator: &ir.Discriminator{
			PropertyName: "kind",
			Mapping:      map[string]ir.TypeID{"a": target},
		},
	}
	doc.Types["t/sub"] = &ir.Model{
		ID: "t/sub", Base: &ir.TypeRef{Target: "t/base"},
	}
	return doc
}

// discriminatorArms pairs each kind that can declare a discriminator with a
// document builder that maps the wire value "a" legally and sets Default to the
// caller's target, plus the one target that is a legal member. Both arms share
// checkMapping, so a claim proven on one is only proven on that one.
func discriminatorArms() []struct {
	name  string
	legal ir.TypeID
	doc   func(def ir.TypeID) *ir.Document
} {
	return []struct {
		name  string
		legal ir.TypeID
		doc   func(def ir.TypeID) *ir.Document
	}{
		{"union", "t/m", func(def ir.TypeID) *ir.Document {
			doc := discriminatedUnion("t/m")
			doc.Types["t/u"].(*ir.Union).Discriminator.Default = def
			return doc
		}},
		{"model", "t/sub", func(def ir.TypeID) *ir.Document {
			doc := discriminatedModel("t/sub")
			doc.Types["t/base"].(*ir.Model).Discriminator.Default = def
			return doc
		}},
	}
}

// TestValidate_DiscriminatorDefaultIsHeldToVariantMembership pins that Default is
// held to the same membership claim a mapped wire value is. The fallback an
// unrecognized tag routes to is the same routing decision every mapped tag makes,
// so a Default outside the discriminated set has an emitter deserializing into a
// type the discriminator does not admit.
//
// The target here resolves, which is the whole point: the reference walk stays
// silent on it, so nothing but the membership claim can catch it.
func TestValidate_DiscriminatorDefaultIsHeldToVariantMembership(t *testing.T) {
	t.Parallel()
	for _, tc := range discriminatorArms() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			diags := validated(t, tc.doc("t/prim/string"))

			assert.Equal(t, 0, countCode(t, diags, "ir/dangling-type-ref"))
			msg := messageForCode(t, diags, "pass/discriminator-missing-variant")
			assert.Contains(t, msg, "discriminator default on",
				"the diagnostic names the default, not a wire key that does not exist")
			assert.Contains(t, msg, "is not a variant of it")
		})
	}
}

// TestValidate_LegalDiscriminatorDefaultIsClean is the silent half: a Default
// naming a legal member reports nothing, and neither does the zero value, which
// is how every discriminator without a declared fallback arrives. A check that
// cannot stay silent is no better than one that cannot fire.
func TestValidate_LegalDiscriminatorDefaultIsClean(t *testing.T) {
	t.Parallel()
	for _, tc := range discriminatorArms() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Empty(t, validated(t, tc.doc(tc.legal)))
			assert.Empty(t, validated(t, tc.doc("")), "no default declared claims nothing")
		})
	}
}

// TestValidate_DanglingDiscriminatorDefaultIsReportedTwice pins that Default
// reaches the same two claims a mapped target does: the reference is not closed,
// and the discriminator cannot route the tag it falls back on.
func TestValidate_DanglingDiscriminatorDefaultIsReportedTwice(t *testing.T) {
	t.Parallel()
	for _, tc := range discriminatorArms() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			diags := validated(t, tc.doc("t/ghost"))

			assert.Equal(t, 1, countCode(t, diags, "ir/dangling-type-ref"))
			assert.Contains(t, messageForCode(t, diags, "pass/discriminator-missing-variant"),
				"no type in the document declares")
		})
	}
}

// TestValidate_MappingDiagnosticsAreDeterministic pins invariant 7 for a
// discriminator's Mapping, whose keys Go ranges in randomized order: they are
// asked in sorted order, so the reports within one discriminator, all placed at
// the union, follow the wire values their messages name.
func TestValidate_MappingDiagnosticsAreDeterministic(t *testing.T) {
	t.Parallel()
	doc := discriminatedUnion("t/m")
	u, ok := doc.Types["t/u"].(*ir.Union)
	require.True(t, ok)
	for _, key := range []string{"z", "b", "q", "c", "m"} {
		u.Discriminator.Mapping[key] = "t/none"
	}
	messages := func() []string {
		found := withCode(validated(t, doc), "pass/discriminator-missing-variant")
		out := make([]string, 0, len(found))
		for _, d := range found {
			out = append(out, d.Message)
		}
		return out
	}
	want := messages()
	require.Len(t, want, 5)
	for i, key := range []string{"b", "c", "m", "q", "z"} {
		assert.Contains(t, want[i], fmt.Sprintf("mapping %q", key))
	}
	for range 8 {
		assert.Equal(t, want, messages())
	}
}

// TestValidate_UnionMappingIsHeldToItsOwnVariants pins that a union's mapping
// answers to its own variants alone. The union read just before it lists the
// target as a variant, so a check carrying what it read of one union into the
// next would let the mapping through.
func TestValidate_UnionMappingIsHeldToItsOwnVariants(t *testing.T) {
	t.Parallel()
	doc := discriminatedUnion("t/n")
	doc.Types["t/n"] = &ir.Model{ID: "t/n"}
	doc.Types["t/a"] = &ir.Union{ // sorts before t/u, so it is read first
		ID:            "t/a",
		Variants:      []ir.Variant{{Type: ir.TypeRef{Target: "t/n"}}},
		Discriminator: &ir.Discriminator{PropertyName: "kind", Mapping: map[string]ir.TypeID{"n": "t/n"}},
	}

	found := withCode(validated(t, doc), "pass/discriminator-missing-variant")
	require.Len(t, found, 1, "the union listing t/n routes to it, and the other does not")
	assert.Equal(t, `discriminator mapping "a" on t/u references "t/n", which is not a variant of it`, found[0].Message)
}
