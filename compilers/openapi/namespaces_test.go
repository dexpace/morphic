package openapi_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/ir"
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

	used := map[string]map[string]bool{}
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
			recordSpacesUsed(used, doc)
		}
	}
	require.NotEmpty(t, declared, "no compiled document declared a vocabulary")

	want := map[string][]string{}
	for kind, spaces := range used {
		for space := range spaces {
			want[kind] = append(want[kind], space)
		}
		slices.Sort(want[kind])
	}
	assert.Empty(t, cmp.Diff(want, declared),
		"declared and used namespaces differ (-used by the corpus +declared by the compiler)")
}

// recordSpacesUsed adds the kind and namespace of every ID doc declares to used,
// leaving out the primitive namespace, which ir owns and no compiler declares.
func recordSpacesUsed(used map[string]map[string]bool, doc *ir.Document) {
	decls, _ := ir.DeclaredIDs(doc)
	for _, d := range decls {
		kind, _, found := strings.Cut(d.ID, ir.IDSeparator)
		if !found || !slices.Contains(ir.IDKinds(), kind) {
			continue
		}
		space, ok := ir.IDSpace(kind, d.ID)
		if !ok || (kind == ir.IDKindType && space == ir.IDSpacePrim) {
			continue
		}
		if used[kind] == nil {
			used[kind] = map[string]bool{}
		}
		used[kind][space] = true
	}
}
