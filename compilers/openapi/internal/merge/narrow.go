package merge

import "github.com/dexpace/morphic/ir"

// maxNarrowDepth bounds the walk up primParent. The longest chain is int8 up
// to number, six edges, so the cap only guards a table edited into a cycle.
const maxNarrowDepth = 8

// primParent maps a primitive kind to its immediate supertype. Most edges nest
// by value domain: integer and float widths, integer under decimal, decimal
// and float under number, each string format under string. Two rest on
// OpenAPI's spelling instead: bytes (octets need not be Unicode text) and
// decimal128 (its domain holds NaN and infinities). Every formatTable row
// narrows its bare type; a test pins that direction only.
//
// The relation is deliberately incomplete. It omits true containments (uint8
// within int16, integer within float, int32 within float64) and overlaps
// like int32 and uint32, which keep the conflicting-redeclaration warning.
var primParent = map[ir.PrimKind]ir.PrimKind{
	ir.PrimInt8:           ir.PrimInt16,
	ir.PrimInt16:          ir.PrimInt32,
	ir.PrimInt32:          ir.PrimInt64,
	ir.PrimInt64:          ir.PrimInteger,
	ir.PrimUint8:          ir.PrimUint16,
	ir.PrimUint16:         ir.PrimUint32,
	ir.PrimUint32:         ir.PrimUint64,
	ir.PrimUint64:         ir.PrimInteger,
	ir.PrimInteger:        ir.PrimDecimal,
	ir.PrimDecimal128:     ir.PrimDecimal,
	ir.PrimDecimal:        ir.PrimNumber,
	ir.PrimFloat32:        ir.PrimFloat64,
	ir.PrimFloat64:        ir.PrimFloat,
	ir.PrimFloat:          ir.PrimNumber,
	ir.PrimDate:           ir.PrimString,
	ir.PrimTime:           ir.PrimString,
	ir.PrimDatetime:       ir.PrimString,
	ir.PrimDatetimeOffset: ir.PrimString,
	ir.PrimDuration:       ir.PrimString,
	ir.PrimURL:            ir.PrimString,
	ir.PrimUUID:           ir.PrimString,
	ir.PrimBytes:          ir.PrimString,
}

// Narrows reports whether narrow is a strict refinement of broad under
// primParent: broad is reached by walking up from narrow, and the kinds
// differ. Under allOf the pair intersects as narrow, so a redeclaration
// naming both keeps narrow.
func Narrows(narrow, broad ir.PrimKind) bool {
	kind, ok := narrow, true
	for depth := 0; depth < maxNarrowDepth && ok; depth++ {
		kind, ok = primParent[kind]
		if ok && kind == broad {
			return true
		}
	}
	return false
}

// bareNarrowing reports whether a and b both target a bare *ir.Primitive and
// one kind strictly narrows the other. aNarrower says it is a that does. A
// Scalar or Enum target is never related here: such a pair resolves by branch
// order and keeps its warning until GitHub #782 settles it.
func (g *Merger) bareNarrowing(a, b ir.TypeRef) (aNarrower, related bool) {
	ap, aok := g.barePrimitive(a)
	bp, bok := g.barePrimitive(b)
	if !aok || !bok {
		return false, false
	}
	if Narrows(ap.Prim, bp.Prim) {
		return true, true
	}
	return false, Narrows(bp.Prim, ap.Prim)
}

// barePrimitive resolves ref once and returns the node when it is a bare
// *ir.Primitive.
func (g *Merger) barePrimitive(ref ir.TypeRef) (*ir.Primitive, bool) {
	td, ok := g.Resolve(ref.Target)
	if !ok {
		return nil, false
	}
	p, ok := td.(*ir.Primitive)
	return p, ok
}
