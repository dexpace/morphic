package merge

import (
	"github.com/dexpace/morphic/compilers/openapi/internal/spelling"
	"github.com/dexpace/morphic/ir"
)

// bareNarrowing reports whether a and b both target a bare *ir.Primitive and
// one kind strictly narrows the other (spelling.Relate). aNarrower says it is
// a that does, and winner is the narrower kind.
// A Scalar or Enum target is never related here: such a pair
// resolves by branch order and keeps its warning until GitHub #782 settles it,
// and so does the top type, which keeps its null admission.
func (g *Merger) bareNarrowing(a, b ir.TypeRef) (aNarrower bool, winner ir.PrimKind, related bool) {
	ap, aok := g.barePrimitive(a)
	bp, bok := g.barePrimitive(b)
	if !aok || !bok || ap.Prim == ir.PrimAny || bp.Prim == ir.PrimAny {
		return false, "", false
	}
	rel := spelling.Relate(ap.Prim, bp.Prim)
	if rel == spelling.ANarrower {
		return true, ap.Prim, true
	}
	if rel == spelling.BNarrower {
		return false, bp.Prim, true
	}
	return false, "", false
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
