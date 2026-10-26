package pass

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// DanglingRefs is checkDanglingRefs over what one walk of doc reads, as validate
// runs it, for the external tests.
func DanglingRefs(tb testing.TB, doc *ir.Document) []ir.Diagnostic {
	tb.Helper()
	w := walkedFor(tb, doc)
	return checkDanglingRefs(w.refs, w.truncated)
}

// KeptPropIDs returns where the PropID references one walk of doc keeps for
// checkPropIDRefs to settle sit, in walk order, for the external tests.
func KeptPropIDs(tb testing.TB, doc *ir.Document) []string {
	tb.Helper()
	sites := walkedFor(tb, doc).props.sites
	paths := make([]string, 0, len(sites))
	for _, s := range sites {
		paths = append(paths, s.Path)
	}
	return paths
}

// WalkVisitors returns the visitors Validate's one walk of doc carries, in
// order, for the external tests to walk doc with themselves.
func WalkVisitors(tb testing.TB, doc *ir.Document) []ir.Visitor {
	tb.Helper()
	var visitors []ir.Visitor
	_, err := walkDocument(tb.Context(), doc, func(_ context.Context, _ any, _ string, vs ...ir.Visitor) (bool, error) {
		visitors = vs
		return false, nil
	})
	require.NoError(tb, err)
	return visitors
}

// walkedFor is what validate's one walk of doc reads under tb's context,
// failing tb unless the walk finishes.
func walkedFor(tb testing.TB, doc *ir.Document) walked {
	tb.Helper()
	w, err := walkDocument(tb.Context(), doc, ir.WalkValuesContext)
	require.NoError(tb, err)
	return w
}

// AppendTypeIDs is appendTypeIDs, for the external tests.
var AppendTypeIDs = appendTypeIDs

// ValidateWith is validate, walking the document with walk, for the external
// tests that count the walks.
var ValidateWith = validate

// Families is how many rule families Validate runs, each after an ask of its
// context.
func Families() int { return len(families(nil, walked{}, nil)) }
