// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package pagination

import (
	"testing"

	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/load"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/runtimeexpr"
	"github.com/dexpace/morphic/ir"
)

// pagerOver is a pager over reg, under the default policy.
func pagerOver(reg ir.TypeRegistry) *pager {
	c := lowering.New(0, &load.Document{Doc: &soa.OpenAPI{}}, lowering.Policy{})
	return &pager{c: c, p: c.Promoter(), reg: reg, lk: runtimeexpr.NewLookups(reg), flat: map[ir.TypeID][]*ir.Property{}}
}

// TestMarker_AUseOfATypeTheRegistryLacksMarksNothing drives the guard no
// document reaches: a use naming a type the registry does not hold, as a
// dangling reference would, is marked by its own keys alone.
func TestMarker_AUseOfATypeTheRegistryLacksMarksNothing(t *testing.T) {
	t.Parallel()
	m := &marker{pg: pagerOver(ir.TypeRegistry{}), target: lowering.TargetPaginationItems,
		src: lowering.ExtensionSource{Key: "x-k", Values: []string{"items"}}}
	keeper, marked := m.marksUse(nil, &ir.Provenance{}, "t/gone")
	assert.Nil(t, keeper)
	assert.False(t, marked)
}

// TestPager_AParameterOnlyABindingNamesResolvesNothing drives the guard no
// document reaches: a binding naming a parameter its operation does not hold
// gives a member of it no type to walk, so the value resolves to nothing.
func TestPager_AParameterOnlyABindingNamesResolvesNothing(t *testing.T) {
	t.Parallel()
	pg := pagerOver(ir.TypeRegistry{})
	in, failure := pg.paramInput(&ir.Operation{ID: "op/x"}, "param/x/gone", []string{"a"})
	assert.Nil(t, in)
	assert.Contains(t, failure, "which a binding names and the operation does not hold")
}

// TestMarker_DescendsOnlyIntoModelsReachingTheKey pins a search to the models
// whose members reach its key (pager.reaching): with no member keeping the key
// no model is flattened and the root is not read, and a level steps into a
// member's model only where that model reaches the key. None of this changes
// what a search finds, only what it reads, so it is held here. A member naming
// a type the registry lacks, as a dangling reference would, keeps nothing.
func TestMarker_DescendsOnlyIntoModelsReachingTheKey(t *testing.T) {
	t.Parallel()
	src := lowering.ExtensionSource{Key: "x-k", Member: "purpose", Values: []string{"items"}}
	member := func(id ir.PropID, target ir.TypeID) ir.Property {
		return ir.Property{ID: id, WireName: string(id), Type: ir.TypeRef{Target: target}}
	}
	reg := ir.TypeRegistry{
		"t/root":   &ir.Model{ID: "t/root", Properties: []ir.Property{member("p/a", "t/marked"), member("p/b", "t/plain")}},
		"t/plain":  &ir.Model{ID: "t/plain", Properties: []ir.Property{member("p/c", "t/text"), member("p/e", "t/gone")}},
		"t/marked": &ir.Model{ID: "t/marked", Properties: []ir.Property{member("p/d", "t/text")}},
		"t/text":   &ir.Scalar{ID: "t/text"},
	}

	unkept := pagerOver(reg)
	assert.Nil(t, (&marker{pg: unkept, target: lowering.TargetPaginationItems, src: src}).search("t/root"))
	assert.Empty(t, unkept.flat, "no member keeps the key, so no model is read")

	reg["t/marked"].(*ir.Model).Properties[0].Unmodeled = ir.Unmodeled{
		"openapi:x-k": {Reason: ir.ReasonVendorExtension, Value: ir.RawValue(`{"purpose":"items"}`)},
	}
	kept := pagerOver(reg)
	m := &marker{pg: kept, target: lowering.TargetPaginationItems, src: src}
	reach := kept.reaching(src.Key)
	assert.Equal(t, map[ir.TypeID]bool{"t/root": true, "t/marked": true}, reach)
	_, next := m.descend([]step{{model: "t/root"}}, map[ir.TypeID]bool{"t/root": true}, reach)
	require.Len(t, next, 1, "a level steps into the model reaching the key, not its sibling")
	assert.Equal(t, ir.TypeID("t/marked"), next[0].model)
	found := m.search("t/root")
	require.Len(t, found, 1)
	assert.Equal(t, []ir.PropID{"p/a", "p/d"}, found[0].segments)
}
