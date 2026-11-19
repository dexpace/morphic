// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package naming

import (
	"cmp"
	"encoding/json/jsontext"
	"slices"

	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
	"github.com/dexpace/morphic/compilers/openapi/internal/reftable"
	"github.com/dexpace/morphic/compilers/openapi/internal/statement"
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
	table *reftable.ReferenceTargetTable
	views map[reftable.DocID]*nodeview.View
	memo  map[reftable.DocumentPointer]string
	// titles reads the title a schema names its node by, and is nil where
	// titles name nothing.
	titles *titles
}

// New returns a Namer over the documents table read, naming a schema by its
// title where byTitle holds (titles.at), and by its position where it does
// not. A nil table reads none, and names each position by its pointer alone.
func New(table *reftable.ReferenceTargetTable, byTitle bool) *Namer {
	namer := &Namer{table: table, views: map[reftable.DocID]*nodeview.View{}, memo: map[reftable.DocumentPointer]string{}}
	if byTitle {
		namer.titles = &titles{table: table, read: map[*yaml.Node]string{}, lists: map[reftable.DocumentPointer]map[string]int{}}
	}
	return namer
}

// At returns the hint the node at documentPointer is named by: the hint
// compile.NamingHint takes, before it is made neutral. A component schema's own
// node is named by its key and carries no hint, but the positions beneath it
// compose theirs from it, so At answers for it too.
func (n *Namer) At(documentPointer reftable.DocumentPointer) string {
	if hint, ok := n.memo[documentPointer]; ok {
		return hint
	}
	hint := n.at(documentPointer, 0)
	n.memo[documentPointer] = hint
	return hint
}

// at is At having followed hops $refs already. It neither reads nor writes the
// memo: a chain followed from another position stops at MaxHops sooner than one
// followed from documentPointer, so a name remembered from either would make
// the other depend on which was asked first.
func (n *Namer) at(documentPointer reftable.DocumentPointer, hops int) string {
	hint, branch, _ := n.walk(documentPointer)
	if branch && !n.conjoined(documentPointer) {
		hint = n.branch(documentPointer, hint, hops)
	}
	return hint
}

// conjoined reports whether the schema at documentPointer writes a key beside
// its reference that it reads (statement.ReadBeside), which makes it the
// conjunction of the two, a node of the position's own, named by its position
// as any other (docs/compilers/openapi.md#naming); a reference alone is named
// after what it names.
func (n *Namer) conjoined(documentPointer reftable.DocumentPointer) bool {
	if n.table == nil {
		return false
	}
	referenceSite, ok := n.table.Entry(documentPointer)
	if !ok {
		return false
	}
	return slices.ContainsFunc(n.view(documentPointer.Doc).MappingPairs(n.node(documentPointer.Doc, documentPointer.Pointer)), func(kv nodeview.Pair) bool {
		return statement.ReadBeside(kv.Key, kv.Val, referenceSite.Keyword)
	})
}

// walk names documentPointer from its owner down the structure to it (steps),
// reading the title of each schema on the way (titles.at) where titles name,
// and reports whether documentPointer is a composition branch its title does
// not name, and whether its title names it.
func (n *Namer) walk(documentPointer reftable.DocumentPointer) (hint string, branch, titled bool) {
	tokens := slices.Collect(documentPointer.Pointer.Tokens())
	reading := n.read(documentPointer)
	start, owner := n.owner(documentPointer, tokens, reading.kinds)
	var title titler
	if n.titles != nil {
		title = func(consumed int, union bool) string {
			return n.titles.at(documentPointer.Doc, tokens[:start+consumed], reading, union)
		}
	}
	return steps(owner, tokens[start:], title)
}

// branch names the composition branch at documentPointer, which steps named
// positionally for want of a title: a branch holding a $ref is named after it
// (Through), keeping its positional name where the $ref suggests none.
func (n *Namer) branch(documentPointer reftable.DocumentPointer, positional string, hops int) string {
	return cmp.Or(n.through(documentPointer, hops), positional)
}

// Through returns the name the $ref written at documentPointer suggests: the
// hint of the position it lands on, else what its text suggests
// (unresolvedName). It is "" where documentPointer holds no $ref, and where a
// $ref names nothing either way, such as one landing on a component keyed "".
func (n *Namer) Through(documentPointer reftable.DocumentPointer) string {
	return n.through(documentPointer, 0)
}

// through is Through having followed hops $refs already.
func (n *Namer) through(documentPointer reftable.DocumentPointer, hops int) string {
	if n.table == nil || hops >= MaxHops {
		return ""
	}
	referenceSite, ok := n.table.Entry(documentPointer)
	switch {
	case !ok:
		return ""
	case referenceSite.Hop.Status == reftable.Resolved:
		return n.at(referenceSite.Hop.Target, hops+1)
	default:
		return unresolvedName(referenceSite.Hop.Ref)
	}
}

// node returns the node written at pointer in doc, read as the parser reads
// it, or nil where the document holds none.
func (n *Namer) node(doc reftable.DocID, pointer jsontext.Pointer) *yaml.Node {
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
func (n *Namer) view(doc reftable.DocID) *nodeview.View {
	view, ok := n.views[doc]
	if !ok {
		view = nodeview.New()
		n.views[doc] = view
	}
	return view
}
