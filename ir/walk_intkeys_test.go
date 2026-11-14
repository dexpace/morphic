// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package ir_test

import (
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// intKeys returns a map of n integer keys from first, each holding its own.
func intKeys(first, n int) map[int]int {
	m := make(map[int]int, n)
	for k := first; k < first+n; k++ {
		m[k] = k
	}
	return m
}

// walkAllocs returns how many allocations a walk of root makes with a visitor
// that does nothing. Not for parallel tests: AllocsPerRun measures the process.
func walkAllocs(root any) float64 {
	nothing := ir.VisitFunc(func(reflect.Value, ir.WalkPath) bool { return true })
	return testing.AllocsPerRun(10, func() { ir.WalkValues(root, "doc", nothing) })
}

// TestWalkValues_IntegerKeysCostNothingFromAHundredUp holds a map keyed by
// integers to costing the same to walk however wide its keys are: a hundred
// keys from 0 spell one or two digits, from 100 three, and from 1,000 four,
// and a key that is rendered to a string to be ordered allocates once it is
// past the digits Go keeps without allocating. It is not run in parallel:
// AllocsPerRun measures the process.
func TestWalkValues_IntegerKeysCostNothingFromAHundredUp(t *testing.T) {
	roots := map[string]func(map[int]int) any{
		"a map":                     func(m map[int]int) any { return m },
		"HTTPBinding.SuccessStatus": func(m map[int]int) any { return &ir.HTTPBinding{SuccessStatus: m} },
	}
	for name, root := range roots {
		t.Run(name, func(t *testing.T) {
			from0 := walkAllocs(root(intKeys(0, 100)))
			from100 := walkAllocs(root(intKeys(100, 100)))
			from1000 := walkAllocs(root(intKeys(1000, 100)))

			assert.Equal(t, from0, from100, "keys from 0 and from 100")
			assert.Equal(t, from100, from1000, "keys from 100 and from 1,000")
		})
	}
}

// keyOrder returns the paths a walk of m shows for its keys, in walk order.
func keyOrder(m any) []string {
	var keys []string
	ir.WalkValues(m, "doc", ir.VisitFunc(func(_ reflect.Value, at ir.WalkPath) bool {
		if path := at.String(); strings.HasSuffix(path, ir.MapKeySuffix) {
			keys = append(keys, path)
		}
		return true
	}))
	return keys
}

// TestWalkValues_IntegerKeysAreVisitedInTheOrderOfTheirLabels holds the order
// of a map keyed by integers to that of the decimal spellings the walk labels
// them with, not to numeric order: "10" before "9", and "-1" first. The
// orders are those the walk has always given, which paths and findings rest on.
func TestWalkValues_IntegerKeysAreVisitedInTheOrderOfTheirLabels(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		m    any
		want []string
	}{
		{"int", map[int]bool{-1: true, 9: true, 10: true, 100: true},
			[]string{"doc[-1].key", "doc[10].key", "doc[100].key", "doc[9].key"}},
		{"uint8", map[uint8]bool{9: true, 10: true},
			[]string{"doc[10].key", "doc[9].key"}},
		{"int64 at its limits", map[int64]bool{math.MinInt64: true, 0: true, math.MaxInt64: true},
			[]string{"doc[-9223372036854775808].key", "doc[0].key", "doc[9223372036854775807].key"}},
		{"uint64 at its limit", map[uint64]bool{9: true, 10: true, math.MaxUint64: true},
			[]string{"doc[10].key", "doc[18446744073709551615].key", "doc[9].key"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Empty(t, cmp.Diff(tc.want, keyOrder(tc.m)), "key paths (-want +got)")
		})
	}
}

// TestWalkValues_IntegerKeyValuesAreVisitedInKeyOrder holds each value to the
// path of its own key, in the same order as the keys.
func TestWalkValues_IntegerKeyValuesAreVisitedInKeyOrder(t *testing.T) {
	t.Parallel()
	var got []string
	ir.WalkValues(map[int]bool{-1: true, 9: true, 10: true}, "doc", ir.VisitFunc(func(v reflect.Value, at ir.WalkPath) bool {
		if v.Kind() == reflect.Bool {
			got = append(got, at.String())
		}
		return true
	}))
	assert.Equal(t, []string{"doc[-1]", "doc[10]", "doc[9]"}, got)
}
