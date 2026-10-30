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
	control := cloner{copies: map[refKey]reflect.Value{}, ranging: &entryStack{}}
	control.fill(reflect.New(alone.Type()).Elem(), alone, 0)
	require.True(t, control.truncated, "copied first, the deep entry must reach past the cap, or no order is tested")
	v := reflect.ValueOf(probe)

	for range 64 {
		c := cloner{copies: map[refKey]reflect.Value{}, ranging: &entryStack{}}
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

// TestCloneNode_CopyingANodeWithNoMapAllocatesOnlyTheCopy pins why the
// cloner holds its entry stack by pointer: escape analysis does not tell a
// struct's fields apart, so a stack held inside the cloner would take its copies
// map, and the map's first group, onto the heap for every copy. A node holding
// no reference costs three: the cell out is, the copy, and the interface it is
// returned through. It is not run in parallel: AllocsPerRun measures the
// process.
func TestCloneNode_CopyingANodeWithNoMapAllocatesOnlyTheCopy(t *testing.T) {
	var ranging entryStack
	td := TypeDef(&Primitive{ID: "t/p"})
	assert.Equal(t, 3.0, testing.AllocsPerRun(10, func() { _, _ = cloneNode(td, &ranging) }))
}

// TestCloneNode_LeavesTheEntryStackAsItFoundIt holds a copy to releasing
// what it read maps into, a property's map as well as the node's own: an edit
// keeps one stack for every copy it makes, so entries a copy left behind would
// pile up for the whole edit, changing no result.
func TestCloneNode_LeavesTheEntryStackAsItFoundIt(t *testing.T) {
	t.Parallel()
	var ranging entryStack
	td := TypeDef(&Model{ID: "t/m", WireNameByFormat: map[string]string{"json": "m", "xml": "M"},
		Properties: []Property{{ID: "p/a", WireNameByFormat: map[string]string{"json": "a"}}}})
	for range 3 {
		_, copied := cloneNode(td, &ranging)
		require.True(t, copied)
		assert.Empty(t, ranging.entries, "entries left on the stack")
		for typ, st := range ranging.stores {
			assert.Zero(t, st.used, "%s slots left in use", typ)
		}
	}
	assert.NotEmpty(t, ranging.stores, "the copy read its maps through the stack")
}
