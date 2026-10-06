package load

import (
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
	"strconv"
	"strings"
	"testing"

	"github.com/speakeasy-api/openapi/jsonpointer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/ynode"
)

// libraryRead is jsonpointer.GetTarget's walk of a YAML tree (yamlnode.go in
// speakeasy-api/openapi v1.25.2) transcribed with a counter: a step for each
// node the walk navigates to and each time a loop over a mapping's pairs runs.
// It is what treeReads' count is checked against, and GetTarget is what this
// transcription is checked against.
type libraryRead struct{ steps int }

func (l *libraryRead) stack(n *yaml.Node, parts []part) (*yaml.Node, bool) {
	if len(parts) == 0 {
		return l.node(n, part{}, nil)
	}
	return l.node(n, parts[0], parts[1:])
}

func (l *libraryRead) node(n *yaml.Node, p part, rest []part) (*yaml.Node, bool) {
	l.steps++
	for n != nil && (n.Kind == yaml.AliasNode || n.Kind == yaml.DocumentNode) {
		if n.Kind == yaml.AliasNode {
			n = n.Alias
			continue
		}
		if len(n.Content) == 0 {
			return nil, false
		}
		n = n.Content[0]
	}
	if n == nil {
		return nil, false
	}
	if len(rest) == 0 && p.value == "" {
		return n, true
	}
	switch n.Kind {
	case yaml.MappingNode:
		return l.mapping(n, p, rest)
	case yaml.SequenceNode:
		i, err := strconv.Atoi(p.value)
		if !p.index || err != nil || i < 0 || i >= len(n.Content) {
			return nil, false
		}
		if len(rest) == 0 {
			return n.Content[i], true
		}
		return l.stack(n.Content[i], rest)
	default:
		return nil, false
	}
}

func (l *libraryRead) mapping(n *yaml.Node, p part, rest []part) (*yaml.Node, bool) {
	key := strings.ReplaceAll(strings.ReplaceAll(p.value, "~1", "/"), "~0", "~")
	for i := 0; i < len(n.Content); i += 2 {
		l.steps++
		if i+1 >= len(n.Content) {
			break
		}
		k := n.Content[i]
		for k.Kind == yaml.AliasNode && k.Alias != nil {
			k = k.Alias
		}
		if k.Kind == yaml.ScalarNode && k.Value == key {
			if len(rest) == 0 {
				return n.Content[i+1], true
			}
			return l.stack(n.Content[i+1], rest)
		}
	}
	for i := 0; i < len(n.Content); i += 2 {
		l.steps++
		if i+1 >= len(n.Content) {
			break
		}
		k, v := n.Content[i], n.Content[i+1]
		if k.Kind != yaml.ScalarNode || k.Value != "<<" {
			continue
		}
		for v.Kind == yaml.AliasNode && v.Alias != nil {
			v = v.Alias
		}
		if v.Kind == yaml.MappingNode {
			if target, ok := l.mapping(v, p, rest); ok {
				return target, true
			}
		}
	}
	return nil, false
}

// TestTreeReads_CountsWhatTheLibraryReadTakes pins treeReads against the
// library on generated trees: for every pointer, it ends where GetTarget ends,
// or fails where GetTarget fails, and counts the steps the transcribed walk
// takes. The trees spell what reads differ on: a key written twice, a key or a
// value that is an alias, `<<` keys naming an alias, an inline mapping, a
// sequence or a scalar, several in one mapping, a document node, an odd pair.
func TestTreeReads_CountsWhatTheLibraryReadTakes(t *testing.T) {
	t.Parallel()
	for seed := range uint64(8) {
		rng := rand.New(rand.NewPCG(seed, 773))
		for range 400 {
			g := &treeGen{rng: rng}
			tree := g.root()
			reads := newTreeReads()
			for range 25 {
				pointer := g.pointer(tree)
				lib, libErr := jsonpointer.GetTarget(tree, jsonpointer.JSONPointer(pointer),
					jsonpointer.WithStructTags("key"))
				steps, target := reads.cost(tree, pointer, math.MaxInt)
				parts, valid := partsOf(pointer)
				if !valid {
					assert.ErrorIs(t, libErr, jsonpointer.ErrValidation, "%q", pointer)
					assert.Zero(t, steps, "%q", pointer)
					assert.Nil(t, target, "%q", pointer)
					continue
				}
				want := &libraryRead{}
				end, found := want.stack(tree, parts)
				require.Equal(t, libErr == nil, found, "seed %d %q: %v", seed, pointer, libErr)
				if found {
					require.Same(t, lib, end, "seed %d %q: the transcription ends where GetTarget does", seed, pointer)
				}
				assert.Equal(t, want.steps, steps, "seed %d %q", seed, pointer)
				assert.Same(t, end, target, "seed %d %q", seed, pointer)
			}
		}
	}
}

// treeGen builds random YAML trees and pointers into them. Every alias names a
// node built before it, so a tree has no cycle, as a parsed one has none.
type treeGen struct {
	rng   *rand.Rand
	built []*yaml.Node
}

var treeGenKeys = []string{"a", "b", "0", "1", "01", "a/b", "a~b", "~1", "/", "", "<<"}

func (g *treeGen) root() *yaml.Node {
	n := g.value(3)
	switch g.rng.IntN(6) {
	case 0:
		return &yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{n}}
	case 1:
		return &yaml.Node{Kind: yaml.DocumentNode}
	default:
		return n
	}
}

func (g *treeGen) value(depth int) *yaml.Node {
	var n *yaml.Node
	switch r := g.rng.IntN(10); {
	case depth <= 0 || r < 2:
		n = ynode.Scalar(treeGenKeys[g.rng.IntN(len(treeGenKeys))])
	case r < 3 && len(g.built) > 0:
		return ynode.Alias(g.built[g.rng.IntN(len(g.built))])
	case r < 5:
		n = ynode.Seq()
		for range g.rng.IntN(3) {
			n.Content = append(n.Content, g.value(depth-1))
		}
	default:
		n = g.mapping(depth)
	}
	g.built = append(g.built, n)
	return n
}

func (g *treeGen) mapping(depth int) *yaml.Node {
	n := ynode.Map()
	for range g.rng.IntN(5) {
		if g.rng.IntN(5) == 0 {
			n.Content = append(n.Content, ynode.Merge(), g.mergeSource(depth))
			continue
		}
		n.Content = append(n.Content, g.key(), g.value(depth-1))
	}
	if g.rng.IntN(20) == 0 {
		n.Content = append(n.Content, ynode.Scalar("a"))
	}
	return n
}

func (g *treeGen) key() *yaml.Node {
	if g.rng.IntN(8) == 0 && len(g.built) > 0 {
		return ynode.Alias(g.built[g.rng.IntN(len(g.built))])
	}
	return ynode.Scalar(treeGenKeys[g.rng.IntN(len(treeGenKeys))])
}

func (g *treeGen) mergeSource(depth int) *yaml.Node {
	var maps []*yaml.Node
	for _, b := range g.built {
		if b.Kind == yaml.MappingNode {
			maps = append(maps, b)
		}
	}
	switch r := g.rng.IntN(5); {
	case r < 2 && len(maps) > 0:
		return ynode.Alias(maps[g.rng.IntN(len(maps))])
	case r < 3:
		return ynode.Seq(g.value(0))
	case r < 4:
		return ynode.Scalar("x")
	default:
		return g.mapping(depth - 1)
	}
}

// pointer returns a pointer that mostly follows the tree from root, by the
// keys and indexes it holds, and sometimes strays: a token naming nothing, an
// empty one, one the library refuses, or no pointer at all.
func (g *treeGen) pointer(root *yaml.Node) string {
	if g.rng.IntN(12) == 0 {
		return []string{"", "/", "x", "/~2", "/a//b", "/-1", "/00"}[g.rng.IntN(7)]
	}
	var b strings.Builder
	n := root
	for range 1 + g.rng.IntN(4) {
		b.WriteString("/" + g.token(n))
		n = g.childOf(n)
	}
	return b.String()
}

func (g *treeGen) token(n *yaml.Node) string {
	n = derefAll(n)
	switch {
	case g.rng.IntN(8) == 0:
		return []string{"zz", "", "~01", "2"}[g.rng.IntN(4)]
	case n != nil && n.Kind == yaml.MappingNode && len(n.Content) >= 2:
		key := derefAll(n.Content[2*g.rng.IntN(len(n.Content)/2)])
		if key == nil || key.Kind != yaml.ScalarNode {
			return "a"
		}
		return strings.ReplaceAll(strings.ReplaceAll(key.Value, "~", "~0"), "/", "~1")
	case n != nil && n.Kind == yaml.SequenceNode && len(n.Content) > 0:
		return strconv.Itoa(g.rng.IntN(len(n.Content)))
	default:
		return treeGenKeys[g.rng.IntN(len(treeGenKeys))]
	}
}

// childOf returns some child of n for the walk to go on from, or n.
func (g *treeGen) childOf(n *yaml.Node) *yaml.Node {
	n = derefAll(n)
	if n == nil || len(n.Content) == 0 {
		return n
	}
	return n.Content[g.rng.IntN(len(n.Content))]
}

func derefAll(n *yaml.Node) *yaml.Node {
	for n != nil && (n.Kind == yaml.AliasNode || n.Kind == yaml.DocumentNode && len(n.Content) > 0) {
		if n.Kind == yaml.AliasNode {
			n = n.Alias
		} else {
			n = n.Content[0]
		}
	}
	return n
}

// TestTreeReads_Cost pins the count on a parsed document, a row for each way
// the library's read spends its steps: a step for each node it navigates to,
// and one for each pair it compares, or passes on its way to a `<<` key.
func TestTreeReads_Cost(t *testing.T) {
	t.Parallel()
	tree, parsed := parseTree([]byte(`a: 1
b: {c: 2}
b: 3
<<: {d: 4}
s: [x, y]
`))
	require.True(t, parsed)
	b := tree.Content[0].Content[3]
	for _, c := range []struct {
		pointer string
		steps   int
		target  *yaml.Node
		why     string
	}{
		{"/a", 2, tree.Content[0].Content[1], "the root, and a, its first key"},
		{"/b", 3, b, "a key written twice is found where it is written first"},
		{"/b/c", 5, b.Content[1], "b's mapping is navigated to, and c is its first key"},
		{"/d", 11, nil, "five keys compared, four passed to reach `<<`, d is the merged mapping's first"},
		{"/z", 13, nil, "five keys compared, the merged mapping's one, its pairs passed, then the root's"},
		{"/s/1", 7, nil, "s is the fifth key, and its sequence is navigated to"},
		{"/a/", 3, tree.Content[0].Content[1], "an empty last token reads as the node it is read on"},
		{"/", 1, tree.Content[0], "the root alone"},
		{"", 0, nil, "a pointer the library refuses before reading"},
	} {
		steps, target := newTreeReads().cost(tree, c.pointer, math.MaxInt)
		assert.Equal(t, c.steps, steps, "%q: %s", c.pointer, c.why)
		if c.target != nil {
			assert.Same(t, c.target, target, "%q", c.pointer)
		}
	}
}

// TestTreeReads_StopsPastTheLimit pins that a count stops once past its limit,
// so what it spends on a read is bounded by what it is allowed: n `<<` keys
// each naming a mapping of n keys, none holding the token, take about n squared
// steps unbounded, and a few past the limit bounded.
func TestTreeReads_StopsPastTheLimit(t *testing.T) {
	t.Parallel()
	const n = 64
	wide := ynode.Map()
	for i := range n {
		wide.Content = append(wide.Content, ynode.Scalar(fmt.Sprint("k", i)), ynode.Scalar("v"))
	}
	root := ynode.Map()
	for range n {
		root.Content = append(root.Content, ynode.Merge(), ynode.Alias(wide))
	}
	unbounded, _ := newTreeReads().cost(root, "/missing", math.MaxInt)
	require.Greater(t, unbounded, n*n)

	const limit = 3 * n
	steps, target := newTreeReads().cost(root, "/missing", limit)
	assert.Nil(t, target)
	assert.Greater(t, steps, limit, "a count past the limit says so")
	assert.LessOrEqual(t, steps, limit+2*n, "and stops within a mapping of it")
}

// TestTreeReads_MergeDepth pins maxScanMerges: a key maxScanMerges merged
// mappings down is counted, and one a level further is past any limit, though
// the library would read it.
func TestTreeReads_MergeDepth(t *testing.T) {
	t.Parallel()
	steps, target := newTreeReads().cost(ynode.MergeChain(maxScanMerges), "/leaf", math.MaxInt)
	require.NotNil(t, target)
	assert.Equal(t, "v", target.Value)
	assert.Less(t, steps, 10*maxScanMerges)

	const limit = 1 << 20
	steps, target = newTreeReads().cost(ynode.MergeChain(maxScanMerges+1), "/leaf", limit)
	assert.Nil(t, target)
	assert.Greater(t, steps, limit)

	_, err := jsonpointer.GetTarget(ynode.MergeChain(maxScanMerges+1), "/leaf")
	assert.NoError(t, err, "the library reads past the bound")
}

// TestTreeReads_IndexesAMappingOnce pins that a mapping's keys are indexed the
// first time a read meets it, and drained as work once, whichever key and
// however many reads follow.
func TestTreeReads_IndexesAMappingOnce(t *testing.T) {
	t.Parallel()
	tree, parsed := parseTree([]byte("x: {a: 1, b: 2, c: 3}\ny: 4\n"))
	require.True(t, parsed)
	reads := newTreeReads()
	for _, pointer := range []string{"/x/a", "/x/c", "/x/missing", "/y"} {
		reads.cost(tree, pointer, math.MaxInt)
	}
	assert.Equal(t, 2+3, reads.drain(), "the root's two pairs and x's three, each once")
	assert.Len(t, reads.keys, 2)
	reads.cost(tree, "/x/b", math.MaxInt)
	assert.Zero(t, reads.drain(), "drained once, and nothing indexed since")
}

// TestTreeReads_ModelCostReadsOnThroughAStructHeldByValue pins that a read
// priced in the model goes on past a struct the model holds by value, as the
// library does: an operation's responses, whose default response only its
// address answers, below which the response's raw extension of n keys is
// scanned for a key it does not hold.
func TestTreeReads_ModelCostReadsOnThroughAStructHeldByValue(t *testing.T) {
	t.Parallel()
	const n = 64
	var ext strings.Builder
	for i := range n {
		fmt.Fprintf(&ext, "k%d: %d, ", i, i)
	}
	spec := "openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\npaths:\n  /a:\n    get:\n      responses:\n" +
		"        default: {description: ok, x-wide: {" + strings.TrimSuffix(ext.String(), ", ") + "}}\n"
	root, _, err := decodeStream([]byte(spec))
	require.NoError(t, err)
	doc, _, err := unmarshal(t.Context(), []byte(spec), root)
	require.NoError(t, err)

	steps := newTreeReads().modelCost(doc, "/paths/~1a/get/responses/default/x-wide/missing", math.MaxInt)
	assert.Greater(t, steps, 2*n, "the wide extension's keys are compared, then passed for a merge key")
}

// TestPartsOf pins how a pointer is split as the library splits it: a token is
// an index when it is digits with no leading zero, '/' alone has no token, and
// what the library's validation refuses is no pointer.
func TestPartsOf(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		pointer string
		want    []part
		ok      bool
	}{
		{"/a/0/10/01/", []part{{"a", false}, {"0", true}, {"10", true}, {"01", false}, {"", false}}, true},
		{"/a~1b/~0", []part{{"a~1b", false}, {"~0", false}}, true},
		{"/", nil, true},
		{"", nil, false},
		{"a", nil, false},
		{"/~2", nil, false},
	} {
		got, ok := partsOf(c.pointer)
		assert.Equal(t, c.ok, ok, "%q", c.pointer)
		assert.Equal(t, c.want, got, "%q", c.pointer)
		_, err := jsonpointer.GetTarget(ynode.Map(), jsonpointer.JSONPointer(c.pointer))
		assert.Equal(t, !c.ok, errors.Is(err, jsonpointer.ErrValidation), "%q: the library refuses it too", c.pointer)
	}
}
