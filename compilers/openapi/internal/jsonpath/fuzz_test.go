// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package jsonpath_test

import (
	json "encoding/json/v2"
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/require"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/budget"
	"github.com/dexpace/morphic/compilers/openapi/internal/jsonpath"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
)

// fuzzSteps bounds each evaluation a fuzz input makes, so a query whose cost
// grows past a document's size is refused rather than run.
const fuzzSteps = 1 << 16

// suiteSelectors returns every selector of the vendored compliance suite, the
// fuzz targets' seeds.
func suiteSelectors(f *testing.F) []string {
	f.Helper()
	data, err := os.ReadFile(suitePath)
	require.NoError(f, err)
	var file struct {
		Tests []struct {
			Selector string `json:"selector"`
		} `json:"tests"`
	}
	require.NoError(f, json.Unmarshal(data, &file))
	out := make([]string, 0, len(file.Tests))
	for _, tc := range file.Tests {
		out = append(out, tc.Selector)
	}
	return out
}

// within evaluates q over root within fuzzSteps, requiring that it spends no
// more and that only a bound refuses it.
func within(t *testing.T, q jsonpath.Query, root *yaml.Node) ([]jsonpath.Located, bool) {
	t.Helper()
	b := budget.New(budget.AtMost(fuzzSteps))
	got, err := q.Select(t.Context(), nodeview.New(), root, &b)
	require.LessOrEqual(t, b.Spent(), fuzzSteps)
	if err != nil {
		require.True(t, errors.Is(err, jsonpath.ErrSteps) || errors.Is(err, jsonpath.ErrMergeBound), "%v", err)
		return nil, false
	}
	return got, true
}

// FuzzParse_NamesEachResultByAQueryForIt holds Parse and Select to each other:
// any text either parses or is a *SyntaxError, never a panic, and each node a
// parsed query selects has a normalized path that parses and selects that
// node alone, at that path.
func FuzzParse_NamesEachResultByAQueryForIt(f *testing.F) {
	for _, s := range suiteSelectors(f) {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, text string) {
		root := readTree(t, `{a: [1, b, {c: 2}], d: {e: null, 'f g': true, "h'": [[]]}, i: j}`)
		q, err := jsonpath.Parse(text)
		if err != nil {
			_, syntax := errors.AsType[*jsonpath.SyntaxError](err)
			require.True(t, syntax, "%v", err)
			return
		}
		got, ok := within(t, q, root)
		if !ok {
			return
		}
		for _, l := range got[:min(len(got), 64)] {
			path := l.Path.String()
			back, err := jsonpath.Parse(path)
			require.NoError(t, err, path)
			again, ok := within(t, back, root)
			require.True(t, ok, path)
			require.Len(t, again, 1, path)
			require.Same(t, l.Node, again[0].Node, path)
			require.Equal(t, path, again[0].Path.String())
		}
	})
}

// sharedSource writes a small document sharing nodes, drawn from seed:
// anchored mappings, then members naming them by alias and by `<<` merge,
// keys few enough that a merge often brings one the mapping writes too.
func sharedSource(seed uint64) string {
	r := rand.New(rand.NewPCG(seed, 9535))
	anchors := make([]string, 0, 3)
	var value func(depth int) string
	value = func(depth int) string {
		switch k := r.IntN(5); {
		case depth == 0 || k == 0:
			return []string{"1", "2", "s", "true"}[r.IntN(4)]
		case k == 1 && len(anchors) > 0:
			return "*" + anchors[r.IntN(len(anchors))]
		case k == 2:
			return "[" + value(depth-1) + ", " + value(depth-1) + "]"
		}
		members := make([]string, 0, 4)
		if len(anchors) > 0 && r.IntN(2) == 0 {
			members = append(members, "<<: [*"+anchors[r.IntN(len(anchors))]+", *"+anchors[r.IntN(len(anchors))]+"]")
		}
		for _, key := range []string{"p", "q", "r"} {
			if r.IntN(2) == 0 {
				members = append(members, key+": "+value(depth-1))
			}
		}
		return "{" + strings.Join(members, ", ") + "}"
	}
	var b strings.Builder
	for i := range 3 {
		v := value(3)
		for !strings.HasPrefix(v, "{") {
			v = value(3)
		}
		fmt.Fprintf(&b, "a%d: &a%d %s\n", i, i, v)
		anchors = append(anchors, fmt.Sprintf("a%d", i))
	}
	fmt.Fprintf(&b, "g: %s\n", value(3))
	return b.String()
}

// selection renders a nodelist as its normalized paths, each with the value
// there decoded, sorted, so two nodelists naming the same nodes in another
// order render alike.
func selection(t *testing.T, got []jsonpath.Located) []string {
	t.Helper()
	out := make([]string, 0, len(got))
	for _, l := range got {
		var v any
		require.NoError(t, l.Node.Decode(&v))
		out = append(out, fmt.Sprintf("%s = %#v", l.Path.String(), v))
	}
	slices.Sort(out)
	return out
}

// FuzzSelect_ReadsASharedValueAsItsExpansion holds an evaluation over a
// document sharing nodes through aliases and merge keys to the same query
// over the value written out with nothing shared: never a panic, never a
// step past the bound, and, where both finish, the same nodes at the same
// paths.
func FuzzSelect_ReadsASharedValueAsItsExpansion(f *testing.F) {
	for i, s := range suiteSelectors(f) {
		f.Add(s, uint64(i))
	}
	f.Fuzz(func(t *testing.T, text string, seed uint64) {
		q, err := jsonpath.Parse(text)
		if err != nil {
			return
		}
		src := sharedSource(seed)
		var value any
		require.NoError(t, yaml.Unmarshal([]byte(src), &value), src)
		expanded, err := yaml.Marshal(value)
		require.NoError(t, err)

		got, ok := within(t, q, readTree(t, src))
		want, twinOK := within(t, q, readTree(t, string(expanded)))
		if !ok || !twinOK {
			return
		}
		if d := cmp.Diff(selection(t, want), selection(t, got)); d != "" {
			t.Fatalf("%s over\n%s\nselects another nodelist than over its expansion (-expanded +shared):\n%s", text, src, d)
		}
	})
}
