package lowering_test

import (
	"testing"

	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/overlay"
	"github.com/dexpace/morphic/ir"
)

// promotionCtx builds a context carrying nothing but the promotion policy.
func promotionCtx(policy lowering.ExtensionPromotions) lowering.Ctx {
	return lowering.New(0, &soa.OpenAPI{}, ir.SourceInfo{}, "", policy, overlay.Origin{})
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
// writes, one field at a time, because the three share a struct and a promotion
// writing the wrong member of it would still look filled.
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
	unmodeled := ir.Unmodeled{"openapi:x-deprecated-reason": vendorExtension(`7`)}
	var dep ir.Deprecation
	var prov ir.Provenance
	diags := promotionCtx(lowering.ExtensionPromotions{}).PromoteDeprecation(unmodeled, &dep, &prov)

	require.Len(t, diags, 1)
	assert.Equal(t, ir.SeverityInfo, diags[0].Severity)
	assert.Equal(t, "openapi/degraded-construct", diags[0].Code)
	assert.Equal(t, "/components/schemas/S", diags[0].Provenance.Pointer,
		"the report names the extension rather than the node holding it")
	assert.Equal(t, ir.Deprecation{}, dep)
	assert.Empty(t, prov.Inferred)
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
