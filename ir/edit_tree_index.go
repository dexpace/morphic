package ir

import "slices"

// serviceTree is the service tree an [Editor] edits, held apart from the
// document it was read off, when first needed and as [ForEachOperation] walks.
//
// A node holds its service, group or operation as last written, the edited
// document's own until then. No slice is written once it exists: a write
// replaces a node's value and marks the lists on the path to it stale, and a
// read or [Editor.Finish] builds a stale list anew. So the edited document is
// never written, a read stays as it was, and a slice nothing under it changed
// stays the edited document's.
type serviceTree struct {
	read bool
	// cut records that groups nested past the cap went unread.
	cut bool
	// services lists the document's services.
	services treeList
	// byID holds the nodes read at each ID of each class, keyed by the ID's
	// string so that no field here is an ID position.
	byID map[treeKey][]*treeNode
	// work counts the nodes the edit has stepped through and the elements of the
	// slices it has built, which is what its writes cost.
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
// where it sits, what it holds, and whether the edit removed it.
type treeNode struct {
	parent *treeNode
	// in is the list holding the node.
	in *treeList
	// service, group and operation hold the node as the edit does, whichever of
	// them its class is: a copy of what a change left, not the node the change
	// was handed and may have kept. A slice in it shows the list of the nodes it
	// holds unless that list is stale.
	service   *Service
	group     *OperationGroup
	operation *Operation
	// groups and operations list the nodes it holds.
	groups, operations treeList
	removed            bool
}

// treeList is the nodes one slice of the tree holds, and whether the edit has
// changed one since that slice was built, so that it no longer shows them.
type treeList struct {
	nodes []*treeNode
	stale bool
}

// index reads the tree out of services.
func (t *serviceTree) index(services []Service) {
	t.read, t.byID = true, map[treeKey][]*treeNode{}
	var path descent
	for i := range services {
		n := t.add(nil, &t.services, serviceNode, string(services[i].ID))
		n.service = &services[i]
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
		g := t.add(parent, &parent.groups, groupNode, string(groups[i].ID))
		g.group = &groups[i]
		for j := range groups[i].Operations {
			op := &groups[i].Operations[j]
			t.add(g, &g.operations, operationNode, string(op.ID)).operation = op
		}
		t.indexGroups(g, groups[i].Groups, depth+1, path)
	}
	path.leave()
}

// add records a node of class with id, held by parent in the list in.
func (t *serviceTree) add(parent *treeNode, in *treeList, class treeClass, id string) *treeNode {
	n := &treeNode{parent: parent, in: in}
	in.nodes = append(in.nodes, n)
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

// touch records that the edit wrote n: the list holding it, and each list on
// the path to it, no longer shows what the edit holds.
func (t *serviceTree) touch(n *treeNode) {
	for m := n; m != nil; m = m.parent { // no deeper than index reads, which MaxGroupDepth bounds
		m.in.stale = true
		t.work++
	}
}

// remove records that the edit removed n, which the next build of the list
// holding it drops.
func (t *serviceTree) remove(n *treeNode) {
	n.removed = true
	t.touch(n)
}

// settle brings out's services up to what the edit holds.
func (t *serviceTree) settle(out *Document) {
	if t.services.stale {
		out.Services = built(t, &t.services, t.serviceOf)
	}
}

// serviceOf returns n's service as the edit holds it, building what is stale in
// it first.
func (t *serviceTree) serviceOf(n *treeNode) Service {
	if n.groups.stale {
		s := *n.service
		s.Groups = built(t, &n.groups, t.groupOf)
		n.service = &s
	}
	return *n.service
}

// groupOf returns n's group as the edit holds it, building what is stale in it
// first. It recurses no deeper than index read, which MaxGroupDepth bounds.
func (t *serviceTree) groupOf(n *treeNode) OperationGroup {
	if !n.groups.stale && !n.operations.stale {
		return *n.group
	}
	g := *n.group
	if n.groups.stale {
		g.Groups = built(t, &n.groups, t.groupOf)
	}
	if n.operations.stale {
		g.Operations = built(t, &n.operations, func(n *treeNode) Operation { return *n.operation })
	}
	n.group = &g
	return g
}

// built drops from list the nodes the edit removed and returns a new slice of
// what element reads off each one left, or nil for none, as the document
// decodes a slice that was empty.
func built[T any](t *serviceTree, list *treeList, element func(*treeNode) T) []T {
	t.work += len(list.nodes)
	list.stale = false
	list.nodes = slices.DeleteFunc(list.nodes, func(n *treeNode) bool { return n.removed })
	if len(list.nodes) == 0 {
		return nil
	}
	elems := make([]T, len(list.nodes))
	for i, n := range list.nodes {
		elems[i] = element(n)
	}
	return elems
}
