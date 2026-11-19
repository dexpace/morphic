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
	doc.Types["type/u"] = &ir.Union{
		ID:       "type/u",
		Variants: []ir.Variant{{Type: ir.TypeRef{Target: "type/m"}}},
		Discriminator: &ir.Discriminator{
			PropertyName: "kind",
			// t/prim/string exists but is not one of the union's variants.
			Mapping: map[string]ir.TypeID{"a": "type/primitive/string"},
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
	base := &ir.Model{ID: "type/base", Abstract: true}
	doc.Types["type/base"] = base
	// Legal subtype via Base.
	doc.Types["type/viaBase"] = &ir.Model{
		ID: "type/viaBase", Base: &ir.TypeRef{Target: "type/base"},
	}
	// Legal subtype via Implements.
	doc.Types["type/viaImpl"] = &ir.Model{
		ID: "type/viaImpl", Implements: []ir.TypeRef{{Target: "type/base"}},
	}
	// Model that is neither a subtype nor implementer of base.
	doc.Types["type/unrelated"] = &ir.Model{ID: "type/unrelated"}

	base.Discriminator = &ir.Discriminator{
		PropertyName: "kind",
		Mapping: map[string]ir.TypeID{
			"a": "type/viaBase",          // valid via Base
			"b": "type/viaImpl",          // valid via Implements
			"c": "type/primitive/string", // not a model -> invalid
			"d": "type/unrelated",        // model but not a subtype -> invalid
		},
	}
	diags := validated(t, doc)
	assert.Equal(t, 2, countCode(t, diags, "pass/discriminator-missing-variant"))
}

// TestValidate_ModelDiscriminatorRoutesToItsConcreteBase pins a route to the
// base holding the discriminator: legal from its Mapping and its Default where
// the base is concrete, as its own instances are a payload, and refused where
// it is Abstract, which no instance is (docs/ir/models.md#discriminator).
func TestValidate_ModelDiscriminatorRoutesToItsConcreteBase(t *testing.T) {
	t.Parallel()
	for _, abstract := range []bool{false, true} {
		doc := validDoc()
		base := &ir.Model{ID: "type/base", Abstract: abstract, Discriminator: &ir.Discriminator{
			PropertyName: "kind", Mapping: map[string]ir.TypeID{"base": "type/base", "sub": "type/sub"}, Default: "type/base",
		}}
		doc.Types["type/base"] = base
		doc.Types["type/sub"] = &ir.Model{ID: "type/sub", Base: &ir.TypeRef{Target: "type/base"}, DiscriminatorValue: "sub"}
		diags := validated(t, doc)

		want := map[bool]int{false: 0, true: 2}[abstract]
		assert.Equal(t, want, countCode(t, diags, "pass/discriminator-missing-variant"), "abstract: %v", abstract)
		assert.Len(t, diags, want, "abstract: %v", abstract)
	}
}

// TestValidate_SubtypeThroughAliasChain pins that a subtype composing an alias
// of the base is still a subtype. A `$ref` carrying siblings composes the
// branch's own node (docs/ir/models.md#composition-entries), so a hierarchy
// written that way names its base one hop away — two hops where the alias
// itself was reached through another, which a single dereference would still
// miss.
//
// The cyclic chain is here because the IR permits one and the walk must
// terminate on it: the alias resolves to no base, so the mapping is reported
// rather than hanging the pass.
func TestValidate_SubtypeThroughAliasChain(t *testing.T) {
	t.Parallel()
	doc := validDoc()
	doc.Types["type/base"] = &ir.Model{ID: "type/base", Abstract: true}
	doc.Types["type/alias"] = &ir.Scalar{
		ID: "type/alias", Base: &ir.TypeRef{Target: "type/aliasOfAlias"},
	}
	doc.Types["type/aliasOfAlias"] = &ir.Scalar{
		ID: "type/aliasOfAlias", Base: &ir.TypeRef{Target: "type/base"},
	}
	doc.Types["type/sub"] = &ir.Model{
		ID: "type/sub", Base: &ir.TypeRef{Target: "type/alias"},
	}
	// A two-node alias cycle: following it must stop, not recurse forever.
	doc.Types["type/loopA"] = &ir.Scalar{
		ID: "type/loopA", Base: &ir.TypeRef{Target: "type/loopB"},
	}
	doc.Types["type/loopB"] = &ir.Scalar{
		ID: "type/loopB", Base: &ir.TypeRef{Target: "type/loopA"},
	}
	doc.Types["type/looped"] = &ir.Model{
		ID: "type/looped", Base: &ir.TypeRef{Target: "type/loopA"},
	}
	doc.Types["type/base"].(*ir.Model).Discriminator = &ir.Discriminator{
		PropertyName: "kind",
		Mapping:      map[string]ir.TypeID{"a": "type/sub", "b": "type/looped"},
	}

	diags := validated(t, doc)
	assert.Equal(t, 1, countCode(t, diags, "pass/discriminator-missing-variant"),
		"only the cyclic alias fails to reach the base: %v", irtest.DiagCodes(diags))
	assert.Contains(t, messageForCode(t, diags, "pass/discriminator-missing-variant"), "type/looped")
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
	doc.Types["type/base"] = &ir.Model{ID: "type/base", Abstract: true}
	doc.Types["type/child"] = &ir.Model{
		ID: "type/child", Base: &ir.TypeRef{Target: "type/base"},
	}
	doc.Types["type/grandchild"] = &ir.Model{
		ID: "type/grandchild", Base: &ir.TypeRef{Target: "type/child"},
	}
	// Conformance reached through an inherited interface: the same distance, on
	// the other composition relation.
	doc.Types["type/viaIface"] = &ir.Model{
		ID: "type/viaIface", Implements: []ir.TypeRef{{Target: "type/child"}},
	}
	doc.Types["type/base"].(*ir.Model).Discriminator = &ir.Discriminator{
		PropertyName: "kind",
		Mapping: map[string]ir.TypeID{
			"child": "type/child",
			"grand": "type/grandchild",
			"iface": "type/viaIface",
			"other": "type/m", // composes nothing: still not a variant.
		},
	}

	assert.Contains(t, messageForCode(t, validated(t, doc), "pass/discriminator-missing-variant"),
		"type/m", "only the model composing nothing is refused")
}

// TestValidate_SubtypeWalkTerminatesOnACompositionCycle pins that walking the
// composition chain terminates. Nothing rejects a model whose Base chain closes
// on itself, so the walk must return and report the mapping it could not
// justify rather than spin — the same obligation the alias walk carries.
func TestValidate_SubtypeWalkTerminatesOnACompositionCycle(t *testing.T) {
	t.Parallel()
	doc := validDoc()
	doc.Types["type/base"] = &ir.Model{
		ID:       "type/base",
		Abstract: true,
		Discriminator: &ir.Discriminator{
			PropertyName: "kind",
			Mapping:      map[string]ir.TypeID{"a": "type/loopA"},
		},
	}
	doc.Types["type/loopA"] = &ir.Model{
		ID: "type/loopA", Base: &ir.TypeRef{Target: "type/loopB"},
	}
	doc.Types["type/loopB"] = &ir.Model{
		ID: "type/loopB", Base: &ir.TypeRef{Target: "type/loopA"},
	}

	assert.Contains(t, messageForCode(t, validated(t, doc), "pass/discriminator-missing-variant"),
		"type/loopA", "a cycle reaches no base, so the mapping is reported")
}

// discriminatedUnion returns a document whose union declares t/m as its only
// variant and maps the wire value "a" onto target.
func discriminatedUnion(target ir.TypeID) *ir.Document {
	doc := validDoc()
	doc.Types["type/u"] = &ir.Union{
		ID:       "type/u",
		Variants: []ir.Variant{{Type: ir.TypeRef{Target: "type/m"}}},
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
	diags := validated(t, discriminatedUnion("type/ghost"))

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
	diags := validated(t, discriminatedUnion("type/primitive/string"))

	assert.Equal(t, 0, countCode(t, diags, "ir/dangling-type-ref"))
	assert.Contains(t, messageForCode(t, diags, "pass/discriminator-missing-variant"),
		"is not a variant of it")
}

// discriminatedModel returns a document whose base model maps the wire value "a"
// onto target, with t/sub composing the base so a legal member exists.
func discriminatedModel(target ir.TypeID) *ir.Document {
	doc := validDoc()
	doc.Types["type/base"] = &ir.Model{
		ID:       "type/base",
		Abstract: true,
		Discriminator: &ir.Discriminator{
			PropertyName: "kind",
			Mapping:      map[string]ir.TypeID{"a": target},
		},
	}
	doc.Types["type/sub"] = &ir.Model{
		ID: "type/sub", Base: &ir.TypeRef{Target: "type/base"},
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
		{"union", "type/m", func(def ir.TypeID) *ir.Document {
			doc := discriminatedUnion("type/m")
			doc.Types["type/u"].(*ir.Union).Discriminator.Default = def
			return doc
		}},
		{"model", "type/sub", func(def ir.TypeID) *ir.Document {
			doc := discriminatedModel("type/sub")
			doc.Types["type/base"].(*ir.Model).Discriminator.Default = def
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
			diags := validated(t, tc.doc("type/primitive/string"))

			assert.Equal(t, 0, countCode(t, diags, "ir/dangling-type-ref"))
			message := messageForCode(t, diags, "pass/discriminator-missing-variant")
			assert.Contains(t, message, "discriminator default on",
				"the diagnostic names the default, not a wire key that does not exist")
			assert.Contains(t, message, "is not a variant of it")
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
			diags := validated(t, tc.doc("type/ghost"))

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
	doc := discriminatedUnion("type/m")
	union, ok := doc.Types["type/u"].(*ir.Union)
	require.True(t, ok)
	for _, key := range []string{"z", "b", "q", "c", "m"} {
		union.Discriminator.Mapping[key] = "type/none"
	}
	messages := func() []string {
		found := withCode(validated(t, doc), "pass/discriminator-missing-variant")
		out := make([]string, 0, len(found))
		for _, diagnostic := range found {
			out = append(out, diagnostic.Message)
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
	doc := discriminatedUnion("type/n")
	doc.Types["type/n"] = &ir.Model{ID: "type/n"}
	doc.Types["type/a"] = &ir.Union{ // sorts before t/u, so it is read first
		ID:            "type/a",
		Variants:      []ir.Variant{{Type: ir.TypeRef{Target: "type/n"}}},
		Discriminator: &ir.Discriminator{PropertyName: "kind", Mapping: map[string]ir.TypeID{"n": "type/n"}},
	}

	found := withCode(validated(t, doc), "pass/discriminator-missing-variant")
	require.Len(t, found, 1, "the union listing type/n routes to it, and the other does not")
	assert.Equal(t, `discriminator mapping "a" on type/u references "type/n", which is not a variant of it`, found[0].Message)
}

// TestValidate_UnionDiscriminatorAsksNothingOfAVariantsTag pins that a union's
// discriminator is held to its routes alone, not to what each variant declares
// of the tag (docs/ir/unions.md#tag-modes): a variant declaring the tag as a
// required string, one declaring none, one leaving it optional and one typing
// it as a number are all clean, as the payload carries the tag.
func TestValidate_UnionDiscriminatorAsksNothingOfAVariantsTag(t *testing.T) {
	t.Parallel()
	doc := validDoc()
	doc.Types["type/primitive/int64"] = &ir.Primitive{ID: "type/primitive/int64", PrimitiveKind: ir.PrimitiveInt64}
	variant := func(id ir.TypeID, tag *ir.Property) *ir.Model {
		model := &ir.Model{ID: id}
		if tag != nil {
			tag.ID = ir.PropertyID("property/" + id + "/kind")
			model.Properties = []ir.Property{*tag}
		}
		return model
	}
	variants := map[ir.TypeID]*ir.Model{
		"type/has":      variant("type/has", &ir.Property{WireName: "kind", Required: true, Type: ir.TypeRef{Target: "type/primitive/string"}}),
		"type/lacks":    variant("type/lacks", nil),
		"type/optional": variant("type/optional", &ir.Property{WireName: "kind", Type: ir.TypeRef{Target: "type/primitive/string"}}),
		"type/number":   variant("type/number", &ir.Property{WireName: "kind", Required: true, Type: ir.TypeRef{Target: "type/primitive/int64"}}),
	}
	union := &ir.Union{ID: "type/pet", Exclusive: true, WireTagged: true, Closed: true, Discriminator: &ir.Discriminator{
		PropertyName: "kind", Mapping: map[string]ir.TypeID{},
	}}
	for _, id := range []ir.TypeID{"type/has", "type/lacks", "type/optional", "type/number"} {
		doc.Types[id] = variants[id]
		union.Variants = append(union.Variants, ir.Variant{Type: ir.TypeRef{Target: id}})
		union.Discriminator.Mapping[string(id)] = id
	}
	doc.Types[union.ID] = union

	assert.Empty(t, validated(t, doc))
}
