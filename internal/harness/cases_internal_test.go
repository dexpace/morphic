// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package harness

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/internal/testspec"
	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/pass"
)

// fake is a product of cases: its axes, each case a tiny spec, and write, when
// set, standing for Write.
type fake struct {
	axes  []Axis
	write func(at []int) (CaseDocument, bool)
}

func (f fake) Axes() []Axis { return f.axes }

func (f fake) Write(at []int) (CaseDocument, bool) {
	if f.write != nil {
		return f.write(at)
	}
	return CaseDocument{Sources: []compilers.Source{{Path: "spec.yaml", Data: []byte(testspec.Tiny)}}}, true
}

// coreAxis returns an axis of core values named as given.
func coreAxis(name string, values ...string) Axis {
	a := Axis{Name: name}
	for _, v := range values {
		a.Values = append(a.Values, Value{Name: v})
	}
	return a
}

// product223 is a 2x2x3 product: four shards of three cases each.
func product223() []Axis {
	return []Axis{coreAxis("a", "a0", "a1"), coreAxis("b", "b0", "b1"), coreAxis("c", "c0", "c1", "c2")}
}

// withAsked makes CheckCase ask oracles for the rest of the test.
func withAsked(t *testing.T, oracles ...func(context.Context, *shard, []int, CaseDocument) Verdict) {
	t.Helper()
	orig := asked
	t.Cleanup(func() { asked = orig })
	asked = oracles
}

// TestCoordinates_InvertIndexOf pins the index space over a 2x3x2 product:
// the last axis counts fastest, coordinates and indexOf are inverse over every
// index, and every case has a name of its own.
func TestCoordinates_InvertIndexOf(t *testing.T) {
	axes := []Axis{coreAxis("a", "a0", "a1"), coreAxis("b", "b0", "b1", "b2"), coreAxis("c", "c0", "c1")}
	assert.Equal(t, []int{0, 0, 1}, coordinates(axes, 1))
	assert.Equal(t, []int{0, 1, 0}, coordinates(axes, 2))
	assert.Equal(t, []int{1, 0, 0}, coordinates(axes, 6))

	names := map[string]int{}
	for i := range 12 {
		at := coordinates(axes, i)
		require.True(t, inProduct(axes, at), "%d: %v", i, at)
		assert.Equal(t, i, indexOf(axes, at), "%v", at)
		names[caseName(axes, at)] = i
	}
	assert.Len(t, names, 12)
	assert.Equal(t, 11, names["a1/b2/c1"])
}

func TestProject_ReplacesEachValueByItsLike(t *testing.T) {
	axes := []Axis{
		{Name: "v", Values: []Value{{Name: "3.0"}, {Name: "3.1"}, {Name: "3.2", Like: "3.1"}}},
		{Name: "s", Values: []Value{{Name: "none"}, {Name: "type", Like: "none"}}},
	}
	assert.Equal(t, []int{1, 0}, project(axes, []int{2, 1}))
	assert.Equal(t, []int{0, 0}, project(axes, []int{0, 0}))
	assert.Equal(t, "3.1/none", caseName(axes, project(axes, []int{2, 1})))
}

// TestCheckCases_AShardCompilesEachCaseOnce asks every case of a 2x2x3
// product for itself and the first case of its shard: the shard compiles each
// once, so twelve cases compile twelve times, and holds no more than its own
// three cases.
func TestCheckCases_AShardCompilesEachCaseOnce(t *testing.T) {
	compiles := 0
	swapCompile(t, func(context.Context, string, []byte, []pass.Pass) (*ir.Document, []ir.Diagnostic, error) {
		compiles++
		return soundDoc(), nil, nil
	})
	most := 0
	withAsked(t, func(ctx context.Context, shardValue *shard, at []int, _ CaseDocument) Verdict {
		for _, c := range [][]int{at, {at[0], at[1], 0}} {
			got, ok := shardValue.compile(ctx, c)
			require.True(t, ok)
			require.NoError(t, got.err)
			assert.NotNil(t, got.doc)
		}
		most = max(most, len(shardValue.kept))
		return passed(OracleSpelling)
	})

	results, err := CheckCases(t.Context(), fake{axes: product223()}, func([]int) bool { return true })
	require.NoError(t, err)

	assert.Len(t, results, 12)
	assert.Equal(t, 12, compiles, "a case compiled once serves every case comparing with it")
	assert.Equal(t, 3, most, "a shard holds its own cases alone")
}

func TestShardCompile_ACaseWritingNothingOrOutsideTheProduct(t *testing.T) {
	fakeValue := fake{axes: product223(), write: func(at []int) (CaseDocument, bool) { return CaseDocument{}, at[2] != 1 }}
	shard := newShard(fakeValue, fakeValue.axes, []int{0, 0, 0})

	_, ok := shard.compile(t.Context(), []int{0, 0, 1})
	assert.False(t, ok, "the case writes nothing")

	got, ok := shard.compile(t.Context(), []int{0, 0, 3})
	require.True(t, ok)
	require.Error(t, got.err)
	assert.Contains(t, got.err.Error(), "case [0 0 3] is outside the product")
	assert.Empty(t, shard.kept)
}

// TestCheckCase_AnOraclesFindingIsTheOutcome pins how a case's verdicts make
// its Result: an oracle's finding is the outcome, and an oracle with no entry
// declines for want of a neighbour.
func TestCheckCase_AnOraclesFindingIsTheOutcome(t *testing.T) {
	withAsked(t,
		func(context.Context, *shard, []int, CaseDocument) Verdict { return passed(OracleSpelling) },
		func(context.Context, *shard, []int, CaseDocument) Verdict {
			return found(OraclePresence, "changes nothing")
		},
	)

	result, ok := CheckCase(t.Context(), fake{axes: product223()}, []int{1, 0, 2})
	require.True(t, ok)

	assert.Equal(t, Result{
		Spec: "a1/b0/c2", Like: "a1/b0/c2", Outcome: OutcomeNotCarried, Detail: "changes nothing",
		Verdicts: []Verdict{
			passed(OracleSpelling), found(OraclePresence, "changes nothing"), declined(OracleInstance, DeclineNoInstance),
		},
	}, result)
}

// TestCheckCase_APanicIsAPanicOutcome pins the barrier around a case: a panic
// in Write or in an oracle is the outcome, a verdict given before it is kept,
// and the oracles not yet asked decline as not reached.
func TestCheckCase_APanicIsAPanicOutcome(t *testing.T) {
	boom := func(context.Context, *shard, []int, CaseDocument) Verdict { panic("oracle exploded") }
	withAsked(t, func(context.Context, *shard, []int, CaseDocument) Verdict { return passed(OracleSpelling) }, boom)

	result, ok := CheckCase(t.Context(), fake{axes: product223()}, []int{0, 0, 0})
	require.True(t, ok)
	assert.Equal(t, OutcomePanic, result.Outcome)
	assert.Contains(t, result.Detail, "oracle exploded")
	assert.Equal(t, []Verdict{
		passed(OracleSpelling), declined(OraclePresence, DeclineNotReached), declined(OracleInstance, DeclineNotReached),
	}, result.Verdicts)

	panicky := fake{axes: product223(), write: func([]int) (CaseDocument, bool) { panic(errors.New("write exploded")) }}
	result, ok = CheckCase(t.Context(), panicky, []int{0, 0, 0})
	require.True(t, ok, "a case whose Write panics is reported")
	assert.Equal(t, OutcomePanic, result.Outcome)
	assert.Contains(t, result.Detail, "write exploded")
	assert.Equal(t, "a0/b0/c0", result.Spec)
}

func TestIdle_NamesNoReasonForAnOracleOfASpec(t *testing.T) {
	for _, oracle := range Oracles() {
		assert.Empty(t, idle(oracle), oracle)
	}
	for _, oracle := range CaseOracles() {
		assert.NotEmpty(t, idle(oracle), oracle)
		_, finds := oracle.finds()
		assert.True(t, finds, oracle)
	}
}
