// Package merge reconciles the properties more than one allOf branch declares:
// either folding the declarations into one or reporting that they disagree.
//
// It is the compiler's most intricate logic and the part least tied to the walk
// that drives it. The walk hands it each declaration through a PropIndex, and a
// Merger folds a field's declarations once the registry is whole, reading it
// through a function, so the conflict lattice can be exercised against a map
// and a recorder rather than a document.
package merge

import (
	"cmp"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/spelling"
	"github.com/dexpace/morphic/ir"
)

// Reporter records one diagnostic at pointer, attributed to the input document
// that supplied that position.
type Reporter func(sev ir.Severity, code string, pointer jsontext.Pointer, format string, args ...any)

// Source renders the construct a declaration was written as, verbatim. It is a
// function because nearly every property is declared once, and rendering a node
// nobody will keep is a cost the merge asks for only when it has something to
// keep.
type Source func() (ir.RawValue, error)

// Settle folds what a PropIndex queued against reg, the registry every
// lowering left, reporting through report.
type Settle func(reg ir.TypeRegistry, report Reporter)

// WireNameIndex maps each property's wire name to its position in props, so a
// lookup by name is one map read rather than a rescan of a wide model.
func WireNameIndex(props []ir.Property) map[string]int {
	idx := make(map[string]int, len(props))
	for i := range props {
		idx[props[i].WireName] = i
	}
	return idx
}

// PropIndex is the properties of one Model as the walk declares them. The
// first declaration of a wire name lands on the model at once, so the walk can
// read its ID and mark it required; every later one is recorded, and the
// model's fields fold once the registry is whole (Settle). Which type a field
// folds to compares the nodes its declarations name, which the walk may not
// have built yet, so a fold mid-walk would follow declaration order (GitHub
// #898).
type PropIndex struct {
	m      *ir.Model
	later  func(Settle)
	byWire map[string]*field
	// redeclared lists the fields declared more than once, in the order of
	// their first redeclaration.
	redeclared []*field
}

// field is one wire name of the model: where its property sits, how to render
// the declaration that put it there, and each declaration after that one.
type field struct {
	pos   int
	first Source
	rest  []declaration
}

// NewPropIndex returns an empty index of m's properties; m holds none yet, so
// every property reaches it through MergeProperty. later queues the model's
// folds, once, at its first redeclaration, for once every lowering has
// returned; it must not be nil.
func NewPropIndex(m *ir.Model, later func(Settle)) *PropIndex {
	return &PropIndex{m: m, later: later, byWire: map[string]*field{}}
}

// MergeProperty appends p to the model, or records it as one more declaration
// of the field that already carries its wire name: overlapping allOf branches
// (and properties co-declared alongside allOf) redeclare one logical field
// under allOf's intersection semantics (ir-design §4.3). Callers must set
// p.WireName (fillModelProperties always does); it keys idx directly.
//
// p.Provenance locates the declaration, and is the only source of that fact
// here. It used to arrive twice — as a parameter and on p — equal by
// construction at the one call site, which left nothing able to catch them
// disagreeing.
func (idx *PropIndex) MergeProperty(p ir.Property, source Source) {
	f, ok := idx.byWire[p.WireName]
	if !ok {
		idx.byWire[p.WireName] = &field{pos: len(idx.m.Properties), first: source}
		idx.m.Properties = append(idx.m.Properties, p)
		return
	}
	if len(f.rest) == 0 {
		if len(idx.redeclared) == 0 {
			idx.later(idx.settle)
		}
		idx.redeclared = append(idx.redeclared, f)
	}
	f.rest = append(f.rest, declaration{prop: p, source: source})
}

// settle folds each field declared more than once against reg, in the order of
// its first redeclaration. A field's first declaration is read off the model as
// the walk left it, required flag included. A fold reads only nodes the walk
// built and writes only its own field, so no fold reads another's result.
func (idx *PropIndex) settle(reg ir.TypeRegistry, report Reporter) {
	g := Merger{
		Resolve: func(id ir.TypeID) (ir.TypeDef, bool) { td, ok := reg[id]; return td, ok },
		Report:  report,
	}
	for _, f := range idx.redeclared {
		decls := append([]declaration{{prop: idx.m.Properties[f.pos], source: f.first}}, f.rest...)
		idx.m.Properties[f.pos] = g.fold(decls)
	}
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
// conflict because nothing is discarded; the fold adds a warning (foldUse).
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

// dropVerdict classifies a drop: the judgment's own disjoint and overlap
// verdicts stand, and typesConflict classifies any other pair.
func (g *Merger) dropVerdict(rel relation, a, b ir.TypeRef) redeclVerdict {
	if rel == relDisjoint {
		return verdictConflict
	}
	if rel == relOverlap {
		return verdictOverlap
	}
	return g.typesConflict(a, b)
}

// intersectNull returns dst admitting null only where both references do, the
// rule foldNullVerdicts states for a single schema. The result's null rests on
// a heuristic wherever either side's did, since the declared reading admits it
// only where both declare it, so it names every marker either side carries,
// dst's first and none twice.
func intersectNull(dst, src ir.TypeRef) ir.TypeRef {
	dst.Nullable = dst.Nullable && src.Nullable
	if !dst.Nullable {
		dst.Inferred = ""
		return dst
	}
	dst.Inferred = joinMarkers(dst.Inferred, src.Inferred)
	return dst
}

// joinMarkers adds each marker of the comma-separated list more to list, in
// order, unless it is empty or already there (joinMarker).
func joinMarkers(list, more string) string {
	for marker := range strings.SplitSeq(more, ",") {
		list = joinMarker(list, marker)
	}
	return list
}

// joinMarker adds marker to the comma-separated list of heuristic names in
// list, unless it is empty or already there.
func joinMarker(list, marker string) string {
	switch {
	case marker == "" || slices.Contains(strings.Split(list, ","), marker):
		return list
	case list == "":
		return marker
	default:
		return list + "," + marker
	}
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

// redeclVerdict classifies a declaration's type the fold drops, against the
// type the field keeps.
type redeclVerdict int

const (
	// verdictUncompared is a pair the merge does not guess about: composites of
	// one kind, an unresolvable target, or a degraded node standing for the top
	// type. Nothing says what their intersection holds.
	verdictUncompared redeclVerdict = iota
	// verdictConflict is a pair reported as incompatible: a scalar against a
	// structure, two structure kinds, or disjoint primitive kinds.
	verdictConflict
	// verdictOverlap is a pair whose primitive kinds share values, or that holds
	// a literal, but which the merge does not narrow to one.
	verdictOverlap
)

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
		return verdictUncompared
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
// other pair to verdictUncompared.
func conflictIf(incompatible bool) redeclVerdict {
	if incompatible {
		return verdictConflict
	}
	return verdictUncompared
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

// constraintKeyword is one constraint keyword the merge compares across
// declarations: whether a constraint set sets it, and whether two sets that
// both do pin it to different values, described as "conflicting maxLength (10
// and 20)".
type constraintKeyword struct {
	set    func(c *ir.Constraints) bool
	differ func(a, b *ir.Constraints) (string, bool)
}

// constraintKeywords lists the keywords the merge compares, in the fixed order
// constraintsConflict tries them, so the one named when several conflict is
// deterministic. UniqueItems is absent: a plain bool cannot tell false from
// unset, and under intersection a true from either side is always right.
func constraintKeywords() []constraintKeyword {
	return []constraintKeyword{
		bigValKeyword("minimum", func(c *ir.Constraints) *ir.BigVal { return c.Min }),
		bigValKeyword("exclusiveMinimum", func(c *ir.Constraints) *ir.BigVal { return c.ExclusiveMin }),
		bigValKeyword("maximum", func(c *ir.Constraints) *ir.BigVal { return c.Max }),
		bigValKeyword("exclusiveMaximum", func(c *ir.Constraints) *ir.BigVal { return c.ExclusiveMax }),
		bigValKeyword("multipleOf", func(c *ir.Constraints) *ir.BigVal { return c.MultipleOf }),
		intKeyword("precision", func(c *ir.Constraints) *int64 { return c.Precision }),
		intKeyword("scale", func(c *ir.Constraints) *int64 { return c.Scale }),
		intKeyword("minLength", func(c *ir.Constraints) *int64 { return c.MinLength }),
		intKeyword("maxLength", func(c *ir.Constraints) *int64 { return c.MaxLength }),
		intKeyword("minItems", func(c *ir.Constraints) *int64 { return c.MinItems }),
		intKeyword("maxItems", func(c *ir.Constraints) *int64 { return c.MaxItems }),
		intKeyword("minProps", func(c *ir.Constraints) *int64 { return c.MinProps }),
		intKeyword("maxProps", func(c *ir.Constraints) *int64 { return c.MaxProps }),
		strKeyword("pattern", func(c *ir.Constraints) string { return c.Pattern }),
		strKeyword("patternMessage", func(c *ir.Constraints) string { return c.PatternMessage }),
	}
}

// bigValKeyword is the arbitrary-precision keyword name, read by of.
func bigValKeyword(name string, of func(*ir.Constraints) *ir.BigVal) constraintKeyword {
	return constraintKeyword{
		set:    func(c *ir.Constraints) bool { return of(c) != nil },
		differ: func(a, b *ir.Constraints) (string, bool) { return bigValConflictDetail(name, of(a), of(b)) },
	}
}

// intKeyword is the integer keyword name, read by of.
func intKeyword(name string, of func(*ir.Constraints) *int64) constraintKeyword {
	return constraintKeyword{
		set:    func(c *ir.Constraints) bool { return of(c) != nil },
		differ: func(a, b *ir.Constraints) (string, bool) { return intConflictDetail(name, of(a), of(b)) },
	}
}

// strKeyword is the string keyword name, read by of.
func strKeyword(name string, of func(*ir.Constraints) string) constraintKeyword {
	return constraintKeyword{
		set:    func(c *ir.Constraints) bool { return of(c) != "" },
		differ: func(a, b *ir.Constraints) (string, bool) { return strConflictDetail(name, of(a), of(b)) },
	}
}

// constraintsConflict reports the first keyword of constraintKeywords that two
// constraint sets pin to different values: its index and the disagreement. A
// keyword set on one side only is not a conflict, since mergeConstraints adopts
// it. Numeric bounds compare by magnitude, so 10 and 10.0 do not conflict.
func constraintsConflict(a, b *ir.Constraints) (keyword int, detail string, conflict bool) {
	if a == nil || b == nil {
		return 0, "", false
	}
	for i, kw := range constraintKeywords() {
		if detail, conflict := kw.differ(a, b); conflict {
			return i, detail, true
		}
	}
	return 0, "", false
}

// bigValConflictDetail reports whether both branches pin the same
// arbitrary-precision keyword to different magnitudes, formatting the
// disagreement when they do. Every numeric keyword of ir.Constraints has this
// shape, each bound stating its own restriction, so one comparison serves them
// all.
//
// A disagreement is usually still satisfiable (minimum 10 in one branch and 20
// in the other together mean 20), but it is diagnosed anyway: the merge keeps
// the first declaration's value over the true intersection, and the discarded
// one may be the stricter, so silence would loosen the validation the spec
// intended.
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
