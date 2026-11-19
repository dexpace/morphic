// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package pagination

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/compileddoc"
	"github.com/dexpace/morphic/compilers/openapi/internal/promotion"
	"github.com/dexpace/morphic/ir"
)

// pagerOver is a pager over reg, under the default policy.
func pagerOver(reg ir.TypeRegistry) *pager {
	return &pager{p: promotion.New(promotion.ExtensionPromotions{}), w: compileddoc.New(&ir.Document{Types: reg})}
}

// TestMarker_AUseOfATypeTheRegistryLacksMarksNothing drives the guard no
// document reaches: a use naming a type the registry does not hold, as a
// dangling reference would, is marked by its own keys alone.
func TestMarker_AUseOfATypeTheRegistryLacksMarksNothing(t *testing.T) {
	t.Parallel()
	markerValue := &marker{pg: pagerOver(ir.TypeRegistry{}), target: promotion.TargetPaginationItems,
		src: promotion.ExtensionSource{Key: "x-k", Values: []string{"items"}}}
	keeper, marked := markerValue.marksUse(nil, &ir.Provenance{}, "type/gone")
	assert.Nil(t, keeper)
	assert.False(t, marked)
}

// TestMarker_DescendsOnlyIntoModelsReachingTheKey pins a search to the models
// whose members reach its key (pager.reaching): with no member keeping the key
// no model reaches it and the root is not read, and a level steps into a
// member's model only where that model reaches the key. None of this changes
// what a search finds, only what it reads, so it is held here; that no model
// is then flattened is compileddoc.TestDocument_APromotionNoMemberMarksFlattensNoModel's.
// A member naming a type the registry lacks, as a dangling reference would,
// keeps nothing.
func TestMarker_DescendsOnlyIntoModelsReachingTheKey(t *testing.T) {
	t.Parallel()
	src := promotion.ExtensionSource{Key: "x-k", Member: "purpose", Values: []string{"items"}}
	member := func(id ir.PropertyID, target ir.TypeID) ir.Property {
		return ir.Property{ID: id, WireName: string(id), Type: ir.TypeRef{Target: target}}
	}
	reg := ir.TypeRegistry{
		"type/root":   &ir.Model{ID: "type/root", Properties: []ir.Property{member("property/a", "type/marked"), member("property/b", "type/plain")}},
		"type/plain":  &ir.Model{ID: "type/plain", Properties: []ir.Property{member("property/c", "type/text"), member("property/e", "type/gone")}},
		"type/marked": &ir.Model{ID: "type/marked", Properties: []ir.Property{member("property/d", "type/text")}},
		"type/text":   &ir.Scalar{ID: "type/text"},
	}

	unkept := pagerOver(reg)
	assert.Nil(t, (&marker{pg: unkept, target: promotion.TargetPaginationItems, src: src}).search("type/root"))
	assert.Empty(t, unkept.reach[src.Key], "no member keeps the key, so no model reaches it")

	reg["type/marked"].(*ir.Model).Properties[0].Unmodeled = ir.Unmodeled{
		"openapi:x-k": {Reason: ir.ReasonVendorExtension, Value: ir.RawValue(`{"purpose":"items"}`)},
	}
	kept := pagerOver(reg)
	markerValue := &marker{pg: kept, target: promotion.TargetPaginationItems, src: src}
	reach := kept.reaching(src.Key)
	assert.Equal(t, map[ir.TypeID]bool{"type/root": true, "type/marked": true}, reach)
	_, next := markerValue.descend([]step{{model: "type/root"}}, map[ir.TypeID]bool{"type/root": true}, reach)
	require.Len(t, next, 1, "a level steps into the model reaching the key, not its sibling")
	assert.Equal(t, ir.TypeID("type/marked"), next[0].model)
	found := markerValue.search("type/root")
	require.Len(t, found, 1)
	assert.Equal(t, []ir.PropertyID{"property/a", "property/d"}, found[0].segments)
}
