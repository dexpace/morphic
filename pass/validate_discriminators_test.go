package pass_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/pass"
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
	diags := pass.Validate(doc)
	require.NotEmpty(t, diags)
	assert.Contains(t, codes(diags), "pass/discriminator-missing-variant")
}

// TestValidate_ModelDiscriminator drives checkModelDiscriminator and every
// ir.IsSubtype branch: subtype via Base, subtype via Implements, a non-model target,
// and a model that is neither, plus the clean (valid) mapping path.
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
	diags := pass.Validate(doc)
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

	diags := pass.Validate(doc)
	assert.Equal(t, 1, countCode(t, diags, "pass/discriminator-missing-variant"),
		"only the cyclic alias fails to reach the base: %v", codes(diags))
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

	assert.Contains(t, messageForCode(t, pass.Validate(doc), "pass/discriminator-missing-variant"),
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

	assert.Contains(t, messageForCode(t, pass.Validate(doc), "pass/discriminator-missing-variant"),
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
	diags := pass.Validate(discriminatedUnion("t/ghost"))

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
	diags := pass.Validate(discriminatedUnion("t/prim/string"))

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
			diags := pass.Validate(tc.doc("t/prim/string"))

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
			assert.Empty(t, pass.Validate(tc.doc(tc.legal)))
			assert.Empty(t, pass.Validate(tc.doc("")), "no default declared claims nothing")
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
			diags := pass.Validate(tc.doc("t/ghost"))

			assert.Equal(t, 1, countCode(t, diags, "ir/dangling-type-ref"))
			assert.Contains(t, messageForCode(t, diags, "pass/discriminator-missing-variant"),
				"no type in the document declares")
		})
	}
}
