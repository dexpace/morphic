package harness_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/internal/harness"
)

// annotationTypeName and siteKindTypeName are the only two type spellings a
// const in annotations.go's two blocks may declare. collectConstSpecs checks
// each spec's type identifier against these literally, rather than bucketing
// by whatever identifier appears, precisely so that a same-file alias of
// either type cannot bucket a constant under a key
// TestConstBlock_TiesToAnnotationsAndSiteKinds never inspects.
const (
	annotationTypeName = "Annotation"
	siteKindTypeName   = "SiteKind"
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

// TestConstBlock_TiesToAnnotationsAndSiteKinds makes real the claim in
// annotations.go's doc comments that adding a slot to either const block
// widens what annotation retention checks: it parses annotations.go with
// go/ast (the same approach internal/archtest/arch_test.go uses for import
// rules) and requires every declared Annotation and SiteKind constant's
// value to appear in Annotations()/SiteKinds(), the functions Cells() builds
// the retention grid from. Without this test, a constant could be added to
// either block and never wired into Cells(), leaving the grid silently short
// of the slot it names. The check is scoped to that one file: an Annotation-
// or SiteKind-typed constant declared in a different file of this package
// would not be seen by it.
func TestConstBlock_TiesToAnnotationsAndSiteKinds(t *testing.T) {
	t.Parallel()
	declared := parseDeclaredConsts(t)

	assert.ElementsMatch(t, declared[annotationTypeName], stringsOf(harness.Annotations()))
	assert.ElementsMatch(t, declared[siteKindTypeName], stringsOf(harness.SiteKinds()))
}

// parseDeclaredConsts parses annotations.go — found relative to this test
// file, since both live in internal/harness — and returns, per declared Go
// type name, the literal string value of every const declared with that
// type.
func parseDeclaredConsts(t *testing.T) map[string][]string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	require.True(t, ok, "runtime.Caller failed")
	src := filepath.Join(filepath.Dir(thisFile), "annotations.go")

	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, src, nil, 0)
	require.NoError(t, err)

	out := make(map[string][]string)
	for _, decl := range f.Decls {
		if gd, ok := decl.(*ast.GenDecl); ok && gd.Tok == token.CONST {
			collectConstSpecs(t, gd, out)
		}
	}
	return out
}

// collectConstSpecs appends each ValueSpec's declared type name and literal
// string value from gd into out. Every spec here is required to have the
// form `Name Type = "literal"`, with Type spelled literally as Annotation or
// SiteKind — never a same-file alias of either (`type Ann = Annotation`),
// which would still convert to Annotation at every usage site but bucket
// under a key ("Ann") this test never inspects, since only
// annotationTypeName and siteKindTypeName are compared against
// Annotations()/SiteKinds() below. This file holds nothing but
// annotation-retention vocabulary, so there is no legitimate reason for a
// const here to be untyped, valueless (as in an iota block's repeat-sugar),
// aliased, or assigned via a conversion or expression rather than written as
// a literal (e.g. Annotation("foo")). Any of those fails loudly instead of
// being silently skipped: skipping would let such a constant join the
// taxonomy at every usage site without this test ever recording it.
func collectConstSpecs(t *testing.T, gd *ast.GenDecl, out map[string][]string) {
	t.Helper()
	for _, spec := range gd.Specs {
		vs, ok := spec.(*ast.ValueSpec)
		require.True(t, ok, "const decl spec is not a ValueSpec: %#v", spec)

		typeIdent, hasType := vs.Type.(*ast.Ident)
		require.True(t, hasType, "%s: const must declare its type explicitly "+
			"(Annotation or SiteKind); this file holds nothing else", vs.Names)
		require.True(t, typeIdent.Name == annotationTypeName || typeIdent.Name == siteKindTypeName,
			"%s: const's declared type is %q, not literally Annotation or SiteKind; a "+
				"same-file alias of either would bucket here under the alias's name and "+
				"never be compared against Annotations()/SiteKinds()", vs.Names, typeIdent.Name)

		require.Len(t, vs.Values, 1, "%s: expected exactly one literal value", vs.Names)
		lit, isLit := vs.Values[0].(*ast.BasicLit)
		require.True(t, isLit && lit.Kind == token.STRING,
			"%s: value must be a string literal, not a conversion or expression", vs.Names)

		value, err := strconv.Unquote(lit.Value)
		require.NoError(t, err)
		out[typeIdent.Name] = append(out[typeIdent.Name], value)
	}
}

// stringsOf converts a slice of a defined string type to plain strings so it
// can be compared against parseDeclaredConsts' output.
func stringsOf[T ~string](vs []T) []string {
	out := make([]string, len(vs))
	for i, v := range vs {
		out[i] = string(v)
	}
	return out
}
