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
	// Report records one diagnostic, stamped with the compile's source index.
	Report func(sev ir.Severity, code, pointer, format string, args ...any)
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

// reconcileProperty folds a redeclaration src into the already-present property
// dst under allOf intersection semantics: required and secret are OR-ed, dst
// keeps its position/identity/type shape (first declaration wins), visibility
// narrows to the lifecycles both branches admit (mergeVisibility — this is an
// intersection, not a plain adopt-if-absent, since either side may already be
// restricted, and narrowing it to nothing is warned about rather than merely
// recorded), and every optional detail dst lacks — docs, default,
// constraints (merged per keyword via mergeConstraints), deprecation, XML,
// examples — is adopted from src.
//
// Unmodeled is the one field where a later branch wins: its entries are keyed
// and namespaced, so the two branches' keys union rather than compete, and a key
// both branches write is the same construct written twice.
//
// Whatever of src the fold cannot carry — an incompatible type, a contradictory
// constraint keyword, or a description, default, examples, deprecation or XML
// hint that dst already holds differently — is reported where it is dropped
// rather than silently losing to the first declaration, and the redeclaration
// is then kept whole beside the winner (keepLosingDeclaration), so what it said
// survives in the document and not only in the diagnostic stream. source
// renders it, and is called only when there is something to keep.
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
// held differently by src — which the fold cannot carry, so src's are lost.
// A constraint keyword both declare differently is recordRedeclarationConflict's
// to report; mergeConstraints keeps dst's.
//
// A Value is a tree and an Example holds several, so "held differently" is a
// deep comparison. reflect.DeepEqual tells a nil slice from an empty one, but
// both sides here were lowered by the same code from the same document, so two
// spellings the source wrote alike cannot come out on opposite sides of that.
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
func (g *Merger) detailDiffersDiag(dst *ir.Property, pointer, detail string) {
	g.Report(ir.SeverityInfo, diag.DegradedConstruct, pointer,
		"declarations of field %q give its %s differently; kept the first declaration, "+
			"with the redeclaration verbatim under Unmodeled", dst.WireName, detail)
}

// mergeConstraints folds src's constraint keywords into dst under allOf
// intersection semantics: dst keeps every keyword it already sets, and adopts
// from src any keyword dst leaves unset (nil/""/false) — a keyword only one
// branch constrains still applies to the merged field, so it is never dropped.
//
// The four numeric bounds are four keywords, not two bounds with an
// exclusivity flag apiece, so each is adopted on its own: a branch declaring
// only exclusiveMinimum contributes it to a merged field whose minimum came
// from elsewhere, and neither displaces the other. UniqueItems has no absent
// state to detect via cmp.Or, but under intersection a true from either branch
// is always correct, so adopting it via cmp.Or never wrongly downgrades dst
// from true to false.
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

// mergeVisibility folds redeclaration src's lifecycle visibility into dst
// under allOf intersection semantics (ir-design §5.2): an instance must
// satisfy every branch, so the merged property is visible only in a
// lifecycle both branches admit.
//
// This cannot be a plain append: an empty Only (None false) means "visible in
// every lifecycle," not "visible in none," so each side must be read as the
// set it actually admits — ∅ under None, the open universal set under an
// unrestricted empty Only, or its own Only otherwise — before intersecting.
// A None on either side already denotes ∅, which intersected with anything
// stays ∅.
//
// Two branches admitting disjoint lifecycles (readOnly paired with writeOnly
// on the same field) intersect to ∅ too: every lifecycle is excluded. That is
// recorded as None — the shape the IR already has for "invisible everywhere"
// (TypeSpec's @invisible) — rather than raised through
// recordRedeclarationConflict. Unlike an incompatible-type redeclaration,
// nothing here is arbitrarily discarded: ∅ is the exact intersection, not a
// guess between two unrepresentable shapes.
//
// Exact is not the same as unremarkable, though, so reconcileProperty pairs
// that result with a diag.DisjointVisibility warning. The IR keeps the honest
// answer; the document still gets told that its composition left a field no
// request or response can carry.
//
// The result is the same whichever branch arrives as dst, down to the order of
// Only — allOf orders its branches but gives that order no meaning, so a merge
// answering differently under a swap would make the IR depend on how the
// composition was spelled. intersectLifecycles is what carries that through.
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
// when they share none. mergeVisibility is the only caller, and it maps a nil
// result to None rather than an empty-but-unrestricted Only.
//
// Both operands are source order, so which of them the result inherits its
// order from is settled by value rather than by which branch was declared
// first: two branches naming the same lifecycles in different orders would
// otherwise intersect to two different slices depending on the spelling.
// annotation.EffectiveVisibility yields one of two fixed sets, either identical
// or disjoint — and None outright where a position is in both, which
// mergeVisibility settles before reaching here — so no OpenAPI document reaches
// a partial overlap today. But this helper is the contract a second visibility
// source would inherit, and an ordering rule that reads off dst is the kind that
// surfaces only once something already depends on it.
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

// recordRedeclarationConflict folds src's type into dst and reports what the
// fold could not represent: an incompatible target type, or a constraint keyword
// both branches pin to different values. It alters dst — intersecting
// nullability, and keeping a dropped type under Unmodeled — as well as
// diagnosing, which is why it is named for recording rather than for diagnosis.
//
// It returns whether src's type was dropped — the target differs, so dst keeps
// its own and src's goes nowhere — which is what tells reconcileProperty not to
// carry that shape's details onto the winner; and whether anything of src was
// lost at all, the type or a constraint keyword, which is what tells it to keep
// the redeclaration whole.
//
// Dropped is a wider set than the conflicts worth reporting: typesConflict
// deliberately does not guess about two composites of one kind, an
// unresolvable target, or the top type against anything, and in each of those
// dst keeps its own type while src's vanishes. Keeping the declaration there
// too is what stops a consumer diffing two versions from seeing no change
// (GitHub #424); the diagnostic stays on the narrower predicate, because
// "dropped" and "contradictory" are different claims.
//
// A type conflict is genuinely unsatisfiable; a
// constraint conflict is usually satisfiable alone, but the merge can't
// represent the true intersection and may keep the looser bound
// (diag.ConflictingRedecl). At most one diagnostic fires: a type conflict
// subsumes any constraint conflict.
func (g *Merger) recordRedeclarationConflict(dst, src *ir.Property) (dropped, lost bool) {
	pointer := src.Provenance.Pointer
	if dst.Type.Target != src.Type.Target {
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

// keepLosingDeclaration keeps the redeclaration verbatim beside the merged
// property, so a consumer reading the document rather than the diagnostic
// stream can still see what the losing declaration said (GitHub #424).
//
// ReasonDegradedLowering is the reason: the IR has no combinator for "string
// here, integer there", or for two defaults, so the pair is lowered to the
// first declaration's shape with the other kept beside it, which is that
// reason's own definition (ir-design §4.8). Not ReasonNoIRHome — the position
// has a field and it is holding the winner, so nothing is waiting on an IR gap
// to close; not ReasonValidationOnly, since a redeclaration is data shape
// rather than validation.
//
// The value is the source construct, rendered by source, and not the IR
// values the merge dropped (GitHub #445): §12 defines an Unmodeled value as
// what the document wrote, a TypeID is a compiler-minted registry ID that
// irverify's reference walk cannot see dangle inside a byte slice, and one
// verbatim node covers every field the fold drops at once where an IR value
// covers one. It is also the whole of what was said — a `$ref` as the `$ref`
// the position wrote, a nullability, a default — with nothing left to lose
// on a field the fold has not learned to compare yet.
//
// The redeclaration's pointer is part of the key rather than only of the
// provenance, so a field three branches type three incompatible ways keeps all
// three entries; a fixed key would leave whichever branch ran last. Provenance
// locates the losing declaration itself, which is the entry's own position and
// not the merged property's. Both are read off src, so the key and the
// provenance cannot disagree about where the loser was written.
//
// A loser with no pointer is not recorded: the key would collapse to the bare
// prefix, and PreserveInto is a plain overwrite, so a second such loser would
// silently replace the first. No production path produces one — ProvenanceAt
// always stamps the pointer it was given — which is why this is a guard rather
// than a diagnostic. A source that will not render is a diagnostic, and an
// error: the merge has already said the redeclaration is kept, and what
// reached the IR in no form must not be left to that announcement (GitHub
// #144).
func (g *Merger) keepLosingDeclaration(dst, src *ir.Property, source func() (ir.RawValue, error)) {
	pointer := src.Provenance.Pointer
	if pointer == "" {
		return
	}
	key := losingDeclarationKey + pointer
	raw, err := source()
	if err != nil {
		g.Report(ir.SeverityError, diag.UnpreservableConstruct, pointer,
			"%s could not be kept verbatim under Unmodeled and is represented in the IR "+
				"in no form at all: %s", key, err.Error())
		return
	}
	annotation.PreserveInto(&dst.Unmodeled, key, raw,
		ir.ReasonDegradedLowering, pointer, src.Provenance.Source)
}

// redeclarationConflictDiag emits the shared conflicting-redeclaration warning,
// naming the field and both declaration sites (dst's own, and the redeclaration
// at pointer). detail is the caller-formatted disagreement — two type IDs, or a
// constraint keyword and its two values — so one wording serves both callers.
// It says "declarations", not "allOf branches": dst's declaration is not always
// inside an allOf branch (a property can also be declared directly alongside
// allOf), so the message must read correctly either way. Severity is warning —
// the merged model is still usable — leaving escalation to the consumer via
// the stable code. Either way the redeclaration is kept whole beside the
// winner (keepLosingDeclaration), which the message says so a reader knows
// where to look.
func (g *Merger) redeclarationConflictDiag(dst *ir.Property, pointer, detail string) {
	g.Report(ir.SeverityWarning, diag.ConflictingRedecl, pointer,
		"declarations of field %q disagree: %s; kept the first declaration (%s) over the redeclaration (%s), "+
			"which is kept verbatim under Unmodeled",
		dst.WireName, detail, dst.Provenance.Pointer, pointer)
}

// typesConflict reports whether two reconciled property types describe an
// unsatisfiable intersection. Identical interned targets and the schemaless top
// type never conflict. Types resolving to different underlying primitives conflict
// (string vs integer, string vs uuid), as does a scalar against a structural type.
// Two distinct composite types of the same kind (two models, two lists) are not
// provably contradictory, so they are never reported — conflict detection does
// not guess.
// KNOWN GAP (GitHub #446): PrimKinds are compared for equality with no notion
// of narrowing, so {string, format: uri} against a bare {string} reads as a
// conflict though the intersection is exactly the url the merge keeps — nothing
// is lost, and both a diagnostic and a preserved entry claim otherwise. The same
// holds for every format-narrowing pair (date-time/string, int32/integer,
// double/number, uuid/string), which the published GitHub spec writes
// throughout.
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
// It follows the Base chain rather than reading only the target node, because a
// position that wrote something only a node can hold gets an alias over the top
// type instead of resolving straight to it. Reading the alias alone would let
// resolvePrimKind answer PrimAny below and then compare it unequal to the
// sibling's kind — reporting the top type as a conflict, which is the one thing
// this function exists to rule out.
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
// to incompatible values, describing which keyword and both values when they
// do (e.g. "conflicting maxLength (10 and 20)"). A keyword set on only one
// side is not a conflict — mergeConstraints adopts it, narrowing the merged
// field rather than discarding it — so only a keyword present and differing on
// both sides counts; numeric bounds compare by magnitude, so 10 and 10.0
// aren't a false conflict. UniqueItems is never compared: it's a plain bool
// with no absent state, so a false on either side can't be told apart from
// "not set". Keywords are checked in the fixed order below, so the keyword
// named when several conflict at once is deterministic.
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
// arbitrary-precision keyword and pin it to different magnitudes, formatting
// the disagreement when they do. Every numeric keyword of ir.Constraints has
// this one shape — each of the four bounds states its own restriction, with no
// exclusivity sense to carry beside it — so one comparison serves them all, by
// magnitude, which is what keeps 10 and 10.0 from reading as a disagreement.
//
// A disagreement between two branches is usually still individually satisfiable
// (minimum: 10 in one and minimum: 20 in the other together just mean ">= 20"),
// but it's diagnosed anyway: the merge keeps dst's value (first declaration
// wins) over the true intersection, and the discarded one may be the stricter —
// staying silent would silently loosen the validation the spec intended.
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
