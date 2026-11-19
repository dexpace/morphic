// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"cmp"
	"encoding/json/jsontext"
	"slices"
	"strconv"
	"strings"

	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/statement"
	"github.com/dexpace/morphic/ir"
)

// conjunctAtom is one conjunct of an allOf, as a disjoint report names it:
// its name below the composing schema, what it admits, and whether its read
// was cut short. Its null is the statement st's, or, for keywords read apart
// from a $ref or an allOf they stand beside, those of the schema words.
// written is where the conjunct is written apart from the allOf, whose report
// of admitting nothing would be its own (toldWhereWritten): what a $ref entry
// lands on, or a `false` entry itself, and nil for any other.
type conjunctAtom struct {
	name    string
	admits  kindSet
	st      *statement.Statement
	words   *statement.Schema
	written *statement.Statement
	// union is the branch set of a union read as one conjunct (unionAtoms),
	// whose null is any branch's.
	union []statement.Sub
	cut   bool
	// null marks an atom whose own read admits null, which admits holds
	// apart from (electedAtom), so it admits a value whatever st reads.
	null bool
}

// noValue is how a disjoint report names a conjunct admitting no value at all.
const noValue = "no value"

// disjointNames names what each conjunct of comp, the allOf s writes, states
// where they share no value: what their kinds leave (disjointAtoms), else,
// where those share a kind, what the election found apart (conjunctsApart).
func disjointNames(c lowering.Ctx, s *statement.Schema, comp allOfComposition) []string {
	if names := disjointAtoms(c, s, conjunctAtoms(c, s, comp)); len(names) > 0 {
		return names
	}
	return conjunctsApart(c, s)
}

// conjunctsApart names what each conjunct of the allOf s writes states where
// the election reads them sharing no value though their kinds share one, as
// two enums sharing no member do: a value set by its member count
// (decision.apart). A null they share (nullShared) is a value, so it names
// none there.
func conjunctsApart(c lowering.Ctx, s *statement.Schema) []string {
	names := decisionOf(c, s).apart
	if len(names) == 0 {
		return nil
	}
	if nullShared(c, s) {
		return nil
	}
	return names
}

// apartNames names what each conjunct of s, read as the election reads them,
// states where their reads share no value at all, the null included, joint
// being what they admit together: a value set by its member count, any other
// conjunct by the kinds and formats it admits, one admitting every kind left
// out, listed by what each states, so no order written orders them. It names
// none where a conjunct admits no value by itself, which its own schema
// reports where it is written, or fewer than two are named.
func apartNames(s *statement.Schema, comp allOfComposition, conjuncts []conjunct, joint admitted) []string {
	if joint.reachable() {
		return nil
	}
	names := make([]string, 0, len(conjuncts))
	for _, cj := range conjuncts {
		if !cj.read.reachable() {
			return nil
		}
		if word := readWords(cj.read); word != "" {
			names = append(names, conjunctName(s, comp, cj)+": "+word)
		}
	}
	if len(names) < 2 {
		return nil
	}
	slices.SortFunc(names, func(a, b string) int {
		_, wa, _ := strings.Cut(a, ": ")
		_, wb, _ := strings.Cut(b, ": ")
		return cmp.Or(strings.Compare(wa, wb), strings.Compare(a, b))
	})
	return names
}

// conjunctName is how a disjoint report names cj, a conjunct of comp, the
// allOf s writes, as conjunctAtoms names one: the keywords beside the allOf by
// the value set or the type they write, a branch by its path, a $ref written
// beside other keys by its own.
func conjunctName(s *statement.Schema, comp allOfComposition, cj conjunct) string {
	switch {
	case cj.branch != nil && splitRef(comp, cj.at):
		return comp.branchPath(cj.branch.pointer) + "/$ref"
	case cj.branch != nil:
		return comp.branchPath(cj.branch.pointer)
	case s.Const() != nil:
		return "const"
	case enumWritten(s):
		return "enum"
	default:
		return "type"
	}
}

// readWords spells what read admits for a disjoint report: a value set's
// member count, else its kinds, null aside, with the formats it is held to,
// and nothing for a read admitting every kind and held to none.
func readWords(read admitted) string {
	if read.fixed {
		if n := read.size(); n != 1 {
			return strconv.Itoa(n) + " values"
		}
		return "1 value"
	}
	formats := make([]string, 0, len(read.forms))
	for _, f := range read.forms {
		formats = append(formats, f.spelled)
	}
	kinds := read.kinds &^ kindNull
	switch {
	case len(formats) == 0 && kinds == anyInstance:
		return ""
	case len(formats) == 0:
		return instanceWords(kinds)
	case kinds == anyInstance:
		return "format " + strings.Join(formats, ", ")
	default:
		return instanceWords(kinds) + " of format " + strings.Join(formats, ", ")
	}
}

// contradictionShape describes what the refinement r of s, whose conjuncts
// share no value, lowered as, for diagDisjointConjuncts: the value set elected
// before them, else the union an inline principal writes, else what the
// keywords beside the allOf write, a family or a type, else the top type,
// where they write neither.
func contradictionShape(r refinement, s *statement.Schema) string {
	if won := electedSet(r, s); won != "" {
		return "the " + won + " it lowered as"
	}
	if r.principal != nil && r.principal.schema.Ref == nil && declaresUnion(r.principal.schema.Schema) {
		return unionShape
	}
	if kinds := declaredInstanceTypes(s); kinds != anyInstance {
		return "the " + instanceWords(kinds) + " it lowered as"
	}
	return "the top type it lowered as"
}

// disjointAtoms names what each of atoms, the conjuncts of the allOf s
// writes, admits where they share no value, and returns nil where they share
// one (docs/compilers/openapi.md#allof-disjoint-conjuncts), a null among them
// where every conjunct may admit it (nullShared). A read cut short decides nothing,
// and a conjunct admitting nothing whose schema reports it where it is written
// leaves the report to it. One conjunct contradicts nothing, so a lone
// `type: "null"` beside what takes its null away draws nil too, but one
// admitting nothing by itself is named, as nothing else reports it.
func disjointAtoms(c lowering.Ctx, s *statement.Schema, atoms []conjunctAtom) []string {
	joint := anyInstance
	for _, a := range atoms {
		if a.cut {
			return nil
		}
		joint &= a.admits
	}
	if joint != 0 {
		return nil
	}
	if nullShared(c, s) {
		return nil
	}
	var names []string
	void := false
	for _, a := range atoms {
		switch {
		case !empty(c, a):
			if a.admits != anyInstance {
				names = append(names, a.name+": "+instanceWords(a.admits))
			}
		case a.written != nil && toldWhereWritten(c, a.written):
			return nil
		default:
			names, void = append(names, a.name+": "+noValue), true
		}
	}
	if len(names) < 2 && !void {
		return nil
	}
	return names
}

// conjunctAtoms returns the conjuncts of comp, in order: the `type` s writes
// beside it, the value set it writes there (valueSetAtom), each union s writes
// beside it that shares no kind with the rest (unionAside) or declares no
// shape, else the one it passes over (passedOver), and each flattened branch:
// a wrapper by its own `type`, an entry nested past statement.MaxFlattenDepth
// as cut, an inline entry writing two unions as the one it elects and the
// other, and a $ref entry writing keywords that narrow what it admits as the
// reference and the keywords beside it.
func conjunctAtoms(c lowering.Ctx, s *statement.Schema, comp allOfComposition) []conjunctAtom {
	out := make([]conjunctAtom, 0, 4+len(comp.branches))
	out = append(out, conjunctAtom{name: "type", admits: declaredInstanceTypes(s), words: s})
	if set, ok := valueSetAtom(c, s); ok {
		out = append(out, set)
	}
	if unionAside(c, s) || declaresUnion(s) && !unionBranchesDeclareShape(c, s) {
		choice := s.ChoiceKeywords()
		out = append(out, unionAtoms(c, "oneOf", choice.OneOf)...)
		out = append(out, unionAtoms(c, "anyOf", choice.AnyOf)...)
	} else if passed, key := passedOver(c, s, s); passed != nil {
		out = append(out, unionAtoms(c, key, passed)...)
	}
	for i, e := range comp.branches {
		name := comp.branchPath(e.pointer)
		r := admissionOf(c, e.schema)
		ref, _ := declaredOf(c, e.schema)
		passed, key := passedOver(c, s, e.schema.Schema)
		switch {
		case e.wrapper:
			out = append(out, conjunctAtom{name: name, admits: declaredInstanceTypes(e.schema.Schema), words: e.schema.Schema})
		case splitRef(comp, i):
			out = append(out, conjunctAtom{name: name + "/$ref", admits: r.admits, st: e.schema, cut: r.cut, written: ref})
		case passed != nil && e.schema.Ref == nil:
			out = append(out, electedAtom(c, s, e, name, ref))
			out = append(out, unionAtoms(c, name+"/"+key, passed)...)
		default:
			a := conjunctAtom{name: name, admits: r.admits, st: e.schema, cut: r.cut || e.capped, written: ref}
			if e.schema.Truth.ReadsFalse() {
				a.written = e.schema
			}
			out = append(out, a)
		}
	}
	return out
}

// refinedAtoms returns the conjuncts of r's composition as conjunctAtoms reads
// them, but for an inline principal lowering to a value set whose keywords
// leave it no kind, which is read as the same keywords beside the allOf are:
// its `type`, its set and each union it writes, a set holding no value its own
// type admits saying so where it is written (valueSetAtom), as the election of
// competing keywords lowers it there (setElected).
func refinedAtoms(c lowering.Ctx, s *statement.Schema, r refinement) []conjunctAtom {
	atoms := conjunctAtoms(c, s, r.comp)
	e, ok := valueSetPrincipal(r)
	if !ok {
		return atoms
	}
	at := slices.IndexFunc(atoms, func(a conjunctAtom) bool { return a.st == e.schema })
	if at < 0 || atoms[at].admits != 0 {
		return atoms
	}
	name, bs := atoms[at].name, e.schema.Schema
	own := []conjunctAtom{{name: name + "/type", admits: declaredInstanceTypes(bs), words: bs}}
	if set, written := valueSetAtom(c, bs); written {
		set.name = name + "/" + set.name
		own = append(own, set)
	}
	choice := bs.ChoiceKeywords()
	own = append(own, unionAtoms(c, name+"/oneOf", choice.OneOf)...)
	own = append(own, unionAtoms(c, name+"/anyOf", choice.AnyOf)...)
	return slices.Concat(atoms[:at], own, atoms[at+1:])
}

// valueSetAtom returns the enum or the const s writes beside its allOf as a
// conjunct, as one an entry writes is (docs/compilers/openapi.md#allof-refinement):
// the kinds its members leave, null aside (valueKinds), its read cut where no
// reading converts them, and false where s writes neither. It is written at s,
// whose report that the set holds no value its type admits leaves this report
// none to make (toldWhereWritten).
func valueSetAtom(c lowering.Ctx, s *statement.Schema) (conjunctAtom, bool) {
	name, members := "enum", s.Enum()
	switch {
	case enumWritten(s):
	case s.Const() != nil:
		name, members = "const", []*yaml.Node{s.Const()}
	default:
		return conjunctAtom{}, false
	}
	unread := memberSetOf(c, valueSetOf{s, name}, members).unread
	return conjunctAtom{name: name, admits: valueKinds(c, s), words: s, written: c.Statements().Read(s), cut: unread}, true
}

// splitRef reports whether the branch at i of comp is the reference an entry
// writes beside other keys, the next branch, at its pointer, being the rest of
// that entry (statement.Entry).
func splitRef(comp allOfComposition, i int) bool {
	return i+1 < len(comp.branches) && comp.branches[i+1].pointer == comp.branches[i].pointer &&
		comp.branches[i].pointer != comp.pointer
}

// toldWhereWritten reports whether st, written apart from the allOf naming it
// and admitting no value, says so where it is written, so the allOf need not:
// it reads as `false` (docs/compilers/openapi.md#allof-false-branch), its
// value set holds no value its own type admits (docs/compilers/openapi.md#enum-const),
// its allOf's conjuncts share no kind, which this report reads at it, or no
// branch of a union beside its object body lives (keepsDeadUnion). A $ref
// landing on a schema admitting no value is read through, up to
// maxAdmissionReads hops. Anything else leaving st no value is reported
// nowhere else.
func toldWhereWritten(c lowering.Ctx, st *statement.Statement) bool {
	for range maxAdmissionReads {
		switch {
		case st.Truth.ReadsFalse():
			return true
		case st.Truth != statement.Stated:
			return false
		case (enumWritten(st.Schema) || st.Const() != nil) && declaredInstanceTypes(st.Schema)&valueKinds(c, st.Schema) == 0:
			return true
		case st.Ref == nil && len(st.AllOf) > 0:
			return conjunctsShareNothing(c, st.Schema)
		case st.Ref == nil && keepsDeadUnion(c, st.Schema):
			return true
		}
		next, _ := declaredOf(c, st)
		if next == nil || !empty(c, conjunctAtom{admits: admissionOf(c, next).admits, st: next}) {
			return false
		}
		st = next
	}
	return false
}

// conjunctsShareNothing reports whether the conjuncts of the allOf s writes,
// read as a disjoint report reads them, share no kind. A read cut short admits
// more than the schema does, never less, so it never makes them share nothing.
func conjunctsShareNothing(c lowering.Ctx, s *statement.Schema) bool {
	at, _ := siteOf(s)
	joint := anyInstance
	for _, a := range conjunctAtoms(c, s, allOfBranches(c, s, at.Pointer)) {
		joint &= a.admits
	}
	return joint == 0
}

// electedAtom is the inline entry e of s's allOf, named name, writing two
// unions, as a conjunct beside the one it passes over: its own keywords and
// the union it elects.
func electedAtom(c lowering.Ctx, s *statement.Schema, e allOfBranch, name string, ref *statement.Statement) conjunctAtom {
	w := newAdmissionReader(c)
	elected, _, _ := unionBranches(c, s, e.schema.Schema)
	read := w.written(e.schema.Schema, w.nullable).and(w.union(elected))
	return conjunctAtom{name: name, admits: read.kinds &^ kindNull, null: read.kinds&kindNull != 0, st: e.schema,
		cut: w.incomplete() || e.capped, written: ref}
}

// unionAtoms returns the union keyword names, written beside an allOf with
// branches, as one conjunct admitting what any branch does, and none where it
// writes none.
func unionAtoms(c lowering.Ctx, keyword string, branches []statement.Sub) []conjunctAtom {
	if len(branches) == 0 {
		return nil
	}
	atom := conjunctAtom{name: keyword, union: branches}
	for _, b := range branches {
		r := admissionOf(c, c.Statements().Sub(b))
		atom.admits |= r.admits
		atom.cut = atom.cut || r.cut
	}
	return []conjunctAtom{atom}
}

// nullShared reports whether the conjuncts of the allOf s writes may share a
// null: each may admit it wherever the IR holds it, as mayAdmitNull reads it,
// so one keeping it out, a union none of whose branches admits it among them,
// leaves none to share. Where the read passes the walk's budget, which
// conjuncts it reached would follow their order, so each is read on its own:
// one that keeps null out within its own budget decides, and otherwise they
// share it.
func nullShared(c lowering.Ctx, s *statement.Schema) bool {
	n := newNulls(c)
	n.variants = true
	if r := n.conjunct(c.Statements().Read(s), nullSite{}); !n.conjuncts.Exhausted() {
		return r.admits(c)
	}
	keepsOut := func(read func(n *nulls) nullReading) bool {
		n := newNulls(c)
		n.variants = true
		return read(&n).forbids(c) && !n.conjuncts.Exhausted()
	}
	if keepsOut(func(n *nulls) nullReading { return n.own(s) }) {
		return false
	}
	return !slices.ContainsFunc(conjunctsIn(c, s), func(b *statement.Statement) bool {
		return keepsOut(func(n *nulls) nullReading { return n.conjunct(b, nullSite{}) })
	})
}

// empty reports whether a admits no value by itself, the null included as
// nulls reads it, which makes it its own schema's to report rather than the
// allOf's.
func empty(c lowering.Ctx, a conjunctAtom) bool {
	if a.admits != 0 || a.null {
		return false
	}
	switch {
	case a.st != nil:
		return !mayAdmitNull(c, a.st, nil)
	case a.union != nil:
		return !slices.ContainsFunc(a.union, func(b statement.Sub) bool { return mayAdmitNull(c, c.Statements().Sub(b), nil) })
	default:
		return typeNullVerdict(a.words) != nullAdmitted
	}
}

// diagDisjointConjuncts is the one report an allOf whose conjuncts share no
// value draws, at the schema that writes them, which lowered to shape: a
// composition, or the value set an election passed it over for.
func diagDisjointConjuncts(c lowering.Ctx, comp allOfComposition, shape string) []ir.Diagnostic {
	if len(comp.disjoint) == 0 {
		return nil
	}
	return []ir.Diagnostic{c.DiagAt(ir.SeverityWarning, diag.DegradedConstruct, comp.pointer,
		"the conjuncts of %s share no value (%s), so no instance satisfies this schema, which %s cannot state",
		comp.subject(), strings.Join(comp.disjoint, ", "), shape)}
}

// composedShape is the shape a composition lowers to, for diagDisjointConjuncts.
const composedShape = "a composition"

// unionShape is the shape a position lowering to the union it elects of two
// standing alone lowers to, for diagDisjointConjuncts.
const unionShape = "the union it lowered as"

// diagUnionsShareNothing is the warning s, written at pointer and writing a
// oneOf and an anyOf, draws where the two share no value (pairApartNames), a
// null only where both may admit it, as the same two on its allOf's entries
// draw (docs/compilers/openapi.md#allof-disjoint-conjuncts): each named by its
// keyword, and the position lowered as shape.
func diagUnionsShareNothing(c lowering.Ctx, s *statement.Schema, pointer jsontext.Pointer, shape string) []ir.Diagnostic {
	comp := allOfComposition{pointer: pointer}
	if comp.disjoint = pairApartNames(c, s); len(comp.disjoint) == 0 {
		comp.disjoint = pairApart(c, s)
	}
	return diagDisjointConjuncts(c, comp, shape)
}

// pairApartNames names the oneOf and the anyOf s writes by the kinds each
// admits where those share none, and is nil where they share one or s
// writes but one of them; pairApart names them otherwise.
func pairApartNames(c lowering.Ctx, s *statement.Schema) []string {
	choice := s.ChoiceKeywords()
	if len(choice.OneOf) == 0 || len(choice.AnyOf) == 0 {
		return nil
	}
	return disjointAtoms(c, s, slices.Concat(unionAtoms(c, "oneOf", choice.OneOf), unionAtoms(c, "anyOf", choice.AnyOf)))
}

// pairApartOf keys what pairApart read of a position.
type pairApartOf struct{ s *statement.Schema }

// pairApart names the oneOf and the anyOf written for the position s, beside
// its allOf or on its inline entries at any depth, where they share no value
// as an admissionReader reads them, value sets included and a null only where
// both admit it: each by where it is written and what it admits (readWords).
// It is nil where they share one, the read is cut short, one admits nothing
// alone, or s and its entries write no oneOf and anyOf. Read once a run, so
// every spelling's lowering and report read one answer
// (docs/compilers/openapi.md#competing-keywords).
func pairApart(c lowering.Ctx, s *statement.Schema) []string {
	return lowering.Remembered(c, pairApartOf{s}, func() []string {
		comp := allOfBranches(c, s, "")
		if !writesUnionPair(s, comp) {
			return nil
		}
		w := newAdmissionReader(c)
		w.nullable = definesNullableAt(c, c.Statements().Read(s))
		p := pairReads{joint: admitted{kinds: kindAny}}
		p.read(&w, "", s)
		for _, e := range comp.branches {
			if e.schema.Ref == nil && declaresUnion(e.schema.Schema) {
				p.read(&w, comp.branchPath(e.pointer), e.schema.Schema)
			}
		}
		if p.joint.reachable() || p.alone || w.incomplete() || len(p.names) < 2 {
			return nil
		}
		slices.Sort(p.names)
		return p.names
	})
}

// pairReads is what pairApart has read of a position's unions: what they
// admit together, each named by what it admits, and whether one admits
// nothing alone, which its own schema reports.
type pairReads struct {
	joint admitted
	names []string
	alone bool
}

// read adds the oneOf and the anyOf x writes, at the path at from the
// position, as w reads each.
func (p *pairReads) read(w *admissionReader, at string, x *statement.Schema) {
	choice := x.ChoiceKeywords()
	for key, branches := range map[string][]statement.Sub{"oneOf": choice.OneOf, "anyOf": choice.AnyOf} {
		if len(branches) == 0 {
			continue
		}
		read := w.union(branches)
		p.joint, p.alone = p.joint.and(read), p.alone || !read.reachable()
		if word := readWords(read); word != "" {
			p.names = append(p.names, strings.TrimPrefix(at+"/"+key, "/")+": "+word)
		}
	}
}
