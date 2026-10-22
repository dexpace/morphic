package lowering_test

import (
	"encoding/json/jsontext"
	"go/types"
	"sync"
	"testing"

	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/overlay"
	"github.com/dexpace/morphic/internal/decls"
	"github.com/dexpace/morphic/ir"
)

// promotionCtx builds a context carrying nothing but the promotion policy.
func promotionCtx(policy lowering.ExtensionPromotions) lowering.Ctx {
	return lowering.New(0, &soa.OpenAPI{}, ir.SourceInfo{}, "", lowering.Limits{}, lowering.StreamingMedia{}, policy, lowering.NullableWidening{}, overlay.Origin{})
}

// vendorExtension is one preserved x-* entry, as ExtensionsFrom writes it.
func vendorExtension(rawJSON string) ir.UnmodeledEntry {
	return ir.UnmodeledEntry{
		Reason:     ir.ReasonVendorExtension,
		Value:      ir.RawValue(rawJSON),
		Provenance: ir.Provenance{Pointer: "/components/schemas/S"},
	}
}

// TestPromoteDeprecation_FillsTheFieldsThePolicyNames pins what each mapping
// writes, one field at a time, because they share a struct and a promotion
// writing the wrong member of it would still look filled. The removal pair is
// why that matters most: a date written into the version field is the defect
// GitHub #417 records, and it reads as a filled Deprecation either way.
func TestPromoteDeprecation_FillsTheFieldsThePolicyNames(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		target lowering.ExtensionTarget
		want   ir.Deprecation
	}{
		{"message", lowering.TargetDeprecationMessage, ir.Deprecation{Message: "why"}},
		{"since", lowering.TargetDeprecationSince, ir.Deprecation{Since: "why"}},
		{"removal version", lowering.TargetDeprecationRemovalVersion, ir.Deprecation{RemovalVersion: "why"}},
		{"removal date", lowering.TargetDeprecationRemovalDate, ir.Deprecation{RemovalDate: "why"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c := promotionCtx(lowering.ExtensionPromotions{
				Targets: map[string]lowering.ExtensionTarget{"x-k": tc.target},
			})
			var dep ir.Deprecation
			var prov ir.Provenance
			diags := c.PromoteDeprecation(ir.Unmodeled{"openapi:x-k": vendorExtension(`"why"`)}, &dep, &prov)

			assert.Empty(t, diags)
			assert.Equal(t, tc.want, dep)
			assert.Equal(t, lowering.ExtensionPromotionHeuristic, prov.Inferred)
		})
	}
}

// TestPromoteDeprecation_LeavesTheEntryItRead is the losslessness half: the
// promotion is a second reading of a preserved entry, never a move, so a
// consumer that disagrees with the guess can still read what the document
// actually wrote.
func TestPromoteDeprecation_LeavesTheEntryItRead(t *testing.T) {
	t.Parallel()
	unmodeled := ir.Unmodeled{"openapi:x-deprecated-reason": vendorExtension(`"why"`)}
	var dep ir.Deprecation
	var prov ir.Provenance
	promotionCtx(lowering.ExtensionPromotions{}).PromoteDeprecation(unmodeled, &dep, &prov)

	entry, kept := unmodeled["openapi:x-deprecated-reason"]
	require.True(t, kept, "the entry survives its own promotion")
	assert.Equal(t, ir.ReasonVendorExtension, entry.Reason)
	assert.JSONEq(t, `"why"`, string(entry.Value))
}

// TestPromoteDeprecation_WritesNothing pins every shape that must leave the
// node exactly as it was. Each is a different reason, and a promotion that
// answered any of them by writing would put a value in the IR that the document
// did not say.
func TestPromoteDeprecation_WritesNothing(t *testing.T) {
	t.Parallel()
	filled := ir.Unmodeled{"openapi:x-deprecated-reason": vendorExtension(`"why"`)}
	tests := []struct {
		name      string
		policy    lowering.ExtensionPromotions
		unmodeled ir.Unmodeled
	}{
		{"promotion disabled", lowering.ExtensionPromotions{Disabled: true}, filled},
		{"no extensions kept", lowering.ExtensionPromotions{}, nil},
		{"a key the document did not write", lowering.ExtensionPromotions{}, ir.Unmodeled{
			"openapi:x-other": vendorExtension(`"why"`),
		}},
		{
			"a target no deprecation field answers to",
			lowering.ExtensionPromotions{Targets: map[string]lowering.ExtensionTarget{
				"x-deprecated-reason": lowering.ExtensionTarget("pagination.items"),
			}},
			filled,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var dep ir.Deprecation
			var prov ir.Provenance
			diags := promotionCtx(tc.policy).PromoteDeprecation(tc.unmodeled, &dep, &prov)
			assert.Empty(t, diags)
			assert.Equal(t, ir.Deprecation{}, dep)
			assert.Empty(t, prov.Inferred, "nothing was inferred, so nothing is marked")
		})
	}
}

// TestPromoteDeprecation_UndeprecatedNodeIsTheWholeAnswer pins the nil cases,
// which are not an oversight to guard against but the ordinary shape: a node
// that never said it was deprecated has no Deprecation to fill, and its
// extension stays where it is.
func TestPromoteDeprecation_UndeprecatedNodeIsTheWholeAnswer(t *testing.T) {
	t.Parallel()
	c := promotionCtx(lowering.ExtensionPromotions{})
	unmodeled := ir.Unmodeled{"openapi:x-deprecated-reason": vendorExtension(`"why"`)}
	assert.Empty(t, c.PromoteDeprecation(unmodeled, nil, &ir.Provenance{}))

	var dep ir.Deprecation
	assert.Empty(t, c.PromoteDeprecation(unmodeled, &dep, nil))
	assert.Equal(t, ir.Deprecation{}, dep, "with nowhere to record the guess, none is made")
}

// TestPromoteDeprecation_ValueThatIsNotTextIsReported pins the one thing a
// promotion reports. Every Deprecation field is prose or a version, so a value
// of another shape means the document uses the key for something else — which
// is a reason to leave the field empty and say so, not to coerce.
func TestPromoteDeprecation_ValueThatIsNotTextIsReported(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, value string
	}{
		{"a number", `7`},
		// JSON null decodes into a plain string as "", so this row is what
		// separates a bare key from an empty string a document wrote on purpose:
		// the first fills nothing and is reported, the second is text.
		{"a bare key", `null`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			unmodeled := ir.Unmodeled{"openapi:x-deprecated-reason": vendorExtension(tc.value)}
			var dep ir.Deprecation
			var prov ir.Provenance
			diags := promotionCtx(lowering.ExtensionPromotions{}).PromoteDeprecation(unmodeled, &dep, &prov)

			require.Len(t, diags, 1)
			assert.Equal(t, ir.SeverityInfo, diags[0].Severity)
			assert.Equal(t, "openapi/degraded-construct", diags[0].Code)
			assert.Equal(t, jsontext.Pointer("/components/schemas/S"), diags[0].Provenance.Pointer,
				"the report names the extension rather than the node holding it")
			assert.Equal(t, ir.Deprecation{}, dep)
			assert.Empty(t, prov.Inferred)
		})
	}
}

// TestPromoteDeprecation_MarksOnceBesideWhateverWasAlreadyThere pins the two
// halves of the marker. A heuristic already recorded must survive, since
// Provenance.Inferred holds one string; and a node annotated twice — which a
// component reached by two references is — must not accumulate the same name
// per reference.
func TestPromoteDeprecation_MarksOnceBesideWhateverWasAlreadyThere(t *testing.T) {
	t.Parallel()
	c := promotionCtx(lowering.ExtensionPromotions{})
	unmodeled := ir.Unmodeled{"openapi:x-deprecated-reason": vendorExtension(`"why"`)}
	var dep ir.Deprecation
	prov := ir.Provenance{Inferred: "group-path-prefix"}

	c.PromoteDeprecation(unmodeled, &dep, &prov)
	assert.Equal(t, "group-path-prefix,extension-promotion", prov.Inferred)

	c.PromoteDeprecation(unmodeled, &dep, &prov)
	assert.Equal(t, "group-path-prefix,extension-promotion", prov.Inferred,
		"a second reading of the same node adds no second marker")
}

// TestDefaultExtensionPromotions_IsCopiedNotShared holds the exported default
// to being a fresh map. A caller who starts from it and edits it must not be
// editing what the next compile reads.
func TestDefaultExtensionPromotions_IsCopiedNotShared(t *testing.T) {
	t.Parallel()
	first := lowering.DefaultExtensionPromotions()
	require.NotEmpty(t, first, "an empty mapping would make this vacuous")
	delete(first, "x-deprecated-reason")
	assert.Contains(t, lowering.DefaultExtensionPromotions(), "x-deprecated-reason")
}

// TestPromoteDeprecation_PolicyMapIsCopiedIntoTheContext pins the same rule for
// the caller's own map: the context is passed by value, so a map shared rather
// than copied would be the one part of it a lowering could write through.
func TestPromoteDeprecation_PolicyMapIsCopiedIntoTheContext(t *testing.T) {
	t.Parallel()
	targets := map[string]lowering.ExtensionTarget{"x-k": lowering.TargetDeprecationMessage}
	c := promotionCtx(lowering.ExtensionPromotions{Targets: targets})
	delete(targets, "x-k")

	var dep ir.Deprecation
	var prov ir.Provenance
	c.PromoteDeprecation(ir.Unmodeled{"openapi:x-k": vendorExtension(`"why"`)}, &dep, &prov)
	assert.Equal(t, "why", dep.Message, "the policy the context read is the one it was given")
}

// loweringPackage is the lowering package type-checked, loaded once per test
// binary.
var loweringPackage = sync.OnceValues(func() (*decls.Package, error) {
	return decls.Load("github.com/dexpace/morphic/compilers/openapi/internal/lowering")
})

// declaredTargets returns every ExtensionTarget constant the package declares,
// read type-checked rather than matched: a regex over one file misses a target
// declared in another file of the package, or spelled with different spacing,
// and misses it silently — which is the failure this check exists to prevent,
// reproduced in the check itself. A declaration typed, converted, aliased or
// computed is read the way the compiler reads it.
//
// A package-level var of the type fails: it is assignable wherever a target is
// but holds no value this check can read, so it would escape the applier check.
func declaredTargets(t *testing.T) []lowering.ExtensionTarget {
	t.Helper()
	p, err := loweringPackage()
	require.NoError(t, err)
	named, err := p.Named("ExtensionTarget")
	require.NoError(t, err)

	for v := range p.Objects[*types.Var]() {
		assert.False(t, types.Identical(v.Type(), named),
			"%s is a var of ExtensionTarget; a target must be a constant to be read here", v.Name())
	}

	consts, err := p.StringConsts("ExtensionTarget")
	require.NoError(t, err)
	require.NotEmpty(t, consts, "no targets found; the check below would pass vacuously")

	out := make([]lowering.ExtensionTarget, len(consts))
	for i, c := range consts {
		out[i] = lowering.ExtensionTarget(c.Value)
	}
	return out
}

// appliers is one closure per promote function the package exports. Each runs
// c's policy against a carrier of its own — the key "x-k", which every caller
// below maps — and reports whether that carrier changed.
//
// The census is hand-written, so a promote function this list does not know
// about is the same gap one level down: the check beneath it would then declare
// a target unapplied that a real lowering does apply.
func appliers() []func(lowering.Ctx) (bool, []ir.Diagnostic) {
	return []func(lowering.Ctx) (bool, []ir.Diagnostic){
		func(c lowering.Ctx) (bool, []ir.Diagnostic) {
			var dep ir.Deprecation
			var prov ir.Provenance
			diags := c.PromoteDeprecation(ir.Unmodeled{"openapi:x-k": vendorExtension(`"v"`)}, &dep, &prov)
			return dep != (ir.Deprecation{}), diags
		},
		func(c lowering.Ctx) (bool, []ir.Diagnostic) {
			enum := ir.Enum{Closed: true}
			var prov ir.Provenance
			c.PromoteEnumOpenness(ir.Unmodeled{"openapi:x-k": vendorExtension(`"v"`)}, &enum, &prov)
			return !enum.Closed, nil
		},
	}
}

// TestExtensionTarget_EveryDeclaredTargetHasAnApplier holds the vocabulary to
// the appliers, the half of "a target is a constant and an applier" that
// nothing else checks: a constant alone compiles, maps cleanly, and promotes
// nothing, with no field written, no diagnostic and no marker.
//
// Every follow-up target (Pagination, Idempotency, Sensitive) arrives as a
// constant first, so this fails on purpose until the new target gets its
// applier and appliers() learns which promote function answers for it.
func TestExtensionTarget_EveryDeclaredTargetHasAnApplier(t *testing.T) {
	t.Parallel()
	for _, target := range declaredTargets(t) {
		c := promotionCtx(lowering.ExtensionPromotions{
			Targets: map[string]lowering.ExtensionTarget{"x-k": target},
		})
		var applied bool
		for _, apply := range appliers() {
			wrote, diags := apply(c)
			assert.Empty(t, diags, "%s: a declared target reports nothing when it is applied", target)
			applied = applied || wrote
		}
		assert.True(t, applied,
			"%s is declared in the vocabulary but no applier fills it, so a policy naming it "+
				"promotes nothing and says nothing", target)
	}
}

// TestPromoteEnumOpenness_ClearsClosedAndMarksTheNode pins the promotion the
// vocabulary's one non-Deprecation target performs. Every enum the compiler
// builds is closed, so the write here is the whole of what x-extensible-enum
// buys a consumer, and the marker is what says a heuristic made it.
func TestPromoteEnumOpenness_ClearsClosedAndMarksTheNode(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		value string
	}{
		{"the member list the convention writes", `["a","b"]`},
		{"an explicit true", `true`},
		{"a value that states nothing", `"whatever"`},
		// JSON null decodes into a plain bool as false, so this row is what
		// separates the presence-only spelling from an explicit decline.
		{"a bare key, which is the presence-only spelling", `null`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			enum := ir.Enum{Closed: true}
			var prov ir.Provenance
			promotionCtx(lowering.ExtensionPromotions{}).PromoteEnumOpenness(
				ir.Unmodeled{"openapi:x-extensible-enum": vendorExtension(tc.value)}, &enum, &prov)

			assert.False(t, enum.Closed, "the key says the member set is open")
			assert.Equal(t, lowering.ExtensionPromotionHeuristic, prov.Inferred)
		})
	}
}

// TestPromoteEnumOpenness_LeavesTheEntryItRead is the losslessness half, for
// the same reason its deprecation twin is: the promotion is a second reading of
// a preserved entry, so a consumer that disagrees still has what was written —
// which for this key is the member list itself.
func TestPromoteEnumOpenness_LeavesTheEntryItRead(t *testing.T) {
	t.Parallel()
	unmodeled := ir.Unmodeled{"openapi:x-extensible-enum": vendorExtension(`["a","b"]`)}
	promotionCtx(lowering.ExtensionPromotions{}).PromoteEnumOpenness(unmodeled, new(ir.Enum{Closed: true}), &ir.Provenance{})

	entry, kept := unmodeled["openapi:x-extensible-enum"]
	require.True(t, kept, "the entry survives its own promotion")
	assert.Equal(t, ir.ReasonVendorExtension, entry.Reason)
	assert.JSONEq(t, `["a","b"]`, string(entry.Value))
}

// TestPromoteEnumOpenness_WritesNothing pins every shape that must leave the
// enum closed. The last row is the one that is not an absence: a document
// writing the key with a boolean false says the set is *not* extensible, and
// reading presence alone there would record the opposite of what it said.
func TestPromoteEnumOpenness_WritesNothing(t *testing.T) {
	t.Parallel()
	filled := ir.Unmodeled{"openapi:x-extensible-enum": vendorExtension(`["a","b"]`)}
	tests := []struct {
		name      string
		policy    lowering.ExtensionPromotions
		unmodeled ir.Unmodeled
	}{
		{"promotion disabled", lowering.ExtensionPromotions{Disabled: true}, filled},
		{"no extensions kept", lowering.ExtensionPromotions{}, nil},
		{"a key the document did not write", lowering.ExtensionPromotions{}, ir.Unmodeled{
			"openapi:x-other": vendorExtension(`["a","b"]`),
		}},
		{
			"a key mapped to another target",
			lowering.ExtensionPromotions{Targets: map[string]lowering.ExtensionTarget{
				"x-extensible-enum": lowering.TargetDeprecationMessage,
			}},
			filled,
		},
		{
			"an explicit false",
			lowering.ExtensionPromotions{},
			ir.Unmodeled{"openapi:x-extensible-enum": vendorExtension(`false`)},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			enum := ir.Enum{Closed: true}
			var prov ir.Provenance
			promotionCtx(tc.policy).PromoteEnumOpenness(tc.unmodeled, &enum, &prov)

			assert.True(t, enum.Closed, "the enum stays as the format declared it")
			assert.Empty(t, prov.Inferred, "nothing was inferred, so nothing is marked")
		})
	}
}

// TestPromoteEnumOpenness_NonEnumCarrierIsTheWholeAnswer pins the nil cases,
// which are the ordinary shape rather than a guard: most nodes an x-* key can
// sit on are not enums, and a node with no provenance could not record the
// guess (promotion rule 4).
func TestPromoteEnumOpenness_NonEnumCarrierIsTheWholeAnswer(t *testing.T) {
	t.Parallel()
	c := promotionCtx(lowering.ExtensionPromotions{})
	unmodeled := ir.Unmodeled{"openapi:x-extensible-enum": vendorExtension(`["a","b"]`)}
	c.PromoteEnumOpenness(unmodeled, nil, &ir.Provenance{})

	enum := ir.Enum{Closed: true}
	c.PromoteEnumOpenness(unmodeled, &enum, nil)
	assert.True(t, enum.Closed, "with nowhere to record the guess, none is made")
}
