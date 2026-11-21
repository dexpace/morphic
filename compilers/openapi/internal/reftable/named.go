// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package reftable

import (
	"context"
	"encoding/json/jsontext"
	"math"
	"slices"
	"strconv"
	"strings"

	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/budget"
	"github.com/dexpace/morphic/compilers/openapi/internal/fragment"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
	"github.com/dexpace/morphic/compilers/openapi/internal/specrules"
)

// NamedPointerBytes totals the JSON pointers, keys as written, of the nodes a
// compile of root names one by one, as docs/compilers/openapi.md#source-budgets
// says, reading schemas by the minor's grammar: its caller answers for
// dialects and for keys the Format section refuses. complete is false where
// the total cannot be taken: a reference leaves the document, names nothing,
// passes a Reference Object or is read under an `$id`; a fragment is no JSON
// pointer; merges nest past nodeview.MergeDepthLimit; or past maxSteps.
//
// Two generated tests hold it to the walks it predicts, over their grammars
// alone: TestNamedPointerBytes_TotalsWhatATablesWalksSpell the binder's,
// e2e's TestCompile_SpellsNoPointerTheNamedTotalLeavesOut a compile's.
func NamedPointerBytes(ctx context.Context, root *yaml.Node, maxSteps int) (bytes int, complete bool) {
	namedValue := named{memo: map[namedKey]namedShape{}, steps: budget.New(budget.AtMost(maxSteps)),
		view: nodeview.New(), root: nodeview.DocumentRoot(root), complete: true}
	if namedValue.root == nil {
		return 0, true
	}
	namedValue.components, namedValue.schemes = namedValue.declared("schemas"), namedValue.declared("securitySchemes")
	version, _ := scalarText(namedValue.view.ChildByToken(namedValue.root, "openapi"))
	namedValue.keysByURI = specrules.Table().SecurityRules(version).NamesByURI()
	total := namedValue.shapeOf(ctx, namedKey{n: namedValue.root, s: sDocument}).pointers
	landed := map[namedLanding]bool{}
	for i := 0; i < len(namedValue.refs) && namedValue.complete; i++ {
		target, pointer, ok := namedValue.landing(namedValue.refs[i].ref)
		at := namedLanding{pointer: pointer, s: namedValue.refs[i].s}
		if !ok || landed[at] || stateOf(sDocument, slices.Collect(pointer.Tokens())) == at.s {
			continue
		}
		landed[at] = true
		got := namedValue.shapeOf(ctx, namedKey{n: target, s: at.s})
		total = sat(total, sat(got.pointers, satMul(got.nodes, pointerBytes(pointer))))
	}
	return total, namedValue.complete && !namedValue.view.Exhausted()
}

// pointerBytes is the bytes of pointer as its tokens are written.
func pointerBytes(pointer jsontext.Pointer) int {
	n := 0
	for token := range pointer.Tokens() {
		n = sat(n, len(token)+1)
	}
	return n
}

// namedKey is a node read at a state of the grammar.
type namedKey struct {
	n *yaml.Node
	s state
}

// namedShape is what a node read at a state names: how many nodes, and how
// many bytes their pointers from the node total.
type namedShape struct{ nodes, pointers int }

// namedRef is a reference a node read at s writes, which lands in s's kind.
type namedRef struct {
	ref string
	s   state
}

// namedLanding is a position a reference lands at, read as the kind s reads.
// The binder walks each from where it lands, and an anchor can place one node
// at many, so a landing is told apart by its pointer, not by its node.
type namedLanding struct {
	pointer jsontext.Pointer
	s       state
}

// named is one NamedPointerBytes in progress: each (node, state) measured,
// and the references met on the way, read once each. components and schemes
// are the keys of the document's schema and security scheme components, which a
// discriminator's value and a Security Requirement Object's key name by key as
// the binder reads one (Config.Components, Config.SecuritySchemes), and
// keysByURI whether the minor it declares reads any other such key as a URI
// (Config.KeysByURI).
type named struct {
	memo       map[namedKey]namedShape
	steps      budget.Budget
	view       *nodeview.View
	root       *yaml.Node
	components map[string]bool
	schemes    map[string]bool
	refs       []namedRef
	keysByURI  bool
	complete   bool
}

// declared is the keys the document writes under kind in its components.
func (w *named) declared(kind string) map[string]bool {
	keys := map[string]bool{}
	for _, pair := range w.view.MappingPairs(w.view.ChildByToken(nodeview.Deref(w.view.ChildByToken(w.root, "components")), kind)) {
		keys[pair.Key] = true
	}
	return keys
}

// namedFrame is a (node, state) the walk has entered, and whether its
// children were pushed.
type namedFrame struct {
	key    namedKey
	pushed bool
}

// shapeOf measures what top names, over an explicit stack: each (node, state)
// once, an alias as what it names, a node read raw as rawNamed counts it.
func (w *named) shapeOf(ctx context.Context, top namedKey) namedShape {
	stack := []namedFrame{{key: top}}
	for len(stack) > 0 && w.complete {
		namedFrame := &stack[len(stack)-1]
		if _, done := w.memo[namedFrame.key]; done {
			stack = stack[:len(stack)-1]
			continue
		}
		if !w.steps.Spend(1) || w.steps.Spent()%nodeview.AskEvery == 0 && ctx.Err() != nil {
			w.complete = false
			break
		}
		if !namedFrame.pushed {
			namedFrame.pushed = true
			stack = append(stack, w.below(namedFrame.key)...)
			continue
		}
		w.memo[namedFrame.key] = w.measured(namedFrame.key)
		stack = stack[:len(stack)-1]
	}
	return w.memo[top]
}

// below returns the (node, state) pairs namedKeyValue's shape is made of, and
// notes the references namedKeyValue writes. A scalar read raw holds none, so a
// raw value costs a step for each mapping and list it holds.
func (w *named) below(namedKeyValue namedKey) []namedFrame {
	node := namedKeyValue.n
	if node == nil {
		return nil
	}
	w.noteRefs(namedKeyValue)
	var out []namedFrame
	w.eachNamedChild(node, namedKeyValue.s, func(child *yaml.Node, stateValue state, _ int) {
		if stateValue == sRaw && (child == nil || child.Kind == yaml.ScalarNode) {
			return
		}
		if _, done := w.memo[namedKey{n: child, s: stateValue}]; !done {
			out = append(out, namedFrame{key: namedKey{n: child, s: stateValue}})
		}
	})
	return out
}

// measured is namedKeyValue's shape once what it is made of is measured. A node
// read raw names what a compile names inside it and not itself, which the node
// holding it adds (rawNamed).
func (w *named) measured(namedKeyValue namedKey) namedShape {
	node := namedKeyValue.n
	if node == nil {
		return namedShape{}
	}
	out := namedShape{nodes: 1}
	if namedKeyValue.s == sRaw {
		out = rawNamed(node)
	}
	member := node.Kind == yaml.MappingNode && namedKeyValue.s != sRaw
	w.eachNamedChild(node, namedKeyValue.s, func(child *yaml.Node, stateValue state, edge int) {
		got := w.memo[namedKey{n: child, s: stateValue}]
		if stateValue == sRaw && namedKeyValue.s != sRaw {
			// A raw value's root, which a compile names alone.
			got.nodes = sat(got.nodes, 1)
		}
		if member {
			// The member's key, a node at its value's pointer.
			got.nodes = sat(got.nodes, 1)
		}
		out.nodes = sat(out.nodes, got.nodes)
		out.pointers = sat(out.pointers, sat(got.pointers, satMul(got.nodes, edge)))
	})
	return out
}

// rawNamed is what a compile names at node itself, a node inside a value the
// grammar keeps raw: each key a mapping writes more than once, where the rule
// table places a finding at that key's pointer (nodeview.RepeatedKeys, as
// specrules' check of a raw value asks), and nothing else.
func rawNamed(node *yaml.Node) namedShape {
	var out namedShape
	if node.Kind != yaml.MappingNode {
		return out
	}
	for _, key := range nodeview.RepeatedKeys(node) {
		out.nodes = sat(out.nodes, 1)
		out.pointers = sat(out.pointers, len(key)+1)
	}
	return out
}

// eachNamedChild hands visit each child of node read at stateValue, what an
// alias names in its place, with the state it is read at and the bytes its step
// adds to a pointer: a key or an index, and a '/'. A mapping's are its members'
// values as the binder reads them (nodeview.View.MappingPairs): those a `<<`
// merge key brings in among its own, which a compile names at the mapping, and
// a key written twice at its last copy.
func (w *named) eachNamedChild(node *yaml.Node, stateValue state, visit func(child *yaml.Node, otherState state, edge int)) {
	switch node.Kind {
	case yaml.MappingNode:
		for _, pair := range w.view.MappingPairs(node) {
			visit(pair.Val, step(stateValue, pair.Key), len(pair.Key)+1)
		}
	case yaml.SequenceNode:
		for i, item := range node.Content {
			token := strconv.Itoa(i)
			visit(nodeview.Deref(item), step(stateValue, token), len(token)+1)
		}
	default:
	}
}

// noteRefs notes what the mapping namedKeyValue reads as a reference: its $ref
// where its state reads one, and for a schema its $dynamicRef, which lands as a
// $ref does or on the anchor its plain name expands to, and each value its
// discriminator names a schema by (mappingValues), by its text whatever its
// tag, that spells no schema component's key, which names that component where
// it is declared. A Security Requirement Object's are its keys (noteKeys).
func (w *named) noteRefs(namedKeyValue namedKey) {
	if namedKeyValue.n.Kind != yaml.MappingNode {
		return
	}
	if namedKeyValue.s == sSecurityRequirement {
		w.noteKeys(namedKeyValue.n)
		return
	}
	_, object := refKinds[namedKeyValue.s]
	if namedKeyValue.s != sSchema && !object {
		return
	}
	if ref, ok := scalarText(w.view.ChildByToken(namedKeyValue.n, "$ref")); ok {
		w.refs = append(w.refs, namedRef{ref: ref, s: namedKeyValue.s})
	}
	if namedKeyValue.s != sSchema {
		return
	}
	if ref, ok := scalarText(w.view.ChildByToken(namedKeyValue.n, "$dynamicRef")); ok {
		w.refs = append(w.refs, namedRef{ref: ref, s: sSchema})
	}
	if w.view.ChildByToken(namedKeyValue.n, "$id") != nil {
		w.complete = false
		return
	}
	for _, node := range mappingValues(w.view, namedKeyValue.n) {
		if !w.components[node.Value] {
			w.refs = append(w.refs, namedRef{ref: node.Value, s: sSchema})
		}
	}
}

// noteKeys notes each key of the Security Requirement Object node that names no
// security scheme component as a reference to a Security Scheme Object, the
// key its URI, where the document's minor reads a key so, as the binder reads
// one (finder.requirementKeys).
func (w *named) noteKeys(node *yaml.Node) {
	for _, pair := range w.view.MappingPairs(node) {
		if w.keysByURI && !w.schemes[pair.Key] {
			w.refs = append(w.refs, namedRef{ref: pair.Key, s: sSecurityScheme})
		}
	}
}

// landing returns the node ref names in the document and its pointer, its
// fragment read as the binder reads one (fragment.Pointer). It is false, and
// the total incomplete, for a reference leaving the document, whose fragment
// is no JSON pointer, or whose pointer names nothing or passes a Reference
// Object, either of which the binder answers by a rule of its own.
func (w *named) landing(ref string) (*yaml.Node, jsontext.Pointer, bool) {
	pointer := fragment.Pointer(ref)
	if !strings.HasPrefix(ref, "#") || pointer != "" && !strings.HasPrefix(string(pointer), "/") || !pointer.IsValid() {
		w.complete = false
		return nil, "", false
	}
	path, found := w.view.DocumentPath(w.root, pointer)
	if !found || len(path) == 0 || w.passesReference(path, pointer) {
		w.complete = false
		return nil, "", false
	}
	return path[len(path)-1], pointer, true
}

// passesReference reports whether pointer, read from the document's root
// along path, passes a mapping the grammar reads as a Reference Object that
// writes a $ref. The binder reads on in the object that reference names
// (binder.through), not in what the mapping writes beside its $ref. A path
// item's own fields, which the binder reads in place, are not told apart.
func (w *named) passesReference(path []*yaml.Node, pointer jsontext.Pointer) bool {
	state, i := sDocument, 0
	for token := range pointer.Tokens() {
		_, object := refKinds[state]
		if _, refs := scalarText(w.view.ChildByToken(path[i], "$ref")); object && refs {
			return true
		}
		state, i = step(state, token), i+1
	}
	return false
}

// scalarText is the text of node, its alias followed, when it is a scalar that
// is no null.
func scalarText(node *yaml.Node) (string, bool) {
	node = nodeview.Deref(node)
	if node == nil || node.Kind != yaml.ScalarNode || node.ShortTag() == "!!null" {
		return "", false
	}
	return node.Value, true
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
