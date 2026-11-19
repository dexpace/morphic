// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package scratchstate_test

import (
	"go/types"
	"reflect"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/internal/decls"
	"github.com/dexpace/morphic/scratchstate"
)

// fill adds n entries to m.
func fill(m map[string]int, n int) {
	for i := range n {
		m[strconv.Itoa(i)] = i
	}
}

// TestMap_EmptiesInPlaceUpToTheLimit holds Emptied to reusing the map it holds
// while the last collection left it at most scratchstate.Limit entries, and to handing
// a fresh one past that: Go never shrinks a map, so emptying one that held a
// huge collection costs in proportion to it, at every collection after.
func TestMap_EmptiesInPlaceUpToTheLimit(t *testing.T) {
	t.Parallel()
	var mapValue scratchstate.Map[string, int]
	first := mapValue.Emptied()
	require.NotNil(t, first, "the zero Map hands a map to write to")

	for _, n := range []int{1, 2} {
		fill(mapValue.Emptied(), n)
		assert.Empty(t, mapValue.Emptied(), "a map left with %d entries is emptied", n)
	}

	fill(first, scratchstate.Limit)
	atLimit := mapValue.Emptied()
	assert.Empty(t, atLimit)
	assert.Equal(t, reflect.ValueOf(first).UnsafePointer(), reflect.ValueOf(atLimit).UnsafePointer(),
		"a map left at the limit is emptied in place")

	fill(atLimit, scratchstate.Limit+1)
	past := mapValue.Emptied()
	assert.Empty(t, past)
	assert.NotEqual(t, reflect.ValueOf(atLimit).UnsafePointer(), reflect.ValueOf(past).UnsafePointer(),
		"a map left past the limit is replaced")
	assert.Equal(t, reflect.ValueOf(past).UnsafePointer(), reflect.ValueOf(mapValue.Emptied()).UnsafePointer(),
		"the fresh map is the one reused from then on")
}

// TestMap_LimitIsTheMostAMapMayHoldAndBeReused pins the limit's value, which
// the cost of a collection after a large one is bounded by.
func TestMap_LimitIsTheMostAMapMayHoldAndBeReused(t *testing.T) {
	t.Parallel()
	assert.Equal(t, 1024, scratchstate.Limit)
}

// TestMap_ClearsOnlyThroughEmptied holds the package to emptying a map in the
// one place the limit is checked: only Emptied clears, and nothing deletes.
func TestMap_ClearsOnlyThroughEmptied(t *testing.T) {
	t.Parallel()
	loadedPackage, err := decls.Load(reflect.TypeFor[scratchstate.Map[string, int]]().PkgPath())
	require.NoError(t, err)
	assert.Equal(t, []string{"Emptied"}, loadedPackage.FuncsUsing(types.Universe.Lookup("clear")))
	assert.Empty(t, loadedPackage.FuncsUsing(types.Universe.Lookup("delete")))
}
