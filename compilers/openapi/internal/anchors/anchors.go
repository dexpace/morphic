// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package anchors

import (
	"encoding/json/jsontext"
	"slices"
	"strconv"

	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/budget"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
)

// MaxDepth bounds how deep the index walk reads. Document trees nest far
// shallower — a schema under a media type under a response is about ten
// levels — so it only ever stops a pathological structure.
const MaxDepth = 512

// MaxNodes bounds how many nodes the walk reads in all. Depth alone does not
// bound it: the walk follows aliases, and an alias graph is a DAG, so a shallow
// document can present far more paths than it has nodes.
const MaxNodes = 1 << 20

// Index is a document's $dynamicAnchor declarations: for each name, the
// pointers of the mappings declaring it, in document order.
type Index struct {
	byName   map[string][]jsontext.Pointer
	complete bool
}

// Sites returns the pointers declaring name.
func (x Index) Sites(name string) []jsontext.Pointer { return slices.Clone(x.byName[name]) }

// Complete reports whether the walk read the whole tree. A partial index can
// only undercount, and an undercount reads as "declared exactly once", the one
// count that expands a $dynamicRef, so a reader of a partial one says so.
func (x Index) Complete() bool { return x.complete }

// frame is one entry of the walk's stack: a node to read at a pointer and
// depth, or, where anchor is set, a mapping's $dynamicAnchor to record.
type frame struct {
	node    *yaml.Node
	pointer jsontext.Pointer
	depth   int
	anchor  bool
}

// Build indexes the document under root. It reads the raw tree, since the
// parser's schema model has no field for the keyword
// (docs/parser-library.md#unmodeled-keywords), position by position as the
// parser presents each mapping's pairs (nodeview.View.MappingPairs): a walk
// of Content (nodeview.Walk) would miss what a merge brings in. It is bounded
// by MaxDepth and MaxNodes, and by the view's merge bound, each reported by
// Complete.
func Build(root *yaml.Node) Index {
	return build(root, MaxDepth, MaxNodes)
}

// build is Build within maxDepth levels and maxNodes nodes.
func build(root *yaml.Node, maxDepth, maxNodes int) Index {
	view := nodeview.New()
	x := Index{byName: map[string][]jsontext.Pointer{}, complete: true}
	stack := []frame{{node: root}}
	nodes := budget.New(budget.AtMost(maxNodes))
	for len(stack) > 0 {
		f := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if f.anchor {
			x.record(f.node, f.pointer)
			continue
		}
		n := nodeview.Deref(f.node)
		if n == nil {
			continue
		}
		if f.depth > maxDepth || !nodes.Spend(1) {
			x.complete = false
			continue
		}
		stack = append(stack, children(view, n, f)...)
	}
	x.complete = x.complete && !view.Exhausted()
	return x
}

// children returns what the walk reads below n, read at f, last first so the
// stack yields them in order: a document's content, a sequence's items, a
// mapping's values, and a mapping's $dynamicAnchor where it writes one. A
// scalar holds nothing.
func children(view *nodeview.View, n *yaml.Node, f frame) []frame {
	var out []frame
	switch n.Kind {
	case yaml.DocumentNode:
		for _, c := range n.Content {
			out = append(out, frame{node: c, pointer: f.pointer, depth: f.depth + 1})
		}
	case yaml.SequenceNode:
		for i, c := range n.Content {
			out = append(out, frame{node: c, pointer: f.pointer + ids.Ptr(strconv.Itoa(i)), depth: f.depth + 1})
		}
	case yaml.MappingNode:
		for _, p := range view.MappingPairs(n) {
			if p.Key == "$dynamicAnchor" {
				out = append(out, frame{node: p.Val, pointer: f.pointer, anchor: true})
				continue
			}
			out = append(out, frame{node: p.Val, pointer: f.pointer + ids.Ptr(p.Key), depth: f.depth + 1})
		}
	default:
	}
	slices.Reverse(out)
	return out
}

// record indexes the anchor val names at pointer. A value that is not a
// non-empty scalar names nothing a $dynamicRef could spell, and a null is the
// keyword left out (docs/compilers/openapi.md#null-keywords), not a name "null".
func (x Index) record(val *yaml.Node, pointer jsontext.Pointer) {
	if val == nil || val.Kind != yaml.ScalarNode || val.Value == "" || val.ShortTag() == "!!null" {
		return
	}
	x.byName[val.Value] = append(x.byName[val.Value], pointer)
}
