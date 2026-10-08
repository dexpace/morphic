// Package merge reconciles the properties more than one allOf branch declares:
// either folding two declarations into one or reporting that they disagree.
//
// It is the compiler's most intricate logic and the part least tied to the walk
// that drives it. Everything it needs from the rest of lowering arrives as the
// two function fields on Merger, so the conflict lattice can be exercised
// against a map and a recorder rather than a document.
package merge

import (
	"cmp"
	"encoding/json/jsontext"
	"fmt"
	"reflect"
	"slices"

	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
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
	// Report records one diagnostic at pointer, attributed to the input document
	// that supplied that position.
	Report func(sev ir.Severity, code string, pointer jsontext.Pointer, format string, args ...any)
}

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

// MergeProperty appends p to m and records it in byWire, or folds it into the
// property that already carries the same wire name — overlapping allOf branches
// (and properties co-declared alongside allOf) redeclare one logical field under
// allOf's intersection semantics (ir-design §4.3). Callers must set p.WireName
// (fillModelProperties always does); it keys byWire directly.
//
// p.Provenance locates the declaration, and is the only source of that fact
// here. It used to arrive twice — as a parameter and on p — equal by
// construction at the one call site, which left nothing able to catch them
// disagreeing.
func (g *Merger) MergeProperty(m *ir.Model, byWire map[string]int, p ir.Property, source func() (ir.RawValue, error)) {
	if i, ok := byWire[p.WireName]; ok {
		g.reconcileProperty(&m.Properties[i], p, source)
		return
	}
	byWire[p.WireName] = len(m.Properties)
	m.Properties = append(m.Properties, p)
}

// reconcileProperty folds a redeclaration src into the property dst already
// holds, under allOf intersection semantics: required and secret are OR-ed,
// visibility narrows to what both branches admit (mergeVisibility), and
// optional details dst lacks are adopted from src. dst, the first declaration,
// keeps its identity and type shape.
//
// Unmodeled is the exception: its keys are namespaced, so entries union and a
// later branch wins a shared key.
//
// What src has that the fold cannot carry is reported where it is dropped, and
// the redeclaration is kept whole beside the winner (keepLosingDeclaration, the
// only place source is rendered).
func (g *Merger) reconcileProperty(dst *ir.Property, src ir.Property, source func() (ir.RawValue, error)) {
	pointer := src.Provenance.Pointer
	dropped, lost := g.recordRedeclarationConflict(dst, &src)

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
	// declaration was dropped. Nothing downstream compares a Value's kind to
	// its property's type, so an emitter renders that pair into code that does
	// not compile. Keyed on the drop, not on the conflict diagnostic: two
	// Models are dropped without being called a conflict, and folded the
	// loser's default onto the winner all the same. Deprecation and XML are not
	// shape bound and are adopted either way.
	if !dropped {
		lost = g.foldShapeDetail(dst, &src) || lost
	}
	lost = g.foldAnnotations(dst, &src) || lost
	dst.Unmodeled = annotation.MergeUnmodeled(dst.Unmodeled, src.Unmodeled)

	if lost {
		g.keepLosingDeclaration(dst, &src, source)
	}
}

// foldDocs adopts src's description where dst has none, and reports whether
// the two declarations describe the field differently — in which case dst's
// stands and src's is lost.
func (g *Merger) foldDocs(dst, src *ir.Property) bool {
	if dst.Docs.Description == "" {
		dst.Docs.Description = src.Docs.Description
		return false
	}
	if src.Docs.Description == "" || src.Docs.Description == dst.Docs.Description {
		return false
	}
	g.detailDiffersDiag(dst, src.Provenance.Pointer, "description")
	return true
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
		"declarations of field %q give its %s differently; kept the first declaration, "+
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

// recordRedeclarationConflict folds src's type into dst and diagnoses what the
// fold could not represent: an incompatible target type or a contradictory
// constraint keyword. At most one diagnostic fires, a type conflict subsuming a
// constraint one. When the targets match it also intersects dst's nullability.
//
// dropped reports that the targets differ, so src's type is lost. lost reports
// that src's type or a constraint keyword was lost, so the redeclaration is
// kept whole (GitHub #424). dropped is wider than the reported conflicts, since
// typesConflict does not guess about composites of one kind, unresolvable
// targets or the top type.
func (g *Merger) recordRedeclarationConflict(dst, src *ir.Property) (dropped, lost bool) {
	pointer := src.Provenance.Pointer
	if dst.Type.Target != src.Type.Target {
		// A format hoist alone differs here: `{type: string, format: password}`
		// owns a Scalar while a bare string stays on the shared primitive, so the
		// format drops without a diagnostic. GitHub #446 owns whether it should.
		dropped = true
	} else {
		// Same referent, and the only thing left for the two to disagree about
		// is whether null is admitted. Under intersection it is admitted only
		// where both branches admit it — the rule foldNullVerdicts already
		// states for a single schema. Without this the answer came from
		// whichever branch was written first, so reordering two allOf branches
		// changed the merged field.
		dst.Type.Nullable = dst.Type.Nullable && src.Type.Nullable
	}
	if g.typesConflict(dst.Type, src.Type) {
		g.redeclarationConflictDiag(dst, pointer,
			fmt.Sprintf("incompatible types %s and %s", dst.Type.Target, src.Type.Target))
		return true, true
	}
	if detail, ok := constraintsConflict(dst.Constraints, src.Constraints); ok {
		g.redeclarationConflictDiag(dst, pointer, detail)
		return dropped, true
	}
	return dropped, dropped
}

// losingDeclarationKey prefixes the Unmodeled entry a redeclaration the merge could not
// fold whole is kept under. The "openapi:" namespace is what keeps two source
// formats' keys from colliding on one node (ir-design §12); the redeclaration's
// own pointer completes it, the way an allOf branch's index completes the
// composition's keys.
const losingDeclarationKey = "openapi:conflicting-redeclaration"

// keepLosingDeclaration keeps the redeclaration verbatim under Unmodeled beside
// the merged property, so a document consumer sees what the loser said (GitHub
// #424).
//
// The value is the construct source renders, not the dropped IR values (GitHub
// #445): Unmodeled holds what the document wrote (ir-design §12), and a TypeID
// in a byte slice is invisible to irverify. The key ends in src's pointer, so
// each loser keeps its own entry.
//
// A loser with no pointer is skipped, since PreserveInto overwrites a collapsed
// key; no production path produces one. A source that will not render is
// reported as an error (GitHub #144).
func (g *Merger) keepLosingDeclaration(dst, src *ir.Property, source func() (ir.RawValue, error)) {
	pointer := src.Provenance.Pointer
	if pointer == "" {
		return
	}
	key := losingDeclarationKey + string(pointer)
	raw, err := source()
	if err != nil {
		g.Report(ir.SeverityError, diag.UnpreservableConstruct, pointer,
			"%s could not be kept verbatim under Unmodeled and is represented in the IR "+
				"in no form at all: %s", key, err.Error())
		return
	}
	// Degraded lowering (ir-design §4.8): the IR has no combinator for two
	// shapes or two defaults at one position.
	annotation.PreserveInto(&dst.Unmodeled, key, raw,
		ir.ReasonDegradedLowering, src.Provenance)
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
		"declarations of field %q disagree: %s; kept the first declaration (%s) over the redeclaration (%s), "+
			"which is kept verbatim under Unmodeled",
		dst.WireName, detail, dst.Provenance.Pointer, pointer)
}

// typesConflict reports whether two reconciled property types describe an
// unsatisfiable intersection. Identical targets and the schemaless top type
// never conflict. Different underlying primitives conflict, as does a scalar
// against a structural type. Two distinct composites of one kind are not
// provably contradictory, so they are never reported.
//
// KNOWN GAP (GitHub #446): PrimKinds compare for equality with no notion of
// narrowing, so {string, format: uri} against a bare {string} reads as a
// conflict though the merge loses nothing, and the diagnostic and preserved
// entry claim otherwise. Every format-narrowing pair is affected.
func (g *Merger) typesConflict(a, b ir.TypeRef) bool {
	if a.Target == b.Target || g.isAnyType(a) || g.isAnyType(b) {
		return false
	}
	ak, aok := g.resolvePrimKind(a)
	bk, bok := g.resolvePrimKind(b)
	switch {
	case aok && bok:
		return ak != bk
	case aok && !bok:
		return g.isStructuralType(b)
	case !aok && bok:
		return g.isStructuralType(a)
	default:
		return g.differentTypeKind(a, b)
	}
}

// isAnyType reports whether ref targets the schemaless top type (a PrimAny
// primitive or an Any node). The top type imposes no constraint under allOf
// intersection, so it never conflicts with a sibling redeclaration.
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
// string enum conflicts with an integer redeclaration but not a string one. A
// Literal is deliberately left unresolved: Value.Kind doesn't map cleanly to
// one PrimKind (a ValueNumber literal spans integer/number/float/decimal), so
// rather than guess it is excluded from isStructuralType below too.
func (g *Merger) resolvePrimKind(ref ir.TypeRef) (ir.PrimKind, bool) {
	id := ref.Target
	for range maxTypeResolveDepth {
		td, ok := g.Resolve(id)
		if !ok {
			return "", false
		}
		switch t := td.(type) {
		case *ir.Primitive:
			return t.Prim, true
		case *ir.Enum:
			return t.ValueType, true
		case *ir.Scalar:
			if t.Base == nil {
				return "", false
			}
			id = t.Base.Target
		default:
			return "", false
		}
	}
	return "", false
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
	return at.Kind() != bt.Kind()
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
