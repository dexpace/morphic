// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema_test

import (
	"encoding/json/jsontext"
	"fmt"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/compilers/openapi/internal/schema"
	"github.com/dexpace/morphic/ir"
)

func TestOneOf_WithDiscriminator(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpec(`    Cat:
      type: object
      properties:
        petType: {type: string}
    Dog:
      type: object
      properties:
        petType: {type: string}
    Pet:
      oneOf:
        - {$ref: "#/components/schemas/Cat"}
        - {$ref: "#/components/schemas/Dog"}
      discriminator:
        propertyName: petType
        mapping:
          cat: "#/components/schemas/Cat"
`)
	doc, diags := schema.LowerSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	pet, ok := doc.Types[componentID("Pet")].(*ir.Union)
	require.True(t, ok, "Pet should be a union")
	assert.True(t, pet.Exclusive, "oneOf is exclusive")
	assert.True(t, pet.WireTagged, "the discriminator's tag property names the variant (docs/ir/unions.md)")
	require.Len(t, pet.Variants, 2, "both variants present")
	require.NotNil(t, pet.Discriminator)
	assert.Equal(t, "petType", pet.Discriminator.PropertyName)
	assert.Equal(t, map[string]ir.TypeID{"cat": "t/openapi/components/schemas/Cat"},
		pet.Discriminator.Mapping)
}

func TestAnyOf_IsNonExclusiveUnion(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpec(`    U:
      anyOf:
        - {type: string}
        - {type: integer}
`)
	doc, diags := schema.LowerSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	u, ok := doc.Types[componentID("U")].(*ir.Union)
	require.True(t, ok, "U should be a union")
	assert.False(t, u.Exclusive, "anyOf is non-exclusive")
	assert.False(t, u.WireTagged, "with no discriminator, validation infers the variant")
	require.Len(t, u.Variants, 2)
}

func TestOneOf_NullVariantCollapses(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpec(`    S:
      type: object
      properties:
        p:
          oneOf:
            - {type: string}
            - {type: "null"}
`)
	doc, diags := schema.LowerSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	s, ok := doc.Types[componentID("S")].(*ir.Model)
	require.True(t, ok)
	require.Len(t, s.Properties, 1)
	assert.Equal(t, ir.TypeRef{Target: "t/prim/string", Nullable: true}, s.Properties[0].Type)
	for id, def := range doc.Types {
		_, isUnion := def.(*ir.Union)
		assert.False(t, isUnion, "null-variant oneOf must not produce a union node: %s", id)
	}
}

func TestOneOf_ThreeVariantsWithNullStripsNullLiftsNullable(t *testing.T) {
	t.Parallel()
	// A oneOf with two non-null branches plus a null branch stays a Union of the
	// two non-null variants (the null branch is NOT emitted as an `any` variant),
	// and the enclosing ref becomes Nullable.
	spec := openapitest.ComponentSpec(`    S:
      type: object
      properties:
        p:
          oneOf:
            - {type: string}
            - {type: integer}
            - {type: "null"}
`)
	doc, diags := schema.LowerSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	s := doc.Types[componentID("S")].(*ir.Model)
	require.Len(t, s.Properties, 1)
	ref := s.Properties[0].Type
	assert.True(t, ref.Nullable, "the null branch lifts onto the enclosing ref")

	u, ok := doc.Types[ref.Target].(*ir.Union)
	require.True(t, ok, "the non-null branches form a Union")
	require.Len(t, u.Variants, 2, "the null branch is stripped, not emitted as a variant")
	for _, v := range u.Variants {
		assert.NotEqual(t, ir.TypeID("t/prim/any"), v.Type.Target,
			"no variant degraded to any")
	}
}

func TestAnyOf_ThreeVariantsWithNull(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpec(`    N:
      anyOf:
        - {type: string}
        - {type: integer}
        - {type: 'null'}
`)
	doc, diags := schema.LowerSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	u := typeByName(doc, "N").(*ir.Union)
	assert.Len(t, u.Variants, 2, "null branch stripped from variants")
	assert.False(t, u.Exclusive, "anyOf is not exclusive")
}

// TestOneOf_CoDeclaredCompositionDistributes is the regression from
// reference-learnings §B11: an allOf composition co-declared with a oneOf used
// to survive while the union was dropped to Unmodeled. Both must survive, and
// the shape that says so is a Union whose every variant carries the composition
// — `Base ∧ (A | B)` written as `(Base ∧ A) | (Base ∧ B)`.
func TestOneOf_CoDeclaredCompositionDistributes(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpec(`    Base: {type: object, properties: {id: {type: string}}}
    A: {type: object, properties: {a: {type: string}}}
    B: {type: object, properties: {b: {type: string}}}
    Combo:
      allOf: [{$ref: '#/components/schemas/Base'}]
      oneOf:
        - {$ref: '#/components/schemas/A'}
        - {$ref: '#/components/schemas/B'}
`)
	doc, diags := schema.LowerSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)

	u, ok := typeByName(doc, "Combo").(*ir.Union)
	require.True(t, ok, "the union is the schema's value, not a preserved sibling")
	assert.True(t, u.Exclusive, "oneOf stays exclusive")
	require.Len(t, u.Variants, 2)
	assert.Empty(t, u.Unmodeled, "distribution loses nothing, so nothing is kept verbatim")

	for i, branch := range []string{"A", "B"} {
		v := doc.Types[u.Variants[i].Type.Target].(*ir.Model)
		require.NotNil(t, v.Base, "the enclosing allOf's sole $ref stays Base (docs/compilers/openapi.md#allof-classification)")
		assert.Equal(t, componentID("Base"), v.Base.Target)
		require.Len(t, v.Mixins, 1, "the branch joins as a mixin, the composition already having a base")
		assert.Equal(t, componentID(branch), v.Mixins[0].Target)
		assert.Equal(t, ir.CanonicalWords(branch), u.Variants[i].Name.Hint,
			"the variant is named after its branch, in the neutral words every name channel carries")
	}
	assert.Equal(t, 1, openapitest.CountDiagsAt(diags, diag.CompositionLowering, ir.SeverityInfo),
		"the reshaping is reported once; got %+v", diags)
}

// TestAnyOf_CoDeclaredPropertiesDistributeAsBase covers the other classification
// arm: with no enclosing composition to take Base, the branch takes it, and an
// anyOf stays non-exclusive through distribution.
func TestAnyOf_CoDeclaredPropertiesDistributeAsBase(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpec(`    A: {type: object, properties: {a: {type: string}}}
    B: {type: object, properties: {b: {type: string}}}
    Combo:
      type: object
      required: [kind]
      additionalProperties: false
      properties: {kind: {type: string}}
      anyOf:
        - {$ref: '#/components/schemas/A'}
        - {$ref: '#/components/schemas/B'}
`)
	doc, diags := schema.LowerSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)

	u, ok := typeByName(doc, "Combo").(*ir.Union)
	require.True(t, ok)
	assert.False(t, u.Exclusive, "anyOf stays non-exclusive")
	require.Len(t, u.Variants, 2)

	v := doc.Types[u.Variants[0].Type.Target].(*ir.Model)
	require.NotNil(t, v.Base, "with nothing composed yet, the branch becomes Base")
	assert.Equal(t, componentID("A"), v.Base.Target)
	assert.Empty(t, v.Mixins)
	require.Len(t, v.Properties, 1, "the body's own properties ride on every variant")
	assert.Equal(t, "kind", v.Properties[0].WireName)
	assert.True(t, v.Properties[0].Required, "so does its required list")
	assert.Equal(t, ir.AdditionalClosed, v.Additional, "and its openness")
}

// TestOneOf_CoDeclaredVariantNotStolenByRefToBranch is the regression for the
// pointer collision: the composed variants used to be interned at the branch
// pointers, which a $ref elsewhere in the document denotes too. Whichever
// lowered first took the pointer, so an unrelated reference could leave one
// variant a bare alias of its branch while its siblings carried the body — the
// disagreement docs/ir/models.md#composition-beside-a-union forbids, decided by
// declaration order.
func TestOneOf_CoDeclaredVariantNotStolenByRefToBranch(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name          string
		branch        int
		outsiderFirst bool
	}{
		{"outsider declared first", 0, true},
		{"outsider declared last", 0, false},
		{"ref to the second branch", 1, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, diags := schema.LowerSpec(t, comboRefStealSpec(tc.branch, tc.outsiderFirst))
			openapitest.RequireNoErrorDiags(t, diags)

			u, ok := typeByName(doc, "Combo").(*ir.Union)
			require.True(t, ok)
			require.Len(t, u.Variants, 2)
			for i, branch := range []string{"A", "B"} {
				v, ok := doc.Types[u.Variants[i].Type.Target].(*ir.Model)
				require.True(t, ok, "variant %d keeps the composed body", i)
				require.NotNil(t, v.Base)
				assert.Equal(t, componentID("Base"), v.Base.Target)
				require.Len(t, v.Mixins, 1)
				assert.Equal(t, componentID(branch), v.Mixins[0].Target)
			}

			// The outside reference gets the branch schema, which is what its
			// pointer names — never the conjunction the variant stands for.
			outsider, ok := typeByName(doc, "Outsider").(*ir.Model)
			require.True(t, ok)
			require.Len(t, outsider.Properties, 1)
			branchNode, ok := doc.Types[outsider.Properties[0].Type.Target].(*ir.Scalar)
			require.True(t, ok, "the branch pointer hoists an alias of the branch's target")
			require.NotNil(t, branchNode.Base)
			assert.Equal(t, componentID([]string{"A", "B"}[tc.branch]), branchNode.Base.Target)
		})
	}
}

// TestOneOf_CoDeclaredDistributionIsOrderIndependent states the property over
// the whole compiled document: the same components in two declaration orders
// compile to the same IR. Each case permutes a site the distribution shares
// with something outside it: an outside $ref aimed at either union branch, and
// the discriminated base the variants take their tag from.
//
// These cases permute only components that emit no diagnostics, since
// diagnostics are appended in traversal order and so differ in order even when
// they match. The registry is also compared on its own, with nothing excluded.
func TestOneOf_CoDeclaredDistributionIsOrderIndependent(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		// spec writes the document with the permuted component declared first.
		spec func(first bool) string
	}{
		{"outside ref at the first branch", func(f bool) string { return comboRefStealSpec(0, f) }},
		{"outside ref at the second branch", func(f bool) string { return comboRefStealSpec(1, f) }},
		{"discriminated base", comboDiscriminatedSpec},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			first, diags := parseFull(t, tc.spec(true))
			openapitest.RequireNoErrorDiags(t, diags)
			last, diags := parseFull(t, tc.spec(false))
			openapitest.RequireNoErrorDiags(t, diags)

			assert.Empty(t, cmp.Diff(first, last, orderInvariantIR()...),
				"declaring the permuted component before or after the union must not change the IR")
			assert.Empty(t, cmp.Diff(first.Types, last.Types), "nor any name hint in the registry")
		})
	}
}

// TestOneOf_CoDeclaredVariantCarriesDiscriminatorValue pins the tag the variants
// inherit. The enclosing schema is an allOf subtype of a discriminated base, so
// every variant is written on the wire with that subtype's tag — and the tag
// comes from the base's mapping entry for the *enclosing* schema, since that is
// the node the mapping names.
func TestOneOf_CoDeclaredVariantCarriesDiscriminatorValue(t *testing.T) {
	t.Parallel()
	doc, diags := schema.LowerSpec(t, comboDiscriminatedSpec(true))
	openapitest.RequireNoErrorDiags(t, diags)

	u, ok := typeByName(doc, "Combo").(*ir.Union)
	require.True(t, ok)
	require.Len(t, u.Variants, 2)
	for i := range u.Variants {
		v, ok := doc.Types[u.Variants[i].Type.Target].(*ir.Model)
		require.True(t, ok)
		assert.Equal(t, "combo", v.DiscriminatorValue,
			"variant %d carries the mapping key naming the enclosing schema, not its own hint", i)
	}
}

// TestOneOf_InlineCoDeclaredDistributionHasNoImplicitDiscriminatorValue extends
// GitHub #517 to buildComposedVariant, the union-branch caller of
// subtypeDiscriminatorValue: the enclosing schema of this co-declared oneOf is
// hoisted inline (a property, not a component), so it has no schema name and
// its distributed variants get no implicit tag either — the same rule the allOf
// case pins, at the other of the two call sites the fix touches. Before the fix
// both variants took the property key "Combo" as their tag, a value the document
// never assigned and one a component named Combo would also answer to.
func TestOneOf_InlineCoDeclaredDistributionHasNoImplicitDiscriminatorValue(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpec(`    Pet:
      type: object
      required: [kind]
      properties:
        kind: {type: string}
      discriminator:
        propertyName: kind
    A: {type: object, properties: {a: {type: string}}}
    B: {type: object, properties: {b: {type: string}}}
    Kennel:
      type: object
      properties:
        Combo:
          allOf:
            - $ref: '#/components/schemas/Pet'
          oneOf:
            - $ref: '#/components/schemas/A'
            - $ref: '#/components/schemas/B'
`)
	doc, diags := schema.LowerSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)

	u, ok := doc.Types[ir.TypeID("t/anon/components/schemas/Kennel/properties/Combo")].(*ir.Union)
	require.True(t, ok, "the co-declared oneOf hoists a union at the inline property")
	require.Len(t, u.Variants, 2)
	for i := range u.Variants {
		v, ok := doc.Types[u.Variants[i].Type.Target].(*ir.Model)
		require.True(t, ok)
		assert.Empty(t, v.DiscriminatorValue,
			"variant %d has no mapping entry and its enclosing schema has no name to fall back to", i)
	}
}

// TestOneOf_CoDeclaredAdditionalPropsHintNamesTheBody covers the naming of the
// one node every variant shares. additionalProperties is declared once, on the
// enclosing schema, and lowers at its pointer — so the first variant to reach it
// must not stamp its own branch name on the shared node.
func TestOneOf_CoDeclaredAdditionalPropsHintNamesTheBody(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpec(`    A: {type: object, properties: {a: {type: string}}}
    B: {type: object, properties: {b: {type: string}}}
    Combo:
      type: object
      properties: {kind: {type: string}}
      additionalProperties: {type: object, properties: {v: {type: string}}}
      oneOf: [{$ref: '#/components/schemas/A'}, {$ref: '#/components/schemas/B'}]
`)
	doc, diags := schema.LowerSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)

	u := typeByName(doc, "Combo").(*ir.Union)
	v, ok := doc.Types[u.Variants[0].Type.Target].(*ir.Model)
	require.True(t, ok)
	require.NotNil(t, v.AdditionalProps)
	assert.Equal(t, "combo_value", doc.Types[v.AdditionalProps.Value.Target].Common().Name.Hint)
}

// TestOneOf_CoDeclaredNonModelBranchIsCarriedAsWritten pins what
// docs/ir/models.md#composition-beside-a-union says Base/Mixins may name. JSON
// Schema lets a composition conjoin any schema, so a branch can reference a
// scalar, an enum or another union; the referent is carried as written, because
// narrowing it would mean dropping a branch the source declared. fillAllOf
// classifies an allOf entry the same way.
func TestOneOf_CoDeclaredNonModelBranchIsCarriedAsWritten(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpec(`    Scalar: {type: string, minLength: 2}
    Choice: {enum: [a, b]}
    Combo:
      type: object
      properties: {kind: {type: string}}
      oneOf: [{$ref: '#/components/schemas/Scalar'}, {$ref: '#/components/schemas/Choice'}]
`)
	doc, diags := schema.LowerSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)

	u, ok := typeByName(doc, "Combo").(*ir.Union)
	require.True(t, ok)
	require.Len(t, u.Variants, 2)
	for i, want := range []string{"Scalar", "Choice"} {
		v, ok := doc.Types[u.Variants[i].Type.Target].(*ir.Model)
		require.True(t, ok)
		require.NotNil(t, v.Base, "variant %d conjoins its branch", i)
		assert.Equal(t, componentID(want), v.Base.Target)
	}
	assert.IsType(t, &ir.Scalar{}, doc.Types[componentID("Scalar")])
	assert.IsType(t, &ir.Enum{}, doc.Types[componentID("Choice")])
}

// TestOneOf_CoDeclaredUnresolvableBranchIsNotDistributed covers the branches
// that are $ref-shaped but name nothing this compilation can resolve. Each
// would otherwise half-distribute — the variant carrying the body while its
// branch vanished — so all of them keep the verbatim lowering, which loses
// nothing, and each still reports the broken reference.
//
// It drives the lowering alone (lowerSpecOnly), so the report checked is the
// lowering's own, at the branch pointer, with the reason the bindings give. A
// full compile
// keeps the load phase's report of the same $ref instead; see
// TestCompile_AResolutionFailureIsReportedOnceAtItsRef.
func TestOneOf_CoDeclaredUnresolvableBranchIsNotDistributed(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpec(`    A: {type: object, properties: {a: {type: string}}}
    Undeclared:
      type: object
      properties: {kind: {type: string}}
      oneOf: [{$ref: '#/components/schemas/Ghost'}, {$ref: '#/components/schemas/A'}]
    CrossDocument:
      type: object
      properties: {kind: {type: string}}
      oneOf: [{$ref: 'other.yaml#/components/schemas/A'}]
    EmptyRef:
      type: object
      properties: {kind: {type: string}}
      oneOf: [{$ref: ''}]
    NoSuchPointer:
      type: object
      properties: {kind: {type: string}}
      oneOf: [{$ref: '#/components/schemas/A/properties/missing'}]
`)
	doc, diags := lowerSpecOnly(t, spec)

	for name, why := range map[string]string{
		"Undeclared":    `"#/components/schemas/Ghost" is unresolved: it names nothing in the document it points into`,
		"CrossDocument": `"other.yaml#/components/schemas/A" is unresolved: it names another document, and external references are not read`,
		"EmptyRef":      `"" is unresolved: it names a position that holds no schema`,
		"NoSuchPointer": `"#/components/schemas/A/properties/missing" is unresolved: it names nothing in the document it points into`,
	} {
		m, ok := typeByName(doc, name).(*ir.Model)
		require.True(t, ok, "%s keeps its structural body rather than becoming a union", name)
		entry, ok := m.Unmodeled["openapi:oneOf"]
		require.True(t, ok, "%s keeps every branch verbatim", name)
		assert.Equal(t, ir.ReasonDegradedLowering, entry.Reason, "%s", name)
		assert.Contains(t,
			openapitest.DiagMessageAt(t, diags, diag.DegradedConstruct, ir.SeverityInfo, "/components/schemas/"+name),
			"names no referent this compilation resolves", name)
		// One error per offending branch, at the branch itself.
		assert.Equal(t, "union branch $ref "+why+"; the branch is kept verbatim",
			openapitest.DiagMessageAt(t, diags, diag.UnresolvedRef, ir.SeverityError, "/components/schemas/"+name+"/oneOf/0"),
			name)
	}
	for _, d := range diags {
		assert.NotEqual(t, jsontext.Pointer("/components/schemas/Undeclared/oneOf/1"), d.Provenance.Pointer,
			"the branch beside the broken one resolves, so nothing is reported against it")
	}
}

// TestOneOf_CoDeclaredNotDistributedReasons pins the five shapes distribution
// declines, each kept verbatim under ReasonDegradedLowering with its own reason
// in the diagnostic (docs/compilers/openapi.md#union-beside-structure).
// Half-distributing any of them would leave a Union whose variants disagree
// about whether they carry the body. The messages are asserted, not just the
// code: the reasons are the only thing telling the otherwise identical
// diagnostics apart.
func TestOneOf_CoDeclaredNotDistributedReasons(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpec(`    A: {type: object, properties: {a: {type: string}}}
    NotAModel:
      const: fixed
      oneOf: [{$ref: '#/components/schemas/A'}]
    InlineBranch:
      type: object
      properties: {kind: {type: string}}
      oneOf: [{$ref: '#/components/schemas/A'}, {type: string}]
    NullBranch:
      properties: {kind: {type: string}}
      oneOf: [{$ref: '#/components/schemas/A'}, {type: 'null'}]
    BothCombinators:
      type: object
      properties: {kind: {type: string}}
      oneOf: [{$ref: '#/components/schemas/A'}]
      anyOf: [{$ref: '#/components/schemas/A'}]
    Discriminated:
      type: object
      properties: {kind: {type: string}}
      discriminator: {propertyName: kind}
      oneOf: [{$ref: '#/components/schemas/A'}]
`)
	doc, diags := schema.LowerSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)

	for name, reason := range map[string]string{
		"NotAModel":       "the body is not a model, so it carries no composition to distribute into",
		"InlineBranch":    "a branch is written inline, so it names no referent to conjoin the body with",
		"NullBranch":      "a branch is written inline, so it names no referent to conjoin the body with",
		"BothCombinators": "oneOf and anyOf are both declared, so distributing either would drop the other",
		"Discriminated":   "a declared discriminator binds the branches by name, which distributing them would break",
	} {
		entry, ok := typeByName(doc, name).Common().Unmodeled["openapi:oneOf"]
		require.True(t, ok, "%s keeps its union verbatim", name)
		assert.Equal(t, ir.ReasonDegradedLowering, entry.Reason, "%s", name)
		assert.Contains(t,
			openapitest.DiagMessageAt(t, diags, diag.DegradedConstruct, ir.SeverityInfo, "/components/schemas/"+name),
			reason, name)
	}
	both := typeByName(doc, "BothCombinators").(*ir.Model)
	_, ok := both.Unmodeled["openapi:anyOf"]
	assert.True(t, ok, "the second combinator is kept too, which is why neither is distributed")
	nullBranch := typeByName(doc, "NullBranch").(*ir.Model)
	assert.Contains(t, string(nullBranch.Unmodeled["openapi:oneOf"].Value), `"null"`,
		"a null branch is written inline, so it blocks distribution rather than lifting to Nullable")
	// The sixth is settling's, once the registry is whole: Discriminated's
	// discriminator routes to no subtype of its model, so it stays beside the
	// union it was written for.
	assert.Contains(t,
		openapitest.DiagMessageAt(t, diags, diag.DegradedConstruct, ir.SeverityInfo,
			"/components/schemas/Discriminated/discriminator"),
		"not a subtype of the model holding it")
	assert.Equal(t, 6, openapitest.CountDiagsAt(diags, diag.DegradedConstruct, ir.SeverityInfo),
		"each declined shape is reported once, and the kept discriminator once; got %+v", diags)
}

// restatingTargets declares the components the restating cases' $refs name.
const restatingTargets = `    Target: {type: object, properties: {t: {type: string}}}
    Other: {type: object, properties: {o: {type: string}}}
`

// TestOneOf_CoDeclaredRestatingBranchDeclaresNoShape holds a branch that only
// repeats the body to the bare form: the union is kept under validation_only,
// and nothing reports a degradation.
func TestOneOf_CoDeclaredRestatingBranchDeclaresNoShape(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct{ version, schema, keyword string }{
		"SameType": {"3.1.0", `      type: object
      properties: {a: {type: string}, b: {type: string}}
      oneOf:
        - {required: [a], properties: {a: {type: string}}}
        - {required: [b], properties: {b: {type: string}}}
`, "openapi:oneOf"},
		"AnyOf": {"3.1.0", `      type: object
      properties: {a: {type: string}, b: {type: string}}
      anyOf:
        - {required: [a], properties: {a: {type: string}}}
        - {required: [b], properties: {b: {type: string}}}
`, "openapi:anyOf"},
		"BodyDocumentsTheProperty": {"3.1.0", `      type: object
      properties: {a: {type: string, description: d, title: t}}
      oneOf: [{required: [a], properties: {a: {type: string}}}]
`, "openapi:oneOf"},
		"BodyNarrowsTheProperty": {"3.1.0", `      type: object
      properties: {a: {type: string, format: uri, minLength: 1}}
      oneOf: [{required: [a], properties: {a: {type: string}}}]
`, "openapi:oneOf"},
		"RestatesTheFormat": {"3.1.0", `      type: object
      properties: {a: {type: string, format: uri, description: d}}
      oneOf: [{required: [a], properties: {a: {type: string, format: uri}}}]
`, "openapi:oneOf"},
		"RestatesTheRef": {"3.1.0", `      type: object
      properties: {a: {$ref: '#/components/schemas/Target', description: d}}
      oneOf: [{required: [a], properties: {a: {$ref: '#/components/schemas/Target'}}}]
`, "openapi:oneOf"},
		"WrittenAlike": {"3.1.0", `      type: object
      properties: {a: {type: array, items: {type: string}, description: d}}
      oneOf: [{required: [a], properties: {a: {type: array, items: {type: string}, description: d}}}]
`, "openapi:oneOf"},
		"RestatesTheNullableType": {"3.0.3", `      type: object
      properties: {a: {type: string, nullable: true, description: d}}
      oneOf: [{required: [a], properties: {a: {type: string, nullable: true}}}]
`, "openapi:oneOf"},
		"RestatesTheBodysType": {"3.1.0", `      type: object
      properties: {a: {type: string}}
      oneOf: [{type: object, required: [a], properties: {a: {type: string}}}]
`, "openapi:oneOf"},
		"RestatesTheBodysTypeAlone": {"3.1.0", `      type: object
      properties: {a: {type: string}}
      oneOf: [{type: object, required: [a]}]
`, "openapi:oneOf"},
		"AliasedProperty": {"3.1.0", `      type: object
      properties: {a: &str {type: string}}
      oneOf: [{required: [a], properties: {a: *str}}]
`, "openapi:oneOf"},
		"MergedKeyword": {"3.1.0", `      type: object
      properties: {a: {type: string, description: d}}
      oneOf: [{required: [a], properties: {a: {<<: {type: string}}}}]
`, "openapi:oneOf"},
		"BranchDocumentsItself": {"3.1.0", `      type: object
      properties: {a: {type: string}}
      oneOf: [{title: t, description: d, required: [a], properties: {a: {type: string}}}]
`, "openapi:oneOf"},
		"NullBranchBeside": {"3.1.0", `      type: object
      properties: {a: {type: string}}
      oneOf: [{type: 'null'}, {required: [a], properties: {a: {type: string}}}]
`, "openapi:oneOf"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			spec := openapitest.ComponentSpecVer(tc.version, restatingTargets+"    S:\n"+tc.schema)
			doc, diags := schema.LowerSpec(t, spec)
			openapitest.RequireNoErrorDiags(t, diags)

			m, ok := typeByName(doc, "S").(*ir.Model)
			require.True(t, ok, "the body lowers")
			entry, ok := m.Unmodeled[tc.keyword]
			require.True(t, ok, "the union is kept verbatim")
			assert.Equal(t, ir.ReasonValidationOnly, entry.Reason)
			assert.Equal(t, 1, openapitest.CountDiagsAt(diags, diag.ValidationOnlyKeyword, ir.SeverityInfo))
			assert.Zero(t, openapitest.CountDiagsAt(diags, diag.DegradedConstruct, ir.SeverityInfo),
				"%+v", diags)
		})
	}
}

// TestOneOf_CoDeclaredBranchSayingMoreDeclaresShape holds a branch that writes
// anything the body does not to the degraded lowering, and pins each way of
// doing so a looser comparison would read as a restatement.
func TestOneOf_CoDeclaredBranchSayingMoreDeclaresShape(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct{ version, schema string }{
		"OtherType": {"3.1.0", `      type: object
      properties: {a: {type: string}}
      oneOf: [{required: [a], properties: {a: {type: integer}}}]
`},
		"UndeclaredProperty": {"3.1.0", `      type: object
      properties: {a: {type: string}}
      oneOf: [{required: [a], properties: {z: {type: string}}}]
`},
		"LaterPropertyDiffers": {"3.1.0", `      type: object
      properties: {a: {type: string}, b: {type: string}}
      oneOf: [{required: [a], properties: {a: {type: string}, b: {type: integer}}}]
`},
		"LaterBranchDeclaresShape": {"3.1.0", `      type: object
      properties: {a: {type: string}}
      oneOf:
        - {required: [a], properties: {a: {type: string}}}
        - {properties: {z: {type: string}}}
`},
		"AnyOfBranchDeclaresShape": {"3.1.0", `      type: object
      properties: {a: {type: string}}
      anyOf: [{required: [a], properties: {z: {type: string}}}]
`},
		"ConstBesideTheType": {"3.1.0", `      type: object
      properties: {kind: {type: string}}
      oneOf: [{properties: {kind: {type: string, const: x}}}]
`},
		"EnumBesideTheType": {"3.1.0", `      type: object
      properties: {a: {type: string}}
      oneOf: [{required: [a], properties: {a: {type: string, enum: [x, y]}}}]
`},
		"ConstraintBesideTheType": {"3.1.0", `      type: object
      properties: {a: {type: string}}
      oneOf: [{required: [a], properties: {a: {type: string, minLength: 1}}}]
`},
		"KeywordTheModelHasNoFieldFor": {"3.1.0", `      type: object
      properties: {a: {type: object}}
      oneOf: [{required: [a], properties: {a: {type: object, dependentRequired: {p: [q]}}}}]
`},
		"NestedKeywordTheModelHasNoFieldFor": {"3.1.0", `      type: object
      properties: {a: {type: object, properties: {x: {type: object}}}}
      oneOf:
        - required: [a]
          properties: {a: {type: object, properties: {x: {type: object, dependentRequired: {p: [q]}}}}}
`},
		"KeywordReadingItsSiblings": {"3.1.0", `      type: object
      properties: {a: {type: object, properties: {x: {type: string}}, additionalProperties: false}}
      oneOf: [{required: [a], properties: {a: {additionalProperties: false}}}]
`},
		"WidensToNull": {"3.1.0", `      type: object
      properties: {a: {type: string}}
      oneOf: [{required: [a], properties: {a: {type: [string, 'null']}}}]
`},
		"BodyIsNullable": {"3.0.3", `      type: object
      properties: {a: {type: string, nullable: true}}
      oneOf: [{required: [a], properties: {a: {type: string}}}]
`},
		"BranchIsNullable": {"3.0.3", `      type: object
      properties: {a: {type: string}}
      oneOf: [{required: [a], properties: {a: {type: string, nullable: true}}}]
`},
		"RefAgainstType": {"3.1.0", `      type: object
      properties: {a: {$ref: '#/components/schemas/Target'}}
      oneOf: [{required: [a], properties: {a: {type: object}}}]
`},
		"OtherRef": {"3.1.0", `      type: object
      properties: {a: {$ref: '#/components/schemas/Target'}}
      oneOf: [{required: [a], properties: {a: {$ref: '#/components/schemas/Other'}}}]
`},
		"RefTheBranchLeavesOut": {"3.0.3", `      type: object
      properties: {a: {$ref: '#/components/schemas/Target', type: string}}
      oneOf: [{required: [a], properties: {a: {type: string}}}]
`},
		"BooleanInTheBranch": {"3.1.0", `      type: object
      properties: {a: {type: string}}
      oneOf: [{required: [a], properties: {a: true}}]
`},
		"BooleanInTheBody": {"3.1.0", `      type: object
      properties: {a: true}
      oneOf: [{required: [a], properties: {a: {type: string}}}]
`},
		"CatchAllBesideRestatement": {"3.1.0", `      type: object
      properties: {a: {type: string}}
      oneOf: [{required: [a], properties: {a: {type: string}}, additionalProperties: false}]
`},
		"NestedUnionBesideRestatement": {"3.1.0", `      type: object
      properties: {a: {type: string}}
      oneOf: [{required: [a], properties: {a: {type: string}}, oneOf: [{required: [a]}]}]
`},
		"BranchTypeWiderThanTheBodys": {"3.1.0", `      type: object
      properties: {a: {type: string}}
      oneOf: [{type: [object, 'null'], required: [a]}]
`},
		"BodyWritesNoType": {"3.1.0", `      properties: {a: {type: string}}
      oneOf: [{type: object, required: [a]}]
`},
		"BranchTypeBesideNullableBody": {"3.0.3", `      type: object
      nullable: true
      properties: {a: {type: string}}
      oneOf: [{type: object, required: [a]}]
`},
		"BooleanInBoth": {"3.1.0", `      type: object
      properties: {a: true}
      oneOf: [{required: [a], properties: {a: true}}]
`},
		"AnyOfBesideRestatement": {"3.1.0", `      type: object
      properties: {a: {type: string}}
      oneOf: [{required: [a], properties: {a: {type: string}}, anyOf: [{required: [a]}]}]
`},
		"AnyOfBesideBareBranch": {"3.1.0", `      type: object
      properties: {a: {type: string}}
      oneOf: [{required: [a], anyOf: [{required: [a]}]}]
`},
		"MinPropertiesBesideRestatement": {"3.1.0", `      type: object
      properties: {a: {type: string}}
      oneOf: [{required: [a], properties: {a: {type: string}}, minProperties: 2}]
`},
		"DynamicRefBesideRestatement": {"3.1.0", `      type: object
      properties: {a: {type: string}}
      oneOf: [{properties: {a: {type: string}}, $dynamicRef: '#node'}]
`},
		"PropertyTheBodyOnlyComposes": {"3.1.0", `      allOf: [{$ref: '#/components/schemas/Target'}]
      oneOf: [{required: [t], properties: {t: {type: string}}}]
`},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			spec := openapitest.ComponentSpecVer(tc.version, restatingTargets+"    S:\n"+tc.schema)
			doc, diags := schema.LowerSpec(t, spec)
			openapitest.RequireNoErrorDiags(t, diags)

			entry, ok := typeByName(doc, "S").Common().Unmodeled["openapi:"+unionKeyword(tc.schema)]
			require.True(t, ok, "the union is kept verbatim")
			assert.Equal(t, ir.ReasonDegradedLowering, entry.Reason)
			assert.Contains(t,
				openapitest.DiagMessageAt(t, diags, diag.DegradedConstruct, ir.SeverityInfo, "/components/schemas/S"),
				"a branch is written inline")
		})
	}
}

// TestOneOf_CoDeclaredBranchSayingMoreBesideANonModel holds a branch that
// repeats the body's type and writes a keyword the body does not to the
// degraded lowering, where the body is no model to distribute into.
func TestOneOf_CoDeclaredBranchSayingMoreBesideANonModel(t *testing.T) {
	t.Parallel()
	for name, schemaText := range map[string]string{
		"ItemsBesideTheType": `      type: array
      oneOf:
        - {type: array, items: {$ref: '#/components/schemas/Target'}}
        - {type: array, items: {$ref: '#/components/schemas/Other'}}
`,
		"FormatBesideTheType": `      type: string
      oneOf: [{type: string, format: date}, {type: string, format: uri}]
`,
		"MinLengthBesideTheType": `      type: string
      oneOf: [{type: string, minLength: 1}, {type: string, maxLength: 3}]
`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			spec := openapitest.ComponentSpecVer("3.1.0", restatingTargets+"    S:\n"+schemaText)
			doc, diags := schema.LowerSpec(t, spec)
			openapitest.RequireNoErrorDiags(t, diags)

			entry, ok := typeByName(doc, "S").Common().Unmodeled["openapi:oneOf"]
			require.True(t, ok, "the union is kept verbatim")
			assert.Equal(t, ir.ReasonDegradedLowering, entry.Reason)
			assert.Contains(t,
				openapitest.DiagMessageAt(t, diags, diag.DegradedConstruct, ir.SeverityInfo, "/components/schemas/S"),
				"the body is not a model")
		})
	}
}

// unionKeyword names the combinator the written schema's body uses, one at the
// body's own indentation and not one nested in a branch.
func unionKeyword(written string) string {
	if strings.Contains("\n"+written, "\n      anyOf:") {
		return "anyOf"
	}
	return "oneOf"
}

// comboRefStealSpec is the co-declared union of Base with A|B, plus one $ref
// aimed at a union branch's own pointer. The two orders of the same four
// components must lower identically: the branch pointer denotes the branch
// schema in both, and the variants are Morphic's own nodes in both.
func comboRefStealSpec(branch int, outsiderFirst bool) string {
	outsider := fmt.Sprintf(
		"    Outsider: {type: object, properties: {x: {$ref: '#/components/schemas/Combo/oneOf/%d'}}}\n", branch)
	combo := `    Base: {type: object, properties: {id: {type: string}}}
    A: {type: object, properties: {a: {type: string}}}
    B: {type: object, properties: {b: {type: string}}}
    Combo:
      allOf: [{$ref: '#/components/schemas/Base'}]
      oneOf: [{$ref: '#/components/schemas/A'}, {$ref: '#/components/schemas/B'}]
`
	if outsiderFirst {
		return openapitest.ComponentSpec(outsider + combo)
	}
	return openapitest.ComponentSpec(combo + outsider)
}

// comboDiscriminatedSpec is the co-declared union of a discriminated Base with
// A|B. Every variant is written on the wire with the enclosing schema's tag,
// which the base's mapping supplies, so the base and the schemas naming it are
// permutable against each other — components lower in source order.
func comboDiscriminatedSpec(baseFirst bool) string {
	base := `    Base:
      type: object
      properties: {kind: {type: string}}
      discriminator:
        propertyName: kind
        mapping: {combo: '#/components/schemas/Combo'}
`
	rest := `    A: {type: object, properties: {a: {type: string}}}
    B: {type: object, properties: {b: {type: string}}}
    Combo:
      allOf: [{$ref: '#/components/schemas/Base'}]
      oneOf: [{$ref: '#/components/schemas/A'}, {$ref: '#/components/schemas/B'}]
`
	if baseFirst {
		return openapitest.ComponentSpec(base + rest)
	}
	return openapitest.ComponentSpec(rest + base)
}
