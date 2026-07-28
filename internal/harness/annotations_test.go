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

// TestMissingCells_IgnoresUnknownCells checks the claim in MissingCells' doc
// comment: a typo'd cell cannot mask a real gap. It covers every cell but
// one, adds a cell matching no real annotation or site kind, and requires
// that the genuinely missing cell still comes back — the bogus entry must not
// be mistaken for it.
func TestMissingCells_IgnoresUnknownCells(t *testing.T) {
	t.Parallel()
	all := harness.Cells()
	require.NotEmpty(t, all)

	omitted := all[0]
	covered := append([]harness.Cell{}, all[1:]...)
	covered = append(covered, harness.Cell{Annotation: "invented", SiteKind: "nowhere"})

	assert.Equal(t, []harness.Cell{omitted}, harness.MissingCells(covered))
}

// TestMissingCells_UnknownCellsDoNotAppearInResult covers the weaker, adjacent
// property that motivated the case above: a set that already covers every
// real cell stays fully covered when an unknown cell is added on top, so the
// unknown entry cannot itself surface as a false positive. Kept separately
// because it exercises covered outnumbering Cells(), which the exact one-gap
// case above does not.
func TestMissingCells_UnknownCellsDoNotAppearInResult(t *testing.T) {
	t.Parallel()
	covered := append(harness.Cells(), harness.Cell{Annotation: "invented", SiteKind: "nowhere"})
	assert.Empty(t, harness.MissingCells(covered))
}
