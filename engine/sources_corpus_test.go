package engine_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/engine"
	"github.com/dexpace/morphic/ir"
)

// corpusSpecFiles returns every spec file under root, in filepath.WalkDir's
// lexical order: a .yaml, .yml or .json file that is not a .golden.json IR
// snapshot. It is the filter internal/harness sweeps a directory with, restated
// because that package does not export it.
func corpusSpecFiles(t *testing.T, root string) []string {
	t.Helper()
	var files []string
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() || strings.HasSuffix(p, ".golden.json") {
			return nil
		}
		switch strings.ToLower(filepath.Ext(p)) {
		case ".yaml", ".yml", ".json":
			files = append(files, p)
		}
		return nil
	})
	require.NoError(t, err)
	require.NotEmpty(t, files, "corpus directory must contain spec files")
	return files
}

// assertSourcesInvariant holds res to Result's contract: every diagnostic's
// Provenance.Source is either ir.NoSource or an index res.Sources has an entry
// for, on a successful compile as much as on a refusal.
func assertSourcesInvariant(t *testing.T, path string, res *engine.Result) {
	t.Helper()
	for _, d := range res.Diagnostics {
		if d.Provenance.Source == ir.NoSource {
			continue
		}
		assert.True(t, d.Provenance.Source >= 0 && d.Provenance.Source < len(res.Sources),
			"%s: diagnostic %+v must index Sources %+v or carry ir.NoSource", path, d, res.Sources)
	}
}

// TestEngine_RunResultSourcesOverTheCorpus sweeps every spec under testdata/
// through the engine and holds each run's diagnostics to Result's contract.
// The sweep reaches the corpus's refusals, which no Document's table covers;
// an overlay is never a corpus file, so its refusal is pinned by
// TestEngine_RunResultSourcesOnARefusedOverlay instead. The validate pass is
// skipped because its findings all carry ir.NoSource.
func TestEngine_RunResultSourcesOverTheCorpus(t *testing.T) {
	t.Parallel()
	eng, err := engine.New()
	require.NoError(t, err)

	for _, path := range corpusSpecFiles(t, "../testdata") {
		t.Run(filepath.Base(path), func(t *testing.T) {
			t.Parallel()
			res, err := eng.Run(t.Context(), path, engine.RunOptions{SkipValidate: true})
			require.NoError(t, err)
			require.NotNil(t, res)
			assertSourcesInvariant(t, path, res)
		})
	}
}
