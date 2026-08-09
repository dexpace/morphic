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

// TestWriteGolden_RejectsBadInput pins the two preconditions. The nil-document
// case is the one with teeth: an unguarded WriteGolden encoded nil to "null",
// left that on disk as a golden, and CompareGolden then matched every later nil
// document against it — a snapshot asserting nothing, indistinguishable from a
// passing one.
func TestWriteGolden_RejectsBadInput(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	cases := map[string]struct {
		path    string
		doc     *ir.Document
		wantErr string
	}{
		"empty path":   {path: "", doc: &ir.Document{Name: "x"}, wantErr: "empty path"},
		"nil document": {path: filepath.Join(dir, "nil.golden.json"), doc: nil, wantErr: "nil document"},
	}
	// Sequential subtests: the file-absence assertion below has to observe the
	// state the nil-document case left, and a parallel subtest runs after its
	// parent's body returns.
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			err := irtest.WriteGolden(tc.path, tc.doc)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantErr)
		})
	}

	_, err := os.Stat(filepath.Join(dir, "nil.golden.json"))
	assert.ErrorIs(t, err, os.ErrNotExist, "a refused write leaves no golden behind")
}

func TestCompareGolden_WritesThenMatches(t *testing.T) {
	// Not parallel: exercises the -update path via WriteGolden.
	dir := t.TempDir()
	path := filepath.Join(dir, "doc.golden.json")
	doc := &ir.Document{IRVersion: ir.IRVersion, Name: "g", Version: "1"}

	// First write the golden explicitly, then compare against it.
	require.NoError(t, irtest.WriteGolden(path, doc))
	irtest.CompareGolden(t, path, doc)

	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.True(t, len(raw) > 0 && raw[len(raw)-1] == '\n', "golden must end in newline")
}
