package load

import (
	"encoding/json/jsontext"
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
// library on generated trees: for every pointer, cost and read end where
// GetTarget ends, or fail where GetTarget fails, and cost counts the steps the
// transcribed walk takes. The trees spell what reads differ on: a key written
// twice, a key or a value that is an alias, `<<` keys naming an alias, an
// inline mapping, a sequence or a scalar, several in one mapping, a document
// node, an odd pair.
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
				_, read := reads.read(tree, pointer, math.MaxInt)
				assert.Same(t, end, read, "seed %d %q: a read counting its own steps finds the same node", seed, pointer)
			}
		}
	}
}

// FuzzTreeReads_ReadEndsWhereGetTargetEnds carries the test above past its
// eight seeds: for every tree treeGen builds from a fuzzed seed and every
// pointer it draws into it, both of treeReads' walks end where GetTarget ends,
// or fail where it fails. loops answers from read, so a read ending elsewhere
// is a chain it follows apart from the resolver.
func FuzzTreeReads_ReadEndsWhereGetTargetEnds(f *testing.F) {
	for seed := range uint64(8) {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, seed uint64) {
		g := &treeGen{rng: rand.New(rand.NewPCG(seed, 775))}
		tree := g.root()
		reads := newTreeReads()
		for range 25 {
			pointer := g.pointer(tree)
			lib, libErr := jsonpointer.GetTarget(tree, jsonpointer.JSONPointer(pointer), jsonpointer.WithStructTags("key"))
			_, read := reads.read(tree, pointer, math.MaxInt)
			_, priced := reads.cost(tree, pointer, math.MaxInt)
			if libErr != nil {
				require.Nil(t, read, "seed %d %q: %v", seed, pointer, libErr)
				require.Nil(t, priced, "seed %d %q: %v", seed, pointer, libErr)
				continue
			}
			require.Same(t, lib, read, "seed %d %q", seed, pointer)
			require.Same(t, lib, priced, "seed %d %q", seed, pointer)
		}
	})
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

// TestTreeReads_ReadsAMergeChainAsDeepAsTheLibrary pins that a read goes as
// deep through `<<` keys as the library does, with no bound of its own: a key
// 2^17 merged mappings down is found where GetTarget finds it, and one no
// mapping holds fails where it fails, each counted as the transcription counts
// it, in steps linear in the depth.
func TestTreeReads_ReadsAMergeChainAsDeepAsTheLibrary(t *testing.T) {
	t.Parallel()
	const levels = 1 << 17
	chain := ynode.MergeChain(levels)
	for _, pointer := range []string{"/leaf", "/missing"} {
		lib, libErr := jsonpointer.GetTarget(chain, jsonpointer.JSONPointer(pointer))
		parts, valid := partsOf(pointer)
		require.True(t, valid)
		want := &libraryRead{}
		end, found := want.stack(chain, parts)
		require.Equal(t, libErr == nil, found, "%q: %v", pointer, libErr)
		if found {
			require.Same(t, lib, end, "%q", pointer)
		}

		steps, target := newTreeReads().cost(chain, pointer, math.MaxInt)
		assert.Same(t, end, target, "%q", pointer)
		assert.Equal(t, want.steps, steps, "%q", pointer)
		assert.Less(t, steps, 3*levels, "%q", pointer)
		_, read := newTreeReads().read(chain, pointer, math.MaxInt)
		assert.Same(t, end, read, "%q", pointer)
	}
}

// TestTreeReads_PricesAMergeCyclePastTheLimit pins the guard on a read through
// `<<` keys entering a mapping it is already in for the same part, which the
// library would follow without end and no parsed tree holds: the read fails
// past its limit where the cycle closes, in one mapping read through, not
// one per step until the limit runs out. With no limit, the count is the most
// an int holds rather than wrapping negative.
func TestTreeReads_PricesAMergeCyclePastTheLimit(t *testing.T) {
	t.Parallel()
	m := ynode.Map()
	m.Content = append(m.Content, ynode.Merge(), ynode.Alias(m), ynode.Scalar("a"), ynode.Scalar("1"))
	parts, valid := partsOf("/missing")
	require.True(t, valid)
	for _, own := range []bool{false, true} {
		s := tally{reads: newTreeReads(), limit: 1 << 20, own: own}
		assert.Nil(t, s.walk(m, parts), "own %t", own)
		assert.Greater(t, s.steps, s.limit, "own %t", own)
		require.Len(t, s.trying, 1, "own %t: refused where the cycle closes, or the unlimited read below runs on", own)
	}

	steps, target := newTreeReads().cost(m, "/missing", math.MaxInt)
	assert.Nil(t, target)
	assert.Equal(t, math.MaxInt, steps)
}

// TestTreeReads_ReadsAMergeCycleTheLibraryEnds pins that the guard above
// refuses only a read that would not end: a mapping open for one part, entered
// again for the next, is read where the library's read of it ends, and counted
// as the transcription counts it.
func TestTreeReads_ReadsAMergeCycleTheLibraryEnds(t *testing.T) {
	t.Parallel()
	a, b := ynode.Map(), ynode.Map()
	a.Content = append(a.Content, ynode.Merge(), ynode.Alias(b))
	b.Content = append(b.Content, ynode.Scalar("k"), ynode.Alias(a))
	for _, pointer := range []string{"/k/k/k", "/k/k/missing"} {
		lib, libErr := jsonpointer.GetTarget(a, jsonpointer.JSONPointer(pointer))
		parts, valid := partsOf(pointer)
		require.True(t, valid)
		want := &libraryRead{}
		end, found := want.stack(a, parts)
		require.Equal(t, libErr == nil, found, "%q: %v", pointer, libErr)
		if found {
			require.Same(t, lib, end, "%q", pointer)
		}

		steps, target := newTreeReads().cost(a, pointer, math.MaxInt)
		assert.Same(t, end, target, "%q", pointer)
		assert.Equal(t, want.steps, steps, "%q", pointer)
		_, read := newTreeReads().read(a, pointer, math.MaxInt)
		assert.Same(t, end, read, "%q", pointer)
	}
}

// TestTreeReads_HoldsAsTheLibraryReadsOneToken pins holds against the library
// on generated trees: for every token their keys spell, and some that none
// does or that the library refuses, holds finds a node exactly where
// GetTarget's read of the one-token pointer does. That read is what a walk
// leaving the model at a reference asks of its target (resolve.Scope.Holds).
func TestTreeReads_HoldsAsTheLibraryReadsOneToken(t *testing.T) {
	t.Parallel()
	found := 0
	for seed := range uint64(8) {
		rng := rand.New(rand.NewPCG(seed, 778))
		for range 200 {
			tree := (&treeGen{rng: rng}).root()
			reads := newTreeReads()
			for _, token := range holdsTokens(tree) {
				_, err := jsonpointer.GetTarget(tree, jsonpointer.JSONPointer("/"+jsonpointer.EscapeString(token)),
					jsonpointer.WithStructTags("key"))
				require.Equal(t, err == nil, reads.holds(tree, token), "seed %d %q: %v", seed, token, err)
				if err == nil {
					found++
				}
			}
		}
	}
	assert.Greater(t, found, 1000, "the trees hold many of the tokens asked")
}

// holdsTokens returns the tokens to ask of n: every key treeGen writes, each
// key n's own pairs spell, and tokens no mapping holds or the library refuses.
func holdsTokens(n *yaml.Node) []string {
	tokens := append([]string{"zz", "~", "~01", "\U0001F600", "\xff"}, treeGenKeys...)
	if n = derefAll(n); n == nil || n.Kind != yaml.MappingNode {
		return tokens
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		if key := derefAll(n.Content[i]); key != nil && key.Kind == yaml.ScalarNode {
			tokens = append(tokens, key.Value)
		}
	}
	return tokens
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

// TestTreeReads_ReadCostsItsDepthNotItsWidth pins what read counts: a step for
// each node visited, each key looked up and each `<<` key tried, so the last
// key of a mapping of n keys costs it what the first does, where the library's
// read, which cost counts, compares n keys. A key held past a merge costs the
// merges tried before it.
func TestTreeReads_ReadCostsItsDepthNotItsWidth(t *testing.T) {
	t.Parallel()
	const n = 64
	wide := ynode.Map()
	for i := range n {
		wide.Content = append(wide.Content, ynode.Scalar(fmt.Sprint("k", i)), ynode.Scalar("v"))
	}
	reads := newTreeReads()
	first, _ := reads.read(wide, "/k0", math.MaxInt)
	last, target := reads.read(wide, fmt.Sprint("/k", n-1), math.MaxInt)
	require.NotNil(t, target)
	assert.Equal(t, first, last, "the last key costs what the first does")
	missing, _ := reads.read(wide, "/missing", math.MaxInt)
	assert.Equal(t, first, missing, "and so does a key the mapping lacks")
	library, _ := reads.cost(wide, fmt.Sprint("/k", n-1), math.MaxInt)
	assert.Greater(t, library, n, "where the library compares every key")

	merged := ynode.Map(ynode.Merge(), ynode.Map(), ynode.Merge(), ynode.Scalar("x"), ynode.Merge(), wide)
	past, target := reads.read(merged, fmt.Sprint("/k", n-1), math.MaxInt)
	require.NotNil(t, target)
	assert.Equal(t, first+2+3, past, "a lookup in each merged mapping, and three merges tried, the last holding it")
	none, _ := reads.read(merged, "/missing", math.MaxInt)
	assert.Equal(t, past, none, "and the same where none holds it")
}

// specHead opens every document these tests price model reads in.
const specHead = "openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\npaths: {}\n"

// wideKeys is how many keys wideMapping spells.
const wideKeys = 64

// wideMapping spells a flow mapping of wideKeys keys, k0 first.
func wideMapping() string {
	keys := make([]string, wideKeys)
	for i := range keys {
		keys[i] = fmt.Sprintf("k%d: 0", i)
	}
	return "{" + strings.Join(keys, ", ") + "}"
}

// TestTreeReads_ModelCostPricesARefusedPointerAtNothing pins that a pointer the
// library refuses before reading, an invalid escape, is priced at nothing,
// though the key its tokens decode to would be scanned for in a wide extension.
func TestTreeReads_ModelCostPricesARefusedPointerAtNothing(t *testing.T) {
	t.Parallel()
	const n = wideKeys
	doc := modelOf(t, specHead+"x-lib: "+wideMapping()+"\n")
	for _, pointer := range []jsontext.Pointer{"/x-lib/D~2", "/x-lib/D~"} {
		_, err := jsonpointer.GetTarget(doc, jsonpointer.JSONPointer(pointer), jsonpointer.WithStructTags("key"))
		require.ErrorIs(t, err, jsonpointer.ErrValidation, "%q: the library reads nothing", pointer)
		assert.Zero(t, newTreeReads().modelCost(doc, pointer, math.MaxInt), "%q", pointer)
	}
	assert.Greater(t, newTreeReads().modelCost(doc, "/x-lib/D", math.MaxInt), n, "a key it lacks is scanned for")
}

// TestTreeReads_ModelCostChargesTheMergeTheLibraryScans pins that a read leaving
// the model is priced along the library's own read of the token and the rest
// together: where the first `<<` merge holds the token but not the rest, the
// library goes on to the next and scans its n keys, which a decoy cannot steer
// the price off. Past a field holding raw YAML, an enum member, the read below
// it is charged as well.
func TestTreeReads_ModelCostChargesTheMergeTheLibraryScans(t *testing.T) {
	t.Parallel()
	const n = wideKeys
	last := jsontext.Pointer(fmt.Sprintf("/x-lib/k%d", n-1))
	decoy := modelOf(t, specHead+"x-decoy: &decoy {x-lib: {k: 0}}\nx-hit: &hit {x-lib: "+wideMapping()+"}\n"+
		"<<: *decoy\n<<: *hit\n")
	_, err := jsonpointer.GetTarget(decoy, jsonpointer.JSONPointer(last), jsonpointer.WithStructTags("key"))
	require.NoError(t, err, "the library finds it past the decoy")
	assert.Greater(t, newTreeReads().modelCost(decoy, last, math.MaxInt), n, "the merged mapping's keys are charged")

	enum := modelOf(t, specHead+"components:\n  schemas:\n    E: {enum: ["+wideMapping()+"]}\n")
	member := jsontext.Pointer(fmt.Sprintf("/components/schemas/E/enum/0/k%d", n-1))
	assert.Greater(t, newTreeReads().modelCost(enum, member, math.MaxInt), n, "the member's keys are charged")
}

// TestTreeReads_ModelCostReadsNothingPastItsLimit pins that pricing a model
// read past its limit reads no raw YAML: the mapping the read leaves for is
// never indexed, so a bound spent bounds the work of its own pricing.
func TestTreeReads_ModelCostReadsNothingPastItsLimit(t *testing.T) {
	t.Parallel()
	doc := modelOf(t, specHead+"x-lib: "+wideMapping()+"\n")
	reads := newTreeReads()
	steps := reads.modelCost(doc, "/x-lib/k63", -1)
	assert.Positive(t, steps, "a count past the limit says so")
	assert.Zero(t, reads.drain(), "and indexes nothing")
	assert.Greater(t, reads.modelCost(doc, "/x-lib/k63", math.MaxInt), wideKeys)
	assert.Positive(t, reads.drain(), "a count within it indexes what it reads")
}

// TestJoinTokens_KeepsTheBytesTheLibraryReads pins that a token is spelled
// with the bytes the library compares, a byte that is no UTF-8 included, and
// that no tokens spell no pointer.
func TestJoinTokens_KeepsTheBytesTheLibraryReads(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		tokens []string
		want   string
	}{
		{nil, ""},
		{[]string{""}, "/"},
		{[]string{"a/b", "~x", ""}, "/a~1b/~0x/"},
		{[]string{"a\xffb", "k"}, "/a\xffb/k"},
	} {
		assert.Equal(t, c.want, joinTokens(c.tokens), "%q", c.tokens)
	}
}
