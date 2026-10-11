package schema

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/ir"
)

// TestMarkSecretUses_ANilDocumentIsANoOp pins the boundary guard.
func TestMarkSecretUses_ANilDocumentIsANoOp(t *testing.T) {
	t.Parallel()
	assert.Empty(t, MarkSecretUses(lowering.Ctx{}, nil))
}

// TestMarkSecretUses_ReportsAPropertyItCannotSet pins that a property reached
// by copy is reported rather than left unmarked in silence.
func TestMarkSecretUses_ReportsAPropertyItCannotSet(t *testing.T) {
	t.Parallel()
	reg := ir.TypeRegistry{"t/pw": &ir.Scalar{TypeCommon: ir.TypeCommon{ID: "t/pw", Sensitive: true}}}
	diags := markSecretUses(lowering.Ctx{}, ir.Property{Type: ir.TypeRef{Target: "t/pw"}}, reg)
	require.Len(t, diags, 1)
	assert.Equal(t, diag.InternalInvariant, diags[0].Code)
	assert.Contains(t, diags[0].Message, "held by value")
}

// TestMarkSecretUses_ReportsATruncatedWalk pins that a walk the depth cap cut
// short is reported, since the properties past the cap went unread.
func TestMarkSecretUses_ReportsATruncatedWalk(t *testing.T) {
	t.Parallel()
	var deep any = ir.Property{}
	for range ir.MaxWalkDepth {
		deep = []any{deep}
	}
	diags := markSecretUses(lowering.Ctx{}, deep, ir.TypeRegistry{})
	require.Len(t, diags, 1)
	assert.Equal(t, diag.InternalInvariant, diags[0].Code)
	assert.Contains(t, diags[0].Message, "MaxWalkDepth")
}

// TestReachesSensitive_StopsWhereTheChainEnds pins each way the Base chain
// ends: a Sensitive node, a target the registry lacks, a kind with no Base,
// and a cycle, which the registry-sized bound cuts.
func TestReachesSensitive_StopsWhereTheChainEnds(t *testing.T) {
	t.Parallel()
	ref := func(id ir.TypeID) *ir.TypeRef { return &ir.TypeRef{Target: id} }
	reg := ir.TypeRegistry{
		"t/pw":    &ir.Scalar{TypeCommon: ir.TypeCommon{ID: "t/pw", Sensitive: true}},
		"t/alias": &ir.Scalar{TypeCommon: ir.TypeCommon{ID: "t/alias"}, Base: ref("t/pw")},
		"t/wrap":  &ir.Model{TypeCommon: ir.TypeCommon{ID: "t/wrap"}, Base: ref("t/alias")},
		"t/list":  &ir.List{TypeCommon: ir.TypeCommon{ID: "t/list"}, Elem: ir.TypeRef{Target: "t/pw"}},
		"t/a":     &ir.Scalar{TypeCommon: ir.TypeCommon{ID: "t/a"}, Base: ref("t/b")},
		"t/b":     &ir.Scalar{TypeCommon: ir.TypeCommon{ID: "t/b"}, Base: ref("t/a")},
		"t/gone":  &ir.Scalar{TypeCommon: ir.TypeCommon{ID: "t/gone"}, Base: ref("t/missing")},
	}
	for id, want := range map[ir.TypeID]bool{
		"t/pw": true, "t/alias": true, "t/wrap": true,
		"t/list": false, "t/a": false, "t/gone": false, "t/missing": false,
	} {
		assert.Equal(t, want, reachesSensitive(reg, ir.TypeRef{Target: id}), "%s", id)
	}
}
