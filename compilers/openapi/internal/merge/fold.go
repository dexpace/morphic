// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package merge

import (
	"bytes"
	"encoding/json/jsontext"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"

	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/ir"
)

// Merger folds the declarations of one field into the property they make
// together. It reads the registry through Resolve, adds to it through Mint and
// reports through Report, so a fold can be driven against a map and a recorder
// rather than a document.
// PropIndex runs it once every node exists.
type Merger struct {
	// Resolve looks a type up in the registry. The judgment compares what two
	// references point at, not the references themselves.
	Resolve func(ir.TypeID) (ir.TypeDef, bool)
	// Mint registers the node a field's declarations compose or meet to.
	Mint Mint
	// Interned reports whether a node stands at a pointer.
	Interned func(jsontext.Pointer) bool
	// PropAt returns the identity a field named for a pointer takes (identify).
	PropAt func(jsontext.Pointer) ir.PropID
	// Report records one diagnostic a fold draws.
	Report Reporter
	// Alike reports whether what is written at a and at b reads each $ref it
	// writes against one base, so the same text there says the same: under an
	// $id naming another URI, a $ref spelled alike names another schema. The
	// fold asks it only of text writing one (annotation.WritesReference).
	Alike func(a, b ir.Provenance) bool
	// Untyped reports whether the schema written at a node's provenance states
	// no instance type, so the structure it lowered to admits every value of
	// another: `properties` alone constrains objects and admits a string
	// (docs/compilers/openapi.md#allof-disjoint-conjuncts). A node it cannot
	// place reads as stating one.
	Untyped func(at ir.Provenance) bool
}

// judgeAt is the judgment over g's registry of two declarations written at a
// and b, or of types no declaration's site decides where both are zero.
func (g *Merger) judgeAt(a, b ir.Provenance) typeJudge {
	return typeJudge{Resolve: g.Resolve, untyped: g.Untyped,
		sites: declarationSites{alike: g.Alike, written: [2]ir.Provenance{a, b}}}
}

// declaration is one declaration of a field, as the walk lowered it, and how
// to render it verbatim.
type declaration struct {
	prop   ir.Property
	source Source
	// secretBy names the heuristics that set prop.Secret, empty where a keyword
	// declared it. prop.Provenance.Inferred cannot say: one marker covers every
	// fact a promotion read into the declaration.
	secretBy ir.Inferred
}

// fold returns the property decls make together under allOf intersection
// (docs/ir/models.md#the-flattening,
// docs/compilers/openapi.md#allof-redeclared-property). decls is every
// declaration of one wire name, at least two, in declaration order; the field
// keeps the first one's identity unless another model carries that declaration
// (identify). Every fact is decided from the whole set, so
// declaration order decides only a tie, and the merge reports each one. What a
// declaration says that the field does not carry is reported at that
// declaration, which is kept verbatim beside the field.
//
// A fold judges every pair of declarations, so it judges at most
// maxFoldDeclarations of them (keepUnjudged).
func (g *Merger) fold(decls []declaration) ir.Property {
	judged, past := decls, []declaration(nil)
	if len(decls) > maxFoldDeclarations {
		judged, past = decls[:maxFoldDeclarations], decls[maxFoldDeclarations:]
	}
	schema, field := fieldSpan(judged)
	f := newFolding(g, judged, field)
	f.foldType()
	f.foldConstraints()
	f.foldValues()
	f.foldUse()
	f.foldDocs()
	f.foldAnnotations()
	f.foldUnmodeled()
	merged := f.finish()
	g.identify(&merged, schema, field)
	g.keepUnjudged(&merged, past)
	return merged
}

// maxFoldDeclarations bounds the declarations of one field a fold judges
// (styleguide bounded-everything rule). Judging every pair costs the square of
// their number, which a document of many allOf branches redeclaring one field
// could drive past any time budget; a real one declares a field a handful of
// times.
const maxFoldDeclarations = 64

// keepUnjudged keeps each declaration past the bound verbatim beside merged,
// and reports at the first of them that the fold judged none: what they say is
// kept, not folded.
func (g *Merger) keepUnjudged(merged *ir.Property, past []declaration) {
	if len(past) == 0 {
		return
	}
	g.Report(ir.SeverityError, diag.BudgetExceeded, past[0].prop.Provenance.Pointer,
		"field %q is declared %d times, past the %d declarations one fold judges; the %d after them are "+
			"kept verbatim under Unmodeled, unjudged",
		merged.WireName, maxFoldDeclarations+len(past), maxFoldDeclarations, len(past))
	for _, d := range past {
		g.keepLosingDeclaration(merged, d.prop.Provenance, d.source)
	}
}

// folding is one fold in progress.
type folding struct {
	g     *Merger
	decls []declaration
	// field is the declaration naming the field (fieldSpan).
	field jsontext.Pointer
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
	// notes holds what the fold reports at each declaration, in order, and
	// lost marks each declaration something of which the field does not carry.
	notes [][]note
	lost  []bool
}

// note is one diagnostic the fold reports at a declaration.
type note struct {
	sev  ir.Severity
	code string
	msg  string
	// detail is what a disagreement is over, and empty on any other note.
	detail string
}

// newFolding judges every pair of decls once, picks the winner (widestCone) or
// the types to compose (composition), and starts the field from the first
// declaration.
func newFolding(g *Merger, decls []declaration, field jsontext.Pointer) *folding {
	n := len(decls)
	ms := make([]member, n)
	for i, d := range decls {
		ms[i] = member{ref: d.prop.Type, decl: i}
	}
	p := g.judgePosition(decls, ms, 0)
	f := &folding{
		g: g, decls: decls, field: field, pos: p, rel: p.rel, winner: p.winner, conjuncts: p.conjuncts,
		cone: make([]bool, n), notes: make([][]note, n), lost: make([]bool, n), merged: decls[0].prop,
	}
	for i := range n {
		f.cone[i] = len(f.conjuncts) > 0 || p.holds(f.winner, i)
	}
	return f
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
	f.merged.Type = f.pos.null(registry{f.g.Resolve}, func(i int) bool { return f.cone[i] }, 0)
	f.merged.Type.Target = target
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
		for _, m := range partners {
			f.cone[m.decl] = false
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
	w, d := f.decls[f.winner].prop.Type, f.decls[i].prop.Type
	switch f.rel[f.winner][i] {
	case relDisjoint:
		f.disagree(i, f.winner, fmt.Sprintf("incompatible types %s and %s", w.Target, d.Target))
	case relOverlap, relBNarrower, relMeet:
		f.note(i, ir.SeverityInfo, diag.DegradedConstruct,
			"declarations of field %q give types %s and %s whose values overlap but that the merge does not "+
				"narrow to one; kept the type the declaration at %s gives over the one at %s, which is kept "+
				"verbatim under Unmodeled",
			f.merged.WireName, w.Target, d.Target, f.at(f.winner), f.at(i))
	default:
		f.note(i, ir.SeverityInfo, diag.DegradedConstruct,
			"declarations of field %q give types %s and %s that the merge does not compare; kept the type the "+
				"declaration at %s gives over the one at %s, which is kept verbatim under Unmodeled",
			f.merged.WireName, w.Target, d.Target, f.at(f.winner), f.at(i))
	}
}

// disagree notes the conflicting-redeclaration warning at decls[i], which says
// something of the field, detail, that decls[kept] says otherwise, and marks it
// lost. Warning, not info: the two cannot both hold, or no one value states
// them, which may loosen what the spec intended. It says "declarations", not
// "allOf branches", because a declaration can sit beside allOf rather than
// inside a branch. A keyword a position writes on its node and its property is
// met twice, and said once.
func (f *folding) disagree(i, kept int, detail string) {
	f.lost[i] = true
	if slices.ContainsFunc(f.notes[i], func(n note) bool { return n.detail == detail }) {
		return
	}
	f.notes[i] = append(f.notes[i], note{sev: ir.SeverityWarning, code: diag.ConflictingRedecl, detail: detail, msg: fmt.Sprintf(
		"declarations of field %q disagree: %s; kept what the declaration at %s says over the one at %s, "+
			"which is kept verbatim under Unmodeled", f.merged.WireName, detail, f.at(kept), f.at(i))})
}

// at is where decls[i] was written.
func (f *folding) at(i int) jsontext.Pointer { return f.decls[i].prop.Provenance.Pointer }

// foldConstraints meets the constraints the cone writes on the property.
func (f *folding) foldConstraints() {
	sets := make([]*ir.Constraints, len(f.decls))
	for i, d := range f.decls {
		if f.cone[i] {
			sets[i] = d.prop.Constraints
		}
	}
	f.merged.Constraints = meetConstraints(sets, f.disagree)
}

// foldValues settles the default and the examples from the cone. A value the
// field does not take (takes) is refused, reported and kept at the declaration
// that wrote it; of the rest, the first written is kept, and a declaration
// writing another is reported and kept. Refusing first means a value the field
// takes is kept whichever order the declarations come in.
func (f *folding) foldValues() {
	nullable := f.merged.Type.Nullable || registry{f.g.Resolve}.isAnyType(f.merged.Type)
	takesDefault := make([]bool, len(f.decls))
	takesExamples := make([]bool, len(f.decls))
	for i, d := range f.decls {
		if !f.cone[i] {
			continue
		}
		var refused []string
		takesDefault[i] = d.prop.Default == nil || f.takes(i, *d.prop.Default, nullable)
		if !takesDefault[i] {
			refused = append(refused, "default")
		}
		takesExamples[i] = !slices.ContainsFunc(d.prop.Examples, func(e ir.Example) bool {
			return e.Value != nil && !f.takes(i, *e.Value, nullable)
		})
		if !takesExamples[i] {
			refused = append(refused, "examples")
		}
		if len(refused) > 0 {
			f.lost[i] = true
			f.note(i, ir.SeverityInfo, diag.DegradedConstruct,
				"the merge cannot show that the type the declarations of field %q merge to admits the %s this "+
					"declaration gives; not adopted, with this declaration verbatim under Unmodeled",
				f.merged.WireName, strings.Join(refused, " and "))
		}
	}
	f.merged.Default = firstWritten(f, "default", func(i int, p *ir.Property) (*ir.Value, bool) {
		return p.Default, takesDefault[i] && p.Default != nil
	}, func(a, b *ir.Value) bool { return sameValue(*a, *b) })
	f.merged.Examples = firstWritten(f, "examples", func(i int, p *ir.Property) ([]ir.Example, bool) {
		return p.Examples, takesExamples[i] && len(p.Examples) > 0
	}, sameExamples)
}

// sameExamples reports whether two example lists say the same, each example's
// value compared as one JSON value (sameValue) and the rest of it deeply.
func sameExamples(a, b []ir.Example) bool {
	return slices.EqualFunc(a, b, func(x, y ir.Example) bool {
		xv, yv := x.Value, y.Value
		x.Value, y.Value = nil, nil
		if !reflect.DeepEqual(x, y) || (xv == nil) != (yv == nil) {
			return false
		}
		return xv == nil || sameValue(*xv, *yv)
	})
}

// takes reports whether the field takes v, which decls[i] wrote: a null where
// the field admits null, which the top type always does (nullable), and any
// other value every bound another declaration brought admits, where decls[i]'s
// own type is the field's, but for those bounds, or the field's type admits it. A
// value a declaration writes against its own type and bounds is that
// declaration's to answer for, not the fold's, and a composed field has no
// declaration whose type it is.
func (f *folding) takes(i int, v ir.Value, nullable bool) bool {
	if v.Kind == ir.ValueNull {
		return nullable
	}
	winner, own := &f.decls[f.winner].prop, &f.decls[i].prop
	judge := f.g.judgeAt(winner.Provenance, own.Provenance)
	if !admitsConstraints(boundsBeyond(allBounds(judge, &f.merged), allBounds(judge, own)), v, 0) {
		return false
	}
	if len(f.conjuncts) == 0 && (f.rel[f.winner][i] == relSame || alike(judge, winner.Type, own.Type)) {
		return true
	}
	ref := f.merged.Type
	ref.Nullable = nullable
	return judge.admits(ref, v)
}

// foldUse settles who sends the field and sees it: required and secret where
// any declaration says so, and visible only in the lifecycles every declaration
// admits (mergeVisibility). The field holds the first declaration's provenance,
// so a later one's promoted redaction brings its marker (secretBy) in either
// order, and a declared one brings none, whatever else that declaration's
// marker covers. Declarations that empty a set none had emptied on its own are
// reported where the last of them is written; nothing is discarded, so nothing
// is kept.
func (f *folding) foldUse() {
	for i, d := range f.decls {
		f.merged.Required = f.merged.Required || d.prop.Required
		f.merged.Secret = f.merged.Secret || d.prop.Secret
		if i == 0 {
			continue
		}
		f.merged.Provenance.Inferred = ir.JoinInferred(f.merged.Provenance.Inferred, d.secretBy)
		visibility := mergeVisibility(f.merged.Visibility, d.prop.Visibility)
		if visibility.None && !f.merged.Visibility.None && !d.prop.Visibility.None {
			f.note(i, ir.SeverityWarning, diag.DisjointVisibility,
				"allOf branches restrict field %q to disjoint lifecycles; the merged field is visible in none",
				f.merged.WireName)
		}
		f.merged.Visibility = visibility
	}
}

// foldDocs settles the field's title, description and external links, each the
// first declaration's to give one (firstWritten), from every declaration: docs
// describe the field, not a shape, so a dropped declaration's count too.
func (f *folding) foldDocs() {
	same := func(a, b string) bool { return a == b }
	f.merged.Docs.Summary = firstWritten(f, "title", func(_ int, p *ir.Property) (string, bool) {
		return p.Docs.Summary, p.Docs.Summary != ""
	}, same)
	f.merged.Docs.Description = firstWritten(f, "description", func(_ int, p *ir.Property) (string, bool) {
		return p.Docs.Description, p.Docs.Description != ""
	}, same)
	f.merged.Docs.ExternalDocs = firstWritten(f, "external docs", func(_ int, p *ir.Property) ([]ir.Link, bool) {
		return p.Docs.ExternalDocs, len(p.Docs.ExternalDocs) > 0
	}, slices.Equal[[]ir.Link])
}

// foldAnnotations settles the field's deprecation and XML hints as foldDocs
// settles its docs: neither is bound to a shape. A promotion marks the
// declaration it read (lowering.Promoter.Deprecation), so the field takes the
// marker of the declaration whose deprecation it keeps, whichever declaration
// that is.
func (f *folding) foldAnnotations() {
	f.merged.Deprecation = firstWritten(f, "deprecation", func(_ int, p *ir.Property) (*ir.Deprecation, bool) {
		return p.Deprecation, p.Deprecation != nil
	}, func(a, b *ir.Deprecation) bool { return *a == *b })
	if i := slices.IndexFunc(f.decls, func(d declaration) bool { return d.prop.Deprecation != nil }); i > 0 {
		f.merged.Provenance.Inferred = ir.JoinInferred(f.merged.Provenance.Inferred, f.decls[i].prop.Provenance.Inferred)
	}
	f.merged.XML = firstWritten(f, "xml", func(_ int, p *ir.Property) (*ir.XMLHints, bool) {
		return p.XML, p.XML != nil
	}, func(a, b *ir.XMLHints) bool { return *a == *b })
}

// firstWritten returns the first value of detail a declaration gives (of), or
// the zero value when none gives one. A later declaration giving a value that
// is not the same is reported and kept: the field has one slot for it.
//
// Each caller says what is the same. A Value or an example list compares
// deeply, which tells a nil slice from an empty one, but every declaration was
// lowered by the same code, so two spellings the source wrote alike cannot land
// on opposite sides.
func firstWritten[T any](f *folding, detail string, of func(i int, p *ir.Property) (T, bool), same func(a, b T) bool) T {
	var kept T
	at := -1
	for i := range f.decls {
		v, gives := of(i, &f.decls[i].prop)
		switch {
		case !gives:
		case at < 0:
			kept, at = v, i
		case !same(kept, v):
			f.lost[i] = true
			f.note(i, ir.SeverityInfo, diag.DegradedConstruct,
				"declarations of field %q give its %s differently; kept the one the declaration at %s gives "+
					"over the one at %s, which is kept verbatim under Unmodeled",
				f.merged.WireName, detail, f.at(at), f.at(i))
		}
	}
	return kept
}

// note records a diagnostic for decls[i], reported when the fold finishes.
func (f *folding) note(i int, sev ir.Severity, code, format string, args ...any) {
	f.notes[i] = append(f.notes[i], note{sev: sev, code: code, msg: fmt.Sprintf(format, args...)})
}

// foldUnmodeled unions every declaration's Unmodeled into the field's. The keys
// are namespaced, so each entry lands; a key two declarations give is the
// first one's, and a later one keeping another construct under it is reported
// and kept, as firstWritten keeps a detail. Keys are read in order, so the
// reports are.
func (f *folding) foldUnmodeled() {
	var merged ir.Unmodeled
	from := map[string]int{}
	for i, d := range f.decls {
		for _, key := range slices.Sorted(maps.Keys(d.prop.Unmodeled)) {
			entry := d.prop.Unmodeled[key]
			at, seen := from[key]
			switch {
			case !seen:
				if merged == nil {
					merged = make(ir.Unmodeled, len(d.prop.Unmodeled))
				}
				merged[key], from[key] = entry, i
			case !sameEntry(f.g.Alike, merged[key], entry):
				f.lost[i] = true
				f.note(i, ir.SeverityInfo, diag.DegradedConstruct,
					"declarations of field %q give its %s differently; kept the one the declaration at %s gives "+
						"over the one at %s, which is kept verbatim under Unmodeled",
					f.merged.WireName, key, f.at(at), f.at(i))
			}
		}
	}
	f.merged.Unmodeled = merged
}

// sameEntry reports whether a and b, the entry two declarations give one key,
// are one: written alike, and read against one base where they write a
// reference (alike, Merger.Alike), which the same text under another base may
// read as another schema.
func sameEntry(alike func(a, b ir.Provenance) bool, a, b ir.UnmodeledEntry) bool {
	return bytes.Equal(a.Value, b.Value) && (!annotation.WritesReference(a.Value) || alike(a.Provenance, b.Provenance))
}

// finish reports, declaration by declaration, what the fold noted there,
// keeping each declaration it lost something of verbatim
// (keepLosingDeclaration).
func (f *folding) finish() ir.Property {
	for i, d := range f.decls {
		for _, n := range f.notes[i] {
			f.g.Report(n.sev, n.code, f.at(i), "%s", n.msg)
		}
		if f.lost[i] {
			f.g.keepLosingDeclaration(&f.merged, d.prop.Provenance, d.source)
		}
	}
	return f.merged
}
