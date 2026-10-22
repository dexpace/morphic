package ir

import (
	"fmt"
	"maps"
	"slices"
)

// EditRefused is the code of the error [Editor.Done] reports for each call an
// [Editor] refused and each node it put back. It names a bug in the code
// editing the document, which no source can produce, so it locates nothing in a
// source.
const EditRefused = "ir/edit-refused"

// Editor is a copy-on-write edit of a [Document]'s type registry, the way
// code outside the compilers changes one, as a pass does.
//
// It never writes the document it edits, puts each node it takes at the key
// its own ID names, and adds no ID, so every ID stays in a namespace the
// document already declares (invariant 3). A write breaking either, or one only
// a bug makes, is refused, and [Editor.Done] reports each refusal, as
// compile.Types reports its own.
//
// Build one with [Edit]; it refuses every write after [Editor.Done], and is
// not safe for concurrent use.
type Editor struct {
	// doc is the document edited, which nothing here writes.
	doc *Document
	// out is the document the edit returns, a copy of doc's fields, and owned
	// reports whether its registry is the edit's own yet rather than doc's.
	out   *Document
	owned bool
	// refused holds the refusals made since Done last reported them.
	refused []error
	// changing records that an UpdateType change is running, which may read the
	// edit but neither write to it nor end it, and intruded that a call it made
	// was refused.
	// done records that Done has returned out, which ends the edit.
	changing, intruded, done bool
}

// Edit starts an edit of doc. A nil doc is refused, and the edit then stands
// for an empty document, so Done still returns one.
func Edit(doc *Document) *Editor {
	if doc == nil {
		e := &Editor{doc: &Document{}, out: &Document{}}
		e.refuse("edit rejected: no document to edit")
		return e
	}
	out := *doc
	return &Editor{doc: doc, out: &out}
}

// Type returns the node the edit holds at id: the one written there, or else
// the edited document's own. The node is shared, so it is read and never
// changed; UpdateType changes a copy.
func (e *Editor) Type(id TypeID) (TypeDef, bool) {
	td, held := e.out.Types[id]
	return td, held
}

// UpdateType hands change a deep copy of the node at id, to alter in any part
// but its ID, and holds the copy in its place. change may read the edit
// through Type but not write to it, end it or keep the node.
//
// It refuses an id holding no node, a nil change, a node that is nil, too deep
// to copy or off its own ID's key, and a change moving the node; one writing
// to or ending the edit is refused with the call it made. A refused or
// panicking update leaves the node as it was.
func (e *Editor) UpdateType(id TypeID, change func(TypeDef)) {
	next, ok := e.copyFor(id, change)
	if !ok {
		return
	}
	e.apply(change, next)
	switch moved := next.Common().ID; {
	case e.intruded:
		e.refuse("update rejected: the change of %q wrote to the edit or ended it; a change alters its node alone", id)
	case moved != id:
		e.refuse("update rejected: the change moved %q to %q; a type keeps its ID", id, moved)
	default:
		e.write()[id] = next
	}
}

// apply runs change over next, the edit counting as inside a change until it
// returns, by a panic too: a caller recovering one finds the edit as it was
// rather than refusing every call as made from inside a change.
func (e *Editor) apply(change func(TypeDef), next TypeDef) {
	e.changing, e.intruded = true, false
	defer func() { e.changing = false }()
	change(next)
}

// copyFor returns a copy of the node the edit holds at id, for change to
// alter, and refuses what UpdateType refuses before change runs.
func (e *Editor) copyFor(id TypeID, change func(TypeDef)) (TypeDef, bool) {
	current, ok := e.writable("update", id)
	switch {
	case !ok:
		return nil, false
	case change == nil:
		e.refuse("update rejected: no change for %q", id)
		return nil, false
	case IsNilTypeDef(current):
		e.refuse("update rejected: %q holds a nil type definition, which has nothing to change", id)
		return nil, false
	case current.Common().ID != id:
		e.refuse("update rejected: %q holds the node of %q, which belongs at its own ID", id, current.Common().ID)
		return nil, false
	}
	next, copied := cloneTypeDef(current)
	if !copied {
		e.refuse("update rejected: %q nests deeper than %d levels, past what an edit copies", id, MaxWalkDepth)
	}
	return next, copied
}

// ReplaceType holds td at its own ID, in place of the node there. td is a node
// built afresh, of any kind, which the edit takes as it is: it shares nothing
// anyone changes with the edited document, and is not changed once taken.
// UpdateType changes a node instead. It refuses a nil td, an ID the edit holds
// no node at, and the edited document's own node at that ID, which can only be
// one changed in place.
func (e *Editor) ReplaceType(td TypeDef) {
	if IsNilTypeDef(td) {
		e.refuse("replace rejected: nil type definition")
		return
	}
	id := td.Common().ID
	if _, ok := e.writable("replace", id); !ok {
		return
	}
	if e.doc.Types[id] == td {
		e.refuse("replace rejected: %q is the edited document's own node, which an edit never changes; "+
			"change a copy with UpdateType", id)
		return
	}
	e.write()[id] = td
}

// RemoveType removes the node the edit holds at id, and refuses an id it holds
// none at. Whatever still references id is left to the passes that check
// references, validate among them.
func (e *Editor) RemoveType(id TypeID) {
	if _, ok := e.writable("remove", id); !ok {
		return
	}
	delete(e.write(), id)
}

// Done ends the edit and returns the edited document: a new Document sharing
// every field with the document edited, the registry until the first write. A
// caller may set its fields but writes neither registry.
//
// It reports an EditRefused error for each refused call, in order, then for
// each node the edit took that has since moved off its key, putting the edited
// document's node back. A later call returns the same document and the
// refusals made since. Inside an UpdateType change it ends nothing, returning
// the refusals not yet reported, its own included, which the next call reports
// again.
func (e *Editor) Done() (*Document, []Diagnostic) {
	if e.changing {
		e.refuse("done rejected: called from inside an UpdateType change; a change alters its node alone")
		return e.out, e.diagnostics()
	}
	if !e.done {
		e.done = true
		e.refuseMoved()
	}
	diags := e.diagnostics()
	e.refused = nil
	return e.out, diags
}

// diagnostics returns an EditRefused error for each refusal not yet reported.
func (e *Editor) diagnostics() []Diagnostic {
	diags := make([]Diagnostic, 0, len(e.refused))
	for _, err := range e.refused {
		diags = append(diags, NewDiagnostic(SeverityError, EditRefused,
			"internal: type edit: "+err.Error(), Provenance{Source: NoSource}))
	}
	return diags
}

// refuseMoved refuses each node the edit took, by ReplaceType or UpdateType,
// whose ID no longer names the key it sits at: one changed after the edit took
// it, as a value reused for several IDs is. The edited document's node goes
// back at that key, so no node the edit took sits off its own. A node the
// edited document holds at that key is its own, which the edit never took.
func (e *Editor) refuseMoved() {
	if !e.owned {
		return
	}
	for _, id := range slices.Sorted(maps.Keys(e.out.Types)) {
		td := e.out.Types[id]
		if td == e.doc.Types[id] {
			continue
		}
		if moved := td.Common().ID; moved != id {
			e.refuse("done: %q holds a node moved to %q after the edit took it, so the edited document's node "+
				"stays; a type keeps its ID", id, moved)
			e.out.Types[id] = e.doc.Types[id] // an edit adds no ID, so doc holds one here
		}
	}
}

// writable returns the node the edit holds at id for a write verb names, and
// refuses one after Done, one from inside an UpdateType change, and one at an
// id the edit holds no node at: an edit adds no ID.
func (e *Editor) writable(verb string, id TypeID) (TypeDef, bool) {
	switch {
	case e.done:
		e.refuse("%s rejected: the edit is done (%q)", verb, id)
	case e.changing:
		e.refuse("%s rejected: called from inside an UpdateType change (%q); a change alters its node alone", verb, id)
	default:
		if td, held := e.Type(id); held {
			return td, true
		}
		e.refuse("%s rejected: no type %q to %s; an edit adds no type", verb, id, verb)
	}
	return nil, false
}

// write returns the registry the edit owns, copying the edited document's at
// the first write, so that document is never written.
func (e *Editor) write() TypeRegistry {
	if !e.owned {
		e.out.Types = maps.Clone(e.doc.Types)
		e.owned = true
	}
	return e.out.Types
}

// refuse records why the edit refused a call or put a node back, for Done to
// report. The edit declines the call rather than returning an error, since its
// caller has no recovery but to stop, and recording it keeps the refusal from
// being lost. A refusal while an UpdateType change runs is of a call the
// change made, so the update is refused with it, whichever check refused it.
func (e *Editor) refuse(format string, args ...any) {
	e.refused = append(e.refused, fmt.Errorf(format, args...))
	if e.changing {
		e.intruded = true
	}
}
