package bind

import (
	"cmp"
	"encoding/json/jsontext"
	"slices"
	"strconv"

	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
)

// Table is every schema reference site of a document and where its chain
// ends, ordered by site. It is built once and never written after.
type Table struct {
	entries []Entry
	steps   int
	cut     bool
}

// Entries returns the table's entries, ordered by site.
func (t *Table) Entries() []Entry { return slices.Clone(t.entries) }

// Steps reports the work the table cost.
func (t *Table) Steps() int { return t.steps }

// Incomplete reports whether the budget ran out, so that a site may be missing
// or read OverBudget.
func (t *Table) Incomplete() bool { return t.cut }

// Bind reads every schema reference site b's document holds and resolves
// each, within b's budget.
func Bind(b *Binder) *Table {
	t := &Table{}
	for _, site := range b.sites() {
		t.entries = append(t.entries, b.Resolve(site))
	}
	t.steps, t.cut = b.steps, b.steps > b.cfg.Limit
	slices.SortFunc(t.entries, func(x, y Entry) int { return cmp.Compare(x.Site, y.Site) })
	return t
}

// frame is one position the site walk has to read.
type frame struct {
	node    *yaml.Node
	pointer jsontext.Pointer
	state   state
}

// sites is every schema position that writes a $ref, in document order.
func (b *Binder) sites() []jsontext.Pointer {
	var out []jsontext.Pointer
	b.eachSchema(func(at jsontext.Pointer, n *yaml.Node) {
		if _, ok := b.refOf(n); ok {
			out = append(out, at)
		}
	})
	return out
}

// eachSchema walks the document by its grammar and calls f with every schema
// position, in document order, each pointer once. A node an alias places at
// several positions is read at each, as the parser reads it.
func (b *Binder) eachSchema(f func(at jsontext.Pointer, n *yaml.Node)) {
	stack := []frame{{node: b.root, state: sDocument}}
	for len(stack) > 0 && b.spend(1) {
		top := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		n := nodeview.Deref(top.node)
		if n == nil || top.state == sRaw || top.state == sValue {
			continue
		}
		if top.state == sSchema {
			f(top.pointer, n)
		}
		stack = append(stack, b.children(n, top)...)
	}
}

// children are the positions directly below f, in reverse so the first is
// read first.
func (b *Binder) children(n *yaml.Node, f frame) []frame {
	var out []frame
	switch n.Kind {
	case yaml.MappingNode:
		for _, p := range b.view.MappingPairs(n) {
			out = append(out, frame{node: p.Val, pointer: f.pointer.AppendToken(p.Key), state: step(f.state, p.Key)})
		}
	case yaml.SequenceNode:
		for i, c := range n.Content {
			token := strconv.Itoa(i)
			out = append(out, frame{node: c, pointer: f.pointer.AppendToken(token), state: step(f.state, token)})
		}
	}
	slices.Reverse(out)
	return out
}
