// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package harness

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// unnamed is a report naming no node at pointer, as reportsOf reads one of
// a source without reversed sequences.
func unnamed(code, pointer string) report {
	d := diagnostic(ir.SeverityWarning, code, pointer, "")
	return report{key: diagnosticKey(d), erased: diagnosticKey(d), pointer: pointer, raw: d}
}

// bothWays compiles src as the oracle does, as re-encoded and reversed, and
// returns both documents and what was reversed.
func bothWays(t *testing.T, src string) (first, second *ir.Document, perm permutation) {
	t.Helper()
	baseline, why := reencodeMappings([]byte(src))
	require.Empty(t, why)
	reversed, perm, why := reverseSequences([]byte(src))
	require.Empty(t, why)
	first, _, err := compile(context.Background(), "spec.yaml", baseline, passes())
	require.NoError(t, err)
	second, _, err = compile(context.Background(), "spec.yaml", reversed, passes())
	require.NoError(t, err)
	require.NotNil(t, first, "the fixture must compile as written")
	require.NotNil(t, second, "the fixture must compile reversed")
	return first, second, perm
}

// TestTieClassesOf_TiesAMemberTheDocumentsShowATieDecided pins which members
// fixtureSwap's two compiles tie, given the reports naming no node each makes:
// p, which folds alpha's and beta's declarations and takes beta's as written
// and alpha's reversed, where each order reports at one of p's declarations;
// not q, declared alike in both, whatever moves between its declarations; and
// not p where only one order reports at its declarations.
func TestTieClassesOf_TiesAMemberTheDocumentsShowATieDecided(t *testing.T) {
	first, second, perm := bothWays(t, fixtureSwap)
	const s = "/components/schemas"
	alphaP, betaP := unnamed("x/swap", s+"/alpha/properties/p"), unnamed("x/swap", s+"/beta/properties/p")
	alphaQ, betaQ := unnamed("x/swap", s+"/alpha/properties/q"), unnamed("x/swap", s+"/beta/properties/q")
	for _, tc := range []struct {
		name    string
		a, b    []report
		members []string
	}{
		{name: "a report at each of p's declarations", a: []report{alphaP}, b: []report{betaP},
			members: []string{"p/composed/components/schemas/AB/allOf/properties/p", "p/composed/components/schemas/AB2/allOf/properties/p"}},
		{name: "a report moving between q's declarations", a: []report{alphaQ}, b: []report{betaQ}},
		{name: "a report at p's declarations as written alone", a: []report{alphaP}, b: []report{betaQ}},
		{name: "no report reversed", a: []report{alphaP}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, why := tieClassesOf(&formReader{doc: first, seqs: perm.written}, &formReader{doc: second, seqs: perm.moved}, tc.a, tc.b)

			require.Empty(t, why)
			assert.Equal(t, tc.members, got.members)
		})
	}

	reversed := &formReader{doc: second, seqs: perm.moved}
	got, _ := tieClassesOf(&formReader{doc: first, seqs: perm.written}, reversed, []report{alphaP}, []report{betaP})
	least := s + "/AB/allOf/properties/p"
	assert.Equal(t, map[string]string{least: least, s + "/AB2/allOf/properties/p": least,
		s + "/alpha/properties/p": least, s + "/beta/properties/p": least}, got.least,
		"p's declarations, its two models' among them, are one class")
	none, why := tieClassesOf(&formReader{seqs: perm.written}, reversed, []report{alphaP}, []report{betaP})
	require.Empty(t, why)
	assert.Empty(t, none.members, "a refused compile shows no tie")
}

// TestTieClasses_JoinKeepsEachClassAtItsLeastPointer pins the classes joined
// declarations make: one tied member's declarations are one class, two
// classes sharing a declaration are one, transitively, whichever was joined
// first, and each declaration maps to its class's least pointer.
func TestTieClasses_JoinKeepsEachClassAtItsLeastPointer(t *testing.T) {
	t.Parallel()
	c := tieClasses{least: map[string]string{}}

	c.join([]string{"/c", "/d"})
	c.join([]string{"/e", "/f"})
	c.join([]string{"/b", "/e"})
	c.join([]string{"/d", "/f"})
	c.join([]string{"/x", "/y"})
	c.settle()

	assert.Equal(t, map[string]string{"/b": "/b", "/c": "/b", "/d": "/b", "/e": "/b", "/f": "/b", "/x": "/x", "/y": "/x"}, c.least)
}

// TestClassed_MatchesAReportAtAClassOnceAtItsLeastPointer pins how a report is
// matched: one naming no node at a declaration of a class by its key at the
// class's least pointer, once however many of the compile's reports so match,
// and one naming a node or sitting at no class's declaration by its own key.
func TestClassed_MatchesAReportAtAClassOnceAtItsLeastPointer(t *testing.T) {
	t.Parallel()
	c := tieClasses{least: map[string]string{"/a": "/a", "/b": "/a"}}
	d := diagnostic(ir.SeverityWarning, "x/swap", "/a", "p/m")
	named := report{key: diagnosticKey(d), pointer: "/a", node: "p/m", raw: d}
	rs := []report{unnamed("x/swap", "/b"), unnamed("x/swap", "/a"), named, unnamed("x/swap", "/c"), unnamed("x/other", "/b")}

	got := c.classed(rs, nil)

	matches := make([]string, len(got))
	for i, r := range got {
		matches[i] = r.matched()
	}
	assert.Equal(t, []string{unnamed("x/swap", "/a").key, named.key, unnamed("x/swap", "/c").key, unnamed("x/other", "/a").key}, matches)
	assert.Equal(t, "/b", got[0].pointer, "a matched report keeps where it sits, as its finding says")
}
