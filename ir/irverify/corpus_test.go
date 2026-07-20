package irverify_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/ir/irverify"
)

// TestVerify_ConformanceCorpusIsClean compiles every conformance spec and asserts
// the produced IR has zero structural violations. A violation here is a compiler
// bug, not a spec problem, so the failure names the offending spec and codes.
func TestVerify_ConformanceCorpusIsClean(t *testing.T) {
	dir := filepath.FromSlash("../../testdata/conformance/openapi")
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)

	var specs []string
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".yaml" {
			specs = append(specs, e.Name())
		}
	}
	require.NotEmpty(t, specs, "no conformance specs found under %s; the corpus path may have moved", dir)

	for _, name := range specs {
		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join(dir, name))
			require.NoError(t, err)

			doc, _, err := openapi.New().Compile(t.Context(),
				[]compilers.Source{{Path: name, Data: data}}, compilers.Options{})
			require.NoError(t, err)
			require.NotNil(t, doc)

			vs := irverify.Verify(doc)
			require.Empty(t, vs, "irverify violations on %s: %+v", name, vs)
		})
	}
}
