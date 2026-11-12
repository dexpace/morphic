// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/speakeasy-api/openapi/jsonschema/oas3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering/loweringtest"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
)

// TestSubtypeTagReading_ReportsWhatItCannotSettle pins that a reading deferred
// for a subtype the registry does not hold as the model its lowering tagged,
// missing, nil, another kind or tagged otherwise, is announced rather than
// taken, and leaves the node as it is.
func TestSubtypeTagReading_ReportsWhatItCannotSettle(t *testing.T) {
	t.Parallel()
	cat := &ir.Literal{ID: "t/cat", Value: ir.Value{Kind: ir.ValueString, Str: "cat"}}
	pinned := func(tag string) *ir.Model {
		return &ir.Model{ID: "t/s", DiscriminatorValue: tag, Properties: []ir.Property{{
			ID: "p/kind", Name: ir.Naming{Source: "kind"}, Type: ir.TypeRef{Target: "t/cat"},
		}}}
	}
	retagged := pinned("Other")
	for name, reg := range map[string]ir.TypeRegistry{
		"missing":  {"t/cat": cat},
		"nil":      {"t/s": (*ir.Model)(nil), "t/cat": cat},
		"no model": {"t/s": &ir.Union{ID: "t/s"}, "t/cat": cat},
		"retagged": {"t/s": retagged, "t/cat": cat},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			reading := subtypeTagReading{c: loweringtest.Context(t, openapitest.EmptySpec, lowering.Policy{}),
				id: "t/s", pointer: "/components/schemas/S", property: "kind", name: "S"}
			diags := reading.Settle(reg)
			require.Len(t, diags, 1)
			assert.Equal(t, diag.InternalInvariant, diags[0].Code)
			assert.Equal(t, ir.SeverityError, diags[0].Severity)
		})
	}
	assert.Equal(t, "Other", retagged.DiscriminatorValue, "a model tagged otherwise keeps its tag")
	assert.Empty(t, retagged.Provenance.Inferred)
}

// TestTypeStrings_ReadsAClosedSetOfStrings pins what a type must be to bound a
// tag to strings: a string Literal, or a closed Enum marking no flags every
// member of which is a string, its members read sorted and once. No other node,
// a nil one included, bounds one. pinnedValue answers for the same nodes,
// pinning the one string a set of one holds.
func TestTypeStrings_ReadsAClosedSetOfStrings(t *testing.T) {
	t.Parallel()
	str := func(s string) ir.EnumMember { return ir.EnumMember{Value: ir.Value{Kind: ir.ValueString, Str: s}} }
	num := ir.EnumMember{Value: ir.Value{Kind: ir.ValueNumber, Num: "1"}}
	for name, tc := range map[string]struct {
		node ir.TypeDef
		want []string // nil where none is bounded
	}{
		"literal":       {&ir.Literal{Value: str("a").Value}, []string{"a"}},
		"enum of one":   {&ir.Enum{Members: []ir.EnumMember{str("a")}, Closed: true}, []string{"a"}},
		"enum":          {&ir.Enum{Members: []ir.EnumMember{str("b"), str("a"), str("b")}, Closed: true}, []string{"a", "b"}},
		"nil literal":   {(*ir.Literal)(nil), nil},
		"nil enum":      {(*ir.Enum)(nil), nil},
		"open enum":     {&ir.Enum{Members: []ir.EnumMember{str("a")}}, nil},
		"flags":         {&ir.Enum{Members: []ir.EnumMember{str("a")}, Closed: true, Flags: true}, nil},
		"a number":      {&ir.Enum{Members: []ir.EnumMember{str("a"), num}, Closed: true}, nil},
		"number":        {&ir.Literal{Value: num.Value}, nil},
		"model":         {&ir.Model{}, nil},
		"through alias": {&ir.Scalar{Base: &ir.TypeRef{Target: "t/lit"}}, []string{"a"}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			reg := ir.TypeRegistry{"t/x": tc.node, "t/lit": &ir.Literal{Value: str("a").Value}}
			got, ok := typeStrings(reg, "t/x")
			assert.Equal(t, tc.want != nil, ok)
			assert.Equal(t, tc.want, got)
			pin, pins := pinnedValue(reg, "t/x")
			assert.Equal(t, len(tc.want) == 1, pins, "pinnedValue pins the one string a set of one holds")
			if pins {
				assert.Equal(t, tc.want[0], pin)
			}
		})
	}
}

// TestThroughAliases_ReadsAtMostTheBound pins the alias scalars pinnedValue and
// typeStrings read a tag's type through: a chain of maxTagAliasHops reaches what
// it stands over, a longer one or a loop stops on an alias, which bounds no tag.
func TestThroughAliases_ReadsAtMostTheBound(t *testing.T) {
	t.Parallel()
	chain := func(hops int) ir.TypeRegistry {
		reg := ir.TypeRegistry{"t/lit": &ir.Literal{ID: "t/lit", Value: ir.Value{Kind: ir.ValueString, Str: "a"}}}
		next := ir.TypeID("t/lit")
		for i := range hops {
			id := ir.TypeID(fmt.Sprintf("t/alias%d", i))
			reg[id] = &ir.Scalar{ID: id, Base: &ir.TypeRef{Target: next}}
			next = id
		}
		reg["t/x"] = reg[next]
		return reg
	}
	for name, tc := range map[string]struct {
		reg   ir.TypeRegistry
		bound bool
	}{
		"within the bound": {chain(maxTagAliasHops), true},
		"past the bound":   {chain(maxTagAliasHops + 1), false},
		"a loop": {ir.TypeRegistry{
			"t/x": &ir.Scalar{ID: "t/x", Base: &ir.TypeRef{Target: "t/y"}},
			"t/y": &ir.Scalar{ID: "t/y", Base: &ir.TypeRef{Target: "t/x"}},
		}, false},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, ok := typeStrings(tc.reg, "t/x")
			assert.Equal(t, tc.bound, ok)
			_, pins := pinnedValue(tc.reg, "t/x")
			assert.Equal(t, tc.bound, pins)
		})
	}
}

// TestAdmittedList_NamesAFew pins how a report names what a subtype admits:
// none, or the first few in order and how many more.
func TestAdmittedList_NamesAFew(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "no string", admittedList(nil))
	assert.Equal(t, `only "a", "b", "c"`, admittedList([]string{"a", "b", "c"}))
	assert.Equal(t, `only "a", "b", "c", 2 more`, admittedList([]string{"a", "b", "c", "d", "e"}))
}

// TestRoutesByName_ReadsAUnionsListOnce pins what asking whether a union's
// discriminator routes a component by name costs once a subtype has asked:
// the same at two widths, so a base listing n subtypes has its branches read
// once a run rather than once a subtype. It is not run in parallel:
// AllocsPerRun measures the process.
func TestRoutesByName_ReadsAUnionsListOnce(t *testing.T) {
	narrow, wide := routesAllocs(t, 2), routesAllocs(t, 40)
	assert.Equal(t, narrow, wide, "the union's branches are read once")
}

// routesAllocs returns the allocations, the fewest of single runs, that asking
// whether Pet's discriminator, beside a oneOf listing width subtypes, routes
// the last of them by name takes.
func routesAllocs(t *testing.T, width int) float64 {
	t.Helper()
	var spec strings.Builder
	spec.WriteString("    Pet:\n      type: object\n      discriminator: {propertyName: kind}\n      oneOf:\n")
	for i := range width {
		fmt.Fprintf(&spec, "        - $ref: '#/components/schemas/S%d'\n", i)
	}
	for i := range width {
		fmt.Fprintf(&spec, "    S%d: {allOf: [{$ref: '#/components/schemas/Pet'}]}\n", i)
	}
	l, _ := LoweredFor(t, openapitest.ComponentSpecVer("3.1.0", spec.String()))
	schemaNamed := func(name string) *oas3.Schema {
		js, ok := l.Ctx.Doc.Components.Schemas.Get(name)
		require.True(t, ok, name)
		return js.GetSchema()
	}
	pet, last := schemaNamed("Pet"), schemaNamed(fmt.Sprintf("S%d", width-1))
	require.True(t, routesByName(l.Ctx, pet, last), "a listed subtype is routed by name")

	fewest := math.Inf(1)
	for range 20 {
		fewest = min(fewest, testing.AllocsPerRun(1, func() { routesByName(l.Ctx, pet, last) }))
	}
	return fewest
}
