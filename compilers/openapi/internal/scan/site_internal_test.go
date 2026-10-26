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
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
	"github.com/dexpace/morphic/compilers/openapi/internal/ynode"
	"github.com/dexpace/morphic/ir"
)

// nodesAt returns the nodes of root's tree at pointers, read through the
// mapping view.
func nodesAt(t *testing.T, root *yaml.Node, pointers []jsontext.Pointer) []*yaml.Node {
	t.Helper()
	view := nodeview.New()
	nodes := make([]*yaml.Node, 0, len(pointers))
	for _, p := range pointers {
		path, complete := view.DocumentPath(root, p)
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

			assert.Same(t, want, Least(t.Context(), root, nodes))
			slices.Reverse(nodes)
			assert.Same(t, want, Least(t.Context(), root, nodes), "whatever order the nodes are given in")
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
	if r.IntN(8) == 0 {
		perm = append(perm, perm[0]) // a repeated key, whose values share a token
	}
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

		pointers := Pointers(t.Context(), root)
		want := slices.MinFunc(candidates, func(a, b *yaml.Node) int {
			pa, _ := pointers(a)
			pb, _ := pointers(b)
			return strings.Compare(string(pa), string(pb))
		})
		got := Least(t.Context(), root, candidates)
		gotPointer, _ := pointers(got)
		wantPointer, _ := pointers(want)
		require.Equal(t, wantPointer, gotPointer, "candidates of\n%s", src)
	}
}

// TestLeast_RanksANodeNoPointerReachesLast pins the nodes Pointers gives no
// pointer: those under a `<<` merge key, and a key that is no scalar, whose
// value is held at the empty key. Any node with a pointer is less than one
// without, and among those without the first given is taken, the one place
// left where the caller's order shows.
func TestLeast_RanksANodeNoPointerReachesLast(t *testing.T) {
	t.Parallel()
	root := indexOf(t, []byte("z: {}\nm: {<<: {k: {}}, ? [c] : {}}\n")).Root()
	z := root.Content[1]
	pairs := root.Content[3].Content
	merged, complexKey, complexValue := pairs[1].Content[1], pairs[2], pairs[3]
	require.Equal(t, yaml.SequenceNode, complexKey.Kind, "the second pair's key is no scalar")

	assert.Same(t, z, Least(t.Context(), root, []*yaml.Node{merged, z, complexKey}))
	assert.Same(t, merged, Least(t.Context(), root, []*yaml.Node{merged, complexKey}))
	assert.Same(t, complexKey, Least(t.Context(), root, []*yaml.Node{complexKey, merged}))
	assert.Same(t, complexValue, Least(t.Context(), root, []*yaml.Node{z, complexValue}),
		"the value is held at /m/, which sorts before /z")
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

	assert.Nil(t, Least(t.Context(), &doc, nil))
	assert.Same(t, stranger, Least(t.Context(), &doc, []*yaml.Node{stranger}))
	assert.Same(t, top, Least(t.Context(), &doc, []*yaml.Node{inner, top}))
	assert.Same(t, inner, Least(t.Context(), &doc, []*yaml.Node{&doc, inner}), "the document node ranks with the unplaced")
	assert.Same(t, stranger, Least(t.Context(), &doc, []*yaml.Node{stranger, &doc}), "and the first of those is taken")
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
	steps, _ := stepsIn(t.Context(), root, len(candidates)*4+depth*4)
	want := map[*yaml.Node]int{}
	for i, c := range candidates {
		want[c] = i
	}

	under := pathsTo(steps, candidates, want)

	recorded := 0
	for _, below := range under {
		recorded += len(below)
	}
	assert.Equal(t, depth+width+1, recorded, "the spine below the top, and each candidate")
	assert.Same(t, candidates[0], Least(t.Context(), root, candidates), "c0 is the least")
}

// TestLeast_FollowsEveryValueOfARepeatedKey pins the one place two siblings
// share a token: a mapping that repeats a key. The least pointer can lie
// under either value, so both are followed, and the answer does not move when
// the two are declared the other way round. Values sharing one pointer exactly
// go to the first given.
func TestLeast_FollowsEveryValueOfARepeatedKey(t *testing.T) {
	t.Parallel()
	for _, src := range []string{"x: {a: {z: {}}, a: {b: {}}}\n", "x: {a: {b: {}}, a: {z: {}}}\n"} {
		root := indexOf(t, []byte(src)).Root()
		pairs := root.Content[1].Content
		first, second := pairs[1].Content[1], pairs[3].Content[1]
		pointers := Pointers(t.Context(), root)

		got, _ := pointers(Least(t.Context(), root, []*yaml.Node{first, second}))
		assert.Equal(t, jsontext.Pointer("/x/a/b"), got, "in %q", src)
	}

	root := indexOf(t, []byte("x: {a: {}, a: {}}\n")).Root()
	first, second := root.Content[1].Content[1], root.Content[1].Content[3]
	assert.Same(t, second, Least(t.Context(), root, []*yaml.Node{second, first}), "one pointer: the first given")
	assert.Same(t, first, Least(t.Context(), root, []*yaml.Node{first, second}))

	// z under the first a is given first, so that a is reached first, and the
	// two k sharing /x/a/k meet in that order rather than the order given.
	root = indexOf(t, []byte("x: {a: {k: {}, z: {}}, a: {k: {}}}\n")).Root()
	a1, a2 := root.Content[1].Content[1], root.Content[1].Content[3]
	k1, z1, k2 := a1.Content[1], a1.Content[3], a2.Content[1]
	assert.Same(t, k2, Least(t.Context(), root, []*yaml.Node{z1, k2, k1}), "one pointer: the first given of those sharing it")
}

// bothOrders is two documents declaring members, one as given and one
// reversed, each member one or more whole lines.
func bothOrders(members []string) map[string][]byte {
	reversed := slices.Clone(members)
	slices.Reverse(reversed)
	return map[string][]byte{
		"as written": []byte(strings.Join(members, "")),
		"reversed":   []byte(strings.Join(reversed, "")),
	}
}

// onlyRefusal runs the scan over data and returns its one refusal, which it
// requires.
func onlyRefusal(t *testing.T, data []byte) ir.Diagnostic {
	t.Helper()
	diags := scanPlaced(t, data)
	require.Len(t, diags, 1, "in\n%s", data)
	require.Equal(t, ir.SeverityError, diags[0].Severity)
	return diags[0]
}

// TestAliases_NamesTheLeastRecursiveAlias pins the site of a recursive anchor
// when a document holds several: the alias with the least pointer, in either
// order. Each anchor stays above its own alias, so both orders parse.
func TestAliases_NamesTheLeastRecursiveAlias(t *testing.T) {
	t.Parallel()
	members := []string{"b: &x {k: [*x]}\n", "a: &y {k: [*y]}\n"}
	for order, data := range bothOrders(members) {
		t.Run(order, func(t *testing.T) {
			t.Parallel()
			d := onlyRefusal(t, data)
			assert.Equal(t, diag.CyclicRef, d.Code)
			assert.Equal(t, jsontext.Pointer("/a/k/0"), d.Provenance.Pointer)
			assert.Equal(t, `recursive YAML anchor "y" references an ancestor node`, d.Message)
		})
	}
}
