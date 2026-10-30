// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package ir_test

import (
	"context"
	"fmt"
	"math"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/internal/ctxtest"
	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irtest"
)

// walkPaths returns every path ir.WalkValues reaches from root, in walk order,
// plus whether the depth cap cut the walk short.
func walkPaths(root any) ([]string, bool) {
	var paths []string
	truncated := ir.WalkValues(root, "doc", ir.VisitFunc(func(_ reflect.Value, at ir.WalkPath) bool {
		paths = append(paths, at.String())
		return true
	}))
	return paths, truncated
}

// TestWalkValues_PathsSpellFieldsIndicesAndKeys pins the three path rules two
// checkers report against: a field is joined by ".", a slice index and a map key
// sit in brackets (a key carrying ir.MapKeySuffix so a caller can tell it from a
// value), and an embedded field contributes no segment of its own — Examples is
// declared on TypeCommon, and is reached below the model rather than below a
// ".TypeCommon" step that neither JSON nor Go has.
func TestWalkValues_PathsSpellFieldsIndicesAndKeys(t *testing.T) {
	t.Parallel()
	doc := &ir.Document{
		Name: "api",
		Types: ir.TypeRegistry{"t/x/M": &ir.Model{
			ID:       "t/x/M",
			Examples: []ir.Example{{Value: new(ir.Value{Kind: ir.ValueNull})}},
		}},
	}

	paths, truncated := walkPaths(doc)
	require.False(t, truncated)
	assert.Contains(t, paths, "doc.Name")
	assert.Contains(t, paths, "doc.Types[t/x/M]"+ir.MapKeySuffix)
	assert.Contains(t, paths, "doc.Types[t/x/M].Examples[0].Value.Kind")
	assert.NotContains(t, paths, "doc.Types[t/x/M].TypeCommon.ID",
		"an embedded field contributes no path segment")
}

// TestWalkValues_MapEntriesAreVisitedInRenderedKeyOrder holds the ordering rule
// invariant 7 rests on. Go randomizes map iteration, and a pointer reachable
// from two entries is descended into at whichever entry the walk reaches first —
// so an unordered walk records a different path for it run to run, which is a
// different result set rather than a different order, and no later sort repairs
// that.
func TestWalkValues_MapEntriesAreVisitedInRenderedKeyOrder(t *testing.T) {
	t.Parallel()
	doc := &ir.Document{Types: ir.TypeRegistry{}}
	for _, id := range []ir.TypeID{"t/c", "t/a", "t/d", "t/b"} {
		doc.Types[id] = &ir.Any{ID: id}
	}

	paths, _ := walkPaths(doc)
	var entries []string
	for _, p := range paths {
		if strings.HasPrefix(p, "doc.Types[") && strings.HasSuffix(p, ir.MapKeySuffix) {
			entries = append(entries, p)
		}
	}
	require.Len(t, entries, 4)
	assert.True(t, slices.IsSorted(entries), "entries are walked in rendered-key order, got %v", entries)
}

// TestWalkValues_NestedMapsAreWalkedEntryByEntry holds a map's entries to the
// map that holds them: a walk ranging over an outer map meets an inner one in
// one of its entries, walks the inner entries there, and goes back to the outer
// map's next entry, not on to the inner map's.
func TestWalkValues_NestedMapsAreWalkedEntryByEntry(t *testing.T) {
	t.Parallel()
	paths, truncated := walkPaths(map[string]map[string]int{"a": {"x": 1}, "b": {"y": 2}})
	require.False(t, truncated)
	assert.Equal(t, []string{
		"doc",
		"doc[a].key", "doc[a]", "doc[a][x].key", "doc[a][x]",
		"doc[b].key", "doc[b]", "doc[b][y].key", "doc[b][y]",
	}, paths)
}

// TestWalkValues_DeepValueTreeIsTruncated drives the depth cap from both sides:
// a document nested past it must report truncation rather than be under-checked
// in silence, and one nested deep but within the bound must not.
func TestWalkValues_DeepValueTreeIsTruncated(t *testing.T) {
	t.Parallel()
	docWith := func(v ir.Value) *ir.Document {
		return &ir.Document{Types: ir.TypeRegistry{"t/m": &ir.Model{
			ID:       "t/m",
			Examples: []ir.Example{{Value: &v}},
		}}}
	}

	_, truncated := walkPaths(docWith(irtest.NestedListValue(ir.MaxWalkDepth)))
	assert.True(t, truncated, "a tree nested past the cap must report truncation")

	_, truncated = walkPaths(docWith(irtest.NestedListValue(200)))
	assert.False(t, truncated, "a tree a compiler can produce must be walked whole")
}

// TestWalkValuesContext_WalksAsWalkValuesUnderALiveContext pins that the
// context changes nothing while it is live: the same values in the same order,
// the same truncation, and no error. A value is told apart by its path where
// the tree is shallow, and by its kind where the cap truncates it, since a
// path there is as long as the tree is deep.
func TestWalkValuesContext_WalksAsWalkValuesUnderALiveContext(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		depth int
		paths bool
	}{{200, true}, {ir.MaxWalkDepth, false}} {
		v := irtest.NestedListValue(tc.depth)
		doc := &ir.Document{Types: ir.TypeRegistry{"t/m": &ir.Model{ID: "t/m", Examples: []ir.Example{{Value: &v}}}}}
		var want, got []string
		record := func(into *[]string) ir.Visitor {
			return ir.VisitFunc(func(v reflect.Value, at ir.WalkPath) bool {
				if tc.paths {
					*into = append(*into, at.String())
				} else {
					*into = append(*into, v.Kind().String())
				}
				return true
			})
		}
		wantTruncated := ir.WalkValues(doc, "doc", record(&want))

		truncated, err := ir.WalkValuesContext(t.Context(), doc, "doc", record(&got))

		require.NoError(t, err)
		assert.Equal(t, wantTruncated, truncated, "depth %d", tc.depth)
		assert.Equal(t, tc.depth == ir.MaxWalkDepth, truncated, "depth %d: the cap truncates only the deep tree", tc.depth)
		assert.Equal(t, want, got, "depth %d", tc.depth)
	}
}

// shownCounter counts the values a walk shows it.
type shownCounter struct{ n int }

func (c *shownCounter) Visit(reflect.Value, ir.WalkPath) bool {
	c.n++
	return true
}

// TestWalkValuesContext_StopsOnceItsContextIsDone cancels a walk at every check
// of its context a whole one makes, and requires each to end with the
// context's error, the values before the refusal shown and none after, and no
// ask after it. The walk asks before its first value and every 256 values
// after, so a cancel waits out 256 values at most.
func TestWalkValuesContext_StopsOnceItsContextIsDone(t *testing.T) {
	t.Parallel()
	root := make([]int, 1000)
	values := 1 + len(root)
	whole := &ctxtest.Countdown{Context: t.Context(), Left: math.MaxInt}
	_, err := ir.WalkValuesContext(whole, root, "doc", &shownCounter{})
	require.NoError(t, err)
	require.Equal(t, (values+255)/256, whole.Calls, "an ask before the first value and every 256 after")

	for left := range whole.Calls {
		var shown shownCounter
		var atRefusal int
		ctx := &ctxtest.Countdown{Context: t.Context(), Left: left, OnRefuse: func() { atRefusal = shown.n }}

		truncated, err := ir.WalkValuesContext(ctx, root, "doc", &shown)

		require.ErrorIs(t, err, context.Canceled, "check %d", left+1)
		assert.False(t, truncated, "check %d", left+1)
		assert.Equal(t, 256*left, shown.n, "check %d: the values before the refusal are shown", left+1)
		assert.Equal(t, atRefusal, shown.n, "check %d: no value is shown after the refusal", left+1)
		assert.Equal(t, left+1, ctx.Calls, "check %d: asked on after the refusal", left+1)
	}
}

// TestWalkValuesContext_AStopEndsEveryTraversal pins a walk carrying more
// visitors than one traversal shares: a refusal in the first traversal starts
// no other, and one in the second leaves the first whole. The 65th visitor is
// the second traversal's.
func TestWalkValuesContext_AStopEndsEveryTraversal(t *testing.T) {
	t.Parallel()
	root := make([]int, 1000)
	values := 1 + len(root)
	perTraversal := (values + 255) / 256
	tests := []struct {
		name                string
		left                int
		wantFirst, wantLast int
	}{
		{"refused in the first traversal", 0, 0, 0},
		{"refused in the second", perTraversal, values, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			counters := make([]shownCounter, 65)
			visitors := make([]ir.Visitor, len(counters))
			for i := range counters {
				visitors[i] = &counters[i]
			}

			ctx := &ctxtest.Countdown{Context: t.Context(), Left: tc.left}

			_, err := ir.WalkValuesContext(ctx, root, "doc", visitors...)

			require.ErrorIs(t, err, context.Canceled)
			assert.Equal(t, tc.wantFirst, counters[0].n)
			assert.Equal(t, tc.wantLast, counters[64].n)
			assert.Equal(t, tc.left+1, ctx.Calls, "no traversal asks after the refusal")
		})
	}
}

// TestWalkValues_SharedPointerIsDescendedIntoOnce holds the cycle guard. Nothing
// in a compiled document reaches it — a Document is a flat registry of values
// referenced by ID, so no two fields point at one struct — which is exactly why
// it needs planting: the guard is what keeps the walk terminating on a document
// that does share a pointer, and without one, removing it changes nothing.
func TestWalkValues_SharedPointerIsDescendedIntoOnce(t *testing.T) {
	t.Parallel()
	shared := &ir.TypeRef{Target: "t/x/Shared"}
	doc := &ir.Document{Types: ir.TypeRegistry{"t/m": &ir.Model{
		ID: "t/m", Instantiation: &ir.TemplateInstantiation{
			Args: []ir.TemplateArg{{Type: shared}, {Type: shared}},
		},
	}}}

	paths, truncated := walkPaths(doc)
	require.False(t, truncated, "two template arguments are not deep")
	var reached int
	for _, p := range paths {
		if strings.HasSuffix(p, ".Target") {
			reached++
		}
	}
	assert.Equal(t, 1, reached, "the second argument finds the pointer already seen and stops there")
}

// TestWalkValues_CyclicPointerGraphTerminates states the same guard as a
// liveness claim: a value graph that points back at itself is normal input for a
// schema language, and the walk has to end on one rather than spin until the
// depth cap.
func TestWalkValues_CyclicPointerGraphTerminates(t *testing.T) {
	t.Parallel()
	type node struct{ Next *node }
	loop := &node{}
	loop.Next = loop

	paths, truncated := walkPaths(loop)
	assert.False(t, truncated, "the cycle guard stops the walk well before the depth cap")
	assert.Equal(t, []string{"doc", "doc", "doc.Next"}, paths,
		"the pointer, the struct behind it, then the field that leads back to it")
}

// payloadBytes sizes the preserved payload the byte-skip test walks. It is large
// enough that descending it would dominate any plausible visit count for a
// two-node document, and small enough to build inline.
const payloadBytes = 4096

// TestWalkValues_ByteSequencesAreNotDescendedInto drives the byte-sequence skip,
// which exists for cost rather than for correctness: a uint8 element is none of
// the things a visitor looks for, so collecting nothing from it is the same
// result either way and only the price differs.
//
// The visit count is what makes that assertable: since the result is the same
// with or without the skip, a test reading the result cannot notice the skip
// going missing. Without it the count grows past the payload's own length.
func TestWalkValues_ByteSequencesAreNotDescendedInto(t *testing.T) {
	t.Parallel()
	doc := &ir.Document{Unmodeled: ir.Unmodeled{"openapi:x-thing": {
		Reason: ir.ReasonVendorExtension,
		Value:  ir.RawValue(`"` + strings.Repeat("t", payloadBytes) + `"`),
	}}}

	paths, truncated := walkPaths(doc)
	require.False(t, truncated)
	assert.Less(t, len(paths), payloadBytes,
		"walking %d bytes of payload one value at a time is what the skip exists to avoid", payloadBytes)
}

// TestWalkValues_VisitorPrunesChildren holds the one lever a visitor has over
// the walk: returning false stops the descent at that value, which is how a
// check that has learned all it needs from a node avoids paying for the subtree
// below it.
func TestWalkValues_VisitorPrunesChildren(t *testing.T) {
	t.Parallel()
	doc := &ir.Document{Name: "api", Types: ir.TypeRegistry{
		"t/m": &ir.Model{ID: "t/m"},
	}}

	var pruned []string
	ir.WalkValues(doc, "doc", ir.VisitFunc(func(v reflect.Value, at ir.WalkPath) bool {
		pruned = append(pruned, at.String())
		return v.Type() != reflect.TypeFor[ir.TypeRegistry]()
	}))

	assert.Contains(t, pruned, "doc.Types")
	for _, p := range pruned {
		assert.False(t, strings.HasPrefix(p, "doc.Types["), "%s sits below the pruned registry", p)
	}
}

// TestWalkValues_NothingUnreachableIsVisited covers the three ways the walk
// reaches nothing: an invalid root, a nil pointer, and a nil interface. None may
// be handed to a visitor, which would then have to guard every Type() call
// against the zero reflect.Value.
func TestWalkValues_NothingUnreachableIsVisited(t *testing.T) {
	t.Parallel()
	paths, truncated := walkPaths(nil)
	assert.Empty(t, paths, "an invalid root is not visited")
	assert.False(t, truncated)

	// Contact is a nil *Contact and the registry entry is a nil TypeDef, so the
	// walk reaches the pointer and the interface but nothing behind either.
	doc := &ir.Document{Contact: nil, Types: ir.TypeRegistry{"t/nil": nil}}
	paths, _ = walkPaths(doc)
	assert.Contains(t, paths, "doc.Contact", "the nil pointer itself is visited")
	assert.Contains(t, paths, "doc.Types[t/nil]", "the nil interface itself is visited")
	assert.NotContains(t, paths, "doc.Contact.Name", "nothing behind a nil pointer is reachable")
}

// TestWalkValues_ArrayElementsAreReached pins that a fixed-size array is walked
// like a slice. The IR declares none today, so this states the rule rather than
// guarding a live shape: a kind the walk silently skipped would take references
// with it.
func TestWalkValues_ArrayElementsAreReached(t *testing.T) {
	t.Parallel()
	holder := &struct{ IDs [2]ir.TypeID }{IDs: [2]ir.TypeID{"t/a", "t/b"}}

	paths, _ := walkPaths(holder)
	assert.Contains(t, paths, "doc.IDs[0]")
	assert.Contains(t, paths, "doc.IDs[1]")
}

// A chain of structs, each embedding the next and an empty map, for a walk whose
// visitor reads no path.
type (
	walkEmptyMap map[string]int
	walkLevel4   struct{ walkEmptyMap }
	walkLevel3   struct {
		walkLevel4
		walkEmptyMap
	}
	walkLevel2 struct {
		walkLevel3
		walkEmptyMap
	}
	walkLevel1 struct {
		walkLevel2
		walkEmptyMap
	}
)

// TestWalkValues_StructsAndMapsCostNoAllocation holds the walk's cost per struct
// and per map at nothing, since pass.Validate walks a whole document on every
// compile. Ranging over reflect.Value.Fields or Seq2 instead of indexing puts
// the loop body on the heap each time, which on Petstore adds more than half
// again to Validate's allocations. The visitor reads no path, so any growth
// with depth is the walk's own. It is not run in parallel: AllocsPerRun
// measures the process.
func TestWalkValues_StructsAndMapsCostNoAllocation(t *testing.T) {
	var shallow, deep any = walkLevel4{walkEmptyMap: walkEmptyMap{}}, walkLevel1{
		walkLevel2: walkLevel2{
			walkLevel3: walkLevel3{
				walkLevel4:   walkLevel4{walkEmptyMap: walkEmptyMap{}},
				walkEmptyMap: walkEmptyMap{},
			},
			walkEmptyMap: walkEmptyMap{},
		},
		walkEmptyMap: walkEmptyMap{},
	}
	paths, _ := walkPaths(deep)
	require.Len(t, paths, 8, "four structs and the four maps they embed are all reached")

	visit := ir.VisitFunc(func(reflect.Value, ir.WalkPath) bool { return true })
	assert.Equal(t,
		testing.AllocsPerRun(10, func() { _ = ir.WalkValues(shallow, "doc", visit) }),
		testing.AllocsPerRun(10, func() { _ = ir.WalkValues(deep, "doc", visit) }),
		"three more structs and maps must cost no more allocations")
}

// Small maps of entries no pointer leads to, of a struct and behind an
// interface as Document.Types holds its nodes, for a walk whose cost per entry
// is its own.
type (
	walkEntry  struct{ Name string }
	walkHolder struct {
		Structs map[string]walkEntry
		Boxed   map[string]any
	}
)

// walkHolders returns n holders, each map of three entries.
func walkHolders(n int) []walkHolder {
	holders := make([]walkHolder, n)
	for i := range holders {
		holders[i] = walkHolder{Structs: map[string]walkEntry{}, Boxed: map[string]any{}}
		for _, k := range []string{"a", "b", "c"} {
			holders[i].Structs[k] = walkEntry{Name: k}
			holders[i].Boxed[k] = walkEntry{Name: k}
		}
	}
	return holders
}

// TestWalkValues_MapEntriesCostNoAllocation holds the walk's cost per map entry
// at nothing once its map type's storage fits, since every type node of a
// document sits in Document.Types and pass.Validate walks one on every compile.
// reflect.MapIter.Key and Value copy each entry into an allocation of its own,
// two for each of the hundreds of entries more the larger walk reaches. It is
// not run in parallel: AllocsPerRun measures the process.
func TestWalkValues_MapEntriesCostNoAllocation(t *testing.T) {
	few, many := walkHolders(4), walkHolders(64)
	visit := ir.VisitFunc(func(reflect.Value, ir.WalkPath) bool { return true })
	assert.Equal(t,
		testing.AllocsPerRun(10, func() { _ = ir.WalkValues(few, "doc", visit) }),
		testing.AllocsPerRun(10, func() { _ = ir.WalkValues(many, "doc", visit) }),
		"sixty more holders of six entries must cost no more allocations")
}

// walkNode nests maps of one type, which share the walk's storage for it.
type walkNode struct {
	N    int
	Kids map[string]walkNode
}

// walkKids returns n nodes keyed "k00" on, numbered from first.
func walkKids(first, n int) map[string]walkNode {
	kids := map[string]walkNode{}
	for i := range n {
		kids[fmt.Sprintf("k%02d", i)] = walkNode{N: first + i}
	}
	return kids
}

// nodeNumbers records under each node's path the number it holds, read by
// plain Go rather than by the walk.
func nodeNumbers(path string, n walkNode, into map[string]int64) {
	into[path+".N"] = int64(n.N)
	for key, kid := range n.Kids {
		nodeNumbers(path+".Kids["+key+"]", kid, into)
	}
}

// TestWalkValues_NestedMapsOfOneTypeKeepTheirEntries holds a map's entries to
// the map: one ranged over inside another of its type takes storage of its own
// beside the outer one's, whether the storage fits both or has to grow while
// the outer map is in it. An inner map writing over the outer one's entries
// would show the walk the inner entries in the outer's place.
func TestWalkValues_NestedMapsOfOneTypeKeepTheirEntries(t *testing.T) {
	t.Parallel()
	root := struct{ Wide, Nested, Grown walkNode }{
		// Wide grows the storage to ten entries first, so Nested's maps all fit.
		Wide: walkNode{Kids: walkKids(100, 10)},
		Nested: walkNode{Kids: map[string]walkNode{
			"a": {N: 1, Kids: walkKids(10, 2)},
			"b": {N: 2, Kids: walkKids(20, 1)},
			"c": {N: 3},
		}},
		Grown: walkNode{Kids: map[string]walkNode{
			"a": {N: 4, Kids: walkKids(30, 40)},
			"b": {N: 5},
		}},
	}
	want := map[string]int64{}
	nodeNumbers("doc.Wide", root.Wide, want)
	nodeNumbers("doc.Nested", root.Nested, want)
	nodeNumbers("doc.Grown", root.Grown, want)

	got := map[string]int64{}
	truncated := ir.WalkValues(root, "doc", ir.VisitFunc(func(v reflect.Value, at ir.WalkPath) bool {
		if path := at.String(); strings.HasSuffix(path, ".N") {
			got[path] = v.Int()
		}
		return true
	}))
	require.False(t, truncated)
	assert.Empty(t, cmp.Diff(want, got))
}

// TestWalkValues_MapEntriesAreTheWalksCopies pins where Visitor.Visit draws a
// map entry's copy: the key and value, and what the value holds by value, are
// the walk's own, which a visitor can set without changing the map, while what
// a slice in it leads to is the document's own.
func TestWalkValues_MapEntriesAreTheWalksCopies(t *testing.T) {
	t.Parallel()
	type entry struct {
		Name  string
		Items []int
	}
	m := map[string]entry{"a": {Name: "x", Items: []int{1}}}
	copies := 0
	ir.WalkValues(m, "doc", ir.VisitFunc(func(v reflect.Value, _ ir.WalkPath) bool {
		switch {
		case v.Kind() == reflect.String && v.CanSet():
			v.SetString("changed")
			copies++
		case v.Kind() == reflect.Int && v.CanSet():
			v.SetInt(2)
		}
		return true
	}))
	require.Equal(t, 2, copies, "the key and the value's field are each the walk's own copy")
	assert.Equal(t, "x", m["a"].Name, "setting the walk's copy changes nothing in the map")
	assert.Equal(t, []int{2}, m["a"].Items, "a slice in the entry leads to the document's own elements")
}

// TestWalkValues_InterfaceInAMapEntryLeadsToTheDocumentsOwn pins the line
// Visitor.Visit draws for an interface, the way Document.Types holds its nodes:
// the walk reuses its copy of the entry, while what the interface holds is the
// document's own, so a visitor can keep the value the walk shows it past the
// map. Holding nothing settable, it is told apart only by outliving the reuse.
func TestWalkValues_InterfaceInAMapEntryLeadsToTheDocumentsOwn(t *testing.T) {
	t.Parallel()
	root := []map[string]any{{"a": walkEntry{Name: "first"}}, {"a": walkEntry{Name: "second"}}}
	var copies, held []reflect.Value
	ir.WalkValues(root, "doc", ir.VisitFunc(func(v reflect.Value, _ ir.WalkPath) bool {
		switch v.Kind() {
		case reflect.Interface:
			copies = append(copies, v)
		case reflect.Struct:
			held = append(held, v)
		default:
			// The slice, the maps and the strings carry nothing this case reads.
		}
		return true
	}))
	require.Len(t, copies, 2)
	require.Equal(t, "second", copies[0].Elem().Field(0).String(),
		"the second map must reuse the walk's copy of the first one's entry, or the case tests nothing")
	require.Len(t, held, 2)
	assert.Equal(t, "first", held[0].Field(0).String(), "what the first entry's interface holds is the document's own")
}

// TestWalkValues_MapBehindAnUnexportedFieldIsReached holds the walk to a map it
// reaches through an unexported field, whose entries it cannot copy into the
// storage it keeps, since reflect refuses to read such a map that way. It
// reaches each entry all the same, and reads it right.
func TestWalkValues_MapBehindAnUnexportedFieldIsReached(t *testing.T) {
	t.Parallel()
	holder := struct{ hidden map[string]int }{hidden: map[string]int{"b": 2, "a": 1}}
	var got []string
	ir.WalkValues(holder, "doc", ir.VisitFunc(func(v reflect.Value, at ir.WalkPath) bool {
		switch v.Kind() {
		case reflect.String:
			got = append(got, at.String()+"="+v.String())
		case reflect.Int:
			got = append(got, at.String()+"="+strconv.FormatInt(v.Int(), 10))
		default:
			got = append(got, at.String())
		}
		return true
	}))
	assert.Equal(t, []string{
		"doc", "doc.hidden",
		"doc.hidden[a].key=a", "doc.hidden[a]=1",
		"doc.hidden[b].key=b", "doc.hidden[b]=2",
	}, got)
}

// growingMaps returns maps of one type sized 1 to n, smallest first, so each
// needs more storage than any before it. Keys are strings, which label
// themselves; an integer key from 100 up allocates its label.
func growingMaps(n int) []map[string]int {
	keys := make([]string, n)
	for k := range keys {
		keys[k] = strconv.Itoa(k)
	}
	sized := make([]map[string]int, n)
	for i := range sized {
		sized[i] = map[string]int{}
		for k := range i + 1 {
			sized[i][keys[k]] = k
		}
	}
	return sized
}

// TestWalkValues_MapStorageGrowsByDoubling holds the storage a walk copies map
// entries into to growing by doubling: a walk meeting ever larger maps of one
// type pays for each doubling of the largest, so twice as many maps, each
// larger, cost one doubling more, as the last twice as many did. Growing to
// fit each map exactly would cost a growth per map. It is not run in parallel:
// AllocsPerRun measures the process.
func TestWalkValues_MapStorageGrowsByDoubling(t *testing.T) {
	visit := ir.VisitFunc(func(reflect.Value, ir.WalkPath) bool { return true })
	allocs := func(n int) float64 {
		sized := growingMaps(n)
		return testing.AllocsPerRun(5, func() { _ = ir.WalkValues(sized, "doc", visit) })
	}
	small, medium, large := allocs(32), allocs(64), allocs(128)
	assert.Equal(t, medium-small, large-medium, "walking 32, 64 and 128 ever larger maps cost %v, %v and %v", small, medium, large)
}
