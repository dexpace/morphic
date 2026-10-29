package irverify

import (
	"go/types"
	"reflect"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestReusedMap_EmptiesInPlaceUpToTheLimit holds emptied to reusing the map it
// holds while the last collection left it at most reuseLimit entries, and to
// handing a fresh one past that: Go never shrinks a map, so emptying one that
// held a huge collection costs in proportion to it, at every collection after.
func TestReusedMap_EmptiesInPlaceUpToTheLimit(t *testing.T) {
	t.Parallel()
	var r reusedMap[string, int]
	first := r.emptied()
	require.NotNil(t, first, "the zero reusedMap hands a map to write to")
	fill := func(m map[string]int, n int) {
		for i := range n {
			m[strconv.Itoa(i)] = i
		}
	}

	fill(first, reuseLimit)
	atLimit := r.emptied()
	assert.Empty(t, atLimit)
	assert.Equal(t, reflect.ValueOf(first).UnsafePointer(), reflect.ValueOf(atLimit).UnsafePointer(),
		"a map left at the limit is emptied in place")

	fill(atLimit, reuseLimit+1)
	past := r.emptied()
	assert.Empty(t, past)
	assert.NotEqual(t, reflect.ValueOf(atLimit).UnsafePointer(), reflect.ValueOf(past).UnsafePointer(),
		"a map left past the limit is replaced")
	assert.Equal(t, reflect.ValueOf(past).UnsafePointer(), reflect.ValueOf(r.emptied()).UnsafePointer(),
		"the fresh map is the one reused from then on")
}

// TestReusedMaps_AreEmptiedThroughReusedMapAlone holds every map the package
// empties for another collection to reusedMap, whose limit bounds what
// emptying costs: a bare clear of a map kept across collections makes one huge
// collection tax every later one.
func TestReusedMaps_AreEmptiedThroughReusedMapAlone(t *testing.T) {
	t.Parallel()
	clearing := verifyDecls(t).FuncsUsing(types.Universe.Lookup("clear"))
	assert.Equal(t, []string{"emptied"}, clearing)
}
