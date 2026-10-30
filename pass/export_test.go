// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package pass

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irfacts"
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
	props := walkedFor(tb, doc).props
	paths := make([]string, 0, len(props.sites))
	for _, at := range props.paths.All() {
		paths = append(paths, at.String())
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

// FamilyAllocs returns the allocations one run of each rule family Validate
// runs over doc makes, in the order it runs them, as testing.AllocsPerRun
// counts. The walk they read is taken beforehand, and so is the index they
// ask, its hierarchy and its exposure built in full, so each family is
// measured at its own cost and not at the index's.
func FamilyAllocs(tb testing.TB, doc *ir.Document) []float64 {
	tb.Helper()
	read := walkedFor(tb, doc)
	facts := irfacts.New(doc)
	facts.Subtypes("") // builds the hierarchy
	for range len(doc.Types) + 1 {
		facts.Exposes("", "") // each takes a step, so the walks run out and the exposure is built
	}
	run := families(doc, read, facts)
	costs := make([]float64, 0, len(run))
	for _, family := range run {
		costs = append(costs, testing.AllocsPerRun(5, func() { _ = family() }))
	}
	return costs
}

// WalkAllocs returns the allocations the one walk Validate takes of doc makes,
// as testing.AllocsPerRun counts.
func WalkAllocs(tb testing.TB, doc *ir.Document) float64 {
	tb.Helper()
	return testing.AllocsPerRun(5, func() { _ = walkedFor(tb, doc) })
}

// ReusedMap is reusedMap, for the external tests.
type ReusedMap[K comparable, V any] = reusedMap[K, V]

// Emptied is r.emptied, for the external tests.
func Emptied[K comparable, V any](r *ReusedMap[K, V]) map[K]V { return r.emptied() }

// ReuseLimit is reuseLimit, for the external tests.
const ReuseLimit = reuseLimit

// PropIDRefs returns checkPropIDRefs over what one walk of doc reads, as
// validate runs it, for the external tests to measure.
func PropIDRefs(tb testing.TB, doc *ir.Document) func() []ir.Diagnostic {
	tb.Helper()
	w := walkedFor(tb, doc)
	return func() []ir.Diagnostic { return checkPropIDRefs(w.props, w.refs) }
}
