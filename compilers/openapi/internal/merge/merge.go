// Package merge reconciles the properties more than one allOf branch declares:
// either folding two declarations into one or reporting that they disagree.
//
// It is the compiler's most intricate logic and the part least tied to the walk
// that drives it. Everything it needs from the rest of lowering arrives as the
// function fields on Merger, so the conflict lattice can be exercised
// against a map and a recorder rather than a document.
package merge

import (
	"cmp"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"

	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/spelling"
	"github.com/dexpace/morphic/ir"
)

// Merger reconciles properties that more than one allOf branch declares:
// either merging the two declarations or reporting that they disagree.
//
// It is not pure — deciding whether two TypeRefs conflict needs the registry —
// but that dependency is narrow enough to arrive as a function, so the conflict
// lattice stays testable against a stub.
type Merger struct {
	// Resolve looks a type up in the registry. Conflict detection compares what
	// two references point at, not the references themselves.
	Resolve func(ir.TypeID) (ir.TypeDef, bool)
	// Report records the diagnostics a decision taken now draws.
	Report Reporter
	// Defer queues a decision for once the walk lowering the model has
	// returned. It may be nil, and a redeclaration is then always
	// judged against the registry as it stands.
	Defer func(Settle)
}

// Reporter records one diagnostic at pointer, attributed to the input document
// that supplied that position.
type Reporter func(sev ir.Severity, code string, pointer jsontext.Pointer, format string, args ...any)

// Settle is a decision taken against the registry the walk left. It reports through
// report, not through the Merger it was queued by, whose sink belongs to a walk
// that has returned.
type Settle func(reg ir.TypeRegistry, report Reporter)

// WireNameIndex maps each property's wire name to its position in props, so a
// redeclaration reconciles in one lookup rather than a rescan (fillModelProperties
// runs once per allOf branch, so a linear scan would be quadratic in a wide model).
func WireNameIndex(props []ir.Property) map[string]int {
	idx := make(map[string]int, len(props))
	for i := range props {
		idx[props[i].WireName] = i
	}
	return idx
}

// Source renders the construct a declaration was written as, verbatim. It is a
// function because nearly every property is declared once, and rendering a node
// nobody will keep is a cost the merge asks for only when it has something to
// keep.
type Source func() (ir.RawValue, error)

// PropIndex finds, for each wire name of one Model, the position of the
// property that holds it and every declaration that wrote its default and
// examples. An origin's Source is what lets a declaration whose value the merge
// refuses be kept verbatim beside the merged field, as a redeclaration is
// (keepLosingDeclaration).
type PropIndex struct {
	byWire map[string]*indexed
}

// indexed is one property's position in its Model and the origins of its values.
type indexed struct {
	pos int
	origins
	// pending holds the redeclarations queued for Settle, once one was. Every
	// later one joins it, so no fold compares against a property an earlier
	// fold has not produced.
	pending *deferredFold
}

// origin is one declaration: where it was written and how to render it.
type origin struct {
	prov   ir.Provenance
	source Source
}

// origins lists, for the default and for the examples a property holds, every
// declaration that wrote that value, equal values included. A value the field
// newly holds replaces its list. Once a refusal empties the field the list is
// stale and unread, and the next value written replaces it.
type origins struct {
	def, examples []origin
}

// NewPropIndex returns an empty index. A Model fills from empty, so every
// property reaches it through MergeProperty.
func NewPropIndex() *PropIndex {
	return &PropIndex{byWire: map[string]*indexed{}}
}

// MergeProperty appends p to m and records it in idx, or folds it into the
// property that already carries the same wire name — overlapping allOf branches
// (and properties co-declared alongside allOf) redeclare one logical field under
// allOf's intersection semantics (ir-design §4.3). Callers must set p.WireName
// (fillModelProperties always does); it keys idx directly.
//
// p.Provenance locates the declaration, and is the only source of that fact
// here. It used to arrive twice — as a parameter and on p — equal by
// construction at the one call site, which left nothing able to catch them
// disagreeing.
func (g *Merger) MergeProperty(m *ir.Model, idx *PropIndex, p ir.Property, source Source) {
	if e, ok := idx.byWire[p.WireName]; ok {
		foldOrDefer(g, m, e, p, source)
		return
	}
	at := []origin{{prov: p.Provenance, source: source}}
	e := &indexed{pos: len(m.Properties)}
	e.def, e.examples = at, at
	idx.byWire[p.WireName] = e
	m.Properties = append(m.Properties, p)
}

// adopted moves each of the default and examples origins after a fold that
// src took part in; had says whether dst held the keyword before it.
func (o *origins) adopted(dst, src *ir.Property, hadDefault, hadExamples bool, at origin) {
	o.def = moveOrigin(o.def, hadDefault, dst.Default != nil, reflect.DeepEqual(dst.Default, src.Default), at)
	o.examples = moveOrigin(o.examples, hadExamples, len(dst.Examples) != 0, reflect.DeepEqual(dst.Examples, src.Examples), at)
}

// moveOrigin returns the writers of a value after src's fold. A value dst did
// not hold but holds now was written by src alone; one dst held that src wrote
// equal gains src as a writer; any other list is unchanged.
func moveOrigin(list []origin, had, holds, equal bool, at origin) []origin {
	switch {
	case !holds:
		return list
	case !had:
		return []origin{at}
	case equal:
		return append(slices.Clone(list), at)
	default:
		return list
	}
}

// reconcileProperty folds a redeclaration src into the property dst already
// holds, under allOf intersection semantics: required and secret are OR-ed,
// visibility narrows to what both branches admit (mergeVisibility), and
// optional details dst lacks are adopted from src. dst, the first declaration,
// keeps its identity, and its type unless src narrows it. Unmodeled unions.
//
// What the fold cannot carry is reported where it is dropped, and the
// redeclaration is kept whole beside the winner (keepLosingDeclaration). So is
// a default or examples the merged type refuses (judgeAdopted), at every
// declaration that wrote it.
func (g *Merger) reconcileProperty(dst *ir.Property, org *origins, src ir.Property, source Source) {
	pointer := src.Provenance.Pointer
	dropped, lost, narrowed := g.recordRedeclarationConflict(dst, &src)
	srcLost, keptDst := g.judgeAdopted(dst, &src, narrowed, dropped, org)
	lost = lost || srcLost

	dst.Required = dst.Required || src.Required
	dst.Secret = dst.Secret || src.Secret

	// Reported off mergeVisibility's inputs and result rather than from inside
	// it: "the branches emptied a set neither had already emptied" is a fact
	// about the intersection's arguments, so stating it here cannot drift from
	// how the intersection is computed, and mergeVisibility stays a pure set
	// operation with no diagnostic channel of its own.
	visibility := mergeVisibility(dst.Visibility, src.Visibility)
	if visibility.None && !dst.Visibility.None && !src.Visibility.None {
		g.Report(ir.SeverityWarning, diag.DisjointVisibility, pointer,
			"allOf branches restrict field %q to disjoint lifecycles; the merged field is visible in none", dst.WireName)
	}
	dst.Visibility = visibility

	lost = g.foldDocs(dst, &src) || lost
	// Skipped when src's type was dropped: default, constraints and examples
	// describe the shape that lost, so folding them onto the winner makes the
	// document assert two contradictory things about one field — an integer
	// carrying a string default, beside an Unmodeled entry saying the string
	// declaration was dropped. Nothing downstream compares a Value's kind to its
	// property's type, so an emitter renders that pair into code that does not
	// compile. Keyed on the drop, not on the conflict diagnostic: two Models are
	// dropped without being called a conflict, and folded the loser's default
	// onto the winner all the same. Deprecation and XML are not shape bound
	// and are adopted either way.
	if !dropped {
		hadDefault, hadExamples := dst.Default != nil, len(dst.Examples) != 0
		lost = g.foldShapeDetail(dst, &src) || lost
		org.adopted(dst, &src, hadDefault, hadExamples, origin{prov: src.Provenance, source: source})
	}
	lost = g.foldAnnotations(dst, &src) || lost
	dst.Unmodeled = annotation.MergeUnmodeled(dst.Unmodeled, src.Unmodeled)

	if lost {
		g.keepLosingDeclaration(dst, src.Provenance, source)
	}
	for _, at := range keptDst {
		g.keepLosingDeclaration(dst, at.prov, at.source)
	}
}

// foldDocs adopts src's summary, description and external links where dst has
// none, and reports whether the two declarations gave any of them differently
// — in which case dst's stands and src's is lost. Every field is judged, so
// each disagreement is reported.
func (g *Merger) foldDocs(dst, src *ir.Property) bool {
	lost := false
	if foldString(&dst.Docs.Summary, src.Docs.Summary) {
		g.detailDiffersDiag(dst, src.Provenance.Pointer, "title")
		lost = true
	}
	if foldString(&dst.Docs.Description, src.Docs.Description) {
		g.detailDiffersDiag(dst, src.Provenance.Pointer, "description")
		lost = true
	}
	if len(dst.Docs.ExternalDocs) == 0 {
		dst.Docs.ExternalDocs = src.Docs.ExternalDocs
	} else if len(src.Docs.ExternalDocs) > 0 && !slices.Equal(dst.Docs.ExternalDocs, src.Docs.ExternalDocs) {
		g.detailDiffersDiag(dst, src.Provenance.Pointer, "external docs")
		lost = true
	}
	return lost
}

// foldString adopts src into an empty dst and reports whether both are set and
// differ, in which case dst's stands.
func foldString(dst *string, src string) bool {
	if *dst == "" {
		*dst = src
		return false
	}
	return src != "" && src != *dst
}

// foldShapeDetail adopts src's default, constraints and examples where dst
// lacks them, and reports whether a default or examples dst already held were
// held differently by src, so src's are lost. A constraint keyword both declare
// differently is recordRedeclarationConflict's to report; mergeConstraints
// keeps dst's.
//
// "Held differently" is a deep comparison, since a Value is a tree.
// reflect.DeepEqual tells a nil slice from an empty one, but both sides were
// lowered by the same code, so two spellings the source wrote alike cannot land
// on opposite sides.
func (g *Merger) foldShapeDetail(dst, src *ir.Property) bool {
	lost := false
	if dst.Default == nil {
		dst.Default = src.Default
	} else if src.Default != nil && !reflect.DeepEqual(dst.Default, src.Default) {
		g.detailDiffersDiag(dst, src.Provenance.Pointer, "default")
		lost = true
	}
	dst.Constraints = mergeConstraints(dst.Constraints, src.Constraints)
	if len(dst.Examples) == 0 {
		// Examples is a slice, not comparable, so it cannot go through
		// cmp.Or like its neighbors; the len()==0 predicate is the rule.
		dst.Examples = src.Examples
	} else if len(src.Examples) != 0 && !reflect.DeepEqual(dst.Examples, src.Examples) {
		g.detailDiffersDiag(dst, src.Provenance.Pointer, "examples")
		lost = true
	}
	return lost
}

// foldAnnotations adopts src's deprecation and XML hints where dst has none,
// and reports whether dst already held either differently — in which case
// dst's stands and src's is lost.
func (g *Merger) foldAnnotations(dst, src *ir.Property) bool {
	lost := false
	if dst.Deprecation == nil {
		dst.Deprecation = src.Deprecation
	} else if src.Deprecation != nil && *src.Deprecation != *dst.Deprecation {
		g.detailDiffersDiag(dst, src.Provenance.Pointer, "deprecation")
		lost = true
	}
	if dst.XML == nil {
		dst.XML = src.XML
	} else if src.XML != nil && *src.XML != *dst.XML {
		g.detailDiffersDiag(dst, src.Provenance.Pointer, "xml")
		lost = true
	}
	return lost
}

// detailDiffersDiag reports a detail both declarations of a field write and
// write differently. Info, not warning: the merged field is complete and
// consistent, and what a reader is told is only that the first declaration's
// spelling was the one kept, with the other beside it under Unmodeled.
func (g *Merger) detailDiffersDiag(dst *ir.Property, pointer jsontext.Pointer, detail string) {
	g.Report(ir.SeverityInfo, diag.DegradedConstruct, pointer,
		"declarations of field %q give its %s differently; kept the first declaration's, "+
			"with the redeclaration verbatim under Unmodeled", dst.WireName, detail)
}

// mergeConstraints folds src's constraint keywords into dst under allOf
// intersection semantics: dst keeps every keyword it sets and adopts any it
// leaves unset (nil, "" or false), so a keyword only one branch constrains is
// never dropped.
//
// The four numeric bounds are separate keywords, so each is adopted on its own:
// a branch declaring only exclusiveMinimum contributes it beside a minimum from
// elsewhere. UniqueItems has no absent state, but under intersection a true
// from either branch is always correct, so cmp.Or never wrongly downgrades dst.
func mergeConstraints(dst, src *ir.Constraints) *ir.Constraints {
	if dst == nil {
		return src
	}
	if src == nil {
		return dst
	}
	dst.Min = cmp.Or(dst.Min, src.Min)
	dst.Max = cmp.Or(dst.Max, src.Max)
	dst.ExclusiveMin = cmp.Or(dst.ExclusiveMin, src.ExclusiveMin)
	dst.ExclusiveMax = cmp.Or(dst.ExclusiveMax, src.ExclusiveMax)
	dst.MultipleOf = cmp.Or(dst.MultipleOf, src.MultipleOf)
	dst.Precision = cmp.Or(dst.Precision, src.Precision)
	dst.Scale = cmp.Or(dst.Scale, src.Scale)
	dst.MinLength = cmp.Or(dst.MinLength, src.MinLength)
	dst.MaxLength = cmp.Or(dst.MaxLength, src.MaxLength)
	dst.Pattern = cmp.Or(dst.Pattern, src.Pattern)
	dst.PatternMessage = cmp.Or(dst.PatternMessage, src.PatternMessage)
	dst.MinItems = cmp.Or(dst.MinItems, src.MinItems)
	dst.MaxItems = cmp.Or(dst.MaxItems, src.MaxItems)
	dst.UniqueItems = cmp.Or(dst.UniqueItems, src.UniqueItems)
	dst.MinProps = cmp.Or(dst.MinProps, src.MinProps)
	dst.MaxProps = cmp.Or(dst.MaxProps, src.MaxProps)
	return dst
}

// mergeVisibility folds src's lifecycle visibility into dst under allOf
// intersection semantics (ir-design §5.2): the merged property is visible only
// in a lifecycle both branches admit.
//
// An empty Only (None false) means visible in every lifecycle, not none, so
// each side is read as the set it admits before intersecting. Disjoint sets
// (readOnly with writeOnly) intersect to none, recorded as None rather than a
// conflict because nothing is discarded; reconcileProperty adds a warning.
//
// The result is the same whichever branch arrives as dst, down to the order of
// Only, since allOf gives branch order no meaning (intersectLifecycles).
func mergeVisibility(dst, src ir.Visibility) ir.Visibility {
	switch {
	case dst.None || src.None:
		return ir.Visibility{None: true}
	case len(dst.Only) == 0:
		return src
	case len(src.Only) == 0:
		return dst
	default:
		if only := intersectLifecycles(dst.Only, src.Only); len(only) > 0 {
			return ir.Visibility{Only: only}
		}
		return ir.Visibility{None: true}
	}
}

// intersectLifecycles returns the lifecycles present in both a and b, or nil
// when they share none; mergeVisibility maps nil to None, not to an
// empty-but-unrestricted Only.
//
// Both operands are in source order, so the result takes its order from
// whichever sorts first by value, not from which branch was declared first, or
// two spellings of the same lifecycles would intersect to different slices. No
// OpenAPI document reaches a partial overlap today, since
// annotation.EffectiveVisibility yields only identical or disjoint sets, but a
// second visibility source would inherit this contract.
func intersectLifecycles(a, b []ir.Lifecycle) []ir.Lifecycle {
	if slices.Compare(a, b) > 0 {
		a, b = b, a
	}
	inB := make(map[ir.Lifecycle]bool, len(b))
	for _, l := range b {
		inB[l] = true
	}
	var out []ir.Lifecycle
	for _, l := range a {
		if inB[l] {
			out = append(out, l)
		}
	}
	return out
}

// maxTypeResolveDepth bounds Base-chain resolution when classifying a property
// type for conflict detection (styleguide bounded-recursion rule). A scalar Base
// chain is far shorter than this in a well-formed document, so the cap only
// guards a pathological or malformed registry.
const maxTypeResolveDepth = 64

// recordRedeclarationConflict folds src's type into dst (foldTypeTargets) and
// diagnoses what the fold could not represent: a conflicting or overlapping
// target type, or a contradictory constraint keyword. At most one diagnostic
// fires, a type verdict subsuming a constraint one.
//
// dropped reports that src's type was lost. lost reports that src's type or a
// constraint keyword was lost, so the redeclaration is kept whole (GitHub #424).
// narrowed says which declaration a narrowing left broader, for judgeAdopted.
func (g *Merger) recordRedeclarationConflict(dst, src *ir.Property) (dropped, lost bool, narrowed narrowing) {
	pointer := src.Provenance.Pointer
	rel, narrowed := g.foldTypeTargets(dst, src)
	dropped = rel.dropped()
	if dropped {
		switch g.dropVerdict(rel, dst.Type, src.Type) {
		case verdictConflict:
			g.redeclarationConflictDiag(dst, pointer,
				fmt.Sprintf("incompatible types %s and %s", dst.Type.Target, src.Type.Target))
			return true, true, narrowed
		case verdictOverlap:
			g.overlappingTypesDiag(dst, pointer, src.Type)
			return true, true, narrowed
		case verdictSilent:
		}
	}
	if detail, ok := constraintsConflict(dst.Constraints, src.Constraints); ok {
		g.redeclarationConflictDiag(dst, pointer, detail)
		return dropped, true, narrowed
	}
	return dropped, dropped, narrowed
}

// dropVerdict classifies a drop: the judgment's own disjoint and overlap
// verdicts stand, and a pair it had no arm for goes to typesConflict.
func (g *Merger) dropVerdict(rel relation, a, b ir.TypeRef) redeclVerdict {
	if rel == relDisjoint {
		return verdictConflict
	}
	if rel == relOverlap {
		return verdictOverlap
	}
	return g.typesConflict(a, b)
}

// narrowing says which declaration a fold that settled on the narrower of two
// types left behind. The zero value is no narrowing.
type narrowing struct {
	// broad is the declaration whose type lost to the narrower one; its
	// default and examples ride onto a type that may not admit them.
	broad *ir.Property
}

// foldTypeTargets settles which target dst keeps and returns the judgment on
// the two (typeJudge.relate). A narrower target wins in either order, and the
// narrowing it returns names the broader declaration. Any other differing
// target is a drop, and dst keeps its own null admission. Where the referent is
// the same or one narrows the other, null is admitted only where both
// branches admit it, as foldNullVerdicts states for a single schema; when the
// loser is the top type, which admits null whatever the bit says, the winner's
// admission stands alone.
func (g *Merger) foldTypeTargets(dst, src *ir.Property) (rel relation, nw narrowing) {
	j := typeJudge{Resolve: g.Resolve}
	rel = j.relate(dst.Type, src.Type)
	if rel.dropped() {
		return rel, nw
	}
	winner, loser := dst.Type, src.Type
	nw.broad = src
	if rel == relBNarrower {
		winner, loser = src.Type, dst.Type
		nw.broad = dst
	}
	if rel == relSame {
		nw.broad = nil
	}
	dst.Type.Target = winner.Target
	dst.Type.Nullable = winner.Nullable && loser.Nullable
	if td, ok := j.Resolve(loser.Target); ok && rel != relSame && isTop(td) {
		dst.Type.Nullable = winner.Nullable
	}
	return rel, nw
}

// judgeAdopted holds both declarations of a fold to the type it settled on. The
// broader side of a narrowing loses a default or example the winner does not
// admit; either side of a fold that kept one type loses a null the merged
// field refuses. Each is reported once per declaration, never adopted.
// Constraints adopted from the broader side are not judged: a bound the winning
// kind cannot meet (`minimum: 0.5, maximum: 0.7` on `int32`) still lands.
//
// dropped says the fold lost src's type: nothing is judged. srcLost says
// src is to be kept verbatim; keptDst lists dst's writers to keep.
func (g *Merger) judgeAdopted(dst, src *ir.Property, nw narrowing, dropped bool, org *origins) (srcLost bool, keptDst []origin) {
	// The top type admits null whatever the bit says: `{}` admits every
	// instance, and a single schema keeps `default: null` on it.
	nullable := dst.Type.Nullable || g.isAnyType(dst.Type)
	nullOnly := func(v ir.Value) bool { return v.Kind != ir.ValueNull || nullable }
	at := []origin{{prov: src.Provenance}}
	srcOwn := origins{def: at, examples: at}
	var refused []refusal
	switch {
	case nw.broad != nil:
		winner := dst.Type
		winner.Nullable = nullable
		kind := func(v ir.Value) bool { return typeJudge{Resolve: g.Resolve}.admits(winner, v) }
		dstAdmit, srcAdmit := nullOnly, kind
		if nw.broad == dst {
			dstAdmit, srcAdmit = kind, nullOnly
		}
		refused = refuseFrom(refused, dst, *org, false, dstAdmit)
		refused = refuseFrom(refused, src, srcOwn, true, srcAdmit)
	case !dropped:
		refused = refuseFrom(refused, dst, *org, false, nullOnly)
		refused = refuseFrom(refused, src, srcOwn, true, nullOnly)
	}
	for _, grp := range groupRefusals(refused) {
		g.Report(ir.SeverityInfo, diag.DegradedConstruct, grp.at.prov.Pointer,
			"the type the declarations of field %q merge it to does not admit the %s one declaration gives; "+
				"not adopted, with that declaration verbatim under Unmodeled",
			dst.WireName, strings.Join(grp.keywords, " and "))
		if grp.fromSrc {
			srcLost = true
			continue
		}
		keptDst = append(keptDst, grp.at)
	}
	return srcLost, keptDst
}

// refuseFrom removes the default and the examples of side that admit does not
// admit, and adds a refusal for each declaration in own that wrote them. An
// example with no Value says nothing a type could refuse.
func refuseFrom(refused []refusal, side *ir.Property, own origins, fromSrc bool, admit func(ir.Value) bool) []refusal {
	if d := side.Default; d != nil && !admit(*d) {
		side.Default = nil
		refused = appendRefusals(refused, "default", own.def, fromSrc)
	}
	refuses := func(e ir.Example) bool { return e.Value != nil && !admit(*e.Value) }
	if slices.ContainsFunc(side.Examples, refuses) {
		side.Examples = nil
		refused = appendRefusals(refused, "examples", own.examples, fromSrc)
	}
	return refused
}

// appendRefusals adds a refusal of keyword for each declaration that wrote it.
func appendRefusals(refused []refusal, keyword string, writers []origin, fromSrc bool) []refusal {
	for _, at := range writers {
		refused = append(refused, refusal{keyword: keyword, at: at, fromSrc: fromSrc})
	}
	return refused
}

// refusal is a keyword the folded type refused and the declaration that wrote it.
type refusal struct {
	keyword string
	at      origin
	// fromSrc says the writer is the redeclaration being folded in, not one dst holds.
	fromSrc bool
}

// refusalGroup is the keywords one declaration wrote that were refused.
type refusalGroup struct {
	keywords []string
	at       origin
	fromSrc  bool
}

// groupRefusals gathers refusals by the declaration that wrote them, in order
// of first appearance.
func groupRefusals(refused []refusal) []refusalGroup {
	var groups []refusalGroup
	for _, r := range refused {
		i := slices.IndexFunc(groups, func(g refusalGroup) bool {
			return g.at.prov.Pointer == r.at.prov.Pointer && g.fromSrc == r.fromSrc
		})
		if i < 0 {
			groups = append(groups, refusalGroup{keywords: []string{r.keyword}, at: r.at, fromSrc: r.fromSrc})
			continue
		}
		groups[i].keywords = append(groups[i].keywords, r.keyword)
	}
	return groups
}

// losingDeclarationKey prefixes the Unmodeled entry a redeclaration the merge could not
// fold whole is kept under. The "openapi:" namespace is what keeps two source
// formats' keys from colliding on one node (ir-design §12); the redeclaration's
// own pointer completes it, the way an allOf branch's index completes the
// composition's keys.
const losingDeclarationKey = "openapi:conflicting-redeclaration"

// errNoSource is why a declaration that must be kept verbatim cannot be: no
// Source was recorded for it.
var errNoSource = errors.New("no source was recorded for the declaration")

// keepLosingDeclaration keeps the declaration at loser verbatim under
// Unmodeled beside the merged property, so a document consumer sees what the
// merge did not carry (GitHub #424).
//
// The value is the construct source renders, not the dropped IR values (GitHub
// #445): Unmodeled holds what the document wrote (ir-design §12), and a TypeID
// in a byte slice is invisible to irverify. The key ends in loser's pointer, so
// each declaration keeps its own entry.
//
// A declaration with no pointer is skipped, since PreserveInto overwrites a
// collapsed key; no production path produces one. A source that will not
// render is reported as an error (GitHub #144).
func (g *Merger) keepLosingDeclaration(dst *ir.Property, loser ir.Provenance, source Source) {
	pointer := loser.Pointer
	if pointer == "" {
		return
	}
	key := losingDeclarationKey + string(pointer)
	raw, err := render(source)
	if err != nil {
		g.Report(ir.SeverityError, diag.UnpreservableConstruct, pointer,
			"%s could not be kept verbatim under Unmodeled and is represented in the IR "+
				"in no form at all: %s", key, err.Error())
		return
	}
	// Degraded lowering (ir-design §4.8,
	// docs/compilers/openapi.md#allof-redeclared-property): the IR has no
	// combinator for two shapes or two defaults at one position.
	annotation.PreserveInto(&dst.Unmodeled, key, raw,
		ir.ReasonDegradedLowering, loser)
}

// render calls source, or fails with errNoSource when there is none.
func render(source Source) (ir.RawValue, error) {
	if source == nil {
		return nil, errNoSource
	}
	return source()
}

// redeclarationConflictDiag emits the shared conflicting-redeclaration warning,
// naming the field and both declaration sites (dst's own, and the redeclaration
// at pointer). detail is the caller-formatted disagreement, so one wording
// serves both callers.
//
// It says "declarations", not "allOf branches", because dst's declaration can
// sit directly beside allOf rather than inside a branch. Severity is warning,
// as the merged model is still usable, leaving escalation to the consumer via
// the stable code. The message says the redeclaration is kept whole beside the
// winner (keepLosingDeclaration), so a reader knows where to look.
func (g *Merger) redeclarationConflictDiag(dst *ir.Property, pointer jsontext.Pointer, detail string) {
	g.Report(ir.SeverityWarning, diag.ConflictingRedecl, pointer,
		"declarations of field %q disagree: %s; kept what the first declaration (%s) says over the redeclaration (%s), "+
			"which is kept verbatim under Unmodeled",
		dst.WireName, detail, dst.Provenance.Pointer, pointer)
}

// redeclVerdict classifies two differing property types that foldTypeTargets
// could not narrow.
type redeclVerdict int

const (
	// verdictSilent is a pair the merge does not guess about: composites of one
	// kind, an unresolvable target, or the top type.
	verdictSilent redeclVerdict = iota
	// verdictConflict is a pair reported as incompatible: a scalar against a
	// structure, two structure kinds, or disjoint primitive kinds.
	verdictConflict
	// verdictOverlap is a pair whose primitive kinds share values, or that holds
	// a literal, but which the merge does not narrow to one.
	verdictOverlap
)

// overlappingTypesDiag reports that two declarations of a field give types
// whose kinds share values but which the merge does not narrow to one. Info,
// not warning: their kinds share values, and the first declaration's kind
// stays, with the redeclaration kept verbatim under Unmodeled.
func (g *Merger) overlappingTypesDiag(dst *ir.Property, pointer jsontext.Pointer, other ir.TypeRef) {
	g.Report(ir.SeverityInfo, diag.DegradedConstruct, pointer,
		"declarations of field %q give types %s and %s whose kinds share values but that the merge "+
			"does not narrow to one; kept the first declaration's type (%s), with the redeclaration "+
			"verbatim under Unmodeled",
		dst.WireName, dst.Type.Target, other.Target, dst.Provenance.Pointer)
}

// typesConflict classifies the drop of two reconciled property types the
// judgment had no arm for. Identical targets and the schemaless top type are
// silent. Two targets that resolve to primitive kinds go through
// spelling.Relate: disjoint kinds, or an enum none of whose members the other
// kind admits, conflict, and anything else overlaps. A literal against
// anything that holds values overlaps, since which values meet is not decided
// here. A scalar against a structural type conflicts, and so do two structure
// kinds. Two distinct composites of one kind are not provably contradictory, so
// they are silent.
func (g *Merger) typesConflict(a, b ir.TypeRef) redeclVerdict {
	if a.Target == b.Target || g.isAnyType(a) || g.isAnyType(b) {
		return verdictSilent
	}
	ak, aok := g.resolvePrimKind(a)
	bk, bok := g.resolvePrimKind(b)
	switch {
	case aok && bok:
		if spelling.Relate(ak, bk) == spelling.Disjoint ||
			enumMisses(g.Resolve, a, bk, b.Nullable) || enumMisses(g.Resolve, b, ak, a.Nullable) {
			return verdictConflict
		}
		return verdictOverlap
	case aok && !bok:
		return g.againstUnresolved(b)
	case !aok && bok:
		return g.againstUnresolved(a)
	default:
		if g.isLiteral(a) && g.isLiteral(b) {
			return verdictOverlap
		}
		return conflictIf(g.differentTypeKind(a, b))
	}
}

// againstUnresolved classifies a type that resolves to no primitive kind,
// against one that does: a literal overlaps it, a structure conflicts with it,
// and anything else is silent.
func (g *Merger) againstUnresolved(ref ir.TypeRef) redeclVerdict {
	if g.isLiteral(ref) {
		return verdictOverlap
	}
	return conflictIf(g.isStructuralType(ref))
}

// isLiteral reports whether ref targets a Literal.
func (g *Merger) isLiteral(ref ir.TypeRef) bool {
	td, ok := g.Resolve(ref.Target)
	if !ok {
		return false
	}
	_, ok = td.(*ir.Literal)
	return ok
}

// conflictIf maps a pair reported as incompatible to verdictConflict, and any
// other pair to verdictSilent.
func conflictIf(incompatible bool) redeclVerdict {
	if incompatible {
		return verdictConflict
	}
	return verdictSilent
}

// isAnyType reports whether ref targets a type the merge does not compare: the
// schemaless top type (a PrimAny primitive or an alias over it), or an Any node
// a degraded lowering minted. Neither conflicts with a sibling redeclaration, so
// a drop against either is silent and kept.
//
// It follows the Base chain rather than reading only the target node: a
// position that wrote something only a node can hold gets an alias over the top
// type, and reading the alias alone would compare PrimAny unequal to the
// sibling's kind and report the top type as a conflict.
func (g *Merger) isAnyType(ref ir.TypeRef) bool {
	if k, ok := g.resolvePrimKind(ref); ok {
		return k == ir.PrimAny
	}
	td, ok := g.Resolve(ref.Target)
	return ok && td.Kind() == ir.KindAny
}

// resolvePrimKind follows ref through the registry to its underlying primitive
// kind, returning ok=false when the target doesn't ultimately resolve to a
// single one (a model, union, list, tuple, literal, external, or a base-less
// opaque scalar). The Base chain is bounded against a malformed registry.
//
// An Enum resolves via its already-computed ValueType (ir-design §4.5), so a
// string enum conflicts with an integer redeclaration and, where no member
// fits, with a string one. A Literal is deliberately left unresolved:
// Value.Kind doesn't map cleanly to one PrimKind (a ValueNumber literal spans
// integer/number/float/decimal), so rather than guess it is excluded from
// isStructuralType below too.
func (g *Merger) resolvePrimKind(ref ir.TypeRef) (ir.PrimKind, bool) {
	return typeJudge{Resolve: g.Resolve}.primKind(ref)
}

// differentTypeKind reports whether two registry targets carry different
// TypeDef kinds (a model vs a union); an unresolvable target is never treated
// as a conflict. Reached only from typesConflict's default case, when neither
// side resolved a PrimKind — e.g. a base-less opaque scalar against a Union.
func (g *Merger) differentTypeKind(a, b ir.TypeRef) bool {
	at, aok := g.Resolve(a.Target)
	bt, bok := g.Resolve(b.Target)
	if !aok || !bok {
		return false
	}
	return objectKind(at.Kind()) != objectKind(bt.Kind())
}

// objectKind folds a MapT into the Model kind: both are objects, and one
// instance can satisfy a map and a struct at once. Which of the two a position
// lowers to depends on what else it declares about its members (ir-design
// §4.6), not on a shape that could contradict the other.
func objectKind(k ir.TypeKind) ir.TypeKind {
	if k == ir.KindMap {
		return ir.KindModel
	}
	return k
}

// isStructuralType reports whether ref targets a type that can never be a
// scalar under allOf intersection: a model, list, map, or tuple — the only
// shapes provably incompatible with a scalar redeclaration.
//
// Enum, Union, and External are deliberately excluded: an enum member or union
// variant can itself be a scalar of the kind being redeclared, so a bare
// scalar sibling isn't provably contradictory. Enum instead resolves via its
// ValueType in resolvePrimKind above; Union/External/Literal stay unresolved
// (Literal for the same reason as in resolvePrimKind), and a base-less opaque
// scalar is likewise unknown, not structural — none of these count as
// conflicts.
func (g *Merger) isStructuralType(ref ir.TypeRef) bool {
	td, ok := g.Resolve(ref.Target)
	if !ok {
		return false
	}
	switch td.(type) {
	case *ir.Model, *ir.List, *ir.MapT, *ir.Tuple:
		return true
	default:
		return false
	}
}

// constraintsConflict reports whether two constraint sets pin the same keyword
// to different values, describing the keyword and both values (for example
// "conflicting maxLength (10 and 20)"). A keyword set on one side only is not a
// conflict, since mergeConstraints adopts it. Numeric bounds compare by
// magnitude, so 10 and 10.0 do not conflict. UniqueItems is never compared: a
// plain bool cannot tell false from unset. Keywords are checked in a fixed
// order, so the one named when several conflict is deterministic.
func constraintsConflict(a, b *ir.Constraints) (string, bool) {
	if a == nil || b == nil {
		return "", false
	}
	checks := []func() (string, bool){
		func() (string, bool) { return bigValConflictDetail("minimum", a.Min, b.Min) },
		func() (string, bool) {
			return bigValConflictDetail("exclusiveMinimum", a.ExclusiveMin, b.ExclusiveMin)
		},
		func() (string, bool) { return bigValConflictDetail("maximum", a.Max, b.Max) },
		func() (string, bool) {
			return bigValConflictDetail("exclusiveMaximum", a.ExclusiveMax, b.ExclusiveMax)
		},
		func() (string, bool) { return bigValConflictDetail("multipleOf", a.MultipleOf, b.MultipleOf) },
		func() (string, bool) { return intConflictDetail("precision", a.Precision, b.Precision) },
		func() (string, bool) { return intConflictDetail("scale", a.Scale, b.Scale) },
		func() (string, bool) { return intConflictDetail("minLength", a.MinLength, b.MinLength) },
		func() (string, bool) { return intConflictDetail("maxLength", a.MaxLength, b.MaxLength) },
		func() (string, bool) { return intConflictDetail("minItems", a.MinItems, b.MinItems) },
		func() (string, bool) { return intConflictDetail("maxItems", a.MaxItems, b.MaxItems) },
		func() (string, bool) { return intConflictDetail("minProps", a.MinProps, b.MinProps) },
		func() (string, bool) { return intConflictDetail("maxProps", a.MaxProps, b.MaxProps) },
		func() (string, bool) { return strConflictDetail("pattern", a.Pattern, b.Pattern) },
		func() (string, bool) { return strConflictDetail("patternMessage", a.PatternMessage, b.PatternMessage) },
	}
	for _, check := range checks {
		if detail, ok := check(); ok {
			return detail, ok
		}
	}
	return "", false
}

// bigValConflictDetail reports whether both branches pin the same
// arbitrary-precision keyword to different magnitudes, formatting the
// disagreement when they do. Every numeric keyword of ir.Constraints has this
// shape, each bound stating its own restriction, so one comparison serves them
// all.
//
// A disagreement is usually still satisfiable (minimum 10 in one branch and 20
// in the other together mean 20), but it is diagnosed anyway: the merge keeps
// dst's value over the true intersection, and the discarded one may be the
// stricter, so silence would loosen the validation the spec intended.
func bigValConflictDetail(keyword string, a, b *ir.BigVal) (string, bool) {
	if a == nil || b == nil || annotation.BigValEqual(*a, *b) {
		return "", false
	}
	return fmt.Sprintf("conflicting %s (%s and %s)", keyword, a.String(), b.String()), true
}

// intConflictDetail reports whether two optional integer bounds are both
// present and differ, formatting the disagreement when they do.
func intConflictDetail(keyword string, a, b *int64) (string, bool) {
	if a == nil || b == nil || *a == *b {
		return "", false
	}
	return fmt.Sprintf("conflicting %s (%d and %d)", keyword, *a, *b), true
}

// strConflictDetail reports whether two string keywords are both set and
// differ, formatting the disagreement when they do.
func strConflictDetail(keyword string, a, b string) (string, bool) {
	if a == "" || b == "" || a == b {
		return "", false
	}
	return fmt.Sprintf("conflicting %s (%q and %q)", keyword, a, b), true
}
