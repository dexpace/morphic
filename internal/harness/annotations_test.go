package harness_test

import (
	"go/types"
	"slices"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/internal/decls"
	"github.com/dexpace/morphic/internal/harness"
)

// annotationTypeName and siteKindTypeName are the two types whose constants
// make up the annotation vocabulary.
const (
	annotationTypeName = "Annotation"
	siteKindTypeName   = "SiteKind"
)

// harnessPackage is the harness package type-checked, loaded once per test
// binary.
var harnessPackage = sync.OnceValues(func() (*decls.Package, error) {
	return decls.Load("github.com/dexpace/morphic/internal/harness")
})

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
	covered := slices.Clone(all[1:])
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

// TestConstBlock_TiesToAnnotationsAndSiteKinds requires every Annotation and
// SiteKind constant the harness package declares to appear in
// Annotations()/SiteKinds(), the functions Cells() builds the retention grid
// from. Without it a constant could be added and never wired into Cells(),
// leaving the grid short of the slot it names. The constants are read
// type-checked, so one in another file, typed through an alias or written as
// an expression is seen as the compiler sees it.
func TestConstBlock_TiesToAnnotationsAndSiteKinds(t *testing.T) {
	t.Parallel()
	assert.ElementsMatch(t, declaredValues(t, annotationTypeName), stringsOf(harness.Annotations()))
	assert.ElementsMatch(t, declaredValues(t, siteKindTypeName), stringsOf(harness.SiteKinds()))
}

// TestConstBlock_VocabularyFilesHoldNothingElse requires every constant in a
// file that declares Annotation or SiteKind to have one of the two types. An
// untyped constant written there is not skipped: it would sit beside the
// vocabulary without ever being compared against Annotations()/SiteKinds().
func TestConstBlock_VocabularyFilesHoldNothingElse(t *testing.T) {
	t.Parallel()
	p, err := harnessPackage()
	require.NoError(t, err)

	vocabulary := make([]*types.Named, 0, 2)
	homes := make(map[string]bool, 2)
	for _, name := range []string{annotationTypeName, siteKindTypeName} {
		named, namedErr := p.Named(name)
		require.NoError(t, namedErr)
		vocabulary = append(vocabulary, named)
		homes[p.Fset.Position(named.Obj().Pos()).Filename] = true
	}

	for c := range p.Objects[*types.Const]() {
		if !homes[p.Fset.Position(c.Pos()).Filename] {
			continue
		}
		ofVocabulary := slices.ContainsFunc(vocabulary, func(named *types.Named) bool {
			return types.Identical(c.Type(), named)
		})
		assert.True(t, ofVocabulary,
			"%s has type %s, not Annotation or SiteKind; the file holds nothing else", c.Name(), c.Type())
	}
}

// declaredValues returns the value of every constant the harness package
// declares with the named type, in declaration order.
func declaredValues(t *testing.T, typeName string) []string {
	t.Helper()
	p, err := harnessPackage()
	require.NoError(t, err)
	consts, err := p.StringConsts(typeName)
	require.NoError(t, err)
	require.NotEmpty(t, consts, "the harness package must declare %s constants", typeName)

	out := make([]string, len(consts))
	for i, c := range consts {
		out[i] = c.Value
	}
	return out
}

// stringsOf converts a slice of a defined string type to plain strings so it
// can be compared against declaredValues' output.
func stringsOf[T ~string](vs []T) []string {
	out := make([]string, len(vs))
	for i, v := range vs {
		out[i] = string(v)
	}
	return out
}
