// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"encoding/json/jsontext"
	"slices"
	"strconv"

	"github.com/dexpace/morphic/compilers/openapi/internal/budget"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/reftable"
	"github.com/dexpace/morphic/compilers/openapi/internal/specrules"
	"github.com/dexpace/morphic/compilers/openapi/internal/statement"
)

// admission is what one statement admits, read once a run, and what the read
// met on the way.
type admission struct {
	// admitted is what an election compares.
	admitted admitted
	// admits is the kinds of instance the statement's keywords leave, null
	// aside; words those the keywords beside its $ref leave; and kinds those it
	// lowers to, which a bound is placed by (settleKinds).
	admits, words, kinds kindSet
	// firm is the kinds what the statement declares leaves, its `type` and
	// value sets conjoined with each conjunct's; claims the kinds the
	// kind-inference policy reads off its own shape keywords (claimedKinds),
	// united with the kinds of each conjunct whose kinds are claims.
	firm, claims kindSet
	// partial is the kinds some instance of which a keyword the read took in
	// refuses while admitting others (narrowedWithin), so admits holds every
	// instance of a kind only where partial does not hold it.
	partial kindSet
	// cut marks a statement on or reaching a cycle of conjunct edges, a $ref
	// landing on no schema or a value set no reading converts; loose one
	// meeting a keyword the read does not follow that could refuse a kind
	// (looseIn), a $dynamicRef the bindings read as no $ref among them, so
	// admits may claim more than the schema admits; inferred one whose kinds
	// are its claims, nothing it declares naming a kind.
	cut, loose, inferred bool
	// reads and members are the schemas and value-set members the read takes
	// in, each counted per path and held to one past a reader's cap.
	reads, members int
}

// conjoin adds what r's conjunct child admits, declares and claims, and what
// its read took in, to r. A kind child admits only in part is admitted so by
// both.
func (r *admission) conjoin(child *admission) {
	r.admits &= child.admits
	r.partial |= child.partial
	r.conjoinKinds(child)
	if r.addChildReadCostAndCheckCaps(child) {
		r.admitted = r.admitted.and(child.admitted)
	}
}

// conjoinKinds adds what child declares to what r declares, and its kinds to
// r's claims where they are claims: JSON Schema applies a shape keyword to the
// instances of its kind and passes every other, so a claim narrows nothing a
// conjunct beside it declares (docs/compilers/openapi.md#shape-applicator).
func (r *admission) conjoinKinds(child *admission) {
	r.firm &= child.firm
	if child.inferred {
		r.claims |= child.kinds
	}
}

// settleKinds derives the kinds r lowers to once its conjuncts are read: what
// it declares where that names a kind, else what it claims where that names
// one, inferred, else every kind.
func (r *admission) settleKinds() {
	switch {
	case r.firm != anyInstance:
		r.kinds, r.inferred = r.firm, false
	case r.claims != 0 && r.claims != anyInstance:
		r.kinds, r.inferred = r.claims, true
	default:
		r.kinds, r.inferred = anyInstance, false
	}
}

// addChildReadCostAndCheckCaps adds what child's read took in and met to r's, and reports whether r
// is still within both caps. One past either stops (past).
func (r *admission) addChildReadCostAndCheckCaps(child *admission) bool {
	r.cut = r.cut || child.cut
	r.loose = r.loose || child.loose
	r.reads = min(r.reads+child.reads, maxAdmissionReads+1)
	r.members = min(r.members+child.members, maxAdmissionMembers+1)
	if r.within() {
		return true
	}
	r.past(child)
	return false
}

// within reports whether r's read takes in no more schemas than
// maxAdmissionReads and no more value-set members than maxAdmissionMembers.
func (r *admission) within() bool {
	return r.reads <= maxAdmissionReads && r.members <= maxAdmissionMembers
}

// past is r once its read takes in more than a cap: what an election compares
// admits every kind and holds no value set or format, so what it merges stops
// growing with the paths to it, as the formats a read conjoins are one per
// schema taken in, and the election reads it as cut short (admissionReader.of). The
// kinds r admits, declares and lowers to are still read over every edge, each
// statement once, so they stay whole and cut stays false. The element schema
// is kept, child's where r holds none, which missingArrayItemsConjoined reads
// past a cut.
func (r *admission) past(child *admission) {
	items := r.admitted.items
	if items == nil {
		items = child.admitted.items
	}
	r.admitted = admitted{kinds: kindAny, items: items}
}

// cycleRead is what a statement on a cycle of conjunct edges reads as: cut,
// whichever of its statements the read entered at, and past every reader's
// cap.
func cycleRead() *admission {
	return &admission{admitted: admitted{kinds: kindAny}, admits: anyInstance, words: anyInstance, kinds: anyInstance,
		firm: anyInstance, partial: anyInstance, cut: true, reads: maxAdmissionReads + 1}
}

// admissionsIn keys a run's admissions. A statement the bindings do not place
// is read in the dialect of the context asking (definesNullableAt), so each
// dialect keeps its own.
type admissionsIn struct {
	nullable bool
}

// admissions is the run's store of what each statement admits; how many
// statements and conjunct edges its walks have visited, which the tests hold to
// one each; and how many formats and value-set members the admissions it
// settled hold, which they hold to grow with the document and not with its
// paths.
type admissions struct {
	read                  map[*statement.Statement]*admission
	visits, edges, merged int
}

// admissionOf returns what st admits, read once a run. The first ask walks
// every statement st reaches by its conjunct edges that no read has settled,
// and settles each.
func admissionOf(loweringContext lowering.Ctx, st *statement.Statement) *admission {
	store := admissionStore(loweringContext)
	if admission, ok := store.read[st]; ok {
		return admission
	}
	admissionWalkValue := admissionWalk{c: loweringContext, store: store, index: map[*statement.Statement]int{}}
	admissionWalkValue.run(st)
	return store.read[st]
}

// admissionStore returns the run's admissions in the dialect loweringContext reads an
// unplaced schema in.
func admissionStore(loweringContext lowering.Ctx) *admissions {
	return lowering.ReadOncePerRun(loweringContext, admissionsIn{loweringContext.SchemaRules().Defines("nullable")}, func() *admissions {
		return &admissions{read: map[*statement.Statement]*admission{}}
	})
}

// conjunctsOf returns the statements st conjoins with, each conjunct edge in
// the order the read conjoins them, a statement as often as st names it: the
// reference written beside its keywords (statement.Schema.BesideRef) and its
// allOf entries, its oneOf and then anyOf branches, and the statement its
// $ref's first hop lands on, or, for a $dynamicRef the bindings read as no
// $ref, the one it expands to (dynamicExpansionOf). A `properties` or an
// `items` consumes the instance, so it is no edge.
func conjunctsOf(loweringContext lowering.Ctx, st *statement.Statement) []*statement.Statement {
	if st.Truth != statement.Stated {
		return nil
	}
	choice := st.ChoiceKeywords()
	subs := slices.Concat(st.AllOf, choice.OneOf, choice.AnyOf)
	out := make([]*statement.Statement, 0, len(subs)+2)
	if st.BesideRef != nil {
		out = append(out, loweringContext.Statements().Read(st.BesideRef))
	}
	for _, b := range subs {
		out = append(out, loweringContext.Statements().Subschema(b))
	}
	if next, _ := declaredOf(loweringContext, st); next != nil {
		out = append(out, next)
	} else if at, ok := dynamicExpansionOf(loweringContext, st); ok {
		out = append(out, loweringContext.Statements().At(at))
	}
	return out
}

// dynamicExpansionOf returns the position the $dynamicRef or $recursiveRef st
// writes names, read at st's own position in its own document, where dynamic
// scope is static (dynamicTarget), and false where it writes none the bindings
// do not read as a $ref, names none, or is placed nowhere. One an allOf entry
// writes names none: no position expands it (keepEntryDynamicRef), so it masks
// no kind (docs/compilers/openapi.md#allof-refinement).
func dynamicExpansionOf(loweringContext lowering.Ctx, st *statement.Statement) (reftable.DocumentPointer, bool) {
	at, placed := siteOf(st.Schema)
	if st.Ref != nil || !placed || !declaresDynamicRef(st.Schema) || writtenAsEntry(at.Pointer) {
		return reftable.DocumentPointer{}, false
	}
	in, _ := lowering.InDocument(loweringContext, at.Doc)
	return dynamicTarget(in, st.Schema, at.Pointer)
}

// writtenAsEntry reports whether pointer names an entry of an allOf.
func writtenAsEntry(pointer jsontext.Pointer) bool {
	_, err := strconv.ParseUint(pointer.LastToken(), 10, 64)
	return err == nil && pointer.Parent().LastToken() == "allOf"
}

// admissionWalk is one walk settling the admissions a first ask needs, by
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
		if admissionFrame := &w.frames[top]; admissionFrame.next < len(admissionFrame.edges) {
			next := admissionFrame.edges[admissionFrame.next]
			admissionFrame.next++
			w.follow(admissionFrame.at, next)
			continue
		}
		admissionFrameValue := w.frames[top]
		w.frames = w.frames[:top]
		if top > 0 {
			parent := w.frames[top-1].at
			w.low[parent] = min(w.low[parent], w.low[admissionFrameValue.at])
		}
		if w.low[admissionFrameValue.at] == admissionFrameValue.at {
			w.settle(&admissionFrameValue)
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

// settle pops the component whose root is admissionFrameValue's statement and records what each
// of its statements admits: one holding more than one statement, or one naming
// itself, is a cycle; any other reads over its edges, each settled.
func (w *admissionWalk) settle(admissionFrameValue *admissionFrame) {
	from := len(w.open) - 1
	for w.open[from] != admissionFrameValue.st {
		from--
	}
	members := w.open[from:]
	if len(members) > 1 || slices.Contains(admissionFrameValue.edges, admissionFrameValue.st) {
		for _, schemaStatement := range members {
			w.store.read[schemaStatement] = cycleRead()
		}
	} else {
		admission := w.readOne(admissionFrameValue.st, admissionFrameValue.edges)
		w.store.read[admissionFrameValue.st] = admission
		w.store.merged += len(admission.admitted.forms) + admission.admitted.size()
	}
	w.open = w.open[:from]
}

// readOne reads st, whose conjunct edges each lead to a settled statement:
// what its own keywords say, in the dialect of the document holding it,
// conjoined with its allOf entries, the join of each union's branches and what
// its $ref lands on, in conjunctsOf's order, its kinds settled last
// (settleKinds). A statement stating no schema object admits nothing where it
// reads as `false`, and anything else.
func (w *admissionWalk) readOne(st *statement.Statement, edges []*statement.Statement) *admission {
	if st.Truth != statement.Stated {
		if st.Truth.ReadsFalse() {
			return &admission{reads: 1}
		}
		return &admission{admitted: admitted{kinds: kindAny}, admits: anyInstance, words: anyInstance, kinds: anyInstance,
			firm: anyInstance, reads: 1}
	}
	admissionValue := w.readOwn(st)
	choice := st.ChoiceKeywords()
	conjuncts := len(st.AllOf)
	if st.BesideRef != nil {
		conjuncts++
	}
	for _, schemaStatement := range edges[:conjuncts] {
		admissionValue.conjoin(w.store.read[schemaStatement])
	}
	edges = edges[conjuncts:]
	for i, branches := range [][]statement.Subschema{choice.OneOf, choice.AnyOf} {
		if len(branches) > 0 {
			admissionValue.conjoin(w.join(edges[:len(branches)], i == 0))
			edges = edges[len(branches):]
		}
	}
	admissionValue.words = admissionValue.admits
	switch {
	case st.Ref != nil && len(edges) == 0:
		admissionValue.cut = true
	case st.Ref != nil:
		admissionValue.conjoin(w.store.read[edges[0]])
	case len(edges) > 0:
		target := w.store.read[edges[0]]
		admissionValue.admits &= target.admits
		admissionValue.partial |= target.partial
		admissionValue.conjoinKinds(target)
		admissionValue.cut = admissionValue.cut || target.cut
		admissionValue.loose = admissionValue.loose || target.loose
	}
	admissionValue.settleKinds()
	return admissionValue
}

// readOwn reads what st's own keywords say: what they admit, the kinds their
// type and value sets leave, and the members those sets hold.
func (w *admissionWalk) readOwn(st *statement.Statement) *admission {
	own := admissionReader{c: w.c, reads: budget.New(budget.Unbounded()), members: budget.New(budget.Unbounded())}
	admissionValue := &admission{reads: 1, admitted: own.written(st.Schema, definesNullableAt(w.c, st))}
	admissionValue.admits = declaredInstanceTypes(st.Schema) & valueKinds(w.c, st.Schema)
	admissionValue.firm, admissionValue.claims = admissionValue.admits, claimedKinds(w.c, st.Schema)
	admissionValue.partial = narrowedWithin(w.c.Statements(), st.Schema)
	admissionValue.members = min(own.members.Spent(), maxAdmissionMembers+1)
	if !admissionValue.within() {
		admissionValue.past(admissionValue)
	}
	admissionValue.cut = own.members.Exhausted()
	admissionValue.loose = declaresDynamicRef(st.Schema) && st.Ref == nil || looseIn(st.Schema)
	return admissionValue
}

// join is what any of a union's settled branches admits, declares and lowers
// to, with what their reads took in: a branch reading as every kind leaves
// what the join declares open, so the kinds the branches lower to reach the
// position as a claim where any branch's is one. A kind is admitted whole
// where some branch admits it whole and, for an exclusive union, which refuses
// an instance two branches admit, no other branch admits it at all.
func (w *admissionWalk) join(branches []*statement.Statement, exclusive bool) *admission {
	first := w.store.read[branches[0]]
	out := &admission{admitted: first.admitted, admits: first.admits, kinds: first.kinds, firm: first.firm,
		inferred: first.inferred}
	out.addChildReadCostAndCheckCaps(first)
	whole, shared := first.admits&^first.partial, kindSet(0)
	for _, b := range branches[1:] {
		branch := w.store.read[b]
		shared |= out.admits & branch.admits
		whole |= branch.admits &^ branch.partial
		out.admits |= branch.admits
		out.kinds |= branch.kinds
		out.firm |= branch.firm
		out.inferred = out.inferred || branch.inferred
		if out.addChildReadCostAndCheckCaps(branch) {
			out.admitted = out.admitted.or(branch.admitted)
		}
	}
	out.partial = out.admits &^ whole
	if exclusive {
		out.partial |= shared
	}
	return out
}

// narrowedWithin returns the kinds some instance of which schema's own keywords may
// refuse while admitting others: every kind for a value set, a number for a
// numeric bound, a string for a string bound, a string and a number for a
// format, and an array or an object for a keyword applying to that kind alone
// (specrules.SlotOf), `dependentSchemas` and the drafts' `dependencies` among them,
// but one refusing none (refusesNothing). A `type` refuses a kind whole, which
// admits holds; a `not` or an `if` makes the read loose (looseIn); and a
// keyword written null is left out.
func narrowedWithin(memoizingSchemaReader *statement.MemoizingSchemaReader, schema *statement.Schema) kindSet {
	var out kindSet
	for _, key := range schema.Keys {
		if key.Null {
			continue
		}
		switch key.Slot {
		case specrules.SlotValues:
			out |= anyInstance
		case specrules.SlotFormat:
			if _, ok := formOf(schema); ok {
				out |= kindString | kindNumber
			}
		case specrules.SlotNumber:
			out |= kindNumber
		case specrules.SlotText:
			out |= kindString
		case specrules.SlotArray:
			if !refusesNothing(memoizingSchemaReader, schema, key.Name) {
				out |= kindArray
			}
		case specrules.SlotObject:
			if !refusesNothing(memoizingSchemaReader, schema, key.Name) {
				out |= kindObject
			}
		case specrules.SlotLogic:
			if key.Name == "dependentSchemas" || key.Name == "dependencies" {
				out |= kindObject
			}
		default:
		}
	}
	return out
}

// refusesNothing reports whether the keyword name schema writes refuses no instance
// of the kind it applies to: a schema applied to each of an instance's parts
// that states nothing, `true` or `{}`, or a `properties`, `patternProperties`
// or `prefixItems` each of whose schemas states nothing. Any other array or
// object keyword is read as refusing some.
func refusesNothing(memoizingSchemaReader *statement.MemoizingSchemaReader, schema *statement.Schema, name string) bool {
	statesNothing := func(sub statement.Subschema) bool {
		return sub.SchemaValueForm() == statement.Anything || sub.SchemaValueForm() == statement.Stated && len(memoizingSchemaReader.Subschema(sub).Keys) == 0
	}
	switch name {
	case "items":
		return statesNothing(schema.ArrayKeywords().Items)
	case "unevaluatedItems":
		return statesNothing(schema.ArrayKeywords().UnevaluatedItems)
	case "prefixItems":
		return !slices.ContainsFunc(schema.ArrayKeywords().PrefixItems, func(sub statement.Subschema) bool { return !statesNothing(sub) })
	case "additionalProperties":
		return statesNothing(schema.ObjectKeywords().AdditionalProperties)
	case "propertyNames":
		return statesNothing(schema.ObjectKeywords().PropertyNames)
	case "unevaluatedProperties":
		return statesNothing(schema.ObjectKeywords().UnevaluatedProperties)
	case "properties":
		return !slices.ContainsFunc(schema.ObjectKeywords().Properties, func(sub statement.Subschema) bool { return !statesNothing(sub) })
	case "patternProperties":
		return !slices.ContainsFunc(schema.ObjectKeywords().PatternProperties, func(sub statement.Subschema) bool { return !statesNothing(sub) })
	default:
		return false
	}
}

// looseIn reports whether schema writes a keyword the read of what it admits does
// not follow and that could refuse a value of some kind: a `not`, an `if` or a
// `dependentSchemas` holding a schema. One written null is the keyword left
// out (docs/compilers/openapi.md#null-schema), and refuses nothing.
func looseIn(schema *statement.Schema) bool {
	if schema.Logic == nil {
		return false
	}
	logic := schema.Logic
	return written(logic.Not) || written(logic.If) || slices.ContainsFunc(logic.DependentSchemas, written)
}
