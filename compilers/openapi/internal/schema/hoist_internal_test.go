package schema

import (
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/naming"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
)

// TestCommonFor_NamesOnlyAComponent pins the split every hoisted node carries.
// A top-level component schema is named from the source; any deeper position is
// anonymous and carries only the hint its coordinate names it by. Getting this backwards
// gives an emitter a name to render where the source declared none, or drops the
// one it did declare.
func TestCommonFor_NamesOnlyAComponent(t *testing.T) {
	t.Parallel()
	c := lowering.Ctx{SrcIndex: 2}

	named := commonFor(c, "t/x", "/components/schemas/User")
	assert.False(t, named.Anonymous, "a component schema is a declaration")
	assert.Equal(t, "User", named.Name.Source)
	assert.Equal(t, "user", named.Name.Canonical, "the framework supplies the neutral words")
	assert.Empty(t, named.Name.Hint, "a named node needs no hint")

	anon := commonFor(c, "t/y", "/components/schemas/User/properties/id")
	assert.True(t, anon.Anonymous, "an inline position declares no name")
	assert.Empty(t, anon.Name.Source)
	assert.Equal(t, "id", anon.Name.Hint, "only the hint survives: the property's key")
}

// TestCommonFor_StampsTheDeclaringPointer pins that the node records where it
// was written, with the compile's own source index — the pair every consumer
// traces a type back through.
func TestCommonFor_StampsTheDeclaringPointer(t *testing.T) {
	t.Parallel()
	got := commonFor(lowering.Ctx{SrcIndex: 5}, "t/x", "/components/schemas/User")

	assert.Equal(t, ir.TypeID("t/x"), got.ID)
	assert.Equal(t, ir.Provenance{Source: 5, Pointer: "/components/schemas/User"}, got.Provenance)
}

// TestInternNode_DerivesTheIDAndInternsOnce pins the single hoisting entry
// point: the ID comes from the pointer, and a pointer visited twice yields the
// same node rather than a second one. Re-entering build is what a recursive
// schema would do forever.
func TestInternNode_DerivesTheIDAndInternsOnce(t *testing.T) {
	t.Parallel()
	c := lowering.Ctx{SrcIndex: 0}
	ts := compile.NewTypes()
	const at = "/components/schemas/User"

	var built int
	build := func(common ir.TypeCommon) ir.TypeDef {
		built++
		return &ir.Model{TypeCommon: common}
	}

	first := internNode(c, ts, at, build)
	second := internNode(c, ts, at, build)

	assert.Equal(t, ids.Document{}.ForPointer(at), first, "the ID is derived from the pointer")
	assert.Equal(t, first, second, "a second visit returns the interned ID")
	assert.Equal(t, 1, built, "and does not build the node again")

	td, ok := ts.Node(first)
	require.True(t, ok, "the node is registered under the ID it returned")
	assert.Equal(t, first, td.Common().ID, "and carries that same ID")
}

// TestRegisteredNode_ReportsAMissRatherThanDroppingIt pins the invariant check.
// Outside its build, every ID interning returns has a node, so a miss is a
// compiler bug no source can provoke — and the caller was about to attach docs
// or preserved constructs to that node, which would vanish without a word.
func TestRegisteredNode_ReportsAMissRatherThanDroppingIt(t *testing.T) {
	t.Parallel()
	c := lowering.Ctx{SrcIndex: 4}
	ts := compile.NewTypes()

	_, ok, diags := registeredNode(c, ts, "t/absent", "/components/schemas/Ghost")

	assert.False(t, ok)
	require.Len(t, diags, 1, "a miss is announced, not swallowed")
	assert.Equal(t, ir.SeverityError, diags[0].Severity)
	assert.Equal(t, diag.InternalInvariant, diags[0].Code)
	assert.Equal(t, ir.Provenance{Source: 4, Pointer: "/components/schemas/Ghost"}, diags[0].Provenance)

	id := internNode(c, ts, "/components/schemas/User", func(cm ir.TypeCommon) ir.TypeDef {
		return &ir.Model{TypeCommon: cm}
	})
	td, ok, diags := registeredNode(c, ts, id, "/components/schemas/User")
	assert.True(t, ok)
	assert.Empty(t, diags, "a hit reports nothing")
	assert.Equal(t, id, td.Common().ID)
}

// TestRegisteredNode_LeavesANodeStillBeingBuiltToItsBuilder pins the one miss
// that is no bug: a revisit while the node's build is still running further up
// the walk. That frame finishes the node once its build returns, so the
// revisit hands back nothing and reports nothing (GitHub #749).
func TestRegisteredNode_LeavesANodeStillBeingBuiltToItsBuilder(t *testing.T) {
	t.Parallel()
	c := lowering.Ctx{}
	ts := compile.NewTypes()
	const at = "/components/schemas/User"

	var midBuild bool
	var midBuildDiags []ir.Diagnostic
	id := internNode(c, ts, at, func(cm ir.TypeCommon) ir.TypeDef {
		_, midBuild, midBuildDiags = registeredNode(c, ts, cm.ID, at)
		return &ir.Model{TypeCommon: cm}
	})
	assert.False(t, midBuild, "there is no node to hand back yet")
	assert.Empty(t, midBuildDiags, "and that is not a broken invariant")

	_, ok, diags := registeredNode(c, ts, id, at)
	assert.True(t, ok, "once the build returns, the node is there")
	assert.Empty(t, diags)
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
