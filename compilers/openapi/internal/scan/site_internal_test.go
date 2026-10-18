package scan

import (
	"encoding/json/jsontext"
	"fmt"
	"math/rand/v2"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
	"github.com/dexpace/morphic/compilers/openapi/internal/ynode"
)

// nodesAt returns the nodes of root's tree at pointers, read through the
// mapping view the way a $ref is.
func nodesAt(t *testing.T, root *yaml.Node, pointers []jsontext.Pointer) []*yaml.Node {
	t.Helper()
	view := nodeview.New()
	nodes := make([]*yaml.Node, 0, len(pointers))
	for _, p := range pointers {
		path, complete := view.PointerPath(root, p)
		require.True(t, complete, "%s names nothing", p)
		nodes = append(nodes, path[len(path)-1])
	}
	return nodes
}

// TestLeast_OrdersByThePointersBytes pins the order as that of the pointers
// spelled out, not of their tokens one by one, in the places the two part: a
// key that another extends with a character sorting below the separator, the
// escapes of `~` and `/`, and sequence indexes, which compare as text.
func TestLeast_OrdersByThePointersBytes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		src   string
		among []jsontext.Pointer
		want  jsontext.Pointer
	}{
		{"siblings", "b: {}\na: {}\n", []jsontext.Pointer{"/b", "/a"}, "/a"},
		{"a shallower one sorting after", "b: {}\na: {x: {y: {}}}\n", []jsontext.Pointer{"/b", "/a/x/y"}, "/a/x/y"},
		{"a key extended past a lower byte", "a: {x: {}}\na-b: {}\n", []jsontext.Pointer{"/a/x", "/a-b"}, "/a-b"},
		{"the shorter key itself", "a: {x: {}}\na-b: {}\n", []jsontext.Pointer{"/a", "/a-b"}, "/a"},
		{"a node and what it holds", "a: {x: {}}\n", []jsontext.Pointer{"/a/x", "/a"}, "/a"},
		{"escaped tilde and slash", "'a/': {}\n'a~': {}\na0: {}\n", []jsontext.Pointer{"/a~0", "/a~1", "/a0"}, "/a0"},
		{"escaped slash before tilde", "'a/': {}\n'a~': {}\n", []jsontext.Pointer{"/a~0", "/a~1"}, "/a~0"},
		{"indexes as text", "s: [{}, {}, {}, {}, {}, {}, {}, {}, {}, {}, {}]\n", []jsontext.Pointer{"/s/2", "/s/10"}, "/s/10"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root := indexOf(t, []byte(tc.src)).Root()
			nodes := nodesAt(t, root, tc.among)
			want := nodesAt(t, root, []jsontext.Pointer{tc.want})[0]

			assert.Same(t, want, Least(root, nodes))
			slices.Reverse(nodes)
			assert.Same(t, want, Least(root, nodes), "whatever order the nodes are given in")
		})
	}
}

// randomTree is a small tree of mappings and sequences whose keys are drawn
// from spellings that sort against each other and against the separator.
func randomTree(r *rand.Rand, depth int) string {
	keys := []string{"a", "a-b", "a.b", "ab", "a~", "'a/'", "b", "0", "1", "10", "x y"}
	if depth == 0 || r.IntN(4) == 0 {
		return "{}"
	}
	if r.IntN(3) == 0 {
		items := make([]string, r.IntN(12)+1) // past ten, so "10" sorts before "2"
		for i := range items {
			items[i] = "{}"
			if r.IntN(4) == 0 {
				items[i] = randomTree(r, depth-1)
			}
		}
		return "[" + strings.Join(items, ", ") + "]"
	}
	perm := r.Perm(len(keys))[:r.IntN(4)+1]
	pairs := make([]string, len(perm))
	for i, k := range perm {
		pairs[i] = keys[k] + ": " + randomTree(r, depth-1)
	}
	return "{" + strings.Join(pairs, ", ") + "}"
}

// TestLeast_AgreesWithSortingEveryPointer holds Least to the plain reading of
// its contract: spell every candidate's pointer, as a diagnostic carries it,
// and take the least string. Thousands of generated trees and candidate sets
// are compared.
func TestLeast_AgreesWithSortingEveryPointer(t *testing.T) {
	t.Parallel()
	r := rand.New(rand.NewPCG(905, 912))
	for range 2000 {
		src := "x: " + randomTree(r, 4) + "\n"
		root := indexOf(t, []byte(src)).Root()
		var all []*yaml.Node
		stack := []*yaml.Node{root}
		for len(stack) > 0 {
			n := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			all = append(all, n)
			stack = append(stack, n.Content...)
		}
		r.Shuffle(len(all), func(i, j int) { all[i], all[j] = all[j], all[i] })
		candidates := all[:r.IntN(len(all))+1]

		pointers := Pointers(root)
		want := slices.MinFunc(candidates, func(a, b *yaml.Node) int {
			pa, _ := pointers(a)
			pb, _ := pointers(b)
			return strings.Compare(string(pa), string(pb))
		})
		got := Least(root, candidates)
		gotPointer, _ := pointers(got)
		wantPointer, _ := pointers(want)
		require.Equal(t, wantPointer, gotPointer, "candidates of\n%s", src)
	}
}

// TestLeast_RanksANodeNoPointerReachesLast pins the nodes Pointers gives no
// pointer: those under a `<<` merge key or a non-scalar key. Any node with a
// pointer is less than one without, and among those without the first given
// is taken, the one place left where the caller's order shows.
func TestLeast_RanksANodeNoPointerReachesLast(t *testing.T) {
	t.Parallel()
	root := indexOf(t, []byte("z: {}\nm: {<<: {k: {}}, ? [c] : {}}\n")).Root()
	z := root.Content[1]
	pairs := root.Content[3].Content
	merged, complexValue := pairs[1].Content[1], pairs[3]
	require.Equal(t, yaml.SequenceNode, pairs[2].Kind, "the second pair's key is no scalar")

	assert.Same(t, z, Least(root, []*yaml.Node{merged, z, complexValue}))
	assert.Same(t, merged, Least(root, []*yaml.Node{merged, complexValue}))
	assert.Same(t, complexValue, Least(root, []*yaml.Node{complexValue, merged}))
}

// TestLeast_EdgeCases pins the answers that need no ordering and the node a
// tree is rooted at: none for no nodes; the one node given, whether or not the
// tree holds it; the top of the tree, whose pointer is empty, before anything
// it holds; and the document node above it, which no pointer names.
func TestLeast_EdgeCases(t *testing.T) {
	t.Parallel()
	var doc yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte("a: {b: {}}\n"), &doc))
	top := doc.Content[0]
	inner := top.Content[1].Content[1]
	stranger := ynode.Map()

	assert.Nil(t, Least(&doc, nil))
	assert.Same(t, stranger, Least(&doc, []*yaml.Node{stranger}))
	assert.Same(t, top, Least(&doc, []*yaml.Node{inner, top}))
	assert.Same(t, inner, Least(&doc, []*yaml.Node{&doc, inner}), "the document node ranks with the unplaced")
	assert.Same(t, stranger, Least(&doc, []*yaml.Node{stranger, &doc}), "and the first of those is taken")
}

// TestPathsTo_ClimbsEachNodeOnce pins Least's cost: many candidates at the
// bottom of one deep spine share the climb, so what it records is the spine
// once and each candidate once, not the spine per candidate.
func TestPathsTo_ClimbsEachNodeOnce(t *testing.T) {
	t.Parallel()
	const depth, width = 500, 500
	var b strings.Builder
	b.WriteString(strings.Repeat("{k: ", depth-1) + "{")
	for i := range width {
		fmt.Fprintf(&b, "c%d: {}, ", i)
	}
	b.WriteString("z: {}}")
	b.WriteString(strings.Repeat("}", depth-1))
	root := indexOf(t, []byte("x: "+b.String()+"\n")).Root()
	bottom := root.Content[1]
	for range depth - 1 {
		bottom = bottom.Content[1]
	}
	var candidates []*yaml.Node
	for i := 1; i < len(bottom.Content); i += 2 {
		candidates = append(candidates, bottom.Content[i])
	}
	steps, _ := stepsIn(root, len(candidates)*4+depth*4)
	want := map[*yaml.Node]bool{}
	for _, c := range candidates {
		want[c] = true
	}

	under := pathsTo(steps, candidates, want)

	recorded := 0
	for _, below := range under {
		recorded += len(below)
	}
	assert.Equal(t, depth+width+1, recorded, "the spine below the top, and each candidate")
	assert.Same(t, candidates[0], Least(root, candidates), "c0 is the least")
}
