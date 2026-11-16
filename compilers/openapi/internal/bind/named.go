// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package bind

import (
	"context"
	"math"
	"strconv"
	"strings"

	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/budget"
	"github.com/dexpace/morphic/compilers/openapi/internal/fragment"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
)

// NamedPointerBytes totals the JSON pointers, keys as written, of the nodes a
// compile of root names one by one (docs/compilers/openapi.md#source-budgets):
// a value the grammar keeps raw, or no model holds, adds its root alone, and a
// raw value a reference lands in is named as the kind it expects, once. It
// reads schemas by the minor's grammar; its caller answers for a document
// naming a dialect. complete is false where the total cannot be taken: a
// reference leaves the document, names nothing its pointer finds, or is read
// under an `$id`; a fragment is no JSON pointer; or past maxSteps.
func NamedPointerBytes(ctx context.Context, root *yaml.Node, maxSteps int) (bytes int, complete bool) {
	w := named{memo: map[namedKey]namedShape{}, steps: budget.New(budget.AtMost(maxSteps)),
		view: nodeview.New(), root: nodeview.DocumentRoot(root), complete: true}
	if w.root == nil {
		return 0, true
	}
	total := w.shapeOf(ctx, namedKey{n: w.root, p: place{state: sDocument}}).pointers
	for i := 0; i < len(w.refs) && w.complete; i++ {
		target, base, ok := w.landing(w.refs[i].ref)
		key := namedKey{n: target, p: place{state: w.refs[i].s}}
		if _, named := w.memo[key]; !ok || named {
			continue
		}
		got := w.shapeOf(ctx, key)
		total = sat(total, sat(got.pointers, satMul(got.nodes, base)))
	}
	return total, w.complete
}

// namedKey is a node read at a place in the grammar.
type namedKey struct {
	n *yaml.Node
	p place
}

// namedShape is what a node read at a place names: how many nodes, and how
// many bytes their pointers from the node total.
type namedShape struct{ nodes, pointers int }

// namedRef is a reference a node read at s writes, which lands in s's kind.
type namedRef struct {
	ref string
	s   state
}

// named is one NamedPointerBytes in progress: each (node, state) measured,
// and the references met on the way, read once each.
type named struct {
	memo     map[namedKey]namedShape
	steps    budget.Budget
	view     *nodeview.View
	root     *yaml.Node
	refs     []namedRef
	complete bool
}

// namedFrame is a (node, state) the walk has entered, and whether its
// children were pushed.
type namedFrame struct {
	key    namedKey
	pushed bool
}

// shapeOf measures what top names, over an explicit stack: each (node, state)
// once, an alias as what it names, a raw value as its root alone.
func (w *named) shapeOf(ctx context.Context, top namedKey) namedShape {
	stack := []namedFrame{{key: top}}
	for len(stack) > 0 && w.complete {
		f := &stack[len(stack)-1]
		if _, done := w.memo[f.key]; done {
			stack = stack[:len(stack)-1]
			continue
		}
		if !w.steps.Spend(1) || w.steps.Spent()%nodeview.AskEvery == 0 && ctx.Err() != nil {
			w.complete = false
			break
		}
		if !f.pushed {
			f.pushed = true
			stack = append(stack, w.below(f.key)...)
			continue
		}
		w.memo[f.key] = w.measured(f.key)
		stack = stack[:len(stack)-1]
	}
	return w.memo[top]
}

// below returns the (node, state) pairs k's shape is made of, and notes the
// references k writes.
func (w *named) below(k namedKey) []namedFrame {
	n := k.n
	if k.p.state == sRaw || n == nil {
		return nil
	}
	if n.Kind == yaml.AliasNode {
		if n.Alias == nil {
			return nil
		}
		return []namedFrame{{key: namedKey{n: n.Alias, p: k.p}}}
	}
	w.noteRefs(k)
	var out []namedFrame
	eachNamedChild(n, k.p, func(c *yaml.Node, p place, _ int) {
		if _, done := w.memo[namedKey{n: c, p: p}]; !done {
			out = append(out, namedFrame{key: namedKey{n: c, p: p}})
		}
	})
	return out
}

// measured is k's shape once what it is made of is measured.
func (w *named) measured(k namedKey) namedShape {
	n := k.n
	if n == nil || k.p.beneath {
		return namedShape{}
	}
	if k.p.state == sRaw {
		return namedShape{nodes: 1}
	}
	if n.Kind == yaml.AliasNode {
		return w.memo[namedKey{n: n.Alias, p: k.p}]
	}
	out := namedShape{nodes: 1}
	eachNamedChild(n, k.p, func(c *yaml.Node, p place, edge int) {
		got := w.memo[namedKey{n: c, p: p}]
		if p.beneath {
			// The field no model holds is kept verbatim: its root alone.
			got = namedShape{nodes: 1}
		}
		out.nodes = sat(out.nodes, got.nodes)
		out.pointers = sat(out.pointers, sat(got.pointers, satMul(got.nodes, edge)))
	})
	return out
}

// eachNamedChild hands visit each child of n read at p, keys included, with
// the place it is read at and the bytes its step adds to a pointer: a key or
// an index, and a '/'. A key holds nothing, so it is read as a value, and is
// counted at its member's pointer as the grammar-free total counts it.
func eachNamedChild(n *yaml.Node, p place, visit func(c *yaml.Node, p place, edge int)) {
	switch n.Kind {
	case yaml.MappingNode:
		for i := 0; i+1 < len(n.Content); i += 2 {
			token := ""
			if key := nodeview.Deref(n.Content[i]); key != nil {
				token = key.Value
			}
			visit(n.Content[i], place{state: sValue, beneath: p.beneath}, len(token)+1)
			visit(n.Content[i+1], p.below(token), len(token)+1)
		}
	case yaml.SequenceNode:
		for i, item := range n.Content {
			token := strconv.Itoa(i)
			visit(item, p.below(token), len(token)+1)
		}
	default:
	}
}

// noteRefs notes what the mapping k reads as a reference: its $ref where its
// state reads one, and for a schema its $dynamicRef, which lands as a $ref
// does or on the anchor its plain name expands to, and each discriminator
// mapping value.
func (w *named) noteRefs(k namedKey) {
	if k.n.Kind != yaml.MappingNode {
		return
	}
	_, object := refKinds[k.p.state]
	if k.p.state != sSchema && !object {
		return
	}
	if ref, ok := scalarText(w.view.ChildByToken(k.n, "$ref")); ok {
		w.refs = append(w.refs, namedRef{ref: ref, s: k.p.state})
	}
	if k.p.state != sSchema {
		return
	}
	if ref, ok := scalarText(w.view.ChildByToken(k.n, "$dynamicRef")); ok {
		w.refs = append(w.refs, namedRef{ref: ref, s: sSchema})
	}
	if w.view.ChildByToken(k.n, "$id") != nil {
		w.complete = false
		return
	}
	mapping := nodeview.Deref(w.view.ChildByToken(nodeview.Deref(w.view.ChildByToken(k.n, "discriminator")), "mapping"))
	for _, p := range w.view.MappingPairs(mapping) {
		if ref, ok := scalarText(p.Val); ok {
			w.refs = append(w.refs, namedRef{ref: ref, s: sSchema})
		}
	}
}

// landing returns the node ref names in the document, its fragment read as the
// binder reads one (fragment.Pointer), and the bytes of its pointer. It is
// false, and the total incomplete, for a reference leaving the document,
// whose fragment is no JSON pointer, or naming nothing the pointer finds,
// which the binder may still answer by a rule of its own.
func (w *named) landing(ref string) (*yaml.Node, int, bool) {
	pointer := fragment.Pointer(ref)
	if !strings.HasPrefix(ref, "#") || pointer != "" && !strings.HasPrefix(string(pointer), "/") || !pointer.IsValid() {
		w.complete = false
		return nil, 0, false
	}
	path, found := w.view.DocumentPath(w.root, pointer)
	if !found || len(path) == 0 {
		w.complete = false
		return nil, 0, false
	}
	base := 0
	for token := range pointer.Tokens() {
		base = sat(base, len(token)+1)
	}
	return path[len(path)-1], base, true
}

// scalarText is the text of n, its alias followed, when it is a scalar that
// is no null.
func scalarText(n *yaml.Node) (string, bool) {
	n = nodeview.Deref(n)
	if n == nil || n.Kind != yaml.ScalarNode || n.ShortTag() == "!!null" {
		return "", false
	}
	return n.Value, true
}

// sat is a+b for counts that are not negative, held at math.MaxInt.
func sat(a, b int) int {
	if a > math.MaxInt-b {
		return math.MaxInt
	}
	return a + b
}

// satMul is a*b for counts that are not negative, held at math.MaxInt.
func satMul(a, b int) int {
	if a != 0 && b > math.MaxInt/a {
		return math.MaxInt
	}
	return a * b
}
