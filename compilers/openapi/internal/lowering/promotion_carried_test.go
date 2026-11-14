// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package lowering_test

import (
	"reflect"
	"slices"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/ir"
)

// stabilityPolicy reads a tier from x-stability alone.
func stabilityPolicy() lowering.Promoter {
	return promoter(mapping(lowering.TargetStability, lowering.ExtensionSource{Key: "x-stability"}))
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
		got := lowering.Carried(p, own, "pathItem")
		assert.Equal(t, reflect.ValueOf(own).Pointer(), reflect.ValueOf(got).Pointer(), name)
		assert.Zero(t, testing.AllocsPerRun(10, func() { _ = lowering.Carried(p, own, "pathItem") }), name)
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

	assert.Empty(t, cmp.Diff(own, lowering.Carried(p, merged, "pathItem")), "the own key wins")
	assert.Empty(t, cmp.Diff(own, own))

	got := lowering.Carried(p, scoped, "pathItem")
	assert.Equal(t, vendorExtension(`"beta"`), got["openapi:x-stability"], "the scoped statement, unscoped")
	assert.Len(t, scoped, 1, "the input is not written")

	two := ir.Unmodeled{
		"openapi:schema/x-stability":    vendorExtension(`"alpha"`),
		"openapi:content/m/x-stability": vendorExtension(`"beta"`),
	}
	assert.Equal(t, vendorExtension(`"beta"`),
		lowering.Carried(p, two, "content/m", "schema")["openapi:x-stability"], "the first scope of those given")
}

// TestLent_ASingleSidedFactHoldsWhereAnyStatesIt holds redaction and deprecation
// to one side: a scope stating either lends it to the referent whatever the
// carrier's own key says, a deprecation's members fill from the nearest scope
// stating them, and a scope stating nothing lends nothing.
func TestLent_ASingleSidedFactHoldsWhereAnyStatesIt(t *testing.T) {
	p := promoter(lowering.ExtensionPromotions{Targets: map[lowering.ExtensionTarget][]lowering.ExtensionSource{
		lowering.TargetRedaction:          {{Key: "x-sensitive"}},
		lowering.TargetDeprecation:        {{Key: "x-deprecated"}},
		lowering.TargetDeprecationMessage: {{Key: "x-why"}},
	}})
	own := ir.Unmodeled{
		"openapi:x-sensitive":         vendorExtension(`false`),
		"openapi:schema/x-sensitive":  vendorExtension(`true`),
		"openapi:schema/x-deprecated": vendorExtension(`true`),
		"openapi:schema/x-why":        vendorExtension(`"old"`),
	}
	got := p.Lent(lowering.Referent{}, own, "schema")
	assert.True(t, got.Redacted, "the carrier's false withdraws nothing the schema states")
	assert.True(t, got.Deprecated)
	assert.Equal(t, "old", got.Members.Message)

	assert.Equal(t, lowering.Referent{}, p.Lent(lowering.Referent{}, ir.Unmodeled{}, "schema"))
	assert.Equal(t, lowering.Referent{}, p.Lent(lowering.Referent{}, ir.Unmodeled{"openapi:x-sensitive": vendorExtension(`true`)}, "schema"),
		"the carrier's own key is the caller's to read, not a scope's")
	from := lowering.Referent{Redacted: true}
	assert.True(t, p.Lent(from, ir.Unmodeled{}, "schema").Redacted, "what the $ref target lends stays")
}

// sortedKeys returns the keys of u in order.
func sortedKeys(u ir.Unmodeled) []string {
	out := make([]string, 0, len(u))
	for k := range u {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}
