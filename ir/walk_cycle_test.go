package ir_test

import (
	"encoding/json/v2"
	"math/rand/v2"
	"reflect"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// walkLimit is how many values a test lets a walk reach before its visitor prunes
// everything below. A guard that goes missing then fails a count rather than
// hanging the run: the shapes below descend without end, and exponentially, when
// nothing stops them.
const walkLimit = 200

// walkBounded returns the paths root's walk reaches, pruning once there are
// walkLimit of them.
func walkBounded(root any) []string {
	var paths []string
	ir.WalkValues(root, "doc", func(_ reflect.Value, path string) bool {
		paths = append(paths, path)
		return len(paths) < walkLimit
	})
	return paths
}

// kidTree is a struct whose slice of itself can hold the slice that holds it.
type kidTree struct{ Kids []kidTree }

// kidMap is a map that can hold itself as a value.
type kidMap map[string]kidMap

// ptrNode is a struct that can point at itself.
type ptrNode struct{ Next *ptrNode }

// TestWalkValues_SelfContainingSliceIsNotEnteredAgain holds the walk to ending on
// a list that contains itself (GitHub #736). Two elements make the descent
// exponential, which is what hung Verify. The visitor still sees the slice at
// the second meeting, as it sees a pointer, and nothing below it.
func TestWalkValues_SelfContainingSliceIsNotEnteredAgain(t *testing.T) {
	t.Parallel()
	kids := make([]kidTree, 2)
	kids[0].Kids, kids[1].Kids = kids, kids

	assert.Equal(t, []string{
		"doc", "doc.Kids",
		"doc.Kids[0]", "doc.Kids[0].Kids",
		"doc.Kids[1]", "doc.Kids[1].Kids",
	}, walkBounded(kidTree{Kids: kids}))
}

// TestWalkValues_SelfContainingMapIsNotEnteredAgain is the same claim for a map,
// the third kind of reference the encoder follows.
func TestWalkValues_SelfContainingMapIsNotEnteredAgain(t *testing.T) {
	t.Parallel()
	m := kidMap{}
	m["a"], m["b"] = m, m

	assert.Equal(t, []string{
		"doc", "doc[a].key", "doc[a]", "doc[b].key", "doc[b]",
	}, walkBounded(m))
}

// TestWalkValues_ASliceReachedTwiceIsWalkedFromEach pins what stays as it was: a
// slice is not entered once, as a pointer is, since each way in is a place the
// encoder writes it out. Only a way in from inside itself is cut.
func TestWalkValues_ASliceReachedTwiceIsWalkedFromEach(t *testing.T) {
	t.Parallel()
	shared := []kidTree{{}}
	paths := walkBounded(struct{ A, B []kidTree }{A: shared, B: shared})

	assert.Contains(t, paths, "doc.A[0].Kids")
	assert.Contains(t, paths, "doc.B[0].Kids")
}

// TestWalkValues_APrefixOfASliceIsAnotherReference holds the length in a slice's
// identity. A prefix shares its backing array, and holding it is not holding the
// whole: the encoder writes the prefix and what is in it, and ends.
func TestWalkValues_APrefixOfASliceIsAnotherReference(t *testing.T) {
	t.Parallel()
	kids := make([]kidTree, 2)
	kids[1].Kids = kids[:1]

	assert.Equal(t, []string{
		"doc", "doc.Kids",
		"doc.Kids[0]", "doc.Kids[0].Kids",
		"doc.Kids[1]", "doc.Kids[1].Kids",
		"doc.Kids[1].Kids[0]", "doc.Kids[1].Kids[0].Kids",
	}, walkBounded(kidTree{Kids: kids}))
}

// firstField is a struct whose first field is another struct, so a pointer to
// the one and a pointer to the other hold one address.
type firstField struct {
	Head headStruct
	Tail string
}

type headStruct struct{ Name string }

// TestWalkValues_AReferenceIsIdentifiedByTypeAsWellAsAddress holds the walk to
// entering a struct reached after a pointer to its own first field. The two
// share an address and nothing else. A walk that keys a pointer by address alone
// takes the second for the first and skips everything beneath it.
func TestWalkValues_AReferenceIsIdentifiedByTypeAsWellAsAddress(t *testing.T) {
	t.Parallel()
	whole := &firstField{}
	root := struct {
		Head  *headStruct
		Whole *firstField
	}{Head: &whole.Head, Whole: whole}

	assert.Contains(t, walkBounded(root), "doc.Whole.Tail")
}

// TestWalkValues_AnArrayOfReferencesIsWalkedElementByElement holds an array to
// being a value rather than a reference: it has no address of its own to track,
// and what it holds is walked as anything is.
func TestWalkValues_AnArrayOfReferencesIsWalkedElementByElement(t *testing.T) {
	t.Parallel()
	root := struct {
		Pointers [2]*ptrNode
		Trees    [1]kidTree
	}{Pointers: [2]*ptrNode{{}, {}}, Trees: [1]kidTree{{Kids: []kidTree{{}}}}}

	paths := walkBounded(root)
	assert.Contains(t, paths, "doc.Pointers[1].Next")
	assert.Contains(t, paths, "doc.Trees[0].Kids[0]")
}

// TestValueCycles_ReportsWhereAReferenceWasMetAgainAndEntered holds the report to
// the three facts a caller acts on, for each kind of reference: where the walk
// was when it met the reference again, where it had gone inside it, and what it
// is.
func TestValueCycles_ReportsWhereAReferenceWasMetAgainAndEntered(t *testing.T) {
	t.Parallel()
	loop := &ptrNode{}
	loop.Next = loop
	kids := make([]kidTree, 1)
	kids[0].Kids = kids
	m := kidMap{}
	m["a"] = m

	for _, tt := range []struct {
		name string
		root any
		want []ir.ValueCycle
	}{
		{"a pointer", loop, []ir.ValueCycle{{Path: "doc.Next", Entered: "doc", Type: "*ir_test.ptrNode"}}},
		{"a slice", kidTree{Kids: kids}, []ir.ValueCycle{{Path: "doc.Kids[0].Kids", Entered: "doc.Kids", Type: "[]ir_test.kidTree"}}},
		{"a map", m, []ir.ValueCycle{{Path: "doc[a]", Entered: "doc", Type: "ir_test.kidMap"}}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, truncated := ir.ValueCycles(tt.root, "doc")
			assert.False(t, truncated)
			assert.Empty(t, cmp.Diff(tt.want, got))
		})
	}
}

// TestValueCycles_FindsNothingInAnAcyclicGraph is the other side: references
// shared between places, none holding another, are not cycles however often the
// walk meets them, and nor is one reference of a type inside another of the same
// type.
func TestValueCycles_FindsNothingInAnAcyclicGraph(t *testing.T) {
	t.Parallel()
	leaf := &ptrNode{}
	shared := []kidTree{{}}
	sharedMap := kidMap{"x": nil}
	root := struct {
		A, B   *ptrNode
		C, D   []kidTree
		M1, M2 kidMap
		Nested kidMap
	}{
		A: &ptrNode{Next: leaf}, B: &ptrNode{Next: leaf},
		C: shared, D: shared,
		M1: sharedMap, M2: sharedMap,
		Nested: kidMap{"a": kidMap{"x": nil}},
	}

	got, truncated := ir.ValueCycles(root, "doc")
	assert.False(t, truncated)
	assert.Empty(t, got)
}

// TestValueCycles_IsDeterministic holds the order of what it returns: two cycles
// under different map entries, found again and again, come back in one order, so
// a violation list built from them does not depend on Go's map iteration.
func TestValueCycles_IsDeterministic(t *testing.T) {
	t.Parallel()
	mkLoop := func() *ptrNode {
		n := &ptrNode{}
		n.Next = n
		return n
	}
	root := map[string]*ptrNode{"a": mkLoop(), "b": mkLoop(), "c": mkLoop()}

	first, _ := ir.ValueCycles(root, "doc")
	require.Len(t, first, 3)
	for range 20 {
		again, _ := ir.ValueCycles(root, "doc")
		require.Empty(t, cmp.Diff(first, again))
	}
	assert.Equal(t, "doc[a].Next", first[0].Path)
}

// TestValueCycles_ReportsTruncation holds the flag it owes its caller, as every
// walk does: a cycle past the depth cap is not found by a walk that stopped.
func TestValueCycles_ReportsTruncation(t *testing.T) {
	t.Parallel()
	_, truncated := ir.ValueCycles(nestedListValue(ir.MaxWalkDepth), "doc")
	assert.True(t, truncated)
}

// nodeKind is what a node of a random graph is built as: one of the three
// references through which a value holds others.
type nodeKind int

const (
	ctorNode   nodeKind = iota // a *ir.CtorValue, holding its arguments
	listNode                   // a []ir.Value, holding its elements
	objectNode                 // a []ir.Field, holding its members' values
)

// graphNode is one node of the random graphs the encoder comparison builds: a
// constructor (a pointer), a list or an object (each a slice), holding the
// nodes it points at.
type graphNode struct {
	kind nodeKind
	out  []int
}

// buildGraph materializes nodes as ir values, returning one value per node. A
// node's value holds the values of the nodes it points at, so an edge back to
// an ancestor makes the Go value contain itself.
func buildGraph(nodes []graphNode) []ir.Value {
	ctors := make([]*ir.CtorValue, len(nodes))
	lists := make([][]ir.Value, len(nodes))
	objects := make([][]ir.Field, len(nodes))
	for i, n := range nodes {
		switch {
		case n.kind == ctorNode:
			ctors[i] = &ir.CtorValue{Scalar: "t/x/S", Name: "n"}
		case len(n.out) == 0: // an empty list or object stays nil
		case n.kind == listNode:
			lists[i] = make([]ir.Value, len(n.out))
		case n.kind == objectNode:
			objects[i] = make([]ir.Field, len(n.out))
		}
	}
	ref := func(i int) ir.Value {
		switch nodes[i].kind {
		case ctorNode:
			return ir.Value{Kind: ir.ValueCtor, Ctor: ctors[i]}
		case listNode:
			return ir.Value{Kind: ir.ValueList, List: lists[i]}
		default:
			return ir.Value{Kind: ir.ValueObject, Object: objects[i]}
		}
	}
	for i, n := range nodes {
		for k, j := range n.out {
			switch n.kind {
			case ctorNode:
				ctors[i].Args = append(ctors[i].Args, ref(j))
			case listNode:
				lists[i][k] = ref(j)
			case objectNode:
				objects[i][k] = ir.Field{Name: "f", Value: ref(j)}
			}
		}
	}
	values := make([]ir.Value, len(nodes))
	for i := range nodes {
		values[i] = ref(i)
	}
	return values
}

// hasCycle reports whether any node of nodes can reach itself, by the textbook
// three-colour search over the graph as drawn, which shares nothing with the
// walk or the encoder.
func hasCycle(nodes []graphNode) bool {
	const (
		unseen = iota
		onPath
		done
	)
	state := make([]int, len(nodes))
	var visit func(i int) bool
	visit = func(i int) bool {
		state[i] = onPath
		for _, j := range nodes[i].out {
			if state[j] == onPath || (state[j] == unseen && visit(j)) {
				return true
			}
		}
		state[i] = done
		return false
	}
	for i := range nodes {
		if state[i] == unseen && visit(i) {
			return true
		}
	}
	return false
}

// unfoldedCtors counts the constructors met in reading nodes as the tree the
// document's JSON encodes, one for each way in, except that a node met again from
// inside itself is not entered: the count a traversal that ends at cycles must
// reach, worked out on the graph as drawn.
func unfoldedCtors(nodes []graphNode) int {
	onPath := make([]bool, len(nodes))
	var count func(i int) int
	count = func(i int) int {
		if onPath[i] {
			return 0
		}
		onPath[i] = true
		n := 0
		if nodes[i].kind == ctorNode {
			n++
		}
		for _, j := range nodes[i].out {
			n += count(j)
		}
		onPath[i] = false
		return n
	}
	total := 0
	for i := range nodes {
		total += count(i)
	}
	return total
}

// randomGraph draws up to five nodes, each a constructor, a list or an object
// pointing at up to two nodes. It is small so that a graph without a cycle
// unfolds into few enough values for the encoder to write it out.
func randomGraph(r *rand.Rand) []graphNode {
	nodes := make([]graphNode, 1+r.IntN(5))
	for i := range nodes {
		nodes[i].kind = nodeKind(r.IntN(3))
		for range r.IntN(3) {
			nodes[i].out = append(nodes[i].out, r.IntN(len(nodes)))
		}
	}
	return nodes
}

// countEdges counts what TypeEdges yields from td, stopping one past limit, so
// that a missing guard fails a count rather than hanging the run.
func countEdges(td ir.TypeDef, limit int) int {
	n := 0
	for range ir.TypeEdges(td) {
		if n++; n > limit {
			break
		}
	}
	return n
}

// encoderRefuses reports whether the JSON encoder refuses doc as containing
// itself. The sentinel is not exported, so the message is what there is to read;
// the question is the encoder's, not ours, and that is the point of asking it.
func encoderRefuses(doc *ir.Document) bool {
	_, err := json.Marshal(doc)
	return err != nil && strings.Contains(err.Error(), "cycle")
}

// TestValueCycles_AgreesWithTheEncoder holds the claim the verifier rests on:
// a document is called cyclic exactly when the encoder refuses it. Graphs are
// drawn at random and three judges asked about each: a search that never sees a
// Go value, the walk, and the encoder. A walk that called a shared value a
// cycle, or missed a cycle through a constructor, a list, an object or a mix,
// would split from the other two on one of them. TypeEdges is held to the same
// graphs by the constructors it meets. The seed is fixed, so a failure can be drawn again.
func TestValueCycles_AgreesWithTheEncoder(t *testing.T) {
	t.Parallel()
	r := rand.New(rand.NewPCG(573, 736))
	var cyclic, acyclic int
	for range 400 {
		nodes := randomGraph(r)
		values := buildGraph(nodes)
		model := &ir.Model{ID: "t/x/M", Name: ir.Naming{Source: "m", Canonical: "m"}}
		for i := range values {
			model.Examples = append(model.Examples, ir.Example{Value: &values[i]})
		}
		doc := &ir.Document{IRVersion: ir.IRVersion, Types: ir.TypeRegistry{model.ID: model}}

		want := hasCycle(nodes)
		cycles, truncated := ir.ValueCycles(doc, ir.DocumentPath)
		require.False(t, truncated, "%+v", nodes)
		require.Equal(t, want, len(cycles) > 0, "the walk disagrees with the graph drawn: %+v", nodes)
		require.Equal(t, want, encoderRefuses(doc), "the encoder disagrees with the graph drawn: %+v", nodes)
		ctors := unfoldedCtors(nodes)
		require.Equal(t, ctors, countEdges(model, ctors),
			"TypeEdges does not end where the graph drawn does: %+v", nodes)
		if want {
			cyclic++
		} else {
			acyclic++
		}
	}
	assert.Greater(t, cyclic, 40, "the draw must reach cycles often enough to test them")
	assert.Greater(t, acyclic, 40, "and acyclic graphs, shared or not, often enough to test those")
}
