package harness_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/internal/harness"
)

func TestCells_IsFullCrossProduct(t *testing.T) {
	t.Parallel()
	cells := harness.Cells()
	assert.Len(t, cells, len(harness.Annotations())*len(harness.SiteKinds()))

	seen := make(map[harness.Cell]bool, len(cells))
	for _, c := range cells {
		require.False(t, seen[c], "duplicate cell %+v", c)
		seen[c] = true
	}
}

func TestMissingCells_ReportsUncoveredInStableOrder(t *testing.T) {
	t.Parallel()
	all := harness.Cells()
	require.Greater(t, len(all), 2)

	covered := all[2:]
	missing := harness.MissingCells(covered)
	assert.Equal(t, all[:2], missing)
}

func TestMissingCells_EmptyWhenFullyCovered(t *testing.T) {
	t.Parallel()
	assert.Empty(t, harness.MissingCells(harness.Cells()))
}

func TestMissingCells_IgnoresUnknownCells(t *testing.T) {
	t.Parallel()
	covered := append(harness.Cells(), harness.Cell{Annotation: "invented", SiteKind: "nowhere"})
	assert.Empty(t, harness.MissingCells(covered))
}
