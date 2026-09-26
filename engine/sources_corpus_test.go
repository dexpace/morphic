package engine_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/engine"
	"github.com/dexpace/morphic/internal/testspec"
	"github.com/dexpace/morphic/ir"
)

// corpusSpecFiles returns every spec file under root, in filepath.WalkDir's
// lexical order: a .yaml or .yml file, or a .json one that is not a
// .golden.json IR snapshot. It mirrors internal/harness's own filter rather
// than importing it — engine may not import internal/harness (layering), and
// what is swept here is a different property than what the oracle checks.
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

// assertSourcesInvariant holds res to the invariant this PR establishes: every
// diagnostic's Provenance.Source is either ir.NoSource or an index res.Sources
// actually has an entry for — on a successful compile as much as on a refusal.
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
// through the engine, with and without SkipValidate, and holds every run's
// diagnostics to the invariant this PR establishes. pass/validate_corpus_test.go
// walks the conformance slice of the same tree for a different property
// (referential integrity); this is the source-table property, which a
// successful compile can violate as easily as a refusal, and which
// SkipValidate can change the diagnostic list of.
func TestEngine_RunResultSourcesOverTheCorpus(t *testing.T) {
	t.Parallel()
	eng, err := engine.New()
	require.NoError(t, err)

	for _, path := range corpusSpecFiles(t, "../testdata") {
		t.Run(filepath.Base(path), func(t *testing.T) {
			t.Parallel()
			for _, skip := range []bool{false, true} {
				res, runErr := eng.Run(t.Context(), path, engine.RunOptions{SkipValidate: skip})
				require.NoError(t, runErr, "skip-validate=%v", skip)
				require.NotNil(t, res)
				assertSourcesInvariant(t, path, res)
			}
		})
	}
}

// TestEngine_RunResultSourcesOverTheCorpus_OverlayRefusal covers the one case
// no fixture under testdata/ can produce by itself: an overlay is never a file
// the corpus names directly, since it reaches the compiler only through
// RunOptions.CompilerOptions, so the invariant is checked here instead of
// folded into the sweep above.
func TestEngine_RunResultSourcesOverTheCorpus_OverlayRefusal(t *testing.T) {
	t.Parallel()
	// The same recursive-anchor shape TestEngine_RunResultSourcesOnARefusedOverlay
	// uses: an anchor inside the update aliases itself, which the overlay
	// library's clone has no base case for.
	const anchorCycle = "overlay: 1.0.0\ninfo: {title: o, version: \"1\"}\n" +
		"actions:\n  - target: $.info\n    update: {p: &a [*a]}\n"

	eng, err := engine.New()
	require.NoError(t, err)
	spec := writeSpec(t, testspec.Tiny)
	overlay := writeNamed(t, "overlay.yaml", anchorCycle)

	for _, skip := range []bool{false, true} {
		res, err := eng.Run(t.Context(), spec, engine.RunOptions{
			SkipValidate:    skip,
			CompilerOptions: map[string]string{"overlay": overlay},
		})
		require.NoError(t, err, "skip-validate=%v", skip)
		require.NotNil(t, res)
		require.Nil(t, res.Document, "the overlay's own cycle refuses the compile")
		assertSourcesInvariant(t, spec, res)
	}
}
