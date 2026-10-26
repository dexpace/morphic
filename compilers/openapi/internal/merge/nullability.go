package merge

import (
	"github.com/dexpace/morphic/ir"
)

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
	dst.Inferred = ir.JoinInferred(dst.Inferred, src.Inferred)
	return dst
}
