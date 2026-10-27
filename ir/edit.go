package ir

import (
	"fmt"
	"reflect"
	"strconv"
)

// EditRefused is the code of the error [Editor.Finish] reports for each call an
// [Editor] refused and each node it put back. It names a bug in the code
// editing the document, which no source can produce, so it locates nothing in a
// source.
const EditRefused = "ir/edit-refused"

// Editor is a copy-on-write edit of a [Document], the way a pass changes one.
// It reads, changes and removes a type, service, group, operation, channel,
// message or auth scheme by ID, and the document's own fields through
// [Editor.UpdateDocument].
//
// It never writes the document it edits, copying a container at its first
// write. It keeps each node at its ID's key and adds no ID, nested ones
// included, so every ID stays in a namespace the document declares (invariant
// 3). A write breaking either, or only a bug's, is refused, and
// [Editor.Finish] reports it. It is not safe for concurrent use.
type Editor struct {
	// doc is the document edited, which nothing here writes.
	doc *Document
	// out is the document the edit returns, a copy of doc's fields. Each
	// container in it is doc's own until the edit first writes into it: owns
	// records which registries are the edit's, and tree which of the service
	// tree's slices are.
	out  *Document
	owns ownership
	tree serviceTree
	// refused holds the refusals made since Finish last reported them, as errors
	// rather than strings: irverify holds every []string ir declares to what a
	// document keeps in it, and this is no part of a document.
	refused []error
	// running names the update whose change is running, which may read the edit
	// but neither write to it nor end it, and intruded records that a call it
	// made was refused. finished records that Finish has returned out.
	running            string
	intruded, finished bool
	// copying holds the map entries an update's copy reads, and declared the IDs
	// its node declares, each kept from one update to the next, so neither
	// costs anything per entry once it has grown.
	copying  entryStack
	declared idCounts
}

// ownership records which of a document's registries an edit has copied, and
// so may write.
type ownership struct {
	types, channels, messages, auth bool
}

// NewEditor starts an edit of doc. A nil doc is refused, and the edit then
// stands for an empty document, so Finish still returns one.
func NewEditor(doc *Document) *Editor {
	if doc == nil {
		e := &Editor{doc: &Document{}, out: &Document{}}
		e.refuse("edit rejected: no document to edit")
		return e
	}
	out := *doc
	return &Editor{doc: doc, out: &out}
}

// nodeKind is what an update needs to know of one kind of node: the name of
// its update method, the class of ID the node declares, how to read that ID,
// and which fields hold nodes the edit keys apart.
type nodeKind[N any] struct {
	method string
	class  reflect.Type
	id     func(N) string
	// keyed names a field of a node holding nodes the edit changes by their own
	// IDs, which an update's copy leaves out, when a change has set one. It is
	// nil for a kind whose nodes hold none.
	keyed func(N) string
}

// updated returns a deep copy of node, the one the edit holds at id, as change
// leaves it, and whether to take it; at names node in refusals.
//
// It refuses a nil change, a node too deep to copy, and a change that writes to
// the edit or ends it, moves the node, sets a field kind.keyed names or
// declares an ID the node does not, or one more often. A refused or panicking
// change leaves the edit as it was.
func updated[N any](e *Editor, kind nodeKind[N], id string, at subject, node N, change func(N)) (N, bool) {
	var none N
	if change == nil {
		e.refuse("update rejected: no change for %s", at)
		return none, false
	}
	next, copied := cloneNode(node, &e.copying)
	if !copied {
		e.refuse("update rejected: %s nests deeper than %d levels, past what an edit copies", at, MaxWalkDepth)
		return none, false
	}
	e.declared.reset()
	e.declared.tally(node, false, kind.class, id)
	e.apply(kind.method, func() { change(next) })
	moved := kind.id(next)
	switch {
	case e.intruded:
		e.refuse("update rejected: the change of %s wrote to the edit or ended it; a change alters its node alone", at)
	case moved != id:
		e.refuse("update rejected: the change moved %s to %q; a node keeps its ID", at, moved)
	case kind.keyed != nil && kind.keyed(next) != "":
		e.refuse("update rejected: the change of %s set its %s, whose nodes the edit changes by their own IDs",
			at, kind.keyed(next))
	default:
		e.declared.tally(next, true, kind.class, moved)
		if added, n, ok := e.declared.excess(); ok {
			e.refuse("update rejected: the change of %s declares %s; an edit adds no ID", at, n.clause(added, "the node"))
			return none, false
		}
		return next, true
	}
	return none, false
}

// apply runs change as the change of the update method names, the edit
// counting as inside it until it returns, by a panic too: a caller recovering
// one finds the edit as it was rather than refusing every call as made from
// inside a change.
func (e *Editor) apply(method string, change func()) {
	e.running, e.intruded = method, false
	defer func() { e.running = "" }()
	change()
}

// open reports whether the edit takes a write verb names, at the node at names,
// refusing one after Finish and one from inside a change.
func (e *Editor) open(verb string, at subject) bool {
	switch {
	case e.finished:
		e.refuse("%s rejected: the edit is finished (%s)", verb, at)
	case e.running != "":
		e.refuse("%s rejected: called from inside an %s change (%s); a change alters its node alone",
			verb, e.running, at)
	default:
		return true
	}
	return false
}

// Finish ends the edit and returns its document: a new Document sharing with
// the edited one every container it wrote nothing into. Its caller writes
// none of it: the edit has a call for every part.
//
// It reports an EditRefused error for each refused call, in order, then for
// each type the edit took that has since moved off its key, putting the edited
// document's node back. A later call returns the same document and the
// refusals made since. Inside a change it ends nothing, returning the refusals
// not yet reported, its own included, which the next call reports again.
func (e *Editor) Finish() (*Document, []Diagnostic) {
	e.tree.settle(e.out)
	if e.running != "" {
		e.refuse("finish rejected: called from inside an %s change; a change alters its node alone", e.running)
		return e.out, e.diagnostics()
	}
	if !e.finished {
		e.finished = true
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
			"internal: edit: "+err.Error(), Provenance{Source: NoSource}))
	}
	return diags
}

// refuse records why the edit refused a call or put a node back, for Finish to
// report. The edit declines the call rather than returning an error, since its
// caller has no recovery but to stop, and recording it keeps the refusal from
// being lost. A refusal while a change runs is of a call the change made, so
// the update is refused with it, whichever check refused it.
func (e *Editor) refuse(format string, args ...any) {
	e.refused = append(e.refused, fmt.Errorf(format, args...))
	if e.running != "" {
		e.intruded = true
	}
}

// subject names the node a call is at in a refusal: by its ID, quoted, or as
// the document. It is spelled only when a call is refused.
type subject struct {
	id       string
	document bool
}

// String spells s.
func (s subject) String() string {
	if s.document {
		return "the document"
	}
	return strconv.Quote(s.id)
}

// subjectAt names the node at id.
func subjectAt[K ~string](id K) subject {
	return subject{id: string(id)}
}
