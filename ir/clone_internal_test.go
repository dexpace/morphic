package ir

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// cloneLink is one link of a chain, the shape the ordering test needs and no
// IR type has: a map whose entries reach one pointer at different depths.
type cloneLink struct {
	Next *cloneLink
}

// cloneProbe holds such a map.
type cloneProbe struct {
	Entries map[string]*cloneLink
}

// chainTo returns a chain of n links ending at tail.
func chainTo(n int, tail *cloneLink) *cloneLink {
	link := tail
	for range n {
		link = &cloneLink{Next: link}
	}
	return link
}

// TestCloner_CopiesMapEntriesInKeyOrder pins that whether a copy is cut does
// not depend on Go's map order. Entry "a" reaches a shared chain at once and
// entry "b" only far down, where the shared chain's tail passes the cap; the
// copy takes "a" first every time, so it copies the shared chain shallowly
// and is never cut. In Go's map order half the copies would take "b" first.
func TestCloner_CopiesMapEntriesInKeyOrder(t *testing.T) {
	t.Parallel()
	// A link is two steps of the walk, its pointer and its struct, so b's own
	// links stay under the cap and the shared chain below them does not.
	deep := (MaxWalkDepth - 64) / 2
	shared := chainTo(64, nil)
	probe := cloneProbe{Entries: map[string]*cloneLink{
		"a": {Next: shared},
		"b": chainTo(deep, shared),
	}}
	alone := reflect.ValueOf(cloneProbe{Entries: map[string]*cloneLink{"b": probe.Entries["b"]}})
	control := cloner{copies: map[refKey]reflect.Value{}}
	control.fill(reflect.New(alone.Type()).Elem(), alone, 0)
	require.True(t, control.truncated, "copied first, the deep entry must reach past the cap, or no order is tested")
	v := reflect.ValueOf(probe)

	for range 64 {
		c := cloner{copies: map[refKey]reflect.Value{}}
		out := reflect.New(v.Type()).Elem()
		c.fill(out, v, 0)

		require.False(t, c.truncated, "the copy took the deep entry first")
		cp, _ := reflect.TypeAssert[cloneProbe](out)
		assert.Same(t, cp.Entries["a"].Next, chainEnd(cp.Entries["b"], deep),
			"the shared chain is copied once")
	}
}

// chainEnd returns the link n steps along from link.
func chainEnd(link *cloneLink, n int) *cloneLink {
	for range n {
		link = link.Next
	}
	return link
}
