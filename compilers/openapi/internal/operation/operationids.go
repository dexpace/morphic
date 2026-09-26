package operation

import (
	"cmp"
	"encoding/json/jsontext"
	"slices"

	soa "github.com/speakeasy-api/openapi/openapi"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/ir"
)

// operationIDClaims collects the operationId each lowered operation claims, so
// that uniqueness is judged once the whole service is lowered rather than as
// each claim arrives. Judging on arrival reported whichever claim was lowered
// second, so where the finding landed depended on declaration order.
//
// It owns the rule. The loader drops the parser validator's own check, which
// sees only some of these operations and reads an alias's reuse as a repeat
// (load's compilerOwned, GitHub #502).
type operationIDClaims struct {
	names  []string // each operationId, in the order it was first claimed
	claims map[string][]operationIDClaim
}

// operationIDClaim is one operation claiming an operationId: the node declaring
// it and the pointers the operation was lowered with. The node and the
// declaration pointer are the two ways a claim names the declaration it mounts.
type operationIDClaim struct {
	node *yaml.Node
	ptrs opPointers
}

func newOperationIDClaims() *operationIDClaims {
	return &operationIDClaims{claims: map[string][]operationIDClaim{}}
}

// add records the claim src makes at ptrs. An operation with no operationId
// claims nothing: emitters synthesize its name from the method and path.
func (o *operationIDClaims) add(src *soa.Operation, ptrs opPointers) {
	name := src.GetOperationID()
	if name == "" {
		return
	}
	if _, seen := o.claims[name]; !seen {
		o.names = append(o.names, name)
	}
	o.claims[name] = append(o.claims[name], operationIDClaim{node: declaringNode(src.GetRootNode()), ptrs: ptrs})
}

// report judges every operationId claimed more than once, in first-claim order.
// The document's text is read only once an id is claimed twice, which most
// documents never do.
func (o *operationIDClaims) report(c lowering.Ctx) []ir.Diagnostic {
	var diags []ir.Diagnostic
	var written *writtenTree
	for _, name := range o.names {
		claims := o.claims[name]
		if len(claims) < 2 {
			continue
		}
		if written == nil {
			written = newWrittenTree(c.Doc.GetRootNode())
		}
		diags = append(diags, judge(c, name, declarations(claims, written))...)
	}
	return diags
}

// declaration is what one declaration's claims say about it: at, the pointer it
// is written at, and mounts, every pointer it is mounted at. The mount it is
// written at, if it has one, comes first, and the rest follow in pointer order.
type declaration struct {
	at     jsontext.Pointer
	mounts []jsontext.Pointer
}

// judge reports one operationId's declarations, ordered as declarations orders
// them, so every finding lands in the same place whatever order the document
// declares them in.
//
// A second declaration writing the id is an error where it is written. The
// document itself repeats the id there, which OpenAPI forbids among all the
// operations it describes, webhooks and callbacks included.
//
// A declaration mounted more than once is a warning at each mount but its first:
// a path item or an operation that a $ref, a YAML alias or a merge key reuses.
// The document writes the id once, but each mount is an operation of its own,
// and an emitter renders them all under one identifier. A declaration mounted
// where it is written is spared there, so the warning lands where a reuse is.
func judge(c lowering.Ctx, name string, decls []declaration) []ir.Diagnostic {
	var diags []ir.Diagnostic
	for i, d := range decls {
		if i > 0 {
			diags = append(diags, c.DiagAt(ir.SeverityError, diag.ConflictingOperationID, d.at,
				"operationId %q is also used by the operation declared at %s; "+
					"OpenAPI requires it to be unique across the API", name, decls[0].at))
		}
		for _, again := range d.mounts[1:] {
			diags = append(diags, c.DiagAt(ir.SeverityWarning, diag.DuplicateOperationID, again,
				"operationId %q is also used by the operation at %s: both mount one declaration, "+
					"and OpenAPI requires the id to be unique across the API", name, d.mounts[0]))
		}
	}
	return diags
}

// declarations sorts claims into the declarations they mount, ordered by the
// pointer each is written at: a property of the document rather than of the
// order it was lowered in.
func declarations(claims []operationIDClaim, written *writtenTree) []declaration {
	groups := byDeclaration(claims)
	decls := make([]declaration, 0, len(groups))
	for _, group := range groups {
		decls = append(decls, declare(group, written))
	}
	slices.SortFunc(decls, func(a, b declaration) int { return cmp.Compare(a.at, b.at) })
	return decls
}

// byDeclaration partitions claims by the declaration they mount. Two claims
// mount one when they share the node declaring it, which is how a $ref, a YAML
// alias and a merge key reuse a declaration, or when they resolve to one
// declaration pointer. The second is how a reference naming this document by
// its file name reuses one: the compiler reads it as internal, but the resolver
// parses the document again to follow it, so the node it yields is a copy. A
// self-reference spelled through a directory, like ./spec.yaml, is read as
// another document instead, here as everywhere else (GitHub #576), so it
// shares neither and reads as a conflict.
//
// Neither test can turn a conflict into a reuse: one node is one declaration,
// and so is one declaration pointer, however it was reached. A claim with no
// node is grouped by its declaration pointer alone.
func byDeclaration(claims []operationIDClaim) [][]operationIDClaim {
	sets := newDisjointSets(len(claims))
	byNode := make(map[*yaml.Node]int, len(claims))
	byDecl := make(map[jsontext.Pointer]int, len(claims))
	for i, cl := range claims {
		if cl.node != nil {
			joinOn(sets, byNode, cl.node, i)
		}
		joinOn(sets, byDecl, cl.ptrs.decl, i)
	}
	groups := make([][]operationIDClaim, 0, len(claims))
	for _, members := range sets.members {
		if len(members) == 0 {
			continue
		}
		group := make([]operationIDClaim, 0, len(members))
		for _, i := range members {
			group = append(group, claims[i])
		}
		groups = append(groups, group)
	}
	return groups
}

// declare reads one declaration's pointers off its claims. A claim is written
// at its declaration pointer when that pointer, walked through the document's
// own text, ends on the claim's node, and a claim also mounted there is the
// declaration's own mount. An alias site or a merge is never where a
// declaration is written, and neither is a reference into another document; a
// declaration no claim is written at is taken to be at the least of its claims'
// declaration pointers.
func declare(group []operationIDClaim, written *writtenTree) declaration {
	var writtenAt, own, reused []jsontext.Pointer
	for _, cl := range group {
		switch {
		case !written.holds(cl.ptrs.decl, cl.node):
			reused = append(reused, cl.ptrs.mount)
		case cl.ptrs.mount == cl.ptrs.decl:
			writtenAt = append(writtenAt, cl.ptrs.decl)
			own = append(own, cl.ptrs.mount)
		default:
			writtenAt = append(writtenAt, cl.ptrs.decl)
			reused = append(reused, cl.ptrs.mount)
		}
	}
	at := slices.MinFunc(group, func(a, b operationIDClaim) int { return cmp.Compare(a.ptrs.decl, b.ptrs.decl) }).ptrs.decl
	if len(writtenAt) > 0 {
		at = slices.Min(writtenAt)
	}
	slices.Sort(own)
	slices.Sort(reused)
	return declaration{at: at, mounts: append(own, reused...)}
}

// declaringNode is the node that declares an operation. One reached through a
// YAML alias is built from the alias node itself, so the alias is followed to
// the node it names: that is what makes an alias's reuse read as the $ref
// form's does, one declaration mounted twice. One hop is all there is, since an
// alias carries no anchor for another alias to name: yaml.v3 refuses `&b *a`.
func declaringNode(n *yaml.Node) *yaml.Node {
	if n != nil && n.Kind == yaml.AliasNode && n.Alias != nil {
		return n.Alias
	}
	return n
}

// writtenTree reads where the document's own text writes a node. Each step
// reads only the pairs a mapping writes itself, following no $ref, alias or
// merge key, so a walk ends on a node only at the pointer its text is at.
//
// A mapping's pairs are indexed the first time a walk passes through it. A walk
// to a declaration under paths crosses the mapping holding every path, and
// reading that pair by pair on each walk would cost the square of their number.
type writtenTree struct {
	root     *yaml.Node
	children map[*yaml.Node]map[string]*yaml.Node
}

func newWrittenTree(root *yaml.Node) *writtenTree {
	return &writtenTree{root: root, children: map[*yaml.Node]map[string]*yaml.Node{}}
}

// holds reports whether the document's text writes n at pointer.
func (w *writtenTree) holds(pointer jsontext.Pointer, n *yaml.Node) bool {
	at := w.root
	for token := range pointer.Tokens() {
		children, indexed := w.children[at]
		if !indexed {
			children = annotation.RawChildNodes(at)
			w.children[at] = children
		}
		if at = children[token]; at == nil {
			return false
		}
	}
	return at == n
}

// disjointSets partitions items 0..n-1 into sets that union merges.
type disjointSets struct {
	set     []int   // item → the set holding it
	members [][]int // set → its items; empty once merged into another
}

func newDisjointSets(n int) *disjointSets {
	d := &disjointSets{set: make([]int, n), members: make([][]int, n)}
	for i := range n {
		d.set[i], d.members[i] = i, []int{i}
	}
	return d
}

// union merges the sets holding items a and b. The smaller set's items move, so
// no item moves more than log n times.
func (d *disjointSets) union(a, b int) {
	into, from := d.set[a], d.set[b]
	if into == from {
		return
	}
	if len(d.members[into]) < len(d.members[from]) {
		into, from = from, into
	}
	for _, item := range d.members[from] {
		d.set[item] = into
	}
	d.members[into] = append(d.members[into], d.members[from]...)
	d.members[from] = nil
}

// joinOn puts item i in the set of the first item seen with key, or records i
// as that item when there is none yet.
func joinOn[K comparable](sets *disjointSets, seen map[K]int, key K, i int) {
	if first, ok := seen[key]; ok {
		sets.union(first, i)
		return
	}
	seen[key] = i
}
