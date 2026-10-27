package ir

import "slices"

// serviceTree is where an [Editor] finds the services, groups and operations of
// the document it edits, and which of the slices holding them it has copied.
//
// It is read off the document when first needed, as [ForEachOperation] walks:
// nothing past [MaxGroupDepth], and a group slice inside itself entered once.
// A node records its position, so a write finds it without a search and copies
// only the slices on the path to it. A removal marks its node, and a slice
// holding one is compacted once, when the edit settles, so emptying a group
// costs its size, not its size squared.
type serviceTree struct {
	read bool
	// cut records that groups nested past the cap went unread.
	cut bool
	// owned records that the edit's Document.Services is its own copy.
	owned    bool
	services []*treeNode
	// byID holds the nodes read at each ID of each class, keyed by the ID's
	// string so that no field here is an ID position.
	byID map[treeKey][]*treeNode
	// sparse holds the nodes some of whose groups or operations the edit removed
	// since it last settled, and sparseServices records that it removed a
	// service.
	sparse         []*treeNode
	sparseServices bool
	// chain holds the nodes from a service down to the one being found.
	chain []*treeNode
	// work counts the nodes the edit has stepped through and the elements it
	// has copied or moved in the tree's slices, which is what its writes cost.
	work int
}

// treeClass is the class of a node of the service tree, spelled as a refusal
// names it.
type treeClass string

const (
	serviceNode   treeClass = "service"
	groupNode     treeClass = "group"
	operationNode treeClass = "operation"
)

// treeKey is a node of the tree by its class and ID.
type treeKey struct {
	class treeClass
	id    string
}

// treeNode is one service, group or operation of the tree an Editor edits:
// where it sits among its parent's, the nodes it holds, which of the slices it
// holds them in the edit has copied, and whether the edit removed it or some of
// what it holds.
type treeNode struct {
	class  treeClass
	parent *treeNode
	at     int
	groups []*treeNode
	ops    []*treeNode
	// ownsGroups and ownsOps record that the node's Groups and Operations are
	// the edit's own copies.
	ownsGroups, ownsOps bool
	removed, sparse     bool
}

// index reads the tree out of services.
func (t *serviceTree) index(services []Service) {
	t.read, t.byID = true, map[treeKey][]*treeNode{}
	var path descent
	for i := range services {
		n := t.add(nil, serviceNode, i, string(services[i].ID))
		t.services = append(t.services, n)
		t.indexGroups(n, services[i].Groups, 0, &path)
	}
}

// indexGroups reads groups, held by parent at depth levels below a service,
// with what they hold, as forEachGroupOperation walks them.
func (t *serviceTree) indexGroups(parent *treeNode, groups []OperationGroup, depth int, path *descent) {
	if len(groups) == 0 {
		return
	}
	if depth > MaxGroupDepth {
		t.cut = true
		return
	}
	if !path.enter(&groups[0], len(groups)) {
		return
	}
	for i := range groups {
		g := t.add(parent, groupNode, i, string(groups[i].ID))
		parent.groups = append(parent.groups, g)
		for j := range groups[i].Operations {
			g.ops = append(g.ops, t.add(g, operationNode, j, string(groups[i].Operations[j].ID)))
		}
		t.indexGroups(g, groups[i].Groups, depth+1, path)
	}
	path.leave()
}

// add records a node of class read at position at under parent, with its ID.
func (t *serviceTree) add(parent *treeNode, class treeClass, at int, id string) *treeNode {
	n := &treeNode{class: class, parent: parent, at: at}
	key := treeKey{class: class, id: id}
	t.byID[key] = append(t.byID[key], n)
	return n
}

// find returns the node of class the edit holds at id, when it holds one, and
// how many it holds there: one the edit removed, or holds inside one it
// removed, it no longer holds.
func (t *serviceTree) find(class treeClass, id string) (*treeNode, int) {
	var (
		found *treeNode
		held  int
	)
	for _, n := range t.byID[treeKey{class: class, id: id}] {
		if n.held() {
			found = n
			held++
		}
	}
	return found, held
}

// held reports whether neither n nor a node holding it was removed.
func (n *treeNode) held() bool {
	for m := n; m != nil; m = m.parent {
		if m.removed {
			return false
		}
	}
	return true
}

// place is where a node sits in a document: the last service, group and
// operation on the path to it.
type place struct {
	svc *Service
	grp *OperationGroup
	op  *Operation
}

// locate returns where n sits in out. A write first copies each slice on the
// path to n that the edit has not copied yet, the one n sits in among them, so
// it writes nothing the edited document holds.
func (t *serviceTree) locate(out *Document, n *treeNode, write bool) place {
	t.chain = t.chain[:0]
	for m := n; m != nil; m = m.parent { // no deeper than index reads, which MaxGroupDepth bounds
		t.chain = append(t.chain, m)
	}
	slices.Reverse(t.chain)
	t.work += len(t.chain)
	var (
		p      place
		holder *treeNode
		groups *[]OperationGroup
	)
	for _, m := range t.chain {
		switch m.class {
		case serviceNode:
			own(t, &out.Services, &t.owned, write)
			p.svc = &out.Services[m.at]
			groups = &p.svc.Groups
		case groupNode:
			own(t, groups, &holder.ownsGroups, write)
			p.grp = &(*groups)[m.at]
			groups = &p.grp.Groups
		case operationNode:
			own(t, &p.grp.Operations, &holder.ownsOps, write)
			p.op = &p.grp.Operations[m.at]
		}
		holder = m
	}
	return p
}

// own makes *s a copy of itself the first time a write reaches it, as owned
// records, so the write lands in nothing the edited document holds.
func own[T any](t *serviceTree, s *[]T, owned *bool, write bool) {
	if write && !*owned {
		*s = slices.Clone(*s)
		*owned = true
		t.work += len(*s)
	}
}

// remove marks n removed, for the slice it sits in to drop it when the edit
// settles.
func (t *serviceTree) remove(n *treeNode) {
	n.removed = true
	switch {
	case n.parent == nil:
		t.sparseServices = true
	case !n.parent.sparse:
		n.parent.sparse = true
		t.sparse = append(t.sparse, n.parent)
	}
}

// settle drops from out each node the edit removed, one pass over each slice
// that holds one, and records the new position of every node after it. A
// slice inside a node the edit removed is left as it is, going with that node.
func (t *serviceTree) settle(out *Document) {
	if t.sparseServices {
		t.sparseServices = false
		compact(t, &out.Services, &t.owned, &t.services)
	}
	for _, h := range t.sparse {
		h.sparse = false
		if !h.held() {
			continue
		}
		p := t.locate(out, h, true)
		switch h.class {
		case serviceNode:
			compact(t, &p.svc.Groups, &h.ownsGroups, &h.groups)
		case groupNode:
			compact(t, &p.grp.Groups, &h.ownsGroups, &h.groups)
			compact(t, &p.grp.Operations, &h.ownsOps, &h.ops)
		case operationNode: // holds no node
		}
	}
	t.sparse = t.sparse[:0]
}

// compact drops from *elems the element of each node of *kids the edit
// removed, and those nodes from *kids, leaving every other node at its new
// position. It copies *elems first unless owned says the edit has, and leaves a
// slice it empties nil, as the document decodes one that was empty.
func compact[T any](t *serviceTree, elems *[]T, owned *bool, kids *[]*treeNode) {
	t.work += len(*kids)
	if !slices.ContainsFunc(*kids, func(k *treeNode) bool { return k.removed }) {
		return
	}
	own(t, elems, owned, true)
	kept, live := (*elems)[:0], (*kids)[:0]
	for i, k := range *kids {
		if k.removed {
			continue
		}
		k.at = len(kept)
		kept = append(kept, (*elems)[i])
		live = append(live, k)
		t.work++
	}
	clear((*elems)[len(kept):])
	clear((*kids)[len(live):])
	*kids = live
	if *elems = kept; len(kept) == 0 {
		*elems = nil
	}
}
