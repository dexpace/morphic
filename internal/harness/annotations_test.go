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
// of the slot it names.
func TestConstBlock_TiesToAnnotationsAndSiteKinds(t *testing.T) {
	t.Parallel()
	declared := parseDeclaredConsts(t)

	assert.ElementsMatch(t, declared["Annotation"], stringsOf(harness.Annotations()))
	assert.ElementsMatch(t, declared["SiteKind"], stringsOf(harness.SiteKinds()))
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
// string value from gd into out. Every Annotation/SiteKind constant in
// annotations.go repeats its type on each line and assigns a plain string
// literal; a spec typed some other way is not part of this taxonomy and is
// skipped.
func collectConstSpecs(t *testing.T, gd *ast.GenDecl, out map[string][]string) {
	t.Helper()
	for _, spec := range gd.Specs {
		vs, ok := spec.(*ast.ValueSpec)
		require.True(t, ok, "const decl spec is not a ValueSpec: %#v", spec)
		typeIdent, ok := vs.Type.(*ast.Ident)
		if !ok {
			continue // not a typed const (e.g. an untyped numeric constant): skip
		}
		require.Len(t, vs.Values, 1, "%s: expected exactly one value", vs.Names)
		lit, ok := vs.Values[0].(*ast.BasicLit)
		require.True(t, ok && lit.Kind == token.STRING, "%s: value is not a string literal", vs.Names)
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
