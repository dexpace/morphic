// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package pass_test

import (
	"go/types"
	"reflect"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/internal/decls"
	"github.com/dexpace/morphic/pass"
)

// TestReusedMap_EmptiesInPlaceUpToTheLimit holds emptied to reusing the map it
// holds while the last collection left it at most the limit's entries, and to
// handing a fresh one past that: Go never shrinks a map, so emptying one that
// held a huge collection costs in proportion to it, at every collection after.
func TestReusedMap_EmptiesInPlaceUpToTheLimit(t *testing.T) {
	t.Parallel()
	var r pass.ReusedMap[string, int]
	first := pass.Emptied(&r)
	require.NotNil(t, first, "the zero reusedMap hands a map to write to")
	fill := func(m map[string]int, n int) {
		for i := range n {
			m[strconv.Itoa(i)] = i
		}
	}

	fill(first, pass.ReuseLimit)
	atLimit := pass.Emptied(&r)
	assert.Empty(t, atLimit)
	assert.Equal(t, reflect.ValueOf(first).UnsafePointer(), reflect.ValueOf(atLimit).UnsafePointer(),
		"a map left at the limit is emptied in place")

	fill(atLimit, pass.ReuseLimit+1)
	past := pass.Emptied(&r)
	assert.Empty(t, past)
	assert.NotEqual(t, reflect.ValueOf(atLimit).UnsafePointer(), reflect.ValueOf(past).UnsafePointer(),
		"a map left past the limit is replaced")
	assert.Equal(t, reflect.ValueOf(past).UnsafePointer(), reflect.ValueOf(pass.Emptied(&r)).UnsafePointer(),
		"the fresh map is the one reused from then on")
}

// TestReusedMaps_AreEmptiedThroughReusedMapAlone holds every map the pass
// empties for another collection to reusedMap, whose limit bounds what
// emptying costs: a bare clear of a map kept across collections makes one huge
// collection tax every later one.
func TestReusedMaps_AreEmptiedThroughReusedMapAlone(t *testing.T) {
	t.Parallel()
	p, err := decls.Load(reflect.TypeFor[pass.Validator]().PkgPath())
	require.NoError(t, err)
	assert.Equal(t, []string{"emptied"}, p.FuncsUsing(types.Universe.Lookup("clear")))
}
