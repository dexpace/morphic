// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package naming

import (
	"cmp"
	"encoding/json/jsontext"
	"slices"

	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/bind"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
)

// MaxHops bounds how many $refs one name follows through composition
// branches that each hold one: a loop of them, and any longer chain, stops
// there, keeping the name it has reached. An acyclic chain that long is legal,
// though no real document writes one.
const MaxHops = 64

// Namer names the nodes built at the positions of the documents a table read.
// It remembers each name it gives, and a name is a function of the documents
// alone, so the memo answers as a fresh Namer would. A Namer must not outlive
// the trees its table holds, and is not safe for concurrent use.
type Namer struct {
	table  *bind.Table
	views  map[bind.DocID]*nodeview.View
	memo   map[bind.Position]string
	titles titles
}

// New returns a Namer over the documents table read. A nil table reads none,
// and names each position by its pointer alone.
func New(table *bind.Table) *Namer {
	return &Namer{table: table, views: map[bind.DocID]*nodeview.View{}, memo: map[bind.Position]string{},
		titles: titles{table: table, read: map[*yaml.Node]string{}, lists: map[bind.Position]map[string]int{}}}
}

// At returns the hint the node at p is named by: the hint compile.NamingHint
// takes, before it is made neutral. A component schema's own node is named by
// its key and carries no hint, but the positions beneath it compose theirs
// from it, so At answers for it too.
func (n *Namer) At(p bind.Position) string {
	if hint, ok := n.memo[p]; ok {
		return hint
	}
	hint := n.at(p, 0)
	n.memo[p] = hint
	return hint
}

// at is At having followed hops $refs already. It neither reads nor writes
// the memo: a chain followed from another position stops at MaxHops sooner
// than one followed from p, so a name remembered from either would make the
// other depend on which was asked first.
func (n *Namer) at(p bind.Position, hops int) string {
	hint, branch, _ := n.walk(p)
	if branch {
		hint = n.branch(p, hint, hops)
	}
	return hint
}

// walk names p from its owner down the structure to it (steps), reading the
// title of each schema on the way (titles.at), and reports whether p is a
// composition branch its title does not name, and whether its title names it.
func (n *Namer) walk(p bind.Position) (hint string, branch, titled bool) {
	tokens := slices.Collect(p.Pointer.Tokens())
	r := n.read(p)
	start, owner := n.owner(p, tokens, r.kinds)
	return steps(owner, tokens[start:], func(consumed int, union bool) string {
		return n.titles.at(p.Doc, tokens[:start+consumed], r, union)
	})
}

// branch names the composition branch at p, which steps named positionally
// for want of a title: a branch holding a $ref is named after it (Through),
// keeping its positional name where the $ref suggests none.
func (n *Namer) branch(p bind.Position, positional string, hops int) string {
	return cmp.Or(n.through(p, hops), positional)
}

// Through returns the name the $ref written at p suggests: the hint of the
// position it lands on, else what its text suggests (unresolvedName). It is ""
// where p holds no $ref, and where a $ref names nothing either way, such as
// one landing on a component keyed "".
func (n *Namer) Through(p bind.Position) string {
	return n.through(p, 0)
}

// through is Through having followed hops $refs already.
func (n *Namer) through(p bind.Position, hops int) string {
	if n.table == nil || hops >= MaxHops {
		return ""
	}
	e, ok := n.table.Entry(p)
	switch {
	case !ok:
		return ""
	case e.Hop.Status == bind.Resolved:
		return n.at(e.Hop.Target, hops+1)
	default:
		return unresolvedName(e.Hop.Ref)
	}
}

// node returns the node written at pointer in doc, read as the parser reads
// it, or nil where the document holds none.
func (n *Namer) node(doc bind.DocID, pointer jsontext.Pointer) *yaml.Node {
	if n.table == nil || doc < 0 || int(doc) >= n.table.Documents() {
		return nil
	}
	path, complete := n.view(doc).DocumentPath(n.table.Document(doc).Root, pointer)
	if !complete {
		return nil
	}
	return path[len(path)-1]
}

// view returns the view the document doc is read through, one per document.
func (n *Namer) view(doc bind.DocID) *nodeview.View {
	v, ok := n.views[doc]
	if !ok {
		v = nodeview.New()
		n.views[doc] = v
	}
	return v
}
