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
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering/loweringtest"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
)

// TestUnionTags_ReportsWhatItCannotSettle pins that a reading deferred for a
// union the registry does not hold as one, missing, nil or another kind, is
// announced rather than skipped.
func TestUnionTags_ReportsWhatItCannotSettle(t *testing.T) {
	t.Parallel()
	for name, reg := range map[string]ir.TypeRegistry{
		"missing":  {},
		"nil":      {"t/u": (*ir.Union)(nil)},
		"no union": {"t/u": &ir.Model{ID: "t/u"}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			tags := unionTags{c: loweringtest.Context(t, openapitest.EmptySpec, lowering.Policy{}), id: "t/u", pointer: "/components/schemas/U"}
			diags := tags.Settle(reg)
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
			ID: ir.PropID("p/" + tag), Name: ir.Naming{Source: "kind"}, Required: true,
			Type: ir.TypeRef{Target: ir.TypeID("t/lit/" + tag)},
		}}}
	}
	variants := []ir.Variant{{Type: ir.TypeRef{Target: "t/a"}}, {Type: ir.TypeRef{Target: "t/b"}}}
	for name, tc := range map[string]struct {
		union    *ir.Union
		at       jsontext.Pointer
		implicit []string
		bug      bool
	}{
		"declared, none held": {&ir.Union{ID: "t/u", Variants: variants, Closed: true}, "/d", []string{"A", "B"}, true},
		"declared, miscounted": {&ir.Union{ID: "t/u", Variants: variants, Closed: true,
			Discriminator: &ir.Discriminator{PropertyName: "kind"}}, "/d", []string{"A"}, true},
		"undeclared, tagged": {&ir.Union{ID: "t/u", Variants: variants, Closed: true, WireTagged: true,
			Discriminator: &ir.Discriminator{PropertyName: "other"}}, "", nil, true},
		"undeclared, open": {&ir.Union{ID: "t/u", Variants: variants}, "", nil, false},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			before := *tc.union
			reg := ir.TypeRegistry{
				"t/u": tc.union, "t/a": pinned("t/a", "a"), "t/b": pinned("t/b", "b"),
				"t/lit/a": &ir.Literal{ID: "t/lit/a", Value: ir.Value{Kind: ir.ValueString, Str: "a"}},
				"t/lit/b": &ir.Literal{ID: "t/lit/b", Value: ir.Value{Kind: ir.ValueString, Str: "b"}},
			}
			tags := unionTags{c: loweringtest.Context(t, openapitest.EmptySpec, lowering.Policy{}), id: "t/u",
				pointer: "/components/schemas/U", at: tc.at, implicit: tc.implicit}
			diags := tags.Settle(reg)
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
	l, _ := LoweredFor(t, openapitest.ComponentSpecVer("3.1.0",
		"    U: {oneOf: [{$ref: '#/components/schemas/A'}], discriminator: {propertyName: k}}\n    A: {type: object}\n"))
	js, ok := l.Ctx.Doc.Components.Schemas.Get("U")
	require.True(t, ok)
	u := &ir.Union{Variants: []ir.Variant{{Type: ir.TypeRef{Target: "t/a"}}, {Type: ir.TypeRef{Target: "t/b"}}},
		Discriminator: &ir.Discriminator{PropertyName: "k"}}

	diags := spellImplicitRoutes(l.Ctx, js.GetSchema(), u, "/components/schemas/U")

	require.Len(t, diags, 1)
	assert.Equal(t, diag.InternalInvariant, diags[0].Code)
	assert.Nil(t, u.Discriminator.Mapping)
}

// TestPinnedValue_ReadsOneStringOnly pins what a type must be to pin a tag: a
// string Literal, or a closed Enum of one string member that marks no flags.
// No other node, a nil one included, pins one.
func TestPinnedValue_ReadsOneStringOnly(t *testing.T) {
	t.Parallel()
	str := ir.Value{Kind: ir.ValueString, Str: "a"}
	member := []ir.EnumMember{{Value: str}}
	for name, tc := range map[string]struct {
		node ir.TypeDef
		ok   bool
	}{
		"literal":     {&ir.Literal{Value: str}, true},
		"enum":        {&ir.Enum{Members: member, Closed: true}, true},
		"nil literal": {(*ir.Literal)(nil), false},
		"nil enum":    {(*ir.Enum)(nil), false},
		"open enum":   {&ir.Enum{Members: member}, false},
		"flags":       {&ir.Enum{Members: member, Closed: true, Flags: true}, false},
		"two members": {&ir.Enum{Members: append(member, ir.EnumMember{Value: ir.Value{Kind: ir.ValueString, Str: "b"}}), Closed: true}, false},
		"number":      {&ir.Literal{Value: ir.Value{Kind: ir.ValueNumber, Num: "1"}}, false},
		"model":       {&ir.Model{}, false},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, ok := pinnedValue(ir.TypeRegistry{"t/x": tc.node}, "t/x")
			assert.Equal(t, tc.ok, ok)
			if tc.ok {
				assert.Equal(t, "a", got)
			}
		})
	}
}

// TestPinnedMember_ReadsEveryDeclaration pins that a member's tag is read off
// every declaration it conjoins, as an instance meets them all: a pin the
// taken declaration overrides stands, two pins of different values pin
// nothing, null admitted by one declaration is excluded by another, and a
// member every declaration lets hold null, or one a required tag needs and the
// variant may leave out, pins nothing.
func TestPinnedMember_ReadsEveryDeclaration(t *testing.T) {
	t.Parallel()
	reg := ir.TypeRegistry{
		"t/a": &ir.Literal{ID: "t/a", Value: ir.Value{Kind: ir.ValueString, Str: "a"}},
		"t/b": &ir.Literal{ID: "t/b", Value: ir.Value{Kind: ir.ValueString, Str: "b"}},
	}
	decl := func(target ir.TypeID, nullable bool) ir.PropertyDecl {
		return ir.PropertyDecl{Owner: "t/m", Property: &ir.Property{Name: ir.Naming{Source: "kind"},
			Type: ir.TypeRef{Target: target, Nullable: nullable}}}
	}
	ref := func(target ir.TypeID) ir.PropertyDecl { return decl(target, false /* nullable */) }
	refOrNull := func(target ir.TypeID) ir.PropertyDecl { return decl(target, true /* nullable */) }
	required := func(taken ir.PropertyDecl, overridden ...ir.PropertyDecl) ir.FlatProperty {
		return ir.FlatProperty{PropertyDecl: taken, Overridden: overridden, Required: true}
	}
	const str = "t/prim/string"
	for name, tc := range map[string]struct {
		p    ir.FlatProperty
		want string // "" for no pin
	}{
		"taken":                          {required(ref("t/a")), "a"},
		"overridden":                     {required(ref(str), ref("t/a")), "a"},
		"agreeing":                       {required(ref("t/a"), ref(str), refOrNull("t/a")), "a"},
		"disagreeing":                    {required(ref("t/a"), ref("t/b")), ""},
		"disagreeing, each admits null":  {required(refOrNull("t/a"), refOrNull("t/b")), ""},
		"a null another excludes":        {required(refOrNull("t/a"), ref(str)), "a"},
		"a null every one admits":        {required(refOrNull("t/a"), refOrNull(str)), ""},
		"optional where one is required": {ir.FlatProperty{PropertyDecl: ref("t/a")}, ""},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, ok := pinnedMember(reg, tc.p, true /* required */)
			assert.Equal(t, tc.want != "", ok)
			assert.Equal(t, tc.want, got)
		})
	}
	got, ok := pinnedMember(reg, ir.FlatProperty{PropertyDecl: ref("t/a")}, false /* required */)
	assert.True(t, ok, "an optional member pins where no requirement is asked")
	assert.Equal(t, "a", got)
}

// TestUnionTags_FlattensTheOtherVariantsOnlyForACandidate pins what an
// undeclared union whose first variant pins nothing costs: the first variant's
// flattening, however many variants follow, which every closed union of a
// document pays. Its allocations, the fewest of single runs, are counted at
// two widths and must agree. It is not run in parallel: AllocsPerRun measures
// the process.
func TestUnionTags_FlattensTheOtherVariantsOnlyForACandidate(t *testing.T) {
	c := loweringtest.Context(t, openapitest.EmptySpec, lowering.Policy{})
	allocs := func(width int) float64 {
		reg := ir.TypeRegistry{}
		variants := make([]ir.Variant, width)
		for i := range variants {
			id := ir.TypeID(fmt.Sprintf("t/v%d", i))
			reg[id] = &ir.Model{ID: id, Properties: []ir.Property{{
				ID: ir.PropID(fmt.Sprintf("p/v%d", i)), Name: ir.Naming{Source: "kind"}, Required: true,
				Type: ir.TypeRef{Target: "t/prim/string"},
			}}}
			variants[i] = ir.Variant{Type: ir.TypeRef{Target: id}}
		}
		reg["t/u"] = &ir.Union{ID: "t/u", Variants: variants, Closed: true}
		tags := unionTags{c: c, id: "t/u", pointer: "/components/schemas/U"}
		fewest := math.Inf(1)
		for range 20 {
			fewest = min(fewest, testing.AllocsPerRun(1, func() { tags.Settle(reg) }))
		}
		return fewest
	}
	narrow, wide := allocs(2), allocs(40)
	assert.Equal(t, narrow, wide, "the other variants are flattened only once the first pins a candidate")
}
