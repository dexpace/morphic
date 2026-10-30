// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package merge

import (
	"slices"

	"github.com/dexpace/morphic/ir"
)

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
