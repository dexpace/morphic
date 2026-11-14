// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package overlay

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/budget"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/jsonpath"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
	"github.com/dexpace/morphic/ir"
)

// stepsHead opens each source below.
const stepsHead = "openapi: 3.1.0\ninfo: {title: T, version: '1'}\npaths: {}\n"

// applyWithinSteps applies ov to root with the steps all of its selectors may
// spend limited to steps. It is the one place the tests of the step bound name
// the package's seam for it: applyWithLimits, applyWithin with the bound on
// steps as a second limit after the attribution walk's, so that the tests that
// call applyWithin keep its signature.
func applyWithinSteps(t testing.TB, root *yaml.Node, ov string, steps int) (Origin, []ir.Diagnostic) {
	t.Helper()
	return applyWithLimits(t.Context(), 1, root, docOf(t, ov),
		Options{Path: "o.yaml", Data: []byte(ov)}, budget.AtMost(maxNodes), budget.AtMost(steps))
}

// chainSource is a source whose x-deep member is a chain of n mappings, each
// the value of the member a of the one before, the last an empty mapping: so
// every a holds an object, and an update selecting several is of one kind, as
// Overlay 1.1 requires of the nodes an update selects.
func chainSource(n int) string {
	return stepsHead + "x-deep: " + strings.Repeat("{a: ", n) + "{}" + strings.Repeat("}", n) + "\n"
}

// needOf returns the steps the query costs over the value src stands for,
// measured by the evaluator itself.
func needOf(t testing.TB, src, query string) int {
	t.Helper()
	q, err := jsonpath.Parse(query)
	require.NoError(t, err)
	b := budget.New(budget.Unbounded())
	_, err = q.Select(t.Context(), nodeview.New(), nodeview.DocumentRoot(docOf(t, src)), &b)
	require.NoError(t, err)
	return b.Spent()
}

// TestMaxSteps_IsTheBoundOnOneApplication pins the constant the steps of one
// application are bounded by, which is about 264 scans of the golden spec.
func TestMaxSteps_IsTheBoundOnOneApplication(t *testing.T) {
	t.Parallel()
	assert.Equal(t, 1<<28, MaxSteps)
}

// TestApply_RefusesATargetThatCostsMoreThanTheStepsAnOverlayMaySpend pins the
// bound on a selector over a hostile source: a descendant segment four deep over
// a chain of 3,000 would return billions of nodes, and is refused as exceeding
// the budget, at the action, with nothing applied and the tree as it was.
func TestApply_RefusesATargetThatCostsMoreThanTheStepsAnOverlayMaySpend(t *testing.T) {
	t.Parallel()
	src := chainSource(3000)
	ov := "overlay: 1.1.0\ninfo: {title: o, version: \"1\"}\nactions:\n  - target: $..a..a..a..a\n    update: {t: 1}\n"
	root := docOf(t, src)
	before, err := yaml.Marshal(root)
	require.NoError(t, err)

	origin, diags := applyWithinSteps(t, root, ov, 1_000_000)

	assert.False(t, origin.Applied())
	require.Len(t, diags, 1, "%+v", diags)
	assert.Equal(t, ir.SeverityError, diags[0].Severity)
	assert.Equal(t, diag.BudgetExceeded, diags[0].Code)
	assert.Equal(t, "/actions/0", string(diags[0].Provenance.Pointer))
	assert.Contains(t, diags[0].Message, "steps")
	after, err := yaml.Marshal(root)
	require.NoError(t, err)
	assert.Equal(t, string(before), string(after), "nothing was applied")
}

// TestApply_SpendsOneBudgetOnEverySelectorOfAnApplication pins that the bound
// is the application's and not each action's: two actions of one cost, and a
// bound of one and a half of them, apply the first and refuse the second, at
// it, where a bound per action would admit both. A bound with the measured
// need and a margin applies one. The target walks the whole chain and selects
// its top alone, so the member the first action writes there leaves the
// second's cost all but the first's.
func TestApply_SpendsOneBudgetOnEverySelectorOfAnApplication(t *testing.T) {
	t.Parallel()
	src := chainSource(40)
	action := "  - target: $[?@..a..a]\n    update: {t: 1}\n"
	head := "overlay: 1.1.0\ninfo: {title: o, version: \"1\"}\nactions:\n"
	need := needOf(t, src, "$[?@..a..a]")
	require.Greater(t, need, 100, "the selector costs something")

	origin, diags := applyWithinSteps(t, docOf(t, src), head+action, need+need/4+1000)
	assert.True(t, origin.Applied(), "one action within its need and a margin: %+v", diags)

	origin, diags = applyWithinSteps(t, docOf(t, src), head+action+action, need+need/2)
	assert.False(t, origin.Applied(), "the second action finds the budget spent")
	require.NotEmpty(t, diags)
	assert.Equal(t, diag.BudgetExceeded, diags[0].Code, "%+v", diags)
	assert.Equal(t, "/actions/1", string(diags[0].Provenance.Pointer), "at the action that found it spent")

	origin, diags = applyWithinSteps(t, docOf(t, src), head+action, need/2)
	assert.False(t, origin.Applied(), "a bound under the need refuses even the first")
	require.NotEmpty(t, diags)
	assert.Equal(t, "/actions/0", string(diags[0].Provenance.Pointer))
}

// pathsSource holds n paths each with a get operation.
func pathsSource(n int) string {
	var b strings.Builder
	b.WriteString("openapi: 3.1.0\ninfo: {title: T, version: '1'}\npaths:\n")
	for i := range n {
		fmt.Fprintf(&b, "  /p%d: {get: {operationId: op%d, responses: {'200': {description: ok}}}}\n", i, i)
	}
	return b.String()
}

// actionsFor is an overlay of n actions, each updating one path's get.
func actionsFor(n int) string {
	var b strings.Builder
	b.WriteString("overlay: 1.1.0\ninfo: {title: o, version: \"1\"}\nactions:\n")
	for i := range n {
		fmt.Fprintf(&b, "  - target: $.paths['/p%d'].get\n    update: {summary: s%d}\n", i, i)
	}
	return b.String()
}

// minimalSteps returns the least bound within which the overlay of n actions
// applies to the source of paths paths, found by halving: the steps its
// selectors spend, read as a count and never as a time.
func minimalSteps(t testing.TB, paths, n int) int {
	t.Helper()
	src, ov := pathsSource(paths), actionsFor(n)
	lo, hi := 0, 1<<26
	origin, diags := applyWithinSteps(t, docOf(t, src), ov, hi)
	require.True(t, origin.Applied(), "within %d steps: %+v", hi, diags)
	for lo < hi {
		mid := lo + (hi-lo)/2
		if origin, _ := applyWithinSteps(t, docOf(t, src), ov, mid); origin.Applied() {
			hi = mid
			continue
		}
		lo = mid + 1
	}
	return lo
}

// TestApply_SpendsStepsInProportionToTheActions pins the cost of an overlay's
// selectors against the number of its actions over one source: the steps
// spent for 250, 500 and 1,000 actions each naming one path of a 1,000-path
// source grow by what the actions added, so the second doubling costs twice the
// first and no more.
func TestApply_SpendsStepsInProportionToTheActions(t *testing.T) {
	t.Parallel()
	const paths = 1000
	steps := map[int]int{}
	for _, n := range []int{250, 500, 1000} {
		steps[n] = minimalSteps(t, paths, n)
	}

	first, second := steps[500]-steps[250], steps[1000]-steps[500]

	assert.Positive(t, first, "%v", steps)
	assert.InDelta(t, 2*first, second, float64(first)/20, "marginal steps double with the actions: %v", steps)
}
