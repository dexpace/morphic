// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package ir_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// stringValues returns n string values, each its own.
func stringValues(n int) []ir.Value {
	list := make([]ir.Value, n)
	for i := range list {
		list[i] = ir.Value{Kind: ir.ValueString, Str: "s"}
	}
	return list
}

// doublingValue returns a value nested depth levels, each a list holding the
// level below twice, so the two elements share one backing array and the tree
// the encoding writes has 2^depth leaves.
func doublingValue(depth int) ir.Value {
	v := ir.Value{Kind: ir.ValueNull}
	for range depth {
		v = ir.Value{Kind: ir.ValueList, List: []ir.Value{v, v}}
	}
	return v
}

// sharedExamples returns 21 examples holding a list of 200 strings: the first
// as its Headers and the rest as their Value. All hold one pointer to it when
// shared, and a list of their own when not.
func sharedExamples(shared bool) []ir.Example {
	one := &ir.Value{Kind: ir.ValueList, List: stringValues(200)}
	examples := make([]ir.Example, 21)
	for i := range examples {
		held := one
		if !shared {
			held = &ir.Value{Kind: ir.ValueList, List: stringValues(200)}
		}
		if i == 0 {
			examples[i] = ir.Example{Name: "h", Headers: held}
			continue
		}
		examples[i] = ir.Example{Value: held}
	}
	return examples
}

// skipsHeaders returns false for a path ending ".Headers" and true elsewhere.
func skipsHeaders(_ reflect.Value, at ir.WalkPath) bool {
	return !strings.HasSuffix(at.String(), ".Headers")
}

// valueOrHeaders returns false for every ir.Value and for a path ending
// ".Headers", true elsewhere.
func valueOrHeaders(v reflect.Value, at ir.WalkPath) bool {
	return v.Type() != reflect.TypeFor[ir.Value]() && skipsHeaders(v, at)
}

// shownBy returns how many values a walk of root shows a counting visitor with
// no limit but the real one.
func shownBy(root any) int {
	var counting shownCounter
	ir.WalkValues(root, "doc", &counting)
	return counting.n
}

// TestWalkValues_ReadsToItsLimit holds a visitor to the values its limit lets
// it be shown, and the result to whether any were left. A list holding the
// level below twice, which shares one slice between its elements, is read from
// each, so it shows 2^depth leaves and what lies between them.
func TestWalkValues_ReadsToItsLimit(t *testing.T) {
	t.Parallel()
	const limit = 1 << 16
	for _, tc := range []struct {
		depth     int
		wantShown int
		wantCut   bool
	}{
		{10, 20_471, false},
		{12, limit, true},
		{14, limit, true},
	} {
		v := doublingValue(tc.depth)
		var counting shownCounter

		cut := ir.WalkValuesWithin(limit, &v, "doc", &counting)

		assert.Equal(t, tc.wantShown, counting.n, "depth %d: values shown", tc.depth)
		assert.Equal(t, tc.wantCut, cut, "depth %d: whether the walk stopped", tc.depth)
	}
}

// TestWalkValues_TheLimitAndOneFewer holds the edge of the limit: a walk whose
// limit is the values it shows is whole, and one value fewer stops it with that
// many shown.
func TestWalkValues_TheLimitAndOneFewer(t *testing.T) {
	t.Parallel()
	for _, n := range []int{1, 5, 100, 1000} {
		flat := make([]ir.Value, n)
		whole := shownBy(&flat)
		require.Greater(t, whole, n, "the walk shows each of %d values and more", n)

		var atLimit, below shownCounter
		assert.False(t, ir.WalkValuesWithin(whole, &flat, "doc", &atLimit), "the limit of %d is what the walk shows", whole)
		assert.Equal(t, whole, atLimit.n)
		assert.True(t, ir.WalkValuesWithin(whole-1, &flat, "doc", &below), "one fewer than %d", whole)
		assert.Equal(t, whole-1, below.n)
	}
}

// TestWalkValues_ASharedPointerIsReadFromEveryHolder holds the walk to the
// document its encoding writes: a pointer two fields hold is read from each,
// so the least limit that leaves nothing unread is the same for one list shared
// by 21 examples and for 21 lists of their own, and does not move with the
// visitors beside the one that is shown it all, whatever they prune.
func TestWalkValues_ASharedPointerIsReadFromEveryHolder(t *testing.T) {
	t.Parallel()
	const least = 42_442
	pruning := ir.VisitFunc(valueOrHeaders)
	for _, shared := range []bool{true, false} {
		examples := sharedExamples(shared)
		for name, visitors := range map[string][]ir.Visitor{
			"one visitor":                    {&shownCounter{}},
			"beside one that prunes at will": {&shownCounter{}, pruning},
		} {
			assert.False(t, ir.WalkValuesWithin(least, examples, "doc", visitors...),
				"shared %t, %s: %d values are the whole", shared, name, least)
			assert.True(t, ir.WalkValuesWithin(least-1, examples, "doc", visitors...),
				"shared %t, %s: one fewer leaves a value unread", shared, name)
		}
	}
}

// cycleExamples returns two examples, the one holding the value in Headers and
// the other in Value, each a constructor whose arguments are a list of 300
// strings and the constructor itself, both through one pointer.
func cycleExamples() []ir.Example {
	d := &ir.CtorValue{Scalar: "t/s", Args: []ir.Value{{Kind: ir.ValueList, List: stringValues(300)}}}
	d.Args = append(d.Args, ir.Value{Kind: ir.ValueCtor, Ctor: d})
	return []ir.Example{
		{Name: "h", Headers: &ir.Value{Kind: ir.ValueCtor, Ctor: d}},
		{Name: "v", Value: &ir.Value{Kind: ir.ValueCtor, Ctor: d}},
	}
}

// TestWalkValues_ACycleIsCutWhereItCloses holds a reference met again inside
// itself to ending the walk there, for each visitor that entered it: the walk
// of two examples sharing one constructor that holds itself has a whole of 6,091
// values, and is not left to run to the limit. One visitor enters the pointer
// first, at the Value holder, and the other meets its back edge fresh, which a
// cycle guard that remembers what an earlier visitor entered would take for
// sharing.
func TestWalkValues_ACycleIsCutWhereItCloses(t *testing.T) {
	t.Parallel()
	const least = 6091
	examples := cycleExamples()
	skipping := ir.VisitFunc(skipsHeaders)

	assert.False(t, ir.WalkValuesWithin(least, examples, "doc", &shownCounter{}, skipping),
		"the cycle ends the walk at %d values", least)
	assert.True(t, ir.WalkValuesWithin(least-1, examples, "doc", &shownCounter{}, skipping),
		"one fewer than the whole is cut")
	assert.False(t, ir.WalkValuesWithin(least, examples, "doc", skipping, &shownCounter{}),
		"in either order of the visitors")
}

// TestWalkValues_ACycleIsToldOncePerHolderEntered holds a visitor told of
// cycles to being told of the one it enters at each holder, and to being told
// what it would alone with the others beside it: the visitor skipping Headers
// enters the pointer once, at Value, and is told once.
func TestWalkValues_ACycleIsToldOncePerHolderEntered(t *testing.T) {
	t.Parallel()
	examples := cycleExamples()
	rules := []func(string) bool{nil, func(path string) bool { return strings.HasSuffix(path, ".Headers") }}

	shared, alone, cut, aloneCut := sharedAndAlone(examples, rules)

	assert.False(t, cut)
	assert.False(t, aloneCut)
	assertSameWalks(t, shared, alone, "a constructor holding itself, held twice")
	require.Len(t, shared[0].cycles, 2, "the visitor that enters both holders is told at each")
	assert.True(t, strings.HasPrefix(shared[0].cycles[0].Entered, "doc[0].Headers"), "%+v", shared[0].cycles[0])
	assert.True(t, strings.HasPrefix(shared[0].cycles[1].Entered, "doc[1].Value"), "%+v", shared[0].cycles[1])
	require.Len(t, shared[1].cycles, 1, "the visitor that skips Headers enters one holder")
	assert.True(t, strings.HasPrefix(shared[1].cycles[0].Entered, "doc[1].Value"), "%+v", shared[1].cycles[0])
}

// pruneAtRoot counts the values it is shown and prunes at the first.
type pruneAtRoot struct{ n int }

func (p *pruneAtRoot) Visit(reflect.Value, ir.WalkPath) bool {
	p.n++
	return false
}

// countersOf returns n counting visitors, each its own, and the same as
// visitors.
func countersOf(n int) ([]*shownCounter, []ir.Visitor) {
	counters := make([]*shownCounter, n)
	visitors := make([]ir.Visitor, n)
	for i := range counters {
		counters[i] = &shownCounter{}
		visitors[i] = counters[i]
	}
	return counters, visitors
}

// TestWalkValues_AVisitorIsShownTheSameWalkBesideOthers holds the limit to each
// visitor alone: what one is shown, and whether it is cut, is the same alone,
// beside any others, and beside enough of them to need more than one traversal.
func TestWalkValues_AVisitorIsShownTheSameWalkBesideOthers(t *testing.T) {
	t.Parallel()
	const limit = 1500
	root := ir.Value{Kind: ir.ValueList, List: stringValues(1000)}

	t.Run("130 visitors are each shown the limit", func(t *testing.T) {
		t.Parallel()
		counters, visitors := countersOf(130)
		assert.True(t, ir.WalkValuesWithin(limit, &root, "doc", visitors...))
		var total int
		for i, c := range counters {
			assert.Equal(t, limit, c.n, "visitor %d", i)
			total += c.n
		}
		assert.Equal(t, 195_000, total)
	})

	t.Run("a visitor pruning at the root is shown one value", func(t *testing.T) {
		t.Parallel()
		for _, beside := range []int{0, 1, 199} {
			var pruning pruneAtRoot
			_, others := countersOf(beside)
			ir.WalkValuesWithin(limit, &root, "doc", append([]ir.Visitor{&pruning}, others...)...)
			assert.Equal(t, 1, pruning.n, "beside %d visitors", beside)
		}
	})

	t.Run("a visitor records the same paths alone and beside another", func(t *testing.T) {
		t.Parallel()
		examples := sharedExamples(true)
		for _, atLimit := range []int{1, 100, ir.MaxWalkValues} {
			alone := &recorder{prune: func(path string) bool { return strings.HasSuffix(path, ".Headers") }}
			beside := &recorder{prune: alone.prune}
			ir.WalkValuesWithin(atLimit, examples, "doc", alone)
			ir.WalkValuesWithin(atLimit, examples, "doc", &shownCounter{}, beside)

			require.NotEmpty(t, alone.paths, "limit %d", atLimit)
			assert.LessOrEqual(t, len(alone.paths), atLimit)
			assert.Empty(t, cmp.Diff(alone.paths, beside.paths), "limit %d: the paths recorded (-alone +beside)", atLimit)
		}
	})
}

// TestWalkLimits_ClearTheGoldenDocument holds both limits to a size the golden
// document fits in many times over: it shows one visitor 7,913,347 values and
// its walks take 595 and 3,778 steps.
func TestWalkLimits_ClearTheGoldenDocument(t *testing.T) {
	t.Parallel()
	assert.GreaterOrEqual(t, ir.MaxWalkValues, 16*7_913_347)
	assert.GreaterOrEqual(t, ir.MaxWalkSteps, 16*(595+3_778))
}
