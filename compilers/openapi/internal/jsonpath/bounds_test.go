// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package jsonpath_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/budget"
	"github.com/dexpace/morphic/compilers/openapi/internal/doctree"
	"github.com/dexpace/morphic/compilers/openapi/internal/jsonpath"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
)

// selectWithin evaluates query over root against a budget of limit, returning
// what it selected, the steps it spent and its error.
func selectWithin(t testing.TB, query string, root *yaml.Node, limit budget.Limit) ([]jsonpath.Located, int, error) {
	t.Helper()
	q, err := jsonpath.Parse(query)
	require.NoError(t, err, "the query %q", query)
	b := budget.New(limit)
	got, err := q.Select(t.Context(), nodeview.New(), root, &b)
	return got, b.Spent(), err
}

// chain returns the value {"a": {"a": ... 0}} holding n members, each the
// value of the one before. It is built without recursion, so a chain deeper
// than any stack the evaluator could use is still a value.
func chain(n int) *yaml.Node {
	cur := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: "0"}
	for range n {
		key := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Style: yaml.DoubleQuotedStyle, Value: "a"}
		cur = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map", Content: []*yaml.Node{key, cur}}
	}
	return cur
}

// readTree reads text as the compiler reads a document, aliases and merges
// kept, and returns the value node.
func readTree(t testing.TB, text string) *yaml.Node {
	t.Helper()
	doc, err := doctree.Read(t.Context(), []byte(text), budget.Unbounded())
	require.NoError(t, err)
	return nodeview.DocumentRoot(doc.Root)
}

// paths returns the normalized path of each located node.
func paths(got []jsonpath.Located) []string {
	if len(got) == 0 {
		return nil
	}
	out := make([]string, len(got))
	for i, l := range got {
		out[i] = l.Path.String()
	}
	return out
}

// TestSelect_ReadsAnAliasAndAMergedMemberAsTheValueTheyStandFor pins what the
// evaluator sees of the YAML a source writes: an alias is what it names, a
// merged member is a member of the mapping that merges it, and the merge key
// itself is no member. Each node is returned once through each path to it.
func TestSelect_ReadsAnAliasAndAMergedMemberAsTheValueTheyStandFor(t *testing.T) {
	t.Parallel()
	root := readTree(t, "a: &x {p: 1}\nb: *x\nc: {<<: *x, q: 2}\n")

	tests := []struct {
		query string
		want  []string
	}{
		{"$.b.p", []string{"$['b']['p']"}},
		{"$.c.p", []string{"$['c']['p']"}},
		{"$.c.*", []string{"$['c']['p']", "$['c']['q']"}},
		{"$..p", []string{"$['a']['p']", "$['b']['p']", "$['c']['p']"}},
		{"$.c['<<']", nil},
		{"$.c.q", []string{"$['c']['q']"}},
	}
	for _, tc := range tests {
		t.Run(tc.query, func(t *testing.T) {
			t.Parallel()
			got, _, err := selectWithin(t, tc.query, root, budget.Unbounded())

			require.NoError(t, err)
			assert.Equal(t, tc.want, paths(got))
		})
	}

	got, _, err := selectWithin(t, "$..p", root, budget.Unbounded())
	require.NoError(t, err)
	require.Len(t, got, 3)
	assert.Same(t, got[0].Node, got[1].Node, "the alias reads as the node it names")
	assert.Same(t, got[0].Node, got[2].Node, "and so does the merged member")
	assert.Equal(t, "1", got[0].Node.Value)
}

// TestSelect_ComparesValuesNotTagsOrSpellings pins the comparison of
// scalars the compiler reads: 0x10 is the number 16, a plain date a string,
// and a scalar the JSON schema has no reading of equals itself and nothing
// else, even a twin written the same.
func TestSelect_ComparesValuesNotTagsOrSpellings(t *testing.T) {
	t.Parallel()
	root := readTree(t, "x: 0x10\ny: 16\nd: 2021-01-01\ne: !!binary aGk=\nf: !!binary aGk=\n")

	tests := []struct {
		query string
		want  []string
	}{
		{"$[?@ == 16]", []string{"$['x']", "$['y']"}},
		{"$[?@ == '2021-01-01']", []string{"$['d']"}},
		{"$[?@ == $.f]", []string{"$['f']"}},
		{"$[?@ == $.e]", []string{"$['e']"}},
		{"$[?@ != $.e]", []string{"$['x']", "$['y']", "$['d']", "$['f']"}},
	}
	for _, tc := range tests {
		t.Run(tc.query, func(t *testing.T) {
			t.Parallel()
			got, _, err := selectWithin(t, tc.query, root, budget.Unbounded())

			require.NoError(t, err)
			assert.Equal(t, tc.want, paths(got))
		})
	}
}

// TestSelect_ComparesNumbersByDecimalNotByFloat pins a number the evaluator
// cannot hold as a float: an exponent past any float is still ordered by its
// sign and magnitude, and one of twenty digits costs no more than a short one.
func TestSelect_ComparesNumbersByDecimalNotByFloat(t *testing.T) {
	t.Parallel()
	root := jsonTree(t, []byte(`{"k":{"a":1e400}}`))

	tests := []struct {
		query string
		want  []string
	}{
		{"$[?@.a == 1e400]", []string{"$['k']"}},
		{"$[?@.a == 1e-400]", nil},
		{"$[?@.a < 1e999999999999999999999]", []string{"$['k']"}},
		{"$[?@.a > 1e-999999999999999999999]", []string{"$['k']"}},
	}
	for _, tc := range tests {
		t.Run(tc.query, func(t *testing.T) {
			t.Parallel()
			got, spent, err := selectWithin(t, tc.query, root, budget.Unbounded())

			require.NoError(t, err)
			assert.Equal(t, tc.want, paths(got))
			assert.Less(t, spent, 100, "a decimal's exponent is read, never expanded")
		})
	}
}

// TestSelect_ChargesEveryNodeAQueryReturns pins the cost of a descendant
// segment: each result is a step, so the nodelist of $..a..a..a over a chain
// is as large as the RFC makes it, and a budget below it is spent. Over a
// chain of 2,000 it is spent at a million steps, and over one of 60 the
// nodelist is n(n-1)(n-2)/6 long.
func TestSelect_ChargesEveryNodeAQueryReturns(t *testing.T) {
	t.Parallel()
	const query = "$..a..a..a"

	_, _, err := selectWithin(t, query, chain(2000), budget.AtMost(1_000_000))
	require.ErrorIs(t, err, jsonpath.ErrSteps, "the budget is what stops it")

	const n = 60
	got, spent, err := selectWithin(t, query, chain(n), budget.Unbounded())
	require.NoError(t, err)
	assert.Len(t, got, n*(n-1)*(n-2)/6)
	assert.GreaterOrEqual(t, spent, len(got), "a step for every result, at least")

	_, _, err = selectWithin(t, "$..*", chain(n), budget.AtMost(10))
	assert.ErrorIs(t, err, jsonpath.ErrSteps, "a bound smaller than the result is spent")
}

// TestSelect_DescendsWithoutRecursion pins that a descendant segment over a
// chain deeper than a stack could hold selects every node, in work linear in
// the chain: doubling it doubles the steps.
func TestSelect_DescendsWithoutRecursion(t *testing.T) {
	t.Parallel()
	steps := map[int]int{}
	for _, n := range []int{2000, 4000, 8000} {
		got, spent, err := selectWithin(t, "$..a", chain(n), budget.Unbounded())
		require.NoError(t, err)
		require.Len(t, got, n)
		assert.Equal(t, fmt.Sprintf("$%s", strings.Repeat("['a']", n)), got[n-1].Path.String())
		steps[n] = spent
	}

	first, second := steps[4000]-steps[2000], steps[8000]-steps[4000]
	assert.InDelta(t, 2*first, second, 8, "marginal steps double with the chain: %v", steps)

	const deep = 100_000
	got, _, err := selectWithin(t, "$..a", chain(deep), budget.Unbounded())
	require.NoError(t, err, "a chain deeper than a goroutine stack evaluates")
	assert.Len(t, got, deep)
}

// TestSelect_BuildsAResultsPathsInLinearMemory pins that a result's path is a
// link to its parent's and not a copy: allocations over a chain of 4n are
// about four times those over n, where copied paths would be sixteen times.
func TestSelect_BuildsAResultsPathsInLinearMemory(t *testing.T) {
	q, err := jsonpath.Parse("$..a")
	require.NoError(t, err)
	allocs := func(n int) float64 {
		root := chain(n)
		return testing.AllocsPerRun(3, func() {
			b := budget.New(budget.Unbounded())
			if _, err := q.Select(t.Context(), nodeview.New(), root, &b); err != nil {
				t.Fatal(err)
			}
		})
	}

	small, large := allocs(1000), allocs(4000)

	assert.Less(t, large/small, 6.0, "allocations grew %v to %v for four times the nodes", small, large)
}

// TestSelect_VisitsAnAliasedNodeOncePerPathToIt pins the cost of aliasing: a
// thousand aliases of one thousand-member mapping are a million nodes to the
// evaluator, which is what a budget exists to refuse.
func TestSelect_VisitsAnAliasedNodeOncePerPathToIt(t *testing.T) {
	t.Parallel()
	var text strings.Builder
	text.WriteString("big: &m {")
	for i := range 1000 {
		fmt.Fprintf(&text, "k%d: 0, ", i)
	}
	text.WriteString("}\n")
	for i := range 1000 {
		fmt.Fprintf(&text, "x%d: *m\n", i)
	}
	root := readTree(t, text.String())

	_, _, err := selectWithin(t, "$..*", root, budget.AtMost(100_000))
	require.ErrorIs(t, err, jsonpath.ErrSteps)

	got, _, err := selectWithin(t, "$.big.*", root, budget.AtMost(100_000))
	require.NoError(t, err, "one copy of the mapping is cheap")
	assert.Len(t, got, 1000)
}

// TestSelect_RefusesAMergeChainPastTheViewsDepth pins the second bound a
// mapping has: merges nested past nodeview.MergeDepthLimit are an error, never
// a quietly shortened expansion, and a short chain of the same shape is read.
func TestSelect_RefusesAMergeChainPastTheViewsDepth(t *testing.T) {
	t.Parallel()
	merged := func(depth int) string {
		var text strings.Builder
		text.WriteString("d0: &a0 {k0: 0}\n")
		for i := 1; i <= depth; i++ {
			fmt.Fprintf(&text, "d%d: &a%d {<<: *a%d, k%d: %d}\n", i, i, i-1, i, i)
		}
		fmt.Fprintf(&text, "<<: *a%d\n", depth)
		return text.String()
	}

	got, _, err := selectWithin(t, "$.*", readTree(t, merged(3)), budget.Unbounded())
	require.NoError(t, err)
	assert.Len(t, got, 4+4, "the four anchors and the four members merged into the root")

	_, _, err = selectWithin(t, "$.*", readTree(t, merged(nodeview.MergeDepthLimit+2)), budget.Unbounded())
	assert.ErrorIs(t, err, jsonpath.ErrMergeBound)
}

// nested returns a query with n filters, each inside the one before.
func nested(n int) string {
	inner := "@.a"
	for range n - 1 {
		inner = "@[?" + inner + "]"
	}
	return "$[?" + inner + "]"
}

// TestParse_BoundsTheNestingOfAQuery pins the depth a query may nest to, at
// the cap and one past it: the first a query, the second a *SyntaxError that
// says why, from a parser that recurses no deeper than the cap.
func TestParse_BoundsTheNestingOfAQuery(t *testing.T) {
	t.Parallel()
	_, err := jsonpath.Parse(nested(64))
	require.NoError(t, err, "64 filters nested")

	_, err = jsonpath.Parse(nested(65))
	var syntax *jsonpath.SyntaxError
	require.ErrorAs(t, err, &syntax, "65 filters nested")
	assert.Contains(t, err.Error(), "depth")

	parens := func(n int) string { return "$[?" + strings.Repeat("(", n) + "@.a" + strings.Repeat(")", n) + "]" }
	_, err = jsonpath.Parse(parens(64))
	require.NoError(t, err, "64 parentheses nested")
	_, err = jsonpath.Parse(parens(65))
	require.ErrorAs(t, err, &syntax, "65 parentheses nested")

	calls := func(n int) string {
		return "$[?" + strings.Repeat("length(", n) + "@" + strings.Repeat(")", n) + " == 1]"
	}
	_, err = jsonpath.Parse(calls(1000))
	require.ErrorAs(t, err, &syntax, "a thousand calls nested ends as a syntax error, not a stack overflow")
}

// TestSelect_MatchesInTimeLinearInTheInput pins the regular-expression
// functions: a pattern that backtracks exponentially in some engines costs the
// input's length here, so a string of twice the length costs exactly that
// much more, and a pattern past 4,096 bytes is no match for any node.
func TestSelect_MatchesInTimeLinearInTheInput(t *testing.T) {
	t.Parallel()
	holding := func(n int) *yaml.Node {
		return jsonTree(t, []byte(`["`+strings.Repeat("a", n)+`"]`))
	}
	const query = "$[?match(@, '(a*)*b')]"

	got, small, err := selectWithin(t, query, holding(10_000), budget.Unbounded())
	require.NoError(t, err)
	assert.Empty(t, got)
	assert.GreaterOrEqual(t, small, 10_000, "a step for each character of the input")
	assert.Less(t, small, 10_000+100, "and a constant besides")

	_, large, err := selectWithin(t, query, holding(20_000), budget.Unbounded())
	require.NoError(t, err)
	assert.Equal(t, 10_000, large-small, "the cost grows by the input's growth, no faster")

	search, _, err := selectWithin(t, "$[?search(@, '(a*)*b')]", holding(10_000), budget.Unbounded())
	require.NoError(t, err)
	assert.Empty(t, search)
}

// TestSelect_RefusesAPatternPastTheCap pins the pattern length: one of 4,096
// bytes is compiled and matches, one of 4,097 answers false for every node, as
// a pattern that is no I-Regexp does (RFC 9535 section 2.4.6).
func TestSelect_RefusesAPatternPastTheCap(t *testing.T) {
	t.Parallel()
	root := jsonTree(t, []byte(`["`+strings.Repeat("a", 5000)+`", "b"]`))
	query := func(n int) string { return fmt.Sprintf("$[?match(@, '%s')]", strings.Repeat("a", n)) }

	got, _, err := selectWithin(t, query(4096), root, budget.Unbounded())
	require.NoError(t, err)
	assert.Empty(t, got, "4,096 a's against a string of 5,000: no full match, and compiled")

	exact := jsonTree(t, []byte(`["`+strings.Repeat("a", 4096)+`"]`))
	got, _, err = selectWithin(t, query(4096), exact, budget.Unbounded())
	require.NoError(t, err)
	assert.Equal(t, []string{"$[0]"}, paths(got), "a pattern at the cap is compiled")

	exact = jsonTree(t, []byte(`["`+strings.Repeat("a", 4097)+`"]`))
	got, _, err = selectWithin(t, query(4097), exact, budget.Unbounded())
	require.NoError(t, err)
	assert.Empty(t, got, "a pattern one past the cap matches nothing, though it would match")

	got, _, err = selectWithin(t, "$[?!match(@, '"+strings.Repeat("a", 5000)+"')]", root, budget.Unbounded())
	require.NoError(t, err)
	assert.Equal(t, []string{"$[0]", "$[1]"}, paths(got), "so its negation holds for every node")
}

// TestSelect_IsRepeatableAndLeavesTheTreeAlone pins that a parsed Query keeps no
// state between evaluations and an evaluation writes nothing to the tree: the
// second run returns the same nodes at the same cost.
func TestSelect_IsRepeatableAndLeavesTheTreeAlone(t *testing.T) {
	t.Parallel()
	root := readTree(t, "a: &x {p: 1}\nb: *x\nc: {<<: *x, q: 2}\n")
	before := fmt.Sprintf("%v", root)
	q, err := jsonpath.Parse("$..[?@ == 1 || match(@.q, '2')]")
	require.NoError(t, err)

	run := func() ([]string, int) {
		b := budget.New(budget.Unbounded())
		got, err := q.Select(t.Context(), nodeview.New(), root, &b)
		require.NoError(t, err)
		return paths(got), b.Spent()
	}
	first, firstSpent := run()
	second, secondSpent := run()

	assert.Equal(t, first, second)
	assert.Equal(t, firstSpent, secondSpent)
	assert.Equal(t, before, fmt.Sprintf("%v", root))
}
