// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"encoding/json/jsontext"
	"fmt"
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/compilers/openapi/internal/service"
	"github.com/dexpace/morphic/compilers/openapi/internal/service/servicetest"
	"github.com/dexpace/morphic/ir"
)

// TestUnionTags_ReportsWhatItCannotSettle pins that a reading deferred for a
// union the registry does not hold as one, missing, nil or another kind, is
// announced rather than skipped.
func TestUnionTags_ReportsWhatItCannotSettle(t *testing.T) {
	t.Parallel()
	for name, reg := range map[string]ir.TypeRegistry{
		"missing":  {},
		"nil":      {"type/u": (*ir.Union)(nil)},
		"no union": {"type/u": &ir.Model{ID: "type/u"}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			tags := unionTags{c: servicetest.Context(t, openapitest.EmptySpec, service.Policy{}).Ctx, id: "type/u", pointer: "/components/schemas/U"}
			diags := tags.Settle(t.Context(), reg)
			require.Len(t, diags, 1)
			assert.Equal(t, diag.InternalInvariant, diags[0].Code)
			assert.Equal(t, ir.SeverityError, diags[0].Severity)
		})
	}
}

// TestUnionTags_ReadsOnlyWhatItWasDeferredFor pins the states only a compiler
// bug leaves: a declared reading meeting a union with no discriminator, or one
// whose variants its branches do not count, and an undeclared reading meeting
// a union already tagged, each reported and the union left as it is. An open
// union is a state a document can write, left alone unreported.
func TestUnionTags_ReadsOnlyWhatItWasDeferredFor(t *testing.T) {
	t.Parallel()
	pinned := func(id ir.TypeID, tag string) ir.TypeDef {
		return &ir.Model{ID: id, Properties: []ir.Property{{
			ID: ir.PropertyID("property/" + tag), Name: ir.Naming{Source: "kind"}, Required: true,
			Type: ir.TypeRef{Target: ir.TypeID("type/lit/" + tag)},
		}}}
	}
	variants := []ir.Variant{{Type: ir.TypeRef{Target: "type/a"}}, {Type: ir.TypeRef{Target: "type/b"}}}
	for name, tc := range map[string]struct {
		union    *ir.Union
		at       jsontext.Pointer
		implicit []implicitRoute
		bug      bool
	}{
		"declared, none held": {&ir.Union{ID: "type/u", Variants: variants, Closed: true}, "/d", []implicitRoute{{name: "A", named: true}, {name: "B", named: true}}, true},
		"declared, miscounted": {&ir.Union{ID: "type/u", Variants: variants, Closed: true,
			Discriminator: &ir.Discriminator{PropertyName: "kind"}}, "/d", []implicitRoute{{name: "A", named: true}}, true},
		"undeclared, tagged": {&ir.Union{ID: "type/u", Variants: variants, Closed: true, WireTagged: true,
			Discriminator: &ir.Discriminator{PropertyName: "other"}}, "", nil, true},
		"undeclared, open": {&ir.Union{ID: "type/u", Variants: variants}, "", nil, false},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			before := *tc.union
			reg := ir.TypeRegistry{
				"type/u": tc.union, "type/a": pinned("type/a", "a"), "type/b": pinned("type/b", "b"),
				"type/lit/a": &ir.Literal{ID: "type/lit/a", Value: ir.Value{Kind: ir.ValueString, String: "a"}},
				"type/lit/b": &ir.Literal{ID: "type/lit/b", Value: ir.Value{Kind: ir.ValueString, String: "b"}},
			}
			tags := unionTags{c: servicetest.Context(t, openapitest.EmptySpec, service.Policy{}).Ctx, id: "type/u",
				pointer: "/components/schemas/U", at: tc.at, implicit: tc.implicit}
			diags := tags.Settle(t.Context(), reg)
			if tc.bug {
				require.Len(t, diags, 1)
				assert.Equal(t, diag.InternalInvariant, diags[0].Code)
				assert.Equal(t, ir.SeverityError, diags[0].Severity)
			} else {
				assert.Empty(t, diags)
			}
			assert.Equal(t, before.Discriminator, tc.union.Discriminator)
			assert.Equal(t, before.WireTagged, tc.union.WireTagged)
			assert.Empty(t, tc.union.Provenance.Inferred)
		})
	}
}

// TestSpellImplicitRoutes_ReportsAVariantNoBranchStandsFor pins the state only
// a compiler bug leaves a distributed union in, a variant count its branches
// do not match: reported, and no route written, since none can be paired.
func TestSpellImplicitRoutes_ReportsAVariantNoBranchStandsFor(t *testing.T) {
	t.Parallel()
	lowerer, _ := LoweredFor(t, openapitest.ComponentSpecVer("3.1.0",
		"    U: {oneOf: [{$ref: '#/components/schemas/A'}], discriminator: {propertyName: k}}\n    A: {type: object}\n"))
	js, ok := lowerer.Doc.Components.Schemas.Get("U")
	require.True(t, ok)
	union := &ir.Union{Variants: []ir.Variant{{Type: ir.TypeRef{Target: "type/a"}}, {Type: ir.TypeRef{Target: "type/b"}}},
		Discriminator: &ir.Discriminator{PropertyName: "k"}}

	diags := spellImplicitRoutes(lowerer.Ctx, besideUnion(lowerer.Ctx.Statements().Of(js).Schema, ""), union, "/components/schemas/U")

	require.Len(t, diags, 1)
	assert.Equal(t, diag.InternalInvariant, diags[0].Code)
	assert.Nil(t, union.Discriminator.Mapping)
}

// TestUnionTags_FlattensTheOtherVariantsOnlyForACandidate pins what an
// undeclared union whose first variant pins nothing costs: the first variant's
// flattening, however many variants follow, which every closed union of a
// document pays. Its allocations are counted at two widths and must agree. It
// is not run in parallel: AllocsPerRun measures the process.
func TestUnionTags_FlattensTheOtherVariantsOnlyForACandidate(t *testing.T) {
	narrow, wide := settleAllocs(t, 2, 0), settleAllocs(t, 40, 0)
	assert.Equal(t, narrow, wide, "the other variants are flattened only once the first pins a candidate")
}

// TestUnionTags_FlattensTheOtherVariantsOnce pins that the other variants are
// flattened and keyed by name once, however many candidates the first pins: a
// candidate after the first costs a lookup in each. What a second candidate
// adds is counted at two widths and must agree. It is not run in parallel:
// AllocsPerRun measures the process.
func TestUnionTags_FlattensTheOtherVariantsOnce(t *testing.T) {
	narrow := settleAllocs(t, 2, 2) - settleAllocs(t, 2, 1)
	wide := settleAllocs(t, 6, 2) - settleAllocs(t, 6, 1)
	assert.Equal(t, narrow, wide, "a second candidate flattens no variant again")
}

// settleAllocs returns the allocations, the fewest of single runs, that the
// discriminator-inference reading of an undeclared closed union of width
// variants takes. Each variant requires four properties, the first pinned of
// them held to one value every variant shares, so each is a candidate the
// reading tries and refuses.
func settleAllocs(t *testing.T, width, pinned int) float64 {
	t.Helper()

	reg := ir.TypeRegistry{"type/lit/x": &ir.Literal{ID: "type/lit/x", Value: ir.Value{Kind: ir.ValueString, String: "x"}}}
	variants := make([]ir.Variant, width)
	for i := range variants {
		id := ir.TypeID(fmt.Sprintf("type/v%d", i))
		properties := make([]ir.Property, 4)
		for j := range properties {
			target := ir.TypeID("type/primitive/string")
			if j < pinned {
				target = "type/lit/x"
			}
			properties[j] = ir.Property{ID: ir.PropertyID(fmt.Sprintf("property/v%d/k%d", i, j)), Name: ir.Naming{Source: fmt.Sprintf("k%d", j)},
				Required: true, Type: ir.TypeRef{Target: target}}
		}
		reg[id] = &ir.Model{ID: id, Properties: properties}
		variants[i] = ir.Variant{Type: ir.TypeRef{Target: id}}
	}
	reg["type/u"] = &ir.Union{ID: "type/u", Variants: variants, Closed: true}

	tags := unionTags{c: servicetest.Context(t, openapitest.EmptySpec, service.Policy{}).Ctx, id: "type/u",
		pointer: "/components/schemas/U"}
	fewest := math.Inf(1)
	for range 20 {
		fewest = min(fewest, testing.AllocsPerRun(1, func() { tags.Settle(t.Context(), reg) }))
	}
	return fewest
}
