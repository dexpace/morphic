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

// run is what the case oracles found at a product's core cases, checked once
// for every test that reads it, and whether every shard checked.
type run struct {
	once    sync.Once
	results []harness.Result
	checked bool
}

// The runs of the schema cases and of the suite groups.
var schemaRun, groupRun run

// coreResults returns what the case oracles found at the schema cases' core.
func coreResults(t *testing.T) []harness.Result {
	t.Helper()
	return schemaRun.of(t, openapicases.New())
}

// groupResults returns what the case oracles found at the suite groups' core.
func groupResults(t *testing.T) []harness.Result {
	t.Helper()
	return groupRun.of(t, openapicases.Groups())
}

// of returns what the case oracles found at c's core cases, in index order.
// The first test asking checks them, a shard of the first two axes' values per
// parallel subtest of its own.
func (r *run) of(t *testing.T, c harness.Cases) []harness.Result {
	t.Helper()
	r.once.Do(func() { r.results, r.checked = checkCore(t, c) })
	require.True(t, r.checked, "a shard of the core cases did not check")
	return r.results
}

// checkCore checks c's core cases shard by shard in parallel subtests of t,
// and reports whether each shard checked.
func checkCore(t *testing.T, c harness.Cases) ([]harness.Result, bool) {
	core := harness.Core(c)
	byShard := shards(t, c, core)
	keys := slices.SortedFunc(maps.Keys(byShard), func(a, b [2]int) int {
		return cmp.Or(cmp.Compare(a[0], b[0]), cmp.Compare(a[1], b[1]))
	})
	parts := make([][]harness.Result, len(keys))
	axes := c.Axes()
	checked := t.Run("core", func(t *testing.T) {
		for i, key := range keys {
			t.Run(axes[0].Values[key[0]].Name+"/"+axes[1].Values[key[1]].Name, func(t *testing.T) {
				t.Parallel()
				results, err := harness.CheckCases(t.Context(), c, func(at []int) bool {
					return at[0] == key[0] && at[1] == key[1] && core(at)
				})
				require.NoError(t, err)
				parts[i] = results
			})
		}
	})
	return slices.Concat(parts...), checked
}
