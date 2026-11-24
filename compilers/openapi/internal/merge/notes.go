// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package merge

import (
	"cmp"
	"reflect"
	"slices"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/ir"
)

// heldNotes is what a property or a node holds of the notes a fold ranks, and
// the mark of what promoted its deprecation.
type heldNotes struct {
	CarriedNotes
	inferred ir.Inferred
}

// noteReading is how a fold reads one note, which detail names: what the
// declaration at i gives the field off its property (given), what a node holds
// (held), and whether two say the same. carry writes a note on those a fold
// tells later ones of, and takenOffNode reports whether the field may take one
// off a node, as it may not an example, which nothing shows its type to admit.
type noteReading[T any] struct {
	detail       string
	given        func(i int) (T, bool)
	held         func(notes heldNotes) (T, bool)
	same         func(a, b T) bool
	carry        func(notes *heldNotes, note T)
	takenOffNode bool
}

// readingOf is the noteReading of a note a property of foldingValue's
// declarations holds as a node does (held).
func readingOf[T any](foldingValue *folding, detail string, held func(notes heldNotes) (T, bool), same func(a, b T) bool,
	carry func(notes *heldNotes, note T),
) noteReading[T] {
	given := func(i int) (T, bool) {
		property := &foldingValue.decls[i].property
		return held(heldNotes{CarriedNotes{property.Docs, property.Deprecation, property.Examples}, property.Provenance.Inferred})
	}
	return noteReading[T]{detail: detail, given: given, held: held, same: same, carry: carry, takenOffNode: true}
}

// nodeNotes returns the notes the node id names holds as it stands, none where
// id names none. A node lists what chose its kind beside what promoted its
// deprecation, and a field names the promotion alone.
func nodeNotes(g *Merger, id ir.TypeID) heldNotes {
	td, ok := g.Reg[id]
	if !ok {
		return heldNotes{}
	}
	common := td.Common()
	held := heldNotes{CarriedNotes: CarriedNotes{common.Docs, common.Deprecation, common.Examples}}
	if ir.JoinInferred(common.Provenance.Inferred, g.Promotion) == common.Provenance.Inferred {
		held.inferred = g.Promotion
	}
	return held
}

// builtNodeNotes returns the notes the node declarationValue built holds, none
// where its type names another: the node of a member its model declares more
// than once itself keeps them, as does one a reference names and one naming a
// $ref.
func builtNodeNotes(g *Merger, declarationValue declaration) heldNotes {
	target := declarationValue.property.Type.Target
	td, ok := g.Reg[target]
	if !ok || !hoistedAt(td, declarationValue.sites()) {
		return heldNotes{}
	}
	return nodeNotes(g, target)
}

// SettledNotes is what a later fold reads of a member a fold settled, whose
// property holds one note of each kind whichever declaration gave it, and whose
// type is judged as its declarations' nodes stand.
type SettledNotes struct {
	// Node is what the node of the member's type is read holding where a fold
	// reads notes there (Merger.Carried): those of the nodes its type is built
	// on, judged so (settledNotes).
	Node CarriedNotes
	// Type is the type such a fold judges the member by (readAsNoted), written
	// at TypeAt: the one the member's declarations settle to judged so, where
	// its node is one of theirs. It names none where the fold would build it.
	Type   ir.TypeRef
	TypeAt []ir.Provenance
	// OffNodes holds each note the member took of a node that no declaration
	// of it gives off none. Any other is the member's own, and OnNode holds
	// each of those its property leaves on the node of its type.
	OffNodes CarriedNotes
	OnNode   heldNotes
}

// noteRank ranks what declarations give of one note: the model's own over one
// another model holds, which keeps it there, and one of a use, which a reader
// finds on the field, over one a node holds or its property carries off it,
// which a reader finds by the field's type.
type noteRank uint8

const (
	givesNone noteRank = iota
	broughtOfNode
	broughtOfUse
	ownOnNode
	ownOnProperty
)

// givenNote is what one declaration gives of a note, off its property, or off
// the node it built where its property gives none. ofNode is that a node is the
// note's home: one holds it, or a property carries it off one.
type givenNote[T any] struct {
	note   T
	rank   noteRank
	ofNode bool
}

// givenNotes returns what each declaration of foldingValue, a member an entry
// brings a declaration of, gives of the note reading reads.
func givenNotes[T any](foldingValue *folding, reading noteReading[T]) []givenNote[T] {
	given := make([]givenNote[T], len(foldingValue.decls))
	for i := range foldingValue.decls {
		given[i] = givenBy(foldingValue, reading, i)
	}
	return given
}

// givenBy returns what the declaration of foldingValue at i gives of the note
// reading reads. A note another model's node holds, or one the field may take
// off no node (noteReading.takenOffNode), is given only where the field's type
// names a node holding it, but for one a member a part settled left on its node
// (SettledNotes.OnNode), which is the member's own as one its property holds is.
func givenBy[T any](foldingValue *folding, reading noteReading[T], i int) givenNote[T] {
	declarationValue := foldingValue.decls[i]
	brought := isBrought(declarationValue)
	if note, gives := reading.given(i); gives {
		ofNode := carriedOff(foldingValue.g, declarationValue, reading, note)
		switch {
		case !brought:
			return givenNote[T]{note: note, rank: ownOnProperty, ofNode: ofNode}
		case ofNode:
			return givenNote[T]{note: note, rank: broughtOfNode, ofNode: true}
		default:
			return givenNote[T]{note: note, rank: broughtOfUse}
		}
	}
	if declarationValue.settled != nil && reading.takenOffNode {
		if note, left := reading.held(declarationValue.settled.OnNode); left {
			return givenNote[T]{note: note, rank: broughtOfUse, ofNode: true}
		}
	}
	note, gives := reading.held(builtNodeNotes(foldingValue.g, declarationValue))
	switch {
	case !gives || (brought || !reading.takenOffNode) && !onFieldsType(foldingValue, reading, note):
		return givenNote[T]{}
	case brought:
		return givenNote[T]{note: note, rank: broughtOfNode, ofNode: true}
	default:
		return givenNote[T]{note: note, rank: ownOnNode, ofNode: true}
	}
}

// carriedOff reports whether note, which reading reads and declarationValue's
// property holds, is of a node: the property carries it off the one its type
// names, or the member a part settled took it off one (SettledNotes.OffNodes).
func carriedOff[T any](g *Merger, declarationValue declaration, reading noteReading[T], note T) bool {
	off := g.Carried[declarationValue.property.Type.Target]
	if declarationValue.settled != nil {
		off = declarationValue.settled.OffNodes
	}
	carried, carries := reading.held(heldNotes{CarriedNotes: off})
	return carries && reading.same(carried, note)
}

// onFieldsType reports whether the node foldingValue's type names holds note,
// which reading reads, so a reader of the field finds it there. A part of a
// composition holding it is not the node.
func onFieldsType[T any](foldingValue *folding, reading noteReading[T], note T) bool {
	held, holds := reading.held(nodeNotes(foldingValue.g, foldingValue.merged.Type.Target))
	return holds && reading.same(held, note)
}

// settleNote returns what the field holds of the note reading reads, the one
// keptNote keeps, unless the field's type holds it and its home is a node
// (homeIsNode): a reader takes the field's note, then its type's. Each
// declaration giving another is reported, but the node of one of the model's
// own where a property of its own gives the note. Where the model alone
// declares the member, the field holds the last its properties give. A
// replacement only a key names stays on the node keeping the key, which
// resolves it (replacement.Resolve).
func settleNote[T any](foldingValue *folding, reading noteReading[T]) T {
	if !slices.ContainsFunc(foldingValue.decls, isBrought) {
		return lastWritten(foldingValue, reading.detail, func(i int, _ *ir.Property) (T, bool) { return reading.given(i) }, reading.same)
	}
	given := givenNotes(foldingValue, reading)
	at := keptNote(foldingValue, reading, given)
	if at < 0 {
		reportNoneKept(foldingValue, reading, given)
		return *new(T)
	}
	kept := given[at]
	reportOthers(foldingValue, reading.detail, at, func(i int) (T, bool) {
		return given[i].note, given[i].rank != givesNone && (given[i].rank != ownOnNode || kept.rank != ownOnProperty)
	}, reading.same, func(i int) bool { return given[i].rank == ownOnNode && isPart(foldingValue, i) })
	offNodes := !slices.ContainsFunc(given, func(givenValue givenNote[T]) bool {
		return givenValue.rank == broughtOfUse || givenValue.rank != givesNone && !givenValue.ofNode
	})
	if offNodes {
		reading.carry(&foldingValue.offNodes, kept.note)
	}
	if !homeIsNode(reading, given, kept.note) || !onFieldsType(foldingValue, reading, kept.note) {
		return kept.note
	}
	if !offNodes {
		reading.carry(&foldingValue.onNode, kept.note)
	}
	return *new(T)
}

// isPart reports whether foldingValue's field's type composes the node the
// declaration at i names, so a reader finds what that node holds there.
func isPart(foldingValue *folding, i int) bool {
	return slices.ContainsFunc(foldingValue.conjuncts, func(part conjunct) bool {
		return part.ref.Target == foldingValue.decls[i].property.Type.Target
	})
}

// homeIsNode reports whether a node is the home of kept, of the notes given: a
// node of one declaration holds it, or a property carries it off one, and the
// last that gives any off no node, which the field is the home of, if one does,
// gives another.
func homeIsNode[T any](reading noteReading[T], given []givenNote[T], kept T) bool {
	for _, givenValue := range slices.Backward(given) {
		if givenValue.rank != givesNone && !givenValue.ofNode {
			if reading.same(givenValue.note, kept) {
				return false
			}
			break
		}
	}
	return slices.ContainsFunc(given, func(givenValue givenNote[T]) bool {
		return givenValue.rank != givesNone && givenValue.ofNode && reading.same(givenValue.note, kept)
	})
}

// keptNote returns the index in given of the note the field keeps, of those
// ranked highest: of several of the model's own on its nodes the last the
// field's type holds, of several of nodes other models' declarations built the
// one ofTypesNode names, or the last the field's type holds, and otherwise the
// last, so the order of the model's entries decides between two of its own its
// type does not hold. It is -1 where none is given, or where none of several
// other models' is the type's.
func keptNote[T any](foldingValue *folding, reading noteReading[T], given []givenNote[T]) int {
	highest := givesNone
	for _, givenValue := range given {
		highest = max(highest, givenValue.rank)
	}
	last, onType, apart := -1, -1, false
	for i, givenValue := range given {
		if highest == givesNone || givenValue.rank != highest {
			continue
		}
		apart = apart || last >= 0 && !reading.same(given[last].note, givenValue.note)
		last = i
		if onFieldsType(foldingValue, reading, givenValue.note) {
			onType = i
		}
	}
	switch {
	case highest == ownOnNode && onType >= 0:
		return onType
	case highest == broughtOfNode && apart:
		if at := ofTypesNode(foldingValue, reading, func(i int) bool { return given[i].rank == highest }); at >= 0 {
			return at
		}
		return onType
	default:
		return last
	}
}

// ofTypesNode returns the declaration gives says gives a note whose node is read
// holding one and the field's type is built on, its types judged with each node
// holding its notes (notedJudgment): the winner's, then each it meets, the later
// first, as a built node adopts one (builder.adoptInto). It is -1 where the
// types compose, a model no declaration wrote standing for them, or where none
// gives any. A note a settled member took off a type it dropped is on no node.
func ofTypesNode[T any](foldingValue *folding, reading noteReading[T], gives func(i int) bool) int {
	noted := notedJudgment(foldingValue)
	if len(noted.conjuncts) > 0 {
		return -1
	}
	base, partners := noted.pos.meeting(func(i int) bool { return noted.cone[i] })
	for _, member := range append([]member{base}, partners...) {
		_, holds := reading.held(heldNotes{CarriedNotes: notesOf(noted.g, member.ref.Target)})
		if holds && gives(member.decl) {
			return member.decl
		}
	}
	return -1
}

// notedJudgment returns foldingValue's declarations judged with each node read
// holding the notes a property carries off it (Merger.noted), and each member a
// part settled by the type it settles to so (readAsNoted), once for the fold.
func notedJudgment(foldingValue *folding) *folding {
	switch {
	case foldingValue.g.noted:
		return foldingValue
	case foldingValue.reading == nil:
		decls := readAsNoted(foldingValue.decls)
		foldingValue.reading = newFolding(foldingValue.g.notedBeside(decls), decls, foldingValue.field)
	}
	return foldingValue.reading
}

// readAsNoted returns decls, each member a part settled of the type that
// judging its declarations with their notes gives it (SettledNotes.Type), which
// may be wider than the one it settled to: judged by the narrower one, a
// declaration beside it could lose to it what it did not while nodes held notes.
// One whose declarations so compose or build a node is judged by the type it
// settled to, and still can cost a declaration beside it.
func readAsNoted(decls []declaration) []declaration {
	out := slices.Clone(decls)
	for i, declarationValue := range out {
		if settled := declarationValue.settled; settled != nil && settled.Type.Target != "" {
			out[i].property.Type, out[i].typeAt = settled.Type, settled.TypeAt
		}
	}
	return out
}

// reportNoneKept reports, at each declaration giving one of several notes the
// field keeps none of, that it keeps none, where given holds any.
func reportNoneKept[T any](foldingValue *folding, reading noteReading[T], given []givenNote[T]) {
	for i, givenValue := range given {
		if givenValue.rank == givesNone {
			continue
		}
		foldingValue.note(i, ir.SeverityInfo, diag.DegradedConstruct,
			"declarations of field %q give its %s differently, and the type it takes is of none of them alone; kept none, "+
				"and the one the declaration at %s gives %s",
			foldingValue.merged.WireName, reading.detail, foldingValue.at(i), foldingValue.decls[i].keptAs())
	}
}

// settledNotes returns what a later fold reads of foldingValue's field, or nil
// where no entry brings a declaration, its nodes keeping their notes. Its types
// are judged with each node holding its own (notedJudgment). Where they
// compose, the model composing them holds no note and is none of theirs.
func settledNotes(foldingValue *folding) *SettledNotes {
	if !slices.ContainsFunc(foldingValue.decls, isBrought) {
		return nil
	}
	settled := &SettledNotes{OffNodes: foldingValue.offNodes.CarriedNotes, OnNode: foldingValue.onNode}
	noted := notedJudgment(foldingValue)
	if len(noted.conjuncts) > 0 {
		return settled
	}
	cone := func(i int) bool { return noted.cone[i] }
	base, partners := noted.pos.meeting(cone)
	settled.Node = metNotes(noted.g, base, partners)
	dry := builder{f: noted, dry: true}
	settled.Type = noted.pos.null(registry{noted.g.resolve}, cone, 0)
	settled.Type.Target = dry.meetNode(base, partners, noted.field, 0)
	settled.TypeAt = noted.g.typeSites(ir.Property{Type: settled.Type}, noted.decls)
	return settled
}

// metNotes returns the notes a node base and partners meet in is read holding:
// base's, then those of each partner it lacks, as a built node adopts them
// (builder.adoptInto).
func metNotes(g *Merger, base member, partners []member) CarriedNotes {
	node := notesOf(g, base.ref.Target)
	for _, member := range partners {
		held := notesOf(g, member.ref.Target)
		if reflect.ValueOf(node.Docs).IsZero() {
			node.Docs = held.Docs
		}
		node.Deprecation = cmp.Or(node.Deprecation, held.Deprecation)
	}
	return node
}

// notesOf returns the notes the node id names is read holding (Merger.resolve),
// none where id names none. Nodes that meet hold the same examples (detailOf).
func notesOf(g *Merger, id ir.TypeID) CarriedNotes {
	td, ok := g.resolve(id)
	if !ok {
		return CarriedNotes{}
	}
	common := td.Common()
	return CarriedNotes{Docs: common.Docs, Deprecation: common.Deprecation, Examples: common.Examples}
}
