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
	union []statement.Subschema
	cut   bool
	// null marks an atom whose own read admits null, which admits holds
	// apart from (electedAtom), so it admits a value whatever st reads.
	null bool
}

// noValue is how a disjoint report names a conjunct admitting no value at all.
const noValue = "no value"

// disjointNames names what each conjunct of comp, the allOf schema writes,
// states where they share no value: what their kinds leave (disjointAtoms),
// else, where those share a kind, what the election found apart
// (conjunctsApart).
func disjointNames(loweringContext lowering.Ctx, schema *statement.Schema, comp allOfComposition) []string {
	if names := disjointAtoms(loweringContext, schema, conjunctAtoms(loweringContext, schema, comp)); len(names) > 0 {
		return names
	}
	return conjunctsApart(loweringContext, schema)
}

// conjunctsApart names what each conjunct of the allOf schema writes states
// where the election reads them sharing no value though their kinds share one,
// as two enums sharing no member do: a value set by its member count
// (decision.apart). A null they share (nullShared) is a value, so it names none
// there.
func conjunctsApart(loweringContext lowering.Ctx, schema *statement.Schema) []string {
	names := decisionOf(loweringContext, schema).apart
	if len(names) == 0 {
		return nil
	}
	if nullShared(loweringContext, schema) {
		return nil
	}
	return names
}

// apartNames names what each conjunct of schema, read as the election reads
// them, states where their reads share no value at all, the null included,
// joint being what they admit together: a value set by its member count, any
// other conjunct by the kinds and formats it admits, one admitting every kind
// left out, listed by what each states, so no order written orders them. It
// names none where a conjunct admits no value by itself, which its own schema
// reports where it is written, or fewer than two are named.
func apartNames(schema *statement.Schema, comp allOfComposition, conjuncts []conjunct, joint admitted) []string {
	if joint.reachable() {
		return nil
	}
	names := make([]string, 0, len(conjuncts))
	for _, cj := range conjuncts {
		if !cj.read.reachable() {
			return nil
		}
		if word := readWords(cj.read); word != "" {
			names = append(names, conjunctName(schema, comp, cj)+": "+word)
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

// conjunctName is how a disjoint report names cj, a conjunct of comp, the allOf
// schema writes, as conjunctAtoms names one: the keywords beside the allOf by
// the value set or the type they write, a branch by its path, a $ref written
// beside other keys by its own.
func conjunctName(schema *statement.Schema, comp allOfComposition, cj conjunct) string {
	switch {
	case cj.branch != nil && splitRef(comp, cj.at):
		return comp.branchPath(cj.branch.pointer) + "/$ref"
	case cj.branch != nil:
		return comp.branchPath(cj.branch.pointer)
	case schema.Const() != nil:
		return "const"
	case enumWritten(schema):
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
	for _, scalarForm := range read.forms {
		formats = append(formats, scalarForm.spelled)
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

// contradictionShape describes what the refinement refinementValue of schema,
// whose conjuncts share no value, lowered as, for diagDisjointConjuncts: the
// value set elected before them, else the union an inline principal writes,
// else what the keywords beside the allOf write, a family or a type, else the
// top type, where they write neither.
func contradictionShape(refinementValue refinement, schema *statement.Schema) string {
	if won := electedSet(refinementValue, schema); won != "" {
		return "the " + won + " it lowered as"
	}
	if refinementValue.principal != nil && refinementValue.principal.schema.Ref == nil && declaresUnion(refinementValue.principal.schema.Schema) {
		return unionShape
	}
	if kinds := declaredInstanceTypes(schema); kinds != anyInstance {
		return "the " + instanceWords(kinds) + " it lowered as"
	}
	return "the top type it lowered as"
}

// disjointAtoms names what each of atoms, the conjuncts of the allOf schema
// writes, admits where they share no value, and returns nil where they share
// one (docs/compilers/openapi.md#allof-disjoint-conjuncts), a null among them
// where every conjunct may admit it (nullShared). A read cut short decides nothing,
// and a conjunct admitting nothing whose schema reports it where it is written
// leaves the report to it. One conjunct contradicts nothing, so a lone
// `type: "null"` beside what takes its null away draws nil too, but one
// admitting nothing by itself is named, as nothing else reports it.
func disjointAtoms(loweringContext lowering.Ctx, schema *statement.Schema, atoms []conjunctAtom) []string {
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
	if nullShared(loweringContext, schema) {
		return nil
	}
	var names []string
	void := false
	for _, a := range atoms {
		switch {
		case !empty(loweringContext, a):
			if a.admits != anyInstance {
				names = append(names, a.name+": "+instanceWords(a.admits))
			}
		case a.written != nil && toldWhereWritten(loweringContext, a.written):
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

// conjunctAtoms returns the conjuncts of comp, in order: the `type` schema
// writes beside it, the value set it writes there (valueSetAtom), each union
// schema writes beside it that shares no kind with the rest (unionAside) or
// declares no shape, else the one it passes over (passedOver), and each
// flattened branch: a wrapper by its own `type`, an entry nested past
// statement.MaxFlattenDepth as cut, an inline entry writing two unions as the
// one it elects and the other, and a $ref entry writing keywords that narrow
// what it admits as the reference and the keywords beside it.
func conjunctAtoms(loweringContext lowering.Ctx, schema *statement.Schema, comp allOfComposition) []conjunctAtom {
	out := make([]conjunctAtom, 0, 4+len(comp.branches))
	out = append(out, conjunctAtom{name: "type", admits: declaredInstanceTypes(schema), words: schema})
	if set, ok := valueSetAtom(loweringContext, schema); ok {
		out = append(out, set)
	}
	if unionAside(loweringContext, schema) || declaresUnion(schema) && !unionBranchesDeclareShape(loweringContext, schema) {
		choice := schema.ChoiceKeywords()
		out = append(out, unionAtoms(loweringContext, "oneOf", choice.OneOf)...)
		out = append(out, unionAtoms(loweringContext, "anyOf", choice.AnyOf)...)
	} else if passed, key := passedOver(loweringContext, schema, schema); passed != nil {
		out = append(out, unionAtoms(loweringContext, key, passed)...)
	}
	for i, allOfBranch := range comp.branches {
		name := comp.branchPath(allOfBranch.pointer)
		admission := admissionOf(loweringContext, allOfBranch.schema)
		ref, _ := declaredOf(loweringContext, allOfBranch.schema)
		passed, key := passedOver(loweringContext, schema, allOfBranch.schema.Schema)
		switch {
		case allOfBranch.wrapper:
			out = append(out, conjunctAtom{name: name, admits: declaredInstanceTypes(allOfBranch.schema.Schema), words: allOfBranch.schema.Schema})
		case splitRef(comp, i):
			out = append(out, conjunctAtom{name: name + "/$ref", admits: admission.admits, st: allOfBranch.schema, cut: admission.cut, written: ref})
		case passed != nil && allOfBranch.schema.Ref == nil:
			out = append(out, electedAtom(loweringContext, schema, allOfBranch, name, ref))
			out = append(out, unionAtoms(loweringContext, name+"/"+key, passed)...)
		default:
			a := conjunctAtom{name: name, admits: admission.admits, st: allOfBranch.schema, cut: admission.cut || allOfBranch.capped, written: ref}
			if allOfBranch.schema.Truth.ReadsFalse() {
				a.written = allOfBranch.schema
			}
			out = append(out, a)
		}
	}
	return out
}

// refinedAtoms returns the conjuncts of refinementValue's composition as
// conjunctAtoms reads them, but for an inline principal lowering to a value set
// whose keywords leave it no kind, which is read as the same keywords beside
// the allOf are: its `type`, its set and each union it writes, a set holding no
// value its own type admits saying so where it is written (valueSetAtom), as
// the election of competing keywords lowers it there (setElected).
func refinedAtoms(loweringContext lowering.Ctx, schema *statement.Schema, refinementValue refinement) []conjunctAtom {
	atoms := conjunctAtoms(loweringContext, schema, refinementValue.comp)
	allOfBranch, ok := valueSetPrincipal(refinementValue)
	if !ok {
		return atoms
	}
	at := slices.IndexFunc(atoms, func(a conjunctAtom) bool { return a.st == allOfBranch.schema })
	if at < 0 || atoms[at].admits != 0 {
		return atoms
	}
	name, bs := atoms[at].name, allOfBranch.schema.Schema
	own := []conjunctAtom{{name: name + "/type", admits: declaredInstanceTypes(bs), words: bs}}
	if set, written := valueSetAtom(loweringContext, bs); written {
		set.name = name + "/" + set.name
		own = append(own, set)
	}
	choice := bs.ChoiceKeywords()
	own = append(own, unionAtoms(loweringContext, name+"/oneOf", choice.OneOf)...)
	own = append(own, unionAtoms(loweringContext, name+"/anyOf", choice.AnyOf)...)
	return slices.Concat(atoms[:at], own, atoms[at+1:])
}

// valueSetAtom returns the enum or the const schema writes beside its allOf as
// a conjunct, as one an entry writes is
// (docs/compilers/openapi.md#allof-refinement): the kinds its members leave,
// null aside (valueKinds), its read cut where no reading converts them, and
// false where schema writes neither. It is written at schema, whose report that
// the set holds no value its type admits leaves this report none to make
// (toldWhereWritten).
func valueSetAtom(loweringContext lowering.Ctx, schema *statement.Schema) (conjunctAtom, bool) {
	name, members := "enum", schema.Enum()
	switch {
	case enumWritten(schema):
	case schema.Const() != nil:
		name, members = "const", []*yaml.Node{schema.Const()}
	default:
		return conjunctAtom{}, false
	}
	unread := memberSetOf(loweringContext, valueSetOf{schema, name}, members).unread
	return conjunctAtom{name: name, admits: valueKinds(loweringContext, schema), words: schema, written: loweringContext.Statements().Read(schema), cut: unread}, true
}

// splitRef reports whether the branch at i of comp is the reference an entry
// writes beside other keys, the next branch, at its pointer, being the rest of
// that entry (statement.AllOfConjunct).
func splitRef(comp allOfComposition, i int) bool {
	return i+1 < len(comp.branches) && comp.branches[i+1].pointer == comp.branches[i].pointer &&
		comp.branches[i].pointer != comp.pointer
}

// toldWhereWritten reports whether st, written apart from the allOf naming it
// and admitting no value, says so where it is written, so the allOf need not:
// it reads as `false` (docs/compilers/openapi.md#allof-false-branch), its
// value set holds no value its own type admits (docs/compilers/openapi.md#enum-const),
// its allOf's conjuncts share no kind, which this report reads at it, no
// branch of a union beside its object body lives (keepsDeadUnion), or a union
// shares no kind with a body that is no Model (keepsUnionApartFromBody). A $ref
// to a schema admitting no value is read through, up to maxAdmissionReads
// hops; nothing else is reported elsewhere.
func toldWhereWritten(loweringContext lowering.Ctx, st *statement.Statement) bool {
	for range maxAdmissionReads {
		switch {
		case st.Truth.ReadsFalse():
			return true
		case st.Truth != statement.Stated:
			return false
		case (enumWritten(st.Schema) || st.Const() != nil) && declaredInstanceTypes(st.Schema)&valueKinds(loweringContext, st.Schema) == 0:
			return true
		case st.Ref == nil && len(st.AllOf) > 0:
			return conjunctsShareNothing(loweringContext, st.Schema)
		case st.Ref == nil && (keepsDeadUnion(loweringContext, st.Schema) || keepsUnionApartFromBody(loweringContext, st.Schema)):
			return true
		}
		next, _ := declaredOf(loweringContext, st)
		if next == nil || !empty(loweringContext, conjunctAtom{admits: admissionOf(loweringContext, next).admits, st: next}) {
			return false
		}
		st = next
	}
	return false
}

// conjunctsShareNothing reports whether the conjuncts of the allOf schema
// writes, read as a disjoint report reads them, share no kind. A read cut short
// admits more than the schema does, never less, so it never makes them share
// nothing.
func conjunctsShareNothing(loweringContext lowering.Ctx, schema *statement.Schema) bool {
	at, _ := siteOf(schema)
	joint := anyInstance
	for _, a := range conjunctAtoms(loweringContext, schema, allOfBranches(loweringContext, schema, at.Pointer)) {
		joint &= a.admits
	}
	return joint == 0
}

// electedAtom is the inline entry allOfBranchValue of schema's allOf, named
// name, writing two unions, as a conjunct beside the one it passes over: its
// own keywords and the union it elects.
func electedAtom(loweringContext lowering.Ctx, schema *statement.Schema, allOfBranchValue allOfBranch, name string, ref *statement.Statement) conjunctAtom {
	admissionReader := newAdmissionReader(loweringContext)
	elected, _, _ := unionBranches(loweringContext, schema, allOfBranchValue.schema.Schema)
	read := admissionReader.written(allOfBranchValue.schema.Schema, admissionReader.nullable).and(admissionReader.union(elected))
	return conjunctAtom{name: name, admits: read.kinds &^ kindNull, null: read.kinds&kindNull != 0, st: allOfBranchValue.schema,
		cut: admissionReader.incomplete() || allOfBranchValue.capped, written: ref}
}

// unionAtoms returns the union keyword names, written beside an allOf with
// branches, as one conjunct admitting what any branch does, and none where it
// writes none.
func unionAtoms(loweringContext lowering.Ctx, keyword string, branches []statement.Subschema) []conjunctAtom {
	if len(branches) == 0 {
		return nil
	}
	atom := conjunctAtom{name: keyword, union: branches}
	for _, b := range branches {
		admission := admissionOf(loweringContext, loweringContext.Statements().Subschema(b))
		atom.admits |= admission.admits
		atom.cut = atom.cut || admission.cut
	}
	return []conjunctAtom{atom}
}

// nullShared reports whether the conjuncts of the allOf schema writes may share
// a null: each may admit it wherever the IR holds it, as mayAdmitNull reads it,
// so one keeping it out, a union none of whose branches admits it among them,
// leaves none to share. Where the read passes the walk's budget, which
// conjuncts it reached would follow their order, so each is read on its own:
// one that keeps null out within its own budget decides, and otherwise they
// share it.
func nullShared(loweringContext lowering.Ctx, schema *statement.Schema) bool {
	nullsValue := newNulls(loweringContext)
	nullsValue.variants = true
	if nullReading := nullsValue.conjunct(loweringContext.Statements().Read(schema), nullSite{}); !nullsValue.conjuncts.Exhausted() {
		return nullReading.admits(loweringContext)
	}
	keepsOut := func(read func(otherNulls *nulls) nullReading) bool {
		anotherNulls := newNulls(loweringContext)
		anotherNulls.variants = true
		return read(&anotherNulls).forbids(loweringContext) && !anotherNulls.conjuncts.Exhausted()
	}
	if keepsOut(func(otherNulls *nulls) nullReading { return otherNulls.own(schema) }) {
		return false
	}
	return !slices.ContainsFunc(conjunctsIn(loweringContext, schema), func(b *statement.Statement) bool {
		return keepsOut(func(otherNulls *nulls) nullReading { return otherNulls.conjunct(b, nullSite{}) })
	})
}

// empty reports whether a admits no value by itself, the null included as
// nulls reads it, which makes it its own schema's to report rather than the
// allOf's.
func empty(loweringContext lowering.Ctx, a conjunctAtom) bool {
	if a.admits != 0 || a.null {
		return false
	}
	switch {
	case a.st != nil:
		return !mayAdmitNull(loweringContext, a.st, nil)
	case a.union != nil:
		return !slices.ContainsFunc(a.union, func(b statement.Subschema) bool {
			return mayAdmitNull(loweringContext, loweringContext.Statements().Subschema(b), nil)
		})
	default:
		return typeNullVerdict(a.words) != nullAdmitted
	}
}

// diagDisjointConjuncts is the one report an allOf whose conjuncts share no
// value draws, at the schema that writes them, which lowered to shape: a
// composition, or the value set an election passed it over for.
func diagDisjointConjuncts(loweringContext lowering.Ctx, comp allOfComposition, shape string) []ir.Diagnostic {
	if len(comp.disjoint) == 0 {
		return nil
	}
	return []ir.Diagnostic{loweringContext.DiagAt(ir.SeverityWarning, diag.DegradedConstruct, comp.pointer,
		"the conjuncts of %s share no value (%s), so no instance satisfies this schema, which %s cannot state",
		comp.subject(), strings.Join(comp.disjoint, ", "), shape)}
}

// composedShape is the shape a composition lowers to, for diagDisjointConjuncts.
const composedShape = "a composition"

// unionShape is the shape a position lowering to the union it elects of two
// standing alone lowers to, for diagDisjointConjuncts.
const unionShape = "the union it lowered as"

// diagUnionsShareNothing is the warning schema, written at pointer and writing
// a oneOf and an anyOf, draws where the two share no value (pairApart), a null
// only where both may admit it, as the same two on its allOf's entries draw
// (docs/compilers/openapi.md#allof-disjoint-conjuncts): each named by its
// keyword, and the position lowered as shape.
func diagUnionsShareNothing(loweringContext lowering.Ctx, schema *statement.Schema, pointer jsontext.Pointer, shape string) []ir.Diagnostic {
	comp := allOfComposition{pointer: pointer}
	comp.disjoint = pairApart(loweringContext, schema)
	return diagDisjointConjuncts(loweringContext, comp, shape)
}

// keywordRank orders the keywords a disjoint report names a conjunct by: the
// type, the oneOf, then the anyOf.
var keywordRank = map[string]int{"type": 0, "oneOf": 1, "anyOf": 2}

// compareConjunctNames orders two names of a disjoint report as every report
// lists them: the keywords beside the allOf by keywordRank, then each entry
// by its path, its own keywords in the same order.
func compareConjunctNames(a, b string) int {
	pa, _, _ := strings.Cut(a, ": ")
	pb, _, _ := strings.Cut(b, ": ")
	da, ka := splitConjunctName(pa)
	db, kb := splitConjunctName(pb)
	if da != db {
		// The keywords beside the allOf have no path, which sorts first.
		return strings.Compare(da, db)
	}
	return cmp.Compare(keywordRank[ka], keywordRank[kb])
}

// splitConjunctName is the entry path and the keyword of name.
func splitConjunctName(name string) (string, string) {
	if path, keyword, ok := strings.CutLast(name, "/"); ok {
		return path, keyword
	}
	return "", name
}

// pairApartOf keys what pairApart read of a position.
type pairApartOf struct{ s *statement.Schema }

// pairApart names the oneOf and the anyOf written for the position schema,
// beside its allOf or on its inline entries at any depth, where they share no
// value as an admissionReader reads them, value sets included and a null only
// where both admit it: each by where it is written and what it admits, nothing
// being noValue, in the order every report names them (compareConjunctNames).
// It is nil where they share one, the read is cut short, or no pair is written.
// Read once a run, so every spelling reads one answer
// (docs/compilers/openapi.md#competing-keywords).
func pairApart(loweringContext lowering.Ctx, schema *statement.Schema) []string {
	return pairReadingOf(loweringContext, schema).apart
}

// pairNullAlone reports whether the oneOf and the anyOf written for the
// position schema, read as pairApart reads them, admit null and nothing else
// together, neither admitting null alone by itself.
func pairNullAlone(loweringContext lowering.Ctx, schema *statement.Schema) bool {
	return pairReadingOf(loweringContext, schema).nullAlone
}

// pairReading is what a position's oneOf and anyOf admit together: named
// apart where nothing, and whether null alone.
type pairReading struct {
	apart     []string
	nullAlone bool
}

// pairReadingOf reads the pair pairApart describes, once a run.
func pairReadingOf(loweringContext lowering.Ctx, schema *statement.Schema) pairReading {
	return lowering.ReadOncePerRun(loweringContext, pairApartOf{schema}, func() pairReading {
		comp := allOfBranches(loweringContext, schema, "")
		if !writesUnionPair(schema, comp) {
			return pairReading{}
		}
		admissionReader := newAdmissionReader(loweringContext)
		admissionReader.nullable = definesNullableAt(loweringContext, loweringContext.Statements().Read(schema))
		pairReadsValue := pairReads{joint: admitted{kinds: kindAny}}
		pairReadsValue.read(&admissionReader, "", schema)
		for _, allOfBranch := range comp.branches {
			if allOfBranch.schema.Ref == nil && declaresUnion(allOfBranch.schema.Schema) {
				pairReadsValue.read(&admissionReader, comp.branchPath(allOfBranch.pointer), allOfBranch.schema.Schema)
			}
		}
		if admissionReader.incomplete() {
			return pairReading{}
		}
		if pairReadsValue.joint.reachable() || len(pairReadsValue.names) < 2 {
			return pairReading{nullAlone: pairReadsValue.joint.reachable() && pairReadsValue.joint.kinds == kindNull && !pairReadsValue.nullUnion}
		}
		slices.SortFunc(pairReadsValue.names, compareConjunctNames)
		return pairReading{apart: pairReadsValue.names}
	})
}

// pairReads is what pairApart has read of a position's unions: what they
// admit together, each named by what it admits, and whether one admits null
// alone.
type pairReads struct {
	joint     admitted
	names     []string
	nullUnion bool
}

// read adds the oneOf and the anyOf schema writes, at the path at from the
// position, as admissionReaderValue reads each.
func (p *pairReads) read(admissionReaderValue *admissionReader, at string, schema *statement.Schema) {
	choice := schema.ChoiceKeywords()
	for key, branches := range map[string][]statement.Subschema{"oneOf": choice.OneOf, "anyOf": choice.AnyOf} {
		if len(branches) == 0 {
			continue
		}
		read := admissionReaderValue.union(branches)
		p.joint = p.joint.and(read)
		p.nullUnion = p.nullUnion || read.reachable() && read.kinds == kindNull
		word := readWords(read)
		if !read.reachable() {
			word = noValue
		}
		if word != "" {
			p.names = append(p.names, strings.TrimPrefix(at+"/"+key, "/")+": "+word)
		}
	}
}
