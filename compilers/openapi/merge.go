package openapi

import (
	"cmp"
	"fmt"
	"math/big"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/ir"
)

// A merger reconciles properties that more than one allOf branch declares:
// either merging the two declarations or reporting that they disagree.
//
// This is the package's largest cohesive block and its most intricate logic, and
// it sits apart from the walk so it can be exercised without one. Its only
// couplings to the rest of lowering are the two fields below, which is what makes
// that possible: a test supplies a map lookup and a recorder rather than standing
// up a compiler.
//
// It is not pure — deciding whether two TypeRefs conflict needs the registry —
// but that dependency is narrow enough to pass as a function, so the conflict
// lattice stays testable against a stub.
type merger struct {
	// resolve looks a type up in the registry. Conflict detection compares what
	// two references point at, not the references themselves.
	resolve func(ir.TypeID) (ir.TypeDef, bool)
	// report records one diagnostic, stamped with the compile's source index.
	report func(sev ir.Severity, code, pointer, format string, args ...any)
}

// wireNameIndex maps each property's wire name to its position in props, so a
// redeclaration reconciles in one lookup rather than a rescan (fillModelProperties
// runs once per allOf branch, so a linear scan would be quadratic in a wide model).
func wireNameIndex(props []ir.Property) map[string]int {
	idx := make(map[string]int, len(props))
	for i := range props {
		idx[props[i].WireName] = i
	}
	return idx
}

// mergeProperty appends p to m and records it in byWire, or folds it into the
// property that already carries the same wire name — overlapping allOf branches
// (and properties co-declared alongside allOf) redeclare one logical field under
// allOf's intersection semantics (ir-design §4.3). Callers must set p.WireName
// (fillModelProperties always does); it keys byWire directly.
func (g *merger) mergeProperty(m *ir.Model, byWire map[string]int, p ir.Property, pointer string) {
	if i, ok := byWire[p.WireName]; ok {
		g.reconcileProperty(&m.Properties[i], p, pointer)
		return
	}
	byWire[p.WireName] = len(m.Properties)
	m.Properties = append(m.Properties, p)
}

// reconcileProperty folds a redeclaration src into the already-present property
// dst under allOf intersection semantics: required and secret are OR-ed, dst
// keeps its position/identity/type shape (first declaration wins), and every
// optional detail dst lacks — docs, default, constraints (merged per keyword via
// mergeConstraints), deprecation, XML, examples — is adopted from src.
//
// Unmodeled is the one field where a later branch wins: its entries are keyed
// and namespaced, so the two branches' keys union rather than compete, and a key
// both branches write is the same construct written twice. A description that
// differs between branches, an incompatible type, or a contradictory constraint
// keyword are genuine conflicts the merge cannot represent (see
// diag.ConflictingRedecl); each is diagnosed before any detail is folded in,
// rather than silently picking an arbitrary winner.
func (g *merger) reconcileProperty(dst *ir.Property, src ir.Property, pointer string) {
	g.diagnoseRedeclarationConflict(dst, &src, pointer)

	dst.Required = dst.Required || src.Required
	dst.Secret = dst.Secret || src.Secret

	if dst.Docs.Description == "" {
		dst.Docs.Description = src.Docs.Description
	} else if src.Docs.Description != "" && src.Docs.Description != dst.Docs.Description {
		g.report(ir.SeverityInfo, diag.DegradedConstruct, pointer,
			"allOf branches describe field %q differently; kept the first declaration", dst.WireName)
	}
	dst.Default = cmp.Or(dst.Default, src.Default)
	dst.Constraints = mergeConstraints(dst.Constraints, src.Constraints)
	dst.Deprecation = cmp.Or(dst.Deprecation, src.Deprecation)
	dst.XML = cmp.Or(dst.XML, src.XML)
	if len(dst.Examples) == 0 {
		// Examples is a slice, not comparable, so it cannot go through cmp.Or
		// like its neighbors above; the len()==0 predicate is the adoption rule.
		dst.Examples = src.Examples
	}
	dst.Unmodeled = mergeUnmodeled(dst.Unmodeled, src.Unmodeled)
}

// mergeConstraints folds src's constraint keywords into dst under allOf
// intersection semantics: dst keeps every keyword it already sets, and adopts
// from src any keyword dst leaves unset (nil/""/false) — a keyword only one
// branch constrains still applies to the merged field, so it is never dropped.
//
// Min and Max are adopted together with their exclusivity flag: taking src.Min
// without src.ExclusiveMin would silently flip an exclusive "> 5" into an
// inclusive ">= 5". UniqueItems has no absent state to detect via cmp.Or, but
// under intersection a true from either branch is always correct, so adopting
// it via cmp.Or never wrongly downgrades dst from true to false.
func mergeConstraints(dst, src *ir.Constraints) *ir.Constraints {
	if dst == nil {
		return src
	}
	if src == nil {
		return dst
	}
	if dst.Min == nil {
		dst.Min, dst.ExclusiveMin = src.Min, src.ExclusiveMin
	}
	if dst.Max == nil {
		dst.Max, dst.ExclusiveMax = src.Max, src.ExclusiveMax
	}
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

// maxTypeResolveDepth bounds Base-chain resolution when classifying a property
// type for conflict detection (styleguide bounded-recursion rule). A scalar Base
// chain is far shorter than this in a well-formed document, so the cap only
// guards a pathological or malformed registry.
const maxTypeResolveDepth = 64

// diagnoseRedeclarationConflict reports when redeclaration src contradicts dst
// under allOf intersection — an incompatible target type, or a constraint
// keyword both branches pin to different values — without altering the merge
// (dst keeps its shape). A type conflict is genuinely unsatisfiable; a
// constraint conflict is usually satisfiable alone, but the merge can't
// represent the true intersection and may keep the looser bound
// (diag.ConflictingRedecl). At most one diagnostic fires: a type conflict
// subsumes any constraint conflict.
func (g *merger) diagnoseRedeclarationConflict(dst, src *ir.Property, pointer string) {
	if g.typesConflict(dst.Type, src.Type) {
		g.redeclarationConflictDiag(dst, pointer,
			fmt.Sprintf("incompatible types %s and %s", dst.Type.Target, src.Type.Target))
		return
	}
	if detail, ok := constraintsConflict(dst.Constraints, src.Constraints); ok {
		g.redeclarationConflictDiag(dst, pointer, detail)
	}
}

// redeclarationConflictDiag emits the shared conflicting-redeclaration warning,
// naming the field and both declaration sites (dst's own, and the redeclaration
// at pointer). detail is the caller-formatted disagreement — two type IDs, or a
// constraint keyword and its two values — so one wording serves both callers.
// It says "declarations", not "allOf branches": dst's declaration is not always
// inside an allOf branch (a property can also be declared directly alongside
// allOf), so the message must read correctly either way. Severity is warning —
// the merged model is still usable — leaving escalation to the consumer via
// the stable code.
func (g *merger) redeclarationConflictDiag(dst *ir.Property, pointer, detail string) {
	g.report(ir.SeverityWarning, diag.ConflictingRedecl, pointer,
		"declarations of field %q disagree: %s; kept the first declaration (%s) over the redeclaration (%s)",
		dst.WireName, detail, dst.Provenance.Pointer, pointer)
}

// typesConflict reports whether two reconciled property types describe an
// unsatisfiable intersection. Identical interned targets and the schemaless top
// type never conflict. Types resolving to different underlying primitives conflict
// (string vs integer, string vs uuid), as does a scalar against a structural type.
// Two distinct composite types of the same kind (two models, two lists) are not
// provably contradictory, so they are never reported — conflict detection does
// not guess.
func (g *merger) typesConflict(a, b ir.TypeRef) bool {
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
func (g *merger) isAnyType(ref ir.TypeRef) bool {
	if k, ok := g.resolvePrimKind(ref); ok {
		return k == ir.PrimAny
	}
	td, ok := g.resolve(ref.Target)
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
func (g *merger) resolvePrimKind(ref ir.TypeRef) (ir.PrimKind, bool) {
	id := ref.Target
	for range maxTypeResolveDepth {
		td, ok := g.resolve(id)
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
func (g *merger) differentTypeKind(a, b ir.TypeRef) bool {
	at, aok := g.resolve(a.Target)
	bt, bok := g.resolve(b.Target)
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
func (g *merger) isStructuralType(ref ir.TypeRef) bool {
	td, ok := g.resolve(ref.Target)
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
		func() (string, bool) {
			return boundConflictDetail("minimum", a.Min, b.Min, a.ExclusiveMin, b.ExclusiveMin)
		},
		func() (string, bool) {
			return boundConflictDetail("maximum", a.Max, b.Max, a.ExclusiveMax, b.ExclusiveMax)
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

// boundConflictDetail reports whether two numeric bounds, each with its
// exclusivity flag, are both present and disagree in magnitude or in
// inclusive/exclusive sense, formatting the disagreement when they do. Such a
// disagreement is usually still individually satisfiable (minimum: 10 and
// exclusiveMinimum: 10 together just mean "> 10"), but it's diagnosed anyway:
// the merge keeps dst's bound (first declaration wins) over the true
// intersection, and the discarded bound is always the stricter one — staying
// silent would silently loosen the validation the spec intended.
func boundConflictDetail(keyword string, a, b *ir.BigVal, exclA, exclB bool) (string, bool) {
	if a == nil || b == nil || (exclA == exclB && bigValEqual(*a, *b)) {
		return "", false
	}
	return fmt.Sprintf("conflicting %s (%s and %s)", keyword, boundText(*a, exclA), boundText(*b, exclB)), true
}

// boundText renders a numeric bound for a conflict detail, marking an
// exclusive bound so "conflicting minimum (10 and exclusive 10)" reads as the
// differing sense it is, not a duplicate magnitude.
func boundText(v ir.BigVal, exclusive bool) string {
	if exclusive {
		return "exclusive " + v.String()
	}
	return v.String()
}

// bigValConflictDetail reports whether two optional numeric values are both
// present and differ by magnitude, formatting the disagreement when they do.
func bigValConflictDetail(keyword string, a, b *ir.BigVal) (string, bool) {
	if a == nil || b == nil || bigValEqual(*a, *b) {
		return "", false
	}
	return fmt.Sprintf("conflicting %s (%s and %s)", keyword, a.String(), b.String()), true
}

// bigValEqual reports whether two numeric literals denote the same value,
// comparing by magnitude so equal values spelled differently (10, 10.0, 1e1) are
// equal. Both are already-validated BigVals, so parsing succeeds; an unparseable
// pair falls back to exact string equality.
func bigValEqual(a, b ir.BigVal) bool {
	ar, aok := new(big.Rat).SetString(a.String())
	br, bok := new(big.Rat).SetString(b.String())
	if !aok || !bok {
		return a == b
	}
	return ar.Cmp(br) == 0
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
