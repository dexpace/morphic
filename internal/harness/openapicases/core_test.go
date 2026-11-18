// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package openapicases_test

import (
	"cmp"
	"maps"
	"slices"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/internal/harness"
	"github.com/dexpace/morphic/internal/harness/openapicases"
)

// run is what the case oracles found at the cases of a product a pick admits,
// checked once for every test that reads it, and whether every shard checked.
type run struct {
	once    sync.Once
	results []harness.Result
	checked bool
}

// The runs of the schema cases and of the suite groups: their core cases, and
// every case (TestFull_EveryCaseHoldsTheRecords).
var schemaRun, groupRun, schemaFull, groupFull run

// coreResults returns what the case oracles found at the schema cases' core.
func coreResults(t *testing.T) []harness.Result {
	t.Helper()
	c := openapicases.New()
	return schemaRun.of(t, c, harness.Core(c))
}

// groupResults returns what the case oracles found at the suite groups' core.
func groupResults(t *testing.T) []harness.Result {
	t.Helper()
	c := openapicases.Groups()
	return groupRun.of(t, c, harness.Core(c))
}

// of returns what the case oracles found at c's cases pick admits, in index
// order. The first test asking checks them, a shard of the first two axes'
// values per parallel subtest of its own.
func (r *run) of(t *testing.T, c harness.Cases, pick func([]int) bool) []harness.Result {
	t.Helper()
	r.once.Do(func() { r.results, r.checked = checkShards(t, c, pick) })
	require.True(t, r.checked, "a shard of the cases did not check")
	return r.results
}

// checkShards checks c's cases pick admits shard by shard in parallel subtests
// of t, and reports whether each shard checked.
func checkShards(t *testing.T, c harness.Cases, pick func([]int) bool) ([]harness.Result, bool) {
	keys := shardKeys(t, c, pick)
	parts := make([][]harness.Result, len(keys))
	axes := c.Axes()
	checked := t.Run("cases", func(t *testing.T) {
		for i, key := range keys {
			t.Run(axes[0].Values[key[0]].Name+"/"+axes[1].Values[key[1]].Name, func(t *testing.T) {
				t.Parallel()
				results, err := harness.CheckCases(t.Context(), c, func(at []int) bool {
					return at[0] == key[0] && at[1] == key[1] && pick(at)
				})
				require.NoError(t, err)
				parts[i] = results
			})
		}
	})
	return slices.Concat(parts...), checked
}

// shardKeys returns the version and target of every case of c pick admits,
// each once, in order, holding no case's coordinates.
func shardKeys(t *testing.T, c harness.Cases, pick func([]int) bool) [][2]int {
	t.Helper()
	seen := map[[2]int]bool{}
	each, err := harness.Each(c)
	require.NoError(t, err)
	for _, at := range each {
		if pick(at) {
			seen[[2]int{at[0], at[1]}] = true
		}
	}
	return slices.SortedFunc(maps.Keys(seen), func(a, b [2]int) int {
		return cmp.Or(cmp.Compare(a[0], b[0]), cmp.Compare(a[1], b[1]))
	})
}
