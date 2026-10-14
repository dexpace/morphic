package load

import (
	"strconv"

	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
)

// budget counts the work one check may do before it gives up. The check reads
// it after each phase, so a spend past the limit is never silent.
type budget struct{ spent, limit int }

// spend charges n units and reports whether the budget still holds.
func (b *budget) spend(n int) bool {
	b.spent += n
	return b.spent <= b.limit
}

// exhausted reports whether any spend has crossed the limit.
func (b *budget) exhausted() bool { return b.spent > b.limit }

// posSet is a set of nodes one lookup lands on. Every reference class making
// that lookup shares it, so its members are read once however many classes
// reach it. next holds the set one token below, built on the first step through
// it, so pointers that share a prefix walk the prefix once.
type posSet struct {
	members []*yaml.Node
	next    map[string]*posSet
}

// addTo puts n in the set m holds under k, creating the set on first use.
func addTo[K comparable](m map[K]*posSet, k K, n *yaml.Node) {
	s := m[k]
	if s == nil {
		s = &posSet{}
		m[k] = s
	}
	s.members = append(s.members, n)
}

// edge is one effective child under the key that reaches it, so a set built
// from several parents holds a child an alias shares between them once.
type edge struct {
	key  string
	node *yaml.Node
}

// tree is the document's node tree read as the library's unmarshaller reads
// it: through a nodeview.View, so a merge key or an alias supplies its pairs
// where the resolver finds them (GitHub #26). Reading raw Content instead
// misses every key a merge supplies.
type tree struct {
	view *nodeview.View
	root *yaml.Node
	// anywhere is every node as one set, the start of a pointer that can be
	// resolved against any node. index fills its next, keyed by the key or
	// index that reaches each child.
	anywhere *posSet
	// parents holds every node a child is an effective child of: more than one
	// when an alias or merge key shares it.
	parents map[*yaml.Node][]*yaml.Node
	budget  *budget
}

func newTree(root *yaml.Node, b *budget) *tree {
	return &tree{
		view:     nodeview.New(),
		root:     nodeview.Deref(nodeview.DocumentRoot(root)),
		anywhere: &posSet{next: map[string]*posSet{}},
		parents:  map[*yaml.Node][]*yaml.Node{},
		budget:   b,
	}
}

// pairs is the effective children of a mapping or sequence, keyed by name or
// index; any other node has none.
func (t *tree) pairs(n *yaml.Node) []nodeview.Pair {
	n = nodeview.Deref(n)
	if n == nil {
		return nil
	}
	switch n.Kind {
	case yaml.MappingNode:
		return t.view.MappingPairs(n)
	case yaml.SequenceNode:
		out := make([]nodeview.Pair, len(n.Content))
		for i, v := range n.Content {
			out[i] = nodeview.Pair{Key: strconv.Itoa(i), Val: v}
		}
		return out
	default:
		return nil
	}
}

// child is the effective child of n under token, or nil. A sequence index is
// read as the library's pointer walk reads it, in decimal with no leading zero,
// which is the key pairs gives the element; nodeview also takes "00" or "+0".
func (t *tree) child(n *yaml.Node, token string) *yaml.Node {
	n = nodeview.Deref(n)
	if n == nil || n.Kind != yaml.SequenceNode {
		return nodeview.Deref(t.view.ChildByToken(n, token))
	}
	i, err := strconv.Atoi(token)
	if err != nil || strconv.Itoa(i) != token || i < 0 || i >= len(n.Content) {
		return nil
	}
	return nodeview.Deref(n.Content[i])
}

// walk follows tokens from n, or returns nil where one is missing.
func (t *tree) walk(n *yaml.Node, tokens []string) *yaml.Node {
	for _, token := range tokens {
		if n = t.child(n, token); n == nil || !t.budget.spend(1) {
			return nil
		}
	}
	return n
}

// refValue is the $ref string n carries, or "" when it has none or the value
// is not a scalar.
func (t *tree) refValue(n *yaml.Node) string {
	v := t.child(n, "$ref")
	if v == nil || v.Kind != yaml.ScalarNode {
		return ""
	}
	return v.Value
}

// index fills anywhere and parents with one walk that enters each node once and
// never follows an alias, and returns the mappings declaring $anchor or $id and
// those whose $ref the resolver reads as a /$defs/ pointer.
func (t *tree) index() (declared, defsRefs []*yaml.Node) {
	seen := map[edge]bool{}
	stack := []*yaml.Node{t.root}
	for len(stack) > 0 && t.budget.spend(1) {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if n == nil || n.Kind == yaml.AliasNode {
			continue
		}
		pairs := t.pairs(n)
		t.budget.spend(len(pairs))
		for _, p := range pairs {
			v := nodeview.Deref(p.Val)
			if e := (edge{p.Key, v}); !seen[e] {
				seen[e] = true
				addTo(t.anywhere.next, p.Key, v)
			}
			t.parents[v] = append(t.parents[v], n)
		}
		if t.declares(n) {
			declared = append(declared, n)
		}
		if isDefsRef(t.refValue(n)) {
			defsRefs = append(defsRefs, n)
		}
		stack = append(stack, n.Content...)
	}
	return declared, defsRefs
}

// declares reports whether mapping n has a $anchor or $id key, whether it
// writes one or a merge key supplies it.
func (t *tree) declares(n *yaml.Node) bool {
	return t.child(n, "$anchor") != nil || t.child(n, "$id") != nil
}

// navigate follows tokens from every member of s at once, or returns nil where
// one names nothing.
func (t *tree) navigate(s *posSet, tokens []string) *posSet {
	for _, token := range tokens {
		if s == nil || !t.budget.spend(1) {
			return nil
		}
		if s.next == nil {
			s.next = t.below(s.members)
		}
		s = s.next[token]
	}
	return s
}

// below indexes the effective children of members by the key that reaches
// each. It is built once per set, so a document of many $defs is read once per
// prefix rather than once per pointer through it.
func (t *tree) below(members []*yaml.Node) map[string]*posSet {
	next := map[string]*posSet{}
	seen := map[edge]bool{}
	for _, m := range members {
		pairs := t.pairs(m)
		if !t.budget.spend(1 + len(pairs)) {
			break
		}
		for _, p := range pairs {
			v := nodeview.Deref(p.Val)
			if e := (edge{p.Key, v}); !seen[e] {
				seen[e] = true
				addTo(next, p.Key, v)
			}
		}
	}
	return next
}
