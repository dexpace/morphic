package irtest_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irtest"
)

func TestCompareGolden_WritesThenMatches(t *testing.T) {
	// Not parallel: exercises the -update path via WriteGolden.
	dir := t.TempDir()
	path := filepath.Join(dir, "doc.golden.json")
	doc := &ir.Document{IRVersion: "0.1.0", Name: "g", Version: "1"}

	// First write the golden explicitly, then compare against it.
	require.NoError(t, irtest.WriteGolden(path, doc))
	irtest.CompareGolden(t, path, doc)

	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.True(t, len(raw) > 0 && raw[len(raw)-1] == '\n', "golden must end in newline")
}
