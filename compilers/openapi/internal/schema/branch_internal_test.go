package schema

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
)

// TestRefBranchTarget_InlineBranchNamesNothing pins that an inline branch has
// no target, which is the answer a caller that did not filter to $refs gets.
func TestRefBranchTarget_InlineBranchNamesNothing(t *testing.T) {
	t.Parallel()
	l, _ := loweredFor(t, openapitest.ComponentSpec(`    M:
      allOf:
        - {type: object}
`))
	decl, ok := l.ctx.Doc.Components.Schemas.Get("M")
	require.True(t, ok)
	assert.Nil(t, refBranchTarget(l.ctx.Bindings(), decl.GetSchema().GetAllOf()[0]))
}

func TestIsRefBranch_Nil(t *testing.T) {
	t.Parallel()
	assert.False(t, isRefBranch(nil))
}
