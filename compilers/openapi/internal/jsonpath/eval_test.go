// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package jsonpath_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/budget"
	"github.com/dexpace/morphic/compilers/openapi/internal/jsonpath"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
)

// bookstore is the example document of RFC 9535 section 1.5.
const bookstore = `{ "store": {
    "book": [
      { "category": "reference", "author": "Nigel Rees", "title": "Sayings of the Century", "price": 8.95 },
      { "category": "fiction", "author": "Evelyn Waugh", "title": "Sword of Honour", "price": 12.99 },
      { "category": "fiction", "author": "Herman Melville", "title": "Moby Dick", "isbn": "0-553-21311-3", "price": 8.99 },
      { "category": "fiction", "author": "J. R. R. Tolkien", "title": "The Lord of the Rings", "isbn": "0-395-19395-8",
        "price": 22.99 }
    ],
    "bicycle": { "color": "red", "price": 399 }
  }
}`

// TestSelect_AnswersTheExamplesOfRFC9535 holds the evaluator to the table of
// RFC 9535 section 1.5, each query with the paths it selects.
func TestSelect_AnswersTheExamplesOfRFC9535(t *testing.T) {
	t.Parallel()
	root := jsonTree(t, []byte(bookstore))
	books := func(is ...int) []string {
		out := make([]string, len(is))
		for i, n := range is {
			out[i] = fmt.Sprintf("$['store']['book'][%d]", n)
		}
		return out
	}
	tests := []struct {
		query string
		want  []string
	}{
		{"$.store.book[*].author", []string{"$['store']['book'][0]['author']", "$['store']['book'][1]['author']",
			"$['store']['book'][2]['author']", "$['store']['book'][3]['author']"}},
		{"$.store.*", []string{"$['store']['book']", "$['store']['bicycle']"}},
		{"$..book[2]", books(2)},
		{"$..book[-1]", books(3)},
		{"$..book[0,1]", books(0, 1)},
		{"$..book[:2]", books(0, 1)},
		{"$..book[?@.isbn]", books(2, 3)},
		{"$..book[?@.price<10]", books(0, 2)},
		{"$.store.book[::-1]", books(3, 2, 1, 0)},
		{"$.store.book[1:3:1]", books(1, 2)},
		{"$.store.book[5:0:-2]", books(3, 1)},
		{"$.store.book[?@.price > 20 || @.category == 'reference']", books(0, 3)},
		{"$.store.book[?@.price > 9 && !(@.category == 'reference')]", books(1, 3)},
		{"$.store.book[?length(@.title) == 9]", books(2)},
		{"$.store[?count(@.*) == 2]", []string{"$['store']['bicycle']"}},
		{"$.store.book[?value(@..isbn) == '0-553-21311-3']", books(2)},
		{"$.store.book[?search(@.author, '[Rr]')]", books(0, 2, 3)},
		{"$.store.book[?@.price <= 8.99 && @.price >= 8.95]", books(0, 2)},
		{"$.store.book[?@.price != 8.95 && @.author > 'J']", books(3)},
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

// TestParse_RefusesWhatTheGrammarDoesNot pins a *SyntaxError, at the offset
// named, for each way a text can fail to be a query.
func TestParse_RefusesWhatTheGrammarDoesNot(t *testing.T) {
	t.Parallel()
	tests := []struct {
		query  string
		offset int
	}{
		{"$" + strings.Repeat(".a", jsonpath.MaxQueryBytes/2), jsonpath.MaxQueryBytes},
		{"$['a\xffb']", 4},
		{"$[?(@.a]", 7},
		{"$[?length(@.a)]", 3},
		{"$[?foo(@)]", 3},
		{"$[?count(1) == 1]", 9},
		{"$[?length(@.a, 1) == 1]", 15},
		{"$[?length(@.a 1) == 1]", 14},
		{"$[?length(!) == 1]", 11},
		{"$[?length(@.a == ) == 1]", 17},
		{"$[?length(@.* == 1) == 1]", 10},
		{"$[?length(@.a == 1 && ) == 1]", 22},
		{"$[?length(~) == 1]", 10},
		{"$[?count(@.a == 1) == 1]", 9},
		{"$[?length((@.a)) == 1]", 10},
		{"$[?@.a == ]", 10},
		{"$[?@.a == @.*]", 10},
		{"$[?!1]", 4},
		{"$[?!(@.a]", 8},
		{"$[?1.]", 5},
		{"$[?1e]", 5},
		{"$[?-]", 4},
		{"$[?nul]", 3},
		{"$[?" + strings.Repeat("(", 65) + "@.a" + strings.Repeat(")", 65) + "]", 67},
	}
	for _, tc := range tests {
		t.Run(tc.query[:min(len(tc.query), 40)], func(t *testing.T) {
			t.Parallel()
			_, err := jsonpath.Parse(tc.query)

			var syntax *jsonpath.SyntaxError
			require.ErrorAs(t, err, &syntax)
			assert.Equal(t, tc.offset, syntax.Offset, "%s", syntax.Reason)
			assert.Contains(t, err.Error(), syntax.Reason)
		})
	}
}

// TestSelect_ComparesAndCallsOverEveryKindOfValue pins comparisons and
// function calls over the kinds a document's values come in.
func TestSelect_ComparesAndCallsOverEveryKindOfValue(t *testing.T) {
	t.Parallel()
	root := readTree(t, "a: {k: 1}\nb: [1, 2]\nc: null\nd: true\ne: 'é1'\nf: !!bool maybe\n"+
		"g: 1e999999999999999999999\nh: -1e999999999999999999999\nr: 'a.c'\ny: [1]\n")
	tests := []struct {
		query string
		want  []string
	}{
		{"$[?@ == 1]", nil},
		{"$[?@.k == 1]", []string{"$['a']"}},
		{"$[?@ == $.a]", []string{"$['a']"}},
		{"$[?@ == $.b]", []string{"$['b']"}},
		{"$[?@ == null]", []string{"$['c']"}},
		{"$[?@ == true]", []string{"$['d']"}},
		{"$[?@ == false]", nil},
		{"$[?@ == $.f]", []string{"$['f']"}},
		{"$[?@ > 1e400]", []string{"$['g']"}},
		{"$[?@ < -1e400]", []string{"$['h']"}},
		{"$[?@ < $.a]", nil},
		{"$[?@ < true]", nil},
		{"$[?length(@) == 2]", []string{"$['b']", "$['e']"}},
		{"$[?length(@) == 1]", []string{"$['a']", "$['y']"}},
		{"$[?length(1) == 1]", nil},
		{"$[?length(length(@)) == 1]", nil},
		{"$[?count(@.k) == 1]", []string{"$['a']"}},
		{"$[?value(@.*) == 1]", []string{"$['a']", "$['y']"}},
		{"$[?value(@.*) == 2]", nil},
		{"$[?match(@, $.r)]", []string{"$['r']"}},
		{"$[?match('abc', $.r) && match(@, $.r)]", []string{"$['r']"}},
		{"$[?match(@, $.nothing)]", nil},
		{"$[?match(@, 1)]", nil},
		{"$[?match(@, 'a{1001}')]", nil},
		{"$[?search(@, '[')]", nil},
		{"$[?search(@, $.b)]", nil},
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

// TestSelect_ReadsAnIRegexpByItsGrammar pins the patterns match and search
// read, and the ones they refuse as no I-Regexp, which match nothing.
func TestSelect_ReadsAnIRegexpByItsGrammar(t *testing.T) {
	t.Parallel()
	root := jsonTree(t, []byte(`["aa", "a", "", "-", "x\n", "A", "͸", "b]", "a.b", "a{", "\t", "\r"]`))
	tests := []struct {
		pattern string
		want    string
	}{
		{`a{2}`, "0"}, {`a{1,}`, "0 1"}, {`a{1,2}`, "0 1"}, {`a{`, ""}, {`a{2`, ""}, {`{2}`, ""}, {`a{,2}`, ""},
		{`*a`, ""}, {`a**`, ""}, {`a)`, ""}, {`(a`, ""}, {`]`, ""}, {`}`, ""}, {`(a|)`, "1 2"},
		{`x\n`, "4"}, {`\r`, "11"}, {`\t`, "10"}, {`a\.b`, "8"}, {`a\{`, "9"}, {`\x`, ""}, {`a\`, ""},
		{`\p{Lu}`, "5"}, {`\P{Ll}`, "3 5 6 10 11"}, {`\p{Cn}`, "6"}, {`\P{Cn}`, "1 3 5 10 11"}, {`\p{C}`, "6 10 11"},
		{`\P{C}`, "1 3 5"}, {`\p{Xx}`, ""}, {`\p`, ""}, {`\pL`, ""}, {`\p{L`, ""},
		{`[a-z]{2}`, "0"}, {`[^a]`, "3 5 6 10 11"}, {`[-a]`, "1 3"}, {`[a-]`, "1 3"}, {`[]`, ""}, {`[^]`, ""},
		{`[a-b-c]`, ""}, {`[z-a]`, ""}, {`[\p{Lu}]`, "5"}, {`[\t-\r]`, "10 11"}, {`[\p{L}-a]`, ""}, {`[a`, ""},
		{`[[]`, ""}, {`[a-\p{L}]`, ""}, {`[^\p{Cn}\p{L}]`, "3 10 11"}, {`b[\]]`, "7"}, {`[a-`, ""},
	}
	for _, tc := range tests {
		t.Run(tc.pattern, func(t *testing.T) {
			t.Parallel()
			literal := strings.NewReplacer(`\`, `\\`, `'`, `\'`).Replace(tc.pattern)
			got, _, err := selectWithin(t, "$[?match(@, '"+literal+"')]", root, budget.Unbounded())

			require.NoError(t, err)
			var indexes []string
			for _, p := range paths(got) {
				indexes = append(indexes, strings.Trim(p, "$[]"))
			}
			assert.Equal(t, tc.want, strings.Join(indexes, " "))
		})
	}
}

// TestSelect_RefusesAMergeChainWhereverItIsRead pins ErrMergeBound at every
// reading of a mapping's members: a name, a wildcard, a descent, a comparison
// and a length.
func TestSelect_RefusesAMergeChainWhereverItIsRead(t *testing.T) {
	t.Parallel()
	var text strings.Builder
	text.WriteString("d0: &a0 {k0: 0}\n")
	for i := 1; i <= nodeview.MergeDepthLimit+2; i++ {
		fmt.Fprintf(&text, "d%d: &a%d {<<: *a%d}\n", i, i, i-1)
	}
	fmt.Fprintf(&text, "m: {<<: *a%d}\nn: {<<: *a%d}\n", nodeview.MergeDepthLimit+2, nodeview.MergeDepthLimit+2)
	root := readTree(t, text.String())

	for _, query := range []string{"$.m.k0", "$.m.*", "$..k0", "$[?@ == $.n]", "$[?$.n == @]", "$[?length(@) == 1]", "$..[0]"} {
		_, _, err := selectWithin(t, query, root, budget.Unbounded())
		assert.ErrorIs(t, err, jsonpath.ErrMergeBound, query)
	}
}

// TestSelect_SpendsNoStepPastItsBudget pins that any budget below what a query
// needs is refused with ErrSteps, wherever the evaluation stands when it runs
// out, and that the need itself is enough. Scalars long enough to cost steps
// to read and to compare are among the values.
func TestSelect_SpendsNoStepPastItsBudget(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("x", 2*jsonpath.CompareBytes)
	root := readTree(t, "a: {k: [1, {x: 2}]}\nb: {k: [1, {x: 2}]}\nr: 'a.*'\ns: abc\n"+
		"l: "+long+"a\nm: "+long+"b\np: ["+long+"a]\nq: ["+long+"b]\n")
	for _, query := range []string{
		"$..[?@ == $.b]", "$[?@.k[0] < 2]", "$[?match(@, $.r) || search(@, 'b')]", "$..*",
		"$[?@ == $.b || @ != $.b]", "$[?length(@.k) == 2]", "$[?count(@.*) > 1]",
		"$[?@ == $.m]", "$[?@ < $.m]", "$[?$.m < @]", "$[?@ == $.q]", "$[?length(@) == 1]",
	} {
		need := func() int {
			_, spent, err := selectWithin(t, query, root, budget.Unbounded())
			require.NoError(t, err, query)
			return spent
		}()
		for b := range need {
			_, _, err := selectWithin(t, query, root, budget.AtMost(b))
			require.ErrorIs(t, err, jsonpath.ErrSteps, "%s within %d of %d", query, b, need)
		}
		_, _, err := selectWithin(t, query, root, budget.AtMost(need))
		require.NoError(t, err, query)
	}
}

// TestSelect_AsksItsContext pins that an evaluation stops once its context is
// done, within the steps between two asks.
func TestSelect_AsksItsContext(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	q, err := jsonpath.Parse("$..*")
	require.NoError(t, err)
	b := budget.New(budget.Unbounded())

	_, err = q.Select(ctx, nodeview.New(), chain(nodeview.AskEvery*2), &b)

	assert.ErrorIs(t, err, context.Canceled)
}

// TestSelect_HoldsANodelistToItsBound pins ErrResults: a nodelist one past its
// bound is refused, and one at it is returned.
func TestSelect_HoldsANodelistToItsBound(t *testing.T) {
	t.Parallel()
	q, err := jsonpath.Parse("$[*]")
	require.NoError(t, err)
	root := jsonTree(t, []byte(`[1, 2, 3]`))

	got, err := q.SelectHolding(t.Context(), root, 3)
	require.NoError(t, err)
	assert.Len(t, got, 3)

	_, err = q.SelectHolding(t.Context(), root, 2)
	assert.ErrorIs(t, err, jsonpath.ErrResults)
}

// TestSelect_ReadsAnAliasNamingNothingAsNoValue pins a tree no parse builds:
// an alias whose anchor is missing is no node, wherever it stands.
func TestSelect_ReadsAnAliasNamingNothingAsNoValue(t *testing.T) {
	t.Parallel()
	dangling := func() *yaml.Node { return &yaml.Node{Kind: yaml.AliasNode} }
	key := func(k string) *yaml.Node { return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: k} }
	seq := func() *yaml.Node { return &yaml.Node{Kind: yaml.SequenceNode, Content: []*yaml.Node{dangling()}} }
	root := &yaml.Node{Kind: yaml.MappingNode, Content: []*yaml.Node{key("s"), seq(), key("t"), seq(), key("u"), dangling()}}

	for query, want := range map[string][]string{
		"$.s[0]": nil, "$.s[0:1]": nil, "$.s[*]": nil, "$.u": nil, "$..*": {"$['s']", "$['t']"},
		"$[?@ == $.t]": {"$['s']", "$['t']"},
	} {
		got, _, err := selectWithin(t, query, root, budget.Unbounded())
		require.NoError(t, err, query)
		assert.Equal(t, want, paths(got), query)
	}
}

// TestSelect_RefusesWhatItCannotEvaluate pins the refusals at Select's
// boundary: no view or budget, and a tree holding no value.
func TestSelect_RefusesWhatItCannotEvaluate(t *testing.T) {
	t.Parallel()
	q, err := jsonpath.Parse("$")
	require.NoError(t, err)
	b := budget.New(budget.Unbounded())

	_, err = q.Select(t.Context(), nil, chain(1), &b)
	require.Error(t, err)
	_, err = q.Select(t.Context(), nodeview.New(), chain(1), nil)
	require.Error(t, err)
	_, err = q.Select(t.Context(), nodeview.New(), &yaml.Node{Kind: yaml.DocumentNode}, &b)
	require.Error(t, err)
	assert.NotErrorIs(t, err, jsonpath.ErrSteps)
}

// TestNumbers_ReadOnlyDecimals pins the one reading of a number the grammar
// never hands it: text that is no decimal.
func TestNumbers_ReadOnlyDecimals(t *testing.T) {
	t.Parallel()
	assert.True(t, jsonpath.IsNumber("-1.5e3"))
	assert.False(t, jsonpath.IsNumber("0x10"))
	assert.False(t, jsonpath.Translate(strings.Repeat("a", 4097)))
	assert.False(t, jsonpath.Translate("a\xff"))
}

// TestSelect_ChargesEveryNodeADescentVisits pins the cost of a descendant
// segment that selects nothing: each node it visits is a step, so a descent
// over a deep or wide document is bounded by the budget, whatever it returns.
func TestSelect_ChargesEveryNodeADescentVisits(t *testing.T) {
	t.Parallel()
	const n = 1000
	got, spent, err := selectWithin(t, "$..none", chain(n), budget.Unbounded())

	require.NoError(t, err)
	assert.Empty(t, got)
	assert.GreaterOrEqual(t, spent, n+1, "a step for each of the chain's mappings and its last value")

	_, _, err = selectWithin(t, "$..none", chain(n), budget.AtMost(n))
	assert.ErrorIs(t, err, jsonpath.ErrSteps)
}

// TestSelect_ChargesTheMembersAMergeExpands pins the cost of reading a mapping
// whose `<<` merges many anchored mappings: each member it expands to is a
// step, though a name selects one of them, so a document of few nodes cannot
// make a query expand far more than it spends.
func TestSelect_ChargesTheMembersAMergeExpands(t *testing.T) {
	t.Parallel()
	var text strings.Builder
	refs := make([]string, 20)
	for a := range refs {
		fmt.Fprintf(&text, "a%d: &a%d {", a, a)
		for k := range 50 {
			fmt.Fprintf(&text, "k%d_%d: 0, ", a, k)
		}
		text.WriteString("}\n")
		refs[a] = fmt.Sprintf("*a%d", a)
	}
	fmt.Fprintf(&text, "m: {<<: [%s], own: 1}\n", strings.Join(refs, ", "))

	got, spent, err := selectWithin(t, "$.m.own", readTree(t, text.String()), budget.Unbounded())

	require.NoError(t, err)
	assert.Len(t, got, 1)
	assert.GreaterOrEqual(t, spent, 20*50+1, "a step for each member the merges expand to")
}

// TestSelect_WeighsAPatternByItsProgram pins the bound on what a pattern
// compiles to: repetition makes a program far larger than its text, so one
// past maxProgram instructions matches nothing though it would match, whether
// a repetition or an atom takes it past, and each byte matched against one
// within it costs a step per eight of its instructions.
func TestSelect_WeighsAPatternByItsProgram(t *testing.T) {
	t.Parallel()
	for _, piece := range []struct{ pattern, text string }{
		{"[a-z]{999}b", strings.Repeat("a", 999) + "b"},
		{"b[a-z]{999}", "b" + strings.Repeat("a", 999)},
	} {
		input := strings.Repeat(piece.text, 9)
		root := jsonTree(t, []byte(`["`+input+`"]`))
		for _, tc := range []struct {
			pieces int
			want   []string
		}{{8, []string{"$[0]"}}, {9, nil}} {
			query := "$[?search(@, '" + strings.Repeat(piece.pattern, tc.pieces) + "')]"
			got, spent, err := selectWithin(t, query, root, budget.Unbounded())
			require.NoError(t, err)
			assert.Equal(t, tc.want, paths(got), "%d of %s", tc.pieces, piece.pattern)
			if tc.want != nil {
				assert.GreaterOrEqual(t, spent, len(input)*(8*1999)/8, "a step per byte per eight instructions")
			}
		}
	}

	pieces := strings.Repeat("[a-z]{999}b", 8)
	input := strings.Repeat(strings.Repeat("a", 999)+"b", 8) + strings.Repeat("b", 393)
	root := jsonTree(t, []byte(`["`+input+`"]`))
	for tail, want := range map[int][]string{392: {"$[0]"}, 393: nil} {
		got, _, err := selectWithin(t, "$[?search(@, '"+pieces+strings.Repeat("b", tail)+"')]", root, budget.Unbounded())
		require.NoError(t, err)
		assert.Equal(t, want, paths(got), "an atom taking the program to %d instructions", 8*1999+tail)
	}
}
