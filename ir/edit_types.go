// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package ir

import (
	"maps"
	"reflect"
	"slices"
)

// Type returns the node the edit holds at id: the one written there, or else
// the edited document's own. The node is shared, so it is read and never
// changed; UpdateType changes a copy.
func (e *Editor) Type(id TypeID) (TypeDef, bool) {
	td, held := e.out.Types[id]
	return td, held
}

// UpdateType hands change a deep copy of the node at id, to alter in any part
// but its ID, and holds the copy in its place. change may read the edit, not
// write to it, end it or keep the node.
//
// It refuses an id holding no node, a nil change, a node that is nil, too deep
// to copy or off its own ID's key, and a change moving the node or declaring
// an ID it does not; one writing to or ending the edit is refused with the
// call it made. A refused or panicking update changes nothing.
func (e *Editor) UpdateType(id TypeID, change func(TypeDef)) {
	current, ok := entryAt(e, e.types(), "update", id)
	switch {
	case !ok:
	case IsNilTypeDef(current):
		e.refuse("update rejected: %q holds a nil type definition, which has nothing to change", id)
	case current.Common().ID != id:
		e.refuse("update rejected: %q holds the node of %q, which belongs at its own ID", id, current.Common().ID)
	default:
		if next, taken := updated(e, typeKind(), string(id), subjectAt(id), current, change); taken {
			e.types().write()[id] = next
		}
	}
}

// typeKind is what an update knows of a type.
func typeKind() nodeKind[TypeDef] {
	return nodeKind[TypeDef]{
		method: "UpdateType",
		id:     func(td TypeDef) string { return string(td.Common().ID) },
	}
}

// ReplaceType holds td at its own ID, in place of the node there. td is a node
// built afresh, of any kind, which the edit takes as it is: it shares nothing
// anyone changes with the edited document, and is not changed once taken.
// UpdateType changes a node instead.
//
// It refuses a nil td, an ID the edit holds no node at, the edited document's
// own node at that ID, which is either unchanged or changed in place, and a td
// declaring an ID the node it replaces does not.
func (e *Editor) ReplaceType(td TypeDef) {
	if IsNilTypeDef(td) {
		e.refuse("replace rejected: nil type definition")
		return
	}
	id := td.Common().ID
	current, ok := entryAt(e, e.types(), "replace", id)
	switch {
	case !ok:
	case e.doc.Types[id] == td:
		e.refuse("replace rejected: %q is the edited document's own node, which an edit never changes; "+
			"change a copy with UpdateType", id)
	case e.replaceable(id, current, td):
		e.types().write()[id] = td
	}
}

// replaceable reports whether td declares no ID current, the node the edit
// holds at id, does not, refusing a replacement that does. Its own ID, which
// its key holds to it, is not among them: current may be nil or off its key.
func (e *Editor) replaceable(id TypeID, current, td TypeDef) bool {
	c := &e.declared
	c.node = c.scan(c.node[:0], current)
	c.change = c.scan(c.change[:0], td)
	own := declaredID{class: reflect.TypeFor[TypeID](), id: string(id)}
	if i := slices.Index(c.change, own); i >= 0 {
		c.change = slices.Delete(c.change, i, i+1)
	}
	if added, n, found := c.excess(); found {
		e.refuse("replace rejected: the node for %q declares %s; an edit adds no ID",
			id, n.clause(added, "the node it replaces"))
		return false
	}
	return true
}

// RemoveType removes the node the edit holds at id, and refuses an id it holds
// none at. Whatever still references id is left to the passes that check
// references, validate among them.
func (e *Editor) RemoveType(id TypeID) {
	removeEntry(e, e.types(), id)
}

// refuseMoved refuses each node the edit took, by ReplaceType or UpdateType,
// whose ID no longer names the key it sits at: one changed after the edit took
// it, as a value reused for several IDs is. The edited document's node goes
// back at that key, so no node the edit took sits off its own. A node the
// edited document holds at that key is its own, which the edit never took.
func (e *Editor) refuseMoved() {
	if !e.owns.types {
		return
	}
	for _, id := range slices.Sorted(maps.Keys(e.out.Types)) {
		td := e.out.Types[id]
		if td == e.doc.Types[id] {
			continue
		}
		if moved := td.Common().ID; moved != id {
			e.refuse("finish: %q holds a node moved to %q after the edit took it, so the edited document's node "+
				"stays; a type keeps its ID", id, moved)
			e.out.Types[id] = e.doc.Types[id] // an edit adds no ID, so doc holds one here
		}
	}
}
