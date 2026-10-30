// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package merge

import (
	"reflect"
	"strings"

	"github.com/dexpace/morphic/compilers/openapi/internal/spelling"
	"github.com/dexpace/morphic/ir"
)

// relation is the verdict of comparing the types of one field declared twice
// under allOf (docs/compilers/openapi.md#allof-redeclared-property).
type relation int

const (
	// relUnrelated is a pair nothing relates, such as two composites of one
	// kind, a union or an opaque scalar against a kind, the top type spelled
	// as an alias, a degraded node, nesting past maxJudgeDepth, or a type the
	// registry does not hold. The fold drops the second and says it does not
	// compare.
	relUnrelated relation = iota
	// relDisjoint is a pair no value satisfies together, but a null both sides
	// admit.
	relDisjoint
	// relOverlap is a pair that shares values and that the judgment does not
	// narrow to one.
	relOverlap
	// relSame is a pair that is one type.
	relSame
	// relANarrower says the first type is within the second.
	relANarrower
	// relBNarrower says the second type is within the first.
	relBNarrower
)

// flipped is the relation seen from the other side.
func (r relation) flipped() relation {
	if r == relANarrower {
		return relBNarrower
	}
	if r == relBNarrower {
		return relANarrower
	}
	return r
}

// maxJudgeDepth bounds the nesting of containers the judgment compares and of
// the values it judges, so a deep or cyclic registry cannot recurse without end.
const maxJudgeDepth = 8

// typeJudge compares two types of one field, and judges whether a value is one
// of a type's. It is a type of its own, not methods on Merger, so that the
// conflict lattice's size stays the lattice's.
type typeJudge struct {
	// Resolve looks a type up in the registry.
	Resolve func(ir.TypeID) (ir.TypeDef, bool)
	// written is where the declarations a and b were written. A node is a
	// hoisted copy only where it was written at or under its declaration's
	// pointer, in the same document; one a $ref reached elsewhere was declared
	// apart.
	written [2]ir.Provenance
}

// relate compares a with b. The winner never depends on which is which:
// swapping the arguments, and written's two pointers with them, flips a
// narrowing and leaves any other verdict as it was. The arms, in order: the
// same target; a degraded Any node, which is unrelated to every type; the top
// type, which loses to anything else; then arms, and classify for a pair arms
// leaves unrelated. Every pair gets its verdict here, a field's types and two
// containers' members alike.
func (j typeJudge) relate(a, b ir.TypeRef) relation { return j.relateAt(a, b, 0) }

// relateAt is relate at a nesting depth.
func (j typeJudge) relateAt(a, b ir.TypeRef, depth int) relation {
	if a.Target == b.Target {
		return relSame
	}
	if depth > maxJudgeDepth {
		return relUnrelated
	}
	at, aok := j.Resolve(a.Target)
	bt, bok := j.Resolve(b.Target)
	if !aok || !bok {
		return relUnrelated
	}
	if isDegraded(at) || isDegraded(bt) {
		return relUnrelated
	}
	aTop, bTop := isTop(at), isTop(bt)
	switch {
	case aTop:
		return relBNarrower
	case bTop:
		return relANarrower
	}
	if rel := j.arms(a, at, b, bt, depth); rel != relUnrelated {
		return rel
	}
	return j.classify(a, at, b, bt)
}

// arms relates two resolved types, neither the top type nor degraded, that it
// can narrow: two bare primitives (spelling.Relate); a Scalar, Enum or Literal
// against a bare primitive; two identical such nodes; two lists or two maps,
// two alike in every member and detail taking the copy rule identical nodes
// take (sameNodes). Any other pair is unrelated here.
func (j typeJudge) arms(a ir.TypeRef, at ir.TypeDef, b ir.TypeRef, bt ir.TypeDef, depth int) relation {
	ap, aBare := at.(*ir.Primitive)
	bp, bBare := bt.(*ir.Primitive)
	switch {
	case aBare && bBare:
		return fromSpelling(spelling.Relate(ap.Prim, bp.Prim))
	case bBare:
		return j.nodeAgainstBare(a, at, b, bp.Prim)
	case aBare:
		return j.nodeAgainstBare(b, bt, a, ap.Prim).flipped()
	case sameNode(at, bt):
		return sameNodes(a, b, hoistedAt(at, j.written[0]), hoistedAt(bt, j.written[1]))
	}
	rel := j.containers(at, bt, depth)
	if rel == relSame {
		// Two distinct containers alike in member and detail: copies, by the
		// rule sameNodes states for the other nodes.
		return sameNodes(a, b, hoistedAt(at, j.written[0]), hoistedAt(bt, j.written[1]))
	}
	return rel
}

// classify judges a pair arms cannot narrow by what each type holds. Two that
// end in primitive kinds go through spelling.Relate: disjoint kinds, or an enum
// none of whose members the other kind admits, are disjoint, and any other
// pair overlaps. A literal against anything that holds values overlaps, since
// which values meet is not decided here. A value type against a structure is
// disjoint, and so are two kinds of structure. Two composites of one kind, or
// the top type spelled as an alias, stay unrelated: nothing says what their
// intersection holds.
func (j typeJudge) classify(a ir.TypeRef, at ir.TypeDef, b ir.TypeRef, bt ir.TypeDef) relation {
	ak, aok := j.primKind(a)
	bk, bok := j.primKind(b)
	switch {
	case aok && ak == ir.PrimAny, bok && bk == ir.PrimAny:
		return relUnrelated
	case aok && bok:
		if spelling.Relate(ak, bk) == spelling.Disjoint ||
			j.enumMisses(a, bk, b.Nullable) || j.enumMisses(b, ak, a.Nullable) {
			return relDisjoint
		}
		return relOverlap
	case aok:
		return againstKind(bt)
	case bok:
		return againstKind(at)
	case isLiteral(at) && isLiteral(bt):
		return relOverlap
	default:
		return disjointIf(objectKind(at.Kind()) != objectKind(bt.Kind()))
	}
}

// againstKind judges td, which ends in no primitive kind, against a type that
// does: a literal overlaps it, a structure is disjoint from it, and anything
// else, a union or an opaque scalar, is unrelated to it.
func againstKind(td ir.TypeDef) relation {
	if isLiteral(td) {
		return relOverlap
	}
	return disjointIf(isStructural(td))
}

// disjointIf maps a pair that cannot share a value but null to relDisjoint,
// and any other to relUnrelated.
func disjointIf(disjoint bool) relation {
	if disjoint {
		return relDisjoint
	}
	return relUnrelated
}

// isLiteral reports whether td is a Literal.
func isLiteral(td ir.TypeDef) bool {
	_, ok := td.(*ir.Literal)
	return ok
}

// isStructural reports whether td can never be a value type under allOf
// intersection: a model, list, map or tuple. An Enum, Union or External is
// not, since a member or variant can itself be a value of the kind against it,
// nor is a base-less opaque scalar, which says nothing of its values.
func isStructural(td ir.TypeDef) bool {
	switch td.(type) {
	case *ir.Model, *ir.List, *ir.MapT, *ir.Tuple:
		return true
	default:
		return false
	}
}

// objectKind folds a MapT into the Model kind: both are objects, and one
// instance can satisfy a map and a struct at once. Which of the two a position
// lowers to depends on what else it declares about its members
// (docs/ir/containers.md), not on a shape that could contradict the other.
func objectKind(k ir.TypeKind) ir.TypeKind {
	if k == ir.KindMap {
		return ir.KindModel
	}
	return k
}

// isAnyType reports whether ref targets a type that admits every value: the
// schemaless top type, bare or an alias over it, or an Any node a degraded
// lowering minted. It follows the Base chain, since a position writing
// something only a node can hold gets an alias over the top type.
func (j typeJudge) isAnyType(ref ir.TypeRef) bool {
	if k, ok := j.primKind(ref); ok {
		return k == ir.PrimAny
	}
	td, ok := j.Resolve(ref.Target)
	return ok && td.Kind() == ir.KindAny
}

// hoistedAt reports whether td is a hoisted node written at or under at, the
// declaration that holds it: in its document, at or below its pointer. A
// hoisted node a $ref reached from elsewhere, another schema's property, a
// parameter's schema or a position of another document that happens to share
// the pointer, was declared apart.
func hoistedAt(td ir.TypeDef, at ir.Provenance) bool {
	c := td.Common()
	p := c.Provenance.Pointer
	return c.Anonymous && c.Provenance.Source == at.Source &&
		(p == at.Pointer || strings.HasPrefix(string(p), string(at.Pointer)+"/"))
}

// sameNodes judges two nodes sameNode holds equal. Two hoisted copies are one
// type. A node declared elsewhere, a component or one a $ref reached, wins over
// a hoisted copy of itself in either order. Two such nodes are two types the
// source declared apart, so neither is dropped unreported: they overlap.
func sameNodes(a, b ir.TypeRef, aHoisted, bHoisted bool) relation {
	switch {
	case aHoisted && bHoisted:
		return byNull(a, b)
	case bHoisted:
		return relANarrower
	case aHoisted:
		return relBNarrower
	default:
		return relOverlap
	}
}

// sameNode reports whether a and b are two Literal, Enum or Scalar nodes equal
// but for what names their site (the ID, the naming, whether it was hoisted,
// and the provenance of the node and of what it keeps under Unmodeled) and
// their members' IDs: one declaration written twice, hoisted to two nodes.
func sameNode(a, b ir.TypeDef) bool {
	switch x := a.(type) {
	case *ir.Literal:
		y, ok := b.(*ir.Literal)
		if !ok {
			return false
		}
		x2, y2 := *x, *y
		x2.TypeCommon, y2.TypeCommon = siteless(x.TypeCommon), siteless(y.TypeCommon)
		return reflect.DeepEqual(x2, y2)
	case *ir.Scalar:
		y, ok := b.(*ir.Scalar)
		if !ok {
			return false
		}
		x2, y2 := *x, *y
		x2.TypeCommon, y2.TypeCommon = siteless(x.TypeCommon), siteless(y.TypeCommon)
		return reflect.DeepEqual(x2, y2)
	case *ir.Enum:
		y, ok := b.(*ir.Enum)
		return ok && sameEnum(*x, *y)
	default:
		return false
	}
}

// sameEnum compares two enums as sameNode does, members by position.
func sameEnum(x, y ir.Enum) bool {
	x.TypeCommon, y.TypeCommon = siteless(x.TypeCommon), siteless(y.TypeCommon)
	x.Members, y.Members = idless(x.Members), idless(y.Members)
	return reflect.DeepEqual(x, y)
}

// siteless returns c with what names where its node was declared zeroed: ID,
// Name, Anonymous, Provenance and the Provenance of each Unmodeled entry. It
// copies the Unmodeled map, which belongs to the registry.
func siteless(c ir.TypeCommon) ir.TypeCommon {
	c.ID, c.Name, c.Anonymous, c.Provenance = "", ir.Naming{}, false, ir.Provenance{}
	if c.Unmodeled == nil {
		return c
	}
	um := make(ir.Unmodeled, len(c.Unmodeled))
	for k, e := range c.Unmodeled {
		e.Provenance = ir.Provenance{}
		um[k] = e
	}
	c.Unmodeled = um
	return c
}

// idless copies members with their IDs cleared.
func idless(ms []ir.EnumMember) []ir.EnumMember {
	out := make([]ir.EnumMember, len(ms))
	for i, m := range ms {
		m.ID = ""
		out[i] = m
	}
	return out
}

// fromSpelling maps spelling's verdict on two kinds to a relation.
func fromSpelling(r spelling.Relation) relation {
	if r == spelling.Same {
		return relSame
	}
	if r == spelling.ANarrower {
		return relANarrower
	}
	if r == spelling.BNarrower {
		return relBNarrower
	}
	if r == spelling.Overlap {
		return relOverlap
	}
	return relDisjoint
}

// isTop reports whether td is the schemaless top type, a bare PrimAny. An
// *ir.Any node is not: this compiler mints one only for a degraded lowering (an
// enum past its member budget, an unconvertible const), which stands for a
// narrower declaration.
func isTop(td ir.TypeDef) bool {
	p, ok := td.(*ir.Primitive)
	return ok && p.Prim == ir.PrimAny
}

// isDegraded reports whether td is an Any node. This compiler mints one only
// for a degraded lowering, so it stands for a declaration the judgment cannot
// see: it is related to nothing, and a drop against it is kept.
func isDegraded(td ir.TypeDef) bool {
	_, ok := td.(*ir.Any)
	return ok
}

// nodeAgainstBare compares a Scalar, Enum or Literal (ref, resolved to td)
// against the bare primitive ref bare, of kind bareKind, from the node's side.
// Any other node is unrelated. Where an Enum's kind and the bare kind overlap,
// the pair is disjoint when the bare kind refuses every member, as literalAgainst
// judges one value, so an enum with no members is disjoint from any such kind.
func (j typeJudge) nodeAgainstBare(ref ir.TypeRef, td ir.TypeDef, bare ir.TypeRef, bareKind ir.PrimKind) relation {
	if lit, ok := td.(*ir.Literal); ok {
		return literalAgainst(lit.Value, bareKind, bare.Nullable)
	}
	if _, ok := td.(*ir.Scalar); !ok {
		if _, ok = td.(*ir.Enum); !ok {
			return relUnrelated
		}
	}
	kind, ok := j.primKind(ref)
	if !ok {
		return relUnrelated
	}
	rel := fromSpelling(spelling.Relate(kind, bareKind))
	if rel == relSame {
		return relANarrower
	}
	if rel == relBNarrower {
		rel = relOverlap
	}
	if en, ok := td.(*ir.Enum); ok && rel == relOverlap && noMemberMeets(en, bareKind, bare.Nullable) {
		return relDisjoint
	}
	return rel
}

// literalAgainst compares a literal's value with a bare primitive: the literal
// is narrower when the kind admits it. When it does not, the pair is disjoint
// where the kind has a judge, and an overlap where it has none, as a bytes or
// datetime kind has none. A null is judged against the bare side's nullability,
// not its kind: narrower where the side admits null, disjoint where it does not.
func literalAgainst(v ir.Value, kind ir.PrimKind, bareNullable bool) relation {
	switch {
	case v.Kind == ir.ValueNull && bareNullable:
		return relANarrower
	case v.Kind == ir.ValueNull:
		return relDisjoint
	case admits(kind, v):
		return relANarrower
	case kind == ir.PrimBytes || kind == ir.PrimDatetime:
		return relOverlap
	default:
		return relDisjoint
	}
}

// noMemberMeets reports whether kind refuses every member of en, each judged
// by literalAgainst: vacuously true for an enum with no members, and false
// where the kind has no judge, as bytes has none.
func noMemberMeets(en *ir.Enum, kind ir.PrimKind, nullable bool) bool {
	for _, m := range en.Members {
		if literalAgainst(m.Value, kind, nullable) != relDisjoint {
			return false
		}
	}
	return true
}

// enumMisses reports whether ref targets an Enum no member of which kind
// admits, judging each member as literalAgainst does: an enum with no members
// is vacuously disjoint, and a kind with no judge refuses nothing.
func (j typeJudge) enumMisses(ref ir.TypeRef, kind ir.PrimKind, nullable bool) bool {
	td, ok := j.Resolve(ref.Target)
	en, isEnum := td.(*ir.Enum)
	return ok && isEnum && noMemberMeets(en, kind, nullable)
}

// extras is what a list or map node holds beyond its element.
type extras struct {
	Constraints *ir.Constraints
	Encoding    *ir.Encoding
	Docs        ir.Docs
	Examples    []ir.Example
	Unmodeled   ir.Unmodeled
}

// containers compares two lists or two maps with an equal key by their element
// or value, recursively.
func (j typeJudge) containers(at, bt ir.TypeDef, depth int) relation {
	switch a := at.(type) {
	case *ir.List:
		b, ok := bt.(*ir.List)
		if !ok {
			return relUnrelated
		}
		return j.contained(a.Elem, b.Elem, extras{a.Constraints, a.Encoding, a.Docs, a.Examples, a.Unmodeled},
			extras{b.Constraints, b.Encoding, b.Docs, b.Examples, b.Unmodeled}, depth)
	case *ir.MapT:
		b, ok := bt.(*ir.MapT)
		if !ok || !reflect.DeepEqual(a.Key, b.Key) {
			return relUnrelated
		}
		return j.contained(a.Value, b.Value, extras{a.Constraints, nil, a.Docs, a.Examples, a.Unmodeled},
			extras{b.Constraints, nil, b.Docs, b.Examples, b.Unmodeled}, depth)
	default:
		return relUnrelated
	}
}

// contained is the verdict on two containers whose members are ae and be. A
// narrower member narrows the container only when the winner admits null no
// more widely than the loser, and the loser holds nothing the winner lacks.
// Disjoint members still overlap in a shared value: the empty container, or a
// null both members admit.
func (j typeJudge) contained(ae, be ir.TypeRef, ax, bx extras, depth int) relation {
	rel := j.relateAt(ae, be, depth+1)
	if rel == relDisjoint && (ae.Nullable && be.Nullable || admitsEmpty(ax) && admitsEmpty(bx)) {
		// An empty list or map, or one of nulls where both members admit null, is a
		// value of both, so disjoint members overlap.
		return relOverlap
	}
	if rel == relSame {
		rel = byNull(ae, be)
	}
	if rel == relANarrower {
		return narrowedBy(ae, be, ax, bx, rel)
	}
	if rel == relBNarrower {
		return narrowedBy(be, ae, bx, ax, rel)
	}
	if rel == relSame && !reflect.DeepEqual(ax, bx) {
		return relOverlap
	}
	return rel
}

// admitsEmpty reports whether a container with x admits the empty one: true when
// x.Constraints is nil or holds neither MinItems nor MinProps above zero.
func admitsEmpty(x extras) bool {
	c := x.Constraints
	if c == nil {
		return true
	}
	return (c.MinItems == nil || *c.MinItems <= 0) && (c.MinProps == nil || *c.MinProps <= 0)
}

// byNull orders two members of one target by null: the one that admits less is
// narrower.
func byNull(a, b ir.TypeRef) relation {
	switch {
	case a.Nullable == b.Nullable:
		return relSame
	case a.Nullable:
		return relBNarrower
	default:
		return relANarrower
	}
}

// narrowedBy returns rel when the narrower member win, held by a container
// with extras wx, may replace the broader one, lose with extras lx; otherwise
// the pair overlaps.
func narrowedBy(win, lose ir.TypeRef, wx, lx extras, rel relation) relation {
	if win.Nullable && !lose.Nullable {
		return relOverlap
	}
	if !reflect.DeepEqual(lx, extras{}) && !reflect.DeepEqual(lx, wx) {
		return relOverlap
	}
	return rel
}

// maxTypeResolveDepth bounds the Base chain primKind follows (styleguide
// bounded-recursion rule). A scalar Base chain is far shorter than this in a
// well-formed document, so the cap only guards a pathological or malformed
// registry.
const maxTypeResolveDepth = 64

// primKind follows ref through the registry to its underlying primitive kind,
// reporting false when the target does not end in a single one (a model, union,
// list, tuple, literal, external, or a base-less opaque scalar). The Base chain
// is bounded against a malformed registry.
//
// An Enum resolves via its already-computed ValueType (docs/ir/enums.md). A
// Literal is deliberately left unresolved: a ValueNumber literal spans integer,
// number, float and decimal, so no one kind stands for it.
func (j typeJudge) primKind(ref ir.TypeRef) (ir.PrimKind, bool) {
	id := ref.Target
	for range maxTypeResolveDepth {
		td, ok := j.Resolve(id)
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

// admits reports whether v is a value of the type ref names. A null is judged
// against ref.Nullable, or the top type; any other value against the type: a
// bare primitive by its kind, an Enum by its members, a Literal by its value, a
// Scalar by its base unless it has an Encoding (which admits nothing, since the
// wire form is not the value's), and a list or map by each member. A Scalar,
// list or map also holds v to its Constraints (admitsConstraints). Nesting past
// maxJudgeDepth is refused.
func (j typeJudge) admits(ref ir.TypeRef, v ir.Value) bool { return j.admitsAt(ref, v, 0) }

func (j typeJudge) admitsAt(ref ir.TypeRef, v ir.Value, depth int) bool {
	td, ok := j.Resolve(ref.Target)
	if !ok || depth > maxJudgeDepth {
		return false
	}
	if v.Kind == ir.ValueNull {
		return ref.Nullable || isTop(td)
	}
	switch t := td.(type) {
	case *ir.Primitive:
		return admits(t.Prim, v)
	case *ir.Any:
		return true
	case *ir.Enum:
		for _, m := range t.Members {
			if sameValue(m.Value, v) {
				return true
			}
		}
		return false
	case *ir.Literal:
		return sameValue(t.Value, v)
	case *ir.Scalar:
		return t.Encoding == nil && t.Base != nil && admitsConstraints(t.Constraints, v, depth) &&
			j.admitsAt(*t.Base, v, depth+1)
	case *ir.List:
		return v.Kind == ir.ValueList && admitsConstraints(t.Constraints, v, depth) && j.admitsAll(t.Elem, v.List, depth)
	case *ir.MapT:
		return v.Kind == ir.ValueObject && admitsConstraints(t.Constraints, v, depth) &&
			j.admitsFields(t.Value, v.Object, depth)
	default:
		return false
	}
}

// admitsAll reports whether every element of vs is a value of elem.
func (j typeJudge) admitsAll(elem ir.TypeRef, vs []ir.Value, depth int) bool {
	for _, e := range vs {
		if !j.admitsAt(elem, e, depth+1) {
			return false
		}
	}
	return true
}

// admitsFields reports whether every member of fs is a value of elem.
func (j typeJudge) admitsFields(elem ir.TypeRef, fs []ir.Field, depth int) bool {
	for _, f := range fs {
		if !j.admitsAt(elem, f.Value, depth+1) {
			return false
		}
	}
	return true
}

// sameValue reports whether a and b are one JSON value: canonicalValue spells
// them alike, so a number compares by magnitude, 1 and 1.0 being one, and an
// object by its members whatever their order. A value canonicalValue cannot
// spell compares deeply.
func sameValue(a, b ir.Value) bool {
	ak, aok := canonicalValue(a, 0)
	bk, bok := canonicalValue(b, 0)
	if aok && bok {
		return ak == bk
	}
	return reflect.DeepEqual(a, b)
}
