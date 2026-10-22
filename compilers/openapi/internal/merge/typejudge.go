package merge

import (
	"encoding/json/jsontext"
	"reflect"
	"strings"

	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/spelling"
	"github.com/dexpace/morphic/ir"
)

// relation is the verdict of comparing the types of one field declared twice
// under allOf (docs/compilers/openapi.md#allof-redeclared-property).
type relation int

const (
	// relUnrelated is a pair the judgment has no arm for; the fold drops the
	// second and typesConflict classifies the drop.
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

// dropped reports whether the fold loses the second declaration's type.
func (r relation) dropped() bool { return r <= relOverlap }

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
	// pointer; one a $ref reached elsewhere was declared apart.
	written [2]jsontext.Pointer
}

// relate compares a with b. The winner never depends on which is which:
// swapping the arguments, and written's two pointers with them, flips a
// narrowing and leaves any other verdict as it was. The arms, in order: the
// same target; a degraded Any node, which is unrelated to every type; the top
// type, which loses to anything else; two bare primitives (spelling.Relate); a
// Scalar, Enum or Literal against a bare primitive; two identical such nodes;
// two lists or two maps; anything else is unrelated.
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
	ap, aBare := at.(*ir.Primitive)
	bp, bBare := bt.(*ir.Primitive)
	switch {
	case aBare && bBare:
		return fromSpelling(spelling.Relate(ap.Prim, bp.Prim))
	case bBare:
		return j.nodeAgainstBare(a, at, b, bp.Prim)
	case aBare:
		return j.nodeAgainstBare(b, bt, a, ap.Prim).flipped()
	}
	if sameNode(at, bt) {
		return sameNodes(a, b, hoistedAt(at, j.written[0]), hoistedAt(bt, j.written[1]))
	}
	return j.containers(at, bt, depth)
}

// hoistedAt reports whether td is a hoisted node written at or under pointer,
// the declaration that holds it. A hoisted node a $ref reached from elsewhere,
// another schema's property or a parameter's schema, was declared apart.
func hoistedAt(td ir.TypeDef, pointer jsontext.Pointer) bool {
	c := td.Common()
	p := c.Provenance.Pointer
	return c.Anonymous && (p == pointer || strings.HasPrefix(string(p), string(pointer)+"/"))
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
func enumMisses(resolve func(ir.TypeID) (ir.TypeDef, bool), ref ir.TypeRef, kind ir.PrimKind, nullable bool) bool {
	td, ok := resolve(ref.Target)
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

// primKind follows ref through the registry to its underlying primitive kind,
// reporting false when the target does not end in a single one (a model, union,
// list, tuple, literal, external, or a base-less opaque scalar). The Base chain
// is bounded against a malformed registry.
//
// An Enum resolves via its already-computed ValueType (ir-design §4.5). A
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

// sameValue reports whether a and b are one value; numbers compare by
// magnitude, so 1 and 1.0 are one.
func sameValue(a, b ir.Value) bool {
	if a.Kind == ir.ValueNumber && b.Kind == ir.ValueNumber {
		order, ok := annotation.CompareBigVal(a.Num, b.Num)
		return ok && order == 0
	}
	return reflect.DeepEqual(a, b)
}
