// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package promotion_test

import (
	"reflect"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/compilers/openapi/internal/promotion"
	"github.com/dexpace/morphic/ir"
)

// stabilityPolicy reads a tier from x-stability alone.
func stabilityPolicy() promotion.Promoter {
	return promotion.New(mapping(promotion.TargetStability, promotion.ExtensionSource{Key: "x-stability"}))
}

// TestCarried_ReturnsOwnWhereNothingIsCarried holds the common case to no cost:
// a map holding no scoped key the policy reads comes back as itself, the same
// map, and the call allocates nothing.
func TestCarried_ReturnsOwnWhereNothingIsCarried(t *testing.T) {
	p := stabilityPolicy()
	for name, own := range map[string]ir.Unmodeled{
		"empty":            {},
		"an unrelated key": {"openapi:x-other": vendorExtension(`1`), "openapi:pathItem/x-other": vendorExtension(`2`)},
		"the key unscoped": {"openapi:x-stability": vendorExtension(`"beta"`)},
	} {
		got := promotion.Carried(p, own, "pathItem")
		assert.Equal(t, reflect.ValueOf(own).Pointer(), reflect.ValueOf(got).Pointer(), name)
		assert.Zero(t, testing.AllocsPerRun(10, func() { _ = promotion.Carried(p, own, "pathItem") }), name)
	}
}

// TestCarried_TheOwnKeyBeatsTheScopedOne holds a valued fact to the nearest
// statement: the key unscoped is the node's own and stays; where it holds none
// the first scope holding one supplies it, keyed unscoped with its own
// provenance; the input is not written.
func TestCarried_TheOwnKeyBeatsTheScopedOne(t *testing.T) {
	p := stabilityPolicy()
	own := ir.Unmodeled{"openapi:x-stability": vendorExtension(`"stable"`)}
	scoped := ir.Unmodeled{"openapi:pathItem/x-stability": vendorExtension(`"beta"`)}
	merged := ir.Unmodeled{
		"openapi:x-stability":          vendorExtension(`"stable"`),
		"openapi:pathItem/x-stability": vendorExtension(`"beta"`),
	}

	assert.Empty(t, cmp.Diff(merged, promotion.Carried(p, merged, "pathItem")), "the own key wins")
	assert.Len(t, own, 1)

	got := promotion.Carried(p, scoped, "pathItem")
	assert.Equal(t, vendorExtension(`"beta"`), got["openapi:x-stability"], "the scoped statement, unscoped")
	assert.Len(t, scoped, 1, "the input is not written")

	two := ir.Unmodeled{
		"openapi:schema/x-stability":    vendorExtension(`"alpha"`),
		"openapi:content/m/x-stability": vendorExtension(`"beta"`),
	}
	assert.Equal(t, vendorExtension(`"beta"`),
		promotion.Carried(p, two, "content/m", "schema")["openapi:x-stability"], "the first scope of those given")
}

// TestLent_ASingleSidedFactHoldsWhereAnyStatesIt holds redaction and deprecation
// to one side: a scope stating either lends it to the referent whatever the
// carrier's own key says, a deprecation's members fill from the nearest scope
// stating them, a scope stating nothing lends nothing, and every fact the $ref
// target lends stays, its members filling what no scope states.
func TestLent_ASingleSidedFactHoldsWhereAnyStatesIt(t *testing.T) {
	p := promotion.New(promotion.ExtensionPromotions{Targets: map[promotion.ExtensionTarget][]promotion.ExtensionSource{
		promotion.TargetRedaction:          {{Key: "x-sensitive"}},
		promotion.TargetDeprecation:        {{Key: "x-deprecated"}},
		promotion.TargetDeprecationMessage: {{Key: "x-why"}},
	}})
	own := ir.Unmodeled{
		"openapi:x-sensitive":         vendorExtension(`false`),
		"openapi:schema/x-sensitive":  vendorExtension(`true`),
		"openapi:schema/x-deprecated": vendorExtension(`true`),
		"openapi:schema/x-why":        vendorExtension(`"old"`),
	}
	got := p.Lent(promotion.Referent{}, own, "schema")
	assert.True(t, got.Redacted, "the carrier's false withdraws nothing the schema states")
	assert.True(t, got.Deprecated)
	assert.Equal(t, "old", got.Members.Message)

	assert.Equal(t, promotion.Referent{}, p.Lent(promotion.Referent{}, ir.Unmodeled{}, "schema"))
	assert.Equal(t, promotion.Referent{}, p.Lent(promotion.Referent{}, ir.Unmodeled{"openapi:x-sensitive": vendorExtension(`true`)}, "schema"),
		"the carrier's own key is the caller's to read, not a scope's")
	from := promotion.Referent{Deprecated: true, Redacted: true, Homes: true,
		Members: ir.Deprecation{Message: "gone", Since: "1", RemovalVersion: "2", RemovalDate: "2099-01-01"}}
	assert.Equal(t, from, p.Lent(from, ir.Unmodeled{}, "schema"), "what the $ref target lends stays, every fact")
	scoped := p.Lent(from, ir.Unmodeled{"openapi:schema/x-why": vendorExtension(`"near"`)}, "schema")
	assert.True(t, scoped.Deprecated && scoped.Redacted && scoped.Homes, "a scope stating a member withdraws no fact")
	assert.Equal(t, ir.Deprecation{Message: "near", Since: "1", RemovalVersion: "2", RemovalDate: "2099-01-01"},
		scoped.Members, "the nearest member wins and the target fills the rest")
}

// TestCarrierScopes_NamesTheSchemaOrTheContentEntryThenItsSchema holds the
// scopes a parameter or a header is read through to the spelling it elects,
// nearest first, a media type escaped as one segment.
func TestCarrierScopes_NamesTheSchemaOrTheContentEntryThenItsSchema(t *testing.T) {
	t.Parallel()
	assert.Equal(t, []string{"schema"}, promotion.CarrierScopes(""))
	assert.Equal(t, []string{"content/text~1plain", "content/text~1plain/schema"}, promotion.CarrierScopes("text/plain"))
}

// TestGroupStability_RecordsTheHeuristicInInferred holds a group's tier to the
// key it keeps, recorded in its Inferred, and a group keeping none to no tier
// and no record.
func TestGroupStability_RecordsTheHeuristicInInferred(t *testing.T) {
	t.Parallel()
	p := stabilityPolicy()
	var tier ir.Stability
	var inferred ir.Inferred
	assert.Empty(t, promotion.GroupStability(p, ir.Unmodeled{"openapi:x-stability": vendorExtension(`"beta"`)}, &tier, &inferred))
	assert.Equal(t, ir.StabilityBeta, tier)
	assert.Equal(t, ir.Inferred("extension-promotion"), inferred)

	tier, inferred = "", ""
	assert.Empty(t, promotion.GroupStability(p, ir.Unmodeled{}, &tier, &inferred))
	assert.Empty(t, tier)
	assert.Empty(t, inferred)
}

// TestUnformedTier_ReportsATierOnATagFormingNoGroup holds a mapped tier key on
// a tag that forms no group to one info at the key, and a tag writing none to
// nothing.
func TestUnformedTier_ReportsATierOnATagFormingNoGroup(t *testing.T) {
	t.Parallel()
	p := stabilityPolicy()
	diags := promotion.UnformedTier(p, "Lone", ir.Unmodeled{"openapi:x-stability": vendorExtension(`"alpha"`)})
	if assert.Len(t, diags, 1) {
		assert.Equal(t, "openapi/unpromoted-extension", diags[0].Code)
		assert.Contains(t, diags[0].Message, `on tag "Lone", which forms no operation group`)
	}
	assert.Empty(t, promotion.UnformedTier(p, "Lone", ir.Unmodeled{}))
}

// TestShadowed_ReturnsWhatCarriedPassesOver holds Shadowed to the scoped
// statements of a read key that the carrier's own key or a nearer scope also
// writes, one layer per scope keyed unscoped, nearest first: a key no nearer
// position writes is Carried's and stays out, and so does a key the policy
// does not read.
func TestShadowed_ReturnsWhatCarriedPassesOver(t *testing.T) {
	t.Parallel()
	p := stabilityPolicy()
	own := ir.Unmodeled{
		"openapi:x-stability":                  vendorExtension(`"stable"`),
		"openapi:content/m/x-stability":        vendorExtension(`5`),
		"openapi:content/m/schema/x-stability": vendorExtension(`6`),
		"openapi:schema/x-stability":           vendorExtension(`7`),
		"openapi:content/m/schema/x-other":     vendorExtension(`8`),
	}
	got := promotion.Shadowed(p, own, "content/m", "content/m/schema")
	assert.Empty(t, cmp.Diff([]ir.Unmodeled{
		{"openapi:x-stability": vendorExtension(`5`)},
		{"openapi:x-stability": vendorExtension(`6`)},
	}, got))

	delete(own, "openapi:x-stability")
	assert.Empty(t, cmp.Diff([]ir.Unmodeled{{"openapi:x-stability": vendorExtension(`6`)}},
		promotion.Shadowed(p, own, "content/m", "content/m/schema")), "the nearest scope is Carried's")
	assert.Nil(t, promotion.Shadowed(p, own, "schema"), "nothing nearer writes the key")
}

// TestReads_NamesAKeyThePolicyReads pins which kept keys a promotion reads:
// the extension its policy names, under any scope, and no other.
func TestReads_NamesAKeyThePolicyReads(t *testing.T) {
	t.Parallel()
	p := stabilityPolicy()
	assert.True(t, p.Reads("openapi:x-stability"))
	assert.True(t, p.Reads("openapi:schema/x-stability"))
	assert.False(t, p.Reads("openapi:x-other"))
}
