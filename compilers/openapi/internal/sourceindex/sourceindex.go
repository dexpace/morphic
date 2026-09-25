// Package sourceindex answers, in one walk, the questions asked of a decoded
// source tree before any of it is lowered.
//
// The questions are small and unrelated to each other — how many nodes did the
// document declare, does any alias point back at one of its own ancestors, and
// does any mapping carry a tag the parser faults on — but each was answered by a
// whole traversal of its own, so a compile walked the same tree once per
// question. They share a walk here instead, and the answers become a value the
// caller carries rather than a walk the caller repeats.
//
// The index is a value with no exported fields and no maps, so a copy is
// independent of the original and nothing that receives one can write through it
// to a holder. It is derived from the tree alone: two indexes built over the same
// tree are equal, whatever order their answers are read in.
package sourceindex

import (
	"slices"

	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
)

// MaxIndexedNodes bounds how many nodes one index walks.
//
// It is not a memory guard: the tree is already materialized by the decode that
// produced it, and the index itself holds one counter and two node pointers
// whatever the tree's size. It is the explicit limit the bounded-everything rule
// requires of every loop, placed far above any document that could have been
// decoded in the first place — the largest spec in this repository's corpora
// parses to fewer than 5,000 nodes, and the largest public OpenAPI documents to
// a few million. A document past it is refused by the caller rather than
// half-counted, because a truncated count would understate the alias-expansion
// allowance derived from it and could refuse a document on a bound it never
// crossed.
const MaxIndexedNodes = 1 << 24 // 16,777,216

// maxTrackedDepth bounds how deep the ancestor path is tracked, and so how deep
// a recursive anchor is still recognized. It is this package's own bound on its
// own walk, deliberately equal to the one the recursive anchor descent it
// replaces carried, so a document the old walk stopped tracking at is the same
// document this one stops tracking at. Nodes below it are still counted; they
// are simply not candidates for an anchor cycle, exactly as before.
const maxTrackedDepth = 10000

// Index is what one walk over a decoded source tree found.
//
// A zero Index is the answer for a document with no content: no root, no nodes,
// no anchor cycle, no tagged mapping, nothing truncated. That is the same answer
// Build returns for an empty document, so a caller never has to tell the two
// apart.
type Index struct {
	root          *yaml.Node
	nodes         int64
	anchorCycle   *yaml.Node
	taggedMapping *yaml.Node
	truncated     bool
}

// Build walks the tree under root once and returns what it found. root may be a
// document node or the content node itself; either way the index is rooted at
// the content, which is the node every consumer scans from.
//
// maxNodes is the caller's bound on the walk — MaxIndexedNodes in the compiler,
// smaller in a test that drives the truncated path. A non-positive bound indexes
// nothing and reports Truncated, which is the safe direction: it never claims a
// count it did not reach.
func Build(root *yaml.Node, maxNodes int64) Index {
	content := nodeview.DocumentRoot(root)
	if content == nil {
		return Index{}
	}
	if maxNodes <= 0 {
		return Index{root: content, truncated: true}
	}

	idx := Index{root: content}
	idx.walk(maxNodes)
	return idx
}

// Root is the node the index was built over — the content of a document node,
// or the node itself — or nil for an empty document.
func (x Index) Root() *yaml.Node { return x.root }

// Nodes is how many nodes the tree declares, before any alias is substituted. An
// alias counts as one and its target is not counted again through it, which is
// what makes this the document's own size rather than its expansion. It is
// meaningful only when Truncated is false.
func (x Index) Nodes() int64 { return x.nodes }

// AnchorCycle is the first alias, in document order, whose target is one of its
// own ancestors — a recursive YAML anchor that expands without bound. Legal
// anchor reuse, where an alias names a node that is not an ancestor, is not one.
func (x Index) AnchorCycle() (*yaml.Node, bool) {
	return x.anchorCycle, x.anchorCycle != nil
}

// MapTag is the tag yaml.v3 resolves every mapping to, and the one tag the
// speakeasy parser accepts on a mapping at a modelled position. A parse never
// leaves a mapping's tag empty, so a mapping reaches the parser with any other
// tag only when the source wrote one: a local `!content:`, or a standard tag
// naming another type (`!!str`, `!!set`).
const MapTag = "!!map"

// TaggedMapping is the first mapping, in document order, whose tag is not
// MapTag. The parser degrades one at a plain object position to a type-mismatch
// finding, but at a reference position whose model is a struct — a request
// body, a response, a parameter — it leaves the model unbuilt and dereferences
// it, on a goroutine of its own where no recover in this compiler reaches
// (GitHub #474). The caller refuses the document on this answer before the
// parser sees it.
//
// A tag on a scalar or a sequence is a different question and not this one:
// neither faults the parser, and what a tagged scalar keeps is decided
// elsewhere (GitHub #245).
func (x Index) TaggedMapping() (*yaml.Node, bool) {
	return x.taggedMapping, x.taggedMapping != nil
}

// Truncated reports whether the walk stopped at its node bound. Every other
// answer is then a partial one and must not be read as a fact about the
// document.
func (x Index) Truncated() bool { return x.truncated }

// frame is one entry of the walk's explicit stack: a node to visit at a known
// depth, or the ancestor-path exit of a node whose children have all been
// visited.
type frame struct {
	n     *yaml.Node
	depth int
	exit  bool
}

// walk visits every node reachable through Content exactly once, in the
// depth-first document order a recursive descent would take. It counts as it
// goes, and records the first alias that points back into the path it arrived
// by and the first mapping whose tag is not MapTag.
//
// The raw parse tree is a tree, not a graph: aliasing only adds edges this walk
// never follows, so no node is reached twice and the ancestor set is exactly the
// path from the root. That is what lets the walk be iterative and bounded by
// maxNodes rather than by a recursion cap.
//
// It does not stop at the first anchor cycle or tagged mapping. A consumer that
// refuses the document on one never reads the count, but an index that answered
// one question only until another was answered would depend on which was asked
// first.
func (x *Index) walk(maxNodes int64) {
	ancestors := map[*yaml.Node]bool{}
	stack := []frame{{n: x.root}}

	for len(stack) > 0 {
		f := stack[len(stack)-1]
		stack = stack[:len(stack)-1]

		if f.exit {
			delete(ancestors, f.n)
			continue
		}
		if f.n == nil {
			continue // never produced by a parse; not counted rather than dereferenced
		}

		x.nodes++
		if x.nodes > maxNodes {
			x.truncated = true
			return
		}

		x.recordTag(f.n)

		tracked := f.depth <= maxTrackedDepth
		if tracked && f.n.Kind == yaml.AliasNode {
			x.recordAlias(f.n, ancestors)
			continue // yaml.v3 gives an alias empty Content; its target is not its child
		}
		if tracked && len(f.n.Content) > 0 {
			ancestors[f.n] = true
			stack = append(stack, frame{n: f.n, exit: true})
		}
		for _, v := range slices.Backward(f.n.Content) {
			stack = append(stack, frame{n: v, depth: f.depth + 1})
		}
	}
}

// recordTag keeps the first mapping whose tag is not MapTag. The comparison is
// the parser's own — the raw tag against the one spelling — rather than
// yaml.v3's ShortTag, which would read an empty tag as MapTag and pass a node
// the parser would still fault on.
func (x *Index) recordTag(n *yaml.Node) {
	if x.taggedMapping != nil || n.Kind != yaml.MappingNode {
		return
	}
	if n.Tag != MapTag {
		x.taggedMapping = n
	}
}

// recordAlias keeps the first alias whose target is on the current path. Later
// ones are dropped rather than overwriting it, so the reported node is the one a
// depth-first descent would have stopped at.
func (x *Index) recordAlias(alias *yaml.Node, ancestors map[*yaml.Node]bool) {
	if x.anchorCycle != nil || alias.Alias == nil {
		return
	}
	if ancestors[alias.Alias] {
		x.anchorCycle = alias
	}
}
