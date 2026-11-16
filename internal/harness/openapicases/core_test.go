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

// coreRun is what the case oracles found at the core cases, checked once for
// every test that reads it, and whether every shard checked.
var coreRun struct {
	once    sync.Once
	results []harness.Result
	checked bool
}

// coreResults returns what the case oracles found at the core cases, in index
// order. The first test asking checks them, a shard of one version and target
// per parallel subtest of its own.
func coreResults(t *testing.T) []harness.Result {
	t.Helper()
	coreRun.once.Do(func() { coreRun.results, coreRun.checked = checkCore(t) })
	require.True(t, coreRun.checked, "a shard of the core cases did not check")
	return coreRun.results
}

// checkCore checks the core cases shard by shard in parallel subtests of t,
// and reports whether each shard checked.
func checkCore(t *testing.T) ([]harness.Result, bool) {
	c := openapicases.New()
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
