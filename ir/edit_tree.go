// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package ir

// Service returns the service the edit holds at id, its groups as the edit
// holds them, which stay so whatever the edit does next: the read builds the
// slices in the service a write reached since the last one. It is shared, so
// it is read and never changed; UpdateService changes a copy. An ID the edit
// holds several services at holds none.
func (e *Editor) Service(id ServiceID) (Service, bool) {
	if treeNode, held := e.indexed().find(serviceNode, string(id)); held == 1 {
		return e.tree.serviceOf(treeNode), true
	}
	return Service{}, false
}

// UpdateService hands change a deep copy of the service at id without its
// groups, which the edit changes by their own IDs, and holds the copy in its
// place, beside the groups as they are. It refuses what UpdateType refuses of
// an id and a change, an id the edit holds several services at, and a change
// setting Groups.
func (e *Editor) UpdateService(id ServiceID, change func(*Service)) {
	treeNode, ok := e.nodeAt("update", serviceNode, string(id))
	if !ok {
		return
	}
	own := *treeNode.service
	own.Groups = nil
	next, taken := updated(e, serviceKind(), string(id), subjectAt(id), &own, change)
	if !taken {
		return
	}
	next.Groups = treeNode.service.Groups
	treeNode.service = new(*next)
	e.tree.touch(treeNode)
}

// serviceKind is what an update knows of a service.
func serviceKind() nodeKind[*Service] {
	return nodeKind[*Service]{
		method: "UpdateService",
		id:     func(service *Service) string { return string(service.ID) },
		keyed: func(service *Service) string {
			if service.Groups != nil {
				return "Groups"
			}
			return ""
		},
	}
}

// RemoveService removes the service at id, with the groups and operations it
// holds. It refuses an id the edit holds no service at, or several.
func (e *Editor) RemoveService(id ServiceID) {
	if treeNode, ok := e.nodeAt("remove", serviceNode, string(id)); ok {
		e.tree.remove(treeNode)
	}
}

// Group returns the operation group the edit holds at id, the groups and
// operations in it as the edit holds them, which stay so whatever the edit does
// next: the read builds the slices in the group a write reached since the last
// one. It is shared, so it is read and never changed; UpdateGroup changes a
// copy. An ID the edit holds several groups at holds none, and nor does one
// ForEachGroup does not reach.
func (e *Editor) Group(id GroupID) (OperationGroup, bool) {
	if treeNode, held := e.indexed().find(groupNode, string(id)); held == 1 {
		return e.tree.groupOf(treeNode), true
	}
	return OperationGroup{}, false
}

// UpdateGroup hands change a deep copy of the operation group at id without
// the groups and operations in it, which the edit changes by their own IDs, and
// holds the copy in its place, beside those as they are. It refuses what
// UpdateType refuses of an id and a change, an id the edit holds several groups
// at, and a change setting Groups or Operations.
func (e *Editor) UpdateGroup(id GroupID, change func(*OperationGroup)) {
	treeNode, ok := e.nodeAt("update", groupNode, string(id))
	if !ok {
		return
	}
	own := *treeNode.group
	own.Groups, own.Operations = nil, nil
	next, taken := updated(e, groupKind(), string(id), subjectAt(id), &own, change)
	if !taken {
		return
	}
	next.Groups, next.Operations = treeNode.group.Groups, treeNode.group.Operations
	treeNode.group = new(*next)
	e.tree.touch(treeNode)
}

// groupKind is what an update knows of an operation group.
func groupKind() nodeKind[*OperationGroup] {
	return nodeKind[*OperationGroup]{
		method: "UpdateGroup",
		id:     func(operationGroup *OperationGroup) string { return string(operationGroup.ID) },
		keyed: func(operationGroup *OperationGroup) string {
			switch {
			case operationGroup.Groups != nil:
				return "Groups"
			case operationGroup.Operations != nil:
				return "Operations"
			default:
				return ""
			}
		},
	}
}

// RemoveGroup removes the operation group at id, with the groups and
// operations it holds; the group or service holding it stays, even emptied. It
// refuses an id the edit holds no group at, or several.
func (e *Editor) RemoveGroup(id GroupID) {
	if treeNode, ok := e.nodeAt("remove", groupNode, string(id)); ok {
		e.tree.remove(treeNode)
	}
}

// Operation returns the operation the edit holds at id. It is shared, so it is
// read and never changed; UpdateOperation changes a copy. An ID the edit holds
// several operations at holds none, and nor does one ForEachOperation does not
// reach.
func (e *Editor) Operation(id OperationID) (Operation, bool) {
	if treeNode, held := e.indexed().find(operationNode, string(id)); held == 1 {
		return *treeNode.operation, true
	}
	return Operation{}, false
}

// UpdateOperation hands change a deep copy of the operation at id, to alter in
// any part but its ID, and holds the copy in its place. It refuses what
// UpdateType refuses of an id and a change, and an id the edit holds several
// operations at.
func (e *Editor) UpdateOperation(id OperationID, change func(*Operation)) {
	treeNode, ok := e.nodeAt("update", operationNode, string(id))
	if !ok {
		return
	}
	if next, taken := updated(e, operationKind(), string(id), subjectAt(id), treeNode.operation, change); taken {
		treeNode.operation = new(*next)
		e.tree.touch(treeNode)
	}
}

// operationKind is what an update knows of an operation.
func operationKind() nodeKind[*Operation] {
	return nodeKind[*Operation]{
		method: "UpdateOperation",
		id:     func(op *Operation) string { return string(op.ID) },
	}
}

// RemoveOperation removes the operation at id; the group holding it stays,
// even emptied. It refuses an id the edit holds no operation at, or several.
func (e *Editor) RemoveOperation(id OperationID) {
	if treeNode, ok := e.nodeAt("remove", operationNode, string(id)); ok {
		e.tree.remove(treeNode)
	}
}

// nodeAt returns the node of class the edit holds at id for a write verb
// names. It refuses one the edit does not take (open), an id the edit holds no
// such node at, as an edit adds none, and one it holds several at, as which to
// write is unknown.
func (e *Editor) nodeAt(verb string, class treeClass, id string) (*treeNode, bool) {
	at := subjectAt(id)
	if !e.open(verb, at) {
		return nil, false
	}
	treeNodeValue, held := e.indexed().find(class, id)
	noun := string(class)
	switch {
	case held == 1:
		return treeNodeValue, true
	case held > 1:
		e.refuse("%s rejected: %d %ss have the ID %s, so the edit cannot tell which to %s", verb, held, noun, at, verb)
	case e.tree.cut:
		e.refuse("%s rejected: no %s %s to %s; an edit adds no %s, and reaches none in groups nested deeper "+
			"than %d levels or past the first %d groups and operations", verb, noun, at, verb, noun,
			MaxGroupDepth, MaxWalkSteps)
	default:
		e.refuse("%s rejected: no %s %s to %s; an edit adds no %s", verb, noun, at, verb, noun)
	}
	return nil, false
}

// indexed returns the edit's tree, read off the document it holds the first
// time.
func (e *Editor) indexed() *serviceTree {
	if !e.tree.read {
		e.tree.index(e.out.Services, MaxWalkSteps)
	}
	return &e.tree
}
