// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package merge

import (
	"bytes"
	"cmp"
	"encoding/json/jsontext"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"

	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/budget"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/provenance"
	"github.com/dexpace/morphic/ir"
)

// Merger is what folding the declarations of one field reads besides them, as
// values, so a test drives a fold with a map and a stub walk rather than a
// document. ModelPropertyDeclarations.Settle runs it once every node exists.
type Merger struct {
	// Reg is the registry every lowering left. The judgment compares what two
	// references point at there, not the references themselves.
	Reg ir.TypeRegistry
	// IDs derives, for the model's document, the identity a field named for a
	// pointer takes (identify).
	IDs ids.DocumentIDNamespace
	// Doc places what a fold reports.
	Doc provenance.Doc
	// Sources names the documents a declaration's Provenance.Source indexes,
	// so a report names the one an entry brought a declaration from (at).
	Sources []ir.SourceInfo
	// Reads is what remains of the run's reads, which telling whether one part's
	// declaration stands within another's composition spends (ownership); nil
	// reads without bound.
	Reads *budget.Budget
	// Walk is the walk that declared the field.
	Walk FieldDeclarationContext
}

// FieldDeclarationContext is what a fold asks of the walk that declared the
// field.
type FieldDeclarationContext interface {
	// Mint registers node, which stands for the declarations of the field
	// pointer names (fieldSpan), or for what their types hold at a position
	// below it, and returns the ID it is registered under. It fills what in
	// node's TypeCommon names and places the node, and an enum's member IDs:
	// the lowering's to derive. The fold reads what it mints, so it registers
	// at once. brought holds where each part of a model composing the
	// declarations' types, Base first (ComposedOf), was brought, and assembled
	// what a node no schema is written at is assembled from
	// (folding.assembled); each is nil otherwise.
	Mint(pointer jsontext.Pointer, node ir.TypeDef, brought, assembled []jsontext.Pointer) ir.TypeID
	// Interned reports whether a node stands at pointer, which only the
	// finished registry can answer.
	Interned(pointer jsontext.Pointer) bool
	// Alike reports whether what is written at a and at b reads each $ref it
	// writes against one base, so the same text there says the same: under an
	// $id naming another URI, a $ref spelled alike names another schema. The
	// fold asks it only of text writing one (annotation.WritesReference).
	Alike(a, b ir.Provenance) bool
	// Admits reports whether the schema written at a node's provenance admits
	// every instance of kind, so the structure it lowered to admits each value
	// of another type of that kind: `properties` alone constrains objects and
	// admits a string (docs/compilers/openapi.md#allof-redeclared-property),
	// while a bound or a value set beside it refuses some integers, so it
	// admits integers but not every one. It is exact where the read follows all
	// the source writes; one it cannot place, or one writing what the read does
	// not follow but which could refuse a value of some kind, is not, and
	// decides nothing.
	Admits(at ir.Provenance, kind JSONInstanceKind) (admits, exact bool)
	// Written returns the Details the declaration of property writes itself,
	// beneath which a $ref's referent lends the rest, and false where it cannot
	// read where property is declared.
	Written(property ir.Property) (Details, bool)
}

// JSONInstanceKind is a kind of JSON instance, which every value of a type a
// structure is judged against is (FieldDeclarationContext.Admits).
type JSONInstanceKind uint8

// The kinds of JSON instance. An integer is a number with no fraction, which
// a schema may admit apart from the others.
const (
	InstanceString JSONInstanceKind = iota + 1
	InstanceInteger
	InstanceNumber
	InstanceBoolean
	InstanceArray
	InstanceObject
	InstanceNull
)

// resolve looks the type id names up in the registry.
func (g *Merger) resolve(id ir.TypeID) (ir.TypeDef, bool) {
	td, ok := g.Reg[id]
	return td, ok
}

// judgeAt is the judgment over g's registry of two declarations whose types
// are written at a and b (declaration.sites).
func (g *Merger) judgeAt(a, b []ir.Provenance) typeJudge {
	return typeJudge{resolve: g.resolve, sites: declarationSites{walk: g.Walk, written: [2][]ir.Provenance{a, b}}}
}

// declaration is one declaration of a field, as the walk lowered it, and how
// to render it verbatim.
type declaration struct {
	property ir.Property
	source   DeclarationConstructRenderer
	// secretBy names the heuristics that set property.Secret, empty where a
	// keyword declared it. property.Provenance.Inferred cannot say: one marker
	// covers every fact a promotion read into the declaration.
	secretBy ir.Inferred
	// order is where the walk met the declaration among the model's
	// declarations and entries (ModelPropertyDeclarations).
	order int
	// via is the entry bringing a declaration another model holds, which stays
	// declared there, so nothing keeps it verbatim and a report about it sits
	// at the entry; nil for one the model declares itself.
	via *composedEntry
	// typeAt is where the nodes of the type of a declaration an entry brings
	// may be written (Declared.TypeAt), and owner the model declaring it.
	typeAt []ir.Provenance
	owner  ir.TypeID
	// settled says a fold settled the declaration, an entry's member, so
	// written holds what the declarations it kept each detail from wrote
	// (Declared), which its own pointer does not say.
	settled bool
	written Details
}

// sites is where the nodes of d's type may be written, which tells one
// hoisted at its declaration from one a $ref brought there (hoistedAt): where
// d is written, or, for a declaration an entry brings, where its part says,
// since a member a part settled keeps its first declaration's identity
// whichever gave its type.
func (d declaration) sites() []ir.Provenance {
	if d.via != nil {
		return d.typeAt
	}
	return []ir.Provenance{d.property.Provenance}
}

// reportAt is where what a fold says of d sits: the entry bringing it, or where
// it is written.
func (d declaration) reportAt() jsontext.Pointer {
	if d.via != nil {
		return d.via.at
	}
	return d.property.Provenance.Pointer
}

// keptAs says what becomes of d where the field does not carry all it says:
// kept verbatim beside the field, or left declared on the model holding it,
// which the flattening still reads.
func (d declaration) keptAs() string {
	if d.via != nil {
		return "stays declared on its model"
	}
	return "is kept verbatim under Unmodeled"
}

// fold returns the property decls make together under allOf intersection
// (docs/compilers/openapi.md#allof-redeclared-property), the Details it keeps
// written (folding.keptWritten), and what it reports. decls is every
// declaration of one wire name, at least two, in declaration order; the field
// keeps the first one's identity unless another model carries that
// declaration (identify). Declaration order decides only a tie, reported,
// which goes to the later declaration as the flattening's override does. What
// the field does not carry of a declaration is reported there and kept beside
// the field.
//
// A fold judges every pair of declarations, so it judges at most
// maxFoldDeclarations of them (keepUnjudged).
func (g *Merger) fold(decls []declaration) (ir.Property, Details, []ir.Diagnostic) {
	judged := decls[:min(len(decls), maxFoldDeclarations)]
	schema, field := fieldSpan(judged)
	merged, written, diags := g.foldAt(decls, field, nil)
	g.identify(&merged, schema, field)
	return merged, written, diags
}

// foldAt is fold with no identity of its own, minting what it must under
// field, placed by assembled where no declaration names field
// (folding.assembled).
func (g *Merger) foldAt(decls []declaration, field jsontext.Pointer, assembled []jsontext.Pointer) (ir.Property, Details, []ir.Diagnostic) {
	judged, past := decls, []declaration(nil)
	if len(decls) > maxFoldDeclarations {
		judged, past = decls[:maxFoldDeclarations], decls[maxFoldDeclarations:]
	}
	folding := newFolding(g, judged, field)
	folding.assembled = assembled
	folding.foldType()
	folding.foldConstraints()
	folding.foldValues()
	folding.foldUse()
	folding.foldDocs()
	folding.foldAnnotations()
	folding.foldUnmodeled()
	merged, diags := folding.finish()
	return merged, folding.keptWritten, append(diags, g.keepUnjudged(&merged, past)...)
}

// maxFoldDeclarations bounds the declarations of one field a fold judges
// (styleguide bounded-everything rule). Judging every pair costs the square of
// their number, which a document of many allOf entries redeclaring one field
// could drive past any time budget; a real one declares a field a handful of
// times.
const maxFoldDeclarations = 64

// keepUnjudged keeps each declaration past the bound that the model declares
// verbatim beside merged, leaves each another model holds declared there, and
// reports at the first of them that the fold judged none: what they say is
// kept, not folded.
func (g *Merger) keepUnjudged(merged *ir.Property, past []declaration) []ir.Diagnostic {
	if len(past) == 0 {
		return nil
	}
	diags := []ir.Diagnostic{diag.Newf(ir.SeverityError, diag.BudgetExceeded, g.Doc.At(past[0].reportAt()),
		"field %q is declared %d times, past the %d declarations one fold judges; the %d after them are "+
			"unjudged, and each the model declares is kept verbatim under Unmodeled",
		merged.WireName, maxFoldDeclarations+len(past), maxFoldDeclarations, len(past))}
	for _, declaration := range past {
		if declaration.via == nil {
			diags = append(diags, g.keepLosingDeclaration(merged, declaration.property.Provenance, declaration.source)...)
		}
	}
	return diags
}

// folding is one fold in progress.
type folding struct {
	g     *Merger
	decls []declaration
	// field is the pointer the nodes the fold mints are named for: the
	// declaration naming the field (fieldSpan), or the member a composing model
	// declares, which no schema is written at, so assembled holds the positions
	// it is assembled from, which place what it mints
	// (FieldDeclarationContext.Mint); nil where a declaration names the field.
	field     jsontext.Pointer
	assembled []jsontext.Pointer
	// pos is the declarations' types judged against each other, and rel, winner
	// and conjuncts what it holds.
	pos position
	// rel[i][j] is the judgment of decls[i]'s type against decls[j]'s.
	rel [][]relation
	// winner is the declaration whose type the field takes, and cone marks
	// each declaration whose type is the same as the winner's, broader, or
	// meets it: the field's type holds every one of them, so it adopts their
	// shape detail. A meeting no node holds takes its declarations out of the
	// cone (meetField).
	winner int
	cone   []bool
	// conjuncts lists the types the field composes, which puts every
	// declaration in the cone, or nothing where the winner's type is the field's
	// (composition).
	conjuncts []conjunct
	merged    ir.Property
	// typeAt is where the nodes of the field's type may be written
	// (Merger.typeSites), once foldType has settled it: a node the fold built
	// holds members of declarations other than the winner.
	typeAt []ir.Provenance
	// notes holds what the fold reports at each declaration, in order, and
	// lost marks each declaration something of which the field does not carry.
	notes [][]note
	lost  []bool
	// written is what each declaration writes itself, and keptWritten each
	// detail the field keeps from a declaration writing it (lastWritten), which
	// a later fold reads where the field loses.
	written     declarationsWritten
	keptWritten Details
}

// note is one diagnostic the fold reports at a declaration.
type note struct {
	sev     ir.Severity
	code    string
	message string
	// detail is what a disagreement is over, and empty on any other note.
	detail string
	// at is where the note sits: where the declaration is reported
	// (declaration.reportAt), or, for a disagreement, where it is written.
	at ir.Provenance
	// kept is, for a disagreement, where the declaration whose say the field
	// kept is written (ir.RelatedKept).
	kept *ir.Provenance
}

// newFolding judges every pair of decls once, picks the winner (widestCone) or
// the types to compose (composition), and starts the field from the first
// declaration, marked by no heuristic: the field names one only for a fact it
// keeps that one promoted (foldUse, foldAnnotations).
func newFolding(merger *Merger, decls []declaration, field jsontext.Pointer) *folding {
	n := len(decls)
	ms := make([]member, n)
	for i, declaration := range decls {
		ms[i] = member{ref: declaration.property.Type, decl: i}
	}
	position := merger.judgePosition(decls, ms, 0)
	foldingValue := &folding{
		g: merger, decls: decls, field: field, pos: position, rel: position.rel, winner: position.winner, conjuncts: position.conjuncts,
		cone: make([]bool, n), notes: make([][]note, n), lost: make([]bool, n), merged: decls[0].property,
		written: declarationsWritten{walk: merger.Walk, decls: decls},
	}
	foldingValue.merged.Provenance.Inferred = ""
	for i := range n {
		foldingValue.cone[i] = len(foldingValue.conjuncts) > 0 || position.holds(foldingValue.winner, i)
	}
	return foldingValue
}

// foldType settles the field's type: the model composing the conjuncts, or the
// node the winner and the declarations meeting it stand on (meetField), admitting
// null as position.null reads the cone. Every declaration outside the cone loses
// its type (drop).
func (f *folding) foldType() {
	target := f.meetField()
	for i := range f.decls {
		if !f.cone[i] {
			f.drop(i)
		}
	}
	f.merged.Type = f.pos.null(registry{f.g.resolve}, func(i int) bool { return f.cone[i] }, 0)
	f.merged.Type.Target = target
	f.typeAt = f.g.typeSites(f.merged, f.decls)
}

// meetField returns the field's target: the model the conjuncts compose, or the
// node the winner and the declarations meeting it stand on (builder). A dry run
// first, so a field whose meeting no node holds registers nothing: it keeps the
// winner's type, and each declaration meeting it leaves the cone, to be dropped
// and reported as an overlap.
func (f *folding) meetField() ir.TypeID {
	b := builder{f: f}
	if len(f.conjuncts) > 0 {
		return b.compose(f.conjuncts, f.field)
	}
	cone := func(i int) bool { return f.cone[i] }
	base, partners := f.pos.meeting(cone)
	dry := builder{f: f, dry: true}
	dry.meetNode(base, partners, f.field, 0)
	if dry.failed {
		for _, member := range partners {
			f.cone[member.decl] = false
		}
		return base.ref.Target
	}
	return b.meetNode(base, partners, f.field, 0)
}

// drop reports how decls[i]'s type, which the field does not hold, relates to
// the winner's (typeJudge.relateFrom), even where nothing says how, since the type
// the field keeps then follows declaration order; and it marks the declaration
// lost. A type narrower than the winner that still lost shares its values. Its
// default, constraints and examples go with its type: they describe the shape
// that lost, and nothing downstream compares a Value's kind to its property's
// type, so adopting them would leave one field asserting two contradictory
// things.
func (f *folding) drop(i int) {
	f.lost[i] = true
	typeRef, irTypeRef := f.decls[f.winner].property.Type, f.decls[i].property.Type
	switch f.rel[f.winner][i] {
	case relDisjoint:
		f.disagree(i, f.winner, fmt.Sprintf("incompatible types %s and %s", typeRef.Target, irTypeRef.Target))
	case relOverlap, secondTypeIsNarrower, relMeet:
		f.note(i, ir.SeverityInfo, diag.DegradedConstruct,
			"declarations of field %q give types %s and %s whose values overlap but that the merge does not "+
				"narrow to one; kept the type the declaration at %s gives over the one at %s, which %s",
			f.merged.WireName, typeRef.Target, irTypeRef.Target, f.at(f.winner), f.at(i), f.decls[i].keptAs())
	default:
		f.note(i, ir.SeverityInfo, diag.DegradedConstruct,
			"declarations of field %q give types %s and %s that the merge does not compare; kept the type the "+
				"declaration at %s gives over the one at %s, which %s",
			f.merged.WireName, typeRef.Target, irTypeRef.Target, f.at(f.winner), f.at(i), f.decls[i].keptAs())
	}
}

// disagree notes the conflicting-redeclaration warning at decls[i], which says
// something of the field, detail, that decls[kept] says otherwise, and marks it
// lost. Warning, not info: the two cannot both hold, or no one value states
// them. It says "declarations", since one can sit beside allOf, not in an
// entry. A keyword a position writes on its node and its property is met
// twice, and said once. It sits where decls[i] is written, not at the entry
// bringing it, so every model composing the two says it alike (ReportOnce),
// and it carries where decls[kept] is written, the place its message names.
func (f *folding) disagree(i, kept int, detail string) {
	f.lost[i] = true
	if slices.ContainsFunc(f.notes[i], func(noteValue note) bool { return noteValue.detail == detail }) {
		return
	}
	at, keptAt := f.place(i), f.place(kept)
	f.notes[i] = append(f.notes[i], note{sev: ir.SeverityWarning, code: diag.ConflictingRedecl, detail: detail, at: at,
		kept: &keptAt,
		message: fmt.Sprintf("declarations of field %q disagree: %s; kept what the declaration at %s says over the one "+
			"at %s, which %s", f.merged.WireName, detail, f.named(keptAt, at.Source), f.named(at, at.Source),
			f.decls[i].keptAs())})
}

// at is how a report the fold makes at a declaration (note), which stands in
// the composing model's document, names where decls[i] was written (place).
func (f *folding) at(i int) string {
	return f.named(f.place(i), f.g.Doc.Source)
}

// named is how a report standing in the document in names irProvenance, a
// place: its pointer, followed by its document's name where that is another
// document, in which the pointer alone would read as one in the report's.
func (f *folding) named(irProvenance ir.Provenance, in int) string {
	if irProvenance.Source == in {
		return string(irProvenance.Pointer)
	}
	return string(irProvenance.Pointer) + " in " + f.g.Sources[irProvenance.Source].Path
}

// place is where decls[i] was written, in its own document, as a declaration
// an entry brings may be written in another. A member an entry brings settled
// names its first declaration, which need not say what the member does, so
// its place is the one place its type is written, or, where that is the
// member's own and no schema is written there (unwritten), the entry bringing
// it. A source out of range reads as the composing model's document.
func (f *folding) place(i int) ir.Provenance {
	declaration := f.decls[i]
	irProvenance := declaration.property.Provenance
	if declaration.via != nil && len(declaration.typeAt) == 1 {
		irProvenance = declaration.typeAt[0]
	}
	if irProvenance.Pointer == declaration.property.Provenance.Pointer && f.unwritten(declaration) {
		return f.g.Doc.At(declaration.via.at)
	}
	if irProvenance.Source == f.g.Doc.Source || irProvenance.Source < 0 || irProvenance.Source >= len(f.g.Sources) {
		return f.g.Doc.At(irProvenance.Pointer)
	}
	return ir.Provenance{Source: irProvenance.Source, Pointer: irProvenance.Pointer, WrittenBy: irProvenance.WrittenBy}
}

// unwritten reports whether declarationValue is a member an entry brings that
// its model's composition settled where no schema is written (settledAt).
func (f *folding) unwritten(declarationValue declaration) bool {
	if declarationValue.via == nil {
		return false
	}
	td, _ := f.g.resolve(declarationValue.owner)
	model, isModel := td.(*ir.Model)
	if !isModel {
		return false
	}
	at, wire := declarationValue.property.Provenance.Pointer, declarationValue.property.WireName
	return at == settledAt(model, wire, false) || at == settledAt(model, wire, true)
}

// foldConstraints meets the constraints the cone writes on the property. Where
// the fold composes the field's model, which holds no bound, the bounds of each
// declaration's own node meet there too (ownBounds): its property leaves them to
// that node, which the composed model does not stand for.
func (f *folding) foldConstraints() {
	sets := make([]*ir.Constraints, len(f.decls))
	for i, declaration := range f.decls {
		switch {
		case !f.cone[i]:
		case len(f.conjuncts) > 0:
			sets[i] = metOrNil(declaration.property.Constraints, f.ownBounds(i))
		default:
			sets[i] = declaration.property.Constraints
		}
	}
	f.merged.Constraints = meetConstraints(sets, f.disagree)
}

// ownBounds returns the value bounds held by the node decls[i]'s type names
// where that node is hoisted at the declaration, its position's own, and nil
// otherwise: a property owning its node leaves those bounds to it
// (docs/compilers/openapi.md#constraints). A List's or a Tuple's are read by
// no property, which never held them.
func (f *folding) ownBounds(i int) *ir.Constraints {
	declaration := f.decls[i]
	td, ok := f.g.resolve(declaration.property.Type.Target)
	if !ok || !hoistedAt(td, declaration.sites()) {
		return nil
	}
	switch n := td.(type) {
	case *ir.Model:
		return n.Constraints
	case *ir.MapT:
		return n.Constraints
	case *ir.Scalar:
		return n.Constraints
	case *ir.Union:
		return n.Constraints
	default:
		return nil
	}
}

// metOrNil returns a copy of a and b met (Constraints), writing to neither, or
// nil where both are.
func metOrNil(a, b *ir.Constraints) *ir.Constraints {
	if a == nil && b == nil {
		return nil
	}
	var met ir.Constraints
	return Constraints(Constraints(&met, a), b)
}

// foldValues settles the default and the examples from the cone. A value the
// field does not take (takes) is refused, reported and kept at the declaration
// that wrote it; of the rest, the last written is kept, and a declaration
// writing another is reported and kept. Refusing first means a value the field
// takes is kept whichever order the declarations come in.
func (f *folding) foldValues() {
	nullable := f.merged.Type.Nullable || registry{f.g.resolve}.isAnyType(f.merged.Type)
	takesDefault := make([]bool, len(f.decls))
	takesExamples := make([]bool, len(f.decls))
	for i, declaration := range f.decls {
		if !f.cone[i] {
			continue
		}
		var refused []string
		takesDefault[i] = declaration.property.Default == nil || f.takes(i, *declaration.property.Default, nullable)
		if !takesDefault[i] {
			refused = append(refused, "default")
		}
		takesExamples[i] = !slices.ContainsFunc(declaration.property.Examples, func(example ir.Example) bool {
			return example.Value != nil && !f.takes(i, *example.Value, nullable)
		})
		if !takesExamples[i] {
			refused = append(refused, "examples")
		}
		if len(refused) > 0 {
			f.lost[i] = true
			f.refuse(i, refused)
		}
	}
	f.merged.Default = lastWritten(f, "default", DetailDefault, func(i int, property *ir.Property) (*ir.Value, bool) {
		return property.Default, takesDefault[i] && property.Default != nil
	}, func(a, b *ir.Value) bool { return sameValue(*a, *b) })
	f.merged.Examples = lastWritten(f, "examples", 0, func(i int, property *ir.Property) ([]ir.Example, bool) {
		return property.Examples, takesExamples[i] && len(property.Examples) > 0
	}, sameExamples)
}

// refuse notes at decls[i] that the field does not adopt the values it gives,
// what, its default, its examples or both, which the merged type is not shown
// to admit.
func (f *folding) refuse(i int, what []string) {
	f.note(i, ir.SeverityInfo, diag.DegradedConstruct,
		"the merge cannot show that the type the declarations of field %q merge to admits the %s the "+
			"declaration at %s gives; not adopted, and that declaration %s",
		f.merged.WireName, strings.Join(what, " and "), f.at(i), f.decls[i].keptAs())
}

// sameExamples reports whether two example lists say the same, each example's
// value compared as one JSON value (sameValue) and the rest of it deeply.
func sameExamples(a, b []ir.Example) bool {
	return slices.EqualFunc(a, b, func(example, irExample ir.Example) bool {
		xv, yv := example.Value, irExample.Value
		example.Value, irExample.Value = nil, nil
		if !reflect.DeepEqual(example, irExample) || (xv == nil) != (yv == nil) {
			return false
		}
		return xv == nil || sameValue(*xv, *yv)
	})
}

// takes reports whether the field takes value, which decls[i] wrote: a null
// where the field admits null, which the top type always does (nullable), and
// any other value every bound another declaration brought admits, where
// decls[i]'s own type is the field's, but for those bounds (ownType), or the
// field's type admits it. A value a declaration writes against its own type and
// bounds is that declaration's to answer for, and a composed field has no
// declaration whose type it is. The field's type is read where its nodes are
// written (typeAt), not at the winner alone.
func (f *folding) takes(i int, value ir.Value, nullable bool) bool {
	if value.Kind == ir.ValueNull {
		return nullable
	}
	own := &f.decls[i].property
	judge := f.g.judgeAt(f.typeAt, f.decls[i].sites())
	if !admitsConstraints(boundsBeyond(allBounds(judge, &f.merged), allBounds(judge, own)), value, 0) {
		return false
	}
	if len(f.conjuncts) == 0 && f.ownType(judge, i) {
		return true
	}
	ref := f.merged.Type
	ref.Nullable = nullable
	return judge.admits(ref, value)
}

// ownType reports whether decls[i]'s type is the field's but for bounds: the
// same as the winner's or alike it, where the field keeps the winner's node,
// and alike the node the merge built for the field otherwise, which can hold
// members or values the winner's does not (builder).
func (f *folding) ownType(judge typeJudge, i int) bool {
	winner, own := f.decls[f.winner].property.Type, f.decls[i].property.Type
	if f.merged.Type.Target == winner.Target {
		return f.rel[f.winner][i] == relSame || sameTypeIgnoringBounds(judge, winner, own)
	}
	return sameTypeIgnoringBounds(judge, f.merged.Type, own)
}

// foldUse settles who sends the field and sees it: required and secret where
// any declaration says so, and visible only in the lifecycles every declaration
// admits (mergeVisibility). A promoted redaction brings its marker (secretBy)
// in either order, and a declared one brings none, whatever else that
// declaration's marker covers. Declarations that empty a set none had emptied
// on its own are reported where the last of them is written; nothing is
// discarded, so nothing is kept.
func (f *folding) foldUse() {
	for i, declaration := range f.decls {
		f.merged.Required = f.merged.Required || declaration.property.Required
		f.merged.Secret = f.merged.Secret || declaration.property.Secret
		f.merged.Provenance.Inferred = ir.JoinInferred(f.merged.Provenance.Inferred, declaration.secretBy)
		if i == 0 {
			continue
		}
		visibility := mergeVisibility(f.merged.Visibility, declaration.property.Visibility)
		if visibility.InvisibleInEveryLifecycle && !f.merged.Visibility.InvisibleInEveryLifecycle && !declaration.property.Visibility.InvisibleInEveryLifecycle {
			f.note(i, ir.SeverityWarning, diag.DisjointVisibility,
				"allOf entries restrict field %q to disjoint lifecycles; the merged field is visible in none",
				f.merged.WireName)
		}
		f.merged.Visibility = visibility
	}
}

// foldDocs settles the field's title, description and external links, each the
// last declaration's to give one (lastWritten), from every declaration: docs
// describe the field, not a shape, so a dropped declaration's count too.
func (f *folding) foldDocs() {
	same := func(a, b string) bool { return a == b }
	f.merged.Docs.Summary = lastWritten(f, "title", DetailTitle, func(_ int, property *ir.Property) (string, bool) {
		return property.Docs.Summary, property.Docs.Summary != ""
	}, same)
	f.merged.Docs.Description = lastWritten(f, "description", DetailDescription, func(_ int, property *ir.Property) (string, bool) {
		return property.Docs.Description, property.Docs.Description != ""
	}, same)
	f.merged.Docs.ExternalDocs = lastWritten(f, "external docs", DetailExternalDocs, func(_ int, property *ir.Property) ([]ir.Link, bool) {
		return property.Docs.ExternalDocs, len(property.Docs.ExternalDocs) > 0
	}, slices.Equal[[]ir.Link])
}

// sameDeprecation reports whether a and b state one deprecation, field by
// field and the replacements in order. TestSameDeprecation_ReadsEveryField
// holds it to every field ir.Deprecation declares.
func sameDeprecation(a, b *ir.Deprecation) bool {
	return a.Message == b.Message && a.Since == b.Since && a.RemovalVersion == b.RemovalVersion &&
		a.RemovalDate == b.RemovalDate && slices.Equal(a.ReplacedBy, b.ReplacedBy)
}

// foldAnnotations settles the field's deprecation and XML hints as foldDocs
// settles its docs: neither is bound to a shape. A promotion marks the
// declaration it read (promotion.Promoter.Deprecation), so the field takes the
// marker of the declaration whose deprecation it keeps, whichever declaration
// that is.
func (f *folding) foldAnnotations() {
	f.merged.Deprecation = lastWritten(f, "deprecation", DetailDeprecation, func(_ int, property *ir.Property) (*ir.Deprecation, bool) {
		return property.Deprecation, property.Deprecation != nil
	}, sameDeprecation)
	for _, declaration := range slices.Backward(f.decls) {
		if declaration.property.Deprecation != nil {
			f.merged.Provenance.Inferred = ir.JoinInferred(f.merged.Provenance.Inferred, declaration.property.Provenance.Inferred)
			break
		}
	}
	// XMLHints.Wrapped is a pointer, which == would compare by address.
	f.merged.XML = lastWritten(f, "xml", 0, func(_ int, property *ir.Property) (*ir.XMLHints, bool) {
		return property.XML, property.XML != nil
	}, func(a, b *ir.XMLHints) bool { return reflect.DeepEqual(a, b) })
}

// lastWritten returns the last value of detail a declaration gives (of), or the
// zero value when none gives one: a later declaration is where a spec writes
// the specialization. An earlier one giving another value is reported and kept,
// as the field has one slot, unless that value is lendable, the Details a $ref's
// referent may lend, zero for none, and the declaration only borrowed it.
//
// Each caller says what is the same. Values and example lists compare deeply,
// telling nil from empty, but one code lowered every declaration, so two
// spellings written alike land on one side.
func lastWritten[T any](foldingValue *folding, detail string, lendable Details, of func(i int, property *ir.Property) (T, bool), same func(a, b T) bool) T {
	var kept T
	at := -1
	for i := len(foldingValue.decls) - 1; i >= 0; i-- {
		v, gives := of(i, &foldingValue.decls[i].property)
		switch {
		case !gives:
		case at < 0:
			kept, at = v, i
		case !same(kept, v) && foldingValue.written.writes(i, lendable):
			foldingValue.lost[i] = true
			foldingValue.note(i, ir.SeverityInfo, diag.DegradedConstruct,
				"declarations of field %q give its %s differently; kept the one the declaration at %s gives "+
					"over the one at %s, which %s",
				foldingValue.merged.WireName, detail, foldingValue.at(at), foldingValue.at(i), foldingValue.decls[i].keptAs())
		}
	}
	if at >= 0 && foldingValue.written.writes(at, lendable) {
		foldingValue.keptWritten |= lendable
	}
	return kept
}

// note records a diagnostic for decls[i], reported when the fold finishes.
func (f *folding) note(i int, sev ir.Severity, code, format string, args ...any) {
	f.notes[i] = append(f.notes[i], note{sev: sev, code: code, message: fmt.Sprintf(format, args...),
		at: f.g.Doc.At(f.decls[i].reportAt())})
}

// foldUnmodeled unions every declaration's Unmodeled into the field's. The keys
// are namespaced, so each entry lands; a key two declarations give is the last
// one's, and an earlier one keeping another construct under it is reported and
// kept, as lastWritten keeps a detail. Of two keeping one construct, the one
// written first in the documents' order (writtenBefore) is kept, so no order
// of the declarations decides where the field says it was written. Keys are
// read in order, so the reports are.
func (f *folding) foldUnmodeled() {
	var merged ir.Unmodeled
	from := map[string]int{}
	for i := len(f.decls) - 1; i >= 0; i-- {
		declaration := f.decls[i]
		for _, key := range slices.Sorted(maps.Keys(declaration.property.Unmodeled)) {
			entry := declaration.property.Unmodeled[key]
			at, seen := from[key]
			switch {
			case !seen:
				if merged == nil {
					merged = make(ir.Unmodeled, len(declaration.property.Unmodeled))
				}
				merged[key], from[key] = entry, i
			case sameEntry(f.g.Walk, merged[key], entry):
				if writtenBefore(entry.Provenance, merged[key].Provenance) {
					merged[key] = entry
				}
			default:
				f.lost[i] = true
				f.note(i, ir.SeverityInfo, diag.DegradedConstruct,
					"declarations of field %q give its %s differently; kept the one the declaration at %s gives "+
						"over the one at %s, which %s",
					f.merged.WireName, key, f.at(at), f.at(i), f.decls[i].keptAs())
			}
		}
	}
	f.merged.Unmodeled = merged
}

// writtenBefore reports whether a is written before b: in a document listed
// before b's, which the load phase orders by name, not as declarations reach
// it, or in one document at a pointer sorting first. Two files each holding
// one schema write the same pointers.
func writtenBefore(a, b ir.Provenance) bool {
	return cmp.Or(cmp.Compare(a.Source, b.Source), cmp.Compare(a.Pointer, b.Pointer)) < 0
}

// sameEntry reports whether a and b, the entry two declarations give one key,
// are one: written alike, and read against one base where they write a
// reference (FieldDeclarationContext.Alike), which the same text under another
// base may read as another schema.
func sameEntry(walk FieldDeclarationContext, a, b ir.UnmodeledEntry) bool {
	return bytes.Equal(a.Value, b.Value) && (!annotation.WritesReference(a.Value) || walk.Alike(a.Provenance, b.Provenance))
}

// finish returns the field and, declaration by declaration, what the fold
// noted there (note.at), keeping each declaration the model declares that it
// lost something of verbatim (keepLosingDeclaration).
func (f *folding) finish() (ir.Property, []ir.Diagnostic) {
	var diags []ir.Diagnostic
	for i, declaration := range f.decls {
		for _, note := range f.notes[i] {
			diagnostic := diag.Newf(note.sev, note.code, note.at, "%s", note.message)
			if note.kept != nil {
				diagnostic.Related = []ir.Related{{Role: ir.RelatedKept, Provenance: *note.kept}}
			}
			diags = append(diags, diagnostic)
		}
		if f.lost[i] && declaration.via == nil {
			diags = append(diags, f.g.keepLosingDeclaration(&f.merged, declaration.property.Provenance, declaration.source)...)
		}
	}
	return f.merged, diags
}
