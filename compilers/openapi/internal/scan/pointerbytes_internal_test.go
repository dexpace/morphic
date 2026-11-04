// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package scan

import (
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/ir"
)

// TestPointerAmplification_RefusesPastTheAllowanceAndNotAtIt pins the bound's edge: a
// document spelling exactly its allowance passes, and one byte less of
// allowance refuses it as diag.PointerAmplification at the document, naming
// what it spells, the allowance and what it writes. Thirty mappings nest under
// 40-byte keys: the mapping at level i spells 41i, its key 41(i+1), and the
// scalar at the bottom 41*30.
func TestPointerAmplification_RefusesPastTheAllowanceAndNotAtIt(t *testing.T) {
	t.Parallel()
	idx := indexOf(t, []byte(nested(strings.Repeat("k", 40), 30)+"\n"))
	const pointers, written = 41*435 + 41*465 + 41*30, 30 + 30*41 + 2
	require.Equal(t, pointers, idx.PointerBytes())
	require.Equal(t, written, idx.Written())
	locate := InSource(0)
	_, refused := pointerAmplificationWithin(locate, idx, idx.PointerBytes())
	assert.False(t, refused, "a document at its allowance is admitted")

	d, refused := pointerAmplificationWithin(locate, idx, idx.PointerBytes()-1)
	require.True(t, refused)
	assert.Equal(t, diag.PointerAmplification, d.Code)
	assert.Equal(t, ir.SeverityError, d.Severity)
	assert.Equal(t, locate(nil), d.Provenance)
	assert.Equal(t, fmt.Sprintf("the pointers naming this document's nodes total %d bytes, "+
		"past the %d-byte allowance for a document writing %d bytes", pointers, pointers-1, written), d.Message)
}

// TestPointerAmplification_ReadsTheRatioAndItsFloor pins the allowance PointerAmplification reads: a
// small document is granted the floor however deep it nests, the ratio binds
// past it, and an allowance past math.MaxInt holds there.
func TestPointerAmplification_ReadsTheRatioAndItsFloor(t *testing.T) {
	t.Parallel()
	assert.Equal(t, minPointerBytes, pointerAllowance(0))
	assert.Equal(t, minPointerBytes, pointerAllowance(minPointerBytes/maxPointerAmplification))
	assert.Equal(t, minPointerBytes+maxPointerAmplification, pointerAllowance(minPointerBytes/maxPointerAmplification+1))
	assert.Equal(t, math.MaxInt, pointerAllowance(math.MaxInt/maxPointerAmplification+1))

	deep := indexOf(t, []byte(nested("k", MaxNesting)+"\n"))
	require.Greater(t, deep.PointerBytes(), maxPointerAmplification*deep.Written(), "the ratio alone would refuse it")
	_, refused := PointerAmplification(InSource(0), deep)
	assert.False(t, refused, "the floor admits a small document nested to the bound")
}

// TestPointerAmplification_LeavesARecursiveAnchorToItsOwnRefusal pins that a document
// holding a recursive anchor is not judged on its pointers, which the index
// counts one turn round the anchor, as Nesting leaves it.
func TestPointerAmplification_LeavesARecursiveAnchorToItsOwnRefusal(t *testing.T) {
	t.Parallel()
	idx := indexOf(t, []byte("x: &a "+strings.Repeat("{k: ", 3)+"*a"+strings.Repeat("}", 3)+"\n"))
	require.NotEmpty(t, idx.AnchorCycles())
	_, refused := pointerAmplificationWithin(InSource(0), idx, 0)
	assert.False(t, refused)
}
