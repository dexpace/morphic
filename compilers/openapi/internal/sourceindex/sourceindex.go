// Package sourceindex answers, in one walk, the questions asked of a decoded
// source tree before any of it is lowered: how many nodes it declares, how
// deeply it nests, which aliases name an ancestor, and which mappings carry a
// tag the parser faults on, as a value the caller carries rather than a walk
// it repeats. Only what an alias names is measured once more.
//
// The index has no exported fields and no maps, and nothing in it changes
// once Build returns, so a copy is as good as the original. Equal trees give
// equal indexes.
package sourceindex

import (
	"context"
	"math"
	"slices"

	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
)

// MaxIndexedNodes bounds how many nodes one index walks. It is the explicit
// loop limit, not a memory guard: the tree is already materialized by the
// decode, and the index holds what it reports, not a record per node. It sits
// far above any document that could have been decoded.
//
// A document past it is refused by the caller rather than half-counted: a
// truncated count would understate the alias-expansion allowance derived from
// it and could refuse a document on a bound it never crossed.
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
// no nesting, no anchor cycle, no tagged mapping, nothing truncated. That is the
// same answer Build returns for an empty document, so a caller never has to
// tell the two apart.
type Index struct {
	root           *yaml.Node
	nodes          int64
	nesting        int64
	anchorCycles   []*yaml.Node
	taggedMappings []*yaml.Node
	truncated      bool
}

// Build walks the tree under root once, measuring what each alias names as it
// meets it, and returns what it found. root may be a document node or the
// content node itself; either way the index is rooted at the content, which is
// the node every consumer scans from.
//
// maxNodes is the caller's bound on the walk — MaxIndexedNodes in the compiler,
// smaller in a test that drives the truncated path. A non-positive bound indexes
// nothing and reports Truncated, which is the safe direction: it never claims a
// count it did not reach.
func Build(ctx context.Context, root *yaml.Node, maxNodes int64) Index {
	content := nodeview.DocumentRoot(root)
	if content == nil {
		return Index{}
	}
	if maxNodes <= 0 {
		return Index{root: content, truncated: true}
	}

	idx := Index{root: content}
	idx.walk(ctx, maxNodes)
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

// Nesting is how many mappings and sequences the deepest path down the tree
// passes through once every alias is expanded, which is the tree the parser
// builds (see Measure). A scalar or empty document nests 0. It is meaningful
// only when Truncated is false and AnchorCycles is empty: an alias back into
// its own ancestor expands without end, and counts here as far as one turn.
func (x Index) Nesting() int64 { return x.nesting }

// AnchorCycles lists, in document order, every alias whose target is one of
// its own ancestors — a recursive YAML anchor that expands without bound.
// Legal anchor reuse, where an alias names a node that is not an ancestor, is
// not one.
func (x Index) AnchorCycles() []*yaml.Node { return slices.Clone(x.anchorCycles) }

// MapTag is the tag yaml.v3 resolves every mapping to, and the one tag the
// speakeasy parser accepts on a mapping at a modelled position. A parse never
// leaves a mapping's tag empty, so a mapping reaches the parser with any other
// tag only when the source wrote one: a local `!content:`, or a standard tag
// naming another type (`!!str`, `!!set`).
const MapTag = "!!map"

// TaggedMappings lists, in document order, every mapping whose tag is not
// MapTag. The parser degrades one at a plain object position to a type-mismatch
// finding, but at a reference position whose model is a struct (a request body,
// a response, a parameter) it leaves the model unbuilt and dereferences it, on
// a goroutine of its own where no recover in this compiler reaches
// (docs/parser-library.md#tagged-mapping-panic, GitHub #474). The caller
// refuses the document on this answer before the parser sees it.
//
// A tag on a scalar or sequence is not this question: neither faults the
// parser (what a tagged scalar keeps is GitHub #245).
func (x Index) TaggedMappings() []*yaml.Node { return slices.Clone(x.taggedMappings) }

// Truncated reports whether the walk stopped at its node bound. Every other
// answer is then a partial one and must not be read as a fact about the
// document.
func (x Index) Truncated() bool { return x.truncated }

// walker is one walk's working state: the ancestors of the node being visited,
// the deepest nesting met so far under each node still open, and the measure
// an alias's target is taken from.
type walker struct {
	x         *Index
	ancestors map[*yaml.Node]bool
	tallest   []int64
	measure   *Measure
}

// walk visits every node once, in document order, counting and measuring as
// it goes, and recording every alias naming an ancestor and every mapping
// whose tag is not MapTag.
//
// The raw parse tree is a tree: aliasing adds only edges this walk never
// follows, so no node is reached twice and the ancestor set is exactly the path
// from the root. An alias nests as its target, which the measure takes once.
// The walk is bounded by maxNodes (nodeview.Walk), and it never stops at a
// finding, or one answer would hide another.
func (x *Index) walk(ctx context.Context, maxNodes int64) {
	w := walker{x: x, ancestors: map[*yaml.Node]bool{}, measure: NewMeasure(maxNodes)}
	visit := func(n *yaml.Node, depth int) nodeview.Step { return w.visit(ctx, n, depth) }
	out := nodeview.Walk{Nodes: int(min(maxNodes, math.MaxInt)), Visit: visit, Leave: w.leave}.Run(ctx, x.root)
	x.nodes = int64(out.Visited)
	x.truncated = out.Cut || w.measure.Truncated()
}

// visit records what n answers, at depth below the root. An alias is a leaf:
// yaml.v3 gives it empty Content, and its target is not its child, so it is
// finished here as the nesting of what it names, measured under ctx. Any other
// node is opened, for leave to close once its children are measured.
func (w *walker) visit(ctx context.Context, n *yaml.Node, depth int) nodeview.Step {
	w.x.recordTag(n)
	tracked := depth <= maxTrackedDepth
	if n.Kind == yaml.AliasNode {
		if tracked {
			w.x.recordAlias(n, w.ancestors)
		}
		w.finish(w.measure.Of(ctx, n))
		return nodeview.Prune
	}
	if tracked {
		w.ancestors[n] = true
	}
	w.tallest = append(w.tallest, 0)
	return nodeview.Descend
}

// leave closes n once its children are measured.
func (w *walker) leave(n *yaml.Node, depth int) {
	if depth <= maxTrackedDepth {
		delete(w.ancestors, n)
	}
	tallest := w.tallest[len(w.tallest)-1]
	w.tallest = w.tallest[:len(w.tallest)-1]
	w.finish(tallest + Level(n))
}

// finish hands a finished node's nesting to the node holding it, or to the
// index when it is the root.
func (w *walker) finish(nesting int64) {
	if len(w.tallest) == 0 {
		w.x.nesting = nesting
		return
	}
	top := &w.tallest[len(w.tallest)-1]
	*top = max(*top, nesting)
}

// recordTag keeps every mapping whose tag is not MapTag. The comparison is the
// parser's own — the raw tag against the one spelling — rather than yaml.v3's
// ShortTag, which would read an empty tag as MapTag and pass a node the parser
// would still fault on.
func (x *Index) recordTag(n *yaml.Node) {
	if n.Kind == yaml.MappingNode && n.Tag != MapTag {
		x.taggedMappings = append(x.taggedMappings, n)
	}
}

// recordAlias keeps an alias whose target is on the current path.
func (x *Index) recordAlias(alias *yaml.Node, ancestors map[*yaml.Node]bool) {
	if alias.Alias != nil && ancestors[alias.Alias] {
		x.anchorCycles = append(x.anchorCycles, alias)
	}
}
