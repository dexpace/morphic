// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"strconv"
	"strings"
	"testing"

	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/load"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/naming"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/compilers/openapi/internal/service"
	"github.com/dexpace/morphic/compilers/openapi/internal/service/servicetest"
	"github.com/dexpace/morphic/ir"
)

// TestCommonFor_NamesOnlyAComponent pins the split every hoisted node carries.
// A top-level component schema is named from the source; any deeper position is
// anonymous and carries only the hint its coordinate names it by. Getting this backwards
// gives an emitter a name to render where the source declared none, or drops the
// one it did declare.
func TestCommonFor_NamesOnlyAComponent(t *testing.T) {
	t.Parallel()
	c := service.New(2, &load.Document{Doc: &soa.OpenAPI{}}, service.Policy{})

	named := commonFor(c.Ctx, "t/x", "/components/schemas/User")
	assert.False(t, named.Anonymous, "a component schema is a declaration")
	assert.Equal(t, "User", named.Name.Source)
	assert.Equal(t, "user", named.Name.Canonical, "the framework supplies the neutral words")
	assert.Empty(t, named.Name.Hint, "a named node needs no hint")

	anon := commonFor(c.Ctx, "t/y", "/components/schemas/User/properties/id")
	assert.True(t, anon.Anonymous, "an inline position declares no name")
	assert.Empty(t, anon.Name.Source)
	assert.Equal(t, "id", anon.Name.Hint, "only the hint survives: the property's key")
}

// TestCommonFor_StampsTheDeclaringPointer pins that the node records where it
// was written, with the compile's own source index — the pair every consumer
// traces a type back through.
func TestCommonFor_StampsTheDeclaringPointer(t *testing.T) {
	t.Parallel()
	c := service.New(5, &load.Document{Doc: &soa.OpenAPI{}}, service.Policy{})
	got := commonFor(c.Ctx, "t/x", "/components/schemas/User")

	assert.Equal(t, ir.TypeID("t/x"), got.ID)
	assert.Equal(t, ir.Provenance{Source: 5, Pointer: "/components/schemas/User"}, got.Provenance)
}

// TestInternNode_DerivesTheIDAndInternsOnce pins the single hoisting entry
// point: the ID comes from the pointer, and a pointer visited twice yields the
// same node rather than a second one. Re-entering build is what a recursive
// schema would do forever.
func TestInternNode_DerivesTheIDAndInternsOnce(t *testing.T) {
	t.Parallel()
	c := servicetest.Context(t, openapitest.EmptySpec, service.Policy{})
	ts := compile.NewTypes()
	const at = "/components/schemas/User"

	var built int
	build := func(common ir.TypeCommon) ir.TypeDef {
		built++
		return &ir.Model{TypeCommon: common}
	}

	first := internNode(c.Ctx, ts, at, build, nil)
	second := internNode(c.Ctx, ts, at, build, nil)

	assert.Equal(t, ids.Document{}.ForPointer(at), first, "the ID is derived from the pointer")
	assert.Equal(t, first, second, "a second visit returns the interned ID")
	assert.Equal(t, 1, built, "and does not build the node again")

	td, ok := ts.Node(first)
	require.True(t, ok, "the node is registered under the ID it returned")
	assert.Equal(t, first, td.Common().ID, "and carries that same ID")
}

// TestInternNode_FinishesTheNodeItBuildsOnceBeforeStoringIt pins where a node
// is finished: on the node its build returned, in hand, before the registry
// holds it, and only by the visit building it, so a later visit writes nothing.
func TestInternNode_FinishesTheNodeItBuildsOnceBeforeStoringIt(t *testing.T) {
	t.Parallel()
	c := servicetest.Context(t, openapitest.EmptySpec, service.Policy{})
	ts := compile.NewTypes()
	const at = "/components/schemas/User"

	var finished []ir.TypeDef
	var stored []bool
	fin := finisher(func(td ir.TypeDef) {
		_, held := ts.Node(td.Common().ID)
		finished, stored = append(finished, td), append(stored, held)
	})
	build := func(common ir.TypeCommon) ir.TypeDef { return &ir.Model{TypeCommon: common} }
	id := internNode(c.Ctx, ts, at, build, fin)
	internNode(c.Ctx, ts, at, build, fin)

	require.Len(t, finished, 1, "only the visit building the node finishes it")
	assert.Equal(t, []bool{false}, stored, "before the registry holds it")
	td, ok := ts.Node(id)
	require.True(t, ok)
	assert.Same(t, td, finished[0], "the node in hand is the node stored")
}

// TestFinisher_ThenRunsBothInOrder pins the composition the frames of a
// position build their finishing from: each in turn, a nil one skipped.
func TestFinisher_ThenRunsBothInOrder(t *testing.T) {
	t.Parallel()
	var ran []string
	first := finisher(func(ir.TypeDef) { ran = append(ran, "first") })
	second := finisher(func(ir.TypeDef) { ran = append(ran, "second") })
	var none finisher
	first.then(second)(&ir.Model{})
	none.then(first)(&ir.Model{})
	first.then(none)(&ir.Model{})
	assert.Equal(t, []string{"first", "second", "first", "first"}, ran)
	assert.Nil(t, none.then(none), "two nil finishers finish nothing")
}

// TestCensusedNode_ReportsAMissRatherThanDroppingIt pins the invariant check
// on a node a census reads but does not finish. Every ID a lowering returns
// has a node, so a miss is a compiler bug no source can provoke, and the
// census was about to keep what that node cannot hold, which would vanish.
func TestCensusedNode_ReportsAMissRatherThanDroppingIt(t *testing.T) {
	t.Parallel()
	c := service.New(4, &load.Document{Doc: &soa.OpenAPI{}}, service.Policy{})
	ts := compile.NewTypes()

	td, diags := censusedNode(c.Ctx, ts, "t/absent", "/components/schemas/Ghost")

	assert.Nil(t, td)
	require.Len(t, diags, 1, "a miss is announced, not swallowed")
	assert.Equal(t, ir.SeverityError, diags[0].Severity)
	assert.Equal(t, diag.InternalInvariant, diags[0].Code)
	assert.Equal(t, ir.Provenance{Source: 4, Pointer: "/components/schemas/Ghost"}, diags[0].Provenance)

	id := ts.PrimID(ir.PrimString)
	td, diags = censusedNode(c.Ctx, ts, id, "/components/schemas/User")
	assert.Empty(t, diags, "a hit reports nothing")
	require.NotNil(t, td)
	assert.Equal(t, id, td.Common().ID)
}

// TestUnionVariant_StopsAtTheHopCap pins naming.MaxHops through a compile.
// Each branch holds a $ref to the next and the last to T. A variant naming the
// first branch is named after T while T is within the cap, and keeps its own
// name once it is not. The node that variant holds is T's, so past the cap the
// two are named apart, as the cap's doc says.
func TestUnionVariant_StopsAtTheHopCap(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		branches int
		variant  string
	}{
		{"T within reach", naming.MaxHops - 1, "t"},
		{"T one hop past the cap", naming.MaxHops, "variant_0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var spec strings.Builder
			spec.WriteString("    Choice: {oneOf: [{$ref: '#/components/schemas/S0/oneOf/0'}, {type: integer}]}\n")
			for i := range tc.branches {
				next := "#/components/schemas/T"
				if i+1 < tc.branches {
					next = "#/components/schemas/S" + strconv.Itoa(i+1) + "/oneOf/0"
				}
				spec.WriteString("    S" + strconv.Itoa(i) + ": {oneOf: [{$ref: '" + next + "'}, {type: integer}]}\n")
			}
			spec.WriteString("    T: {type: object, properties: {t: {type: string}}}\n")
			doc, diags := LowerSpec(t, openapitest.ComponentSpec(spec.String()))
			openapitest.RequireNoErrorDiags(t, diags)

			choice, ok := doc.Types[ids.Document{}.ForPointer(ids.Ptr("components", "schemas", "Choice"))].(*ir.Union)
			require.True(t, ok, "Choice should be a union")
			variant := choice.Variants[0]
			assert.Equal(t, tc.variant, variant.Name.Hint)
			node, ok := doc.Types[variant.Type.Target]
			require.True(t, ok, "the variant's target %s is interned", variant.Type.Target)
			assert.Equal(t, "t", node.Common().Name.Hint, "the node the variant holds reaches T either way")
		})
	}
}

// TestMarkKindInferred_JoinsWhatTheNodeNames pins that the kind-inference
// marker joins what a node's provenance already names, once however often it
// is written.
func TestMarkKindInferred_JoinsWhatTheNodeNames(t *testing.T) {
	t.Parallel()
	node := &ir.Model{Provenance: ir.Provenance{Inferred: lowering.NullableInferenceHeuristic}}
	markKindInferred(node)
	markKindInferred(node)
	assert.Equal(t, ir.JoinInferred(lowering.NullableInferenceHeuristic, lowering.KindInferenceHeuristic),
		node.Provenance.Inferred)
}
