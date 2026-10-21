package merge

import (
	"reflect"

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
	// relDisjoint is a pair no value satisfies together.
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
}

// relate compares a with b. The winner never depends on which is which: swapping
// the arguments flips a narrowing and leaves any other verdict as it was.
// The arms, in order: the same target; the top type, which loses to anything;
// two bare primitives (spelling.Relate); a Scalar, Enum or Literal against a
// bare primitive; two lists or two maps; anything else is unrelated.
func (j typeJudge) relate(a, b ir.TypeRef) relation { return j.relateAt(a, b, 0) }

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
	aTop, bTop := isTop(at), isTop(bt)
	switch {
	case aTop && bTop:
		return relUnrelated
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
		return j.nodeAgainstBare(a, at, bp.Prim)
	case aBare:
		return j.nodeAgainstBare(b, bt, ap.Prim).flipped()
	}
	return j.containers(at, bt, depth)
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

// isTop reports whether td is the schemaless top type.
func isTop(td ir.TypeDef) bool {
	if p, ok := td.(*ir.Primitive); ok {
		return p.Prim == ir.PrimAny
	}
	_, ok := td.(*ir.Any)
	return ok
}

// nodeAgainstBare compares a Scalar, Enum or Literal (ref, resolved to td)
// against a bare primitive of kind bare, from the node's side. Any other node is
// unrelated.
func (j typeJudge) nodeAgainstBare(ref ir.TypeRef, td ir.TypeDef, bare ir.PrimKind) relation {
	if lit, ok := td.(*ir.Literal); ok {
		return literalAgainst(lit.Value, bare)
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
	rel := fromSpelling(spelling.Relate(kind, bare))
	if rel == relSame {
		return relANarrower
	}
	if rel == relBNarrower {
		return relOverlap
	}
	return rel
}

// literalAgainst compares a literal's value with a bare primitive: the literal
// is narrower when the kind admits it. When it does not, the pair is disjoint
// where the kind has a judge, and an overlap where it has none, as a bytes or
// datetime kind has none. A null is judged against the field's nullability, not
// a kind, so it never claims disjointness.
func literalAgainst(v ir.Value, kind ir.PrimKind) relation {
	switch {
	case v.Kind == ir.ValueNull:
		return relOverlap
	case admits(kind, v):
		return relANarrower
	case kind == ir.PrimBytes || kind == ir.PrimDatetime:
		return relOverlap
	default:
		return relDisjoint
	}
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
func (j typeJudge) contained(ae, be ir.TypeRef, ax, bx extras, depth int) relation {
	rel := j.relateAt(ae, be, depth+1)
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
// wire form is not the value's), and a list or map by each member. Nesting past
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
		return t.Encoding == nil && t.Base != nil && j.admitsAt(*t.Base, v, depth+1)
	case *ir.List:
		return v.Kind == ir.ValueList && j.admitsAll(t.Elem, v.List, depth)
	case *ir.MapT:
		return v.Kind == ir.ValueObject && j.admitsFields(t.Value, v.Object, depth)
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
