// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"slices"

	"github.com/dexpace/morphic/compilers/openapi/internal/bind"
	"github.com/dexpace/morphic/compilers/openapi/internal/budget"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/statement"
)

// reading is what one statement admits, read once a run, and what the read
// met. admitted is what an election compares; kinds the kinds of instance it
// lowers to, null aside, which a bound is placed by. cut marks a statement on
// or reaching a cycle of conjunct edges, a $ref landing on no schema or a
// value set no reading converts; loose one meeting a $dynamicRef the bindings
// read as no $ref. reads and members are the schemas and value-set members the
// read takes in, each counted per path and held to one past a reader's cap.
type reading struct {
	admitted       admitted
	kinds          kindSet
	cut, loose     bool
	reads, members int
}

// conjoin adds what r's conjunct child admits, and what its read took in, to r.
func (r *reading) conjoin(child *reading) {
	r.kinds &= child.kinds
	if r.took(child) {
		r.admitted = r.admitted.and(child.admitted)
	}
}

// took adds what child's read took in and met to r's, and reports whether r
// is still within both caps. One past either stops (past).
func (r *reading) took(child *reading) bool {
	r.cut = r.cut || child.cut
	r.loose = r.loose || child.loose
	r.reads = min(r.reads+child.reads, maxAdmissionReads+1)
	r.members = min(r.members+child.members, maxAdmissionMembers+1)
	if r.reads <= maxAdmissionReads && r.members <= maxAdmissionMembers {
		return true
	}
	r.past(child)
	return false
}

// past is r once its read takes in more than a reader's cap, which every
// reader reads as cut short: it admits every kind and holds no value set or
// format, so what it merges stops growing with the paths to it, as the formats
// a read conjoins are one per schema taken in. The element schema is kept,
// child's where r holds none, which missingArrayItemsBesideRef reads past a
// cut.
func (r *reading) past(child *reading) {
	items := r.admitted.items
	if items == nil {
		items = child.admitted.items
	}
	r.admitted = admitted{kinds: kindAny, items: items}
}

// cycleRead is what a statement on a cycle of conjunct edges reads as: cut,
// whichever of its statements the read entered at, and past every reader's
// cap.
func cycleRead() *reading {
	return &reading{admitted: admitted{kinds: kindAny}, kinds: anyInstance, cut: true, reads: maxAdmissionReads + 1}
}

// admissionsIn keys a run's admissions. A statement the bindings do not place
// is read in the dialect of the context asking (definesNullableAt), so each
// dialect keeps its own.
type admissionsIn struct {
	nullable bool
}

// admissions is the run's store of readings; how many statements and conjunct
// edges its walks have visited, which the tests hold to one each; and how many
// formats and value-set members the readings it settled hold, which they hold
// to grow with the document and not with its paths.
type admissions struct {
	read                  map[*statement.Statement]*reading
	visits, edges, merged int
}

// admissionOf returns what st admits, read once a run. The first ask walks
// every statement st reaches by its conjunct edges that no read has settled,
// and settles each.
func admissionOf(c lowering.Ctx, st *statement.Statement) *reading {
	store := admissionStore(c)
	if r, ok := store.read[st]; ok {
		return r
	}
	w := admissionWalk{c: c, store: store, index: map[*statement.Statement]int{}}
	w.run(st)
	return store.read[st]
}

// admissionStore returns the run's admissions in the dialect c reads an
// unplaced schema in.
func admissionStore(c lowering.Ctx) *admissions {
	return lowering.Remembered(c, admissionsIn{c.SchemaRules().Defines("nullable")}, func() *admissions {
		return &admissions{read: map[*statement.Statement]*reading{}}
	})
}

// conjunctsOf returns the statements st conjoins with, each conjunct edge in
// the order the read conjoins them, a statement as often as st names it: its
// allOf entries, its oneOf and then anyOf branches, and the statement its
// $ref's first hop lands on, or, for a $dynamicRef the bindings read as no
// $ref, the one it expands to (dynamicExpansionOf). A `properties` or an
// `items` consumes the instance, so it is no edge.
func conjunctsOf(c lowering.Ctx, st *statement.Statement) []*statement.Statement {
	if st.Truth != statement.Stated {
		return nil
	}
	choice := choiceIn(st.Schema)
	subs := slices.Concat(st.AllOf, choice.OneOf, choice.AnyOf)
	out := make([]*statement.Statement, 0, len(subs)+1)
	for _, b := range subs {
		out = append(out, c.Statements().Sub(b))
	}
	if next, _ := declaredOf(c, st); next != nil {
		out = append(out, next)
	} else if at, ok := dynamicExpansionOf(c, st); ok {
		out = append(out, c.Statements().At(at))
	}
	return out
}

// dynamicExpansionOf returns the position the $dynamicRef or $recursiveRef st
// writes names, read at st's own position in its own document, where dynamic
// scope is static (dynamicTarget), and false where it writes none the bindings
// do not read as a $ref, names none, or is placed nowhere.
func dynamicExpansionOf(c lowering.Ctx, st *statement.Statement) (bind.Position, bool) {
	at, placed := siteOf(st.Schema)
	if st.Ref != nil || !placed || !declaresDynamicRef(st.Schema) {
		return bind.Position{}, false
	}
	in, _ := lowering.InDocument(c, at.Doc)
	return dynamicTarget(in, st.Schema, at.Pointer)
}

// admissionWalk is one walk settling the readings a first ask needs, by
// Tarjan's strongly connected components over the conjunct edges, on stacks
// of its own: it recurses nowhere, and visits each statement it has not
// settled and each of that statement's edges once. A component is settled
// when its root returns, every component it reaches settled before it.
type admissionWalk struct {
	c      lowering.Ctx
	store  *admissions
	index  map[*statement.Statement]int
	low    []int
	open   []*statement.Statement
	frames []admissionFrame
}

// admissionFrame is one statement the walk is inside: its place in the order
// visited, its conjunct edges, and the next one to follow.
type admissionFrame struct {
	st    *statement.Statement
	at    int
	edges []*statement.Statement
	next  int
}

// run walks from root. Each pass of the loop follows one edge or leaves one
// statement, so it ends after the statements and edges reached, twice over.
func (w *admissionWalk) run(root *statement.Statement) {
	w.enter(root)
	for len(w.frames) > 0 {
		top := len(w.frames) - 1
		if f := &w.frames[top]; f.next < len(f.edges) {
			next := f.edges[f.next]
			f.next++
			w.follow(f.at, next)
			continue
		}
		f := w.frames[top]
		w.frames = w.frames[:top]
		if top > 0 {
			parent := w.frames[top-1].at
			w.low[parent] = min(w.low[parent], w.low[f.at])
		}
		if w.low[f.at] == f.at {
			w.settle(&f)
		}
	}
}

// enter visits st: it is given the next place in the order, pushed open, and
// its edges read.
func (w *admissionWalk) enter(st *statement.Statement) {
	at := len(w.low)
	w.index[st] = at
	w.low = append(w.low, at)
	w.open = append(w.open, st)
	edges := conjunctsOf(w.c, st)
	w.frames = append(w.frames, admissionFrame{st: st, at: at, edges: edges})
	w.store.visits++
	w.store.edges += len(edges)
}

// follow takes the edge from the statement at place from to next: settled
// already, there is nothing to walk; unvisited, it is entered; still open, it
// is on from's component.
func (w *admissionWalk) follow(from int, next *statement.Statement) {
	if _, settled := w.store.read[next]; settled {
		return
	}
	at, seen := w.index[next]
	if !seen {
		w.enter(next)
		return
	}
	w.low[from] = min(w.low[from], at)
}

// settle pops the component whose root is f's statement and records what each
// of its statements admits: one holding more than one statement, or one naming
// itself, is a cycle; any other reads over its edges, each settled.
func (w *admissionWalk) settle(f *admissionFrame) {
	from := len(w.open) - 1
	for w.open[from] != f.st {
		from--
	}
	members := w.open[from:]
	if len(members) > 1 || slices.Contains(f.edges, f.st) {
		for _, m := range members {
			w.store.read[m] = cycleRead()
		}
	} else {
		r := w.readOne(f.st, f.edges)
		w.store.read[f.st] = r
		w.store.merged += len(r.admitted.forms) + len(r.admitted.values)
	}
	w.open = w.open[:from]
}

// readOne reads st, whose conjunct edges each lead to a settled statement:
// what its own keywords say, in the dialect of the document holding it,
// conjoined with its allOf entries, the join of each union's branches and what
// its $ref lands on, in conjunctsOf's order. A statement stating no schema
// object admits nothing where it reads as `false`, and anything else. One
// whose kinds that leaves every kind and whose `properties` lists a member
// lowers to an object.
func (w *admissionWalk) readOne(st *statement.Statement, edges []*statement.Statement) *reading {
	if st.Truth != statement.Stated {
		if st.Truth.ReadsFalse() {
			return &reading{reads: 1}
		}
		return &reading{admitted: admitted{kinds: kindAny}, kinds: anyInstance, reads: 1}
	}
	r := w.readOwn(st)
	choice := choiceIn(st.Schema)
	for _, e := range edges[:len(st.AllOf)] {
		r.conjoin(w.store.read[e])
	}
	edges = edges[len(st.AllOf):]
	for _, branches := range [][]statement.Sub{choice.OneOf, choice.AnyOf} {
		if len(branches) > 0 {
			r.conjoin(w.join(edges[:len(branches)]))
			edges = edges[len(branches):]
		}
	}
	switch {
	case st.Ref != nil && len(edges) == 0:
		r.cut = true
	case st.Ref != nil:
		r.conjoin(w.store.read[edges[0]])
	case len(edges) > 0:
		r.kinds &= w.store.read[edges[0]].kinds
		r.cut = r.cut || w.store.read[edges[0]].cut
	}
	if r.kinds == anyInstance && len(objectIn(st.Schema).Properties) > 0 {
		r.kinds = kindObject
	}
	return r
}

// readOwn reads what st's own keywords say: what they admit, the kinds their
// type and value sets leave, and the members those sets hold.
func (w *admissionWalk) readOwn(st *statement.Statement) *reading {
	own := admission{c: w.c, reads: budget.New(budget.Unbounded()), members: budget.New(budget.Unbounded())}
	r := &reading{reads: 1, admitted: own.written(st.Schema, definesNullableAt(w.c, st))}
	r.kinds = declaredInstanceTypes(st.Schema) & valueKinds(w.c, st.Schema)
	r.members = min(own.members.Spent(), maxAdmissionMembers+1)
	if r.members > maxAdmissionMembers {
		r.past(r)
	}
	r.cut = own.members.Exhausted()
	r.loose = declaresDynamicRef(st.Schema) && st.Ref == nil
	return r
}

// join is what any of a union's settled branches admits, with what their reads
// took in.
func (w *admissionWalk) join(branches []*statement.Statement) *reading {
	first := w.store.read[branches[0]]
	out := &reading{admitted: first.admitted, kinds: first.kinds}
	out.took(first)
	for _, b := range branches[1:] {
		out.kinds |= w.store.read[b].kinds
		if out.took(w.store.read[b]) {
			out.admitted = out.admitted.or(w.store.read[b].admitted)
		}
	}
	return out
}
