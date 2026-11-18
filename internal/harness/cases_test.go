// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package harness_test

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/internal/harness"
	"github.com/dexpace/morphic/internal/testspec"
)

// grid is a fake product of cases: its axes, each case a tiny spec but those
// skip leaves unwritten.
type grid struct {
	axes []harness.Axis
	skip func(at []int) bool
}

func (g grid) Axes() []harness.Axis { return g.axes }

func (g grid) Write(at []int) (harness.Document, bool) {
	if g.skip != nil && g.skip(at) {
		return harness.Document{}, false
	}
	return harness.Document{Sources: []compilers.Source{{Path: "spec.yaml", Data: []byte(testspec.Tiny)}}}, true
}

// axis returns an axis of core values named as given.
func axis(name string, values ...string) harness.Axis {
	a := harness.Axis{Name: name}
	for _, v := range values {
		a.Values = append(a.Values, harness.Value{Name: v})
	}
	return a
}

// numbered returns an axis of n core values, named by their indices.
func numbered(name string, n int) harness.Axis {
	a := harness.Axis{Name: name, Values: make([]harness.Value, n)}
	for i := range a.Values {
		a.Values[i].Name = fmt.Sprint(i)
	}
	return a
}

// threeAxes is the 2x3x2 product the tests below share, with one value of the
// last axis like the other.
func threeAxes() []harness.Axis {
	last := axis("c", "x")
	last.Values = append(last.Values, harness.Value{Name: "y", Like: "x"})
	return []harness.Axis{axis("a", "a0", "a1"), axis("b", "b0", "b1", "b2"), last}
}

// idleVerdicts are what every case oracle says of a case whose document names
// no neighbour.
func idleVerdicts() []harness.Verdict {
	return []harness.Verdict{
		{Oracle: harness.OracleSpelling, Declined: harness.DeclineNoPartner},
		{Oracle: harness.OraclePresence, Declined: harness.DeclineNoConstruct},
		{Oracle: harness.OracleInstance, Declined: harness.DeclineNoInstance},
	}
}

func TestCaseCount_IsTheProductOfTheAxes(t *testing.T) {
	n, err := harness.CaseCount(grid{axes: threeAxes()})
	require.NoError(t, err)
	assert.Equal(t, 12, n)

	n, err = harness.CaseCount(grid{axes: []harness.Axis{numbered("a", 1<<11), numbered("b", 1<<11)}})
	require.NoError(t, err, "a product of exactly the bound is admitted")
	assert.Equal(t, 1<<22, n)
}

// TestCaseCount_RefusesWhatNoProductHolds holds each guard of CaseCount to
// the product it refuses.
func TestCaseCount_RefusesWhatNoProductHolds(t *testing.T) {
	like := func(name, like string) harness.Axis {
		return harness.Axis{Name: "a", Values: []harness.Value{{Name: "x"}, {Name: name, Like: like}}}
	}
	for _, tc := range []struct {
		name  string
		cases harness.Cases
		want  string
	}{
		{"no cases", nil, "no cases"},
		{"no axis", grid{}, "no axis"},
		{"an axis with no value", grid{axes: []harness.Axis{axis("a", "x"), axis("b")}}, `axis "b" has no value`},
		{"two values of one name", grid{axes: []harness.Axis{axis("a", "x", "y", "x")}}, `two values named "x"`},
		{"two axes of one name", grid{axes: []harness.Axis{axis("a", "x"), axis("a", "y")}}, `two axes named "a"`},
		{"an axis with no name", grid{axes: []harness.Axis{axis("", "x")}}, "an axis has no name"},
		{"a value with no name", grid{axes: []harness.Axis{axis("a", "x", "")}}, "a value of axis a has no name"},
		{"a value named with a slash", grid{axes: []harness.Axis{axis("a", "x/y")}}, `"x/y"`},
		{"a value named with a tab", grid{axes: []harness.Axis{axis("a", "x\ty")}}, `"x\ty"`},
		{"a like naming no value", grid{axes: []harness.Axis{like("y", "z")}}, `is like "z", no core value`},
		{"a like naming a value like another", grid{axes: []harness.Axis{like("y", "y")}}, `is like "y", no core value`},
		{"a product past the bound", grid{axes: []harness.Axis{numbered("a", 5), numbered("b", 838861)}}, "past 4194304 cases"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := harness.CaseCount(tc.cases)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
		})
	}
	assert.Equal(t, 1<<22+1, 5*838861, "the last row is one case past the bound")
}

func TestCoordinates_NameEachCaseByItsIndices(t *testing.T) {
	g := grid{axes: threeAxes()}
	at, err := harness.Coordinates(g, 7)
	require.NoError(t, err)
	assert.Equal(t, []int{1, 0, 1}, at, "the last axis counts fastest")

	for _, i := range []int{-1, 12} {
		_, err := harness.Coordinates(g, i)
		require.Error(t, err, "%d", i)
		assert.Contains(t, err.Error(), fmt.Sprintf("case %d is outside the 12 cases", i))
	}
	_, err = harness.Coordinates(grid{}, 0)
	require.Error(t, err)
}

func TestCore_AdmitsTheCasesOfCoreValuesAlone(t *testing.T) {
	core := harness.Core(grid{axes: threeAxes()})
	assert.True(t, core([]int{1, 2, 0}))
	assert.False(t, core([]int{1, 2, 1}), "y is like x")
	first := threeAxes()
	first[0].Values[1].Like = "a0"
	assert.False(t, harness.Core(grid{axes: first})([]int{1, 2, 0}), "a1 is like a0, on the first axis")
	assert.False(t, core([]int{1, 2}), "no case of the product")
	assert.False(t, core([]int{1, 3, 0}), "no case of the product")
	assert.False(t, core([]int{-1, 0, 0}), "no case of the product")
	assert.False(t, harness.Core(nil)(nil), "no cases hold no case")
	assert.False(t, harness.Core(grid{})(nil), "a product CaseCount refuses holds no case")
}

// TestCheckCases_ResultsFollowTheIndexOrder pins which cases a run reports and
// how: those pick admits and Write writes, in index order, each named by its
// values and by the core case it projects to, and each case oracle declining
// for want of a neighbour, as no document here names one.
func TestCheckCases_ResultsFollowTheIndexOrder(t *testing.T) {
	g := grid{axes: threeAxes(), skip: func(at []int) bool { return at[1] == 1 }}
	results, err := harness.CheckCases(t.Context(), g, func(at []int) bool { return at[0] == 1 })
	require.NoError(t, err)

	specs, likes := make([]string, 0, len(results)), make([]string, 0, len(results))
	for _, r := range results {
		specs, likes = append(specs, r.Spec), append(likes, r.Like)
		assert.Equal(t, harness.OutcomeOK, r.Outcome, r.Spec)
		assert.Equal(t, idleVerdicts(), r.Verdicts, r.Spec)
	}
	assert.Equal(t, []string{"a1/b0/x", "a1/b0/y", "a1/b2/x", "a1/b2/y"}, specs)
	assert.Equal(t, []string{"a1/b0/x", "a1/b0/x", "a1/b2/x", "a1/b2/x"}, likes)

	all, err := harness.CheckCases(t.Context(), grid{axes: threeAxes()}, func([]int) bool { return true })
	require.NoError(t, err)
	names := make([]string, len(all))
	for i, r := range all {
		names[i] = r.Spec
	}
	assert.Len(t, names, 12)
	assert.Len(t, slices.Compact(slices.Sorted(slices.Values(names))), 12, "every case has a name of its own")
}

func TestCheckCases_RefusesACallersMistakes(t *testing.T) {
	g := grid{axes: threeAxes()}
	all := func([]int) bool { return true }
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	contexts := map[string]context.Context{"live": t.Context(), "canceled": canceled, "nil": nil}
	for _, tc := range []struct {
		name, ctx string
		cases     harness.Cases
		pick      func([]int) bool
		want      string
	}{
		{"a nil context", "nil", g, all, "nil context"},
		{"no pick", "live", g, nil, "no pick"},
		{"cases CaseCount refuses", "live", grid{}, all, "no axis"},
		{"a canceled context", "canceled", g, all, "case 0: context canceled"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			results, err := harness.CheckCases(contexts[tc.ctx], tc.cases, tc.pick)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
			assert.Nil(t, results)
		})
	}
}

func TestCheckCase_AnswersForOneCase(t *testing.T) {
	g := grid{axes: threeAxes(), skip: func(at []int) bool { return at[1] == 1 }}
	r, ok := harness.CheckCase(t.Context(), g, []int{0, 2, 1})
	require.True(t, ok)
	assert.Equal(t, "a0/b2/y", r.Spec)
	assert.Equal(t, "a0/b2/x", r.Like)
	assert.Equal(t, idleVerdicts(), r.Verdicts)

	r, ok = harness.CheckCase(t.Context(), g, []int{0, 1, 0})
	assert.False(t, ok, "the case writes nothing")
	assert.Equal(t, harness.Result{Spec: "a0/b1/x"}, r)
}

// parted is a fake product of one axis whose first case names a partner and
// the given parts, each case writing a tiny spec.
type parted struct{ parts [][]int }

func (parted) Axes() []harness.Axis { return []harness.Axis{axis("n", "a", "b", "c")} }

func (p parted) Write(at []int) (harness.Document, bool) {
	doc := harness.Document{Sources: []compilers.Source{{Path: "spec.yaml", Data: []byte(testspec.Tiny)}}}
	if at[0] == 0 {
		doc.Equal = &harness.Equal{To: []int{1}, Rule: "docs/compilers/openapi.md#ref-use-site",
			Relocate: func(s string) string { return s }, Parts: p.parts}
	}
	return doc, true
}

// TestCheckCase_NamesTheParts holds a result to the parts its case names, and a
// part outside the product or naming the case itself to a panic: either is
// the product's mistake.
func TestCheckCase_NamesTheParts(t *testing.T) {
	r, ok := harness.CheckCase(t.Context(), parted{parts: [][]int{{1}, {2}}}, []int{0})
	require.True(t, ok)
	assert.Equal(t, []string{"b", "c"}, r.Parts)
	assert.Equal(t, harness.OutcomeOK, r.Outcome)

	for _, bad := range [][]int{{3}, {0}, nil} {
		r, ok = harness.CheckCase(t.Context(), parted{parts: [][]int{bad}}, []int{0})
		require.True(t, ok)
		assert.Equal(t, harness.OutcomePanic, r.Outcome, "%v", bad)
		assert.Contains(t, r.Detail, "outside the product or itself", "%v", bad)
	}
}

func TestCheckCase_ACallersMistakeIsAnErrorOutcome(t *testing.T) {
	g := grid{axes: threeAxes()}
	for _, tc := range []struct {
		name, spec, want string
		nilContext       bool
		cases            harness.Cases
		at               []int
	}{
		{"cases CaseCount refuses", "", "no axis", false, grid{}, nil},
		{"a case outside the product", "", "case [0 3 0] is outside the product", false, g, []int{0, 3, 0}},
		{"too few indices", "", "case [0 0] is outside the product", false, g, []int{0, 0}},
		{"a nil context", "a0/b0/x", "harness: nil context", true, g, []int{0, 0, 0}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := t.Context()
			if tc.nilContext {
				ctx = nil
			}
			r, ok := harness.CheckCase(ctx, tc.cases, tc.at)
			require.True(t, ok)
			assert.Equal(t, harness.OutcomeError, r.Outcome)
			assert.Contains(t, r.Detail, tc.want)
			assert.Equal(t, tc.spec, r.Spec)
			for _, v := range r.Verdicts {
				assert.Equal(t, harness.DeclineNotReached, v.Declined, v.Oracle)
			}
			assert.Len(t, r.Verdicts, len(harness.CaseOracles()))
		})
	}
}

func TestCaseSummary_CountsTheCaseOracles(t *testing.T) {
	results, err := harness.CheckCases(t.Context(), grid{axes: threeAxes()}, func([]int) bool { return true })
	require.NoError(t, err)

	got := harness.CaseSummary(results)

	want := "oracle    asked  found declined\n" +
		"spelling      0      0       12  no partner 12\n" +
		"presence      0      0       12  no construct 12\n" +
		"instance      0      0       12  no instance 12\n"
	assert.Equal(t, want, got)
	assert.NotContains(t, got, string(harness.OracleNoPanic), "a case summary counts the case oracles alone")
	assert.True(t, strings.HasPrefix(harness.Summary(results), "oracle "))
}

// counted is a grid counting the calls of its Axes.
type counted struct {
	grid
	calls *int
}

func (c counted) Axes() []harness.Axis {
	*c.calls++
	return c.grid.Axes()
}

// TestEach_WalksTheProductReadingItsAxesOnce walks a 2x3x2 product: every
// case in index order, at the coordinates Coordinates gives, with one call of
// Axes, where a walk through Coordinates makes one per case.
func TestEach_WalksTheProductReadingItsAxesOnce(t *testing.T) {
	calls := 0
	c := counted{axes: threeAxes(), calls: &calls}
	each, err := harness.Each(c)
	require.NoError(t, err)

	var got [][]int
	for i, at := range each {
		require.Len(t, got, i)
		got = append(got, at)
		if i == 5 {
			break
		}
	}
	assert.Equal(t, 1, calls, "Each reads the axes once")
	require.Len(t, got, 6, "the walk stops where its caller does")
	want, err := harness.Coordinates(c, 5)
	require.NoError(t, err)
	assert.Equal(t, want, got[5])

	_, err = harness.Each(grid{})
	require.Error(t, err)
}
