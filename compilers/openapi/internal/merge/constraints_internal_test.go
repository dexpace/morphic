package merge

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// TestMerger_MergeConstraintsKeepsBothSidesBounds covers the merge half rather
// than the conflict half: constraints that do not disagree combine.
func TestMerger_MergeConstraintsKeepsBothSidesBounds(t *testing.T) {
	t.Parallel()
	got := mergeConstraints(
		&ir.Constraints{MaxLength: new(int64(10))},
		&ir.Constraints{MinLength: new(int64(2))},
	)
	require.NotNil(t, got)
	require.NotNil(t, got.MaxLength)
	require.NotNil(t, got.MinLength)
	assert.Equal(t, int64(10), *got.MaxLength)
	assert.Equal(t, int64(2), *got.MinLength, "a bound only the second side declares survives the merge")
}
