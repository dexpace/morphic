package openapi

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/ir"
)

// TestCommonFor_NamesOnlyAComponent pins the split every hoisted node carries.
// A top-level component schema is named from the source; any deeper position is
// anonymous and carries only its context-derived hint. Getting this backwards
// gives an emitter a name to render where the source declared none, or drops the
// one it did declare.
func TestCommonFor_NamesOnlyAComponent(t *testing.T) {
	t.Parallel()
	c := lowerCtx{SrcIndex: 2}

	named := commonFor(c, "t/x", "/components/schemas/User", "ignored_hint")
	assert.False(t, named.Anonymous, "a component schema is a declaration")
	assert.Equal(t, "User", named.Name.Source)
	assert.Equal(t, "user", named.Name.Canonical, "the framework supplies the neutral words")
	assert.Empty(t, named.Name.Hint, "a named node needs no hint")

	anon := commonFor(c, "t/y", "/components/schemas/User/properties/id", "user_id")
	assert.True(t, anon.Anonymous, "an inline position declares no name")
	assert.Empty(t, anon.Name.Source)
	assert.Equal(t, "user_id", anon.Name.Hint, "only the hint survives")
}

// TestCommonFor_StampsTheDeclaringPointer pins that the node records where it
// was written, with the compile's own source index — the pair every consumer
// traces a type back through.
func TestCommonFor_StampsTheDeclaringPointer(t *testing.T) {
	t.Parallel()
	got := commonFor(lowerCtx{SrcIndex: 5}, "t/x", "/components/schemas/User", "")

	assert.Equal(t, ir.TypeID("t/x"), got.ID)
	assert.Equal(t, ir.Provenance{Source: 5, Pointer: "/components/schemas/User"}, got.Provenance)
}

// TestInternNode_DerivesTheIDAndInternsOnce pins the single hoisting entry
// point: the ID comes from the pointer, and a pointer visited twice yields the
// same node rather than a second one. Re-entering build is what a recursive
// schema would do forever.
func TestInternNode_DerivesTheIDAndInternsOnce(t *testing.T) {
	t.Parallel()
	c := lowerCtx{SrcIndex: 0}
	ts := compile.NewTypes(0)
	const at = "/components/schemas/User"

	var built int
	build := func(common ir.TypeCommon) ir.TypeDef {
		built++
		return &ir.Model{TypeCommon: common}
	}

	first := internNode(c, ts, at, "", build)
	second := internNode(c, ts, at, "", build)

	assert.Equal(t, ids.ForPointer(at), first, "the ID is derived from the pointer")
	assert.Equal(t, first, second, "a second visit returns the interned ID")
	assert.Equal(t, 1, built, "and does not build the node again")

	td, ok := ts.Node(first)
	require.True(t, ok, "the node is registered under the ID it returned")
	assert.Equal(t, first, td.Common().ID, "and carries that same ID")
}

// TestRegisteredNode_ReportsAMissRatherThanDroppingIt pins the invariant check.
// Interning records a pointer's ID and its node together, so a miss is a
// compiler bug no source can provoke — and the caller was about to attach docs
// or preserved constructs to that node, which would vanish without a word.
func TestRegisteredNode_ReportsAMissRatherThanDroppingIt(t *testing.T) {
	t.Parallel()
	c := lowerCtx{SrcIndex: 4}
	ts := compile.NewTypes(0)

	_, ok, diags := registeredNode(c, ts, "t/absent", "/components/schemas/Ghost")

	assert.False(t, ok)
	require.Len(t, diags, 1, "a miss is announced, not swallowed")
	assert.Equal(t, ir.SeverityError, diags[0].Severity)
	assert.Equal(t, diag.InternalInvariant, diags[0].Code)
	assert.Equal(t, ir.Provenance{Source: 4, Pointer: "/components/schemas/Ghost"}, diags[0].Provenance)

	id := internNode(c, ts, "/components/schemas/User", "", func(cm ir.TypeCommon) ir.TypeDef {
		return &ir.Model{TypeCommon: cm}
	})
	td, ok, diags := registeredNode(c, ts, id, "/components/schemas/User")
	assert.True(t, ok)
	assert.Empty(t, diags, "a hit reports nothing")
	assert.Equal(t, id, td.Common().ID)
}
