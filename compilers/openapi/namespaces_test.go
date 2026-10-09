package openapi_test

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/ir/irtest"
)

// TestNamespaces_DeclaredAreExactlyWhatTheCorpusUses holds the compiler's
// declared vocabulary to the namespaces its IDs really live in, in the direction
// irverify cannot see. A namespace used and not declared is irverify's to report
// on every document; one declared and never used is a loophole that would admit
// a glued path spelled the same, and nothing else notices it.
//
// The corpus is every committed spec, compiled under each grouping strategy,
// because the path-prefix groups exist only under the second and a vocabulary
// listing them would otherwise look unused.
func TestNamespaces_DeclaredAreExactlyWhatTheCorpusUses(t *testing.T) {
	t.Parallel()
	specs, err := filepath.Glob("../../testdata/*/openapi/*.yaml")
	require.NoError(t, err)
	require.NotEmpty(t, specs, "the corpus must exist, or this test holds nothing to anything")

	used := map[string][]string{}
	var declared map[string][]string
	for _, spec := range specs {
		data, err := os.ReadFile(spec)
		require.NoError(t, err)
		for _, grouping := range []openapi.GroupingStrategy{openapi.GroupByTags, openapi.GroupByPathPrefix} {
			doc, _, err := openapi.New().Compile(t.Context(),
				[]compilers.Source{{Path: filepath.Base(spec), Data: data}},
				compilers.Options{FormatOptions: openapi.Options{Grouping: grouping}})
			require.NoError(t, err, spec)
			if doc == nil {
				continue // a spec refused outright has no IDs to hold anything to
			}
			if declared == nil {
				declared = doc.IDSpaces
			}
			assert.Equal(t, declared, doc.IDSpaces, "%s declares a vocabulary of its own", spec)
			for kind, spaces := range irtest.SpacesUsed(doc) {
				used[kind] = append(used[kind], spaces...)
			}
		}
	}
	require.NotEmpty(t, declared, "no compiled document declared a vocabulary")

	for kind, spaces := range used {
		slices.Sort(spaces)
		used[kind] = slices.Compact(spaces)
	}
	assert.Empty(t, cmp.Diff(used, declared),
		"declared and used namespaces differ (-used by the corpus +declared by the compiler)")
}
